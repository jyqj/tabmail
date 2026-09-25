import type { DraftPayload, MailDraft } from "@/lib/company";
export type DraftInput = {
    mailbox_id: string;
    payload: DraftPayload;
};
export type DraftTransport = {
    write: (draft: MailDraft, create: boolean) => Promise<MailDraft>;
    read: (id: string) => Promise<MailDraft>;
};
// Same wire semantics as Go's omitempty; object ordering is not an edit.
export function draftKey(input: DraftInput): string {
    const p = { ...input.payload };
    for (const k of ["cc", "bcc", "attachment_ids"] as const)
        if (!p[k]?.length)
            delete p[k];
    if (!p.html_body)
        delete p.html_body;
    if (!p.template_version_id)
        delete p.template_version_id;
    for (const k of ["headers", "template_vars"] as const)
        if (!Object.keys(p[k] ?? {}).length)
            delete p[k];
    return JSON.stringify({ mailbox_id: input.mailbox_id, payload: p }, (_key, value) => {
        if (value && typeof value === "object" && !Array.isArray(value))
            return Object.fromEntries(Object.keys(value).sort().filter(k => value[k] !== undefined).map(k => [k, value[k]]));
        return value;
    });
}
export function draftErrorCode(e: unknown): string | undefined {
    return (e as {
        error?: {
            code?: string;
        };
    })?.error?.code;
}
// One revision lane shared by autosave, explicit save and submit flush. No
// request can observe a stale locally acknowledged revision, and a conflict
// latches the lane until the user explicitly reloads or creates a copy.
type PendingWrite = { draft: MailDraft; create: boolean; uncertain: boolean };

export class DraftWriter {
    private snapshot: MailDraft;
    private tail: Promise<unknown> = Promise.resolve();
    private closed = false;
    private conflict: unknown;
    private pending?: PendingWrite;
    constructor(initial: MailDraft, private transport: DraftTransport, private guard: () => void = () => { }) {
        this.snapshot = structuredClone({ ...initial, id: initial.id || crypto.randomUUID() });
    }
    get id() { return this.snapshot.id!; }
    close() { this.closed = true; }
    private check() {
        this.guard();
        if (this.closed)
            throw new DOMException("Editor closed", "AbortError");
    }
    private acknowledgementMatches(observed: MailDraft, command: PendingWrite): boolean {
        return observed.id === command.draft.id &&
            observed.revision === command.draft.revision + 1 &&
            draftKey(observed) === draftKey(command.draft);
    }
    private async persist(command: PendingWrite): Promise<MailDraft> {
        this.check();
        let saved: MailDraft;
        try {
            saved = await this.transport.write(structuredClone(command.draft), command.create);
            this.check();
            if (!this.acknowledgementMatches(saved, command))
                throw new Error("Draft acknowledgement does not match the submitted revision");
        } catch (e) {
            this.check();
            const code = draftErrorCode(e);
            const unknown = !code || code === "INTERNAL_ERROR" || code === "INTERNAL";
            const mayReplayConflict = command.uncertain && code === "CONFLICT";
            if (unknown || mayReplayConflict) {
                command.uncertain = true;
                // Pin the exact UUID, revision AND input until its outcome is
                // known. New typing must not change a retried creation/update.
                this.pending = command;
                let observed: MailDraft | undefined;
                try { observed = await this.transport.read(this.id); }
                catch { /* a failed readback is not proof that the write failed */ }
                this.check();
                if (observed && this.acknowledgementMatches(observed, command)) {
                    this.snapshot = observed;
                    this.pending = undefined;
                    return structuredClone(observed);
                }
                if (observed && (observed.id !== this.id || observed.revision > command.draft.revision)) {
                    this.conflict = { error: { code: "CONFLICT", message: "Draft changed elsewhere; reload or keep a new private copy" } };
                    throw this.conflict;
                }
            }
            if (mayReplayConflict)
                throw new Error("Draft save is still unconfirmed; retry the original request when connected");
            if (["CONFLICT", "FORBIDDEN", "NOT_FOUND", "UNAUTHORIZED"].includes(code ?? ""))
                this.conflict = e;
            if (!unknown && !mayReplayConflict)
                this.pending = undefined;
            throw e;
        }
        this.snapshot = saved;
        this.pending = undefined;
        return structuredClone(saved);
    }
    save(input: DraftInput): Promise<MailDraft> {
        const copy = structuredClone(input);
        const operation = this.tail.catch(() => { }).then(async () => {
            this.check();
            if (this.conflict)
                throw this.conflict;
            if (this.pending)
                await this.persist(this.pending);
            this.check();
            if (this.snapshot.revision > 0 && draftKey(copy) === draftKey(this.snapshot))
                return structuredClone(this.snapshot);
            return this.persist({ draft: { ...this.snapshot, ...copy }, create: this.snapshot.revision === 0, uncertain: false });
        });
        this.tail = operation;
        return operation;
    }
    async reload(): Promise<MailDraft> {
        await this.tail.catch(() => { });
        this.check();
        const observed = await this.transport.read(this.id);
        this.check();
        if (observed.id !== this.id || observed.revision < 1)
            throw new Error("Draft readback identity does not match this editor");
        this.snapshot = observed;
        this.conflict = undefined;
        this.pending = undefined;
        return structuredClone(observed);
    }
}
