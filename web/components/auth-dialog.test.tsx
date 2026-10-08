import React from "react";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { AuthDialog } from "./auth-dialog";

const { toastSuccess, toastError, fetchMock, authStateRef } = vi.hoisted(() => ({
  toastSuccess: vi.fn(),
  toastError: vi.fn(),
  fetchMock: vi.fn(),
  authStateRef: {
    current: null as {
      level: "public" | "admin" | "user";
      user: { email: string; display_name: string; role: "admin" | "user" } | null;
      loginWithTokens: ReturnType<typeof vi.fn>;
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
      loginWithTokens: vi.fn(),
      logout: vi.fn(),
    };
    fetchMock.mockReset();
    vi.stubGlobal("fetch", fetchMock);
    vi.stubGlobal("navigator", { locks: { request: vi.fn(async (_name: string, run: () => Promise<unknown>) => run()) } });
    toastSuccess.mockReset();
    toastError.mockReset();
  });
  afterEach(() => { vi.unstubAllGlobals(); });

  it("支持账号登录并写入 JWT", async () => {
    fetchMock.mockResolvedValue(new Response(JSON.stringify({
      data: {
        access_token: "access-token",
        user: {
          id: "user-1",
          email: "user@mail.test",
          display_name: "User",
          role: "user",
          tenant_id: "tenant-1",
        },
      },
    }), { status: 200, headers: { "Content-Type": "application/json" } }));

    render(<AuthDialog />);

    fireEvent.change(screen.getAllByLabelText("auth.email")[0], {
      target: { value: "user@mail.test" },
    });
    fireEvent.change(screen.getByLabelText("auth.password"), {
      target: { value: "Passw0rd!" },
    });

    fireEvent.click(screen.getAllByRole("button", { name: /auth.loginBtn/ }).at(-1) as HTMLButtonElement);

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(expect.stringContaining("/api/v1/auth/login"), expect.objectContaining({
        method: "POST", credentials: "include", body: JSON.stringify({ email: "user@mail.test", password: "Passw0rd!" }),
      }));
    });
    await waitFor(() => expect(toastSuccess).toHaveBeenCalledWith("Welcome, User"));
    // Cookie-changing requests install identity while owning the shared lock;
    // a component must never install those credentials a second time later.
    expect(localStorage.getItem("tabmail_access_token")).toBe("access-token");
    expect(JSON.parse(localStorage.getItem("tabmail_user")!)).toEqual(expect.objectContaining({ email: "user@mail.test" }));
    expect(authStateRef.current?.loginWithTokens).not.toHaveBeenCalled();
  });

});
