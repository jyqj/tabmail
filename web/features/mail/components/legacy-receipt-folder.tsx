"use client";
import { useState } from "react";
import { useAPI } from "@/hooks/use-api";
import { legacyOutboundReceipt, legacyOutboundReceipts } from "@/lib/legacy-outbound";
import { useSessionScope } from "@/lib/session";
import { useI18n } from "@/lib/i18n";
import { ActionButton, LoadError, useText } from "@/components/company/common";
import { ReceiptContentDisclosure, ReceiptSummary } from "./submission-pane";
import { ListFeedback } from "./list-controls";

// Compatibility entry points share ordinary aggregate receipts. No raw job
// field can confer content permission or supply the subject/address/body.
export function LegacyReceiptFolder({ initialSelected }: { initialSelected?: string }) {
  const t = useText();
  const { t: receiptText } = useI18n();
  const scope = useSessionScope();
  const [selected, setSelected] = useState(initialSelected ?? "");
  const rows = useAPI("legacy-outbound-receipts", () => legacyOutboundReceipts());
  const detail = useAPI(selected ? ["legacy-outbound-receipt", selected] : null, () => legacyOutboundReceipt(selected));
  return <section aria-label={t("兼容任务回执", "Compatibility task receipts")}>
    <p>{receiptText("ordinaryReceipt.nextHopNotice")}</p>
    <ListFeedback list={rows} loading={t("正在加载兼容回执…", "Loading compatibility receipts…")} refreshing={t("正在刷新兼容回执…", "Refreshing compatibility receipts…")}
      empty={!rows.data?.length ? t("没有兼容回执", "No compatibility receipts") : undefined}/>
    {!rows.error && (rows.data ?? []).map(row => <div key={row.id}>
      <span>{receiptText("ordinaryReceipt.task")}: {row.id}</span>
      <ActionButton onClick={() => setSelected(row.id === selected ? "" : row.id)}>{t("查看兼容回执", "View compatibility receipt")}</ActionButton>
    </div>)}
    <LoadError error={detail.error} onRetry={() => void detail.mutate()} />
    {!detail.error && detail.data && <section aria-label={t("兼容回执详情", "Compatibility receipt detail")}>
      <ReceiptSummary receipt={detail.data} />
      {detail.data.capabilities?.view_content === true && <ReceiptContentDisclosure key={`${scope}:${selected}`} id={detail.data.id} />}
    </section>}
  </section>;
}
