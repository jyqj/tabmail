import React from "react";
import { createHash } from "node:crypto";
import { execFileSync } from "node:child_process";
import { mkdtempSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { resolve } from "node:path";
import { cleanup, render, screen } from "@testing-library/react";
import { afterAll, afterEach, beforeAll, describe, expect, it, vi } from "vitest";
import type { Submission } from "@/lib/company";
import { SubmissionPane } from "@/features/mail/components/submission-pane";

const { api } = vi.hoisted(() => ({ api: vi.fn() }));
// Only transport is injected. The receipt is the actual Go production-policy
// output, never a value reconstructed from expected status/capabilities.
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
let observations: { case_sha256: string; receipts: Record<string, Submission> };

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
      const receipt = observations.receipts[c.id];
      expect(receipt).toBeDefined();
      // Directly compare the backend's real projection with the same JSON
      // target before handing it to the actual shipping component.
      expect(receipt.status).toBe(want.status);
      expect(receipt.recipients.map((r) => r.address)).toEqual(want.visible_addresses);
      expect(receipt.capabilities?.retry).toBe(want.retry);
      api.mockReturnValue({ data: receipt, error: undefined, mutate: vi.fn(), isLoading: false });
      render(<SubmissionPane id={receipt.id} />);
      expect(screen.getByText(want.label_en)).toBeInTheDocument();
      const table = screen.getByRole("table");
      for (const address of want.visible_addresses) expect(table).toHaveTextContent(address);
      for (const address of want.hidden_addresses) expect(document.body).not.toHaveTextContent(address);
      const retry = screen.queryByRole("button", { name: "Retry unfinished recipients (re-authorized by server)" });
      expect(Boolean(retry)).toBe(want.retry);
      expect(Boolean(screen.queryByRole("status"))).toBe(want.delivery_uncertain);
      expect(document.body).not.toHaveTextContent("private body");
      expect(document.body).not.toHaveTextContent("private protocol");
      expect(document.body).not.toHaveTextContent("12345678-1234-5678-9012-123456789012");
      // The normal accepted UI must never assert final delivery.
      expect(document.body).not.toHaveTextContent("Delivered");
    });
  }
});
