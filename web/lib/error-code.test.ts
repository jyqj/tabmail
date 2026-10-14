import { describe, expect, it } from "vitest";
import {
  DETERMINISTIC_ERROR_CODES,
  errorCode,
  errorReason,
  isConflict,
  isDeterministicErrorCode,
} from "./error-code";

// The exact envelope lib/api/base throws on non-2xx responses.
const apiError = (code: string, extra: Record<string, unknown> = {}) => ({
  error: { code, message: "boom", ...extra },
});

describe("errorCode", () => {
  it("reads the APIError envelope thrown by lib/api/base", () => {
    expect(errorCode(apiError("CONFLICT"))).toBe("CONFLICT");
    expect(errorCode(apiError("QUOTA_EXCEEDED"))).toBe("QUOTA_EXCEEDED");
  });

  it("returns undefined for every non-APIError thrown shape", () => {
    expect(errorCode(undefined)).toBeUndefined();
    expect(errorCode(null)).toBeUndefined();
    expect(errorCode("boom")).toBeUndefined();
    expect(errorCode(42)).toBeUndefined();
    expect(errorCode(new Error("boom"))).toBeUndefined();
    expect(errorCode({})).toBeUndefined();
    expect(errorCode([])).toBeUndefined();
    expect(errorCode({ error: null })).toBeUndefined();
    expect(errorCode({ error: "CONFLICT" })).toBeUndefined();
    expect(errorCode({ error: { code: 5 } })).toBeUndefined();
    expect(errorCode({ error: { code: "" } })).toBeUndefined();
  });
});

describe("errorReason", () => {
  it("reads the machine-readable qualifier", () => {
    expect(errorReason(apiError("CONFLICT", { reason: "state_changed" }))).toBe(
      "state_changed",
    );
    expect(errorReason(apiError("CONFLICT"))).toBeUndefined();
    expect(errorReason(new Error("boom"))).toBeUndefined();
    expect(errorReason({ error: { reason: 7 } })).toBeUndefined();
  });
});

describe("isConflict", () => {
  it("accepts the real CONFLICT envelope", () => {
    expect(isConflict({ error: { code: "CONFLICT", message: "stale revision" } })).toBe(true);
  });

  it("rejects other codes and unknown shapes", () => {
    expect(isConflict(apiError("FORBIDDEN"))).toBe(false);
    expect(isConflict(new Error("network down"))).toBe(false);
    expect(isConflict(null)).toBe(false);
    expect(isConflict({ error: { code: "CONFLICTX" } })).toBe(false);
  });
});

describe("isDeterministicErrorCode", () => {
  it("accepts exactly the four non-retryable codes", () => {
    for (const code of DETERMINISTIC_ERROR_CODES) {
      expect(isDeterministicErrorCode(code)).toBe(true);
    }
  });

  it("rejects retryable and missing codes", () => {
    expect(isDeterministicErrorCode("BAD_REQUEST")).toBe(false);
    expect(isDeterministicErrorCode("QUOTA_EXCEEDED")).toBe(false);
    expect(isDeterministicErrorCode("INTERNAL_ERROR")).toBe(false);
    expect(isDeterministicErrorCode(undefined)).toBe(false);
    expect(isDeterministicErrorCode("")).toBe(false);
  });
});
