import React from "react";
import { readFileSync } from "node:fs";
import { render, waitFor } from "@testing-library/react";
import { expect, test, vi } from "vitest";
import { useCompanyEventConsumer } from "@/features/company/company-event-consumer";
import { installSession } from "@/lib/session";
import type { AuthUser } from "@/lib/types";
import { createR5FetchObserver } from "./r5-streaming-fetch-observer";

const path = process.env.TABMAIL_PE_INDEPENDENT_FIXTURE;
if (!path) throw new Error("Explicit Go-owned fixture required");
const fixture = JSON.parse(readFileSync(path, "utf8")) as { api_url: string; token: string; user: AuthUser };
function Consumer() {
  useCompanyEventConsumer({ tenantId: fixture.user.tenant_id!, enabled: true, cacheKeys: [],
    onInvalidate() {}, async revalidate() {}, onRevoked() {} });
  return null;
}
test("shipping event consumer unmount aborts actual SSE before observer closure", async () => {
  process.env.NEXT_PUBLIC_API_URL = fixture.api_url;
  installSession(fixture.token, fixture.user);
  const calls: Parameters<typeof createR5FetchObserver>[2] = [];
  const observer = createR5FetchObserver(globalThis.fetch.bind(globalThis), fixture.api_url, calls);
  vi.stubGlobal("fetch", observer.fetch);
  const view = render(<Consumer />);
  try {
    await waitFor(() => expect(observer.streams.some(stream => stream.frames.some(frame =>
      frame.event === "ready" && frame.tenant_id === fixture.user.tenant_id))).toBe(true), { timeout: 5000 });
    view.unmount();
    // This assertion precedes close(): owner cleanup cannot disguise a missing
    // shipping React effect cancellation.
    await waitFor(() => expect(observer.streams.every(stream => stream.aborted && !stream.error)).toBe(true));
    expect(calls.every(call => call.method === "GET")).toBe(true);
    await observer.close();
  } finally {
    view.unmount(); await observer.close(); vi.unstubAllGlobals(); delete process.env.NEXT_PUBLIC_API_URL;
  }
});
