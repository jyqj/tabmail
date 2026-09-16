"use client";
import { useAPI } from "@/hooks/use-api";
import {
  company,
  type MailTemplate,
  type TemplateVersion,
} from "@/lib/company";
import {
  LoadError,
  Section,
  useText,
} from "@/components/company/common";

// TemplateVersionsView lists the immutable published versions of one template:
// version number, publish time and content_hash prefix, with the snapshot
// content expandable.
export function TemplateVersionsView({
  template,
  refreshKey,
}: {
  template: MailTemplate | null;
  refreshKey: number;
}) {
  const t = useText();
  const versions = useAPI(
    template?.id ? ["template-versions", template.id, refreshKey] : null,
    () => company<TemplateVersion[]>(`/templates/${template!.id}/versions`),
  );
  return (
    <Section title={t("版本历史", "Version history")}>
      {!template && (
        <p className="text-muted-foreground">
          {t(
            "从模板库选择一个模板后查看其已发布版本。",
            "Pick a template from the library to inspect its published versions.",
          )}
        </p>
      )}
      {template && (
        <LoadError
          error={versions.error}
          onRetry={() => void versions.mutate()}
        />
      )}
      {template &&
        (versions.data ?? []).map((v) => (
          <details key={v.id} className="text-sm">
            <summary>
              v{v.version} · {new Date(v.published_at).toLocaleString()} ·{" "}
              {v.content_hash.slice(0, 16)}
              {v.revoked_at
                ? ` · ${t("已撤销", "Revoked")}`
                : ` · ${t("已发布", "Published")}`}
            </summary>
            <pre className="whitespace-pre-wrap p-3">
              {v.snapshot.subject}
              {"\n"}
              {v.snapshot.text_body}
            </pre>
          </details>
        ))}
      {template && !versions.isLoading && !versions.data?.length && (
        <p className="text-muted-foreground">
          {t("该模板尚未发布任何版本", "This template has no published versions")}
        </p>
      )}
    </Section>
  );
}
