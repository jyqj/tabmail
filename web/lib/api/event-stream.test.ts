import { describe, expect, it } from "vitest";
import { createEventStreamParser, MAX_EVENT_FRAME_LENGTH, type EventStreamFrame } from "./event-stream";

function parse(chunks: string[]) {
  const frames: EventStreamFrame[] = [];
  const parser = createEventStreamParser(frame => frames.push(frame));
  for (const chunk of chunks) parser.push(chunk);
  return frames;
}

describe("incremental event framing", () => {
  it("is invariant under every split point, including CRLF and field names", () => {
    const wire = ": comment\r\nid: c-1\r\nevent: changed\r\ndata: one\r\ndata: two\r\n\r\nid:\rdata\r\n\r\n";
    const expected = [{ type: "changed", id: "c-1", data: "one\ntwo" }, { type: "message", id: "", data: "" }];
    for (let index = 0; index <= wire.length; index++) {
      expect(parse([wire.slice(0, index), "", wire.slice(index)])).toEqual(expected);
    }
    expect(parse(Array.from(wire))).toEqual(expected);
  });

  it("keeps one-space field semantics, case sensitivity and all remaining colons", () => {
    expect(parse(["Data: ignored\n: comment\nretry: 0\nunknown: x\nevent:  custom \nid:  cursor \ndata:  a:b\ndata:\tvalue\n\n"])).toEqual([
      { type: " custom ", id: " cursor ", data: " a:b\n\tvalue" },
    ]);
  });

  it("ignores null IDs, keeps valid earlier ID and resets per-frame event/data fields", () => {
    expect(parse(["id: valid\nid: invalid\0id\nevent: custom\n\ndata: next\n\nid\n\nevent:\ndata\n\n"])).toEqual([
      { type: "custom", id: "valid" }, { type: "message", data: "next" },
      { type: "message", id: "" }, { type: "message", data: "" },
    ]);
  });

  it("does not emit unfinished data or IDs", () => {
    expect(parse(["data: complete\n\nid: next\ndata: unfinished\n"])).toEqual([{ type: "message", data: "complete" }]);
  });

  it("counts a frame across line and chunk boundaries", () => {
    const parser = createEventStreamParser(() => {});
    parser.push(":" + "x".repeat(MAX_EVENT_FRAME_LENGTH - 2));
    parser.push("\n");
    expect(() => parser.push("x")).toThrow("Oversized event stream frame");
  });

  it("accepts an exact-bound frame, resets its budget and counts CRLF only once", () => {
    const wire = "data:" + "x".repeat(MAX_EVENT_FRAME_LENGTH - 7) + "\r\n\r\n";
    const frames = parse([wire, wire]);
    expect(frames).toHaveLength(2);
    expect(frames[0].data).toHaveLength(MAX_EVENT_FRAME_LENGTH - 7);
  });

  it.each(["data:", ":", "unknown:"])("bounds oversized %s lines before buffering the entire input", field => {
    expect(() => parse([field + "x".repeat(MAX_EVENT_FRAME_LENGTH)])).toThrow("Oversized event stream frame");
  });

  it("stops delivering frames when the consumer throws", () => {
    const received: EventStreamFrame[] = [];
    const parser = createEventStreamParser(frame => { received.push(frame); throw new Error("stop"); });
    expect(() => parser.push("data: first\n\ndata: second\n\n")).toThrow("stop");
    expect(received).toEqual([{ type: "message", data: "first" }]);
  });
});
