"use client";
import { useEffect, useState } from "react";
import { useSWRConfig } from "swr";
import { toast } from "sonner";
import { useAPI } from "@/hooks/use-api";
import { request } from "@/lib/api/base";
import type { APIError } from "@/lib/types";
import { submission } from "@/lib/company";
import { useSessionScope } from "@/lib/session";
import { ActionButton, LoadError, useAction, useText } from "@/components/company/common";
import { StatusBadge } from "./delivery-status";
import { SubmissionContentView } from "./submission-content";
const RECIPIENT_STATE_LABELS: Record<string, [
    string,
    string
]> = {
    pending: ["待处理", "Pending"],
    accepted: ["已接受", "Accepted"],
    temporary: ["临时失败", "Temporary failure"],
    permanent: ["永久失败", "Permanent failure"],
    uncertain: ["结果不确定", "Uncertain"],
};
export function SubmissionPane({ id }: {
    id: string;
}) {
    const t = useText();
    const { busy, run } = useAction();
    const detail = useAPI(["submission", id], () => submission(id), {
        refreshInterval: 10000,
    });
    const s = detail.error ? undefined : detail.data;
    const canViewContent = Boolean(s && !s.content_redacted && s.capabilities?.view_content !== false);
    const scope = useSessionScope();
    const { mutate: mutateCache } = useSWRConfig();
    useEffect(() => {
        if (canViewContent)
            return;
        for (const key of ["submission-content", "submission-attachments"]) {
            // Invalidate request deduplication too; clearing data alone can leave a
            // just-regranted disclosure stuck on a discarded pre-revocation promise.
            void mutateCache(["session", scope, [key, id]], undefined, { revalidate: true });
        }
    }, [canViewContent, id, scope, mutateCache]);
    return (<div className="mt-4 space-y-3 rounded-md border p-4">
      <LoadError error={detail.error} onRetry={() => void detail.mutate()}/>
      {s && (<>
          <p className="text-sm break-words">
            {s.from} →{" "}
            {(s.recipients ?? []).map((v) => v.address).join(", ") || "—"}
          </p>
          <p className="text-sm">
            <StatusBadge status={s.status}/>
          </p>
          <p className="text-xs text-muted-foreground">
            {t("附件", "Attachments")}: {s.attachment_count}
            {s.draft_consumed
                ? ` · ${t("来自草稿", "from a draft")}`
                : ""}
            {s.template_version_id
                ? ` · ${t("模板版本", "Template version")}: ${s.template_version_id}`
                : ""}
          </p>
          {/* capabilities is an interaction hint, not a credential; absent on
                stale cached data keeps the current behavior. The content
                endpoint re-checks read access on every call. */}
          {canViewContent && <SubmissionContentDisclosure id={id}/>}
          <div className="overflow-x-auto">
            <table className="w-full text-left text-sm">
              <thead>
                <tr>
                  <th>{t("收件人", "Recipient")}</th>
                  <th>{t("结果", "Outcome")}</th>
                </tr>
              </thead>
              <tbody>
                {(s.recipients ?? []).map((v) => {
                const l = RECIPIENT_STATE_LABELS[v.state] ?? [
                    v.state,
                    v.state,
                ];
                return (<tr key={v.address}>
                      <td className="py-2 break-all">{v.address}</td>
                      <td>{t(l[0], l[1])}</td>
                    </tr>);
            })}
              </tbody>
            </table>
          </div>
          {/* capabilities.retry is the interaction hint; when absent (old
                cached data) fall back to the status heuristic. The retry POST
                re-authorizes server-side either way. */}
          {(s.capabilities
                ? s.capabilities.retry
                : s.status === "needs_attention") && (<ActionButton disabled={busy} onClick={() => run(async () => {
                    try {
                        await request(`/api/v1/outbound/${id}/retry`, {
                            method: "POST",
                            body: {},
                        });
                        toast.success(t("已请求安全重试", "Safe retry requested"));
                        void detail.mutate();
                    }
                    catch (e) {
                        if ((e as APIError)?.error?.code === "CONFLICT") {
                            const reason = (e as APIError)?.error?.reason;
                            toast.error(reason === "state_changed"
                                ? t("任务状态已变化，请刷新后查看。", "The task state has changed; refresh to see the latest status.")
                                : t("结果不确定，已禁止重试。请到恢复中心核实下一跳记录后再处理。", "Uncertain outcome: retry is blocked. Review next-hop evidence in the recovery center."));
                            return;
                        }
                        throw e;
                    }
                })}>
              {t("重试未成功目标（服务端重新鉴权）", "Retry unfinished recipients (re-authorized by server)")}
            </ActionButton>)}
          {s.delivery_uncertain && (<p role="status" className="text-sm">
              {t("结果不确定，已禁止重试。请联系平台运维核实下一跳记录后再处理。", "Uncertain outcome: retry is blocked. Ask an operator to verify next-hop evidence.")}
            </p>)}
        </>)}
    </div>);
}
// The disclosure owns its state so capability revocation unmounts and resets
// it. Re-granting access never reopens a previously expanded body implicitly.
function SubmissionContentDisclosure({ id }: {
    id: string;
}) {
    const t = useText();
    const [open, setOpen] = useState(false);
    return <>
    <ActionButton aria-expanded={open} onClick={() => setOpen((v) => !v)}>
      {open ? t("收起内容", "Hide content") : t("查看内容", "View content")}
    </ActionButton>
    {open && <SubmissionContentView id={id}/>}
  </>;
}
