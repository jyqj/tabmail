"use client";
import Link from "next/link";
import { useAPI } from "@/hooks/use-api";
import type { WorkMailbox } from "@/lib/company";
import { archivedPage, changeArchived } from "../api";
import { ActionButton, LoadError, useAction, useText } from "@/components/company/common";
import { SubmissionContentView } from "./submission-content";
import { Pager } from "./list-controls";
export function SentFolder({ mailbox, folder, q, page, selected, onSelect, onPage }: {
    mailbox: WorkMailbox;
    folder: string;
    q: string;
    page: number;
    selected: string;
    onSelect: (id: string) => void;
    onPage: (p: number) => void;
}) {
    const t = useText();
    const { busy, run } = useAction();
    const list = useAPI(["sent-assets", mailbox.mailbox.id, folder, q, page], () => archivedPage(mailbox.mailbox.id, folder, q, page), { refreshInterval: 15000 });
    const item = !list.error ? list.data?.data.find(m => m.id === selected) : undefined;
    return <div className="space-y-3">
  <p className="text-xs text-muted-foreground">{t("已发送内容是当前邮箱的邮件资产；即使投递任务归档清理，邮件仍保留。发送结果请查看发送状态。", "Sent content belongs to this mailbox and survives delivery-job cleanup. Delivery status is a separate view.")}</p>
  <LoadError error={list.error} onRetry={() => void list.mutate()}/>
  <div className="grid min-h-96 gap-4 lg:grid-cols-[minmax(240px,0.85fr)_minmax(0,1.5fr)]">
   <div className="min-w-0 space-y-1 rounded-lg border p-2">{!list.error && (list.data?.data ?? []).map(m => <button key={m.id} onClick={() => onSelect(m.id)} aria-pressed={m.id === selected} className={`block w-full rounded px-3 py-3 text-left hover:bg-muted ${m.id === selected ? "bg-muted" : ""}`}>
    <p className="truncate text-sm font-medium">{m.subject || t("无主题", "No subject")}</p><p className="truncate text-xs">{m.from} → {(m.to ?? []).join(", ")}</p><time className="text-xs text-muted-foreground">{new Date(m.created_at).toLocaleString()}</time>
   </button>)}{!list.isLoading && !list.error && !list.data?.data.length && <p className="p-3 text-sm">{t("没有邮件", "No messages")}</p>}</div>
   <div className="min-w-0 space-y-3">{item && <>
    <h2 className="text-lg font-semibold">{item.subject || t("无主题", "No subject")}</h2>
    <p className="break-words text-sm">{item.from} → {(item.to ?? []).join(", ")}</p>
    <div className="flex flex-wrap gap-2">{mailbox.can_organize && (folder === "trash" ? ["restore"] : folder === "archive" ? ["unarchive", "trash"] : ["archive", "trash"]).map(action => <ActionButton key={action} disabled={busy} onClick={() => run(async () => { await changeArchived(mailbox.mailbox.id, item.id, item.revision, action); onSelect(""); await list.mutate(); })}>{({ restore: t("恢复", "Restore"), unarchive: t("移回已发送", "Move to sent"), archive: t("归档", "Archive"), trash: t("移入回收站", "Move to trash") } as Record<string, string>)[action]}</ActionButton>)}
    {item.delivery_available ? <Link className="rounded border px-3 py-2 text-sm" href={`/mail?folder=receipts&message=${encodeURIComponent(item.id)}`}>{t("查看发送状态", "View delivery status")}</Link> : <span className="text-xs text-muted-foreground">{t("投递记录已清理，邮件内容仍保留。", "Delivery history was cleaned up; message content remains.")}</span>}</div>
    <SubmissionContentView key={item.id} id={item.id}/>
   </>}</div>
  </div><Pager page={page} total={list.data?.meta.total ?? 0} onPage={onPage}/>
 </div>;
}
