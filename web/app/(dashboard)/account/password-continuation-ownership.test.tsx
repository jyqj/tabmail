import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { toast } from "sonner";
import { installSession } from "@/lib/session";
import AccountPage from "./page";

const { replace } = vi.hoisted(() => ({ replace: vi.fn() }));
vi.mock("next/navigation", () => ({ useRouter: () => ({ replace }) }));
const original = { id: "password-user", tenant_id: "password-tenant", role: "user" as const, email: "password@example.test", display_name: "Password user" };
const newer = { ...original, id: "newer-user", tenant_id: "newer-tenant", email: "newer@example.test" };
const response = (body: unknown, status = 200) => new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
const success = () => response({ data: { status: "password changed; sign in again" } });
const failure = () => response({ error: { code: "INVALID_PASSWORD", message: "synthetic current-password rejection" } }, 403);
let reply: () => Promise<Response>;
let afterSuccess: (() => void) | undefined;
let afterFailure: (() => void) | undefined;
let calls: { path: string; body: unknown }[];
const passwordInput = () => screen.getByLabelText("New password (12–72 bytes)") as HTMLInputElement;
function fill(password = "synthetic-new-password") {
  fireEvent.change(screen.getByLabelText("Current password"), { target: { value: "synthetic-current-password" } });
  fireEvent.change(passwordInput(), { target: { value: password } });
  fireEvent.change(screen.getByLabelText("Confirm new password"), { target: { value: password } });
}
function pendingReply() {
  let resolve!: (value: Response) => void;
  const result = new Promise<Response>(resolveReply => { resolve = resolveReply; });
  reply = () => result;
  return resolve;
}
async function settle() { await act(async () => { for (let i = 0; i < 30; i++) await Promise.resolve(); }); }

beforeEach(() => {
  calls = [];
  reply = async () => success();
  afterSuccess = undefined;
  afterFailure = undefined;
  installSession("original-token", original);
  vi.stubGlobal("navigator", { locks: { request: vi.fn(async (_name: string, work: () => Promise<unknown>) => {
    // Keep the real credential request/session path, controlling only the gap
    // between lock completion and the component's awaited continuation.
    try { const value = await work(); afterSuccess?.(); return value; }
    catch (error) { afterFailure?.(); throw error; }
  }) } });
  vi.spyOn(toast, "success").mockImplementation(() => "success-toast");
  vi.spyOn(toast, "error").mockImplementation(() => "error-toast");
  vi.stubGlobal("fetch", async (input: RequestInfo | URL, init?: RequestInit) => {
    calls.push({ path: new URL(String(input), "http://localhost").pathname, body: init?.body ? JSON.parse(String(init.body)) : undefined });
    return reply();
  });
});
afterEach(() => { cleanup(); vi.unstubAllGlobals(); });

describe("password-change continuation belongs to its originating view and credential commit", () => {
  it("clears credentials once inside the request layer and completes the current view", async () => {
    render(<AccountPage />);
    fill();
    const changed = vi.fn();
    window.addEventListener("tabmail-auth-change", changed);
    try {
      fireEvent.click(screen.getByRole("button", { name: "Update password" }));
      await waitFor(() => expect(replace).toHaveBeenCalledWith("/"));
      expect(localStorage.getItem("tabmail_access_token")).toBeNull();
      expect(changed).toHaveBeenCalledTimes(1);
      expect(toast.success).toHaveBeenCalledWith("Password changed. Please sign in again.");
      expect(calls).toEqual([{ path: "/api/v1/auth/change-password", body: { old_password: "synthetic-current-password", new_password: "synthetic-new-password" } }]);
    } finally { window.removeEventListener("tabmail-auth-change", changed); }
  });

  it("preserves a new login that commits before the previous password continuation", async () => {
    afterSuccess = () => installSession("newer-token", newer);
    render(<AccountPage />);
    fill();
    fireEvent.click(screen.getByRole("button", { name: "Update password" }));
    await settle();
    expect(localStorage.getItem("tabmail_access_token")).toBe("newer-token");
    expect(JSON.parse(localStorage.getItem("tabmail_user")!)).toEqual(newer);
    expect(localStorage.getItem("tabmail_tenant_id")).toBe(newer.tenant_id);
    expect(replace).not.toHaveBeenCalled();
    expect(toast.success).not.toHaveBeenCalled();
    expect(calls).toHaveLength(1);
  });

  it.each(["success", "failure"] as const)("does not navigate or report a departed form's late %s", async result => {
    const resolve = pendingReply();
    const view = render(<AccountPage />);
    fill();
    fireEvent.click(screen.getByRole("button", { name: "Update password" }));
    await settle();
    view.unmount();
    await act(async () => resolve(result === "success" ? success() : failure()));
    await settle();
    expect(replace).not.toHaveBeenCalled();
    expect(toast.success).not.toHaveBeenCalled();
    expect(toast.error).not.toHaveBeenCalled();
    expect(calls).toHaveLength(1);
  });

  it.each(["success", "failure"] as const)("preserves later form edits after a late %s", async result => {
    const resolve = pendingReply();
    render(<AccountPage />);
    fill();
    fireEvent.click(screen.getByRole("button", { name: "Update password" }));
    await settle();
    fill("later-edited-password");
    await act(async () => resolve(result === "success" ? success() : failure()));
    await settle();
    expect(passwordInput()).toHaveValue("later-edited-password");
    expect(replace).not.toHaveBeenCalled();
    expect(toast.success).not.toHaveBeenCalled();
    expect(toast.error).not.toHaveBeenCalled();
  });

  it("does not report an old rejection after another identity owns the session", async () => {
    reply = async () => failure();
    afterFailure = () => installSession("newer-token", newer);
    render(<AccountPage />);
    fill();
    fireEvent.click(screen.getByRole("button", { name: "Update password" }));
    await settle();
    expect(localStorage.getItem("tabmail_access_token")).toBe("newer-token");
    expect(toast.error).not.toHaveBeenCalled();
    expect(replace).not.toHaveBeenCalled();
  });

  it("keeps current rejection feedback, inputs, credentials and an explicit retry", async () => {
    reply = async () => failure();
    render(<AccountPage />);
    fill();
    fireEvent.click(screen.getByRole("button", { name: "Update password" }));
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith("synthetic current-password rejection"));
    expect(passwordInput()).toHaveValue("synthetic-new-password");
    expect(localStorage.getItem("tabmail_access_token")).toBe("original-token");
    expect(screen.getByRole("button", { name: "Update password" })).toBeEnabled();
    expect(replace).not.toHaveBeenCalled();
  });

  it("preserves the request layer's stale-session rejection while the response is pending", async () => {
    const resolve = pendingReply();
    render(<AccountPage />);
    fill();
    fireEvent.click(screen.getByRole("button", { name: "Update password" }));
    await settle();
    installSession("newer-token", newer);
    await act(async () => resolve(success()));
    await settle();
    expect(localStorage.getItem("tabmail_access_token")).toBe("newer-token");
    expect(toast.error).not.toHaveBeenCalled();
    expect(toast.success).not.toHaveBeenCalled();
    expect(replace).not.toHaveBeenCalled();
  });
});
