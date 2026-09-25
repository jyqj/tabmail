import { describe, it, expect, vi } from "vitest";
import { DraftWriter, draftKey, type DraftTransport } from "./draft-writer";
import type { MailDraft } from "@/lib/company";
const initial: MailDraft = { mailbox_id: "box", revision: 0, payload: { to: [], subject: "first", text_body: "draft" } };
const input = (subject: string) => ({ mailbox_id: "box", payload: { ...initial.payload, subject } });
function deferred<T>() { let resolve!: (v: T) => void; let reject!: (v: unknown) => void; const promise = new Promise<T>((a, b) => { resolve = a; reject = b; }); return { promise, resolve, reject }; }
describe("DraftWriter persistence lane", () => {
    it("serializes autosave and manual flush using acknowledged revisions", async () => {
        const gate = deferred<MailDraft>();
        const write = vi.fn().mockImplementationOnce(() => gate.promise).mockImplementationOnce(async (d: MailDraft) => ({ ...d, revision: d.revision + 1 }));
        const lane = new DraftWriter(initial, { write, read: vi.fn() });
        const a = lane.save(input("a"));
        const b = lane.save(input("b"));
        await Promise.resolve();
        await Promise.resolve();
        expect(write).toHaveBeenCalledTimes(1);
        expect(write.mock.calls[0][1]).toBe(true);
        gate.resolve({ ...initial, id: lane.id, payload: input("a").payload, revision: 1 });
        await a;
        const last = await b;
        expect(write.mock.calls[1][0].revision).toBe(1);
        expect(write.mock.calls[1][1]).toBe(false);
        expect(last.payload.subject).toBe("b");
        expect(last.revision).toBe(2);
    });
    it("recovers lost initial acknowledgement by exact UUID readback", async () => {
        const write = vi.fn().mockRejectedValue(new Error("connection lost"));
        let lane: DraftWriter;
        const read = vi.fn(async () => ({ ...initial, id: lane.id, revision: 1, payload: input("a").payload }));
        lane = new DraftWriter(initial, { write, read });
        const value = await lane.save(input("a"));
        expect(value.revision).toBe(1);
        expect(read).toHaveBeenCalledWith(lane.id);
        await lane.save(input("a"));
        expect(write).toHaveBeenCalledTimes(1);
    });
    it("does not overwrite a different readback after an uncertain response", async () => {
        const write = vi.fn().mockRejectedValue(new Error("lost"));
        const read = vi.fn(async () => ({ ...initial, id: "other", revision: 2, payload: input("someone else").payload }));
        const lane = new DraftWriter(initial, { write, read });
        await expect(lane.save(input("a"))).rejects.toThrow("lost");
        expect(write).toHaveBeenCalledTimes(1);
    });
    it("latches conflicts until explicit reload and keeps all queued writes off the server", async () => {
        const conflict = { error: { code: "CONFLICT" } };
        const write = vi.fn().mockRejectedValue(conflict);
        const read = vi.fn(async () => ({ ...initial, id: "saved", revision: 4, payload: input("server").payload }));
        const lane = new DraftWriter({ ...initial, id: "saved", revision: 1 }, { write, read });
        await expect(lane.save(input("a"))).rejects.toBe(conflict);
        await expect(lane.save(input("b"))).rejects.toBe(conflict);
        expect(write).toHaveBeenCalledTimes(1);
        await lane.reload();
        write.mockResolvedValue({ ...initial, id: "saved", revision: 5 });
        await lane.save(input("c"));
        expect(write.mock.calls[1][0].revision).toBe(4);
    });
    it("rejects queued work after account switch", async () => {
        let allowed = true;
        const gate = deferred<MailDraft>();
        const write = vi.fn(() => gate.promise);
        const guard = () => { if (!allowed)
            throw new Error("session changed"); };
        const lane = new DraftWriter(initial, { write, read: vi.fn() }, guard);
        const a = lane.save(input("a"));
        const b = lane.save(input("b"));
        const rejectedA = expect(a).rejects.toThrow("session changed");
        const rejectedB = expect(b).rejects.toThrow("session changed");
        await Promise.resolve();
        await Promise.resolve();
        allowed = false;
        gate.resolve({ ...initial, revision: 1 });
        await rejectedA;
        await rejectedB;
        expect(write).toHaveBeenCalledTimes(1);
    });
    it("ignores empty optional wire fields and key ordering, not actual edits", () => {
        expect(draftKey({ ...initial, payload: { ...initial.payload, cc: [], bcc: [], headers: {}, html_body: "" } })).toBe(draftKey(initial));
        expect(draftKey(input("changed"))).not.toBe(draftKey(initial));
    });
});
