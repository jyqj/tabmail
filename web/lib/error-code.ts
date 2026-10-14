// AR09: the single home for reading error codes off rejected API responses.
// The transport throws plain APIError objects ({ error: { code, message,
// reason? } }) from lib/api/base, but catch sites must also survive Error
// instances, strings and arbitrary junk thrown across the boundary, so every
// read is defensive about the shape it unwraps.

export interface ApiErrorShape {
  error?: {
    code?: unknown;
    message?: unknown;
    reason?: unknown;
  };
}

function errorPayload(e: unknown): ApiErrorShape["error"] | undefined {
  if (typeof e !== "object" || e === null || !("error" in e)) return undefined;
  const payload = (e as ApiErrorShape).error;
  return typeof payload === "object" && payload !== null ? payload : undefined;
}

export function errorCode(e: unknown): string | undefined {
  const code = errorPayload(e)?.code;
  return typeof code === "string" && code ? code : undefined;
}

export function errorReason(e: unknown): string | undefined {
  const reason = errorPayload(e)?.reason;
  return typeof reason === "string" && reason ? reason : undefined;
}

export function isConflict(e: unknown): boolean {
  return errorCode(e) === "CONFLICT";
}

// Deterministic client-visible outcomes (HTTP 4xx semantics): retrying the
// same request cannot succeed, so callers latch/hide retry affordances.
// Mirrors the draft lane's latching list in features/mail/draft-writer.
export const DETERMINISTIC_ERROR_CODES = [
  "CONFLICT",
  "FORBIDDEN",
  "NOT_FOUND",
  "UNAUTHORIZED",
] as const;
export type DeterministicErrorCode = (typeof DETERMINISTIC_ERROR_CODES)[number];

export function isDeterministicErrorCode(
  code: string | undefined | null,
): code is DeterministicErrorCode {
  return !!code && (DETERMINISTIC_ERROR_CODES as readonly string[]).includes(code);
}
