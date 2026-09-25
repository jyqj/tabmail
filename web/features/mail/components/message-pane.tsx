"use client";
import { useState } from "react";
import { useAPI } from "@/hooks/use-api";
import { company, workMessage, workPath, downloadCompanyFile, type DraftPayload, type WorkMailbox, type InboundAttachment } from "@/lib/company";
import { composeIdentity } from "@/lib/compose-identity";
import { ActionButton, LoadError, MailHTML, Section, useAction, useText } from "@/components/company/common";
import { ConversationList } from "./conversation-list";
export function MessagePane({ mailbox, mailboxes, id, onMutation, onCompose, }: {
    mailbox: WorkMailbox;
    /** Send-identity data source; defaults to the current mailbox only. */
    mailboxes?: WorkMailbox[];
    id: string;
    onMutation: () => void;
    onCompose: (p: DraftPayload) => void;
}) {
    const t = useText();
    const { busy, run } = useAction();
    const [conversation, setConversation] = useState(false);
    // Reply/forward only needs some sendable identity to exist — the compose
    // view owns the identity switch. The server re-authorizes every send.
    const composeFrom = composeIdentity(mailboxes ?? [mailbox], mailbox);
    const canCompose = Boolean(composeFrom);
    const detail = useAPI(["work-message", mailbox.mailbox.id, id], () => workMessage(mailbox.mailbox.id, id), { refreshInterval: 15000 });
    const base = `${workPath(mailbox.mailbox.id)}/messages/${id}`;
    const files = useAPI(["inbound-attachments", mailbox.mailbox.id, id], () => company<InboundAttachment[]>(`${base}/attachments`));
    async function act(action: string) {
        await company(`${base}/actions`, { method: "POST", body: { action } });
        onMutation();
    }
    return (<Section title={detail.data?.subject || t("邮件详情", "Message details")}>
      <LoadError error={detail.error} onRetry={() => void detail.mutate()}/>
      {detail.data && !detail.error && (<>
          <p className="text-sm break-words">
            {detail.data.starred ? "★ " : ""}
            {detail.data.sender} → {(detail.data.recipients ?? []).join(", ")}
          </p>
          <div className="flex flex-wrap gap-2">
            {(["reply", "reply_all", "forward"] as const).map((mode, i) => (<ActionButton key={mode} disabled={busy || !canCompose} onClick={() => run(async () => onCompose(await company<DraftPayload>(`${base}/compose`, {
                    method: "POST",
                    body: { mode, from_mailbox_id: composeFrom!.mailbox.id },
                })))}>
                {[
                    t("回复", "Reply"),
                    t("回复全部", "Reply all"),
                    t("转发", "Forward"),
                ][i]}
              </ActionButton>))}
            <ActionButton disabled={busy} onClick={() => run(() => downloadCompanyFile(`${base}/source`, "message.eml"))}>
              {t("下载原件", "Download original")}
            </ActionButton>
          </div>
          <pre className="whitespace-pre-wrap break-words text-sm leading-7">
            {detail.data.text_body}
          </pre>
          {detail.data.html_body && (<details>
              <summary className="cursor-pointer">
                {t("安全 HTML 视图（外部资源已阻止）", "Safe HTML view (external resources blocked)")}
              </summary>
              <MailHTML html={detail.data.html_body}/>
            </details>)}
          <LoadError error={files.error} onRetry={() => void files.mutate()}/>
          {(!files.error ? files.data ?? [] : []).map((f) => (<ActionButton key={f.id || f.index} disabled={busy} onClick={() => run(() => downloadCompanyFile(f.id ? `${base}/parts/${encodeURIComponent(f.id)}` : `${base}/attachments/${f.index}`, f.filename))}>
              {f.filename} · {Math.ceil(f.size / 1024)} KiB
            </ActionButton>))}
          <ActionButton aria-expanded={conversation} onClick={() => setConversation(!conversation)}>{t("同邮箱会话", "Conversation in this mailbox")}</ActionButton>
          {conversation && <ConversationList mailbox={mailbox} message={id}/>}
          {/* Personal per-user state: any reader may toggle seen/starred.
                The server still re-authorizes every action call. */}
          {mailbox.can_read && (<div className="flex flex-wrap gap-2 border-t pt-3">
              <ActionButton disabled={busy} onClick={() => run(() => act(detail.data!.seen ? "unseen" : "seen"))}>
                {detail.data.seen
                    ? t("标为未读", "Mark unread")
                    : t("标为已读", "Mark read")}
              </ActionButton>
              <ActionButton disabled={busy} onClick={() => run(() => act(detail.data!.starred ? "unstarred" : "starred"))}>
                {detail.data.starred
                    ? t("取消星标", "Unstar")
                    : t("加星标", "Star")}
              </ActionButton>
            </div>)}
          {/* Shared-folder organizing (archive/trash/restore) stays behind
                the organizer grant. */}
          {mailbox.can_organize && (<div className="flex flex-wrap gap-2 border-t pt-3">
              {detail.data.deleted_at ? (<ActionButton disabled={busy} onClick={() => run(() => act("restore"))}>
                  {t("恢复邮件", "Restore")}
                </ActionButton>) : (<>
                  <ActionButton disabled={busy} onClick={() => run(() => act(detail.data!.archived_at ? "unarchive" : "archive"))}>
                    {detail.data.archived_at
                        ? t("移回收件箱", "Move to inbox")
                        : t("归档", "Archive")}
                  </ActionButton>
                  <ActionButton disabled={busy} onClick={() => run(() => act("trash"))}>
                    {t("移到回收站", "Move to trash")}
                  </ActionButton>
                </>)}
            </div>)}
        </>)}
    </Section>);
}
