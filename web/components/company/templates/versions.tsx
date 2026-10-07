"use client";
import { useLayoutEffect, useRef } from "react";
import { useAPI } from "@/hooks/use-api";
import { sessionScope, useSessionScope } from "@/lib/session";
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
type VersionsProps = {
  template: MailTemplate | null;
  refreshKey: number;
  onRevoked?: () => void | Promise<void>;
};

export function TemplateVersionsView(props: VersionsProps) {
  const scope = useSessionScope();
  return <TemplateVersionHistory
    key={JSON.stringify([scope, props.template?.id, props.template?.revision, props.refreshKey])}
    {...props}
    scope={scope}
  />;
}

function TemplateVersionHistory({ template, refreshKey, onRevoked, scope }: VersionsProps & {
  scope: string;
}) {
  const t = useText();
  const { busy, run } = useAction();
  const versions = useAPI(
    template?.id ? ["template-versions", template.id, template.revision, refreshKey] : null,
    () => company<TemplateVersion[]>(`/templates/${template!.id}/versions`),
  );
  const ready = !!template?.id && !versions.error && !versions.isLoading && !versions.isValidating && Array.isArray(versions.data);
  const lifetime = useRef<object | null>(null);
  const currentRead = useRef<{ data: TemplateVersion[] | undefined; ready: boolean } | null>(null);
  useLayoutEffect(() => {
    lifetime.current = {};
    return () => { lifetime.current = null; };
  }, []);
  useLayoutEffect(() => {
    currentRead.current = { data: versions.data, ready };
    return () => { currentRead.current = null; };
  }, [versions.data, ready]);
  const owns = (owner: object) => lifetime.current === owner && scope === sessionScope();

  function revokeVersion(v: TemplateVersion) {
    const observed = currentRead.current;
    const owner = lifetime.current;
    if (!template?.id || busy || !owner || !owns(owner) || !observed?.ready ||
      !observed.data?.includes(v) || v.template_id !== template.id || v.revoked_at) return;
    const confirmed = safeConfirm(
      t(
        `撤销不可逆：v${v.version} 的所有未发出的投递将被立即停止，已发出的投递结果与历史完全不变，同模板其他版本不受影响。要让模板整体不再可选，请在模板库使用「停用」。确定撤销该版本？`,
        `Irreversible: revoking v${v.version} immediately stops every delivery of this version that has not started yet. Already-delivered outcomes and history are unchanged, and sibling versions are unaffected. To make the whole template unselectable, use Retire in the library. Revoke this version?`,
      ),
    );
    if (!confirmed || currentRead.current !== observed || !owns(owner)) return;
    void run(async () => {
      try {
        await revokeTemplateVersion(template.id!, v.version, template.revision);
        if (!owns(owner)) return;
        await versions.mutate();
        if (owns(owner)) await onRevoked?.();
      } catch (error) {
        if (owns(owner)) throw error;
      }
    });
  }
  return (
    <Section title={t("版本历史", "Version history")}>
      {!template?.id && (
        <p className="text-muted-foreground">
          {t(
            "从模板库选择一个模板后查看其已发布版本。",
            "Pick a template from the library to inspect its published versions.",
          )}
        </p>
      )}
      {template?.id && (
        <LoadError
          error={versions.error}
          onRetry={() => void versions.mutate()}
        />
      )}
      {template?.id && (versions.isLoading || versions.isValidating) && (
        <p role="status" className="text-muted-foreground">
          {versions.data
            ? t("正在刷新版本历史…", "Refreshing version history…")
            : t("正在加载版本历史…", "Loading version history…")}
        </p>
      )}
      {template?.id && !versions.error && !versions.isLoading &&
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
                disabled={busy || !ready}
                data-testid={`revoke-version-${v.version}`}
                onClick={() => revokeVersion(v)}
                className="shrink-0 rounded-md border border-destructive px-2 py-1 text-xs font-medium text-destructive hover:bg-destructive/10 disabled:opacity-50"
              >
                {t("撤销", "Revoke")}
              </button>
            )}
          </div>
        ))}
      {ready && versions.data?.length === 0 && (
        <p className="text-muted-foreground">
          {t("该模板尚未发布任何版本", "This template has no published versions")}
        </p>
      )}
    </Section>
  );
}
