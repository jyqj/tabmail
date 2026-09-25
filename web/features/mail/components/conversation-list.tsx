"use client";
import Link from "next/link";
import { useAPI } from "@/hooks/use-api";
import { request } from "@/lib/api/base";
import type { WorkMailbox } from "@/lib/company";
import type { APIListResponse, Message } from "@/lib/types";
import { LoadError, useText } from "@/components/company/common";
export function ConversationList({ mailbox, message }: {
    mailbox: WorkMailbox;
    message: string;
}) {
    const t = useText();
    const list = useAPI(["mail-conversation", mailbox.mailbox.id, message], () => request<APIListResponse<Message>>(`/api/v1/company/mailboxes/${encodeURIComponent(mailbox.mailbox.id)}/messages/${encodeURIComponent(message)}/conversation`, { params: { page: 1, per_page: 100 } }));
    return <div className="space-y-2 rounded border p-3 text-sm">
   <p>{t("按邮件引用头关联，仅显示当前邮箱内可读、未删除的邮件。未索引部分可能尚未出现。", "Reference-header matches within this readable mailbox only. Unindexed messages may be missing.")}</p>
   <LoadError error={list.error} onRetry={() => void list.mutate()}/>
   {!list.error && (list.data?.data ?? []).map(m => <p key={m.id}><Link className="underline" href={`/mail?mailbox=${encodeURIComponent(mailbox.mailbox.id)}&message=${encodeURIComponent(m.id)}`}>{m.subject || t("无主题", "No subject")} · {m.sender}</Link></p>)}
   {(list.data?.meta.total ?? 0) > 100 && <p>{t("当前显示前 100 封，请使用邮箱搜索查找其余邮件。", "Showing the first 100. Use mailbox search to find the remaining messages.")}</p>}
 </div>;
}
