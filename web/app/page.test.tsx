import React from "react";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import HomePage from "./page";
const m = vi.hoisted(() => ({
  push: vi.fn(),
  login: vi.fn(),
  install: vi.fn(),
  logout: vi.fn(),
  user: null as null | { id: string; email: string; display_name: string },
}));
vi.mock("next/navigation", () => ({ useRouter: () => ({ push: m.push }) }));
vi.mock("next/link", () => ({
  default: ({
    href,
    children,
  }: {
    href: string;
    children: React.ReactNode;
  }) => <a href={href}>{children}</a>,
}));
vi.mock("@/components/theme-toggle", () => ({ ThemeToggle: () => null }));
vi.mock("@/contexts/auth-context", () => ({
  useAuth: () => ({
    user: m.user,
    level: m.user ? "user" : "public",
    hydrated: true,
    loginWithTokens: m.install,
    logout: m.logout,
  }),
}));
vi.mock("@/lib/api/auth", () => ({ login: m.login }));
describe("company entry point", () => {
  beforeEach(() => {
    m.user = null;
    vi.clearAllMocks();
  });
  afterEach(cleanup);
  it("only offers employee sign-in, not disposable mailbox creation or public registration", () => {
    render(<HomePage />);
    expect(screen.getByLabelText("Login email")).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: /register|random|open inbox/i }),
    ).not.toBeInTheDocument();
    expect(screen.getByText(/single-use activation link/)).toBeInTheDocument();
  });
  it("signs in and opens the mail workspace", async () => {
    const user = {
      id: "u",
      email: "employee@company.test",
      display_name: "Employee",
      role: "user",
      tenant_id: "t",
    };
    m.login.mockResolvedValue({ data: { access_token: "token", user } });
    render(<HomePage />);
    fireEvent.change(screen.getByLabelText("Login email"), {
      target: { value: user.email },
    });
    fireEvent.change(screen.getByLabelText("Password"), {
      target: { value: "a-long-password" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Sign in" }));
    await waitFor(() => expect(m.push).toHaveBeenCalledWith("/mail"));
    expect(m.install).toHaveBeenCalledWith("token", user);
  });
  it("employees see their workspace, not administrative entry points", () => {
    m.user = {
      id: "u",
      email: "employee@company.test",
      display_name: "Employee",
    };
    render(<HomePage />);
    expect(screen.getByRole("link", { name: "Open mail" })).toHaveAttribute(
      "href",
      "/mail",
    );
    expect(
      screen.queryByRole("link", { name: "Company administration" }),
    ).not.toBeInTheDocument();
  });
});
