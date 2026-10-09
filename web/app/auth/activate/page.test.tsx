import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { toast } from "sonner";
import ActivatePage from "./page";

// The employee activation page and company request facade are real. Synthetic
// HTTP responses control only the server outcome and timing of one-shot tokens.
const tokenA = "a".repeat(64), tokenB = "b".repeat(64);
const ready = "Your account and personal mailbox are ready. Sign in with the login email specified in your invitation.";
const submitLabel = "Activate and provision mailbox";
type Call = { path: string; method: string; headers: Headers; body: { token: string; password: string } };
let calls: Call[];
let reply: () => Promise<Response>;
const json = (data: unknown, status = 200) => new Response(JSON.stringify(status === 200 ? { data } : data), {
  status, headers: { "Content-Type": "application/json" },
});
const deferred = () => {
  let resolve!: (response: Response) => void;
  const promise = new Promise<Response>(done => { resolve = done; });
  return { promise, resolve };
};
function invitation(token: string) {
  history.replaceState({ route: "activation", retained: 1 }, "", `/auth/activate?retained=1#${token}`);
  window.dispatchEvent(new HashChangeEvent("hashchange"));
}
async function settle() { await act(async () => { for (let i = 0; i < 20; i++) await Promise.resolve(); }); }
async function mount(password = "valid password") {
  const mounted = render(<ActivatePage />);
  await settle();
  fireEvent.change(screen.getByLabelText("New password"), { target: { value: password } });
  fireEvent.change(screen.getByLabelText("Confirm password"), { target: { value: password } });
  return mounted;
}
const submit = () => fireEvent.submit(screen.getByLabelText("New password").closest("form")!);

beforeEach(() => {
  calls = [];
  invitation(tokenA);
  reply = async () => json({ activated: true });
  vi.spyOn(toast, "error").mockImplementation(() => "error-toast");
  vi.stubGlobal("fetch", async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = new URL(String(input), window.location.href);
    calls.push({ path: url.pathname, method: init?.method ?? "GET", headers: new Headers(init?.headers), body: JSON.parse(String(init?.body)) });
    return reply();
  });
});
afterEach(() => { cleanup(); vi.unstubAllGlobals(); history.replaceState(null, "", "/"); });

describe("employee activation password bytes", () => {
  it.each([
    ["12 ASCII bytes", "a".repeat(12)], ["12 CJK bytes", "员工密码"], ["12 emoji bytes", "🙂".repeat(3)],
    ["72 ASCII bytes", "a".repeat(72)], ["72 CJK bytes", "员".repeat(24)], ["72 emoji bytes", "🙂".repeat(18)],
    ["surrounding spaces", "  password  "],
  ])("accepts %s without trimming, truncating or character-count rejection", async (_name, password) => {
    await mount(password);
    const button = screen.getByRole("button", { name: submitLabel });
    expect(button).toBeEnabled();
    expect(screen.getByLabelText("New password")).toBeValid();
    fireEvent.click(button);
    await settle();
    expect(calls).toHaveLength(1);
    expect(calls[0]).toMatchObject({ path: "/api/v1/company/activate", method: "POST", body: { token: tokenA, password } });
    expect(calls[0].headers.get("Authorization")).toBeNull();
    expect(screen.getByRole("status")).toHaveTextContent(ready);
  });

  it.each(["a".repeat(11), "a".repeat(73), "员".repeat(25), "🙂".repeat(19)])(
    "rejects invalid byte length in both button eligibility and programmatic submit: %j", async password => {
      await mount(password);
      const disabled = (screen.getByRole("button", { name: submitLabel }) as HTMLButtonElement).disabled;
      submit(); // Exercise the action guard independently of native form validation.
      await settle();
      expect.soft(disabled).toBe(true);
      expect(calls).toHaveLength(0);
      expect(window.location.hash).toBe(`#${tokenA}`);
      expect(screen.queryByText(ready)).not.toBeInTheDocument();
    },
  );

  it("keeps confirmation and invitation validity independent of byte length", async () => {
    await mount();
    fireEvent.change(screen.getByLabelText("Confirm password"), { target: { value: "different password" } });
    submit();
    await settle();
    expect(calls).toHaveLength(0);
    expect(toast.error).toHaveBeenCalledWith("Passwords do not match");
    await act(async () => invitation("invalid-token"));
    fireEvent.change(screen.getByLabelText("New password"), { target: { value: "valid password" } });
    fireEvent.change(screen.getByLabelText("Confirm password"), { target: { value: "valid password" } });
    submit();
    await settle();
    expect(calls).toHaveLength(0);
    expect(toast.error).toHaveBeenCalledWith("Invalid link. Ask your administrator for the complete activation link.");
  });
});

describe("employee activation token and outcome ownership", () => {
  it("freezes submitted password inputs and admits only one pending POST", async () => {
    const pending = deferred(); reply = () => pending.promise;
    await mount();
    submit(); submit();
    await settle();
    const passwordDisabled = (screen.getByLabelText("New password") as HTMLInputElement).disabled;
    const confirmationDisabled = (screen.getByLabelText("Confirm password") as HTMLInputElement).disabled;
    await act(async () => pending.resolve(json({ activated: true })));
    expect(passwordDisabled).toBe(true);
    expect(confirmationDisabled).toBe(true);
    expect(calls).toHaveLength(1);
  });

  it("clears only its activation fragment after a confirmed success", async () => {
    await mount(); submit(); await settle();
    expect(screen.getByRole("status")).toHaveTextContent(ready);
    expect(window.location.hash).toBe("");
    expect(window.location.search).toBe("?retained=1");
    expect(history.state).toEqual({ route: "activation", retained: 1 });
  });

  it.each([null, {}, { activated: false }])("does not declare readiness from an unconfirmed response %j", async data => {
    reply = async () => json(data);
    await mount(); submit(); await settle();
    expect(screen.queryByText(ready)).not.toBeInTheDocument();
    expect(window.location.hash).toBe(`#${tokenA}`);
    expect(screen.getByLabelText("New password")).toHaveValue("valid password");
    expect(screen.getByRole("button", { name: submitLabel })).toBeEnabled();
    expect(toast.error).toHaveBeenCalled();
  });

  it.each(["new invitation", "away and back"])("a late success cannot consume a %s view", async navigation => {
    const pending = deferred(); reply = () => pending.promise;
    await mount(); submit(); await settle();
    await act(async () => invitation(tokenB));
    if (navigation === "away and back") await act(async () => invitation(tokenA));
    const activeToken = navigation === "new invitation" ? tokenB : tokenA;
    const resetValue = (screen.getByLabelText("New password") as HTMLInputElement).value;
    await act(async () => pending.resolve(json({ activated: true })));
    await settle();
    expect(resetValue).toBe("");
    expect(window.location.hash).toBe(`#${activeToken}`);
    expect(screen.queryByText(ready)).not.toBeInTheDocument();
    expect(screen.getByLabelText("New password")).toHaveValue("");
    expect(toast.error).not.toHaveBeenCalled();
  });

  it("a late failure cannot report over the next invitation", async () => {
    const pending = deferred(); reply = () => pending.promise;
    await mount(); submit(); await settle();
    await act(async () => invitation(tokenB));
    await act(async () => pending.resolve(json({ error: { code: "NOT_FOUND", message: "Old invitation expired" } }, 404)));
    await settle();
    expect(toast.error).not.toHaveBeenCalled();
    expect(window.location.hash).toBe(`#${tokenB}`);
    expect(screen.getByLabelText("New password")).toHaveValue("");
  });

  it.each([true, false])("unmount retires URL and feedback effects after success=%s", async success => {
    const pending = deferred(); reply = () => pending.promise;
    const mounted = await mount(); submit(); await settle();
    mounted.unmount();
    const before = window.location.href;
    await act(async () => pending.resolve(success ? json({ activated: true }) : json({ error: { code: "NOT_FOUND", message: "Old invitation expired" } }, 404)));
    await settle();
    expect(window.location.href).toBe(before);
    expect(toast.error).not.toHaveBeenCalled();
  });

  it("retains the current invitation and exact inputs after a current failure", async () => {
    reply = async () => json({ error: { code: "NOT_FOUND", message: "Invitation expired" } }, 404);
    await mount(); submit(); await settle();
    expect(toast.error).toHaveBeenCalledWith("Invitation expired");
    expect(screen.getByLabelText("New password")).toHaveValue("valid password");
    expect(screen.getByLabelText("Confirm password")).toHaveValue("valid password");
    expect(window.location.hash).toBe(`#${tokenA}`);
    expect(screen.getByRole("button", { name: submitLabel })).toBeEnabled();
  });
});
