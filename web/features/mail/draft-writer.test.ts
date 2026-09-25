import { describe, it, expect, vi } from "vitest";
import { DraftWriter, draftKey } from "./draft-writer";
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
        const read = vi.fn();
        const lane = new DraftWriter(initial, { write, read });
        read.mockResolvedValue({ ...initial, id: lane.id, revision: 1, payload: input("a").payload });
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
        await expect(lane.save(input("a"))).rejects.toMatchObject({ error: { code: "CONFLICT" } });
        await expect(lane.save(input("b"))).rejects.toMatchObject({ error: { code: "CONFLICT" } });
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
        write.mockImplementation(async (d: MailDraft) => ({ ...d, revision: d.revision + 1 }));
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


describe("Unknown draft write outcomes", () => {
    it("replays the original creation before saving new typing after both responses were lost", async () => {
        const write = vi.fn().mockRejectedValueOnce(new Error("write response lost"))
            .mockImplementation(async (d: MailDraft) => ({ ...d, revision: d.revision + 1 }));
        const read = vi.fn().mockRejectedValue(new Error("readback unavailable"));
        const lane = new DraftWriter(initial, { write, read });
        await expect(lane.save(input("a"))).rejects.toThrow("write response lost");
        const last = await lane.save(input("b"));
        expect(write).toHaveBeenCalledTimes(3);
        expect(write.mock.calls[1]).toEqual(write.mock.calls[0]);
        expect(write.mock.calls[2][1]).toBe(false);
        expect(write.mock.calls[2][0].revision).toBe(1);
        expect(last.payload.subject).toBe("b");
        expect(last.revision).toBe(2);
    });
    it("recovers a committed update on retry conflict without treating later typing as its input", async () => {
        const lost = new Error("lost acknowledgement");
        const write = vi.fn().mockRejectedValueOnce(lost)
            .mockRejectedValueOnce({ error: { code: "CONFLICT" } })
            .mockImplementation(async (d: MailDraft) => ({ ...d, revision: d.revision + 1 }));
        const read = vi.fn().mockRejectedValueOnce(new Error("offline"))
            .mockResolvedValue({ ...initial, id: "saved", revision: 4, payload: input("a").payload });
        const lane = new DraftWriter({ ...initial, id: "saved", revision: 3 }, { write, read });
        await expect(lane.save(input("a"))).rejects.toBe(lost);
        const b = await lane.save(input("b"));
        expect(write.mock.calls[0]).toEqual(write.mock.calls[1]);
        expect(write.mock.calls[2][0].revision).toBe(4);
        expect(b.revision).toBe(5);
        expect(b.payload.subject).toBe("b");
    });
    it("does not adopt an unrelated later revision even if its text happens to match", async () => {
        const write = vi.fn().mockRejectedValue(new Error("lost"));
        const read = vi.fn().mockResolvedValue({ ...initial, id: "saved", revision: 7, payload: input("a").payload });
        const lane = new DraftWriter({ ...initial, id: "saved", revision: 3 }, { write, read });
        await expect(lane.save(input("a"))).rejects.toMatchObject({ error: { code: "CONFLICT" } });
        await expect(lane.save(input("b"))).rejects.toMatchObject({ error: { code: "CONFLICT" } });
        expect(write).toHaveBeenCalledTimes(1);
    });
});

it("keeps an uncertain replay retryable when the conflict readback is also offline", async () => {
    const conflict = { error: { code: "CONFLICT" } };
    const write = vi.fn().mockRejectedValueOnce(new Error("offline"))
        .mockRejectedValueOnce(conflict).mockRejectedValueOnce(conflict)
        .mockImplementation(async (d: MailDraft) => ({ ...d, revision: d.revision + 1 }));
    const read = vi.fn().mockRejectedValueOnce(new Error("offline"))
        .mockRejectedValueOnce(new Error("offline"))
        .mockResolvedValue({ ...initial, id: "saved", revision: 4, payload: input("a").payload });
    const lane = new DraftWriter({ ...initial, id: "saved", revision: 3 }, { write, read });
    await expect(lane.save(input("a"))).rejects.toThrow("offline");
    await expect(lane.save(input("b"))).rejects.toThrow("still unconfirmed");
    const last = await lane.save(input("c"));
    expect(write.mock.calls[0]).toEqual(write.mock.calls[1]);
    expect(write.mock.calls[0]).toEqual(write.mock.calls[2]);
    expect(last.revision).toBe(5);
    expect(last.payload.subject).toBe("c");
});
