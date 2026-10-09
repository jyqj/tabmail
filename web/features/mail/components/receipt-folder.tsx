"use client";
import { useState } from "react";
import { useAPI } from "@/hooks/use-api";
import { submissions } from "@/lib/company";
import { useText } from "@/components/company/common";
import { useI18n } from "@/lib/i18n";
import { SubmissionPane } from "./submission-pane";
import { StatusBadge } from "./delivery-status";
import { ListFeedback, listReady, Pager } from "./list-controls";
import { LegacyReceiptFolder } from "./legacy-receipt-folder";
export function ReceiptFolder({ page, selected, onSelect, onPage, includeCompatibility = false }: {
    page: number;
    selected: string;
    onSelect: (id: string) => void;
    onPage: (p: number) => void;
    includeCompatibility?: boolean;
}) {
    const t = useText();
    const { t: receiptText } = useI18n();
    const [compatibility, setCompatibility] = useState(false);
    const list = useAPI(["work-submissions", page], () => submissions(page), { refreshInterval: 10000 });
    return <section className="space-y-3">
  {includeCompatibility && <nav aria-label={t("回执入口", "Receipt entry points")} className="flex gap-3">
    <button type="button" aria-pressed={!compatibility} onClick={() => setCompatibility(false)}>{t("公司提交回执", "Company receipts")}</button>
    <button type="button" aria-pressed={compatibility} onClick={() => setCompatibility(true)}>{t("兼容任务回执", "Compatibility receipts")}</button>
  </nav>}
  {compatibility ? <LegacyReceiptFolder /> : <>
  <p className="text-sm text-muted-foreground">{t("范围：我的提交，以及当前有阅读权邮箱的提交（跨邮箱）。下一跳接受不等于收件人已读；不确定结果需运维核查。", "My submissions and submissions from readable mailboxes, across mailboxes. Next-hop acceptance is not a read receipt; uncertain outcomes need operator review.")}</p>
  <ListFeedback list={list} loading={t("正在加载发送记录…", "Loading submissions…")} refreshing={t("正在刷新发送记录…", "Refreshing submissions…")}
    empty={!list.data?.data.length ? t("本页没有发送记录", "No submissions on this page") : undefined}/>
  {!list.error && (list.data?.data ?? []).map(s => <article key={s.id} className="rounded border p-3"><button className="w-full text-left" onClick={() => onSelect(s.id === selected ? "" : s.id)}><h2 className="font-medium">{receiptText("ordinaryReceipt.task")}: {s.id}</h2><p className="text-sm"><StatusBadge status={s.status}/></p>{s.created_at && <time className="text-xs">{new Date(s.created_at).toLocaleString()}</time>}</button>{selected === s.id && <SubmissionPane id={s.id}/>}</article>)}
  {selected && !list.error && !list.data?.data.some(s => s.id === selected) && <SubmissionPane id={selected}/>}
  {listReady(list) && <Pager page={page} total={list.data!.meta.total} onPage={onPage}/>}
  </>}
 </section>;
}
