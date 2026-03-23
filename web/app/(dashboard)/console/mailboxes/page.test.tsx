import React from "react";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import MailboxesPage from "./page";

const {
  listMailboxesMock,
  createMailboxMock,
  deleteMailboxMock,
  toastSuccess,
  toastError,
} = vi.hoisted(() => ({
  listMailboxesMock: vi.fn(),
  createMailboxMock: vi.fn(),
  deleteMailboxMock: vi.fn(),
  toastSuccess: vi.fn(),
  toastError: vi.fn(),
}));

vi.mock("@/lib/api", () => ({
  listMailboxes: (...args: unknown[]) => listMailboxesMock(...args),
  createMailbox: (...args: unknown[]) => createMailboxMock(...args),
  deleteMailbox: (...args: unknown[]) => deleteMailboxMock(...args),
}));

vi.mock("sonner", () => ({
  toast: {
    success: toastSuccess,
    error: toastError,
  },
}));

vi.mock("next/link", () => ({
  default: ({ href, children }: { href: string; children: React.ReactNode }) => (
    <a href={href}>{children}</a>
  ),
}));

vi.mock("@/components/layout/page-header", () => ({
  PageHeader: ({
    title,
    description,
    actions,
  }: {
    title: string;
    description?: string;
    actions?: React.ReactNode;
  }) => (
    <div>
      <h1>{title}</h1>
      <div>{description}</div>
      <div>{actions}</div>
    </div>
  ),
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

vi.mock("@/components/ui/badge", () => ({
  Badge: ({ children }: { children: React.ReactNode }) => <span>{children}</span>,
}));

vi.mock("@/components/ui/card", () => ({
  Card: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  CardContent: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  CardDescription: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  CardHeader: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  CardTitle: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
}));

vi.mock("@/components/ui/table", () => ({
  Table: ({ children }: { children: React.ReactNode }) => <table>{children}</table>,
  TableBody: ({ children }: { children: React.ReactNode }) => <tbody>{children}</tbody>,
  TableCell: ({ children }: { children: React.ReactNode }) => <td>{children}</td>,
  TableHead: ({ children }: { children: React.ReactNode }) => <th>{children}</th>,
  TableHeader: ({ children }: { children: React.ReactNode }) => <thead>{children}</thead>,
  TableRow: ({ children }: { children: React.ReactNode }) => <tr>{children}</tr>,
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

vi.mock("@/components/ui/select", () => ({
  Select: ({
    value,
    onValueChange,
    children,
  }: {
    value: string;
    onValueChange: (value: string) => void;
    children: React.ReactNode;
  }) => (
    <div data-testid="select-root" data-value={value}>
      <button type="button" onClick={() => onValueChange("token")}>
        choose-token
      </button>
      <button type="button" onClick={() => onValueChange("api_key")}>
        choose-api-key
      </button>
      {children}
    </div>
  ),
  SelectContent: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  SelectItem: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  SelectTrigger: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  SelectValue: () => <span>select-value</span>,
}));

vi.mock("@/components/ui/dropdown-menu", () => ({
  DropdownMenu: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  DropdownMenuContent: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  DropdownMenuItem: ({
    children,
    onClick,
    render,
  }: {
    children: React.ReactNode;
    onClick?: () => void;
    render?: React.ReactElement;
  }) =>
    render
      ? React.cloneElement(render, undefined, children)
      : (
          <button type="button" onClick={onClick}>
            {children}
          </button>
        ),
  DropdownMenuTrigger: ({
    children,
    render,
  }: {
    children: React.ReactNode;
    render: React.ReactElement;
  }) => React.cloneElement(render, undefined, children),
}));

vi.mock("@/components/ui/skeleton", () => ({
  Skeleton: () => <div>loading</div>,
}));

describe("console/mailboxes page", () => {
  beforeEach(() => {
    listMailboxesMock.mockReset();
    createMailboxMock.mockReset();
    deleteMailboxMock.mockReset();
    toastSuccess.mockReset();
    toastError.mockReset();
  });

  afterEach(() => {
    cleanup();
  });

  it("加载列表并支持创建 token mailbox", async () => {
    listMailboxesMock
      .mockResolvedValueOnce({
        data: [
          {
            id: "mb-1",
            full_address: "first@mail.test",
            access_mode: "public",
            resolved_domain: "mail.test",
            created_at: new Date().toISOString(),
          },
        ],
        meta: { total: 1, page: 1, per_page: 30 },
      })
      .mockResolvedValueOnce({
        data: [
          {
            id: "mb-1",
            full_address: "first@mail.test",
            access_mode: "public",
            resolved_domain: "mail.test",
            created_at: new Date().toISOString(),
          },
          {
            id: "mb-2",
            full_address: "secure@mail.test",
            access_mode: "token",
            resolved_domain: "mail.test",
            created_at: new Date().toISOString(),
          },
        ],
        meta: { total: 2, page: 1, per_page: 30 },
      });
    createMailboxMock.mockResolvedValue({ data: {} });

    render(<MailboxesPage />);

    expect(await screen.findByText("first@mail.test")).toBeInTheDocument();

    fireEvent.change(screen.getByPlaceholderText("mail.example.com"), {
      target: { value: "secure@mail.test" },
    });
    fireEvent.click(screen.getByRole("button", { name: "choose-token" }));
    fireEvent.change(screen.getByPlaceholderText("Enter mailbox password"), {
      target: { value: "Passw0rd!" },
    });
    fireEvent.change(screen.getByPlaceholderText("Inherit tenant default"), {
      target: { value: "12" },
    });
    fireEvent.change(screen.getByPlaceholderText("Optional"), {
      target: { value: "2099-01-01T08:00" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Create" }));

    await waitFor(() => {
      expect(createMailboxMock).toHaveBeenCalledWith(expect.objectContaining({
        address: "secure@mail.test",
        access_mode: "token",
        password: "Passw0rd!",
        retention_hours_override: 12,
      }));
    });
    await waitFor(() => {
      const body = createMailboxMock.mock.calls[0][0];
      expect(typeof body.expires_at).toBe("string");
      expect(String(body.expires_at)).toContain("2099-01-01T");
    });
    await waitFor(() => {
      expect(listMailboxesMock).toHaveBeenCalledTimes(2);
    });
    expect(toastSuccess).toHaveBeenCalledWith("Mailbox created");
  });

  it("支持删除 mailbox", async () => {
    listMailboxesMock
      .mockResolvedValueOnce({
        data: [
          {
            id: "mb-1",
            full_address: "delete@mail.test",
            access_mode: "api_key",
            resolved_domain: "mail.test",
            created_at: new Date().toISOString(),
          },
        ],
        meta: { total: 1, page: 1, per_page: 30 },
      })
      .mockResolvedValueOnce({
        data: [],
        meta: { total: 0, page: 1, per_page: 30 },
      });
    deleteMailboxMock.mockResolvedValue(undefined);

    render(<MailboxesPage />);

    expect(await screen.findByText("delete@mail.test")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Delete" }));

    await waitFor(() => {
      expect(deleteMailboxMock).toHaveBeenCalledWith("mb-1");
    });
    await waitFor(() => {
      expect(listMailboxesMock).toHaveBeenCalledTimes(2);
    });
    expect(toastSuccess).toHaveBeenCalledWith("Mailbox deleted");
  });
});
