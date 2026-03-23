import React from "react";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import TenantsPage from "./page";

const {
  listTenantsMock,
  createTenantMock,
  deleteTenantMock,
  listPlansMock,
  updateTenantOverridesMock,
  getTenantConfigMock,
  createAPIKeyMock,
  listAPIKeysMock,
  revokeAPIKeyMock,
  toastSuccess,
  toastError,
} = vi.hoisted(() => ({
  listTenantsMock: vi.fn(),
  createTenantMock: vi.fn(),
  deleteTenantMock: vi.fn(),
  listPlansMock: vi.fn(),
  updateTenantOverridesMock: vi.fn(),
  getTenantConfigMock: vi.fn(),
  createAPIKeyMock: vi.fn(),
  listAPIKeysMock: vi.fn(),
  revokeAPIKeyMock: vi.fn(),
  toastSuccess: vi.fn(),
  toastError: vi.fn(),
}));

vi.mock("@/lib/api", () => ({
  listTenants: (...args: unknown[]) => listTenantsMock(...args),
  createTenant: (...args: unknown[]) => createTenantMock(...args),
  deleteTenant: (...args: unknown[]) => deleteTenantMock(...args),
  listPlans: (...args: unknown[]) => listPlansMock(...args),
  updateTenantOverrides: (...args: unknown[]) => updateTenantOverridesMock(...args),
  getTenantConfig: (...args: unknown[]) => getTenantConfigMock(...args),
  createAPIKey: (...args: unknown[]) => createAPIKeyMock(...args),
  listAPIKeys: (...args: unknown[]) => listAPIKeysMock(...args),
  revokeAPIKey: (...args: unknown[]) => revokeAPIKeyMock(...args),
}));

vi.mock("sonner", () => ({
  toast: {
    success: toastSuccess,
    error: toastError,
  },
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
  }: {
    children: React.ReactNode;
    onClick?: () => void;
  }) => (
    <button type="button" onClick={onClick}>
      {children}
    </button>
  ),
  DropdownMenuSeparator: () => <hr />,
  DropdownMenuTrigger: ({
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
    <div data-testid="plan-select" data-value={value}>
      <button type="button" onClick={() => onValueChange("plan-1")}>
        choose-plan-1
      </button>
      {children}
    </div>
  ),
  SelectContent: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  SelectItem: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  SelectTrigger: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  SelectValue: ({ placeholder }: { placeholder?: string }) => <span>{placeholder ?? "select"}</span>,
}));

vi.mock("@/components/ui/skeleton", () => ({
  Skeleton: () => <div>loading</div>,
}));

describe("admin/tenants page", () => {
  const plans = [
    {
      id: "plan-1",
      name: "starter",
      max_domains: 5,
      max_mailboxes_per_domain: 100,
      max_messages_per_mailbox: 200,
      max_message_bytes: 10485760,
      retention_hours: 48,
      rpm_limit: 60,
      daily_quota: 1000,
      created_at: new Date().toISOString(),
      updated_at: new Date().toISOString(),
    },
  ];

  const tenants = [
    {
      id: "tenant-1",
      name: "Acme",
      plan_id: "plan-1",
      is_super: false,
      created_at: new Date().toISOString(),
    },
  ];

  beforeEach(() => {
    listTenantsMock.mockReset();
    createTenantMock.mockReset();
    deleteTenantMock.mockReset();
    listPlansMock.mockReset();
    updateTenantOverridesMock.mockReset();
    getTenantConfigMock.mockReset();
    createAPIKeyMock.mockReset();
    listAPIKeysMock.mockReset();
    revokeAPIKeyMock.mockReset();
    toastSuccess.mockReset();
    toastError.mockReset();
  });

  afterEach(() => {
    cleanup();
  });

  it("加载列表并支持创建 tenant", async () => {
    listTenantsMock.mockResolvedValueOnce({ data: tenants }).mockResolvedValueOnce({
      data: [...tenants, { ...tenants[0], id: "tenant-2", name: "Beta" }],
    });
    listPlansMock.mockResolvedValue({ data: plans });
    createTenantMock.mockResolvedValue({ data: {} });

    render(<TenantsPage />);

    expect(await screen.findByText("Acme")).toBeInTheDocument();

    fireEvent.change(screen.getByPlaceholderText("Example: Acme Corp"), {
      target: { value: "Beta" },
    });
    fireEvent.click(screen.getByRole("button", { name: "choose-plan-1" }));
    fireEvent.click(screen.getByRole("button", { name: "Create" }));

    await waitFor(() => {
      expect(createTenantMock).toHaveBeenCalledWith({
        name: "Beta",
        plan_id: "plan-1",
      });
    });
    await waitFor(() => {
      expect(listTenantsMock).toHaveBeenCalledTimes(2);
    });
    expect(toastSuccess).toHaveBeenCalledWith("Tenant created");
  });

  it("支持生成 API key", async () => {
    listTenantsMock.mockResolvedValue({ data: tenants });
    listPlansMock.mockResolvedValue({ data: plans });
    listAPIKeysMock
      .mockResolvedValueOnce({ data: [] })
      .mockResolvedValueOnce({
        data: [
          {
            id: "key-1",
            tenant_id: "tenant-1",
            key_prefix: "tb_1234567890",
            label: "",
            scopes: ["*"],
            expires_at: null,
            created_at: new Date().toISOString(),
          },
        ],
      });
    createAPIKeyMock.mockResolvedValue({
      data: {
        id: "key-1",
        key: "tb_secret_key",
        key_prefix: "tb_1234567890",
        label: "",
        scopes: ["*"],
        tenant_id: "tenant-1",
        expires_at: null,
        created_at: new Date().toISOString(),
      },
    });

    render(<TenantsPage />);

    expect(await screen.findByText("Acme")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "API Keys" }));

    await waitFor(() => {
      expect(listAPIKeysMock).toHaveBeenCalledWith("tenant-1");
    });

    fireEvent.click(screen.getByRole("button", { name: /Generate Key/ }));

    await waitFor(() => {
      expect(createAPIKeyMock).toHaveBeenCalledWith("tenant-1", { scopes: ["*"] });
    });
    await waitFor(() => {
      expect(listAPIKeysMock).toHaveBeenCalledTimes(2);
    });
    expect(toastSuccess).toHaveBeenCalledWith("API key created");
    expect(screen.getByText("tb_secret_key")).toBeInTheDocument();
  });

  it("支持保存 tenant overrides", async () => {
    listTenantsMock.mockResolvedValue({ data: tenants });
    listPlansMock.mockResolvedValue({ data: plans });
    getTenantConfigMock
      .mockResolvedValueOnce({
        data: {
          max_domains: 5,
          max_mailboxes_per_domain: 100,
          max_messages_per_mailbox: 200,
          max_message_bytes: 10485760,
          retention_hours: 48,
          rpm_limit: 60,
          daily_quota: 1000,
        },
      })
      .mockResolvedValueOnce({
        data: {
          max_domains: 9,
          max_mailboxes_per_domain: 100,
          max_messages_per_mailbox: 200,
          max_message_bytes: 10485760,
          retention_hours: 72,
          rpm_limit: 60,
          daily_quota: 1000,
        },
      });
    updateTenantOverridesMock.mockResolvedValue({ data: {} });

    render(<TenantsPage />);

    expect(await screen.findByText("Acme")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Overrides" }));

    await waitFor(() => {
      expect(getTenantConfigMock).toHaveBeenCalledWith("tenant-1");
    });

    const overrideInputs = screen.getAllByPlaceholderText("inherit");
    fireEvent.change(overrideInputs[0], {
      target: { value: "9" },
    });
    fireEvent.change(overrideInputs[4], {
      target: { value: "72" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save Overrides" }));

    await waitFor(() => {
      expect(updateTenantOverridesMock).toHaveBeenCalledWith("tenant-1", {
        max_domains: 9,
        max_mailboxes_per_domain: null,
        max_messages_per_mailbox: null,
        max_message_bytes: null,
        retention_hours: 72,
        rpm_limit: null,
        daily_quota: null,
      });
    });
    expect(toastSuccess).toHaveBeenCalledWith("Tenant overrides updated");
  });
});
