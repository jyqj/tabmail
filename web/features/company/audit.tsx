"use client";
import { useState } from "react";
import { useAPI } from "@/hooks/use-api";
import { request } from "@/lib/api/base";
import { useSessionScope } from "@/lib/session";
import type { APIListResponse } from "@/lib/types";
import type { CompanyAdminAudit } from "./api";
import { ActionButton, LoadError, useText } from "@/components/company/common";
import { Pager } from "@/features/mail/components/list-controls";
export function CompanyAudit() {
    const scope = useSessionScope();
    return <AuditSession key={scope}/>;
}

function AuditSession() {
    const t = useText();
    const [page, setPage] = useState(1);
    const list = useAPI(["company-audit", page], () => request<APIListResponse<CompanyAdminAudit>>("/api/v1/company/audit", { params: { page, per_page: 30 } }));
    const loading = list.isLoading || list.isValidating || (!list.data && !list.error);
    const ready = !loading && !list.error && !!list.data;
    const rows = ready ? list.data?.data ?? [] : [];
    const refresh = () => {
        if (!list.isValidating) void list.mutate().catch(() => undefined);
    };
    return <div className="space-y-3">
        <p className="text-sm text-muted-foreground">{t("仅展示本公司管理操作的人员、资源、时间和理由，不返回邮件正文或任意审计载荷。", "Only this company's administrative actor, resource, time and reason are exposed; message bodies and arbitrary audit payloads are excluded.")}</p>
        <ActionButton disabled={!ready} onClick={refresh}>{t("刷新", "Refresh")}</ActionButton>
        <fieldset disabled={list.isValidating}><LoadError error={list.error} onRetry={refresh}/></fieldset>
        {loading && <p role="status" className="text-sm text-muted-foreground">{t("正在加载审计记录…", "Loading audit records…")}</p>}
        {ready && rows.length === 0 && <p className="text-sm text-muted-foreground">{t("暂无审计记录", "No audit records")}</p>}
        {rows.map(e => <article key={e.id} className="space-y-1 rounded border p-3 text-sm">
            <h2 className="font-semibold">{e.action}</h2>
            <p className="break-all">{e.actor} · {e.resource_type} · {e.resource_id}</p>
            {e.reason && <p>{e.reason}</p>}
            <time className="text-xs text-muted-foreground">{new Date(e.created_at).toLocaleString()}</time>
        </article>)}
        {ready && <Pager page={page} total={list.data?.meta.total ?? 0} onPage={setPage}/>}
    </div>;
}
