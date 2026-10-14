import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { toast } from "sonner";
import { AuthProvider, useAuth } from "@/contexts/auth-context";
import { clearSessionCredentials, installSession } from "@/lib/session";
import AccountPage from "./page";

const { replace } = vi.hoisted(() => ({ replace: vi.fn() }));
vi.mock("next/navigation", () => ({ useRouter: () => ({ replace }) }));
const original = { id: "provider-password-user", tenant_id: "provider-password-tenant", role: "user" as const, email: "password@example.test", display_name: "Password user" };
const newer = { ...original, id: "newer-user", tenant_id: "newer-tenant", email: "newer@example.test", display_name: "Newer user" };
const response = (body: unknown) => new Response(JSON.stringify(body), { status: 200, headers: { "Content-Type": "application/json" } });
const success = () => response({ data: { status: "password changed; sign in again" } });
let passwordReply: () => Promise<Response>;
let afterCommit: (() => void) | undefined;
let passwordCalls: number;
function Identity() {
  const { user } = useAuth();
  return <p data-testid="current-identity">{user?.email ?? "anonymous"}</p>;
}
function pendingReply() {
  let resolve!: (response: Response) => void;
  const promise = new Promise<Response>(resolveReply => { resolve = resolveReply; });
  passwordReply = () => promise;
  return resolve;
}
function fill(password = "synthetic-new-password") {
  fireEvent.change(screen.getByLabelText("Current password"), { target: { value: "synthetic-current-password" } });
  fireEvent.change(screen.getByLabelText("New password (12–72 bytes)"), { target: { value: password } });
  fireEvent.change(screen.getByLabelText("Confirm new password"), { target: { value: password } });
}
async function open() {
  const view = render(<AuthProvider><Identity /><AccountPage /></AuthProvider>);
  await waitFor(() => expect(screen.getByTestId("current-identity")).toHaveTextContent(original.email));
  fill();
  return view;
}
async function settle() { await act(async () => { for (let i = 0; i < 40; i++) await Promise.resolve(); }); }

beforeEach(() => {
  installSession("original-token", original);
  passwordCalls = 0;
  passwordReply = async () => success();
  afterCommit = undefined;
  vi.stubGlobal("navigator", { locks: { request: vi.fn(async (_name: string, run: () => Promise<unknown>) => { const value = await run(); afterCommit?.(); return value; }) } });
  vi.spyOn(toast, "success").mockImplementation(() => "success-toast");
  vi.spyOn(toast, "error").mockImplementation(() => "error-toast");
  vi.stubGlobal("fetch", async (input: RequestInfo | URL) => {
    if (String(input).endsWith("/auth/change-password")) { passwordCalls++; return passwordReply(); }
    return response({ data: { can_send: true } });
  });
});
afterEach(() => { cleanup(); vi.unstubAllGlobals(); });

describe("password completion across the real provider's credential-clear remount", () => {
  it("reports success and returns to sign-in once after its own credentials are cleared", async () => {
    await open();
    const changed = vi.fn();
    window.addEventListener("tabmail-auth-change", changed);
    try {
      fireEvent.click(screen.getByRole("button", { name: "Update password" }));
      await waitFor(() => expect(screen.getByTestId("current-identity")).toHaveTextContent("anonymous"));
      await waitFor(() => expect(replace).toHaveBeenCalledWith("/"));
      expect(replace).toHaveBeenCalledTimes(1);
      expect(toast.success).toHaveBeenCalledWith("Password changed. Please sign in again.");
      expect(toast.success).toHaveBeenCalledTimes(1);
      expect(changed).toHaveBeenCalledTimes(1);
      expect(localStorage.getItem("tabmail_access_token")).toBeNull();
      expect(passwordCalls).toBe(1);
    } finally { window.removeEventListener("tabmail-auth-change", changed); }
  });

  it("does not revive an edited form's old completion after provider remount", async () => {
    const resolve = pendingReply();
    await open();
    fireEvent.click(screen.getByRole("button", { name: "Update password" }));
    await settle();
    fill("later-edited-password");
    await act(async () => resolve(success()));
    await waitFor(() => expect(screen.getByTestId("current-identity")).toHaveTextContent("anonymous"));
    await settle();
    expect(replace).not.toHaveBeenCalled();
    expect(toast.success).not.toHaveBeenCalled();
    expect(toast.error).not.toHaveBeenCalled();
  });

  it("does not revive a departed account page after its pending request clears credentials", async () => {
    const resolve = pendingReply();
    const view = await open();
    fireEvent.click(screen.getByRole("button", { name: "Update password" }));
    await settle();
    view.unmount();
    await act(async () => resolve(success()));
    await settle();
    expect(localStorage.getItem("tabmail_access_token")).toBeNull();
    expect(replace).not.toHaveBeenCalled();
    expect(toast.success).not.toHaveBeenCalled();
    expect(toast.error).not.toHaveBeenCalled();
  });

  it("preserves a newer identity installed between the old clear and continuation", async () => {
    afterCommit = () => installSession("newer-token", newer);
    await open();
    fireEvent.click(screen.getByRole("button", { name: "Update password" }));
    await waitFor(() => expect(screen.getByTestId("current-identity")).toHaveTextContent(newer.email));
    await settle();
    expect(localStorage.getItem("tabmail_access_token")).toBe("newer-token");
    expect(replace).not.toHaveBeenCalled();
    expect(toast.success).not.toHaveBeenCalled();
    expect(toast.error).not.toHaveBeenCalled();
  });

  it("does not claim a later anonymous session after an intervening login and logout", async () => {
    afterCommit = () => { installSession("newer-token", newer); clearSessionCredentials(); };
    await open();
    fireEvent.click(screen.getByRole("button", { name: "Update password" }));
    await waitFor(() => expect(screen.getByTestId("current-identity")).toHaveTextContent("anonymous"));
    await settle();
    expect(localStorage.getItem("tabmail_access_token")).toBeNull();
    expect(replace).not.toHaveBeenCalled();
    expect(toast.success).not.toHaveBeenCalled();
    expect(toast.error).not.toHaveBeenCalled();
  });

  it("keeps a newer session when the old password response arrives after its commit", async () => {
    const resolve = pendingReply();
    await open();
    fireEvent.click(screen.getByRole("button", { name: "Update password" }));
    await settle();
    await act(async () => installSession("newer-token", newer));
    await act(async () => resolve(success()));
    await waitFor(() => expect(screen.getByTestId("current-identity")).toHaveTextContent(newer.email));
    await settle();
    expect(localStorage.getItem("tabmail_access_token")).toBe("newer-token");
    expect(replace).not.toHaveBeenCalled();
    expect(toast.success).not.toHaveBeenCalled();
    expect(toast.error).not.toHaveBeenCalled();
  });
});
