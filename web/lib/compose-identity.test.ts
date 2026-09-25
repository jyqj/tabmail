import { describe, expect, it } from "vitest";
import { composeIdentity } from "./compose-identity";
import type { WorkMailbox } from "./company";

const box = (id: string, can_send: boolean, template_only = false): WorkMailbox => ({
  mailbox: { id }, can_send, template_only,
} as WorkMailbox);

describe("composeIdentity", () => {
  it("preserves the current authorized sender", () => {
    const current = box("current", true, true);
    expect(composeIdentity([current, box("free", true)], current)).toBe(current);
  });
  it("prefers free-form fallback but still supports template-only senders", () => {
    const read = box("read", false), template = box("template", true, true), free = box("free", true);
    expect(composeIdentity([read, template, free], read)).toBe(free);
    expect(composeIdentity([read, template], read)).toBe(template);
  });
  it("does not trust a stale preferred object after its rights change", () => {
    const stale = box("read", true), current = box("read", false), fallback = box("template", true, true);
    expect(composeIdentity([current, fallback], stale)).toBe(fallback);
  });
  it("reports no sender instead of choosing a read-only mailbox", () => {
    expect(composeIdentity([box("read", false)])).toBeUndefined();
    expect(composeIdentity([], box("gone", true))).toBeUndefined();
  });
});
