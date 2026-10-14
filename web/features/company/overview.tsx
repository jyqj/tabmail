"use client";
import Link from "next/link";
import { useLayoutEffect, useRef, useState } from "react";
import { toast } from "sonner";
import { useAPI } from "@/hooks/use-api";
import { company, errorText } from "@/lib/company";
import { sessionScope, useSessionScope } from "@/lib/session";
import { ActionButton, Field, inputClass, LoadError, useText } from "@/components/company/common";
import type { CompanyOverview } from "./api";

function requeuedCount(value: unknown): number | null {
    if (!value || typeof value !== "object" || Array.isArray(value)) return null;
    const receipt = value as Record<string, unknown>;
    return typeof receipt.requeued === "number" && Number.isInteger(receipt.requeued) &&
        receipt.requeued >= 0 && receipt.requeued <= 100 && receipt.limit === 100
        ? receipt.requeued : null;
}

export function Overview() {
    const scope = useSessionScope();
    return <OverviewSession key={scope} scope={scope}/>;
}

function OverviewSession({ scope }: { scope: string }) {
    const t = useText();
    const [reason, setReason] = useState("");
    const [busy, setBusy] = useState(false);
    const [refreshing, setRefreshing] = useState(false);
    const [needsReview, setNeedsReview] = useState(false);
    const [summaryDenied, setSummaryDenied] = useState(false);
    const [acknowledged, setAcknowledged] = useState<number | null>(null);
    const [recoveryError, setRecoveryError] = useState<Error | null>(null);
    const lifetime = useRef<object | null>(null);
    const activeWrite = useRef<object | null>(null);
    const activeRead = useRef<object | null>(null);
    useLayoutEffect(() => {
        lifetime.current = {};
        return () => { lifetime.current = null; };
    }, []);
    const owns = (owner: object | null) => owner !== null && lifetime.current === owner && scope === sessionScope();
    const data = useAPI("company-overview", () => company<CompanyOverview>("/overview"), { refreshInterval: 15000 });
    const reasonBytes = new TextEncoder().encode(reason.trim()).length;
    const validReason = reasonBytes >= 8 && reasonBytes <= 1000;
    const readbackFailure = (count: number | null) => new Error(count !== null
        ? t(`已重新排队 ${count} 项索引任务，但公司概览刷新失败。请重试加载以核对当前状态。`,
            `${count} index jobs were requeued, but the company summary could not be refreshed. Retry loading to check the current state.`)
        : t("公司概览刷新失败。请重试加载并核对当前状态后，再决定是否重试索引恢复。",
            "The company summary could not be refreshed. Retry loading and review the current state before deciding whether to retry index recovery."));
    async function refreshSummary(owner: object | null, count: number | null) {
        if (!owns(owner) || activeRead.current) return;
        const operation = {};
        activeRead.current = operation;
        setRefreshing(true);
        try {
            // An ordinary SWR revalidation can resolve stale cached data on
            // failure. The actual GET must succeed before clearing review.
            await data.mutate(async () => {
                const current = await company<CompanyOverview>("/overview");
                if (!owns(owner)) throw new DOMException("Overview changed", "AbortError");
                return current;
            }, { revalidate: false });
            if (!owns(owner)) return;
            setRecoveryError(null);
            setSummaryDenied(false);
            setNeedsReview(false);
            setAcknowledged(null);
        } catch (error) {
            if (!owns(owner)) return;
            setRecoveryError(readbackFailure(count));
            const code = (error as { error?: { code?: unknown } } | null)?.error?.code;
            if (code === "FORBIDDEN" || code === "UNAUTHORIZED") {
                setSummaryDenied(true);
                // Rejected reads must also retire SWR's old value so a same-
                // session remount cannot display it before fresh authorization.
                await data.mutate(undefined, { revalidate: false });
            }
        } finally {
            if (activeRead.current === operation) {
                activeRead.current = null;
                if (owns(owner)) setRefreshing(false);
            }
        }
    }
    async function recoverIndexes() {
        const owner = lifetime.current;
        if (!owns(owner) || activeWrite.current || activeRead.current || needsReview || summaryDenied ||
            !validReason || !data.data?.index_failed || data.error) return;
        const operation = {};
        activeWrite.current = operation;
        setBusy(true);
        try {
            const response = await company<unknown>("/index/retry", { method: "POST", body: { reason: reason.trim() } });
            if (!owns(owner)) return;
            const count = requeuedCount(response);
            setNeedsReview(true);
            if (count === null) {
                setAcknowledged(null);
                setRecoveryError(new Error(t(
                    "无法确认索引恢复结果。请重新加载公司概览并核对后，再决定是否重试。",
                    "Could not confirm the index recovery result. Reload the company summary and review it before deciding whether to retry.",
                )));
                return;
            }
            setAcknowledged(count);
            setReason("");
            toast.success(t(`已重新排队 ${count} 项索引任务`, `Requeued ${count} index jobs`));
            await refreshSummary(owner, count);
        } catch (error) {
            if (!owns(owner)) return;
            setNeedsReview(true);
            setAcknowledged(null);
            setRecoveryError(new Error(t(
                "无法确认索引恢复请求。请重新加载公司概览并核对后，再决定是否重试。",
                "Could not confirm the index recovery request. Reload the company summary and review it before deciding whether to retry.",
            )));
            toast.error(errorText(error));
        } finally {
            if (activeWrite.current === operation) {
                activeWrite.current = null;
                if (owns(owner)) setBusy(false);
            }
        }
    }
    const labels: Record<keyof CompanyOverview, string> = { mailboxes: t("公司邮箱", "Mailboxes"), active_employees: t("启用成员", "Active employees"), pending_invitations: t("待激活邀请", "Pending invitations"), queued: t("待完成投递", "Queued/in-flight deliveries"), uncertain: t("需核查的结果", "Uncertain results"), index_failed: t("正文索引失败", "Failed body indexes") };
    return <div className="space-y-5"><fieldset disabled={busy || refreshing}><LoadError error={recoveryError || data.error} onRetry={() => {
        if (!busy) void refreshSummary(lifetime.current, acknowledged);
    }}/></fieldset>{(data.isLoading || refreshing) && <p role="status" className="text-sm text-muted-foreground">{t("正在加载公司概览…", "Loading company summary…")}</p>}{!summaryDenied && !data.error && data.data && <dl className="grid grid-cols-2 gap-4 lg:grid-cols-3">{(Object.keys(labels) as (keyof CompanyOverview)[]).map(key => <div key={key} className="rounded-xl border p-5"><dt className="text-sm text-muted-foreground">{labels[key]}</dt><dd className="text-3xl font-semibold">{data.data![key]}</dd></div>)}</dl>}
 {!summaryDenied && Boolean(data.data?.index_failed) && !data.error && <section className="space-y-3 rounded-xl border p-4">
 <h2 className="font-medium">{t("恢复正文索引", "Recover body indexing")}</h2>
 <p className="text-sm">{t("只重试本公司失败的派生索引，每次最多100项；不重新收信、不重发邮件，也不授予正文阅读权。", "Retry only this company's failed derived indexes, up to 100 per action. No mail is received again or resent, and no content access is granted.")}</p>
 <Field label={t("恢复原因", "Recovery reason")}>{id => <input id={id} className={inputClass} value={reason} maxLength={1000} disabled={busy || refreshing} onChange={e=>setReason(e.target.value)} />}</Field>
 <ActionButton disabled={busy || refreshing || needsReview || !validReason} onClick={() => void recoverIndexes()}>{t("重试失败索引", "Retry failed indexes")}</ActionButton>
 </section>}
 <p className="text-sm text-muted-foreground">{t("这里只展示本公司运行摘要，不授予任何员工邮件正文访问权。投递不确定时联系平台运维核查；持续失败的原件需排查解析或存储问题。", "This tenant-only operational summary grants no employee-content access. Contact operators about uncertain delivery or persistently unparseable sources.")}</p>
 <div className="grid gap-3 md:grid-cols-2">{[["domains", t("配置域名与公司发送策略", "Configure domains and company sending policy")], ["employees", t("邀请员工与离职交接", "Invite employees and manage offboarding")], ["mailboxes", t("分配邮箱与解释权限", "Assign mailboxes and explain access")], ["templates", t("管理发布模板", "Manage published templates")], ["audit", t("查看公司管理审计", "Review company administration audit")]].map(([path, label]) => <Link key={path} className="rounded border p-4 hover:bg-muted" href={`/company/${path}`}>{label}</Link>)}</div></div>;
}
