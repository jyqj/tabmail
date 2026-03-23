import React from "react";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import AdminMonitorPage from "./page";

const {
  listMonitorHistoryMock,
  streamAdminMonitorEventsMock,
  toastError,
} = vi.hoisted(() => ({
  listMonitorHistoryMock: vi.fn(),
  streamAdminMonitorEventsMock: vi.fn(),
  toastError: vi.fn(),
}));

vi.mock("@/lib/api", () => ({
  listMonitorHistory: (...args: unknown[]) => listMonitorHistoryMock(...args),
  streamAdminMonitorEvents: (...args: unknown[]) => streamAdminMonitorEventsMock(...args),
}));

vi.mock("sonner", () => ({
  toast: {
    error: toastError,
    success: vi.fn(),
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

vi.mock("@/components/ui/scroll-area", () => ({
  ScrollArea: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
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
      <button type="button" onClick={() => onValueChange("delete")}>
        choose-delete
      </button>
      {children}
    </div>
  ),
  SelectContent: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  SelectItem: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  SelectTrigger: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  SelectValue: ({ placeholder }: { placeholder?: string }) => <span>{placeholder ?? "select"}</span>,
}));

describe("admin/monitor page", () => {
  beforeEach(() => {
    listMonitorHistoryMock.mockReset();
    streamAdminMonitorEventsMock.mockReset();
    toastError.mockReset();
  });

  afterEach(() => {
    cleanup();
  });

  it("加载历史并接收实时事件", async () => {
    let onEvent: ((event: { type: string; data: unknown }) => void) | undefined;
    streamAdminMonitorEventsMock.mockImplementation(
      async ({ onEvent: handler }: { onEvent: typeof onEvent }) => {
        onEvent = handler;
      }
    );
    listMonitorHistoryMock.mockResolvedValue({
      data: [
        {
          type: "message",
          mailbox: "history@mail.test",
          sender: "history@test.dev",
          subject: "History message",
          message_id: "hist-1",
          at: new Date().toISOString(),
        },
      ],
      meta: { total: 1, page: 1, per_page: 30 },
    });

    render(<AdminMonitorPage />);

    expect(await screen.findByText("History message")).toBeInTheDocument();
    expect(streamAdminMonitorEventsMock).toHaveBeenCalledTimes(1);

    onEvent?.({ type: "ready", data: {} });
    await waitFor(() => {
      expect(screen.getByText("Live")).toBeInTheDocument();
    });

    onEvent?.({
      type: "message",
      data: {
        type: "message",
        mailbox: "live@mail.test",
        sender: "live@test.dev",
        subject: "Live message",
        message_id: "live-1",
        at: new Date().toISOString(),
      },
    });

    expect(await screen.findByText("Live message")).toBeInTheDocument();
    expect(screen.getByText("Buffered events")).toBeInTheDocument();
    expect(screen.getByText("Messages")).toBeInTheDocument();
  });

  it("支持筛选历史并重新连接", async () => {
    streamAdminMonitorEventsMock.mockResolvedValue(undefined);
    listMonitorHistoryMock.mockResolvedValue({
      data: [],
      meta: { total: 40, page: 1, per_page: 30 },
    });

    render(<AdminMonitorPage />);

    await waitFor(() => {
      expect(listMonitorHistoryMock).toHaveBeenCalledWith({
        page: 1,
        per_page: 30,
        type: undefined,
        mailbox: undefined,
        sender: undefined,
      });
    });

    fireEvent.click(screen.getByRole("button", { name: "choose-delete" }));
    fireEvent.change(screen.getByPlaceholderText("Filter mailbox"), {
      target: { value: "box" },
    });
    fireEvent.change(screen.getByPlaceholderText("Filter sender"), {
      target: { value: "alice" },
    });

    await waitFor(() => {
      expect(listMonitorHistoryMock).toHaveBeenLastCalledWith({
        page: 1,
        per_page: 30,
        type: "delete",
        mailbox: "box",
        sender: "alice",
      });
    });

    fireEvent.click(screen.getByRole("button", { name: "Reconnect" }));
    await waitFor(() => {
      expect(streamAdminMonitorEventsMock).toHaveBeenCalledTimes(2);
    });
  });
});
