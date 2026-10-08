import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { toast } from "sonner";
import { AuthProvider } from "@/contexts/auth-context";
import { installSession } from "@/lib/session";
import { AuthDialog } from "./auth-dialog";

const user = { id: "provider-candidate", tenant_id: "provider-tenant", role: "user" as const, email: "candidate@example.test", display_name: "Provider candidate" };
const newer = { ...user, id: "newer-user", tenant_id: "newer-tenant", email: "newer@example.test", display_name: "Newer candidate" };
const response = (body: unknown) => new Response(JSON.stringify(body), { status: 200, headers: { "Content-Type": "application/json" } });
const success = () => response({ data: { access_token: "provider-token", user } });
let credentialReply: () => Promise<Response>;
let afterCommit: (() => void) | undefined;
function pendingReply() {
  let resolve!: (response: Response) => void;
  const promise = new Promise<Response>(resolveReply => { resolve = resolveReply; });
  credentialReply = () => promise;
  return resolve;
}
async function open(mode: "login" | "register") {
  render(<AuthProvider><AuthDialog /></AuthProvider>);
  await act(async () => { await new Promise(resolve => setTimeout(resolve, 10)); });
  fireEvent.click(screen.getByRole("button", { name: "Sign In" }));
  if (mode === "register") fireEvent.click(await screen.findByRole("button", { name: "Register now" }));
  fireEvent.change(await screen.findByLabelText("Email address"), { target: { value: user.email } });
  fireEvent.change(screen.getByLabelText("Password"), { target: { value: "synthetic-password" } });
}
async function settle() { await act(async () => { for (let i = 0; i < 40; i++) await Promise.resolve(); }); }

beforeEach(() => {
  credentialReply = async () => success();
  afterCommit = undefined;
  vi.stubGlobal("navigator", { locks: { request: vi.fn(async (_name: string, run: () => Promise<unknown>) => { const value = await run(); afterCommit?.(); return value; }) } });
  vi.spyOn(toast, "success").mockImplementation(() => "success-toast");
  vi.spyOn(toast, "error").mockImplementation(() => "error-toast");
  vi.stubGlobal("fetch", async (input: RequestInfo | URL) => {
    if (/\/auth\/(login|register)$/.test(String(input))) return credentialReply();
    return response({ data: { can_send: true } });
  });
});
afterEach(() => { cleanup(); vi.unstubAllGlobals(); });

describe("credential completion across the real provider's session remount", () => {
  it("reports ordinary registration once after its own identity is installed", async () => {
    await open("register");
    fireEvent.keyDown(screen.getByLabelText("Password"), { key: "Enter" });
    await waitFor(() => expect(screen.getByText(user.display_name)).toBeVisible());
    await waitFor(() => expect(toast.success).toHaveBeenCalledWith("Account created! Welcome, Provider candidate"));
    expect(toast.success).toHaveBeenCalledTimes(1);
    expect(localStorage.getItem("tabmail_access_token")).toBe("provider-token");
  });

  it("does not revive completion for a form edited before its login commits", async () => {
    const resolve = pendingReply();
    await open("login");
    fireEvent.keyDown(screen.getByLabelText("Password"), { key: "Enter" });
    await settle();
    fireEvent.change(screen.getByLabelText("Email address"), { target: { value: "later@example.test" } });
    await act(async () => resolve(success()));
    await waitFor(() => expect(screen.getByText(user.display_name)).toBeVisible());
    await settle();
    expect(toast.success).not.toHaveBeenCalled();
    expect(toast.error).not.toHaveBeenCalled();
  });

  it("does not revive a closed registration dialog when the cookie mutation finishes", async () => {
    const resolve = pendingReply();
    await open("register");
    fireEvent.keyDown(screen.getByLabelText("Password"), { key: "Enter" });
    await settle();
    fireEvent.click(screen.getByRole("button", { name: "Close sign-in dialog" }));
    await act(async () => resolve(success()));
    await waitFor(() => expect(screen.getByText(user.display_name)).toBeVisible());
    await settle();
    expect(toast.success).not.toHaveBeenCalled();
    expect(toast.error).not.toHaveBeenCalled();
  });

  it("rejects its remount receipt if a newer identity commits before continuation", async () => {
    afterCommit = () => installSession("newer-token", newer);
    await open("login");
    fireEvent.keyDown(screen.getByLabelText("Password"), { key: "Enter" });
    await waitFor(() => expect(screen.getByText(newer.display_name)).toBeVisible());
    await settle();
    expect(localStorage.getItem("tabmail_access_token")).toBe("newer-token");
    expect(toast.success).not.toHaveBeenCalled();
    expect(toast.error).not.toHaveBeenCalled();
  });
});
