import { beforeEach, afterEach, describe, expect, it, vi } from "vitest";
import {
  canRetryInspection,
  inspectIngress,
  IngressRequestError,
  listIngress,
  retryIngress,
} from "./ingress-recovery";
import {
  deferred,
  receipt,
  session,
  storeSession,
  listed,
} from "@/test/ingress-fixtures";
const respond = (value: unknown, status = 200) =>
  new Response(JSON.stringify(value), {
    status,
    headers: { "Content-Type": "application/json" },
  });
let fetchMock: ReturnType<typeof vi.fn>;
beforeEach(() => {
  storeSession();
  fetchMock = vi.fn();
  vi.stubGlobal("fetch", fetchMock);
});
afterEach(() => vi.unstubAllGlobals());
describe("captured-session ingress transport", () => {
  it("encodes filters, captures scope, disables caches and propagates cancellation", async () => {
    fetchMock.mockResolvedValue(respond(listed()));
    const signal = new AbortController().signal;
    await listIngress(
      session,
      {
        page: 2,
        per_page: 30,
        recipient: "alice+tag@example.test",
        state: "dead",
      },
      signal,
    );
    const [url, opts] = fetchMock.mock.calls[0];
    const query = new URL(String(url), "http://localhost").searchParams;
    expect(query.get("recipient")).toBe("alice+tag@example.test");
    expect(query.get("page")).toBe("2");
    expect(opts).toMatchObject({
      method: "GET",
      cache: "no-store",
      credentials: "omit",
      signal,
      headers: {
        Authorization: "Bearer operator-token-a",
        "X-Tenant-ID": "tenant-a",
      },
    });
  });
  it("posts only the reviewed revision and reason, preserving server identity", async () => {
    fetchMock.mockResolvedValue(respond({ data: { requeued: true } }));
    await retryIngress(
      session,
      receipt,
      " repaired ",
      new AbortController().signal,
    );
    expect(JSON.parse(fetchMock.mock.calls[0][1].body)).toEqual({
      reason: "repaired",
      observed_updated_at: receipt.updated_at,
    });
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });
  it("never automatically retries or refreshes an unauthorized mutation", async () => {
    localStorage.setItem("tabmail_refresh_token", "must-not-be-used");
    fetchMock.mockResolvedValue(
      respond({ error: { message: "Expired" } }, 401),
    );
    await expect(
      retryIngress(session, receipt, "reason", new AbortController().signal),
    ).rejects.toMatchObject({ status: 401 });
    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(localStorage.getItem("tabmail_refresh_token")).toBe(
      "must-not-be-used",
    );
  });
  it("discards late data after account replacement, without changing the replacement credentials", async () => {
    const later = deferred<Response>();
    fetchMock.mockReturnValue(later.promise);
    const request = inspectIngress(
      session,
      receipt.id,
      new AbortController().signal,
    );
    storeSession({ ...session, userId: "other", accessToken: "other-token" });
    later.resolve(respond({ data: receipt }));
    await expect(request).rejects.toMatchObject({ status: 401 });
    expect(localStorage.getItem("tabmail_access_token")).toBe("other-token");
  });
  it("rejects stale credentials before making a request", async () => {
    localStorage.removeItem("tabmail_access_token");
    await expect(
      inspectIngress(session, receipt.id, new AbortController().signal),
    ).rejects.toBeInstanceOf(IngressRequestError);
    expect(fetchMock).not.toHaveBeenCalled();
  });
  it("rejects malformed/foreign snapshots", async () => {
    fetchMock.mockResolvedValue(
      respond({ data: { ...receipt, id: "another" } }),
    );
    await expect(
      inspectIngress(session, receipt.id, new AbortController().signal),
    ).rejects.toMatchObject({ status: 502 });
    fetchMock.mockResolvedValue(respond({ data: [], meta: { total: -1 } }));
    await expect(
      listIngress(
        session,
        { page: 1, per_page: 30 },
        new AbortController().signal,
      ),
    ).rejects.toMatchObject({ status: 502 });
  });
  it("requires the server's retry decision and coherent target states", () => {
    expect(canRetryInspection(receipt)).toBe(true);
    for (const partial of [
      { can_retry: false },
      { recovery_managed: false },
      { state: "processing" },
      { retry_block_reason: "legacy_receipt" },
      { updated_at: "invalid" },
      { targets: [] },
      { targets: [{ ...receipt.targets[1], job_id: "other" }] },
    ])
      expect(canRetryInspection({ ...receipt, ...partial })).toBe(false);
  });
  it("validates Unicode reasons by code point before sending", async () => {
    fetchMock.mockResolvedValue(respond({ data: { requeued: true } }));
    await retryIngress(
      session,
      receipt,
      "🔧".repeat(2000),
      new AbortController().signal,
    );
    for (const reason of [" ", "🔧".repeat(2001)])
      expect(() =>
        retryIngress(session, receipt, reason, new AbortController().signal),
      ).toThrow();
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });
});
