"use client";
import { useEffect, useRef, useState } from "react";
import { useAPI } from "@/hooks/use-api";
import { workMessages, type WorkMailbox, type DraftPayload } from "@/lib/company";
import { sessionScope, useSessionScope } from "@/lib/session";
import { indexStatus } from "../api";
import { LoadError, useText } from "@/components/company/common";
import { MessagePane } from "./message-pane";
import { Pager } from "./list-controls";
type SelectionObservation = { key: string; observed: boolean; retired: boolean };
export function ReceivedFolder({ mailbox, mailboxes, folder, q, page, selected, onSelect, onPage, onCompose }: {
    mailbox: WorkMailbox;
    mailboxes: WorkMailbox[];
    folder: string;
    q: string;
    page: number;
    selected: string;
    onSelect: (id: string) => void;
    onPage: (p: number) => void;
    onCompose: (pending: Promise<DraftPayload>) => void | Promise<void>;
}) {
    const t = useText();
    const scope = useSessionScope();
    const list = useAPI(["work-messages", mailbox.mailbox.id, folder, q, page], () => workMessages(mailbox.mailbox.id, folder, q, page), { refreshInterval: 25000 });
    const index = useAPI(["mail-index-status", mailbox.mailbox.id], () => indexStatus(mailbox.mailbox.id), { refreshInterval: 15000 });
    const selectionKey = JSON.stringify([scope, mailbox.mailbox.id, folder, q, page, selected]);
    const [selection, setSelection] = useState<SelectionObservation>({ key: selectionKey, observed: false, retired: false });
    let observation = selection.key === selectionKey ? selection : { key: selectionKey, observed: false, retired: false };
    if (!observation.retired && selected && !list.error && Array.isArray(list.data?.data)) {
        const present = list.data.data.some(message => message.id === selected);
        if (present && !observation.observed) observation = { ...observation, observed: true };
        // An initially absent row may be a valid deep link outside this page.
        // Only a settled successful list can retire a previously observed row.
        else if (!present && observation.observed && !list.isLoading && !list.isValidating) observation = { ...observation, retired: true };
    }
    if (observation !== selection) setSelection(observation);
    const notified = useRef<SelectionObservation | null>(null);
    useEffect(() => {
        // Keep retired content closed until navigation acknowledges the clear.
        // The committed selection owns this callback, including rapid rerenders.
        if (selection.key !== selectionKey || !selection.retired || notified.current === selection || scope !== sessionScope()) return;
        notified.current = selection;
        onSelect("");
    }, [selection, selectionKey, onSelect, scope]);
    return <div className="space-y-3">
  {index.data && <p className="text-xs text-muted-foreground">{t(`正文索引 ${index.data.indexed}/${index.data.total}；失败 ${index.data.failed}。未索引邮件仍可按主题和地址搜索。`, `Body index ${index.data.indexed}/${index.data.total}; failed ${index.data.failed}. Subject/address search remains available for unindexed mail.`)}</p>}
  <LoadError error={list.error} onRetry={() => void list.mutate()}/>
  <div className="grid min-h-96 gap-4 lg:grid-cols-[minmax(240px,0.85fr)_minmax(0,1.5fr)]">
   <div className="min-w-0 space-y-1 rounded-lg border p-2">
    {!list.error && (list.data?.data ?? []).map(m => <button key={m.id} onClick={() => onSelect(m.id)} aria-pressed={selected === m.id} className={`block w-full rounded px-3 py-3 text-left hover:bg-muted ${selected === m.id ? "bg-muted" : ""}`}>
     <p className={`truncate text-sm ${m.seen ? "" : "font-semibold"}`}>{m.starred ? "★ " : ""}{m.subject || t("无主题", "No subject")}</p><p className="truncate text-xs text-muted-foreground">{m.sender}</p><time className="text-xs text-muted-foreground">{new Date(m.received_at).toLocaleString()}</time>
    </button>)}
    {!list.isLoading && !list.error && !list.data?.data.length && <p className="p-3 text-sm">{t("没有邮件", "No messages")}</p>}
   </div>
   <div className="min-w-0">{selected && !observation.retired ? <MessagePane key={`${mailbox.mailbox.id}:${selected}`} mailbox={mailbox} mailboxes={mailboxes} id={selected} onMutation={() => void list.mutate()} onCompose={onCompose}/> : <p className="p-5 text-sm text-muted-foreground">{t("选择邮件以阅读", "Select a message to read")}</p>}</div>
  </div>
  <Pager page={page} total={list.data?.meta.total ?? 0} onPage={onPage}/>
 </div>;
}
