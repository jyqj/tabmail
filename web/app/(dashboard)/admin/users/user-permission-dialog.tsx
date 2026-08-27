"use client";

import { useState } from "react";
import { Gauge, Trash2 } from "lucide-react";
import { toast } from "sonner";

import {
  getUserPermission,
  setUserPermissionOverride,
  deleteUserPermissionOverride,
  updateUser,
} from "@/lib/api";
import type {
  AdminUser,
  DomainZone,
  PermissionProfile,
  UserPermissionOverride,
} from "@/lib/types";
import { useI18n } from "@/lib/i18n";
import { useCRUDPage } from "@/hooks/use-crud-page";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { Skeleton } from "@/components/ui/skeleton";

export const NONE_PROFILE = "__none__";

interface PermOverrideForm {
  can_send: boolean | null;
  daily_send_quota: string;
  daily_receive_quota: string;
  max_mailboxes: string;
  max_domains: string;
  allowed_zone_ids: string[] | null;
  can_create_domains: boolean | null;
  can_create_routes: boolean | null;
  can_create_api_keys: boolean | null;
}

const emptyOverrideForm: PermOverrideForm = {
  can_send: null,
  daily_send_quota: "",
  daily_receive_quota: "",
  max_mailboxes: "",
  max_domains: "",
  allowed_zone_ids: null,
  can_create_domains: null,
  can_create_routes: null,
  can_create_api_keys: null,
};

interface UserPermissionDialogProps {
  /** The user whose permissions are being edited, or null when closed. */
  user: AdminUser | null;
  profiles: PermissionProfile[];
  domains: DomainZone[];
  onClose: () => void;
  /** Called after a change that the user list has to pick up. */
  onUserChanged: () => void;
}

export function UserPermissionDialog({
  user,
  profiles,
  domains,
  onClose,
  onUserChanged,
}: UserPermissionDialogProps) {
  const { t } = useI18n();
  const userId = user?.id ?? null;

  const [form, setForm] = useState<PermOverrideForm>(emptyOverrideForm);
  const [profileId, setProfileId] = useState(user?.permission_profile_id || NONE_PROFILE);
  const [saving, setSaving] = useState(false);
  const [resetting, setResetting] = useState(false);

  // The dialog stays mounted so it can animate out, so opening it for a
  // different user has to clear whatever the previous one left behind.
  const [editedUserId, setEditedUserId] = useState(userId);
  if (userId !== editedUserId) {
    setEditedUserId(userId);
    setForm(emptyOverrideForm);
    setProfileId(user?.permission_profile_id || NONE_PROFILE);
  }

  const { data: effectiveRes, mutate: refreshEffective } = useCRUDPage(
    userId ? ["user-permission", userId] : null,
    () => getUserPermission(userId as string),
    "admin.permLoadFailed",
  );
  const effective = effectiveRes?.data ?? null;

  const domainLabel = (id: string) =>
    domains.find((domain) => domain.id === id)?.domain ?? id.slice(0, 8);

  const handleProfileChange = async (value: string | null) => {
    if (!user) return;
    const selection = value ?? NONE_PROFILE;
    const newProfileId = selection === NONE_PROFILE ? null : selection;
    const previous = profileId;
    setProfileId(selection);
    try {
      await updateUser(user.id, { permission_profile_id: newProfileId });
      toast.success(t("admin.permProfileUpdated"));
      onUserChanged();
      refreshEffective();
    } catch (e: unknown) {
      const err = e as { error?: { message?: string } };
      toast.error(err?.error?.message || t("admin.permProfileUpdateFailed"));
      setProfileId(previous);
    }
  };

  const handleSave = async () => {
    if (!user) return;
    setSaving(true);
    try {
      const body: Partial<UserPermissionOverride> = {};
      if (form.can_send !== null) body.can_send = form.can_send;
      if (form.daily_send_quota.trim() !== "") body.daily_send_quota = Number(form.daily_send_quota);
      if (form.daily_receive_quota.trim() !== "") body.daily_receive_quota = Number(form.daily_receive_quota);
      if (form.max_mailboxes.trim() !== "") body.max_mailboxes = Number(form.max_mailboxes);
      if (form.max_domains.trim() !== "") body.max_domains = Number(form.max_domains);
      if (form.allowed_zone_ids !== null) body.allowed_zone_ids = form.allowed_zone_ids;
      if (form.can_create_domains !== null) body.can_create_domains = form.can_create_domains;
      if (form.can_create_routes !== null) body.can_create_routes = form.can_create_routes;
      if (form.can_create_api_keys !== null) body.can_create_api_keys = form.can_create_api_keys;

      await setUserPermissionOverride(user.id, body);
      toast.success(t("admin.permSaved"));
      refreshEffective();
    } catch (e: unknown) {
      const err = e as { error?: { message?: string } };
      toast.error(err?.error?.message || t("admin.permSaveFailed"));
    } finally {
      setSaving(false);
    }
  };

  const handleReset = async () => {
    if (!user) return;
    setResetting(true);
    try {
      await deleteUserPermissionOverride(user.id);
      toast.success(t("admin.permResetSuccess"));
      setForm(emptyOverrideForm);
      refreshEffective();
    } catch (e: unknown) {
      const err = e as { error?: { message?: string } };
      toast.error(err?.error?.message || t("admin.permResetFailed"));
    } finally {
      setResetting(false);
    }
  };

  const effectiveEntries: { key: string; label: string; value: string }[] = effective
    ? [
        { key: "can_send", label: t("admin.permCanSend"), value: effective.can_send ? "true" : "false" },
        { key: "daily_send_quota", label: t("admin.permDailySendQuota"), value: String(effective.daily_send_quota) },
        { key: "daily_receive_quota", label: t("admin.permDailyReceiveQuota"), value: String(effective.daily_receive_quota) },
        { key: "max_mailboxes", label: t("admin.permMaxMailboxes"), value: String(effective.max_mailboxes) },
        { key: "max_domains", label: t("admin.permMaxDomains"), value: String(effective.max_domains) },
        {
          key: "allowed_zone_ids",
          label: t("admin.permAllowedZoneScope"),
          value: effective.allowed_zone_ids?.length
            ? effective.allowed_zone_ids.map(domainLabel).join(", ")
            : t("admin.permAllDomains"),
        },
        { key: "can_create_domains", label: t("admin.permCanCreateDomains"), value: effective.can_create_domains ? "true" : "false" },
        { key: "can_create_routes", label: t("admin.permCanCreateRoutes"), value: effective.can_create_routes ? "true" : "false" },
        { key: "can_create_api_keys", label: t("admin.permCanCreateApiKeys"), value: effective.can_create_api_keys ? "true" : "false" },
      ]
    : [];

  const booleanOverrides: { key: keyof PermOverrideForm; label: string }[] = [
    { key: "can_send", label: t("admin.permCanSend") },
    { key: "can_create_domains", label: t("admin.permCanCreateDomains") },
    { key: "can_create_routes", label: t("admin.permCanCreateRoutes") },
    { key: "can_create_api_keys", label: t("admin.permCanCreateApiKeys") },
  ];

  const quotaOverrides: { key: keyof PermOverrideForm; label: string }[] = [
    { key: "daily_send_quota", label: t("admin.permDailySendQuota") },
    { key: "daily_receive_quota", label: t("admin.permDailyReceiveQuota") },
    { key: "max_mailboxes", label: t("admin.permMaxMailboxes") },
    { key: "max_domains", label: t("admin.permMaxDomains") },
  ];

  return (
    <Dialog open={user !== null} onOpenChange={(open) => { if (!open) onClose(); }}>
      <DialogContent className="sm:max-w-3xl">
        <DialogHeader>
          <DialogTitle>{t("admin.permTitle")}</DialogTitle>
          <DialogDescription>
            {t("admin.permDesc", { name: user?.email ?? "" })}
          </DialogDescription>
        </DialogHeader>

        <div className="grid gap-6 py-4 lg:grid-cols-2">
          {/* Left: Effective permissions (read-only) */}
          <Card className="border-primary/10 bg-[radial-gradient(circle_at_top,rgba(99,102,241,0.08),transparent_35%),var(--card)]">
            <CardHeader>
              <CardTitle className="flex items-center gap-2 text-base">
                <Gauge className="h-4 w-4 text-primary" />
                {t("admin.permEffective")}
              </CardTitle>
              <CardDescription>{t("admin.permEffectiveDesc")}</CardDescription>
            </CardHeader>
            <CardContent className="space-y-3">
              {effective ? (
                effectiveEntries.map((entry) => (
                  <div key={entry.key} className="flex items-center justify-between gap-3 text-sm">
                    <span className="text-muted-foreground">{entry.label}</span>
                    <span className="font-medium tabular-nums">{entry.value}</span>
                  </div>
                ))
              ) : (
                <div className="space-y-3">
                  {Array.from({ length: 8 }).map((_, i) => (
                    <Skeleton key={i} className="h-6 w-full" />
                  ))}
                </div>
              )}
            </CardContent>
          </Card>

          {/* Right: Override form */}
          <div className="space-y-4">
            <div className="space-y-2">
              <Label>{t("admin.permProfileLabel")}</Label>
              <Select value={profileId} onValueChange={handleProfileChange}>
                <SelectTrigger>
                  <SelectValue placeholder={t("admin.permSelectProfile")} />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value={NONE_PROFILE}>{t("admin.permDefault")}</SelectItem>
                  {profiles.map((profile) => (
                    <SelectItem key={profile.id} value={profile.id}>
                      {profile.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>

            <div className="border-t pt-4 space-y-4">
              <p className="text-sm font-medium text-muted-foreground">{t("admin.permOverride")}</p>

              {booleanOverrides.map(({ key, label }) => {
                const value = form[key] as boolean | null;
                return (
                  <div key={key} className="flex items-center justify-between">
                    <Label>{label}</Label>
                    <div className="flex items-center gap-2">
                      <span className="text-xs text-muted-foreground">{t("admin.permInherit")}</span>
                      <Switch
                        size="sm"
                        checked={value ?? false}
                        onCheckedChange={(checked) => setForm((prev) => ({ ...prev, [key]: checked }))}
                      />
                      {value !== null && (
                        <Button
                          variant="ghost"
                          size="icon"
                          className="h-6 w-6"
                          onClick={() => setForm((prev) => ({ ...prev, [key]: null }))}
                        >
                          <Trash2 className="h-3 w-3" />
                        </Button>
                      )}
                    </div>
                  </div>
                );
              })}

              <div className="space-y-3 rounded-md border p-3">
                <div className="flex items-center justify-between gap-3">
                  <div>
                    <Label>{t("admin.permAllowedZoneScope")}</Label>
                    <p className="text-xs text-muted-foreground">
                      {t("admin.permAllowedZoneHint")}
                    </p>
                  </div>
                  <div className="flex shrink-0 gap-2">
                    <Button
                      type="button"
                      variant={form.allowed_zone_ids === null ? "default" : "outline"}
                      size="sm"
                      onClick={() => setForm((prev) => ({ ...prev, allowed_zone_ids: null }))}
                    >
                      {t("admin.permInheritShort")}
                    </Button>
                    <Button
                      type="button"
                      variant={form.allowed_zone_ids !== null && form.allowed_zone_ids.length === 0 ? "default" : "outline"}
                      size="sm"
                      onClick={() => setForm((prev) => ({ ...prev, allowed_zone_ids: [] }))}
                    >
                      {t("admin.permAll")}
                    </Button>
                  </div>
                </div>

                {domains.length === 0 ? (
                  <p className="text-xs text-muted-foreground">{t("admin.permNoDomains")}</p>
                ) : (
                  <div className="grid gap-2">
                    {domains.map((domain) => {
                      const selected = form.allowed_zone_ids?.includes(domain.id) ?? false;
                      return (
                        <label
                          key={domain.id}
                          className="flex items-center justify-between rounded border px-3 py-2 text-sm"
                        >
                          <span className="truncate">{domain.domain}</span>
                          <Switch
                            size="sm"
                            checked={selected}
                            onCheckedChange={(checked) =>
                              setForm((prev) => {
                                const current = prev.allowed_zone_ids ?? [];
                                return {
                                  ...prev,
                                  allowed_zone_ids: checked
                                    ? Array.from(new Set([...current, domain.id]))
                                    : current.filter((id) => id !== domain.id),
                                };
                              })
                            }
                          />
                        </label>
                      );
                    })}
                  </div>
                )}
              </div>

              {quotaOverrides.map(({ key, label }) => (
                <div key={key} className="space-y-2">
                  <Label>{label}</Label>
                  <Input
                    type="number"
                    placeholder={t("admin.permInherit")}
                    value={form[key] as string}
                    onChange={(e) => setForm((prev) => ({ ...prev, [key]: e.target.value }))}
                  />
                  <p className="text-xs text-muted-foreground">{t("admin.permZeroUnlimited")}</p>
                </div>
              ))}
            </div>
          </div>
        </div>

        <DialogFooter className="gap-2">
          <Button variant="outline" onClick={handleReset} disabled={resetting || !user}>
            {resetting ? t("admin.permResetting") : t("admin.permReset")}
          </Button>
          <Button onClick={handleSave} disabled={saving || !user}>
            {saving ? t("admin.permSaving") : t("admin.permSave")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
