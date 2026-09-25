import React from "react";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import CompanyPage from "@/app/(dashboard)/company/page";

const { domains, verify } = vi.hoisted(() => ({ domains: vi.fn(), verify: vi.fn() }));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock("@/contexts/auth-context", () => ({ useAuth: () => ({ user: { id: "admin", role: "admin" } }) }));
vi.mock("@/lib/company", async (original) => ({
  ...(await original<typeof import("@/lib/company")>()),
  companyDomains: () => domains(),
  verifyCompanyDomain: (...args: unknown[]) => verify(...args),
  allEmployees: async () => [],
  workMailboxes: async () => [],
  company: async (path: string) => path === "/settings" ? { name: "Example", primary_zone_id: "", revision: 0 } : [],
}));

describe("Company domain onboarding", () => {
  afterEach(() => cleanup());
  it("updates the actual primary-domain selector after verification without reloading the page", async () => {
    const domain = { id: "zone-1", domain: "company.test", is_verified: false, mx_verified: false, dkim_enabled: false, txt_record: "token", expected_mx: "mx.test", created_at: "2026-01-01" };
    domains.mockResolvedValue([domain]);
    verify.mockImplementation(async () => {
      const verified = { ...domain, is_verified: true, mx_verified: true };
      domains.mockResolvedValue([verified]);
      return { ...verified, checks: { txt: { status: "pass" }, mx: { status: "pass" }, spf: { status: "pass" }, dkim: { status: "pass" }, dmarc: { status: "pass" } } };
    });
    render(<CompanyPage />);
    await screen.findByText("company.test");
    expect(screen.queryByRole("option", { name: "company.test" })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: /^Verify$/ }));
    expect(await screen.findByRole("option", { name: "company.test" })).toHaveValue("zone-1");
  });
});
