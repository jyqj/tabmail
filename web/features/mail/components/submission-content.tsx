"use client";
import { useAPI } from "@/hooks/use-api";
import { submissionContent, submissionAttachments, downloadCompanyFile } from "@/lib/company";
import { ActionButton, LoadError, MailHTML, useAction, useText } from "@/components/company/common";
// SubmissionContentView renders the actual sent body and the pinned
// attachments of one submission. It reuses the message pane's safe rendering
// conventions: plain text in a pre, HTML only through the sandboxed MailHTML
// view, and downloads through the per-submission server endpoint (storage
// keys never reach the client).
export function SubmissionContentView({ id }: {
    id: string;
}) {
    const t = useText();
    const { busy, run } = useAction();
    const content = useAPI(["submission-content", id], () => submissionContent(id), { refreshInterval: 15000 });
    const files = useAPI(["submission-attachments", id], () => submissionAttachments(id), { refreshInterval: 15000 });
    const c = content.error ? undefined : content.data;
    return (<div className="space-y-3 border-t pt-3">
      <LoadError error={content.error} onRetry={() => void content.mutate()}/>
      {content.isLoading && (<p className="text-sm text-muted-foreground">
          {t("加载中…", "Loading…")}
        </p>)}
      {c && (<>
          {c.content_redacted ? (<p role="status" className="text-sm">
              {t("正文内容对你不可见。", "The message body is not visible to you.")}
            </p>) : (<>
              {c.text_body && (<pre className="whitespace-pre-wrap break-words text-sm leading-7">
                  {c.text_body}
                </pre>)}
              {c.html_body && (<details>
                  <summary className="cursor-pointer">
                    {t("安全 HTML 视图（外部资源已阻止）", "Safe HTML view (external resources blocked)")}
                  </summary>
                  <MailHTML html={c.html_body}/>
                </details>)}
            </>)}
        </>)}
      <LoadError error={files.error} onRetry={() => void files.mutate()}/>
      {(!files.error && !content.error && !c?.content_redacted ? files.data ?? [] : []).map((f) => (<ActionButton key={f.id} disabled={busy} onClick={() => run(() => downloadCompanyFile(`/submissions/${encodeURIComponent(id)}/attachments/${encodeURIComponent(f.id)}/download`, f.filename))}>
          {f.filename} · {Math.ceil(f.size / 1024)} KiB
        </ActionButton>))}
    </div>);
}
