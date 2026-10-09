import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { toast } from "sonner";
import { AuthDialog } from "./auth-dialog";

vi.mock("@/contexts/auth-context", () => ({
  useAuth: () => ({ level: "public", user: null, loginWithTokens: vi.fn(), logout: vi.fn() }),
}));

type Call = { path: string; method: string; body: unknown };
let calls: Call[];
const passwordInput = () => screen.getByLabelText("Password") as HTMLInputElement;
const createButton = () => screen.getByRole("button", { name: "Create Account" });
async function openRegistration() {
  render(<AuthDialog />);
  fireEvent.click(screen.getByRole("button", { name: "Sign In" }));
  fireEvent.click(await screen.findByRole("button", { name: "Register now" }));
  fireEvent.change(screen.getByLabelText("Email address"), { target: { value: "candidate@example.test" } });
}
async function settle() { await act(async () => { for (let i = 0; i < 20; i++) await Promise.resolve(); }); }

beforeEach(() => {
  calls = [];
  vi.stubGlobal("navigator", { locks: { request: vi.fn(async (_name: string, run: () => Promise<unknown>) => run()) } });
  vi.spyOn(toast, "error").mockImplementation(() => "error-toast");
  vi.stubGlobal("fetch", async (input: RequestInfo | URL, init?: RequestInit) => {
    calls.push({ path: new URL(String(input), "http://localhost").pathname, method: init?.method ?? "GET", body: init?.body ? JSON.parse(String(init.body)) : undefined });
    // Keep the form open after observing the real credential request boundary.
    return new Response(JSON.stringify({ error: { code: "BAD_REQUEST", message: "synthetic registration rejection" } }),
      { status: 400, headers: { "Content-Type": "application/json" } });
  });
});
afterEach(() => { cleanup(); vi.unstubAllGlobals(); });

describe("registration uses the server's UTF-8 password policy", () => {
  it.each([
    ["ASCII lower boundary", "a".repeat(12)],
    ["ASCII upper boundary", "a".repeat(72)],
    ["four Chinese characters", "密码测试"],
    ["Chinese upper boundary", "密".repeat(24)],
    ["three astral characters", "🔐".repeat(3)],
    ["astral upper boundary", "🔐".repeat(18)],
    ["two-byte characters", "é".repeat(6)],
    ["combining characters", "e\u0301".repeat(4)],
    ["literal whitespace", " ".repeat(12)],
    ["surrounding whitespace", " 密码测试 "],
  ])("submits %s byte-for-byte through the real request layer", async (_name, password) => {
    await openRegistration();
    fireEvent.change(passwordInput(), { target: { value: password } });
    expect(createButton()).toBeEnabled();
    fireEvent.click(createButton());
    await waitFor(() => expect(calls).toHaveLength(1));
    expect(calls[0]).toEqual({ path: "/api/v1/auth/register", method: "POST", body: { email: "candidate@example.test", password } });
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith("synthetic registration rejection"));
    expect(passwordInput()).toHaveValue(password);
  });

  it.each([
    ["empty", ""],
    ["eight ASCII bytes", "a".repeat(8)],
    ["eleven ASCII bytes", "a".repeat(11)],
    ["seventy-three ASCII bytes", "a".repeat(73)],
    ["nine Chinese bytes", "密".repeat(3)],
    ["seventy-five Chinese bytes", "密".repeat(25)],
    ["seventy-six astral bytes", "🔐".repeat(19)],
    ["seventy-three mixed bytes", "密".repeat(24) + "a"],
  ])("disables submission for %s", async (_name, password) => {
    await openRegistration();
    fireEvent.change(passwordInput(), { target: { value: password } });
    expect(createButton()).toBeDisabled();
    expect(calls).toHaveLength(0);
  });

  it.each(["a".repeat(8), "a".repeat(73), "密".repeat(25)])("blocks invalid keyboard submission as well: %s", async password => {
    await openRegistration();
    fireEvent.change(passwordInput(), { target: { value: password } });
    fireEvent.keyDown(passwordInput(), { key: "Enter" });
    await settle();
    expect(calls).toHaveLength(0);
    expect(passwordInput()).toHaveValue(password);
  });

  it("announces the byte count and invalid state without character truncation", async () => {
    await openRegistration();
    fireEvent.change(passwordInput(), { target: { value: "密".repeat(25) } });
    expect(passwordInput()).toHaveAccessibleDescription("75 UTF-8 bytes; enter 12–72 bytes.");
    expect(passwordInput()).toHaveAttribute("aria-invalid", "true");
    expect(passwordInput()).not.toHaveAttribute("minlength");
    expect(passwordInput()).not.toHaveAttribute("maxlength");
    fireEvent.change(passwordInput(), { target: { value: "密码测试" } });
    expect(passwordInput()).toHaveAccessibleDescription("12 UTF-8 bytes; enter 12–72 bytes.");
    expect(passwordInput()).not.toHaveAttribute("aria-invalid", "true");
  });

  it("uses the same byte limits and count in Chinese", async () => {
    localStorage.setItem("tabmail-locale", "zh");
    render(<AuthDialog />);
    fireEvent.click(screen.getByRole("button", { name: "登录" }));
    fireEvent.click(await screen.findByRole("button", { name: "立即注册" }));
    fireEvent.change(screen.getByLabelText("密码"), { target: { value: "密码测试" } });
    expect(screen.getByLabelText("密码")).toHaveAccessibleDescription("当前 12 个 UTF-8 字节，需为 12–72 字节。");
    expect(screen.getByText("12–72 个 UTF-8 字节")).toBeVisible();
  });

  it.each([" ".repeat(12), " 密码测试 ", "legacy-8"])("can sign in with the unchanged accepted or legacy password: %s", async password => {
    render(<AuthDialog />);
    fireEvent.click(screen.getByRole("button", { name: "Sign In" }));
    fireEvent.change(await screen.findByLabelText("Email address"), { target: { value: "candidate@example.test" } });
    fireEvent.change(passwordInput(), { target: { value: password } });
    const loginButton = screen.getAllByRole("button", { name: "Sign In" }).at(-1)!;
    expect(loginButton).toBeEnabled();
    fireEvent.click(loginButton);
    await waitFor(() => expect(calls).toHaveLength(1));
    expect(calls[0]).toEqual({ path: "/api/v1/auth/login", method: "POST", body: { email: "candidate@example.test", password } });
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith("synthetic registration rejection"));
  });
});
