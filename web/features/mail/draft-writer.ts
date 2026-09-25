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
export class DraftWriter {
    private snapshot: MailDraft;
    private tail: Promise<unknown> = Promise.resolve();
    private closed = false;
    private conflict: unknown;
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
    save(input: DraftInput): Promise<MailDraft> {
        const copy = structuredClone(input);
        const operation = this.tail.catch(() => { }).then(async () => {
            this.check();
            if (this.conflict)
                throw this.conflict;
            if (this.snapshot.revision > 0 && draftKey(copy) === draftKey(this.snapshot))
                return structuredClone(this.snapshot);
            const next = { ...this.snapshot, ...copy };
            let saved: MailDraft;
            try {
                saved = await this.transport.write(next, this.snapshot.revision === 0);
            }
            catch (e) {
                this.check();
                const code = draftErrorCode(e);
                if (!code || code === "INTERNAL_ERROR" || code === "INTERNAL") {
                    // The server may have committed before the connection failed. Read
                    // back that exact UUID/input; never guess that it is safe to overwrite.
                    try {
                        const observed = await this.transport.read(this.id);
                        this.check();
                        if (draftKey(observed) === draftKey(copy) && observed.revision > this.snapshot.revision) {
                            this.snapshot = observed;
                            return structuredClone(observed);
                        }
                    }
                    catch { /* leave the acknowledged revision unchanged */ }
                }
                if (["CONFLICT", "FORBIDDEN", "NOT_FOUND", "UNAUTHORIZED"].includes(code ?? ""))
                    this.conflict = e;
                throw e;
            }
            this.check();
            this.snapshot = saved;
            return structuredClone(saved);
        });
        this.tail = operation;
        return operation;
    }
    async reload(): Promise<MailDraft> {
        await this.tail.catch(() => { });
        this.check();
        const observed = await this.transport.read(this.id);
        this.check();
        this.snapshot = observed;
        this.conflict = undefined;
        return structuredClone(observed);
    }
}
