import React from "react";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import AdminPolicyPage from "./page";

const {
  getSMTPPolicyMock,
  updateSMTPPolicyMock,
  toastSuccess,
  toastError,
} = vi.hoisted(() => ({
  getSMTPPolicyMock: vi.fn(),
  updateSMTPPolicyMock: vi.fn(),
  toastSuccess: vi.fn(),
  toastError: vi.fn(),
}));

vi.mock("@/lib/api", () => ({
  getSMTPPolicy: (...args: unknown[]) => getSMTPPolicyMock(...args),
  updateSMTPPolicy: (...args: unknown[]) => updateSMTPPolicyMock(...args),
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

vi.mock("@/components/ui/skeleton", () => ({
  Skeleton: () => <div>loading</div>,
}));

vi.mock("@/components/ui/switch", () => ({
  Switch: ({
    checked,
    onCheckedChange,
  }: {
    checked: boolean;
    onCheckedChange: (checked: boolean) => void;
  }) => (
    <button type="button" aria-pressed={checked} onClick={() => onCheckedChange(!checked)}>
      {String(checked)}
    </button>
  ),
}));

describe("admin/policy page", () => {
  beforeEach(() => {
    getSMTPPolicyMock.mockReset();
    updateSMTPPolicyMock.mockReset();
    toastSuccess.mockReset();
    toastError.mockReset();
  });

  afterEach(() => {
    cleanup();
  });

  it("加载并展示 SMTP policy", async () => {
    getSMTPPolicyMock.mockResolvedValue({
      data: {
        default_accept: false,
        accept_domains: ["example.com", "*.internal"],
        reject_domains: ["blocked.test"],
        default_store: true,
        store_domains: ["persisted.test"],
        discard_domains: ["discard.test"],
        reject_origin_domains: ["spam.test"],
      },
    });

    render(<AdminPolicyPage />);

    expect(await screen.findByDisplayValue("example.com, *.internal")).toBeInTheDocument();
    expect(screen.getByDisplayValue("blocked.test")).toBeInTheDocument();
    expect(screen.getByDisplayValue("persisted.test")).toBeInTheDocument();
    expect(screen.getByDisplayValue("discard.test")).toBeInTheDocument();
    expect(screen.getByDisplayValue("spam.test")).toBeInTheDocument();
  });

  it("支持编辑并保存 SMTP policy", async () => {
    getSMTPPolicyMock.mockResolvedValue({
      data: {
        default_accept: true,
        accept_domains: [],
        reject_domains: [],
        default_store: true,
        store_domains: [],
        discard_domains: [],
        reject_origin_domains: [],
      },
    });
    updateSMTPPolicyMock.mockResolvedValue({ data: {} });

    render(<AdminPolicyPage />);

    await screen.findByText("Delivery rules");

    fireEvent.click(screen.getAllByRole("button", { pressed: true })[0]);
    fireEvent.click(screen.getAllByRole("button", { pressed: true })[0]);

    const inputs = screen.getAllByRole("textbox");
    fireEvent.change(inputs[0], { target: { value: "example.com, *.internal" } });
    fireEvent.change(inputs[1], { target: { value: "blocked.test" } });
    fireEvent.change(inputs[2], { target: { value: "persisted.test" } });
    fireEvent.change(inputs[3], { target: { value: "discard.test" } });
    fireEvent.change(inputs[4], { target: { value: "spam.test, *.bad.sender" } });

    fireEvent.click(screen.getByRole("button", { name: "Save Policy" }));

    await waitFor(() => {
      expect(updateSMTPPolicyMock).toHaveBeenCalledWith({
        default_accept: false,
        accept_domains: ["example.com", "*.internal"],
        reject_domains: ["blocked.test"],
        default_store: false,
        store_domains: ["persisted.test"],
        discard_domains: ["discard.test"],
        reject_origin_domains: ["spam.test", "*.bad.sender"],
      });
    });
    expect(toastSuccess).toHaveBeenCalledWith("SMTP policy updated");
  });
});
