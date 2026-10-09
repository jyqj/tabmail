"use client";
import { useCallback, useEffect, useLayoutEffect, useRef, useState } from "react";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { useSWRConfig } from "swr";
import { useAPI } from "@/hooks/use-api";
import { workMailboxes, type DraftPayload, type MailDraftEditor } from "@/lib/company";
import { streamEvents } from "@/lib/api/base";
import { sessionScope, useSessionScope } from "@/lib/session";
import { composeIdentity } from "@/lib/compose-identity";
import { ActionButton, LoadError, useText } from "@/components/company/common";
import { Compose } from "@/components/company/compose";
import { SearchMail } from "./components/list-controls";
import { ReceivedFolder } from "./components/received-folder";
import { SentFolder } from "./components/sent-folder";
import { DraftFolder } from "./components/draft-folder";
import { ReceiptFolder } from "./components/receipt-folder";
import { LiveContentRefresh } from "./live-content-refresh";
const folders = ["inbox", "sent", "drafts", "archive", "trash", "receipts"] as const;
type Folder = typeof folders[number];
// Reconnect resync and manual refresh must cover mounted readers as well as
// folder lists. Compatibility receipts do not poll; aggregate details carry
// current capabilities. Private live readers receive a separate local signal.
const mailKeys = new Set(["work-messages", "work-message", "inbound-attachments", "mail-conversation", "sent-assets", "work-drafts", "work-submissions", "submission", "legacy-outbound-receipt", "mail-index-status"]);
export function MailWorkspace() {
    const t = useText();
    const router = useRouter();
    const pathname = usePathname();
    const params = useSearchParams();
    const scope = useSessionScope();
    const { mutate } = useSWRConfig();
    const [contentRefresh, setContentRefresh] = useState(0);
    const [editor, setEditor] = useState<{
        draft: MailDraftEditor;
        key: string;
    } | null>(null);
    const boxes = useAPI("work-mailboxes", workMailboxes, { refreshInterval: 15000 });
    const usable = (boxes.error ? [] : boxes.data ?? []).filter(m => m.can_read || m.can_send);
    // Only an absent parameter opts into the ordinary first-mailbox default.
    const requestedMailbox = params.get("mailbox");
    const mailbox = requestedMailbox === null ? usable[0] : usable.find(m => m.mailbox.id === requestedMailbox);
    const sender = requestedMailbox !== null && !mailbox ? undefined : composeIdentity(usable, mailbox);
    const folder: Folder = folders.includes(params.get("folder") as Folder) ? params.get("folder") as Folder : "inbox";
    const page = Math.max(1, Number.parseInt(params.get("page") ?? "1", 10) || 1);
    const q = params.get("q") ?? "";
    const selected = params.get("message") ?? "";
    const archiveSent = folder === "sent" || (["archive", "trash"].includes(folder) && params.get("source") === "sent");
    // Editor/send ownership survives query-only navigation. The permission to
    // redirect after sending belongs to the originating committed mail view.
    const navigationKey = JSON.stringify([scope, pathname, requestedMailbox, mailbox?.mailbox.id, folder, page, q, selected, archiveSent]);
    const [navigation, setNavigation] = useState({ key: navigationKey });
    if (navigation.key !== navigationKey) setNavigation({ key: navigationKey });
    // Opening an editor is a separate intent from completing an existing send.
    // Only committed views publish authority; away/back and unmount retire it.
    const openingView = useRef<object | null>(null);
    const openingIntent = useRef<object | null>(null);
    useLayoutEffect(() => {
        openingView.current = {};
        return () => { openingView.current = null; };
    }, [navigation]);
    const setQuery = (patch: Record<string, string | null>) => { const next = new URLSearchParams(params.toString()); for (const [k, v] of Object.entries(patch)) {
        if (!v || (k === "page" && v === "1") || (k === "folder" && v === "inbox"))
            next.delete(k);
        else
            next.set(k, v);
    } router.replace(`${pathname}${next.size ? "?" + next : ""}`, { scroll: false }); };
    const completion = useRef<{ navigation: object; editorKey: string | null; setQuery: typeof setQuery } | null>(null);
    useLayoutEffect(() => {
        // Publish only committed state. An abandoned render must not revoke a
        // visible send, and unrelated query data must use the latest callback.
        completion.current = { navigation, editorKey: editor?.key ?? null, setQuery };
        return () => { completion.current = null; };
    });
    const refresh = useCallback(() => {
        // Retired callbacks cannot refresh a replacement session's visible
        // private reader. The signal never opens disclosure or stores content.
        if (scope === sessionScope()) setContentRefresh(value => value + 1);
        void mutate((key: unknown) => Array.isArray(key) && key[0] === "session" && key[1] === scope && (key[2] === "work-mailboxes" || key[2] === "legacy-outbound-receipts" || (Array.isArray(key[2]) && mailKeys.has(key[2][0])))); }, [mutate, scope]);
    const id = mailbox?.can_read ? mailbox.mailbox.id : undefined;
    useEffect(() => { if (!id)
        return; const abort = new AbortController(); void streamEvents(`/api/v1/company/mailboxes/${encodeURIComponent(id)}/events`, { signal: abort.signal, onEvent: event => { if (event.type !== "ping")
            refresh(); } }).catch(() => { }); return () => abort.abort(); }, [id, refresh]);
    function newDraft(payload: DraftPayload): MailDraftEditor | null {
        return sender ? { mailbox_id: sender.mailbox.id, revision: 0, payload } : null;
    }
    function start() {
        // Supersede pending preparation immediately, before the new editor commits.
        openingIntent.current = {};
        const draft = newDraft({ to: [], subject: "", text_body: "" });
        if (draft) setEditor({ key: crypto.randomUUID(), draft });
    }
    async function openEditor(pending: Promise<MailDraftEditor | null>) {
        const view = openingView.current;
        const intent = {};
        openingIntent.current = intent;
        const owns = () => view !== null && openingView.current === view && openingIntent.current === intent;
        try {
            const draft = await pending;
            if (owns() && draft) setEditor({ key: crypto.randomUUID(), draft });
        } catch (error) {
            // A superseded read/preparation cannot report into the newer view.
            if (owns()) throw error;
        }
    }
    const compose = (pending: Promise<DraftPayload>) => openEditor(pending.then(newDraft));
    const label = (f: Folder) => ({ inbox: t("收件箱", "Inbox"), sent: t("已发送邮件", "Sent mail"), drafts: t("草稿", "Drafts"), archive: t("归档", "Archive"), trash: t("回收站", "Trash"), receipts: t("发送状态", "Delivery status") })[f];
    const onPage = (p: number) => setQuery({ page: String(p), message: null });
    const onSelect = (message: string) => setQuery({ message });
    return <LiveContentRefresh value={contentRefresh}><main className="mx-auto w-full max-w-screen-2xl space-y-4 p-4 md:p-6">
  <header className="flex flex-wrap items-center justify-between gap-3"><div><h1 className="text-2xl font-semibold">{t("公司邮箱", "Company mail")}</h1><p className="text-sm text-muted-foreground">{t("个人与共享邮箱 · 邮件资产与投递状态分离", "Personal and shared mailboxes · message content and delivery status are separate")}</p></div><div className="flex gap-2"><ActionButton onClick={refresh}>{t("刷新", "Refresh")}</ActionButton><ActionButton disabled={Boolean(editor) || !sender} onClick={() => start()}>{t("写邮件", "Compose")}</ActionButton></div></header>
  <LoadError error={boxes.error} onRetry={() => void boxes.mutate()}/>
  {editor ? <Compose key={editor.key} initial={editor.draft} mailboxes={usable} onClose={() => { setEditor(null); refresh(); }} onSent={() => {
      const current = completion.current;
      // A successful submit consumes its own draft. Retire that editor even if
      // navigation changed, without clearing a replacement or changing its URL.
      setEditor(value => value?.key === editor.key ? null : value);
      if (current?.navigation === navigation && current.editorKey === editor.key)
          current.setQuery({ folder: "receipts", message: null, page: null });
      refresh();
  }}/> : <div className="grid gap-5 md:grid-cols-[210px_minmax(0,1fr)]">
    <aside className="space-y-5"><nav aria-label={t("邮箱", "Mailboxes")} className="space-y-1">{usable.map(m => <button key={m.mailbox.id} className={`block w-full rounded border p-3 text-left text-xs ${mailbox?.mailbox.id === m.mailbox.id ? "bg-muted" : ""}`} aria-pressed={m.mailbox.id === mailbox?.mailbox.id} onClick={() => setQuery({ mailbox: m.mailbox.id, message: null, page: null })}><span className="block break-all font-medium">{m.mailbox.full_address}</span><span className="text-muted-foreground">{m.mailbox.kind === "shared" ? t("共享邮箱", "Shared") : t("个人邮箱", "Personal")} · {m.can_read ? t("可读", "read") : t("仅代发", "send only")}</span></button>)}</nav>
    <nav aria-label={t("邮件文件夹", "Mail folders")} className="flex flex-wrap gap-2 md:flex-col">{folders.map(f => <ActionButton key={f} aria-pressed={folder === f} className={folder === f ? "bg-muted text-left" : "text-left"} onClick={() => setQuery({ folder: f, source: null, page: null, message: null, q: null })}>{label(f)}</ActionButton>)}</nav></aside>
    <section className="min-w-0 space-y-4"><h2 className="text-lg font-semibold">{label(folder)}</h2>
    {folder !== "receipts" && folder !== "drafts" && <>
      <p className="text-sm">{t("当前邮箱", "Current mailbox")}: {mailbox?.mailbox.full_address ?? "—"}</p>
      {["archive", "trash"].includes(folder) && <div className="flex gap-2"><ActionButton aria-pressed={!archiveSent} onClick={() => setQuery({ source: null, page: null, message: null })}>{t("收件", "Received")}</ActionButton><ActionButton aria-pressed={archiveSent} onClick={() => setQuery({ source: "sent", page: null, message: null })}>{t("已发送", "Sent")}</ActionButton></div>}
      {folder === "trash" && <p className="text-xs text-muted-foreground">{t("邮件删除后进入 30 天回收站；共享整理影响同邮箱所有成员。", "Deleted mail is recoverable for 30 days; shared organizing affects everyone in the mailbox.")}</p>}
      <SearchMail key={`${mailbox?.mailbox.id}:${folder}:${q}`} value={q} onSearch={q => setQuery({ q, message: null, page: null })}/>
    </>}
    {folder === "receipts" ? <ReceiptFolder page={page} selected={selected} onPage={onPage} onSelect={onSelect} includeCompatibility/> : folder === "drafts" ? <DraftFolder page={page} mailboxes={usable} onPage={onPage} onEdit={openEditor}/> : !mailbox?.can_read ? <p role="status">{boxes.isLoading ? t("正在加载邮箱…", "Loading mailboxes…")
      : requestedMailbox !== null && !mailbox && !boxes.error ? t("指定邮箱当前不可用。请刷新重试或选择其他邮箱。", "The requested mailbox is unavailable. Refresh or choose another mailbox.")
      : t("当前没有可阅读的邮箱；代发身份仍可用于写信。", "No readable mailbox is selected; authorized send identities remain available for composing.")}</p> : archiveSent ? <SentFolder key={`${mailbox.mailbox.id}:${folder}`} mailbox={mailbox} folder={folder} q={q} page={page} selected={selected} onPage={onPage} onSelect={onSelect}/> : <ReceivedFolder key={`${mailbox.mailbox.id}:${folder}`} mailbox={mailbox} mailboxes={usable} folder={folder} q={q} page={page} selected={selected} onPage={onPage} onSelect={onSelect} onCompose={compose}/>}
    </section>
  </div>}
 </main></LiveContentRefresh>;
}
