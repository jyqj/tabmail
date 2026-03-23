import React from "react";
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import AdminPage from "./page";

const { getStatsMock, toastError } = vi.hoisted(() => ({
  getStatsMock: vi.fn(),
  toastError: vi.fn(),
}));

vi.mock("@/lib/api", () => ({
  getStats: (...args: unknown[]) => getStatsMock(...args),
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
  }: {
    title: string;
    description?: string;
  }) => (
    <div>
      <h1>{title}</h1>
      <div>{description}</div>
    </div>
  ),
}));

vi.mock("@/components/ui/card", () => ({
  Card: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  CardContent: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  CardDescription: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  CardHeader: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  CardTitle: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
}));

vi.mock("@/components/ui/badge", () => ({
  Badge: ({ children }: { children: React.ReactNode }) => <span>{children}</span>,
}));

vi.mock("@/components/ui/skeleton", () => ({
  Skeleton: () => <div>loading</div>,
}));

vi.mock("@/components/ui/table", () => ({
  Table: ({ children }: { children: React.ReactNode }) => <table>{children}</table>,
  TableBody: ({ children }: { children: React.ReactNode }) => <tbody>{children}</tbody>,
  TableCell: ({ children }: { children: React.ReactNode }) => <td>{children}</td>,
  TableHead: ({ children }: { children: React.ReactNode }) => <th>{children}</th>,
  TableHeader: ({ children }: { children: React.ReactNode }) => <thead>{children}</thead>,
  TableRow: ({ children }: { children: React.ReactNode }) => <tr>{children}</tr>,
}));

describe("admin dashboard page", () => {
  beforeEach(() => {
    getStatsMock.mockReset();
    toastError.mockReset();
  });

  afterEach(() => {
    cleanup();
  });

  it("加载并渲染 stats 概览、审计与 dead letters", async () => {
    getStatsMock.mockResolvedValue({
      data: {
        tenants_count: 2,
        plans_count: 1,
        domains_count: 3,
        mailboxes_count: 4,
        messages_count: 5,
        tenant_delivery: [
          { key: "tenant-a", accepted: 10, rejected: 1, deliveries_ok: 8, deliveries_failed: 2 },
        ],
        mailbox_delivery: [
          { key: "user@mail.test", accepted: 9, rejected: 0, deliveries_ok: 9, deliveries_failed: 0 },
        ],
        dead_letters: [
          {
            id: "dead-1",
            url: "https://hook.test",
            event_type: "message.received",
            payload: {},
            attempts: 3,
            last_error: "timeout",
            created_at: new Date().toISOString(),
            last_tried_at: new Date().toISOString(),
          },
        ],
        metrics: {
          started_at: new Date().toISOString(),
          uptime_seconds: 3600,
          smtp: {
            sessions_opened: 11,
            sessions_active: 2,
            recipients_accepted: 20,
            recipients_rejected: 3,
            messages_accepted: 12,
            messages_rejected: 1,
            deliveries_succeeded: 8,
            deliveries_failed: 2,
            bytes_received: 2048,
          },
          webhooks: {
            enabled: true,
            configured: 2,
            queued: 1,
            delivered: 5,
            failed: 1,
            retried: 2,
            dead_letter_size: 1,
          },
          realtime: {
            subscribers_current: 3,
            events_published: 15,
          },
          time_series: [
            {
              at: new Date().toISOString(),
              smtp_accepted: 1,
              smtp_rejected: 0,
              deliveries_ok: 1,
              deliveries_failed: 0,
              webhooks_delivered: 1,
              webhooks_failed: 0,
              realtime_published: 2,
            },
            {
              at: new Date().toISOString(),
              smtp_accepted: 2,
              smtp_rejected: 1,
              deliveries_ok: 1,
              deliveries_failed: 1,
              webhooks_delivered: 0,
              webhooks_failed: 1,
              realtime_published: 3,
            },
          ],
        },
        recent_audit: [
          {
            id: "audit-1",
            actor: "admin",
            action: "tenant.create",
            resource_type: "tenant",
            created_at: new Date().toISOString(),
          },
        ],
      },
    });

    render(<AdminPage />);

    expect(await screen.findByText("Admin Dashboard")).toBeInTheDocument();
    expect(screen.getByText("Tenants")).toBeInTheDocument();
    expect(screen.getByText("Messages")).toBeInTheDocument();
    expect(screen.getByText("Delivery timeline")).toBeInTheDocument();
    expect(screen.getByText("Top tenant delivery")).toBeInTheDocument();
    expect(screen.getByText("tenant-a")).toBeInTheDocument();
    expect(screen.getByText("Recent audit")).toBeInTheDocument();
    expect(screen.getByText("tenant.create")).toBeInTheDocument();
    expect(screen.getByText("Dead-letter queue")).toBeInTheDocument();
    expect(screen.getByText("https://hook.test")).toBeInTheDocument();
    expect(screen.getByText("timeout")).toBeInTheDocument();
  });
});
