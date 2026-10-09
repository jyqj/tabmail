"use client";
import { useLayoutEffect, useRef } from "react";
import { useSWRConfig } from "swr";
import { useAPI } from "@/hooks/use-api";
import { company, type WorkMailbox, type MailDraft } from "@/lib/company";
import { assertSession, useSessionScope } from "@/lib/session";
import { draftPage } from "../api";
import { ActionButton, useAction, useText } from "@/components/company/common";
import { ListFeedback, listReady, Pager } from "./list-controls";
export function DraftFolder({ page, mailboxes, onPage, onEdit }: {
    page: number;
    mailboxes: WorkMailbox[];
    onPage: (p: number) => void;
    onEdit: (pending: Promise<MailDraft>) => void | Promise<void>;
}) {
    const t = useText();
    const scope = useSessionScope();
    const { mutate } = useSWRConfig();
    const { busy, run } = useAction();
    const drafts = useAPI(["work-drafts", page], () => draftPage(page), { refreshInterval: 20000 });
    const view = useRef<object | null>(null);
    useLayoutEffect(() => {
        view.current = {};
        return () => { view.current = null; };
    }, [scope, page]);
    async function remove(draft: MailDraft) {
        const owner = view.current;
        try {
            if (!window.confirm(t("确认删除这份草稿？", "Delete this draft?"))) return;
            assertSession(scope);
            await company(`/drafts/${draft.id}`, { method: "DELETE", params: { revision: draft.revision } });
            assertSession(scope);
            // A successful deletion belongs to the submitted page, even if a
            // newer page is now mounted on this hook's bound mutate function.
            await mutate(["session", scope, ["work-drafts", page]]);
        } catch (error) {
            if (owner && view.current === owner) throw error;
        }
    }
    return <section className="space-y-3">
  <p className="text-sm text-muted-foreground">{t("我的可编辑草稿（跨邮箱）；封存草稿不会出现在此处。", "My editable drafts across mailboxes; sealed drafts are not exposed here.")}</p>
  <ListFeedback list={drafts} loading={t("正在加载草稿…", "Loading drafts…")} refreshing={t("正在刷新草稿…", "Refreshing drafts…")}
    empty={!drafts.data?.data.length ? t("没有草稿", "No drafts") : undefined}/>
  {!drafts.error && (drafts.data?.data ?? []).map(d => <article key={d.id} className="flex flex-wrap items-center justify-between gap-3 rounded border p-3"><div><h2 className="font-medium">{d.payload.subject || t("模板邮件 / 无主题", "Template / no subject")}</h2><p className="text-xs text-muted-foreground">{mailboxes.find(m => m.mailbox.id === d.mailbox_id)?.mailbox.full_address} · v{d.revision}</p></div><div className="flex gap-2">
    <ActionButton disabled={busy} onClick={() => run(async () => onEdit(company<MailDraft>(`/drafts/${d.id}`)))}>{t("继续编辑", "Continue editing")}</ActionButton>
    <ActionButton disabled={busy} onClick={() => run(() => remove(d))}>{t("删除草稿", "Delete draft")}</ActionButton>
   </div></article>)}
   {listReady(drafts) && <Pager page={page} total={drafts.data!.meta.total} onPage={onPage}/>}
 </section>;
}
