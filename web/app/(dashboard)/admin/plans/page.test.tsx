import React from "react";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import PlansPage from "./page";

const {
  listPlansMock,
  createPlanMock,
  deletePlanMock,
  toastSuccess,
  toastError,
} = vi.hoisted(() => ({
  listPlansMock: vi.fn(),
  createPlanMock: vi.fn(),
  deletePlanMock: vi.fn(),
  toastSuccess: vi.fn(),
  toastError: vi.fn(),
}));

vi.mock("@/lib/api", () => ({
  listPlans: (...args: unknown[]) => listPlansMock(...args),
  createPlan: (...args: unknown[]) => createPlanMock(...args),
  deletePlan: (...args: unknown[]) => deletePlanMock(...args),
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

vi.mock("@/components/ui/skeleton", () => ({
  Skeleton: () => <div>loading</div>,
}));

describe("admin/plans page", () => {
  beforeEach(() => {
    listPlansMock.mockReset();
    createPlanMock.mockReset();
    deletePlanMock.mockReset();
    toastSuccess.mockReset();
    toastError.mockReset();
  });

  afterEach(() => {
    cleanup();
  });

  it("加载列表并支持创建 plan", async () => {
    const basePlan = {
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
    };

    listPlansMock
      .mockResolvedValueOnce({ data: [basePlan] })
      .mockResolvedValueOnce({
        data: [
          basePlan,
          {
            ...basePlan,
            id: "plan-2",
            name: "pro",
            max_domains: 20,
          },
        ],
      });
    createPlanMock.mockResolvedValue({ data: {} });

    render(<PlansPage />);

    expect(await screen.findByText("starter")).toBeInTheDocument();

    fireEvent.change(screen.getByPlaceholderText("Plan Name"), {
      target: { value: "pro" },
    });
    fireEvent.change(screen.getByPlaceholderText("Max Domains"), {
      target: { value: "20" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Create" }));

    await waitFor(() => {
      expect(createPlanMock).toHaveBeenCalledWith({
        name: "pro",
        max_domains: 20,
        max_mailboxes_per_domain: 100,
        max_messages_per_mailbox: 200,
        max_message_bytes: 10485760,
        retention_hours: 48,
        rpm_limit: 60,
        daily_quota: 1000,
      });
    });
    await waitFor(() => {
      expect(listPlansMock).toHaveBeenCalledTimes(2);
    });
    expect(toastSuccess).toHaveBeenCalledWith("Plan created");
  });

  it("支持删除 plan", async () => {
    const plan = {
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
    };

    listPlansMock
      .mockResolvedValueOnce({ data: [plan] })
      .mockResolvedValueOnce({ data: [] });
    deletePlanMock.mockResolvedValue(undefined);

    render(<PlansPage />);

    expect(await screen.findByText("starter")).toBeInTheDocument();

    const buttons = screen.getAllByRole("button");
    fireEvent.click(buttons[buttons.length - 1]);

    await waitFor(() => {
      expect(deletePlanMock).toHaveBeenCalledWith("plan-1");
    });
    await waitFor(() => {
      expect(listPlansMock).toHaveBeenCalledTimes(2);
    });
    expect(toastSuccess).toHaveBeenCalledWith("Plan deleted");
  });
});
