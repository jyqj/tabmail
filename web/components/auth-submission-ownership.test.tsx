import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { toast } from "sonner";
import { installSession } from "@/lib/session";
import { AuthDialog } from "./auth-dialog";

const { loginWithTokens } = vi.hoisted(() => ({ loginWithTokens: vi.fn() }));
vi.mock("@/contexts/auth-context", () => ({
  useAuth: () => ({ level: "public", user: null, loginWithTokens, logout: vi.fn() }),
}));

type Mode = "login" | "register";
type Call = { path: string; body: unknown };
let calls: Call[];
let reply: (call: Call) => Promise<Response>;
let lockRequest: ReturnType<typeof vi.fn>;
let afterCredentialCommit: (() => void) | undefined;
const user = { id: "sign-in-user", tenant_id: "sign-in-tenant", role: "user" as const, email: "candidate@example.test", display_name: "Candidate" };
const other = { ...user, id: "newer-user", tenant_id: "newer-tenant", email: "newer@example.test", display_name: "Newer user" };
const response = (data: unknown, status = 200) => new Response(JSON.stringify(data), { status, headers: { "Content-Type": "application/json" } });
const success = () => response({ data: { access_token: "candidate-token", user } });
const failure = () => response({ error: { code: "BAD_REQUEST", message: "synthetic credential failure" } }, 400);
function pendingReply() {
  let resolve!: (value: Response) => void;
  const promise = new Promise<Response>(resolveReply => { resolve = resolveReply; });
  let first = true;
  reply = async () => { if (first) { first = false; return promise; } return failure(); };
  return resolve;
}
const passwordInput = () => screen.getByLabelText("Password") as HTMLInputElement;
const actionButton = (mode: Mode) => screen.getAllByRole("button", { name: mode === "login" ? "Sign In" : "Create Account" }).at(-1)!;
async function openDialog(mode: Mode = "login") {
  const rendered = render(<AuthDialog />);
  fireEvent.click(screen.getByRole("button", { name: "Sign In" }));
  await screen.findByLabelText("Email address");
  if (mode === "register") fireEvent.click(screen.getByRole("button", { name: "Register now" }));
  fill();
  return rendered;
}
function fill(email = "candidate@example.test", password = "synthetic-password") {
  fireEvent.change(screen.getByLabelText("Email address"), { target: { value: email } });
  fireEvent.change(passwordInput(), { target: { value: password } });
}
function closeDialog() {
  // The original custom close control has no accessible name; retain the same
  // real control for the fixed baseline and candidate behavior comparison.
  fireEvent.click(screen.getByRole("dialog").querySelector("button")!);
}
async function settle() { await act(async () => { for (let i = 0; i < 35; i++) await Promise.resolve(); }); }

beforeEach(() => {
  calls = [];
  afterCredentialCommit = undefined;
  reply = async () => failure();
  loginWithTokens.mockImplementation(installSession);
  let tail = Promise.resolve<unknown>(undefined);
  lockRequest = vi.fn((_name: string, work: () => Promise<unknown>) => {
    const result = tail.then(async () => {
      const value = await work();
      // A controlled lock-completion handoff models another credential owner
      // committing before the first component's awaited continuation resumes.
      afterCredentialCommit?.();
      return value;
    });
    tail = result.then(() => undefined, () => undefined);
    return result;
  });
  vi.stubGlobal("navigator", { locks: { request: lockRequest } });
  vi.spyOn(toast, "success").mockImplementation(() => "success-toast");
  vi.spyOn(toast, "error").mockImplementation(() => "error-toast");
  vi.stubGlobal("fetch", async (input: RequestInfo | URL, init?: RequestInit) => {
    const call = { path: new URL(String(input), "http://localhost").pathname, body: init?.body ? JSON.parse(String(init.body)) : undefined };
    calls.push(call);
    return reply(call);
  });
});
afterEach(() => { cleanup(); vi.unstubAllGlobals(); });

describe("credential submission ownership", () => {
  it.each(["login", "register"] as const)("does not queue duplicate %s writes from repeated Enter or a simultaneous click", async mode => {
    const resolve = pendingReply();
    await openDialog(mode);
    const input = passwordInput(), button = actionButton(mode);
    act(() => {
      fireEvent.keyDown(input, { key: "Enter" });
      fireEvent.keyDown(input, { key: "Enter" });
      fireEvent.click(button);
    });
    await settle();
    const queued = lockRequest.mock.calls.length;
    await act(async () => resolve(failure()));
    await settle();
    expect(queued).toBe(1);
    expect(calls).toHaveLength(1);
    expect(toast.error).toHaveBeenCalledTimes(1);
    expect(actionButton(mode)).toBeEnabled();
  });

  it.each(["login", "register"] as const)("does not submit %s while Enter finishes IME composition", async mode => {
    await openDialog(mode);
    fireEvent.keyDown(passwordInput(), { key: "Enter", isComposing: true });
    await settle();
    expect(calls).toHaveLength(0);
    expect(lockRequest).not.toHaveBeenCalled();
  });

  it.each(["login", "register"] as const)("does not submit %s from the legacy IME key-code event", async mode => {
    await openDialog(mode);
    fireEvent.keyDown(passwordInput(), { key: "Enter", keyCode: 229 });
    await settle();
    expect(calls).toHaveLength(0);
  });

  it("releases the pending slot after rejection and requires a new explicit retry", async () => {
    const resolve = pendingReply();
    await openDialog();
    fireEvent.keyDown(passwordInput(), { key: "Enter" });
    await settle();
    await act(async () => resolve(failure()));
    await waitFor(() => expect(actionButton("login")).toBeEnabled());
    expect(calls).toHaveLength(1);
    fireEvent.keyDown(passwordInput(), { key: "Enter" });
    await waitFor(() => expect(calls).toHaveLength(2));
    await settle();
    expect(toast.error).toHaveBeenCalledTimes(2);
  });

  it.each(["success", "failure"] as const)("retires a closed and reopened dialog before a late %s", async result => {
    const resolve = pendingReply();
    await openDialog();
    fireEvent.keyDown(passwordInput(), { key: "Enter" });
    await settle();
    closeDialog();
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    fireEvent.click(screen.getByRole("button", { name: "Sign In" }));
    await screen.findByLabelText("Email address");
    fill("later@example.test", "later-password");
    await act(async () => resolve(result === "success" ? success() : failure()));
    await settle();
    expect(screen.getByRole("dialog")).toBeVisible();
    expect(screen.getByLabelText("Email address")).toHaveValue("later@example.test");
    expect(passwordInput()).toHaveValue("later-password");
    expect(actionButton("login")).toBeEnabled();
    expect(toast.success).not.toHaveBeenCalled();
    expect(toast.error).not.toHaveBeenCalled();
    expect(loginWithTokens).not.toHaveBeenCalled();
  });

  it.each(["success", "failure"] as const)("does not overwrite a different form after a late %s", async result => {
    const resolve = pendingReply();
    await openDialog();
    fireEvent.keyDown(passwordInput(), { key: "Enter" });
    await settle();
    fireEvent.click(screen.getByRole("button", { name: "Register now" }));
    fill("registration@example.test", "registration-password");
    fireEvent.keyDown(passwordInput(), { key: "Enter" });
    await act(async () => resolve(result === "success" ? success() : failure()));
    await settle();
    expect(screen.getByRole("dialog")).toBeVisible();
    expect(screen.getByLabelText("Email address")).toHaveValue("registration@example.test");
    expect(passwordInput()).toHaveValue("registration-password");
    expect(calls).toHaveLength(1);
    expect(toast.success).not.toHaveBeenCalled();
    expect(toast.error).not.toHaveBeenCalled();
  });

  it.each(["success", "failure"] as const)("drops a departed form's late %s feedback", async result => {
    const resolve = pendingReply();
    const view = await openDialog();
    fireEvent.keyDown(passwordInput(), { key: "Enter" });
    await settle();
    view.unmount();
    await act(async () => resolve(result === "success" ? success() : failure()));
    await settle();
    expect(toast.success).not.toHaveBeenCalled();
    expect(toast.error).not.toHaveBeenCalled();
    expect(loginWithTokens).not.toHaveBeenCalled();
  });

  it("does not reinstall an old credential after a newer owner commits", async () => {
    reply = async () => success();
    afterCredentialCommit = () => installSession("newer-token", other);
    await openDialog();
    fireEvent.keyDown(passwordInput(), { key: "Enter" });
    await settle();
    expect(calls).toHaveLength(1);
    expect(localStorage.getItem("tabmail_access_token")).toBe("newer-token");
    expect(JSON.parse(localStorage.getItem("tabmail_user")!)).toEqual(other);
    expect(localStorage.getItem("tabmail_tenant_id")).toBe(other.tenant_id);
    expect(loginWithTokens).not.toHaveBeenCalled();
    expect(toast.success).not.toHaveBeenCalled();
  });

  it.each(["login", "register"] as const)("keeps normal successful %s under the credential layer's single installation", async mode => {
    reply = async () => success();
    await openDialog(mode);
    fireEvent.keyDown(passwordInput(), { key: "Enter" });
    await waitFor(() => expect(toast.success).toHaveBeenCalledTimes(1));
    expect(localStorage.getItem("tabmail_access_token")).toBe("candidate-token");
    expect(JSON.parse(localStorage.getItem("tabmail_user")!)).toEqual(user);
    expect(loginWithTokens).not.toHaveBeenCalled();
    expect(calls).toHaveLength(1);
  });
});
