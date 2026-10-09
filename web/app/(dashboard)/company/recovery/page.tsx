"use client";
import { useRef, useState } from "react";
import { toast } from "sonner";
import { useAPI } from "@/hooks/use-api";
import { request } from "@/lib/api/base";
import type { APIListResponse, APIResponse } from "@/lib/types";
import { company, type Receipt } from "@/lib/company";
import { useAuth } from "@/contexts/auth-context";
import { isConflict } from "@/lib/error-code";
import {
  isInspectionID,
  parseOutboundInspection,
  validInspectionReason,
  type OutboundInspection,
} from "@/lib/outbound-inspection-types";
import {
  ActionButton,
  Field,
  inputClass,
  LoadError,
  Section,
  useAction,
  useText,
} from "@/components/company/common";
export default function RecoveryPage() {
  const t = useText();
  const auth = useAuth();
  const canInspect = auth.hydrated && auth.level === "super_admin" && isInspectionID(auth.tenantId);
  const { busy, run } = useAction();
  const [page, setPage] = useState(1);
  const receipts = useAPI(["recovery-receipts", page], () =>
    request<APIListResponse<Receipt>>("/api/v1/company/recovery", {
      params: { page, per_page: 30 },
    }),
  );
  const queueLoading = receipts.isLoading || receipts.isValidating;
  const queueAvailable = !!receipts.data && !receipts.error;
  const receiptRows = queueAvailable ? (receipts.data?.data ?? []) : [];
  const runtime = useAPI("runtime-config", () =>
    request<APIResponse<Record<string, unknown>>>(
      "/api/v1/admin/runtime-config",
    ),
  );
  const [inspection, setInspection] = useState<{
    receipt: Receipt;
    original_valid: boolean;
  } | null>(null);
  const [targets, setTargets] = useState<string[]>([]);
  const [inboundReviewRequired, setInboundReviewRequired] = useState(false);
  const [reason, setReason] = useState("");
  const [jobId, setJobId] = useState("");
  const [jobInspection, setJobInspection] = useState<OutboundInspection | null>(null);
  const [outboundReviewRequired, setOutboundReviewRequired] = useState(false);
  // Target edits fence in-page requests. Account/company/session changes use
  // the existing request lease and AuthProvider's session-keyed subtree.
  const inspectionVersion = useRef(0);
  const [outcomes, setOutcomes] = useState<Record<string, string>>({});
  const recipientStateLabel = (state: string) => ({
    pending: t("待处理", "Pending"),
    accepted: t("下一跳已接受（不代表最终送达）", "Next-hop accepted (not final delivery)"),
    temporary: t("临时拒绝", "Temporary rejection"),
    permanent: t("永久拒绝", "Permanent rejection"),
    uncertain: t("结果不确定", "Uncertain outcome"),
  }[state] ?? t("未知类别，需核查", "Unknown category; review required"));
  const jobStatusLabel = (status: string) => ({
    submitted: t("已提交", "Submitted"),
    cancelled: t("已取消", "Cancelled"),
    sending: t("发送中", "Sending"),
    partially_accepted: t("部分目标获下一跳接受（不代表最终送达）", "Partially next-hop accepted (not final delivery)"),
    accepted: recipientStateLabel("accepted"),
    needs_attention: t("需要核查", "Needs attention"),
  }[status] ?? t("未知类别，需核查", "Unknown category; review required"));
  const recipientKindLabel = (kind: string) => ({
    to: t("收件人", "To"), cc: t("抄送", "Cc"), bcc: t("密送（受审计例外）", "Bcc (audited exception)"),
    envelope: t("信封收件人", "Envelope recipient"),
  }[kind] ?? t("未知类别，需核查", "Unknown category; review required"));
  return (
    <main className="mx-auto w-full max-w-6xl space-y-6 p-4 md:p-7">
      <header>
        <h1 className="text-2xl font-semibold">
          {t("故障恢复与受审计检查", "Recovery and audited inspection")}
        </h1>
        <p className="mt-2 text-sm text-muted-foreground">
          {t(
            "仅平台管理员可操作。入站内容和已接受目标不可修改；不确定出站结果必须先核对下一跳记录。",
            "Platform administrators only. Accepted inbound content and targets are immutable; uncertain outbound results require next-hop evidence.",
          )}
        </p>
      </header>
      <Field
        label={t(
          "本次检查 / 恢复原因（去除首尾空白后 8–1000 UTF-8 字节）",
          "Inspection / recovery reason (8–1000 UTF-8 bytes after trimming)",
        )}
      >
        {(id) => (
          <textarea
            id={id}
            className={inputClass}
            value={reason}
            maxLength={1000}
            rows={2}
            onChange={(e) => setReason(e.target.value)}
          />
        )}
      </Field>
      <Section title={t("入站恢复队列", "Inbound recovery queue")}>
        {queueLoading && (
          <p role="status" className="text-sm text-muted-foreground">
            {receipts.data
              ? t("正在刷新恢复任务…", "Refreshing recovery tasks…")
              : t("正在加载恢复任务…", "Loading recovery tasks…")}
          </p>
        )}
        <LoadError
          error={queueLoading ? undefined : receipts.error}
          onRetry={() => void receipts.mutate()}
        />
        {receiptRows.map((v) => (
          <div
            key={v.id}
            className="flex flex-wrap items-center justify-between gap-3 border-b py-3"
          >
            <div className="text-sm">
              <p>
                {v.id} · {v.state}
              </p>
              <p className="max-w-3xl break-words text-muted-foreground">
                {v.error}
              </p>
            </div>
            <ActionButton
              disabled={busy || queueLoading}
              onClick={() =>
                run(async () => {
                  const next = await company<{
                    receipt: Receipt;
                    original_valid: boolean;
                  }>(`/recovery/${v.id}/inspect`, {
                    method: "POST",
                    body: { reason },
                  });
                  setInspection(next);
                  setTargets([]);
                  setInboundReviewRequired(false);
                })
              }
            >
              {t("检查原件与目标", "Inspect original and targets")}
            </ActionButton>
          </div>
        ))}
        {!queueLoading && queueAvailable && !receiptRows.length && (
          <p>{t("没有待恢复任务", "No recovery tasks")}</p>
        )}
        <div className="flex gap-3">
          <ActionButton
            disabled={queueLoading || !queueAvailable || page === 1}
            onClick={() => setPage((v) => v - 1)}
          >
            {t("上一页", "Previous")}
          </ActionButton>
          <span>{page}</span>
          <ActionButton
            disabled={queueLoading || !queueAvailable || page * 30 >= (receipts.data?.meta.total ?? 0)}
            onClick={() => setPage((v) => v + 1)}
          >
            {t("下一页", "Next")}
          </ActionButton>
          <ActionButton disabled={queueLoading} onClick={() => void receipts.mutate()}>
            {t("刷新", "Refresh")}
          </ActionButton>
        </div>
        {inboundReviewRequired && (
          <p role="alert" className="text-sm text-destructive">
            {t(
              "恢复状态已变化。请重新检查原件与目标后再重试。",
              "Recovery state changed. Inspect the original and targets again before retrying.",
            )}
          </p>
        )}
        {inspection && (
          <div className="space-y-3 rounded border p-4">
            <p className="font-medium">{inspection.receipt.id}</p>
            <p role="status">
              {inspection.original_valid
                ? t(
                    "原件大小与哈希验证通过",
                    "Original size and checksum verified",
                  )
                : t(
                    "原件验证失败，禁止重试",
                    "Original failed verification; retry blocked",
                  )}
            </p>
            <p className="text-xs text-muted-foreground">
              {t("检查版本", "Inspection version")}:{" "}
              {inspection.receipt.updated_at}
            </p>
            {inspection.receipt.targets.map((v) => (
              <label
                className="flex items-start gap-2 text-sm"
                key={v.mailbox_id}
              >
                <input
                  type="checkbox"
                  disabled={v.state === "delivered" || busy}
                  checked={targets.includes(v.mailbox_id)}
                  onChange={(e) =>
                    setTargets((old) =>
                      e.target.checked
                        ? [...old, v.mailbox_id]
                        : old.filter((x) => x !== v.mailbox_id),
                    )
                  }
                />
                <span>
                  {v.address} · {v.state}
                  <br />
                  {v.error}
                </span>
              </label>
            ))}
            <ActionButton
              disabled={
                busy ||
                !inspection.original_valid ||
                !targets.length ||
                reason.trim().length < 8 ||
                inspection.receipt.state === "processing"
              }
              onClick={() =>
                run(async () => {
                  try {
                    await company(`/recovery/${inspection.receipt.id}/retry`, {
                      method: "POST",
                      body: {
                        updated_at: inspection.receipt.updated_at,
                        targets,
                        reason,
                      },
                    });
                  } catch (error) {
                    if (isConflict(error)) {
                      setInspection(null);
                      setTargets([]);
                      setInboundReviewRequired(true);
                    }
                    throw error;
                  }
                  setInspection(null);
                  await receipts.mutate();
                  toast.success(
                    t(
                      "选中的未完成目标已重新入队",
                      "Selected unfinished targets requeued",
                    ),
                  );
                })
              }
            >
              {t(
                "仅重试所选未完成目标",
                "Retry only selected unfinished targets",
              )}
            </ActionButton>
          </div>
        )}
      </Section>
      <Section
        title={t(
          "出站例外检查与不确定结果核对",
          "Outbound break-glass and uncertain-result reconciliation",
        )}
      >
        <p className="text-sm text-muted-foreground">
          {t(
            "仅当前有效的平台超级管理员可在所选公司范围内使用受审计例外。审计提交后显示正文、结构化密送和安全诊断类别/状态码，不显示原始协议诊断。下一跳接受不代表最终送达；核对服务器证据，不可猜测重试。",
            "Only a current platform super administrator may use this audited exception in the selected company. A committed audit reveals content, structured Bcc and safe diagnostic classes/codes, never raw protocol diagnostics. Next-hop acceptance is not final delivery; verify server evidence, never guess by retrying.",
          )}
        </p>
        <Field label={t("发送任务 ID", "Outbound job ID")}>
          {(id) => (
            <input
              id={id}
              className={inputClass}
              value={jobId}
              onChange={(e) => {
                inspectionVersion.current++;
                setJobId(e.target.value);
                setJobInspection(null);
                setOutcomes({});
                setOutboundReviewRequired(false);
              }}
            />
          )}
        </Field>
        <ActionButton
          disabled={busy || !canInspect || !isInspectionID(jobId.trim()) || !validInspectionReason(reason)}
          onClick={() =>
            run(async () => {
              if (!canInspect || !auth.tenantId || !validInspectionReason(reason)) return;
              const target = jobId.trim();
              if (!isInspectionID(target)) return;
              const version = ++inspectionVersion.current;
              const tenantId = auth.tenantId;
              setJobInspection(null);
              setOutcomes({});
              const value = await company<unknown>(`/outbound/${encodeURIComponent(target)}/inspect`, {
                method: "POST",
                body: { reason: reason.trim() },
              });
              if (version !== inspectionVersion.current) return;
              setJobInspection(parseOutboundInspection(value, { jobId: target, tenantId }));
              setOutboundReviewRequired(false);
            })
          }
        >
          {t("记录审计并检查", "Audit and inspect")}
        </ActionButton>
        {outboundReviewRequired && (
          <p role="alert" className="text-sm text-destructive">
            {t(
              "投递状态已变化。请重新检查此任务后再保存有证据的结果。",
              "Delivery state changed. Inspect this job again before saving evidenced outcomes.",
            )}
          </p>
        )}
        {canInspect && jobInspection && (
          <div className="rounded border p-4 space-y-3" aria-label={t("受审计出站检查结果", "Audited outbound inspection result")}>
            <h3 className="font-medium">{jobInspection.job.subject}</h3>
            <p className="text-sm">
              {jobInspection.job.mail_from} · {jobStatusLabel(jobInspection.job.status)}
            </p>
            <dl className="grid gap-2 text-sm md:grid-cols-2">
              <div><dt>{t("任务 / 公司", "Job / company")}</dt><dd className="break-all">{jobInspection.job.id} / {jobInspection.job.tenant_id}</dd></div>
              <div><dt>{t("队列状态", "Queue state")}</dt><dd>{jobInspection.job.state}</dd></div>
              <div><dt>{t("创建时间", "Created at")}</dt><dd>{jobInspection.job.created_at}</dd></div>
              <div><dt>{t("检查版本", "Inspection version")}</dt><dd>{jobInspection.job.updated_at}</dd></div>
              <div><dt>{t("收件人", "To")}</dt><dd>{jobInspection.job.to.join(", ") || "—"}</dd></div>
              <div><dt>{t("抄送", "Cc")}</dt><dd>{jobInspection.job.cc.join(", ") || "—"}</dd></div>
              <div><dt>{t("密送（仅本次受审计例外）", "Bcc (this audited exception only)")}</dt><dd>{jobInspection.job.bcc.join(", ") || "—"}</dd></div>
            </dl>
            <div><p className="text-sm font-medium">{t("文本正文", "Text body")}</p>
              <pre className="whitespace-pre-wrap break-words text-sm">{jobInspection.job.text_body}</pre></div>
            <div><p className="text-sm font-medium">{t("HTML 源文（不执行）", "HTML source (not executed)")}</p>
              <pre className="whitespace-pre-wrap break-words text-sm">{jobInspection.job.html_body}</pre></div>
            <div><p className="text-sm font-medium">{t("允许的结构化邮件头", "Allowlisted structural headers")}</p>
              <dl className="space-y-1 text-xs">{Object.entries(jobInspection.job.headers).map(([name, value]) => (
                <div className="break-words" key={name}><dt className="inline font-medium">{name}: </dt><dd className="inline">{value}</dd></div>
              ))}</dl></div>
            {jobInspection.recipients.map((v) => (
              <div
                className="grid gap-2 md:grid-cols-2 text-sm"
                key={v.address}
              >
                <div>
                  {v.address} · {recipientKindLabel(v.kind)} · {recipientStateLabel(v.state)}
                  <p className="break-words text-muted-foreground">
                    {t("安全诊断类别", "Safe diagnostic class")}: {recipientStateLabel(v.diagnostic_class)}
                    {" · SMTP "}{v.smtp_code || "—"}{v.enhanced_code ? ` · ${v.enhanced_code}` : ""}
                  </p>
                  <p className="text-xs text-muted-foreground">{t("尝试次数", "Attempts")}: {v.attempts} · {v.updated_at}</p>
                </div>
                {v.state === "uncertain" && (
                  <select
                    aria-label={`${t("确认结果", "Confirm outcome")} ${v.address}`}
                    className={inputClass}
                    value={outcomes[v.address] ?? ""}
                    onChange={(e) =>
                      setOutcomes({ ...outcomes, [v.address]: e.target.value })
                    }
                  >
                    <option value="">
                      {t(
                        "尚未确认，保持不确定",
                        "Not verified; keep uncertain",
                      )}
                    </option>
                    <option value="accepted">
                      {t(
                        "记录证实下一跳已接受",
                        "Evidence confirms next-hop acceptance",
                      )}
                    </option>
                    <option value="temporary">
                      {t(
                        "记录证实未接受，可再次尝试",
                        "Evidence confirms not accepted; retryable",
                      )}
                    </option>
                    <option value="permanent">
                      {t(
                        "记录证实永久拒绝",
                        "Evidence confirms permanent rejection",
                      )}
                    </option>
                  </select>
                )}
              </div>
            ))}
            <ActionButton
              disabled={
                busy ||
                !canInspect ||
                !validInspectionReason(reason) ||
                !Object.values(outcomes).some(Boolean)
              }
              onClick={() =>
                run(async () => {
                  if (!canInspect || !validInspectionReason(reason)) return;
                  const version = inspectionVersion.current;
                  try {
                    await company(`/outbound/${jobInspection.job.id}/reconcile`, {
                      method: "POST",
                      body: {
                        updated_at: jobInspection.job.updated_at,
                        reason,
                        results: Object.entries(outcomes)
                          .filter(([, state]) => state)
                          .map(([address, state]) => ({ address, state })),
                      },
                    });
                  } catch (error) {
                    if (isConflict(error) && version === inspectionVersion.current) {
                      setJobInspection(null);
                      setOutcomes({});
                      setOutboundReviewRequired(true);
                    }
                    throw error;
                  }
                  setJobInspection(null);
                  toast.success(
                    t(
                      "已保存确认结果；尚未触发新的网络投递",
                      "Confirmed outcomes saved; no new network delivery was triggered",
                    ),
                  );
                })
              }
            >
              {t("保存有证据的结果", "Save evidenced outcomes")}
            </ActionButton>
          </div>
        )}
      </Section>
      <Section
        title={t(
          "运行配置与重启边界",
          "Runtime configuration and restart boundaries",
        )}
      >
        <LoadError
          error={runtime.error}
          onRetry={() => void runtime.mutate()}
        />
        <p className="text-sm text-muted-foreground">
          {t(
            "这些是当前 API 进程的启动配置，不包含密钥。数据库已保存的设置不一定已被所有进程采用。",
            "This is the current API process's startup configuration, without secrets. Saved database settings may not yet be active in every process.",
          )}
        </p>
        <pre className="overflow-x-auto whitespace-pre-wrap text-xs">
          {JSON.stringify(runtime.data?.data ?? {}, null, 2)}
        </pre>
      </Section>
    </main>
  );
}
