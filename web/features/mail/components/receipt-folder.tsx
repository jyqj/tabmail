"use client";
import { useAPI } from "@/hooks/use-api";
import { submissions } from "@/lib/company";
import { LoadError, useText } from "@/components/company/common";
import { SubmissionPane } from "./submission-pane";
import { StatusBadge } from "./delivery-status";
import { Pager } from "./list-controls";
export function ReceiptFolder({ page, selected, onSelect, onPage }: {
    page: number;
    selected: string;
    onSelect: (id: string) => void;
    onPage: (p: number) => void;
}) {
    const t = useText();
    const list = useAPI(["work-submissions", page], () => submissions(page), { refreshInterval: 10000 });
    return <section className="space-y-3">
  <p className="text-sm text-muted-foreground">{t("范围：我的提交，以及当前有阅读权邮箱的提交（跨邮箱）。下一跳接受不等于收件人已读；不确定结果需运维核查。", "My submissions and submissions from readable mailboxes, across mailboxes. Next-hop acceptance is not a read receipt; uncertain outcomes need operator review.")}</p>
  <LoadError error={list.error} onRetry={() => void list.mutate()}/>
  {!list.error && (list.data?.data ?? []).map(s => <article key={s.id} className="rounded border p-3"><button className="w-full text-left" onClick={() => onSelect(s.id === selected ? "" : s.id)}><h2 className="font-medium">{s.subject || t("无主题", "No subject")}</h2><p className="text-sm">{s.from} · <StatusBadge status={s.status}/></p><time className="text-xs">{new Date(s.created_at).toLocaleString()}</time></button>{selected === s.id && <SubmissionPane id={s.id}/>}</article>)}
  {selected && !list.error && !list.data?.data.some(s => s.id === selected) && <SubmissionPane id={selected}/>}
  {!list.isLoading && !list.error && !list.data?.data.length && !selected && <p>{t("没有发送记录", "No submissions")}</p>}
  <Pager page={page} total={list.data?.meta.total ?? 0} onPage={onPage}/>
 </section>;
}
