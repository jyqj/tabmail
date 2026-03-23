"use client";

import { useSettings, type Settings } from "@/lib/settings";
import { useI18n, type Locale } from "@/lib/i18n";
import { useTheme } from "next-themes";
import {
  Sheet,
  SheetTrigger,
  SheetContent,
  SheetHeader,
  SheetTitle,
  SheetDescription,
} from "@/components/ui/sheet";
import { Button } from "@/components/ui/button";
import { Switch } from "@/components/ui/switch";
import { Label } from "@/components/ui/label";
import { Separator } from "@/components/ui/separator";
import { Settings as SettingsIcon } from "lucide-react";
import { cn } from "@/lib/utils";

function OptionGroup({
  options,
  value,
  onChange,
}: {
  options: { value: string; label: string }[];
  value: string;
  onChange: (v: string) => void;
}) {
  return (
    <div className="flex gap-0.5 rounded-lg bg-muted p-1">
      {options.map((opt) => (
        <button
          key={opt.value}
          onClick={() => onChange(opt.value)}
          className={cn(
            "flex-1 rounded-md px-3 py-1.5 text-xs font-medium transition-colors cursor-pointer",
            value === opt.value
              ? "bg-background text-foreground shadow-sm"
              : "text-muted-foreground hover:text-foreground"
          )}
        >
          {opt.label}
        </button>
      ))}
    </div>
  );
}

function SettingRow({
  label,
  desc,
  children,
}: {
  label: string;
  desc?: string;
  children: React.ReactNode;
}) {
  return (
    <div className="flex items-center justify-between gap-4">
      <div className="min-w-0">
        <Label className="text-sm">{label}</Label>
        {desc && <p className="text-xs text-muted-foreground mt-0.5">{desc}</p>}
      </div>
      {children}
    </div>
  );
}

export function SettingsPanel() {
  const { settings, update } = useSettings();
  const { locale, setLocale, t } = useI18n();
  const { theme, setTheme } = useTheme();

  return (
    <Sheet>
      <SheetTrigger
        render={
          <Button variant="ghost" size="icon" className="h-8 w-8" aria-label="Settings" />
        }
      >
        <SettingsIcon className="h-4 w-4" />
      </SheetTrigger>
      <SheetContent side="right" className="w-80 overflow-y-auto">
        <SheetHeader>
          <SheetTitle>{t("settings.title")}</SheetTitle>
          <SheetDescription>{t("settings.desc")}</SheetDescription>
        </SheetHeader>

        <div className="space-y-6 px-4 pt-4 pb-8">
          {/* Language */}
          <div className="space-y-2">
            <Label className="text-[11px] font-medium text-muted-foreground uppercase tracking-wider">
              {t("settings.language")}
            </Label>
            <OptionGroup
              options={[
                { value: "zh", label: "中文" },
                { value: "en", label: "English" },
              ]}
              value={locale}
              onChange={(v) => setLocale(v as Locale)}
            />
          </div>

          {/* Theme */}
          <div className="space-y-2">
            <Label className="text-[11px] font-medium text-muted-foreground uppercase tracking-wider">
              {t("settings.theme")}
            </Label>
            <OptionGroup
              options={[
                { value: "system", label: t("settings.themeSystem") },
                { value: "light", label: t("settings.themeLight") },
                { value: "dark", label: t("settings.themeDark") },
              ]}
              value={theme ?? "system"}
              onChange={setTheme}
            />
          </div>

          <Separator />

          {/* Auto-refresh */}
          <SettingRow label={t("settings.autoRefresh")} desc={t("settings.autoRefreshDesc")}>
            <Switch
              checked={settings.autoRefresh}
              onCheckedChange={(checked: boolean) => update({ autoRefresh: checked })}
            />
          </SettingRow>

          {settings.autoRefresh && (
            <div className="space-y-2">
              <Label className="text-[11px] font-medium text-muted-foreground uppercase tracking-wider">
                {t("settings.refreshInterval")}
              </Label>
              <OptionGroup
                options={[
                  { value: "5", label: "5s" },
                  { value: "10", label: "10s" },
                  { value: "30", label: "30s" },
                  { value: "60", label: "60s" },
                ]}
                value={String(settings.refreshInterval)}
                onChange={(v) => update({ refreshInterval: Number(v) })}
              />
            </div>
          )}

          {/* SSE */}
          <SettingRow label={t("settings.preferSSE")} desc={t("settings.preferSSEDesc")}>
            <Switch
              checked={settings.preferSSE}
              onCheckedChange={(checked: boolean) => update({ preferSSE: checked })}
            />
          </SettingRow>

          <Separator />

          {/* Default tab */}
          <div className="space-y-2">
            <Label className="text-[11px] font-medium text-muted-foreground uppercase tracking-wider">
              {t("settings.defaultTab")}
            </Label>
            <OptionGroup
              options={[
                { value: "html", label: "HTML" },
                { value: "text", label: t("msgDetail.text") },
                { value: "source", label: t("msgDetail.source") },
              ]}
              value={settings.defaultTab}
              onChange={(v) => update({ defaultTab: v as Settings["defaultTab"] })}
            />
          </div>

          {/* Time format */}
          <div className="space-y-2">
            <Label className="text-[11px] font-medium text-muted-foreground uppercase tracking-wider">
              {t("settings.timeFormat")}
            </Label>
            <OptionGroup
              options={[
                { value: "relative", label: t("settings.timeRelative") },
                { value: "absolute", label: t("settings.timeAbsolute") },
              ]}
              value={settings.timeFormat}
              onChange={(v) => update({ timeFormat: v as Settings["timeFormat"] })}
            />
          </div>
        </div>
      </SheetContent>
    </Sheet>
  );
}
