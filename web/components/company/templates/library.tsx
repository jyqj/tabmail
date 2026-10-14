"use client";
import {
  ActionButton,
  Section,
  useText,
} from "@/components/company/common";
import type { MailTemplate } from "@/lib/company";

// TemplateLibraryView lists every company template with its draft revision,
// retired flag and the retire/reactivate switch. Selecting a template hands it
// to the editor view.
export function TemplateLibraryView({
  templates,
  busy,
  isLoading,
  isValidating,
  error,
  selectedId,
  onSelect,
  onRetire,
}: {
  templates: MailTemplate[] | undefined;
  busy: boolean;
  isLoading: boolean;
  isValidating: boolean;
  error: unknown;
  selectedId?: string;
  onSelect: (template: MailTemplate) => void;
  onRetire: (template: MailTemplate) => Promise<void>;
}) {
  const t = useText();
  const ready = !error && !isLoading && !isValidating && Array.isArray(templates);
  return (
    <Section title={t("模板库", "Template library")}>
      {(isLoading || isValidating) && (
        <p role="status" className="text-muted-foreground">
          {templates
            ? t("正在刷新模板库…", "Refreshing template library…")
            : t("正在加载模板库…", "Loading template library…")}
        </p>
      )}
      {!error && !isLoading && Array.isArray(templates) && templates.map((v) => (
        <div
          key={v.id}
          className="flex flex-wrap justify-between gap-3 border-b pb-3"
        >
          <button
            className="text-left"
            disabled={!ready}
            onClick={() => { if (ready) onSelect(v); }}
            data-active={v.id === selectedId || undefined}
          >
            <p className="font-medium">{v.name}</p>
            <p className="text-xs text-muted-foreground">
              {t("草稿修订", "Draft revision")} {v.revision} ·{" "}
              {v.retired ? t("已停用", "Retired") : t("可用", "Active")}
            </p>
          </button>
          <ActionButton
            disabled={busy || !ready}
            onClick={() => { if (!busy && ready) void onRetire(v); }}
          >
            {v.retired
              ? t("重新启用", "Reactivate")
              : t("停用（包括待发任务）", "Retire (including queued sends)")}
          </ActionButton>
        </div>
      ))}
      {ready && templates?.length === 0 && <p>{t("尚无模板", "No templates yet")}</p>}
    </Section>
  );
}
