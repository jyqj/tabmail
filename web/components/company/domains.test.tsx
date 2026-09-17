import React from "react";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { CompanyDomainsSection } from "./domains";

const { toastSuccess, toastError, domainsMock, addMock, verifyMock, deleteMock } =
  vi.hoisted(() => ({
    toastSuccess: vi.fn(),
    toastError: vi.fn(),
    domainsMock: vi.fn(),
    addMock: vi.fn(),
    verifyMock: vi.fn(),
    deleteMock: vi.fn(),
  }));

vi.mock("sonner", () => ({
  toast: { success: toastSuccess, error: toastError },
}));

vi.mock("@/lib/company", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/company")>()),
  companyDomains: () => domainsMock(),
  addCompanyDomain: (...args: unknown[]) => addMock(...args),
  verifyCompanyDomain: (...args: unknown[]) => verifyMock(...args),
  deleteCompanyDomain: (...args: unknown[]) => deleteMock(...args),
}));

import type { CompanyDomain, DomainVerification } from "@/lib/company";

const baseDomain: CompanyDomain = {
  id: "dom-1",
  domain: "mail.example.com",
  is_verified: false,
  mx_verified: true,
  dkim_enabled: false,
  txt_record: "tabmail-verify=abc123",
  expected_mx: "10 mx.tabmail.test",
  created_at: "2026-09-17T00:00:00Z",
};

const verifiedDomain: CompanyDomain = {
  ...baseDomain,
  id: "dom-2",
  domain: "verified.example.com",
  is_verified: true,
  dkim_enabled: true,
  dkim_host: "tabmail._domainkey.verified.example.com",
  dkim_record: "v=DKIM1; k=rsa; p=MIIB",
};

const verification: DomainVerification = {
  id: verifiedDomain.id,
  domain: verifiedDomain.domain,
  is_verified: true,
  mx_verified: true,
  dkim_enabled: true,
  txt_record: verifiedDomain.txt_record,
  expected_mx: verifiedDomain.expected_mx,
  dkim_host: verifiedDomain.dkim_host,
  dkim_record: verifiedDomain.dkim_record,
  checks: {
    txt: { status: "pass", details: ["TXT record found"] },
    mx: { status: "pass" },
    spf: { status: "fail", details: ["no SPF record"] },
    dkim: { status: "pass" },
    dmarc: { status: "fail" },
  },
};

describe("CompanyDomainsSection", () => {
  beforeEach(() => {
    toastSuccess.mockReset();
    toastError.mockReset();
    addMock.mockReset();
    verifyMock.mockReset();
    deleteMock.mockReset();
    domainsMock.mockReset().mockResolvedValue([baseDomain, verifiedDomain]);
    vi.spyOn(window, "confirm").mockReturnValue(true);
  });

  afterEach(() => cleanup());

  it("renders the domain list with verification badges", async () => {
    render(<CompanyDomainsSection />);
    expect(await screen.findByText("mail.example.com")).toBeTruthy();
    expect(screen.getByText("Ownership unverified")).toBeTruthy();
    expect(screen.getAllByText("MX verified").length).toBe(2);
    expect(screen.getByText("DKIM enabled")).toBeTruthy();
  });

  it("adds a domain, refreshes the list and clears the input", async () => {
    addMock.mockResolvedValue(baseDomain);
    render(<CompanyDomainsSection />);
    await screen.findByText("mail.example.com");
    const callsBefore = domainsMock.mock.calls.length;
    fireEvent.change(screen.getByLabelText("Add a domain"), {
      target: { value: "new.example.com" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Add domain" }));
    await waitFor(() => {
      expect(addMock).toHaveBeenCalledWith("new.example.com");
      expect(domainsMock.mock.calls.length).toBeGreaterThan(callsBefore);
    });
    expect(
      (screen.getByLabelText("Add a domain") as HTMLInputElement).value,
    ).toBe("");
    expect(toastSuccess).toHaveBeenCalled();
  });

  it("shows DNS records and explains a missing DKIM record", async () => {
    render(<CompanyDomainsSection />);
    await screen.findByText("mail.example.com");
    fireEvent.click(
      screen.getAllByRole("button", { name: "View DNS records" })[0],
    );
    expect(screen.getByText("tabmail-verify=abc123")).toBeTruthy();
    expect(
      screen.getByText(
        "No DKIM record yet: it is generated after ownership verification completes.",
      ),
    ).toBeTruthy();
  });

  it("runs verification, shows all five checks and refreshes", async () => {
    verifyMock.mockResolvedValue(verification);
    render(<CompanyDomainsSection />);
    await screen.findByText("mail.example.com");
    const callsBefore = domainsMock.mock.calls.length;
    fireEvent.click(screen.getAllByRole("button", { name: "Verify" })[0]);
    expect(await screen.findByText("Verification results")).toBeTruthy();
    expect(screen.getByText("TXT record found")).toBeTruthy();
    expect(screen.getByText("no SPF record")).toBeTruthy();
    expect(screen.getAllByText("pass").length).toBe(3);
    expect(screen.getAllByText("fail").length).toBe(2);
    await waitFor(() => {
      expect(domainsMock.mock.calls.length).toBeGreaterThan(callsBefore);
    });
  });

  it("confirms via safeConfirm before deleting", async () => {
    deleteMock.mockResolvedValue(undefined);
    render(<CompanyDomainsSection />);
    await screen.findByText("mail.example.com");
    fireEvent.click(screen.getAllByRole("button", { name: "Delete" })[0]);
    await waitFor(() => {
      expect(window.confirm).toHaveBeenCalled();
      expect(deleteMock).toHaveBeenCalledWith("dom-1");
    });
    expect(toastSuccess).toHaveBeenCalledWith("Domain deleted");
  });
});
