"use client";

import { useEffect, useState, useMemo } from "react";
import { toast } from "sonner";
import { Settings2 } from "lucide-react";

import { listSettings, updateSettings } from "@/lib/api";
import type { SystemSetting } from "@/lib/types";
import { PageHeader } from "@/components/layout/page-header";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { Skeleton } from "@/components/ui/skeleton";
import { useAPI } from "@/hooks/use-api";

// Well-known setting definitions for rendering
const SETTING_DEFS: Record<string, { label: string; type: "int" | "bool" | "select"; options?: string[]; group: string }> = {
  auto_create_route_rpm:   { label: "Auto-Create Route RPM",        type: "int",    group: "SMTP / Ingest" },
  auto_create_tenant_rpm:  { label: "Auto-Create Tenant RPM",       type: "int",    group: "SMTP / Ingest" },
  mailbox_naming:          { label: "Mailbox Naming Mode",           type: "select", options: ["full", "local", "domain"], group: "SMTP / Ingest" },
  strip_plus_tag:          { label: "Strip +tag from Local Part",    type: "bool",   group: "SMTP / Ingest" },
  fallback_retention_hours:{ label: "Fallback Retention (hours)",    type: "int",    group: "Storage" },
  monitor_history:         { label: "Monitor Event History Size",    type: "int",    group: "Monitoring" },
  open_registration:       { label: "Open Registration",             type: "bool",   group: "Auth" },
  public_ip_rpm:           { label: "Public IP RPM (0=disable)",     type: "int",    group: "Rate Limiting" },
};

export default function AdminSettingsPage() {
  const { data: settingsRes, isLoading: loading, error: settingsError, mutate: mutateSettings } = useAPI(
    "system-settings",
    () => listSettings(),
  );
  const settings = useMemo(() => settingsRes?.data ?? [], [settingsRes?.data]);

  useEffect(() => { if (settingsError) toast.error("Failed to load settings"); }, [settingsError]);

  const [saving, setSaving] = useState(false);
  const [edits, setEdits] = useState<Record<string, string>>({});
  const [editsInit, setEditsInit] = useState(false);

  useEffect(() => {
    if (settings.length > 0 && !editsInit) {
      const initial: Record<string, string> = {};
      for (const s of settings) {
        initial[s.key] = s.value;
      }
      setEdits(initial);
      setEditsInit(true);
    }
  }, [settings, editsInit]);

  const handleSave = async () => {
    setSaving(true);
    try {
      // Only send changed values
      const changed: Record<string, string> = {};
      for (const s of settings) {
        if (edits[s.key] !== undefined && edits[s.key] !== s.value) {
          changed[s.key] = edits[s.key];
        }
      }
      if (Object.keys(changed).length === 0) {
        toast.info("No changes to save");
        setSaving(false);
        return;
      }
      const res = await updateSettings(changed);
      const updated: Record<string, string> = {};
      for (const s of res.data || []) {
        updated[s.key] = s.value;
      }
      setEdits(updated);
      setEditsInit(true);
      mutateSettings();
      toast.success("Settings saved");
    } catch (e: unknown) {
      const err = e as { error?: { message?: string } };
      toast.error(err?.error?.message || "Failed to save settings");
    } finally {
      setSaving(false);
    }
  };

  // Group settings
  const groups = useMemo(() => {
    const map = new Map<string, { key: string; value: string; def: (typeof SETTING_DEFS)[string] | null; setting: SystemSetting }[]>();
    for (const s of settings) {
      const def = SETTING_DEFS[s.key] || null;
      const group = def?.group || "Other";
      if (!map.has(group)) map.set(group, []);
      map.get(group)!.push({ key: s.key, value: edits[s.key] ?? s.value, def, setting: s });
    }
    return map;
  }, [settings, edits]);

  return (
    <div className="flex flex-col">
      <PageHeader
        title="System Settings"
        description="Runtime configuration persisted to database. Changes take effect within seconds."
        actions={
          <Button onClick={handleSave} disabled={loading || saving}>
            {saving ? "Saving..." : "Save Changes"}
          </Button>
        }
      />

      <div className="space-y-4 p-4">
        {loading ? (
          <Card>
            <CardContent className="p-6">
              {Array.from({ length: 6 }).map((_, i) => (
                <Skeleton key={i} className="h-12 w-full mb-3" />
              ))}
            </CardContent>
          </Card>
        ) : (
          Array.from(groups.entries()).map(([group, items]) => (
            <Card key={group} className="border-primary/10 bg-[radial-gradient(circle_at_top,rgba(99,102,241,0.08),transparent_35%),var(--card)]">
              <CardHeader>
                <CardTitle className="flex items-center gap-2">
                  <Settings2 className="h-4 w-4 text-primary" />
                  {group}
                </CardTitle>
                <CardDescription>
                  {group === "SMTP / Ingest" && "Controls how incoming mail is processed and mailboxes are auto-created."}
                  {group === "Storage" && "Data retention and storage configuration."}
                  {group === "Monitoring" && "Real-time monitoring configuration."}
                  {group === "Auth" && "Authentication and registration settings."}
                  {group === "Rate Limiting" && "Request rate limiting for unauthenticated access."}
                </CardDescription>
              </CardHeader>
              <CardContent className="space-y-4">
                {items.map(({ key, value, def, setting }) => (
                  <SettingField
                    key={key}
                    settingKey={key}
                    value={value}
                    label={def?.label || key}
                    description={setting.description}
                    type={def?.type || "int"}
                    options={def?.options}
                    onChange={(v) => setEdits((prev) => ({ ...prev, [key]: v }))}
                  />
                ))}
              </CardContent>
            </Card>
          ))
        )}
      </div>
    </div>
  );
}

function SettingField({
  settingKey,
  value,
  label,
  description,
  type,
  options,
  onChange,
}: {
  settingKey: string;
  value: string;
  label: string;
  description: string;
  type: "int" | "bool" | "select";
  options?: string[];
  onChange: (value: string) => void;
}) {
  if (type === "bool") {
    return (
      <div className="flex items-center justify-between rounded-xl border bg-background px-4 py-3">
        <div>
          <Label className="font-medium">{label}</Label>
          {description && <p className="text-xs text-muted-foreground mt-0.5">{description}</p>}
        </div>
        <Switch
          checked={value === "true"}
          onCheckedChange={(checked) => onChange(checked ? "true" : "false")}
        />
      </div>
    );
  }

  if (type === "select" && options) {
    return (
      <div className="space-y-2">
        <div>
          <Label className="font-medium">{label}</Label>
          {description && <p className="text-xs text-muted-foreground mt-0.5">{description}</p>}
        </div>
        <div className="flex gap-2">
          {options.map((opt) => (
            <Button
              key={opt}
              variant={value === opt ? "default" : "outline"}
              size="sm"
              onClick={() => onChange(opt)}
            >
              {opt}
            </Button>
          ))}
        </div>
      </div>
    );
  }

  return (
    <div className="space-y-2">
      <div>
        <Label htmlFor={`setting-${settingKey}`} className="font-medium">{label}</Label>
        {description && <p className="text-xs text-muted-foreground mt-0.5">{description}</p>}
      </div>
      <Input
        id={`setting-${settingKey}`}
        type="number"
        value={value}
        onChange={(e) => onChange(e.target.value)}
        className="max-w-[200px]"
      />
    </div>
  );
}
