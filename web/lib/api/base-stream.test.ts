import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { advanceSession } from "../session";
import { streamEvents } from "./base";

const encoder = new TextEncoder();
const flush = () => vi.advanceTimersByTimeAsync(0);
const event = (data: unknown, type = "message") => ({ type, data });

function response(chunks: Uint8Array[]) {
  return new Response(new ReadableStream<Uint8Array>({
    start(controller) {
      for (const chunk of chunks) controller.enqueue(chunk);
      controller.close();
    },
  }), { headers: { "Content-Type": "text/event-stream" } });
}

beforeEach(() => vi.useFakeTimers());
afterEach(() => { vi.useRealTimers(); vi.unstubAllGlobals(); });

async function capture(chunks: Uint8Array[]) {
  const fetcher = vi.fn(async () => response(chunks));
  vi.stubGlobal("fetch", fetcher);
  const abort = new AbortController();
  const onEvent = vi.fn();
  const running = streamEvents("/events", { signal: abort.signal, onEvent }).catch(error => error);
  await flush();
  abort.abort();
  await running;
  return onEvent.mock.calls.map(call => call[0]);
}

describe("streamEvents framing over actual ReadableStream chunks", () => {
  it.each(["\n", "\r\n", "\r"])("decodes one-byte UTF-8 chunks and %j line endings", async newline => {
    const bytes = encoder.encode(`id: cursor-1${newline}event: mailbox.changed${newline}data: {"label":"邮件🙂"}${newline}${newline}`);
    expect(await capture(Array.from(bytes, byte => Uint8Array.of(byte)))).toEqual([
      event(null, "resync"), event({ label: "邮件🙂" }, "mailbox.changed"),
    ]);
  });

  it("dispatches many small frames in a chunk larger than the single-frame bound", async () => {
    const count = 300;
    const payload = "a".repeat(4096);
    const frames = await capture([encoder.encode(`data: ${payload}\n\n`.repeat(count))]);
    expect(frames).toHaveLength(count + 1);
    expect(frames.slice(1)).toEqual(Array.from({ length: count }, () => event(payload)));
  });

  it("preserves raw whitespace and empty data fields instead of trimming payloads", async () => {
    expect(await capture([encoder.encode("event:\ndata:  indented\ndata:\ndata:\tkept\n\ndata\n\n")])).toEqual([
      event(null, "resync"), event(" indented\n\n\tkept"), event(""),
    ]);
  });

  it.each(["id: next\r\n\r\n", "id: next\n\n"])("reconnects with a completed cursor after %j", async wire => {
    const fetcher = vi.fn<typeof fetch>(async () => response([encoder.encode(wire)]));
    vi.stubGlobal("fetch", fetcher);
    const abort = new AbortController(), onEvent = vi.fn();
    const running = streamEvents("/events", { signal: abort.signal, onEvent }).catch(error => error);
    await flush();
    await vi.advanceTimersByTimeAsync(1000);
    expect(fetcher).toHaveBeenCalledTimes(2);
    expect(new Headers(fetcher.mock.calls[1]?.[1]?.headers).get("Last-Event-ID")).toBe("next");
    expect(onEvent.mock.calls.map(call => call[0])).toEqual([event(null, "resync"), event(null, "resync")]);
    abort.abort(); await running;
  });

  it.each([
    ["id: committed\n\ndata: ignored partial\nid: unfinished", "committed"],
    ["id: committed\n\nid: bad\0id\ndata: ok\n\n", "committed"],
    ["id: committed\n\nid\n\n", null],
  ] as const)("uses only complete valid cursor updates: %j", async (wire, cursor) => {
    const fetcher = vi.fn<typeof fetch>(async () => response([encoder.encode(wire)]));
    vi.stubGlobal("fetch", fetcher);
    const abort = new AbortController();
    const running = streamEvents("/events", { signal: abort.signal, onEvent: vi.fn() }).catch(error => error);
    await flush(); await vi.advanceTimersByTimeAsync(1000);
    expect(fetcher).toHaveBeenCalledTimes(2);
    expect(new Headers(fetcher.mock.calls[1]?.[1]?.headers).get("Last-Event-ID")).toBe(cursor);
    abort.abort(); await running;
  });

  it.each(["abort", "session change"])("does not dispatch a later buffered frame after %s", async boundary => {
    vi.stubGlobal("fetch", vi.fn(async () => response([encoder.encode("data: first\n\ndata: stale\n\n")])));
    const abort = new AbortController();
    const onEvent = vi.fn((value: { type: string; data: unknown }) => {
      if (value.data === "first") {
        if (boundary === "abort") abort.abort(); else advanceSession();
      }
    });
    const running = streamEvents("/events", { signal: abort.signal, onEvent }).catch(error => error);
    await flush(); abort.abort(); await running;
    expect(onEvent.mock.calls.map(call => call[0])).toEqual([event(null, "resync"), event("first")]);
  });

  it("keeps completed events and rejects an oversized unfinished frame", async () => {
    expect(await capture([encoder.encode("data: valid\n\n" + "data: " + "x".repeat(1024 * 1024))])).toEqual([
      event(null, "resync"), event("valid"),
    ]);
  });

  it("does not dispatch an unterminated frame at EOF", async () => {
    expect(await capture([encoder.encode("data: unfinished\n")])).toEqual([event(null, "resync")]);
  });
});
