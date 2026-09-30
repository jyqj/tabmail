"use client";
import { useState } from "react";
import { useAPI } from "@/hooks/use-api";
import { legacyOutboundReceipt, legacyOutboundReceipts } from "@/lib/legacy-outbound";
import { ActionButton, LoadError, useText } from "@/components/company/common";
import { SubmissionContentView } from "./submission-content";

// Real legacy routes remain visible as compatibility receipts. All normal
// content uses the authoritative sent-content endpoint, never a legacy body.
export function LegacyReceiptFolder({ initialSelected }: { initialSelected?: string }) {
  const t = useText();
  const [selected, setSelected] = useState(initialSelected ?? "");
  const rows = useAPI("legacy-outbound-receipts", () => legacyOutboundReceipts());
  const detail = useAPI(["legacy-outbound-receipt", selected], () => selected ? legacyOutboundReceipt(selected) : Promise.resolve(undefined));
  const [content, setContent] = useState(false);
  return <section aria-label={t("兼容任务回执", "Compatibility task receipts")}>
    <p>{t("兼容任务记录不是正文凭证；下一跳接受不等于最终送达。", "A compatibility task receipt is not content authority; next-hop acceptance is not final delivery.")}</p>
    <LoadError error={rows.error} onRetry={() => void rows.mutate()} />
    {(rows.data ?? []).map(row => <div key={row.id}>
      <span>{row.subject}</span>{" "}<span>{row.mail_from}</span>{" "}<span>{row.state}</span>
      <ActionButton onClick={() => { setSelected(row.id); setContent(false); }}>{t("查看兼容回执", "View compatibility receipt")}</ActionButton>
    </div>)}
    <LoadError error={detail.error} onRetry={() => void detail.mutate()} />
    {!detail.error && detail.data && <section aria-label={t("兼容回执详情", "Compatibility receipt detail")}>
      <p>{detail.data.subject}</p><p>{detail.data.mail_from}</p>
      <p>{[...(detail.data.to ?? []), ...(detail.data.cc ?? [])].join(", ")}</p>
      <p>{detail.data.state} · {detail.data.created_at}</p>
      {detail.data.content_redacted && <p role="status">{t("正文不可用；安全回执仍保留。", "Content unavailable; the safe receipt is retained.")}</p>}
      {detail.data.content_redacted !== true && <ActionButton onClick={() => setContent(value => !value)}>{t("展开保留的发件内容（重新鉴权）", "Open preserved sent content (re-authorized)")}</ActionButton>}
      {content && detail.data.content_redacted !== true && <SubmissionContentView id={detail.data.id} />}
    </section>}
  </section>;
}
