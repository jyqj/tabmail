import React from "react";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import RoutesPage from "./page";

const {
  listRoutesMock,
  createRouteMock,
  deleteRouteMock,
  toastSuccess,
  toastError,
} = vi.hoisted(() => ({
  listRoutesMock: vi.fn(),
  createRouteMock: vi.fn(),
  deleteRouteMock: vi.fn(),
  toastSuccess: vi.fn(),
  toastError: vi.fn(),
}));

vi.mock("next/navigation", () => ({
  useParams: () => ({ id: "zone-1" }),
}));

vi.mock("@/lib/api", () => ({
  listRoutes: (...args: unknown[]) => listRoutesMock(...args),
  createRoute: (...args: unknown[]) => createRouteMock(...args),
  deleteRoute: (...args: unknown[]) => deleteRouteMock(...args),
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
      <button type="button" onClick={() => onValueChange("sequence")}>
        choose-sequence
      </button>
      <button type="button" onClick={() => onValueChange("token")}>
        choose-token
      </button>
      {children}
    </div>
  ),
  SelectContent: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  SelectItem: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  SelectTrigger: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  SelectValue: () => <span>select</span>,
}));

vi.mock("@/components/ui/skeleton", () => ({
  Skeleton: () => <div>loading</div>,
}));

describe("console/domain routes page", () => {
  beforeEach(() => {
    listRoutesMock.mockReset();
    createRouteMock.mockReset();
    deleteRouteMock.mockReset();
    toastSuccess.mockReset();
    toastError.mockReset();
  });

  afterEach(() => {
    cleanup();
  });

  it("加载列表并支持创建 sequence route", async () => {
    listRoutesMock
      .mockResolvedValueOnce({
        data: [
          {
            id: "route-1",
            route_type: "wildcard",
            match_value: "*.mail.test",
            range_start: null,
            range_end: null,
            auto_create_mailbox: true,
            access_mode_default: "public",
            created_at: new Date().toISOString(),
          },
        ],
      })
      .mockResolvedValueOnce({
        data: [
          {
            id: "route-1",
            route_type: "wildcard",
            match_value: "*.mail.test",
            range_start: null,
            range_end: null,
            auto_create_mailbox: true,
            access_mode_default: "public",
            created_at: new Date().toISOString(),
          },
          {
            id: "route-2",
            route_type: "sequence",
            match_value: "box-{n}.mail.test",
            range_start: 1,
            range_end: 100,
            auto_create_mailbox: false,
            access_mode_default: "token",
            created_at: new Date().toISOString(),
          },
        ],
      });
    createRouteMock.mockResolvedValue({ data: {} });

    render(<RoutesPage />);

    expect(await screen.findByText("*.mail.test")).toBeInTheDocument();

    fireEvent.click(screen.getAllByRole("button", { name: "choose-sequence" })[0]);
    fireEvent.change(screen.getByPlaceholderText("box-{n}.mail.example.com"), {
      target: { value: "box-{n}.mail.test" },
    });
    fireEvent.change(screen.getByPlaceholderText("1"), {
      target: { value: "1" },
    });
    fireEvent.change(screen.getByPlaceholderText("5000"), {
      target: { value: "100" },
    });
    fireEvent.change(screen.getByPlaceholderText("Inherit tenant default"), {
      target: { value: "72" },
    });
    fireEvent.click(screen.getByRole("button", { name: "true" }));
    fireEvent.click(screen.getAllByRole("button", { name: "choose-token" })[1]);
    fireEvent.click(screen.getByRole("button", { name: "Create" }));

    await waitFor(() => {
      expect(createRouteMock).toHaveBeenCalledWith("zone-1", {
        route_type: "sequence",
        match_value: "box-{n}.mail.test",
        range_start: 1,
        range_end: 100,
        auto_create_mailbox: false,
        retention_hours_override: 72,
        access_mode_default: "token",
      });
    });
    await waitFor(() => {
      expect(listRoutesMock).toHaveBeenCalledTimes(2);
    });
    expect(toastSuccess).toHaveBeenCalledWith("Route created");
  });

  it("支持删除 route", async () => {
    listRoutesMock
      .mockResolvedValueOnce({
        data: [
          {
            id: "route-1",
            route_type: "exact",
            match_value: "user@mail.test",
            range_start: null,
            range_end: null,
            auto_create_mailbox: true,
            access_mode_default: "public",
            created_at: new Date().toISOString(),
          },
        ],
      })
      .mockResolvedValueOnce({ data: [] });
    deleteRouteMock.mockResolvedValue(undefined);

    render(<RoutesPage />);

    expect(await screen.findByText("user@mail.test")).toBeInTheDocument();

    const buttons = screen.getAllByRole("button");
    fireEvent.click(buttons[buttons.length - 1]);

    await waitFor(() => {
      expect(deleteRouteMock).toHaveBeenCalledWith("zone-1", "route-1");
    });
    await waitFor(() => {
      expect(listRoutesMock).toHaveBeenCalledTimes(2);
    });
    expect(toastSuccess).toHaveBeenCalledWith("Route deleted");
  });
});
