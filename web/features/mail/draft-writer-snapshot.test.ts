import { describe, expect, it, vi } from "vitest";
import type { MailDraft, MailDraftInput } from "@/lib/company";
import { DraftWriter, draftKey, type DraftInput } from "./draft-writer";

const id = "c157fc94-5463-49d0-bd4e-ed7834b6d8e7";
function draft(revision = 0): MailDraft {
    return {
        id, revision, mailbox_id: "original-mailbox", updated_at: "2026-10-02T12:00:00Z",
        payload: {
            to: ["first@example.test", "second@example.test"], cc: ["cc@example.test"], bcc: ["bcc@example.test"],
            subject: "original subject", text_body: "original body", html_body: "<p>original</p>",
            headers: { "X-First": "one", "X-Second": "two" }, attachment_ids: ["attachment-a", "attachment-b"],
            template_version_id: "template-version", template_vars: { first: "one", second: "two" },
        },
        template_version: {
            id: "template-version", status: "usable",
            snapshot: { subject: "template", text_body: "template body", html_body: "", variables: [{ name: "first", type: "text", required: true, max_length: 10, options: ["one", "two"] }] },
        },
    };
}
function input(value: MailDraft): DraftInput {
    return structuredClone({ mailbox_id: value.mailbox_id, payload: value.payload });
}
function mutate(value: MailDraftInput) {
    value.id = "outside-id";
    value.mailbox_id = "outside-mailbox";
    value.revision = 99;
    value.payload.subject = "outside subject";
    value.payload.to.reverse();
    value.payload.cc!.push("outside-cc@example.test");
    value.payload.bcc![0] = "outside-bcc@example.test";
    value.payload.attachment_ids!.push("outside-attachment");
    value.payload.headers!["X-First"] = "outside header";
    value.payload.template_vars!.first = "outside variable";
    value.template_version!.snapshot!.variables[0].options!.push("outside option");
}
function acknowledge(command: MailDraftInput): MailDraft {
    if (!command.id) throw new Error("Draft command must carry its replay identity");
    return { ...structuredClone(command), id: command.id, revision: command.revision + 1,
        updated_at: command.updated_at ?? "2026-10-02T12:00:00Z" };
}
function deferred<T>() {
    let resolve!: (value: T) => void;
    let reject!: (reason: unknown) => void;
    const promise = new Promise<T>((a, b) => { resolve = a; reject = b; });
    return { promise, resolve, reject };
}

describe("DraftWriter owns confirmed response snapshots", () => {
    it.each(["ack", "readback", "reload"] as const)("isolates mutable %s responses and every returned copy from the revision lane", async (source) => {
        const observed = draft(source === "reload" ? 7 : 1);
        const confirmed = structuredClone(observed);
        const write = vi.fn(async (command: MailDraftInput) => acknowledge(command));
        if (source === "ack") write.mockResolvedValueOnce(observed);
        if (source === "readback") write.mockRejectedValueOnce(new Error("ack lost"));
        const read = vi.fn(async () => observed);
        const lane = new DraftWriter(draft(), { write, read });
        const returned = source === "reload" ? await lane.reload() : await lane.save(input(observed));
        expect(returned).toEqual(confirmed);
        mutate(observed);
        mutate(returned);
        expect(lane.id).toBe(id);
        const writesBeforeNoop = write.mock.calls.length;
        const noop = await lane.save(input(confirmed));
        expect(noop).toEqual(confirmed);
        expect(write).toHaveBeenCalledTimes(writesBeforeNoop);
        mutate(noop);
        const edited = input(confirmed);
        edited.payload.subject = "next local edit";
        const saved = await lane.save(edited);
        expect(write.mock.calls.at(-1)).toEqual([{ ...confirmed, ...edited }, false]);
        expect(saved).toEqual({ ...confirmed, ...edited, revision: confirmed.revision + 1 });
        expect(read).toHaveBeenCalledTimes(source === "ack" ? 0 : 1);
    });

    it("detaches constructor input and save input before the queued write starts", async () => {
        const initial = draft(3);
        const original = structuredClone(initial);
        const edited = input(initial);
        edited.payload.subject = "queued edit";
        const pinned = structuredClone(edited);
        const write = vi.fn(async (command: MailDraftInput) => acknowledge(command));
        const lane = new DraftWriter(initial, { write, read: vi.fn() });
        const saved = lane.save(edited);
        mutate(initial);
        edited.mailbox_id = "outside-input";
        edited.payload.to.push("outside@example.test");
        edited.payload.headers!["X-First"] = "outside-input";
        edited.payload.template_vars!.first = "outside-input";
        expect(await saved).toEqual({ ...original, ...pinned, revision: 4 });
        expect(write.mock.calls[0]).toEqual([{ ...original, ...pinned }, false]);
    });

    it("pins lost-response UUID/revision/payload while new typing queues, even when transport mutates its command", async () => {
        const first = deferred<MailDraft>();
        const started = deferred<void>();
        const replay = deferred<MailDraft>();
        const replayStarted = deferred<MailDraftInput>();
        const commands: Array<{ value: MailDraftInput; create: boolean }> = [];
        const write = vi.fn(async (command: MailDraftInput, create: boolean) => {
            commands.push({ value: structuredClone(command), create });
            if (commands.length === 1) { mutate(command); started.resolve(); return first.promise; }
            if (commands.length === 2) { replayStarted.resolve(structuredClone(command)); mutate(command); return replay.promise; }
            return acknowledge(command);
        });
        const read = vi.fn().mockRejectedValue(new Error("readback lost"));
        const lane = new DraftWriter(draft(), { write, read });
        const original = input(draft());
        const lost = lane.save(original);
        const lostCheck = expect(lost).rejects.toThrow("response lost");
        await started.promise;
        original.payload.subject = "not the original command";
        const newer = input(draft());
        newer.payload.subject = "new typing";
        const pinnedNewer = structuredClone(newer);
        const queued = lane.save(newer);
        newer.payload.to.push("outside-new-typing@example.test");
        newer.payload.template_vars!.first = "outside-new-typing";
        first.reject(new Error("response lost"));
        await lostCheck;
        const retried = await replayStarted.promise;
        expect(commands[1]).toEqual(commands[0]);
        expect(commands[1].value.id).toBe(id);
        expect(commands[1].value.revision).toBe(0);
        const ack = acknowledge(retried);
        replay.resolve(ack);
        const saved = await queued;
        mutate(ack);
        expect(commands[2]).toEqual({ value: { ...draft(1), ...pinnedNewer }, create: false });
        expect(saved).toEqual({ ...draft(2), ...pinnedNewer });
        expect(write).toHaveBeenCalledTimes(3);
        expect(read).toHaveBeenCalledWith(id);
    });
});

const invalidRevisions = [Number.NaN, Number.POSITIVE_INFINITY, Number.NEGATIVE_INFINITY, -1, 0, 1.5, Number.MAX_SAFE_INTEGER + 1];
describe("DraftWriter revision and acknowledgement boundaries", () => {
    it.each(invalidRevisions)("rejects reload revision %s without replacing the confirmed snapshot", async (revision) => {
        const initial = draft(3);
        const write = vi.fn(async (command: MailDraftInput) => acknowledge(command));
        const lane = new DraftWriter(initial, { write, read: vi.fn(async () => draft(revision)) });
        await expect(lane.reload()).rejects.toThrow();
        const next = input(initial);
        next.payload.subject = "local after rejected readback";
        expect(await lane.save(next)).toEqual({ ...initial, ...next, revision: 4 });
        expect(write.mock.calls[0]).toEqual([{ ...initial, ...next }, false]);
    });

    it.each(invalidRevisions.filter(revision => revision !== 0))("does not issue CAS from unrepresentable initial revision %s", async (revision) => {
        const write = vi.fn();
        const read = vi.fn();
        const lane = new DraftWriter(draft(revision), { write, read });
        await expect(lane.save(input(draft()))).rejects.toThrow();
        expect(write).not.toHaveBeenCalled();
        expect(read).not.toHaveBeenCalled();
    });

    it("accepts the last safe acknowledgement but never sends an overflowing next revision", async () => {
        const initial = draft(Number.MAX_SAFE_INTEGER - 1);
        const write = vi.fn(async (command: MailDraftInput) => acknowledge(command));
        const lane = new DraftWriter(initial, { write, read: vi.fn() });
        const next = input(initial);
        next.payload.subject = "last safe edit";
        const confirmed = await lane.save(next);
        expect(confirmed.revision).toBe(Number.MAX_SAFE_INTEGER);
        expect(await lane.save(next)).toEqual(confirmed);
        const overflow = structuredClone(next);
        overflow.payload.subject = "cannot represent next revision";
        await expect(lane.save(overflow)).rejects.toThrow();
        expect(write).toHaveBeenCalledTimes(1);
    });

    it.each([...invalidRevisions, 2])("does not adopt acknowledgement revision %s and replays the exact command after offline readback", async (revision) => {
        const observed = draft(revision);
        const write = vi.fn().mockResolvedValueOnce(observed)
            .mockImplementation(async (command: MailDraftInput) => acknowledge(command));
        const read = vi.fn().mockRejectedValue(new Error("offline readback"));
        const lane = new DraftWriter(draft(), { write, read });
        await expect(lane.save(input(draft()))).rejects.toThrow("acknowledgement");
        const next = input(draft());
        next.payload.subject = "new input after bad acknowledgement";
        const confirmed = await lane.save(next);
        expect(write.mock.calls[1]).toEqual(write.mock.calls[0]);
        expect(write.mock.calls[2][0].revision).toBe(1);
        expect(write.mock.calls[2][1]).toBe(false);
        expect(confirmed.revision).toBe(2);
        expect(confirmed.payload.subject).toBe(next.payload.subject);
    });

    it("rejects an acknowledgement for another UUID without replacing the pinned command", async () => {
        const write = vi.fn().mockResolvedValueOnce({ ...draft(1), id: "another-draft" })
            .mockImplementation(async (command: MailDraftInput) => acknowledge(command));
        const lane = new DraftWriter(draft(), { write, read: vi.fn().mockRejectedValue(new Error("offline")) });
        await expect(lane.save(input(draft()))).rejects.toThrow("acknowledgement");
        expect(lane.id).toBe(id);
        expect((await lane.save(input(draft()))).revision).toBe(1);
        expect(write.mock.calls[1]).toEqual(write.mock.calls[0]);
        expect(write).toHaveBeenCalledTimes(2);
    });

    it.each(["wrong identity", "unsafe revision"])("does not recover lost acknowledgement from %s readback and keeps the conflict latch", async (kind) => {
        const observed = kind === "wrong identity" ? { ...draft(1), id: "another-draft" } : draft(Number.MAX_SAFE_INTEGER + 1);
        const write = vi.fn().mockRejectedValue(new Error("ack lost"));
        const lane = new DraftWriter(draft(), { write, read: vi.fn(async () => observed) });
        await expect(lane.save(input(draft()))).rejects.toMatchObject({ error: { code: "CONFLICT" } });
        await expect(lane.save(input(draft()))).rejects.toMatchObject({ error: { code: "CONFLICT" } });
        expect(lane.id).toBe(id);
        expect(write).toHaveBeenCalledTimes(1);
    });

    it.each(invalidRevisions)("never adopts invalid readback revision %s after a lost response", async (revision) => {
        const write = vi.fn().mockRejectedValueOnce(new Error("ack lost"))
            .mockImplementation(async (command: MailDraftInput) => acknowledge(command));
        const lane = new DraftWriter(draft(), { write, read: vi.fn(async () => draft(revision)) });
        if (revision > 0) {
            // Preserve the existing newer-readback conflict policy, even when
            // that response cannot become an acknowledged local revision.
            await expect(lane.save(input(draft()))).rejects.toMatchObject({ error: { code: "CONFLICT" } });
            await expect(lane.save(input(draft()))).rejects.toMatchObject({ error: { code: "CONFLICT" } });
            expect(write).toHaveBeenCalledTimes(1);
        } else {
            // NaN/zero/negative are not proof of commit or of write failure.
            await expect(lane.save(input(draft()))).rejects.toThrow("ack lost");
            expect((await lane.save(input(draft()))).revision).toBe(1);
            expect(write.mock.calls[1]).toEqual(write.mock.calls[0]);
            expect(write).toHaveBeenCalledTimes(2);
        }
        expect(lane.id).toBe(id);
    });

    it("rejects wrong-identity reload without clearing an existing conflict", async () => {
        const conflict = { error: { code: "CONFLICT" } };
        const write = vi.fn().mockRejectedValue(conflict);
        const lane = new DraftWriter(draft(3), { write, read: vi.fn(async () => ({ ...draft(7), id: "another-draft" })) });
        const next = input(draft());
        next.payload.subject = "changed";
        await expect(lane.save(next)).rejects.toBe(conflict);
        await expect(lane.reload()).rejects.toThrow();
        await expect(lane.save(next)).rejects.toBe(conflict);
        expect(write).toHaveBeenCalledTimes(1);
    });
});

describe("DraftWriter guard and wire semantics stay unchanged", () => {
    it.each(["close", "session switch"])("rejects late readback and queued work after %s without touching the replacement editor", async (boundary) => {
        const gate = deferred<MailDraft>();
        const started = deferred<void>();
        const read = vi.fn(() => { started.resolve(); return gate.promise; });
        const write = vi.fn().mockRejectedValue(new Error("response lost"));
        let active = true;
        const old = new DraftWriter(draft(), { write, read }, () => { if (!active) throw new Error("session changed"); });
        const first = old.save(input(draft()));
        const second = old.save(input(draft()));
        const firstCheck = expect(first).rejects.toThrow(boundary === "close" ? "Editor closed" : "session changed");
        const secondCheck = expect(second).rejects.toThrow(boundary === "close" ? "Editor closed" : "session changed");
        await started.promise;
        const replacementDraft = { ...draft(5), id: "replacement-session-draft" };
        const replacementWrite = vi.fn(async (command: MailDraftInput) => acknowledge(command));
        const replacement = new DraftWriter(replacementDraft, { write: replacementWrite, read: vi.fn() });
        if (boundary === "close") old.close(); else active = false;
        gate.resolve(draft(1));
        await Promise.all([firstCheck, secondCheck]);
        expect(write).toHaveBeenCalledTimes(1);
        expect(replacement.id).toBe(replacementDraft.id);
        const next = input(replacementDraft);
        next.payload.subject = "replacement input";
        expect((await replacement.save(next)).revision).toBe(6);
        expect(replacementWrite.mock.calls[0][0].id).toBe(replacementDraft.id);
    });

    it("preserves Go omitempty/object-key semantics while recipient and attachment array order remains meaningful", async () => {
        const initial = draft(3);
        delete initial.payload.cc;
        delete initial.payload.bcc;
        delete initial.payload.headers;
        delete initial.payload.template_vars;
        delete initial.payload.html_body;
        delete initial.payload.template_version_id;
        const write = vi.fn(async (command: MailDraftInput) => acknowledge(command));
        const lane = new DraftWriter(initial, { write, read: vi.fn() });
        const same = input(initial);
        Object.assign(same.payload, { cc: [], bcc: [], headers: {}, template_vars: {}, html_body: "", template_version_id: "" });
        expect(await lane.save(same)).toEqual(initial);
        expect(write).not.toHaveBeenCalled();
        const reordered = structuredClone(same);
        reordered.payload.to.reverse();
        reordered.payload.attachment_ids!.reverse();
        expect(draftKey(reordered)).not.toBe(draftKey(same));
        expect((await lane.save(reordered)).revision).toBe(4);
        expect(write).toHaveBeenCalledTimes(1);
        const keyOrderA = input(draft());
        const keyOrderB = structuredClone(keyOrderA);
        keyOrderB.payload.headers = { "X-Second": "two", "X-First": "one" };
        keyOrderB.payload.template_vars = { second: "two", first: "one" };
        expect(draftKey(keyOrderA)).toBe(draftKey(keyOrderB));
        const omitted = input(initial);
        delete omitted.payload.attachment_ids;
        const empty = structuredClone(omitted);
        Object.assign(empty.payload, { cc: [], bcc: [], attachment_ids: [], headers: {}, template_vars: {}, html_body: "", template_version_id: "" });
        const nulls = structuredClone(omitted);
        Object.assign(nulls.payload, { cc: null, bcc: null, attachment_ids: null, headers: null, template_vars: null, html_body: null, template_version_id: null });
        expect(draftKey(empty)).toBe(draftKey(omitted));
        expect(draftKey(nulls)).toBe(draftKey(omitted));
    });
});
