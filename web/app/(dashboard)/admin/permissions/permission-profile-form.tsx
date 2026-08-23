"use client";

import type { Dispatch, SetStateAction } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import type { DomainZone, Tenant } from "@/lib/types";
import { useI18n } from "@/lib/i18n";

export const GLOBAL_PROFILE_SCOPE = "__global__";

export interface PermissionFormData {
  tenant_id: string | null;
  name: string;
  description: string;
  can_send: boolean;
  daily_send_quota: string;
  daily_receive_quota: string;
  max_mailboxes: string;
  max_domains: string;
  allowed_zone_ids: string[];
  can_create_domains: boolean;
  can_create_routes: boolean;
  can_create_api_keys: boolean;
}

export const defaultPermissionForm: PermissionFormData = {
  tenant_id: null,
  name: "",
  description: "",
  can_send: true,
  daily_send_quota: "0",
  daily_receive_quota: "0",
  max_mailboxes: "0",
  max_domains: "0",
  allowed_zone_ids: [],
  can_create_domains: false,
  can_create_routes: false,
  can_create_api_keys: false,
};

export function shortId(id: string): string {
  return `${id.slice(0, 8)}…`;
}

function zoneLabel(zone: DomainZone): string {
  return zone.domain || shortId(zone.id);
}

export function PermissionFormFields({
  form,
  setForm,
  domainOptions,
  isPlatformAdmin,
  tenants,
  tenantScopeLocked = false,
}: {
  form: PermissionFormData;
  setForm: Dispatch<SetStateAction<PermissionFormData>>;
  domainOptions: DomainZone[];
  isPlatformAdmin: boolean;
  tenants: Tenant[];
  tenantScopeLocked?: boolean;
}) {
  const { t } = useI18n();
  const scopedDomainOptions =
    isPlatformAdmin && form.tenant_id
      ? domainOptions.filter((zone) => zone.tenant_id === form.tenant_id)
      : isPlatformAdmin
        ? []
        : domainOptions;
  const domainPickerDisabled = isPlatformAdmin && !form.tenant_id;
  const switchFields: { key: keyof PermissionFormData; label: string }[] = [
    { key: "can_send", label: t("permissions.canSend") },
    { key: "can_create_domains", label: t("permissions.canCreateDomains") },
    { key: "can_create_routes", label: t("permissions.canCreateRoutes") },
    { key: "can_create_api_keys", label: t("permissions.canCreateApiKeys") },
  ];

  const numberFields: { key: keyof PermissionFormData; label: string }[] = [
    { key: "daily_send_quota", label: t("permissions.dailySendQuota") },
    { key: "daily_receive_quota", label: t("permissions.dailyReceiveQuota") },
    { key: "max_mailboxes", label: t("permissions.maxMailboxes") },
    { key: "max_domains", label: t("permissions.maxDomains") },
  ];

  return (
    <div className="space-y-4 py-4 max-h-[60vh] overflow-y-auto">
      <div className="space-y-1.5">
        <Label className="text-xs">{t("permissions.name")}</Label>
        <Input
          value={form.name}
          onChange={(e) =>
            setForm((prev) => ({ ...prev, name: e.target.value }))
          }
          placeholder={t("permissions.namePlaceholder")}
        />
      </div>
      <div className="space-y-1.5">
        <Label className="text-xs">{t("permissions.descriptionField")}</Label>
        <Input
          value={form.description}
          onChange={(e) =>
            setForm((prev) => ({ ...prev, description: e.target.value }))
          }
          placeholder={t("permissions.descriptionPlaceholder")}
        />
      </div>

      {isPlatformAdmin && (
        <div className="space-y-1.5">
          <Label className="text-xs">{t("permissions.scope")}</Label>
          <Select
            value={form.tenant_id ?? GLOBAL_PROFILE_SCOPE}
            disabled={tenantScopeLocked}
            onValueChange={(value) =>
              setForm((prev) => {
                const nextTenantID = value === GLOBAL_PROFILE_SCOPE ? null : value;
                return {
                  ...prev,
                  tenant_id: nextTenantID,
                  allowed_zone_ids: nextTenantID
                    ? prev.allowed_zone_ids.filter((id) =>
                        domainOptions.some(
                          (zone) => zone.id === id && zone.tenant_id === nextTenantID,
                        ),
                      )
                    : [],
                };
              })
            }
          >
            <SelectTrigger className="w-full">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value={GLOBAL_PROFILE_SCOPE}>{t("permissions.scopeGlobalOption")}</SelectItem>
              {tenants.map((tenant) => (
                <SelectItem key={tenant.id} value={tenant.id}>
                  {tenant.name} · {shortId(tenant.id)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <p className="text-[10px] text-muted-foreground">
            {tenantScopeLocked
              ? t("permissions.scopeLockedHint")
              : t("permissions.scopeHint")}
          </p>
        </div>
      )}

      <div className="grid grid-cols-2 gap-3">
        {switchFields.map((f) => (
          <div
            key={f.key}
            className="flex items-center justify-between rounded-md border p-3"
          >
            <Label className="text-xs font-normal">{f.label}</Label>
            <Switch
              size="sm"
              checked={form[f.key] as boolean}
              onCheckedChange={(checked: boolean) =>
                setForm((prev) => ({ ...prev, [f.key]: checked }))
              }
            />
          </div>
        ))}
      </div>

      <div className="grid grid-cols-2 gap-3">
        {numberFields.map((f) => (
          <div key={f.key} className="space-y-1.5">
            <Label className="text-xs">{f.label}</Label>
            <Input
              type="number"
              min={0}
              value={form[f.key] as string}
              onChange={(e) =>
                setForm((prev) => ({ ...prev, [f.key]: e.target.value }))
              }
              placeholder="0"
            />
            <p className="text-[10px] text-muted-foreground">
              {t("permissions.zeroUnlimited")}
            </p>
          </div>
        ))}
      </div>

      <div className="space-y-2">
        <div className="flex items-center justify-between">
          <Label className="text-xs">{t("permissions.allowedZones")}</Label>
          <span className="text-[10px] text-muted-foreground">
            {form.allowed_zone_ids.length === 0
              ? t("permissions.allDomains")
              : t("permissions.domainCount", { count: form.allowed_zone_ids.length })}
          </span>
        </div>
        <div className="rounded-md border p-3 space-y-2">
          <Button
            type="button"
            variant="ghost"
            size="sm"
            className="h-7 px-2 text-xs"
            onClick={() => setForm((prev) => ({ ...prev, allowed_zone_ids: [] }))}
            disabled={domainPickerDisabled}
          >
            {t("permissions.allowAllDomains")}
          </Button>
          {domainPickerDisabled ? (
            <p className="text-xs text-muted-foreground">
              {t("permissions.globalNoDomainsHint")}
            </p>
          ) : scopedDomainOptions.length === 0 ? (
            <p className="text-xs text-muted-foreground">{t("permissions.noDomainsHint")}</p>
          ) : (
            <div className="grid gap-2">
              {scopedDomainOptions.map((zone) => {
                const checked = form.allowed_zone_ids.includes(zone.id);
                return (
                  <label key={zone.id} className="flex items-center justify-between gap-3 rounded border px-2 py-1.5 text-xs">
                    <span className="truncate" title={zone.domain}>{zoneLabel(zone)}</span>
                    <Switch
                      size="sm"
                      checked={checked}
                      onCheckedChange={(next: boolean) =>
                        setForm((prev) => ({
                          ...prev,
                          allowed_zone_ids: next
                            ? Array.from(new Set([...prev.allowed_zone_ids, zone.id]))
                            : prev.allowed_zone_ids.filter((id) => id !== zone.id),
                        }))
                      }
                    />
                  </label>
                );
              })}
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
