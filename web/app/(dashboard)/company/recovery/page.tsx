"use client";
import { useState } from "react";
import { toast } from "sonner";
import { useAPI } from "@/hooks/use-api";
import { request } from "@/lib/api/base";
import type { APIListResponse, APIResponse, OutboundJob } from "@/lib/types";
import { company, type Receipt, type RecipientResult } from "@/lib/company";
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
  const { busy, run } = useAction();
  const [page, setPage] = useState(1);
  const receipts = useAPI(["recovery-receipts", page], () =>
    request<APIListResponse<Receipt>>("/api/v1/company/recovery", {
      params: { page, per_page: 30 },
    }),
  );
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
  const [reason, setReason] = useState("");
  const [jobId, setJobId] = useState("");
  const [jobInspection, setJobInspection] = useState<{
    job: OutboundJob;
    recipients: RecipientResult[];
  } | null>(null);
  const [outcomes, setOutcomes] = useState<Record<string, string>>({});
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
          "本次检查 / 恢复原因（至少 8 个字符）",
          "Inspection / recovery reason (8+ characters)",
        )}
      >
        {(id) => (
          <textarea
            id={id}
            className={inputClass}
            value={reason}
            minLength={8}
            maxLength={1000}
            rows={2}
            onChange={(e) => setReason(e.target.value)}
          />
        )}
      </Field>
      <Section title={t("入站恢复队列", "Inbound recovery queue")}>
        <LoadError
          error={receipts.error}
          onRetry={() => void receipts.mutate()}
        />
        {(receipts.data?.data ?? []).map((v) => (
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
              disabled={busy}
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
                })
              }
            >
              {t("检查原件与目标", "Inspect original and targets")}
            </ActionButton>
          </div>
        ))}
        {!receipts.isLoading && !receipts.data?.data.length && (
          <p>{t("没有待恢复任务", "No recovery tasks")}</p>
        )}
        <div className="flex gap-3">
          <ActionButton
            disabled={page === 1}
            onClick={() => setPage((v) => v - 1)}
          >
            {t("上一页", "Previous")}
          </ActionButton>
          <span>{page}</span>
          <ActionButton
            disabled={page * 30 >= (receipts.data?.meta.total ?? 0)}
            onClick={() => setPage((v) => v + 1)}
          >
            {t("下一页", "Next")}
          </ActionButton>
          <ActionButton onClick={() => void receipts.mutate()}>
            {t("刷新", "Refresh")}
          </ActionButton>
        </div>
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
                  await company(`/recovery/${inspection.receipt.id}/retry`, {
                    method: "POST",
                    body: {
                      updated_at: inspection.receipt.updated_at,
                      targets,
                      reason,
                    },
                  });
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
            "检查会先写审计，再显示正文、密送和协议诊断。只有确认了服务器记录，才能标记接收或明确未接收；不能把“未知”改成“重试”来猜测。",
            "Inspection records an audit before revealing content, Bcc and diagnostics. Confirm server evidence before recording acceptance or non-acceptance; never guess by retrying.",
          )}
        </p>
        <Field label={t("发送任务 ID", "Outbound job ID")}>
          {(id) => (
            <input
              id={id}
              className={inputClass}
              value={jobId}
              onChange={(e) => {
                setJobId(e.target.value);
                setJobInspection(null);
                setOutcomes({});
              }}
            />
          )}
        </Field>
        <ActionButton
          disabled={busy || !jobId || reason.trim().length < 8}
          onClick={() =>
            run(async () => {
              const value = await company<{
                job: OutboundJob;
                recipients: RecipientResult[];
              }>(`/outbound/${encodeURIComponent(jobId)}/inspect`, {
                method: "POST",
                body: { reason },
              });
              setJobInspection(value);
              setOutcomes({});
            })
          }
        >
          {t("记录审计并检查", "Audit and inspect")}
        </ActionButton>
        {jobInspection && (
          <div className="rounded border p-4 space-y-3">
            <h3 className="font-medium">{jobInspection.job.subject}</h3>
            <p className="text-sm">
              {jobInspection.job.mail_from} · {jobInspection.job.state}
            </p>
            <pre className="whitespace-pre-wrap text-sm">
              {jobInspection.job.text_body}
            </pre>
            {jobInspection.recipients.map((v) => (
              <div
                className="grid gap-2 md:grid-cols-2 text-sm"
                key={v.address}
              >
                <div>
                  {v.address} · {v.state}
                  <p className="break-words text-muted-foreground">
                    {v.diagnostic}
                  </p>
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
                reason.trim().length < 8 ||
                !Object.values(outcomes).some(Boolean)
              }
              onClick={() =>
                run(async () => {
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
