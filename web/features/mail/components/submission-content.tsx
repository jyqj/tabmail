"use client";
import { useContext, useEffect, useRef, useState } from "react";
import { submissionContent, submissionAttachments, downloadCompanyFile, type SubmissionContent, type SubmissionAttachment } from "@/lib/company";
import { assertSession, useSessionScope } from "@/lib/session";
import { useI18n } from "@/lib/i18n";
import { ActionButton, MailHTML, useAction, useText } from "@/components/company/common";
import { LiveContentRefresh } from "../live-content-refresh";

// This is the LIVE sent-asset reader, not an operation receipt or a platform
// inspection. Private content is intentionally never retained in an SWR cache.
export function SubmissionContentView({ id }: { id: string }) {
  const scope = useSessionScope();
  return <LiveSubmissionContent key={`${scope}:${id}`} id={id} scope={scope} />;
}
function LiveSubmissionContent({ id, scope }: { id: string; scope: string }) {
  const refresh = useContext(LiveContentRefresh);
  const t = useText();
  const { t: receiptText } = useI18n();
  const { busy, run } = useAction();
  const epoch = useRef(0);
  const [reload, setReload] = useState(0);
  const [state, setState] = useState<{ content?: SubmissionContent; files: SubmissionAttachment[]; failed: boolean; loading: boolean }>({ files: [], failed: false, loading: true });
  useEffect(() => {
    let disposed = false;
    let controller: AbortController | undefined;
    const clear = () => setState({ files: [], failed: true, loading: false });
    async function load() {
      controller?.abort();
      controller = new AbortController();
      const signal = controller.signal;
      const version = ++epoch.current;
      try {
        // Each response must belong to the stable session scope AND the latest
        // disclosure request. Content must pass its live authorization first.
        const content = await submissionContent(id, signal);
        assertSession(scope);
        if (disposed || signal.aborted || version !== epoch.current) return;
        if (content.content_redacted) { clear(); return; }
        const files = await submissionAttachments(id, signal);
        assertSession(scope);
        if (disposed || signal.aborted || version !== epoch.current) return;
        setState({ content, files: files ?? [], failed: false, loading: false });
      } catch {
        if (!disposed && version === epoch.current) {
          // Final 401/403, expired/purged 404, invalid DTOs and all other read
          // failures clear both private body and attachment metadata.
          controller?.abort();
          clear();
        }
      }
    }
    void load();
    const timer = window.setInterval(() => { void load(); }, 15000);
    return () => { disposed = true; epoch.current++; controller?.abort(); window.clearInterval(timer); };
  }, [id, scope, reload, refresh]);
  const content = state.content;
  return <div data-testid="live-submission-content" className="space-y-3 border-t pt-3">
    {state.failed && <div role="alert"><p>{receiptText("ordinaryReceipt.contentUnavailable")}</p><ActionButton onClick={() => setReload(value => value + 1)}>{t("重试加载", "Retry loading")}</ActionButton></div>}
    {state.loading && <p>{t("加载中…", "Loading…")}</p>}
    {content && <>
      <h3 className="break-words font-medium">{content.subject || t("无主题", "No subject")}</h3>
      <p className="break-words text-sm">{content.from} → {content.to.join(", ")}</p>
      {Boolean(content.cc?.length) && <p className="break-words text-sm">Cc: {content.cc!.join(", ")}</p>}
      <p data-testid="sent-structured-bcc" className="break-words text-sm">
        {receiptText("ordinaryReceipt.bcc")}: {content.recipient_completeness === "legacy_unknown"
          ? receiptText("ordinaryReceipt.bccUnknown") : content.bcc!.length ? content.bcc!.join(", ") : receiptText("ordinaryReceipt.bccEmpty")}
      </p>
      {content.text_body && <pre className="whitespace-pre-wrap break-words text-sm leading-7">{content.text_body}</pre>}
      {content.html_body && <details><summary>{t("安全 HTML 视图（外部资源已阻止）", "Safe HTML view (external resources blocked)")}</summary><MailHTML html={content.html_body} /></details>}
      {state.files.map(file => <ActionButton key={file.id} disabled={busy} onClick={() => run(() => downloadCompanyFile(`/submissions/${encodeURIComponent(id)}/attachments/${encodeURIComponent(file.id)}/download`, file.filename))}>
        {file.filename} · {Math.ceil(file.size / 1024)} KiB
      </ActionButton>)}
    </>}
  </div>;
}
