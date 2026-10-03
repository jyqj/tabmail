import { describe, expect, it } from "vitest";
import type { ReceiptCapabilities } from "@/lib/receipt-types";
import { expectedReceiptCapabilities, receiptCapabilitiesMatch, type ReceiptSurface } from "./r5-receipt-contract-oracle";

// Separate new UNIT evidence. All expectations are synthetic seed/case values;
// no HTTP/PG fixture, response-derived expectation or shipping runtime credit.
const list: ReceiptCapabilities = { view_content: false, retry: false, retry_block_reason: "unknown" };
const nonRetryable: ReceiptCapabilities = { view_content: false, retry: false, retry_block_reason: "state_not_retryable" };
const retryable: ReceiptCapabilities = { view_content: false, retry: true, retry_block_reason: "" };
const currentCases: ReceiptCapabilities[] = [nonRetryable, retryable,
  { view_content: false, retry: false, retry_block_reason: "delivery_uncertain" },
  { view_content: false, retry: false, retry_block_reason: "sender_authority" },
  { view_content: false, retry: false, retry_block_reason: "unknown" }];
const surfaces: ReceiptSurface[] = ["list", "detail", "replay"];

describe("RC receipt surface oracle unit: exact seed-owned authority", () => {
  it.each(currentCases)("list stays conservative for current case %#", current => {
    expect(expectedReceiptCapabilities("list", current)).toEqual(list);
    expect(receiptCapabilitiesMatch(list, expectedReceiptCapabilities("list", current))).toBe(true);
  });
  it.each(["detail", "replay"] as const)("%s retains every original exact capability", surface => {
    for (const current of currentCases) {
      expect(expectedReceiptCapabilities(surface, current)).toEqual(current);
      expect(receiptCapabilitiesMatch(current, expectedReceiptCapabilities(surface, current))).toBe(true);
      for (const wrong of currentCases.filter(candidate => candidate !== current)) {
        expect(receiptCapabilitiesMatch(wrong, expectedReceiptCapabilities(surface, current))).toBe(false);
      }
    }
  });
  it.each(["detail", "replay"] as const)("rejects list/%s swapped non-retryable authority", surface => {
    expect(receiptCapabilitiesMatch(nonRetryable, expectedReceiptCapabilities("list", nonRetryable))).toBe(false);
    expect(receiptCapabilitiesMatch(list, expectedReceiptCapabilities(surface, nonRetryable))).toBe(false);
  });
  it.each(["detail", "replay"] as const)("rejects list/%s swapped retryable authority", surface => {
    expect(receiptCapabilitiesMatch(retryable, expectedReceiptCapabilities("list", retryable))).toBe(false);
    expect(receiptCapabilitiesMatch(list, expectedReceiptCapabilities(surface, retryable))).toBe(false);
  });
  it("rejects list unknown changed to state_not_retryable or retry true", () => {
    const expected = expectedReceiptCapabilities("list", nonRetryable);
    for (const wrong of [nonRetryable, { ...list, retry: true }, retryable]) {
      expect(receiptCapabilitiesMatch(wrong, expected)).toBe(false);
    }
  });
  it.each(surfaces)("%s preserves the seeded content decision", surface => {
    for (const view_content of [false, true]) {
      const expected = expectedReceiptCapabilities(surface, { ...nonRetryable, view_content });
      expect(expected.view_content).toBe(view_content);
      expect(receiptCapabilitiesMatch({ ...expected, view_content: !view_content }, expected)).toBe(false);
    }
  });
  it.each(surfaces)("%s requires all and only the three capability fields", surface => {
    const expected = expectedReceiptCapabilities(surface, nonRetryable);
    for (const field of Object.keys(expected)) {
      const missing = { ...expected };
      Reflect.deleteProperty(missing, field);
      expect(receiptCapabilitiesMatch(missing, expected)).toBe(false);
    }
    expect(receiptCapabilitiesMatch({ ...expected, sender_id: "private" }, expected)).toBe(false);
    expect(receiptCapabilitiesMatch(undefined, expected)).toBe(false);
  });
  it("fails closed without an explicit recognized surface", () => {
    expect(() => expectedReceiptCapabilities(undefined as unknown as ReceiptSurface, nonRetryable)).toThrow("Explicit receipt surface required");
    expect(() => expectedReceiptCapabilities("other" as ReceiptSurface, nonRetryable)).toThrow("Explicit receipt surface required");
  });
});
