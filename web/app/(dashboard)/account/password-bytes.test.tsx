import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { toast } from "sonner";
import { installSession } from "@/lib/session";
import AccountPage from "./page";

const { replace } = vi.hoisted(() => ({ replace: vi.fn() }));
vi.mock("next/navigation", () => ({ useRouter: () => ({ replace }) }));

type Call = { path: string; method: string; body: unknown };
let calls: Call[];
let reply: () => Promise<Response>;
const response = (data: unknown, status = 200) => new Response(JSON.stringify(data), { status, headers: { "Content-Type": "application/json" } });
const oldPassword = "synthetic-current-password";
const nextInput = () => screen.getByLabelText("New password (12–72 bytes)") as HTMLInputElement;
const submit = () => screen.getByRole("button", { name: "Update password" });
function fill(next: string, confirmation = next, old = oldPassword) {
  fireEvent.change(screen.getByLabelText("Current password"), { target: { value: old } });
  fireEvent.change(nextInput(), { target: { value: next } });
  fireEvent.change(screen.getByLabelText("Confirm new password"), { target: { value: confirmation } });
}
async function settle() { await act(async () => { for (let i = 0; i < 20; i++) await Promise.resolve(); }); }

beforeEach(() => {
  calls = [];
  reply = async () => response({ data: { status: "password changed; sign in again" } });
  installSession("synthetic-password-session", { id: "password-user", tenant_id: "password-tenant", role: "user", email: "password@example.test", display_name: "Password user" });
  vi.stubGlobal("navigator", { locks: { request: vi.fn(async (_name: string, run: () => Promise<unknown>) => run()) } });
  vi.spyOn(toast, "success").mockImplementation(() => "success-toast");
  vi.spyOn(toast, "error").mockImplementation(() => "error-toast");
  vi.stubGlobal("fetch", async (input: RequestInfo | URL, init?: RequestInit) => {
    calls.push({ path: new URL(String(input), "http://localhost").pathname, method: init?.method ?? "GET", body: init?.body ? JSON.parse(String(init.body)) : undefined });
    return reply();
  });
});
afterEach(() => { cleanup(); vi.unstubAllGlobals(); });

describe("account password UTF-8 byte policy", () => {
  it.each([
    ["ASCII lower bound", "a".repeat(12)],
    ["ASCII upper bound", "a".repeat(72)],
    ["four Chinese characters", "密码测试"],
    ["Chinese upper bound", "密".repeat(24)],
    ["three astral characters", "🔐".repeat(3)],
    ["astral upper bound", "🔐".repeat(18)],
    ["two-byte characters", "é".repeat(6)],
    ["combining characters", "e\u0301".repeat(4)],
    ["literal whitespace", " ".repeat(12)],
    ["leading and trailing whitespace", " 密码测试 "],
  ])("submits %s unchanged when its UTF-8 length is valid", async (_name, password) => {
    render(<AccountPage />);
    fill(password);
    expect(submit()).toBeEnabled();
    fireEvent.click(submit());
    await waitFor(() => expect(replace).toHaveBeenCalledWith("/"));
    expect(calls).toEqual([{ path: "/api/v1/auth/change-password", method: "POST", body: { old_password: oldPassword, new_password: password } }]);
    expect(localStorage.getItem("tabmail_access_token")).toBeNull();
    expect(toast.success).toHaveBeenCalledWith("Password changed. Please sign in again.");
  });

  it.each([
    ["empty password", ""],
    ["eleven ASCII bytes", "a".repeat(11)],
    ["seventy-three ASCII bytes", "a".repeat(73)],
    ["nine Chinese bytes", "密".repeat(3)],
    ["seventy-five Chinese bytes", "密".repeat(25)],
    ["seventy-six astral bytes", "🔐".repeat(19)],
    ["seventy-three mixed bytes", "密".repeat(24) + "a"],
  ])("blocks %s in the button and submit handler", async (_name, password) => {
    render(<AccountPage />);
    fill(password);
    expect(submit()).toBeDisabled();
    fireEvent.submit(nextInput().closest("form")!);
    await settle();
    expect(calls).toHaveLength(0);
    expect(replace).not.toHaveBeenCalled();
    expect(localStorage.getItem("tabmail_access_token")).toBe("synthetic-password-session");
  });

  it("does not send an empty current password through a direct form submission", async () => {
    render(<AccountPage />);
    fill("a".repeat(12), "a".repeat(12), "");
    expect(submit()).toBeDisabled();
    fireEvent.submit(nextInput().closest("form")!);
    await settle();
    expect(calls).toHaveLength(0);
    expect(replace).not.toHaveBeenCalled();
  });

  it("keeps mismatched confirmation blocked without normalizing either value", async () => {
    render(<AccountPage />);
    fill("é".repeat(12), "e\u0301".repeat(12));
    expect(submit()).toBeDisabled();
    fireEvent.submit(nextInput().closest("form")!);
    await settle();
    expect(calls).toHaveLength(0);
    expect(toast.error).toHaveBeenCalledWith("Passwords do not match");
    expect(replace).not.toHaveBeenCalled();
  });

  it("retains input and the session when the server rejects the current password", async () => {
    reply = async () => response({ error: { code: "INVALID_PASSWORD", message: "incorrect old password" } }, 403);
    render(<AccountPage />);
    fill("a".repeat(12));
    fireEvent.click(submit());
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith("incorrect old password"));
    expect(nextInput()).toHaveValue("a".repeat(12));
    expect(screen.getByLabelText("Current password")).toHaveValue(oldPassword);
    expect(submit()).toBeEnabled();
    expect(localStorage.getItem("tabmail_access_token")).toBe("synthetic-password-session");
    expect(replace).not.toHaveBeenCalled();
  });

  it("does not silently truncate typed values at a UTF-16 character limit", () => {
    render(<AccountPage />);
    expect(nextInput()).not.toHaveAttribute("minlength");
    expect(screen.getByLabelText("Confirm new password")).not.toHaveAttribute("minlength");
    expect(nextInput()).not.toHaveAttribute("maxlength");
    expect(screen.getByLabelText("Confirm new password")).not.toHaveAttribute("maxlength");
  });
});
