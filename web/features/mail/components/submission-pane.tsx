"use client";
import { useState } from "react";
import { toast } from "sonner";
import { useAPI } from "@/hooks/use-api";
import { submission, type Submission } from "@/lib/company";
import { retryOutboundReceipt } from "@/lib/legacy-outbound";
import { sessionScope, useSessionScope } from "@/lib/session";
import { useI18n } from "@/lib/i18n";
import { ActionButton, LoadError, useAction } from "@/components/company/common";
import { StatusBadge } from "./delivery-status";
import { SubmissionContentView } from "./submission-content";

export function ReceiptSummary({ receipt }: { receipt: Submission }) {
  const { t } = useI18n();
  const counts = receipt.progress.completeness === "known" ? receipt.progress.counts : undefined;
  return <div data-testid="ordinary-receipt-aggregate" className="space-y-2">
    <p className="break-all text-sm">{t("ordinaryReceipt.task")}: {receipt.id}</p>
    <p className="text-sm"><StatusBadge status={receipt.status} /></p>
    <p className="text-xs text-muted-foreground">{t("ordinaryReceipt.nextHopNotice")}</p>
    {counts ? <dl className="flex flex-wrap gap-3 text-sm">{(["total", "accepted", "pending", "temporary", "permanent", "uncertain"] as const).map(key =>
      <div key={key} data-testid={`receipt-count-${key}`}><dt>{t(`ordinaryReceipt.counts.${key}`)}</dt><dd>{counts[key]}</dd></div>)}
    </dl> : <p role="status">{t("ordinaryReceipt.progressUnknown")}</p>}
    {receipt.created_at && <time className="text-xs">{new Date(receipt.created_at).toLocaleString()}</time>}
  </div>;
}
// Mount only for an affirmative CURRENT backend capability. A scope/key change
// unmounts disclosure state; token-only rotation is deliberately not a key.
export function ReceiptContentDisclosure({ id }: { id: string }) {
  const { t } = useI18n();
  const [open, setOpen] = useState(false);
  return <>
    <ActionButton data-testid="receipt-content-disclosure" aria-expanded={open} onClick={() => setOpen(value => !value)}>
      {t(open ? "ordinaryReceipt.hideContent" : "ordinaryReceipt.viewContent")}
    </ActionButton>
    {open && <SubmissionContentView id={id} />}
  </>;
}
export function SubmissionPane({ id }: { id: string }) {
  const { t } = useI18n();
  const scope = useSessionScope();
  const { busy, run } = useAction();
  const detail = useAPI(["submission", id], () => submission(id), { refreshInterval: 10000 });
  const receipt = detail.error ? undefined : detail.data;
  return <div data-testid="ordinary-submission-pane" className="mt-4 space-y-3 rounded-md border p-4">
    <LoadError error={detail.error} onRetry={() => void detail.mutate()} />
    {receipt && <>
      <ReceiptSummary receipt={receipt} />
      {receipt.capabilities?.view_content === true && <ReceiptContentDisclosure key={`${scope}:${id}`} id={id} />}
      {receipt.capabilities?.retry === true && <ActionButton data-testid="receipt-retry" disabled={busy} onClick={() => run(async () => {
        try {
          await retryOutboundReceipt(id);
          if (scope !== sessionScope()) return;
          toast.success(t("ordinaryReceipt.retryRequested"));
          await detail.mutate();
        } catch {
          if (scope !== sessionScope()) return;
          // Do not leak server diagnostics through an operation-status toast.
          toast.error(t("ordinaryReceipt.retryBlocked"));
          void detail.mutate();
        }
      })}>{t("ordinaryReceipt.retry")}</ActionButton>}
      {receipt.delivery_uncertain && <p role="status">{t("ordinaryReceipt.retryBlocked")}</p>}
    </>}
  </div>;
}
