import React from "react";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import DomainsPage from "./page";

const {
  listDomainsMock,
  createDomainMock,
  deleteDomainMock,
  verifyDomainMock,
  getVerificationStatusMock,
  toastSuccess,
  toastError,
} = vi.hoisted(() => ({
  listDomainsMock: vi.fn(),
  createDomainMock: vi.fn(),
  deleteDomainMock: vi.fn(),
  verifyDomainMock: vi.fn(),
  getVerificationStatusMock: vi.fn(),
  toastSuccess: vi.fn(),
  toastError: vi.fn(),
}));

vi.mock("@/lib/api", () => ({
  listDomains: (...args: unknown[]) => listDomainsMock(...args),
  createDomain: (...args: unknown[]) => createDomainMock(...args),
  deleteDomain: (...args: unknown[]) => deleteDomainMock(...args),
  verifyDomain: (...args: unknown[]) => verifyDomainMock(...args),
  getVerificationStatus: (...args: unknown[]) => getVerificationStatusMock(...args),
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

describe("console/domains page", () => {
  beforeEach(() => {
    listDomainsMock.mockReset();
    createDomainMock.mockReset();
    deleteDomainMock.mockReset();
    verifyDomainMock.mockReset();
    getVerificationStatusMock.mockReset();
    toastSuccess.mockReset();
    toastError.mockReset();
  });

  afterEach(() => {
    cleanup();
  });

  it("加载列表并支持创建 domain", async () => {
    listDomainsMock
      .mockResolvedValueOnce({
        data: [
          {
            id: "zone-1",
            domain: "mail.test",
            is_verified: false,
            mx_verified: false,
            txt_record: "tabmail-verify=abc",
            created_at: new Date().toISOString(),
          },
        ],
      })
      .mockResolvedValueOnce({
        data: [
          {
            id: "zone-1",
            domain: "mail.test",
            is_verified: false,
            mx_verified: false,
            txt_record: "tabmail-verify=abc",
            created_at: new Date().toISOString(),
          },
          {
            id: "zone-2",
            domain: "mx.example.com",
            is_verified: false,
            mx_verified: false,
            txt_record: "tabmail-verify=def",
            created_at: new Date().toISOString(),
          },
        ],
      });
    createDomainMock.mockResolvedValue({ data: {} });

    render(<DomainsPage />);

    expect(await screen.findByText("mail.test")).toBeInTheDocument();

    fireEvent.change(screen.getByPlaceholderText("mail.example.com"), {
      target: { value: "mx.example.com" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Create" }));

    await waitFor(() => {
      expect(createDomainMock).toHaveBeenCalledWith("mx.example.com");
    });
    await waitFor(() => {
      expect(listDomainsMock).toHaveBeenCalledTimes(2);
    });
    expect(toastSuccess).toHaveBeenCalledWith("Domain created");
  });

  it("支持 verify 与 delete", async () => {
    const zone = {
      id: "zone-1",
      domain: "mail.test",
      is_verified: false,
      mx_verified: false,
      txt_record: "tabmail-verify=abc",
      created_at: new Date().toISOString(),
    };
    listDomainsMock
      .mockResolvedValueOnce({
        data: [zone],
      })
      .mockResolvedValueOnce({
        data: [zone],
      })
      .mockResolvedValue({
        data: [],
      });

    verifyDomainMock.mockResolvedValue({ data: {} });
    getVerificationStatusMock.mockResolvedValue({
      data: {
        checks: {
          txt: { status: "pass" },
          mx: { status: "fail" },
        },
      },
    });
    deleteDomainMock.mockResolvedValue(undefined);

    render(<DomainsPage />);

    expect(await screen.findByText("mail.test")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Verify DNS" }));
    await waitFor(() => {
      expect(verifyDomainMock).toHaveBeenCalledWith("zone-1");
    });
    await waitFor(() => {
      expect(getVerificationStatusMock).toHaveBeenCalledWith("zone-1");
    });
    expect(toastSuccess).toHaveBeenCalledWith("TXT PASS · MX FAIL");

    fireEvent.click(screen.getByRole("button", { name: "Delete" }));
    await waitFor(() => {
      expect(deleteDomainMock).toHaveBeenCalledWith("zone-1");
    });
    expect(toastSuccess).toHaveBeenCalledWith("Domain deleted");
  });
});
