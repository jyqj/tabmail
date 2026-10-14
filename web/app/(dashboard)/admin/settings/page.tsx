"use client";

import { useEffect, useRef, useState, useMemo } from "react";
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
import { sessionScope, useSessionScope } from "@/lib/session";
import { LoadError } from "@/components/company/common";

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
  const scope = useSessionScope();
  return <SettingsEditor key={scope} scope={scope} />;
}

function SettingsEditor({ scope }: { scope: string }) {
  const mounted = useRef(true);
  const operation = useRef(false);
  const read = useRef({ generation: 0, pending: false });
  const current = () => mounted.current && scope === sessionScope();
  useEffect(() => {
    mounted.current = true;
    return () => { mounted.current = false; };
  }, []);
  const { data: settingsRes, isLoading: loading, isValidating, error: settingsError, mutate: mutateSettings } = useAPI(
    "system-settings",
    async () => {
      const generation = ++read.current.generation;
      read.current.pending = true;
      try { return await listSettings(); }
      finally { if (generation === read.current.generation) read.current.pending = false; }
    },
  );
  const settings = useMemo(() => settingsRes?.data ?? [], [settingsRes]);

  useEffect(() => { if (settingsError) toast.error("Failed to load settings"); }, [settingsError]);

  const [saving, setSaving] = useState(false);
  // Only explicit edits belong to the draft. Unedited fields always follow the
  // latest successful snapshot. Entry identity distinguishes edits made while
  // a PATCH is pending, including a deliberate return to its old value.
  const [edits, setEdits] = useState<Record<string, { value: string }>>({});
  const blocked = !settingsRes || loading || isValidating || !!settingsError || saving;
  function change(key: string, value: string) {
    if (!current()) return;
    const reverted = !operation.current && settings.find(setting => setting.key === key)?.value === value;
    setEdits(previous => {
      const next = { ...previous };
      if (reverted) delete next[key];
      else next[key] = { value };
      return next;
    });
  }
  const reload = () => {
    if (!current() || operation.current || read.current.pending) return;
    void mutateSettings().catch(() => {});
  };

  const handleSave = async () => {
    if (!current() || blocked || operation.current || read.current.pending) return;
    const submitted: typeof edits = {};
    const changed: Record<string, string> = {};
    for (const setting of settings) {
      const edit = edits[setting.key];
      if (edit && edit.value !== setting.value) {
        submitted[setting.key] = edit;
        changed[setting.key] = edit.value;
      }
    }
    if (Object.keys(changed).length === 0) {
      toast.info("No changes to save");
      return;
    }
    operation.current = true;
    setSaving(true);
    try {
      const res = await updateSettings(changed);
      if (!current()) return;
      await mutateSettings(res, { revalidate: false });
      if (!current()) return;
      setEdits(previous => {
        const next = { ...previous };
        for (const key of Object.keys(submitted)) {
          if (next[key] === submitted[key]) delete next[key];
        }
        return next;
      });
      toast.success("Settings saved");
      // Re-read after the acknowledgement. Failure blocks another write but
      // does not discard newer draft entries or report the PATCH as failed.
      await mutateSettings().catch(() => {});
    } catch (e: unknown) {
      if (!current()) return;
      const err = e as { error?: { message?: string } };
      toast.error(err?.error?.message || "Failed to save settings");
    } finally {
      operation.current = false;
      if (current()) setSaving(false);
    }
  };

  // Group settings
  const groups = useMemo(() => {
    const map = new Map<string, { key: string; value: string; def: (typeof SETTING_DEFS)[string] | null; setting: SystemSetting }[]>();
    for (const s of settings) {
      const def = SETTING_DEFS[s.key] || null;
      const group = def?.group || "Other";
      if (!map.has(group)) map.set(group, []);
      map.get(group)!.push({ key: s.key, value: edits[s.key]?.value ?? s.value, def, setting: s });
    }
    return map;
  }, [settings, edits]);

  return (
    <div className="flex flex-col">
      <PageHeader
        title="System Settings"
        description="Runtime configuration persisted to database. Changes take effect within seconds."
        actions={
          <Button onClick={handleSave} disabled={blocked}>
            {saving ? "Saving..." : "Save Changes"}
          </Button>
        }
      />

      <div className="space-y-4 p-4">
        <LoadError error={settingsError} onRetry={reload} />
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
                    onChange={(v) => change(key, v)}
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
