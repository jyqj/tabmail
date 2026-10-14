import { readFileSync } from "node:fs";
import { expect, test } from "vitest";
import { createR5FetchObserver, type R5ObservedCall } from "./r5-streaming-fetch-observer";

// Invoked only by the dedicated Go probe, against its own router and database.
const path = process.env.TABMAIL_R5_TRANSPORT_FIXTURE;
if (!path) throw new Error("Explicit real transport fixture required");
const fixture = JSON.parse(readFileSync(path, "utf8")) as { api_url: string; token: string; tenant_id: string; employee_id: string };
const headers = { Authorization: `Bearer ${fixture.token}`, "Content-Type": "application/json" };
async function until(predicate: () => boolean) {
  const deadline = Date.now() + 8000;
  while (!predicate()) {
    if (Date.now() >= deadline) throw new Error("Actual wire observation timed out");
    await new Promise(resolve => setTimeout(resolve, 10));
  }
}
test("real ready/event, unchanged JSON, caller abort and close terminate readers", async () => {
  const calls: R5ObservedCall[] = [];
  const observer = createR5FetchObserver(globalThis.fetch.bind(globalThis), fixture.api_url, calls);
  const consumer = new AbortController();
  let reader: ReadableStreamDefaultReader<Uint8Array> | undefined;
  try {
    // This await must return while the shipping stream is still live.
    const response = await observer.fetch(`${fixture.api_url}/api/v1/company/events`, { headers, signal: consumer.signal });
    expect(response.status).toBe(200);
    reader = response.body!.getReader();
    const first = await reader.read();
    expect(new TextDecoder().decode(first.value)).toContain("event: ready");
    await until(() => observer.streams[0].frames.some(frame => frame.event === "ready"));
    expect(observer.streams[0].ended).toBe(false);
    expect(observer.streams[0].frames[0]).toEqual({ event: "ready", tenant_id: fixture.tenant_id });
    const editor = `/api/v1/admin/users/${fixture.employee_id}/permission-editor`;
    const before = await observer.fetch(fixture.api_url + editor, { headers });
    const snapshot = await before.json();
    expect(calls.at(-1)!.data).toEqual(snapshot);
    const command = JSON.stringify({ expected_revision: snapshot.data.revision, patch: { can_send: false } });
    const changed = await observer.fetch(fixture.api_url + editor, { method: "PATCH", headers, body: command });
    const changedPacket = await changed.json();
    expect(calls.at(-1)).toMatchObject({ method: "PATCH", path: editor, status: 200, body: JSON.parse(command), data: changedPacket });
    await until(() => observer.streams[0].frames.some(frame => frame.action === "permission.override.patch" && frame.resource_id === fixture.employee_id));
    expect(observer.streams[0].error).toBeUndefined();
    consumer.abort();
    await until(() => observer.streams[0].aborted);
    // Reconnect delivers shipping ready again; leave its original tee unread.
    await observer.fetch(`${fixture.api_url}/api/v1/company/events`, { headers });
    await until(() => observer.streams[1].frames.some(frame => frame.event === "ready"));
    await observer.close();
    expect(observer.streams.every(stream => stream.aborted && !stream.error)).toBe(true);
    expect(calls.filter(call => call.path.endsWith("/events")).every(call => call.status === 200 && call.data === undefined)).toBe(true);
  } finally {
    consumer.abort();
    await observer.close();
    await reader?.cancel().catch(() => {});
    reader?.releaseLock();
  }
});
