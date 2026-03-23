import React from "react";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { AuthDialog } from "./auth-dialog";

const { toastSuccess, toastError, issueTokenMock, authStateRef } = vi.hoisted(() => ({
  toastSuccess: vi.fn(),
  toastError: vi.fn(),
  issueTokenMock: vi.fn(),
  authStateRef: {
    current: null as {
      level: "public" | "admin" | "tenant" | "mailbox";
      adminKey: string | null;
      apiKey: string | null;
      tenantId: string | null;
      mailboxAddress: string | null;
      setAdminKey: ReturnType<typeof vi.fn>;
      setApiKey: ReturnType<typeof vi.fn>;
      setTenantId: ReturnType<typeof vi.fn>;
      setMailboxAuth: ReturnType<typeof vi.fn>;
      clearMailboxAuth: ReturnType<typeof vi.fn>;
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
      adminKey: null,
      apiKey: null,
      tenantId: null,
      mailboxAddress: null,
      setAdminKey: vi.fn(),
      setApiKey: vi.fn(),
      setTenantId: vi.fn(),
      setMailboxAuth: vi.fn(),
      clearMailboxAuth: vi.fn(),
      logout: vi.fn(),
    };
    issueTokenMock.mockReset();
    toastSuccess.mockReset();
    toastError.mockReset();
  });

  it("支持 admin 登录并清理 mailbox 鉴权", () => {
    render(<AuthDialog />);

    fireEvent.change(screen.getByLabelText("auth.adminKey"), {
      target: { value: "admin-secret" },
    });
    fireEvent.change(screen.getByLabelText("auth.tenantId"), {
      target: { value: "tenant-1" },
    });

    const connectButtons = screen.getAllByRole("button", { name: "auth.connect" });
    fireEvent.click(connectButtons[3]);

    expect(authStateRef.current?.clearMailboxAuth).toHaveBeenCalledTimes(1);
    expect(authStateRef.current?.setAdminKey).toHaveBeenCalledWith("admin-secret");
    expect(authStateRef.current?.setApiKey).toHaveBeenCalledWith(null);
    expect(authStateRef.current?.setTenantId).toHaveBeenCalledWith("tenant-1");
    expect(toastSuccess).toHaveBeenCalledWith("toast.adminOk");
  });

  it("支持 mailbox 登录并写入 mailbox token", async () => {
    issueTokenMock.mockResolvedValue({
      data: { token: "mailbox-token" },
    });

    render(<AuthDialog />);

    fireEvent.change(screen.getByLabelText("auth.mailboxAddr"), {
      target: { value: "user@mail.test" },
    });
    fireEvent.change(screen.getByLabelText("auth.mailboxPwd"), {
      target: { value: "Passw0rd!" },
    });
    fireEvent.keyDown(screen.getByLabelText("auth.mailboxPwd"), { key: "Enter" });

    await waitFor(() => {
      expect(issueTokenMock).toHaveBeenCalledWith("user@mail.test", "Passw0rd!");
    });
    expect(authStateRef.current?.setAdminKey).toHaveBeenCalledWith(null);
    expect(authStateRef.current?.setApiKey).toHaveBeenCalledWith(null);
    expect(authStateRef.current?.setTenantId).toHaveBeenCalledWith(null);
    expect(authStateRef.current?.setMailboxAuth).toHaveBeenCalledWith("user@mail.test", "mailbox-token");
    expect(toastSuccess).toHaveBeenCalledWith("toast.tokenIssued");
  });
});
