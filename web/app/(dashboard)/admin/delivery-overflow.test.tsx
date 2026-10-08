import React from "react";
import { cleanup, render, screen, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import AdminPage from "./page";

const { getStatsMock } = vi.hoisted(() => ({ getStatsMock: vi.fn() }));

vi.mock("@/lib/api", () => ({
  getStats: (...args: unknown[]) => getStatsMock(...args),
}));

// The surrounding dashboard shell owns the sidebar provider. Keep this test
// on the real page, data hook, tables and locale consumer without mounting it.
vi.mock("@/components/layout/page-header", () => ({
  PageHeader: ({ title, description }: { title: string; description?: string }) => (
    <header><h1>{title}</h1><p>{description}</p></header>
  ),
}));

function statsFixture(includeAggregate = true) {
  const ordinaryTenant = { key: "aggregate", accepted: 11, rejected: 12, deliveries_ok: 13, deliveries_failed: 14 };
  const ordinaryMailbox = { key: "__overflow__@example.test", accepted: 21, rejected: 22, deliveries_ok: 23, deliveries_failed: 24 };
  const aggregate = { key: "", aggregate: true, accepted: 101, rejected: 102, deliveries_ok: 103, deliveries_failed: 104 };
  return {
    data: {
      tenants_count: 2, plans_count: 1, domains_count: 3, mailboxes_count: 4, messages_count: 5,
      tenant_delivery: includeAggregate ? [ordinaryTenant, aggregate] : [ordinaryTenant],
      mailbox_delivery: includeAggregate ? [ordinaryMailbox, aggregate] : [ordinaryMailbox],
      recent_audit: [], dead_letters: [],
      metrics: {
        started_at: "2026-10-08T00:00:00Z", uptime_seconds: 60,
        smtp: {
          sessions_opened: 1, sessions_active: 0, recipients_accepted: 1000, recipients_rejected: 2000,
          messages_accepted: 0, messages_rejected: 0, deliveries_succeeded: 0, deliveries_failed: 0, bytes_received: 0,
        },
        webhooks: { enabled: false, configured: 0, queued: 0, delivered: 0, failed: 0, retried: 0, dead_letter_size: 0 },
        realtime: { subscribers_current: 0, events_published: 0 },
        time_series: [],
      },
    },
  };
}

describe("admin delivery overflow consumer", () => {
  beforeEach(() => { getStatsMock.mockReset(); });
  afterEach(() => { cleanup(); });

  for (const scenario of [
    { locale: "en", dimension: "tenant", label: "Other tenants (combined)", description: /first 4,096 tenants.*since process start.*final row/i },
    { locale: "en", dimension: "mailbox", label: "Other mailboxes (combined)", description: /first 4,096 mailboxes.*since process start.*final row/i },
    { locale: "zh", dimension: "tenant", label: "其他租户（合计）", description: /本进程启动.*4,096 个租户.*末行/ },
    { locale: "zh", dimension: "mailbox", label: "其他邮箱（合计）", description: /本进程启动.*4,096 个邮箱.*末行/ },
  ]) {
    it(`${scenario.locale} identifies the ${scenario.dimension} aggregate and its counting scope`, async () => {
      localStorage.setItem("tabmail-locale", scenario.locale);
      getStatsMock.mockResolvedValue(statsFixture());
      render(<AdminPage />);

      const label = await screen.findByText(scenario.label);
      const row = label.closest("tr");
      if (!row) throw new Error("aggregate label is not a delivery table row");
      expect(within(row).getAllByRole("cell").map((cell) => cell.textContent)).toEqual([
        scenario.label, "101", "102", "103", "104",
      ]);
      expect(screen.getByText(scenario.description)).toBeInTheDocument();
      expect(getStatsMock).toHaveBeenCalledTimes(1);
    });
  }

  it("keeps real keys resembling sentinels separate from the aggregate", async () => {
    getStatsMock.mockResolvedValue(statsFixture());
    render(<AdminPage />);

    const tenant = await screen.findByText("aggregate");
    const mailbox = screen.getByText("__overflow__@example.test");
    expect(tenant.closest("tr")).toHaveTextContent("11121314");
    expect(mailbox.closest("tr")).toHaveTextContent("21222324");
    expect(screen.getByText("Other tenants (combined)").closest("tr")).not.toBe(tenant.closest("tr"));
    expect(screen.getByText("Other mailboxes (combined)").closest("tr")).not.toBe(mailbox.closest("tr"));
  });

  it("still renders ordinary legacy responses without an aggregate flag", async () => {
    getStatsMock.mockResolvedValue(statsFixture(false));
    render(<AdminPage />);

    expect(await screen.findByText("aggregate")).toBeInTheDocument();
    expect(screen.getByText("__overflow__@example.test")).toBeInTheDocument();
    expect(screen.queryByText("Other tenants (combined)")).not.toBeInTheDocument();
    expect(screen.queryByText("Other mailboxes (combined)")).not.toBeInTheDocument();
    expect(screen.getByText("1000")).toBeInTheDocument();
    expect(screen.getByText("2000")).toBeInTheDocument();
  });
});
