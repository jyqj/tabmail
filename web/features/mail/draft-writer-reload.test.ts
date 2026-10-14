import { describe, expect, it, vi } from "vitest";
import type { MailDraft, MailDraftInput } from "@/lib/company";
import { DraftWriter, type DraftInput } from "./draft-writer";

const id = "draft-reload-lane";
function draft(revision = 3, subject = "server draft"): MailDraft {
    return {
        id, revision, mailbox_id: "box", updated_at: "2026-10-05T00:00:00Z",
        payload: { to: ["recipient@example.test"], subject, text_body: "draft body" },
    };
}
function input(subject: string): DraftInput {
    return { mailbox_id: "box", payload: { ...draft().payload, subject } };
}
function acknowledge(command: MailDraftInput): MailDraft {
    if (!command.id) throw new Error("Missing draft identity");
    return { ...structuredClone(command), id: command.id, revision: command.revision + 1,
        updated_at: "2026-10-05T00:00:00Z" };
}
function deferred<T>() {
    let resolve!: (value: T) => void;
    let reject!: (reason: unknown) => void;
    const promise = new Promise<T>((a, b) => { resolve = a; reject = b; });
    return { promise, resolve, reject };
}
// Let runnable promise continuations drain while the controlled transport stays pending.
const nextTurn = () => new Promise<void>(resolve => setTimeout(resolve, 0));

describe("DraftWriter reload joins the revision lane", () => {
    it.each(["same turn", "read in flight"])("keeps a later save behind reload: %s", async (timing) => {
        const started = deferred<void>();
        const response = deferred<MailDraft>();
        const read = vi.fn(() => { started.resolve(); return response.promise; });
        const write = vi.fn(async (command: MailDraftInput) => acknowledge(command));
        const lane = new DraftWriter(draft(), { write, read });
        const reloaded = lane.reload();
        if (timing === "read in flight") await started.promise;
        const edit = input("queued typing");
        const saved = lane.save(edit);
        edit.payload.subject = "outside mutation";
        await started.promise;
        await nextTurn();
        expect(write).not.toHaveBeenCalled();
        response.resolve(draft(7));
        expect((await reloaded).revision).toBe(7);
        expect(await saved).toMatchObject({ revision: 8, payload: { subject: "queued typing" } });
        expect(write.mock.calls[0][0].revision).toBe(7);
    });

    it("preserves save, reload, save order when the first write is still pending", async () => {
        const firstStarted = deferred<MailDraftInput>();
        const firstResponse = deferred<MailDraft>();
        const readStarted = deferred<void>();
        const readResponse = deferred<MailDraft>();
        const write = vi.fn(async (command: MailDraftInput) => acknowledge(command))
            .mockImplementationOnce(command => { firstStarted.resolve(command); return firstResponse.promise; });
        const read = vi.fn(() => { readStarted.resolve(); return readResponse.promise; });
        const lane = new DraftWriter(draft(), { write, read });
        const first = lane.save(input("first edit"));
        const command = await firstStarted.promise;
        const reload = lane.reload();
        const last = lane.save(input("last edit"));
        await nextTurn();
        expect(read).not.toHaveBeenCalled();
        expect(write).toHaveBeenCalledTimes(1);
        firstResponse.resolve(acknowledge(command));
        await first;
        await readStarted.promise;
        await nextTurn();
        expect(write).toHaveBeenCalledTimes(1);
        readResponse.resolve(draft(6, "remote edit after first save"));
        await reload;
        expect((await last).revision).toBe(7);
        expect(write.mock.calls.map(([value]) => value.revision)).toEqual([3, 6]);
    });

    it("never rolls back a newer acknowledged revision when an older read settles late", async () => {
        let server = draft();
        const readStarted = deferred<void>();
        const response = deferred<MailDraft>();
        const read = vi.fn(() => { readStarted.resolve(); return response.promise; });
        const write = vi.fn(async (command: MailDraftInput) => {
            if (command.revision !== server.revision)
                throw { error: { code: "CONFLICT" } };
            server = acknowledge(command);
            return server;
        });
        const lane = new DraftWriter(server, { write, read });
        const oldRead = structuredClone(server);
        const reloaded = lane.reload();
        await readStarted.promise;
        const saved = lane.save(input("new typing"));
        await nextTurn();
        response.resolve(oldRead);
        await reloaded;
        expect((await saved).revision).toBe(4);
        await expect(lane.save(input("next edit"))).resolves.toMatchObject({ revision: 5 });
        expect(write.mock.calls.map(([command]) => command.revision)).toEqual([3, 4]);
    });

    it("serializes two reloads even when the second response is ready first", async () => {
        const firstStarted = deferred<void>();
        const firstResponse = deferred<MailDraft>();
        const secondResponse = deferred<MailDraft>();
        const read = vi.fn().mockImplementationOnce(() => { firstStarted.resolve(); return firstResponse.promise; })
            .mockImplementationOnce(() => secondResponse.promise);
        const write = vi.fn(async (command: MailDraftInput) => acknowledge(command));
        const lane = new DraftWriter(draft(), { write, read });
        const first = lane.reload();
        await firstStarted.promise;
        const second = lane.reload();
        secondResponse.resolve(draft(7, "newer server version"));
        await nextTurn();
        expect(read).toHaveBeenCalledTimes(1);
        firstResponse.resolve(draft(4));
        expect((await first).revision).toBe(4);
        expect((await second).revision).toBe(7);
        expect((await lane.save(input("next edit"))).revision).toBe(8);
        expect(read.mock.calls).toEqual([[id], [id]]);
    });

    it.each(["network", "wrong identity", "unsafe revision"])("continues queued saves after a rejected reload: %s", async (failure) => {
        const started = deferred<void>();
        const response = deferred<MailDraft>();
        const write = vi.fn(async (command: MailDraftInput) => acknowledge(command));
        const lane = new DraftWriter(draft(), { write, read: vi.fn(() => { started.resolve(); return response.promise; }) });
        const reloadCheck = expect(lane.reload()).rejects.toThrow();
        await started.promise;
        const saved = lane.save(input("surviving local edit"));
        await nextTurn();
        expect(write).not.toHaveBeenCalled();
        if (failure === "network") response.reject(new Error("offline read"));
        else response.resolve(failure === "wrong identity" ? { ...draft(7), id: "another-draft" } : draft(0));
        await reloadCheck;
        expect((await saved).revision).toBe(4);
        expect(write.mock.calls[0][0]).toMatchObject({ id, revision: 3, payload: { subject: "surviving local edit" } });
    });

    it("allows a later reload to recover after a rejected read", async () => {
        const read = vi.fn().mockRejectedValueOnce(new Error("offline read")).mockResolvedValueOnce(draft(7));
        const write = vi.fn(async (command: MailDraftInput) => acknowledge(command));
        const lane = new DraftWriter(draft(), { write, read });
        const failed = expect(lane.reload()).rejects.toThrow("offline read");
        const recovered = lane.reload();
        const saved = lane.save(input("after recovered reload"));
        await failed;
        expect((await recovered).revision).toBe(7);
        expect((await saved).revision).toBe(8);
        expect(write.mock.calls[0][0].revision).toBe(7);
    });

    it("preserves a conflict after invalid reload and clears it before saves following a valid reload", async () => {
        const conflict = { error: { code: "CONFLICT" } };
        const started = deferred<void>();
        const response = deferred<MailDraft>();
        const read = vi.fn().mockResolvedValueOnce({ ...draft(7), id: "another-draft" })
            .mockImplementationOnce(() => { started.resolve(); return response.promise; });
        const write = vi.fn(async (command: MailDraftInput) => acknowledge(command)).mockRejectedValueOnce(conflict);
        const lane = new DraftWriter(draft(), { write, read });
        await expect(lane.save(input("conflicting edit"))).rejects.toBe(conflict);
        await expect(lane.reload()).rejects.toThrow();
        await expect(lane.save(input("still blocked"))).rejects.toBe(conflict);
        const reload = lane.reload();
        await started.promise;
        const savedCheck = expect(lane.save(input("after explicit reload"))).resolves.toMatchObject({ revision: 8 });
        response.resolve(draft(7));
        await reload;
        await savedCheck;
        expect(write).toHaveBeenCalledTimes(2);
        expect(write.mock.calls[1][0].revision).toBe(7);
    });

    it("retains an uncertain exact replay when reload fails before newer typing is saved", async () => {
        const started = deferred<void>();
        const response = deferred<MailDraft>();
        const write = vi.fn(async (command: MailDraftInput) => acknowledge(command))
            .mockRejectedValueOnce(new Error("write response lost"));
        const read = vi.fn().mockRejectedValueOnce(new Error("readback offline"))
            .mockImplementationOnce(() => { started.resolve(); return response.promise; });
        const lane = new DraftWriter(draft(), { write, read });
        await expect(lane.save(input("original command"))).rejects.toThrow("write response lost");
        const reloadCheck = expect(lane.reload()).rejects.toThrow("reload offline");
        await started.promise;
        const saved = lane.save(input("new typing"));
        await nextTurn();
        expect(write).toHaveBeenCalledTimes(1);
        response.reject(new Error("reload offline"));
        await reloadCheck;
        expect((await saved).revision).toBe(5);
        expect(write.mock.calls[1]).toEqual(write.mock.calls[0]);
        expect(write.mock.calls[2][0]).toMatchObject({ id, revision: 4, payload: { subject: "new typing" } });
    });

    it("replaces an uncertain command only after successful explicit reload", async () => {
        const started = deferred<void>();
        const response = deferred<MailDraft>();
        const write = vi.fn(async (command: MailDraftInput) => acknowledge(command))
            .mockRejectedValueOnce(new Error("write response lost"));
        const read = vi.fn().mockRejectedValueOnce(new Error("readback offline"))
            .mockImplementationOnce(() => { started.resolve(); return response.promise; });
        const lane = new DraftWriter(draft(), { write, read });
        await expect(lane.save(input("uncertain command"))).rejects.toThrow("write response lost");
        const reload = lane.reload();
        await started.promise;
        const edit = { ...input("new mailbox edit"), mailbox_id: "another-authorized-mailbox" };
        const saved = lane.save(edit);
        response.resolve(draft(7));
        await reload;
        expect(await saved).toMatchObject({ id, revision: 8, mailbox_id: edit.mailbox_id });
        expect(write).toHaveBeenCalledTimes(2);
        expect(write.mock.calls[1][0]).toMatchObject({ id, revision: 7, ...edit });
    });

    it.each(["close", "session switch"])("rejects in-flight reload and queued work after %s, without changing a replacement writer", async (boundary) => {
        const started = deferred<void>();
        const response = deferred<MailDraft>();
        const read = vi.fn(() => { started.resolve(); return response.promise; });
        const write = vi.fn(async (command: MailDraftInput) => acknowledge(command));
        let active = true;
        const lane = new DraftWriter(draft(), { write, read }, () => { if (!active) throw new Error("session changed"); });
        const message = boundary === "close" ? "Editor closed" : "session changed";
        const reloadCheck = expect(lane.reload()).rejects.toThrow(message);
        await started.promise;
        const saveCheck = expect(lane.save(input("old session edit"))).rejects.toThrow(message);
        const queuedReloadCheck = expect(lane.reload()).rejects.toThrow(message);
        const replacementWrite = vi.fn(async (command: MailDraftInput) => acknowledge(command));
        const replacement = new DraftWriter({ ...draft(10), id: "replacement-draft", mailbox_id: "replacement-box" },
            { write: replacementWrite, read: vi.fn() });
        if (boundary === "close") lane.close(); else active = false;
        response.resolve(draft(7));
        await Promise.all([reloadCheck, saveCheck, queuedReloadCheck]);
        expect(read).toHaveBeenCalledTimes(1);
        expect(write).not.toHaveBeenCalled();
        expect(await replacement.save({ ...input("replacement edit"), mailbox_id: "replacement-box" }))
            .toMatchObject({ id: "replacement-draft", revision: 11, mailbox_id: "replacement-box" });
    });

    it("checks the guard before a queued reload starts", async () => {
        const read = vi.fn();
        const lane = new DraftWriter(draft(), { write: vi.fn(), read });
        const reloadCheck = expect(lane.reload()).rejects.toThrow("Editor closed");
        lane.close();
        await reloadCheck;
        expect(read).not.toHaveBeenCalled();
    });
});
