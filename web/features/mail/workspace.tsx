"use client";
import { useCallback, useEffect, useState } from "react";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { useSWRConfig } from "swr";
import { useAPI } from "@/hooks/use-api";
import { workMailboxes, type DraftPayload, type MailDraft } from "@/lib/company";
import { streamEvents } from "@/lib/api/base";
import { useSessionScope } from "@/lib/session";
import { composeIdentity } from "@/lib/compose-identity";
import { ActionButton, LoadError, useText } from "@/components/company/common";
import { Compose } from "@/components/company/compose";
import { SearchMail } from "./components/list-controls";
import { ReceivedFolder } from "./components/received-folder";
import { SentFolder } from "./components/sent-folder";
import { DraftFolder } from "./components/draft-folder";
import { ReceiptFolder } from "./components/receipt-folder";
const folders = ["inbox", "sent", "drafts", "archive", "trash", "receipts"] as const;
type Folder = typeof folders[number];
const mailKeys = new Set(["work-messages", "sent-assets", "work-drafts", "work-submissions", "mail-index-status"]);
export function MailWorkspace() {
    const t = useText();
    const router = useRouter();
    const pathname = usePathname();
    const params = useSearchParams();
    const scope = useSessionScope();
    const { mutate } = useSWRConfig();
    const [editor, setEditor] = useState<{
        draft: MailDraft;
        key: string;
    } | null>(null);
    const boxes = useAPI("work-mailboxes", workMailboxes, { refreshInterval: 15000 });
    const usable = (boxes.error ? [] : boxes.data ?? []).filter(m => m.can_read || m.can_send);
    const mailbox = usable.find(m => m.mailbox.id === params.get("mailbox")) ?? usable[0];
    const folder: Folder = folders.includes(params.get("folder") as Folder) ? params.get("folder") as Folder : "inbox";
    const page = Math.max(1, Number.parseInt(params.get("page") ?? "1", 10) || 1);
    const q = params.get("q") ?? "";
    const selected = params.get("message") ?? "";
    const archiveSent = folder === "sent" || (["archive", "trash"].includes(folder) && params.get("source") === "sent");
    const setQuery = (patch: Record<string, string | null>) => { const next = new URLSearchParams(params.toString()); for (const [k, v] of Object.entries(patch)) {
        if (!v || (k === "page" && v === "1") || (k === "folder" && v === "inbox"))
            next.delete(k);
        else
            next.set(k, v);
    } router.replace(`${pathname}${next.size ? "?" + next : ""}`, { scroll: false }); };
    const refresh = useCallback(() => { void mutate((key: unknown) => Array.isArray(key) && key[0] === "session" && key[1] === scope && (key[2] === "work-mailboxes" || (Array.isArray(key[2]) && mailKeys.has(key[2][0])))); }, [mutate, scope]);
    const id = mailbox?.can_read ? mailbox.mailbox.id : undefined;
    useEffect(() => { if (!id)
        return; const abort = new AbortController(); void streamEvents(`/api/v1/company/mailboxes/${encodeURIComponent(id)}/events`, { signal: abort.signal, onEvent: event => { if (event.type !== "ping")
            refresh(); } }).catch(() => { }); return () => abort.abort(); }, [id, refresh]);
    const start = (payload?: DraftPayload) => { const from = composeIdentity(usable, mailbox); if (from)
        setEditor({ key: crypto.randomUUID(), draft: { mailbox_id: from.mailbox.id, revision: 0, payload: payload ?? { to: [], subject: "", text_body: "" } } }); };
    const label = (f: Folder) => ({ inbox: t("收件箱", "Inbox"), sent: t("已发送邮件", "Sent mail"), drafts: t("草稿", "Drafts"), archive: t("归档", "Archive"), trash: t("回收站", "Trash"), receipts: t("发送状态", "Delivery status") })[f];
    const onPage = (p: number) => setQuery({ page: String(p), message: null });
    const onSelect = (message: string) => setQuery({ message });
    return <main className="mx-auto w-full max-w-screen-2xl space-y-4 p-4 md:p-6">
  <header className="flex flex-wrap items-center justify-between gap-3"><div><h1 className="text-2xl font-semibold">{t("公司邮箱", "Company mail")}</h1><p className="text-sm text-muted-foreground">{t("个人与共享邮箱 · 邮件资产与投递状态分离", "Personal and shared mailboxes · message content and delivery status are separate")}</p></div><div className="flex gap-2"><ActionButton onClick={refresh}>{t("刷新", "Refresh")}</ActionButton><ActionButton disabled={Boolean(editor) || !composeIdentity(usable, mailbox)} onClick={() => start()}>{t("写邮件", "Compose")}</ActionButton></div></header>
  <LoadError error={boxes.error} onRetry={() => void boxes.mutate()}/>
  {editor ? <Compose key={editor.key} initial={editor.draft} mailboxes={usable} onClose={() => { setEditor(null); refresh(); }} onSent={() => { setEditor(null); setQuery({ folder: "receipts", message: null, page: null }); refresh(); }}/> : <div className="grid gap-5 md:grid-cols-[210px_minmax(0,1fr)]">
    <aside className="space-y-5"><nav aria-label={t("邮箱", "Mailboxes")} className="space-y-1">{usable.map(m => <button key={m.mailbox.id} className={`block w-full rounded border p-3 text-left text-xs ${mailbox?.mailbox.id === m.mailbox.id ? "bg-muted" : ""}`} aria-pressed={m.mailbox.id === mailbox?.mailbox.id} onClick={() => setQuery({ mailbox: m.mailbox.id, message: null, page: null })}><span className="block break-all font-medium">{m.mailbox.full_address}</span><span className="text-muted-foreground">{m.mailbox.kind === "shared" ? t("共享邮箱", "Shared") : t("个人邮箱", "Personal")} · {m.can_read ? t("可读", "read") : t("仅代发", "send only")}</span></button>)}</nav>
    <nav aria-label={t("邮件文件夹", "Mail folders")} className="flex flex-wrap gap-2 md:flex-col">{folders.map(f => <ActionButton key={f} aria-pressed={folder === f} className={folder === f ? "bg-muted text-left" : "text-left"} onClick={() => setQuery({ folder: f, source: null, page: null, message: null, q: null })}>{label(f)}</ActionButton>)}</nav></aside>
    <section className="min-w-0 space-y-4"><h2 className="text-lg font-semibold">{label(folder)}</h2>
    {folder !== "receipts" && folder !== "drafts" && <>
      <p className="text-sm">{t("当前邮箱", "Current mailbox")}: {mailbox?.mailbox.full_address ?? "—"}</p>
      {["archive", "trash"].includes(folder) && <div className="flex gap-2"><ActionButton aria-pressed={!archiveSent} onClick={() => setQuery({ source: null, page: null, message: null })}>{t("收件", "Received")}</ActionButton><ActionButton aria-pressed={archiveSent} onClick={() => setQuery({ source: "sent", page: null, message: null })}>{t("已发送", "Sent")}</ActionButton></div>}
      {folder === "trash" && <p className="text-xs text-muted-foreground">{t("邮件删除后进入 30 天回收站；共享整理影响同邮箱所有成员。", "Deleted mail is recoverable for 30 days; shared organizing affects everyone in the mailbox.")}</p>}
      <SearchMail key={`${mailbox?.mailbox.id}:${folder}:${q}`} value={q} onSearch={q => setQuery({ q, message: null, page: null })}/>
    </>}
    {folder === "receipts" ? <ReceiptFolder page={page} selected={selected} onPage={onPage} onSelect={onSelect}/> : folder === "drafts" ? <DraftFolder page={page} mailboxes={usable} onPage={onPage} onEdit={draft => setEditor({ draft, key: crypto.randomUUID() })}/> : !mailbox?.can_read ? <p role="status">{t("当前没有可阅读的邮箱；代发身份仍可用于写信。", "No readable mailbox is selected; authorized send identities remain available for composing.")}</p> : archiveSent ? <SentFolder key={`${mailbox.mailbox.id}:${folder}`} mailbox={mailbox} folder={folder} q={q} page={page} selected={selected} onPage={onPage} onSelect={onSelect}/> : <ReceivedFolder key={`${mailbox.mailbox.id}:${folder}`} mailbox={mailbox} mailboxes={usable} folder={folder} q={q} page={page} selected={selected} onPage={onPage} onSelect={onSelect} onCompose={start}/>}
    </section>
  </div>}
 </main>;
}
