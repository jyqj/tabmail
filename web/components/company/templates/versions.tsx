"use client";
import { useAPI } from "@/hooks/use-api";
import {
  company,
  revokeTemplateVersion,
  type MailTemplate,
  type TemplateVersion,
} from "@/lib/company";
import { safeConfirm } from "@/lib/utils";
import {
  LoadError,
  Section,
  useAction,
  useText,
} from "@/components/company/common";

// TemplateVersionsView lists the immutable published versions of one template:
// version number, publish time and content_hash prefix, with the snapshot
// content expandable. Each live version carries the emergency "Revoke" action:
// a one-way stop of that version's not-yet-started deliveries. It is
// deliberately worded apart from the template-level retire switch in the
// library view (retire = no longer selectable; revoke = urgent stop of
// undelivered sends of this one version, delivered results untouched).
export function TemplateVersionsView({
  template,
  refreshKey,
  onRevoked,
}: {
  template: MailTemplate | null;
  refreshKey: number;
  onRevoked?: () => void | Promise<void>;
}) {
  const t = useText();
  const { busy, run } = useAction();
  const versions = useAPI(
    template?.id ? ["template-versions", template.id, refreshKey] : null,
    () => company<TemplateVersion[]>(`/templates/${template!.id}/versions`),
  );
  function revokeVersion(v: TemplateVersion) {
    if (!template?.id) return;
    const confirmed = safeConfirm(
      t(
        `撤销不可逆：v${v.version} 的所有未发出的投递将被立即停止，已发出的投递结果与历史完全不变，同模板其他版本不受影响。要让模板整体不再可选，请在模板库使用「停用」。确定撤销该版本？`,
        `Irreversible: revoking v${v.version} immediately stops every delivery of this version that has not started yet. Already-delivered outcomes and history are unchanged, and sibling versions are unaffected. To make the whole template unselectable, use Retire in the library. Revoke this version?`,
      ),
    );
    if (!confirmed) return;
    void run(async () => {
      await revokeTemplateVersion(template.id!, v.version, template.revision);
      await versions.mutate();
      await onRevoked?.();
    });
  }
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
          <div
            key={v.id}
            className="flex items-start justify-between gap-3 border-b pb-2"
          >
            <details className="grow text-sm">
              <summary>
                v{v.version} · {new Date(v.published_at).toLocaleString()} ·{" "}
                {v.content_hash.slice(0, 16)}
                {v.revoked_at ? (
                  <span className="text-destructive">
                    {" "}
                    · {t("已撤销", "Revoked")}
                  </span>
                ) : (
                  ` · ${t("已发布", "Published")}`
                )}
              </summary>
              <pre className="whitespace-pre-wrap p-3">
                {v.snapshot.subject}
                {"\n"}
                {v.snapshot.text_body}
              </pre>
            </details>
            {!v.revoked_at && (
              <button
                type="button"
                disabled={busy}
                data-testid={`revoke-version-${v.version}`}
                onClick={() => revokeVersion(v)}
                className="shrink-0 rounded-md border border-destructive px-2 py-1 text-xs font-medium text-destructive hover:bg-destructive/10 disabled:opacity-50"
              >
                {t("撤销", "Revoke")}
              </button>
            )}
          </div>
        ))}
      {template && !versions.isLoading && !versions.data?.length && (
        <p className="text-muted-foreground">
          {t("该模板尚未发布任何版本", "This template has no published versions")}
        </p>
      )}
    </Section>
  );
}
