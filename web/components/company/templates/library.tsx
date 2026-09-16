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
  selectedId,
  onSelect,
  onRetire,
}: {
  templates: MailTemplate[] | undefined;
  busy: boolean;
  selectedId?: string;
  onSelect: (template: MailTemplate) => void;
  onRetire: (template: MailTemplate) => Promise<void>;
}) {
  const t = useText();
  return (
    <Section title={t("模板库", "Template library")}>
      {(templates ?? []).map((v) => (
        <div
          key={v.id}
          className="flex flex-wrap justify-between gap-3 border-b pb-3"
        >
          <button
            className="text-left"
            onClick={() => onSelect(v)}
            data-active={v.id === selectedId || undefined}
          >
            <p className="font-medium">{v.name}</p>
            <p className="text-xs text-muted-foreground">
              {t("草稿修订", "Draft revision")} {v.revision} ·{" "}
              {v.retired ? t("已停用", "Retired") : t("可用", "Active")}
            </p>
          </button>
          <ActionButton
            disabled={busy}
            onClick={() => onRetire(v)}
          >
            {v.retired
              ? t("重新启用", "Reactivate")
              : t("停用（包括待发任务）", "Retire (including queued sends)")}
          </ActionButton>
        </div>
      ))}
      {!templates?.length && <p>{t("尚无模板", "No templates yet")}</p>}
    </Section>
  );
}
