"use client";
import { useState } from "react";
import { useAPI } from "@/hooks/use-api";
import { request } from "@/lib/api/base";
import type { APIListResponse } from "@/lib/types";
import type { CompanyAdminAudit } from "./api";
import { LoadError, useText } from "@/components/company/common";
import { Pager } from "@/features/mail/components/list-controls";
export function CompanyAudit() {
    const t = useText();
    const [page, setPage] = useState(1);
    const list = useAPI(["company-audit", page], () => request<APIListResponse<CompanyAdminAudit>>("/api/v1/company/audit", { params: { page, per_page: 30 } }));
    return <div className="space-y-3"><p className="text-sm text-muted-foreground">{t("仅展示本公司管理操作的人员、资源、时间和理由，不返回邮件正文或任意审计载荷。", "Only this company's administrative actor, resource, time and reason are exposed; message bodies and arbitrary audit payloads are excluded.")}</p><LoadError error={list.error} onRetry={() => void list.mutate()}/>{!list.error && (list.data?.data ?? []).map(e => <article key={e.id} className="space-y-1 rounded border p-3 text-sm"><h2 className="font-semibold">{e.action}</h2><p className="break-all">{e.actor} · {e.resource_type} · {e.resource_id}</p>{e.reason && <p>{e.reason}</p>}<time className="text-xs text-muted-foreground">{new Date(e.created_at).toLocaleString()}</time></article>)}<Pager page={page} total={list.data?.meta.total ?? 0} onPage={setPage}/></div>;
}
