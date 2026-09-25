"use client";
import Link from "next/link";
import { useAPI } from "@/hooks/use-api";
import { company } from "@/lib/company";
import { LoadError, useText } from "@/components/company/common";
import type { CompanyOverview } from "./api";
export function Overview() {
    const t = useText();
    const data = useAPI("company-overview", () => company<CompanyOverview>("/overview"), { refreshInterval: 15000 });
    const labels: Record<keyof CompanyOverview, string> = { mailboxes: t("公司邮箱", "Mailboxes"), active_employees: t("启用成员", "Active employees"), pending_invitations: t("待激活邀请", "Pending invitations"), queued: t("待完成投递", "Queued/in-flight deliveries"), uncertain: t("需核查的结果", "Uncertain results"), index_failed: t("正文索引失败", "Failed body indexes") };
    return <div className="space-y-5"><LoadError error={data.error} onRetry={() => void data.mutate()}/>{!data.error && data.data && <dl className="grid grid-cols-2 gap-4 lg:grid-cols-3">{(Object.keys(labels) as (keyof CompanyOverview)[]).map(key => <div key={key} className="rounded-xl border p-5"><dt className="text-sm text-muted-foreground">{labels[key]}</dt><dd className="text-3xl font-semibold">{data.data![key]}</dd></div>)}</dl>}
 <p className="text-sm text-muted-foreground">{t("这里只展示本公司运行摘要，不授予任何员工邮件正文访问权。投递不确定或索引失败时请联系平台运维核查。", "This tenant-only operational summary grants no employee-content access. Contact operators about uncertain delivery or failed indexing.")}</p>
 <div className="grid gap-3 md:grid-cols-2">{[["domains", t("配置域名与公司发送策略", "Configure domains and company sending policy")], ["employees", t("邀请员工与离职交接", "Invite employees and manage offboarding")], ["mailboxes", t("分配邮箱与解释权限", "Assign mailboxes and explain access")], ["templates", t("管理发布模板", "Manage published templates")], ["audit", t("查看公司管理审计", "Review company administration audit")]].map(([path, label]) => <Link key={path} className="rounded border p-4 hover:bg-muted" href={`/company/${path}`}>{label}</Link>)}</div></div>;
}
