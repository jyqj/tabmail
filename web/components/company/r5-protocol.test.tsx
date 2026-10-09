import React from "react";
import { createHash } from "node:crypto";
import { execFileSync } from "node:child_process";
import { mkdtempSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { resolve } from "node:path";
import { cleanup, render, screen } from "@testing-library/react";
import { afterAll, afterEach, beforeAll, describe, expect, it, vi } from "vitest";
import type { Submission } from "@/lib/company";
import { parseOrdinaryReceipt, type ReceiptCounts } from "@/lib/receipt-types";
import { SubmissionPane } from "@/features/mail/components/submission-pane";

const { api } = vi.hoisted(() => ({ api: vi.fn() }));
// The Go-policy observation is the actual closed ordinary wire projection.
// Only the hook response is injected here: this is not HTTP/PG/auth evidence.
// Never reconstruct a receipt from expected status/capabilities or legacy
// address-redaction expectations; the latter remain independently checked in Go.
vi.mock("@/hooks/use-api", () => ({ useAPI: (...args: unknown[]) => api(...args) }));

interface Case {
  id: string;
  input: { operation: unknown; job_state?: string; recipients?: { category: string; address: string; state: string }[] };
  expected: { projection?: { status: Submission["status"]; visible_addresses: string[]; hidden_addresses: string[]; retry: boolean; delivery_uncertain: boolean; label_zh: string; label_en: string } };
}
const root = resolve(process.cwd(), "..");
const raw = readFileSync(resolve(root, "docs/company-mail/evidence/R5-PROTOCOL-CASES.json"));
const cases = (JSON.parse(raw.toString()) as { cases: Case[] }).cases.filter((c) => c.input.operation === "receipt" && c.input.job_state);
let temporary: string | undefined;
let observations: { case_sha256: string; receipts: Record<string, unknown> };

beforeAll(() => {
  let observationPath = process.env.TABMAIL_R5_PROTOCOL_OBSERVATIONS;
  if (!observationPath) {
    temporary = mkdtempSync(resolve(tmpdir(), "tabmail-r5-protocol-component-"));
    observationPath = resolve(temporary, "observations.json");
    execFileSync("go", ["test", "-count=1", "-run", "^TestR5ProtocolDeliverySharedCases$", "./internal/delivery"], {
      cwd: root,
      env: { ...process.env, TABMAIL_R5_PROTOCOL_OBSERVATIONS: observationPath },
      timeout: 120_000,
      stdio: "pipe",
    });
  }
  observations = JSON.parse(readFileSync(observationPath, "utf8"));
  expect(observations.case_sha256).toBe(createHash("sha256").update(raw).digest("hex"));
  expect(cases).toHaveLength(3);
});
afterEach(cleanup);
afterAll(() => { if (temporary) rmSync(temporary, { recursive: true }); });

describe("R5 shared receipt", () => {
  for (const c of cases) {
    it(c.id, () => {
      const want = c.expected.projection!;
      const wire = observations.receipts[c.id];
      expect(wire).toBeDefined();
      // Actual Go output must pass the shipping closed parser before React.
      // A stale raw-job/address DTO fails instead of receiving a type cast.
      const receipt = parseOrdinaryReceipt(wire);
      expect(receipt.status).toBe(want.status);
      expect(receipt.capabilities?.retry).toBe(want.retry);
      expect(receipt.delivery_uncertain).toBe(want.delivery_uncertain);
      // These three shared Go scenarios require a complete known ledger. Its
      // full input is an aggregate oracle only, never a replacement wire DTO.
      expect(receipt.progress.completeness).toBe("known");
      if (receipt.progress.completeness === "known") {
        const recipients = c.input.recipients;
        if (!recipients?.length) throw new Error("Known shared receipt requires a complete input ledger");
        const counts: ReceiptCounts = { total: recipients.length, accepted: 0, pending: 0, temporary: 0, permanent: 0, uncertain: 0 };
        for (const recipient of recipients) {
          switch (recipient.state) {
            case "accepted": counts.accepted++; break;
            case "pending": counts.pending++; break;
            case "temporary": counts.temporary++; break;
            case "permanent": counts.permanent++; break;
            case "uncertain": counts.uncertain++; break;
            default: throw new Error("Unknown shared ledger state cannot produce known counts");
          }
        }
        expect(receipt.progress.counts).toEqual(counts);
      } else {
        // Unknown progress never inherits manufactured zero/known counts.
        expect(receipt.progress).toEqual({ completeness: "unknown" });
      }
      const encodedWire = JSON.stringify(wire);
      for (const address of [...(c.input.recipients ?? []).map(recipient => recipient.address),
        ...want.visible_addresses, ...want.hidden_addresses]) expect(encodedWire).not.toContain(address);
      api.mockReturnValue({ data: receipt, error: undefined, mutate: vi.fn(), isLoading: false });
      render(<SubmissionPane id={receipt.id} />);
      expect(screen.getByText(want.label_en)).toBeInTheDocument();
      expect(screen.queryByRole("table")).not.toBeInTheDocument();
      for (const address of [...(c.input.recipients ?? []).map(recipient => recipient.address),
        ...want.visible_addresses, ...want.hidden_addresses]) expect(document.body).not.toHaveTextContent(address);
      const retry = screen.queryByRole("button", { name: "Safely retry unfinished targets (re-authorized)" });
      expect(Boolean(retry)).toBe(want.retry);
      expect(Boolean(screen.queryByRole("status"))).toBe(want.delivery_uncertain);
      expect(encodedWire).not.toContain("private body");
      expect(encodedWire).not.toContain("private protocol");
      expect(encodedWire).not.toContain("12345678-1234-5678-9012-123456789012");
      expect(document.body).not.toHaveTextContent("private body");
      expect(document.body).not.toHaveTextContent("private protocol");
      expect(document.body).not.toHaveTextContent("12345678-1234-5678-9012-123456789012");
      // The normal accepted UI must never assert final delivery.
      expect(document.body).not.toHaveTextContent("Delivered");
    });
  }
});
