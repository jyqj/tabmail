"use client";
import { useAPI } from "@/hooks/use-api";
import { company, type WorkMailbox, type MailDraft } from "@/lib/company";
import { draftPage } from "../api";
import { ActionButton, LoadError, useAction, useText } from "@/components/company/common";
import { Pager } from "./list-controls";
export function DraftFolder({ page, mailboxes, onPage, onEdit }: {
    page: number;
    mailboxes: WorkMailbox[];
    onPage: (p: number) => void;
    onEdit: (d: MailDraft) => void;
}) {
    const t = useText();
    const { busy, run } = useAction();
    const drafts = useAPI(["work-drafts", page], () => draftPage(page), { refreshInterval: 20000 });
    return <section className="space-y-3">
  <p className="text-sm text-muted-foreground">{t("我的可编辑草稿（跨邮箱）；封存草稿不会出现在此处。", "My editable drafts across mailboxes; sealed drafts are not exposed here.")}</p>
  <LoadError error={drafts.error} onRetry={() => void drafts.mutate()}/>
  {!drafts.error && (drafts.data?.data ?? []).map(d => <article key={d.id} className="flex flex-wrap items-center justify-between gap-3 rounded border p-3"><div><h2 className="font-medium">{d.payload.subject || t("模板邮件 / 无主题", "Template / no subject")}</h2><p className="text-xs text-muted-foreground">{mailboxes.find(m => m.mailbox.id === d.mailbox_id)?.mailbox.full_address} · v{d.revision}</p></div><div className="flex gap-2">
    <ActionButton disabled={busy} onClick={() => run(async () => onEdit(await company<MailDraft>(`/drafts/${d.id}`)))}>{t("继续编辑", "Continue editing")}</ActionButton>
    <ActionButton disabled={busy} onClick={() => run(async () => { if (!window.confirm(t("确认删除这份草稿？", "Delete this draft?")))
            return; await company(`/drafts/${d.id}`, { method: "DELETE", params: { revision: d.revision } }); await drafts.mutate(); })}>{t("删除草稿", "Delete draft")}</ActionButton>
   </div></article>)}
   {!drafts.isLoading && !drafts.error && !drafts.data?.data.length && <p>{t("没有草稿", "No drafts")}</p>}
   <Pager page={page} total={drafts.data?.meta.total ?? 0} onPage={onPage}/>
 </section>;
}
