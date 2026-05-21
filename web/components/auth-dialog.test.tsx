import React from "react";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { AuthDialog } from "./auth-dialog";

const { toastSuccess, toastError, issueTokenMock, loginMock, registerMock, logoutSessionMock, authStateRef } = vi.hoisted(() => ({
  toastSuccess: vi.fn(),
  toastError: vi.fn(),
  issueTokenMock: vi.fn(),
  loginMock: vi.fn(),
  registerMock: vi.fn(),
  logoutSessionMock: vi.fn(),
  authStateRef: {
    current: null as {
      level: "public" | "admin" | "mailbox" | "user";
      user: { email: string; display_name: string; role: "admin" | "user" } | null;
      refreshToken: string | null;
      mailboxAddress: string | null;
      loginWithTokens: ReturnType<typeof vi.fn>;
      setMailboxAuth: ReturnType<typeof vi.fn>;
      logout: ReturnType<typeof vi.fn>;
    } | null,
  },
}));

vi.mock("@/contexts/auth-context", () => ({
  useAuth: () => authStateRef.current,
}));

vi.mock("@/lib/i18n", () => ({
  useI18n: () => ({
    t: (key: string) => key,
  }),
}));

vi.mock("@/lib/api", () => ({
  issueToken: (...args: unknown[]) => issueTokenMock(...args),
  login: (...args: unknown[]) => loginMock(...args),
  register: (...args: unknown[]) => registerMock(...args),
  logoutSession: (...args: unknown[]) => logoutSessionMock(...args),
}));

vi.mock("sonner", () => ({
  toast: {
    success: toastSuccess,
    error: toastError,
  },
}));

vi.mock("@/components/ui/button", () => ({
  Button: ({ children, ...props }: React.ButtonHTMLAttributes<HTMLButtonElement>) => (
    <button {...props}>{children}</button>
  ),
}));

vi.mock("@/components/ui/input", () => ({
  Input: (props: React.InputHTMLAttributes<HTMLInputElement>) => <input {...props} />,
}));

vi.mock("@/components/ui/label", () => ({
  Label: ({ children, ...props }: React.LabelHTMLAttributes<HTMLLabelElement>) => (
    <label {...props}>{children}</label>
  ),
}));

vi.mock("@/components/ui/dialog", () => ({
  Dialog: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  DialogContent: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  DialogDescription: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  DialogFooter: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  DialogHeader: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  DialogTitle: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  DialogTrigger: ({
    children,
    render,
  }: {
    children: React.ReactNode;
    render: React.ReactElement;
  }) => React.cloneElement(render, undefined, children),
}));

vi.mock("@/components/ui/tabs", () => ({
  Tabs: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  TabsContent: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  TabsList: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  TabsTrigger: ({ children }: { children: React.ReactNode }) => <button type="button">{children}</button>,
}));

describe("AuthDialog", () => {
  beforeEach(() => {
    authStateRef.current = {
      level: "public",
      user: null,
      refreshToken: null,
      mailboxAddress: null,
      loginWithTokens: vi.fn(),
      setMailboxAuth: vi.fn(),
      logout: vi.fn(),
    };
    issueTokenMock.mockReset();
    loginMock.mockReset();
    registerMock.mockReset();
    logoutSessionMock.mockReset();
    toastSuccess.mockReset();
    toastError.mockReset();
  });

  it("支持账号登录并写入 JWT", async () => {
    loginMock.mockResolvedValue({
      data: {
        access_token: "access-token",
        refresh_token: "refresh-token",
        user: {
          id: "user-1",
          email: "user@mail.test",
          display_name: "User",
          role: "user",
          tenant_id: "tenant-1",
        },
      },
    });

    render(<AuthDialog />);

    fireEvent.change(screen.getAllByLabelText("auth.email")[0], {
      target: { value: "user@mail.test" },
    });
    fireEvent.change(screen.getByLabelText("auth.password"), {
      target: { value: "Passw0rd!" },
    });

    fireEvent.click(screen.getAllByRole("button", { name: /auth.loginBtn/ }).at(-1) as HTMLButtonElement);

    await waitFor(() => {
      expect(loginMock).toHaveBeenCalledWith("user@mail.test", "Passw0rd!");
    });
    expect(authStateRef.current?.loginWithTokens).toHaveBeenCalledWith(
      "access-token",
      "refresh-token",
      expect.objectContaining({ email: "user@mail.test" })
    );
    expect(toastSuccess).toHaveBeenCalledWith("Welcome, User");
  });

  it("mailbox 登录态显示单邮箱身份", () => {
    authStateRef.current = {
      level: "mailbox",
      user: null,
      refreshToken: null,
      mailboxAddress: "user@mail.test",
      loginWithTokens: vi.fn(),
      setMailboxAuth: vi.fn(),
      logout: vi.fn(),
    };

    render(<AuthDialog />);

    expect(screen.getByText("auth.level.mailbox")).toBeInTheDocument();
    expect(screen.getByText("user@mail.test")).toBeInTheDocument();
    expect(issueTokenMock).not.toHaveBeenCalled();
  });
});
