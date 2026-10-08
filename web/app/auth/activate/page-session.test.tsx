import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { toast } from "sonner";
import { advanceSession, AUTH_EVENT } from "@/lib/session";
import ActivatePage from "./page";

// Exercise the real activation page, session store and HTTP facade. Replacing
// a session retires old work, but must leave the same invitation usable anew.
const token = "a".repeat(64);
const ready = "Your account and personal mailbox are ready. Sign in with the login email specified in your invitation.";
const submitLabel = "Activate and provision mailbox";
let calls: { token: string; password: string }[];
let reply: () => Promise<Response>;
const json = (data: unknown, status = 200) => new Response(JSON.stringify(status === 200 ? { data } : data), {
  status, headers: { "Content-Type": "application/json" },
});
const deferred = () => {
  let resolve!: (response: Response) => void;
  const promise = new Promise<Response>(done => { resolve = done; });
  return { promise, resolve };
};
async function settle() { await act(async () => { for (let i = 0; i < 20; i++) await Promise.resolve(); }); }
function fill(password: string) {
  fireEvent.change(screen.getByLabelText("New password"), { target: { value: password } });
  fireEvent.change(screen.getByLabelText("Confirm password"), { target: { value: password } });
}
const submit = () => fireEvent.submit(screen.getByLabelText("New password").closest("form")!);
async function mount() {
  render(<ActivatePage />);
  await settle();
  fill("old session password");
}

beforeEach(() => {
  calls = [];
  history.replaceState({ retained: 1 }, "", `/auth/activate?retained=1#${token}`);
  reply = async () => json({ activated: true });
  vi.spyOn(toast, "error").mockImplementation(() => "error-toast");
  vi.stubGlobal("fetch", async (_input: RequestInfo | URL, init?: RequestInit) => {
    calls.push(JSON.parse(String(init?.body)));
    return reply();
  });
});
afterEach(() => { cleanup(); vi.unstubAllGlobals(); history.replaceState(null, "", "/"); });

describe("activation form session lifecycle", () => {
  it.each([true, false])("admits an explicit fresh submit on the same invitation while old success=%s is pending", async oldSuccess => {
    const old = deferred(), fresh = deferred();
    reply = () => old.promise;
    await mount(); submit(); await settle();
    expect(calls).toHaveLength(1);

    await act(async () => advanceSession());
    expect(screen.getByLabelText("New password")).toHaveValue("");
    expect(screen.getByLabelText("Confirm password")).toHaveValue("");
    expect(screen.getByLabelText("New password")).toBeEnabled();
    expect(window.location.hash).toBe(`#${token}`);
    expect(calls).toHaveLength(1); // Retiring a session must never replay a POST.

    fill("fresh session password");
    reply = () => fresh.promise;
    submit(); await settle();
    expect(calls).toEqual([
      { token, password: "old session password" },
      { token, password: "fresh session password" },
    ]);
    await act(async () => old.resolve(oldSuccess ? json({ activated: true }) : json({ error: { code: "NOT_FOUND", message: "Old invitation result" } }, 404)));
    await settle();
    expect(screen.queryByText(ready)).not.toBeInTheDocument();
    expect(screen.getByLabelText("New password")).toHaveValue("fresh session password");
    expect(screen.getByLabelText("New password")).toBeDisabled();
    expect(window.location.hash).toBe(`#${token}`);
    expect(toast.error).not.toHaveBeenCalled();

    await act(async () => fresh.resolve(json({ activated: true })));
    await settle();
    expect(screen.getByRole("status")).toHaveTextContent(ready);
    expect(window.location.hash).toBe("");
    expect(window.location.search).toBe("?retained=1");
    expect(history.state).toEqual({ retained: 1 });
  });

  it("resets and accepts the same invitation after a session change from another tab", async () => {
    await mount();
    await act(async () => {
      localStorage.setItem("tabmail_tenant_id", "another-session-tenant");
      window.dispatchEvent(new StorageEvent("storage", { key: "tabmail_tenant_id" }));
    });
    expect(screen.getByLabelText("New password")).toHaveValue("");
    expect(screen.getByLabelText("Confirm password")).toHaveValue("");
    fill("fresh session password");
    submit(); await settle();
    expect(calls).toEqual([{ token, password: "fresh session password" }]);
    expect(screen.getByRole("status")).toHaveTextContent(ready);
  });

  it("keeps the current form and pending action through same-session token rotation", async () => {
    const pending = deferred(); reply = () => pending.promise;
    await mount(); submit(); await settle();
    const input = screen.getByLabelText("New password");
    await act(async () => {
      localStorage.setItem("tabmail_access_token", "rotated-access-token");
      window.dispatchEvent(new Event(AUTH_EVENT));
    });
    expect(screen.getByLabelText("New password")).toBe(input);
    expect(input).toHaveValue("old session password");
    expect(input).toBeDisabled();
    expect(screen.getByRole("button", { name: submitLabel })).toBeDisabled();
    expect(calls).toHaveLength(1);
    await act(async () => pending.resolve(json({ activated: true })));
    await settle();
    expect(screen.getByRole("status")).toHaveTextContent(ready);
  });
});
