import { describe, expect, it } from "vitest";
import { receiptCapabilitiesMatch, receiptCountsMatch } from "./r5-receipt-contract-oracle";

// New UNIT evidence only: synthetic objects, no HTTP, PG or shipping component run.
const counts = { total: 3, accepted: 1, pending: 1, temporary: 0, permanent: 1, uncertain: 0 };
const capabilities = { view_content: false, retry: true, retry_block_reason: "" as const };
function reordered(value: object) { return Object.fromEntries(Object.entries(value).reverse()); }
function without(value: object, key: string) { return Object.fromEntries(Object.entries(value).filter(([name]) => name !== key)); }

describe("RC acceptance oracle unit: closed fields independent of JSON order", () => {
  it("accepts reordered legal count fields", () => {
    expect(receiptCountsMatch(reordered(counts), counts)).toBe(true);
    expect(receiptCountsMatch(counts, reordered(counts) as typeof counts)).toBe(true);
  });
  it("accepts reordered legal capability fields", () => {
    expect(receiptCapabilitiesMatch(reordered(capabilities), capabilities)).toBe(true);
    expect(receiptCapabilitiesMatch(capabilities, reordered(capabilities) as typeof capabilities)).toBe(true);
  });
  it.each(Object.keys(counts))("rejects missing count field %s", key => {
    expect(receiptCountsMatch(without(counts, key), counts)).toBe(false);
  });
  it.each(Object.keys(counts))("rejects wrong count value %s", key => {
    expect(receiptCountsMatch({ ...counts, [key]: counts[key as keyof typeof counts] + 1 }, counts)).toBe(false);
  });
  it("rejects extra count fields", () => {
    expect(receiptCountsMatch({ ...counts, extra: 0 }, counts)).toBe(false);
  });
  it.each(Object.keys(capabilities))("rejects missing capability field %s", key => {
    expect(receiptCapabilitiesMatch(without(capabilities, key), capabilities)).toBe(false);
  });
  it.each(Object.keys(capabilities))("rejects wrong capability value %s", key => {
    const original = capabilities[key as keyof typeof capabilities];
    expect(receiptCapabilitiesMatch({ ...capabilities, [key]: typeof original === "boolean" ? !original : "unknown" }, capabilities)).toBe(false);
  });
  it("rejects extra capability fields", () => {
    expect(receiptCapabilitiesMatch({ ...capabilities, extra: false }, capabilities)).toBe(false);
  });
});
