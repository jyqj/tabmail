"use client";
import Link from "next/link";
import { useState } from "react";
import { toast } from "sonner";
import { useAPI } from "@/hooks/use-api";
import { company } from "@/lib/company";
import { ActionButton, Field, inputClass, LoadError, useAction, useText } from "@/components/company/common";
import type { CompanyOverview } from "./api";
export function Overview() {
    const t = useText();
    const { busy, run } = useAction();
    const [reason, setReason] = useState("");
    const data = useAPI("company-overview", () => company<CompanyOverview>("/overview"), { refreshInterval: 15000 });
    const labels: Record<keyof CompanyOverview, string> = { mailboxes: t("公司邮箱", "Mailboxes"), active_employees: t("启用成员", "Active employees"), pending_invitations: t("待激活邀请", "Pending invitations"), queued: t("待完成投递", "Queued/in-flight deliveries"), uncertain: t("需核查的结果", "Uncertain results"), index_failed: t("正文索引失败", "Failed body indexes") };
    return <div className="space-y-5"><LoadError error={data.error} onRetry={() => void data.mutate()}/>{!data.error && data.data && <dl className="grid grid-cols-2 gap-4 lg:grid-cols-3">{(Object.keys(labels) as (keyof CompanyOverview)[]).map(key => <div key={key} className="rounded-xl border p-5"><dt className="text-sm text-muted-foreground">{labels[key]}</dt><dd className="text-3xl font-semibold">{data.data![key]}</dd></div>)}</dl>}
 {Boolean(data.data?.index_failed) && !data.error && <section className="space-y-3 rounded-xl border p-4">
 <h2 className="font-medium">{t("恢复正文索引", "Recover body indexing")}</h2>
 <p className="text-sm">{t("只重试本公司失败的派生索引，每次最多100项；不重新收信、不重发邮件，也不授予正文阅读权。", "Retry only this company's failed derived indexes, up to 100 per action. No mail is received again or resent, and no content access is granted.")}</p>
 <Field label={t("恢复原因", "Recovery reason")}>{id => <input id={id} className={inputClass} value={reason} maxLength={1000} disabled={busy} onChange={e=>setReason(e.target.value)} />}</Field>
 <ActionButton disabled={busy || new TextEncoder().encode(reason.trim()).length < 8 || new TextEncoder().encode(reason.trim()).length > 1000} onClick={()=>run(async()=>{
  const result=await company<{requeued:number;limit:number}>("/index/retry",{method:"POST",body:{reason:reason.trim()}});
  toast.success(t(`已重新排队 ${result.requeued} 项索引任务`, `Requeued ${result.requeued} index jobs`));
  setReason("");await data.mutate();
 })}>{t("重试失败索引", "Retry failed indexes")}</ActionButton>
 </section>}
 <p className="text-sm text-muted-foreground">{t("这里只展示本公司运行摘要，不授予任何员工邮件正文访问权。投递不确定时联系平台运维核查；持续失败的原件需排查解析或存储问题。", "This tenant-only operational summary grants no employee-content access. Contact operators about uncertain delivery or persistently unparseable sources.")}</p>
 <div className="grid gap-3 md:grid-cols-2">{[["domains", t("配置域名与公司发送策略", "Configure domains and company sending policy")], ["employees", t("邀请员工与离职交接", "Invite employees and manage offboarding")], ["mailboxes", t("分配邮箱与解释权限", "Assign mailboxes and explain access")], ["templates", t("管理发布模板", "Manage published templates")], ["audit", t("查看公司管理审计", "Review company administration audit")]].map(([path, label]) => <Link key={path} className="rounded border p-4 hover:bg-muted" href={`/company/${path}`}>{label}</Link>)}</div></div>;
}
