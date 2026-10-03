// RC acceptance-only oracle. Returns a boolean so failures never dump wire data.
import type { ReceiptCapabilities, ReceiptCounts } from "@/lib/receipt-types";

function exactFieldsMatch(actual: unknown, expected: unknown, keys: readonly string[]): boolean {
  function closedRecord(value: unknown): value is Record<string, unknown> {
    return value !== null && typeof value === "object" && !Array.isArray(value) &&
      Object.keys(value).length === keys.length && keys.every(key => Object.hasOwn(value, key));
  }
  return closedRecord(actual) && closedRecord(expected) &&
    keys.every(key => Object.is(actual[key], expected[key]));
}

export function receiptCountsMatch(actual: unknown, expected: ReceiptCounts): boolean {
  return exactFieldsMatch(actual, expected, ["total", "accepted", "pending", "temporary", "permanent", "uncertain"]);
}

export function receiptCapabilitiesMatch(actual: unknown, expected: ReceiptCapabilities): boolean {
  return exactFieldsMatch(actual, expected, ["view_content", "retry", "retry_block_reason"]);
}
