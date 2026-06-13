"use client";

import { useState, type Dispatch, type SetStateAction } from "react";
import { PageHeader } from "@/components/layout/page-header";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Badge } from "@/components/ui/badge";
import { Switch } from "@/components/ui/switch";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import {
  listPermissionProfiles,
  createPermissionProfile,
  updatePermissionProfile,
  deletePermissionProfile,
  listDomains,
  listAdminDomains,
  listTenants,
} from "@/lib/api";
import type { DomainZone, PermissionProfile, Tenant } from "@/lib/types";
import {
  Plus,
  MoreHorizontal,
  Trash2,
  Shield,
  Copy,
  Pencil,
} from "lucide-react";
import { toast } from "sonner";
import { formatDistanceToNow } from "date-fns";
import { useAPI } from "@/hooks/use-api";
import { useCRUDPage } from "@/hooks/use-crud-page";
import { useAuth } from "@/contexts/auth-context";
import { isSuperAdminLevel } from "@/lib/permissions";
import { useI18n } from "@/lib/i18n";

const GLOBAL_PROFILE_SCOPE = "__global__";

interface PermissionFormData {
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

const defaultForm: PermissionFormData = {
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

function confirmAction(message: string) {
  if (typeof window === "undefined" || typeof window.confirm !== "function")
    return true;
  return window.confirm(message) !== false;
}

function formatQuota(value: number): string {
  return value === 0 ? "∞" : String(value);
}

function profileScope(profile: PermissionProfile): "system" | "global" | "tenant" {
  if (profile.is_system) return "system";
  return profile.tenant_id ? "tenant" : "global";
}

function shortId(id: string): string {
  return `${id.slice(0, 8)}…`;
}

function tenantLabel(tenants: Tenant[], tenantId?: string | null): string {
  if (!tenantId) return "global";
  return tenants.find((tenant) => tenant.id === tenantId)?.name ?? shortId(tenantId);
}

function zoneLabel(zone: DomainZone): string {
  return zone.domain || shortId(zone.id);
}

function PermissionFormFields({
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

export default function PermissionsPage() {
  const { level } = useAuth();
  const { t } = useI18n();
  // UX-only gate; the backend authz seam is authoritative.
  const isPlatformAdmin = isSuperAdminLevel(level);
  const {
    data: profilesRes,
    isLoading: loading,
    mutate: mutateProfiles,
  } = useCRUDPage("permission-profiles", () => listPermissionProfiles(), "permissions.loadFailed");
  const profiles = profilesRes?.data ?? [];
  const total = profiles.length;
  const { data: domainsRes } = useAPI(
    isPlatformAdmin ? "permission-profile-admin-domains" : "permission-profile-domains",
    () => (isPlatformAdmin ? listAdminDomains() : listDomains()),
  );
  const { data: tenantsRes } = useAPI(
    isPlatformAdmin ? "permission-profile-tenants" : null,
    () => listTenants(),
  );
  const domainOptions = domainsRes?.data ?? [];
  const tenants = tenantsRes?.data ?? [];
  const zoneById = new Map(domainOptions.map((zone) => [zone.id, zone]));

  const [dialogOpen, setDialogOpen] = useState(false);
  const [creating, setCreating] = useState(false);
  const [form, setForm] = useState<PermissionFormData>(defaultForm);

  const [editOpen, setEditOpen] = useState(false);
  const [editingProfile, setEditingProfile] =
    useState<PermissionProfile | null>(null);
  const [editForm, setEditForm] = useState<PermissionFormData>(defaultForm);
  const [saving, setSaving] = useState(false);

  const handleCreate = async () => {
    if (!form.name.trim()) return;
    setCreating(true);
    try {
      await createPermissionProfile({
        ...(isPlatformAdmin ? { tenant_id: form.tenant_id } : {}),
        name: form.name.trim(),
        description: form.description.trim(),
        can_send: form.can_send,
        daily_send_quota: Number(form.daily_send_quota),
        daily_receive_quota: Number(form.daily_receive_quota),
        max_mailboxes: Number(form.max_mailboxes),
        max_domains: Number(form.max_domains),
        allowed_zone_ids: form.allowed_zone_ids,
        can_create_domains: form.can_create_domains,
        can_create_routes: form.can_create_routes,
        can_create_api_keys: form.can_create_api_keys,
      });
      setForm(defaultForm);
      setDialogOpen(false);
      toast.success(t("permissions.created"));
      mutateProfiles();
    } catch (e: unknown) {
      const err = e as { error?: { message?: string } };
      toast.error(err?.error?.message || t("permissions.createFailed"));
    } finally {
      setCreating(false);
    }
  };

  const openEdit = (profile: PermissionProfile) => {
    if (profile.is_system) {
      toast.error(t("permissions.systemCannotEdit"));
      return;
    }
    setEditingProfile(profile);
    setEditForm({
      tenant_id: profile.tenant_id ?? null,
      name: profile.name,
      description: profile.description,
      can_send: profile.can_send,
      daily_send_quota: String(profile.daily_send_quota),
      daily_receive_quota: String(profile.daily_receive_quota),
      max_mailboxes: String(profile.max_mailboxes),
      max_domains: String(profile.max_domains),
      allowed_zone_ids: profile.allowed_zone_ids ?? [],
      can_create_domains: profile.can_create_domains,
      can_create_routes: profile.can_create_routes,
      can_create_api_keys: profile.can_create_api_keys,
    });
    setEditOpen(true);
  };

  const handleEdit = async () => {
    if (!editingProfile || !editForm.name.trim()) return;
    setSaving(true);
    try {
      await updatePermissionProfile(editingProfile.id, {
        name: editForm.name.trim(),
        description: editForm.description.trim(),
        can_send: editForm.can_send,
        daily_send_quota: Number(editForm.daily_send_quota),
        daily_receive_quota: Number(editForm.daily_receive_quota),
        max_mailboxes: Number(editForm.max_mailboxes),
        max_domains: Number(editForm.max_domains),
        allowed_zone_ids: editForm.allowed_zone_ids,
        can_create_domains: editForm.can_create_domains,
        can_create_routes: editForm.can_create_routes,
        can_create_api_keys: editForm.can_create_api_keys,
      });
      setEditOpen(false);
      setEditingProfile(null);
      toast.success(t("permissions.updated"));
      mutateProfiles();
    } catch (e: unknown) {
      const err = e as { error?: { message?: string } };
      toast.error(err?.error?.message || t("permissions.updateFailed"));
    } finally {
      setSaving(false);
    }
  };

  const handleDelete = async (profile: PermissionProfile) => {
    if (profile.is_system) {
      toast.error(t("permissions.systemCannotDelete"));
      return;
    }
    if (!confirmAction(t("permissions.confirmDelete")))
      return;
    try {
      await deletePermissionProfile(profile.id);
      toast.success(t("permissions.deleted"));
      mutateProfiles();
    } catch (e: unknown) {
      const err = e as { error?: { message?: string } };
      toast.error(err?.error?.message || t("permissions.deleteFailed"));
    }
  };

  return (
    <div className="flex flex-col">
      <PageHeader
        title={t("permissions.title")}
        description={t("permissions.total", { count: total })}
        actions={
          <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
            <DialogTrigger render={<Button size="sm" className="gap-1.5" />}>
              <Plus className="h-3.5 w-3.5" />
              {t("permissions.create")}
            </DialogTrigger>
            <DialogContent className="sm:max-w-lg">
              <DialogHeader>
                <DialogTitle>{t("permissions.createTitle")}</DialogTitle>
                <DialogDescription>
                  {t("permissions.createDesc")}
                </DialogDescription>
              </DialogHeader>
              <PermissionFormFields
                form={form}
                setForm={setForm}
                domainOptions={domainOptions}
                isPlatformAdmin={isPlatformAdmin}
                tenants={tenants}
              />
              <DialogFooter>
                <Button
                  onClick={handleCreate}
                  disabled={creating || !form.name.trim()}
                >
                  {creating ? t("permissions.creating") : t("permissions.create")}
                </Button>
              </DialogFooter>
            </DialogContent>
          </Dialog>
        }
      />

      <div className="p-4">
        <Card>
          <CardHeader className="pb-3">
            <CardTitle className="text-base">{t("permissions.allProfiles")}</CardTitle>
            <CardDescription>
              {t("permissions.allProfilesDesc")}
            </CardDescription>
          </CardHeader>
          <CardContent>
            {loading ? (
              <div className="space-y-3">
                {Array.from({ length: 3 }).map((_, i) => (
                  <Skeleton key={i} className="h-12 w-full" />
                ))}
              </div>
            ) : profiles.length === 0 ? (
              <div className="text-center py-12 text-muted-foreground">
                <Shield className="h-10 w-10 mx-auto mb-3 opacity-30" />
                <p className="text-sm">{t("permissions.noProfiles")}</p>
              </div>
            ) : (
              <div className="overflow-x-auto">
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>{t("permissions.name")}</TableHead>
                      <TableHead>{t("permissions.scope")}</TableHead>
                      <TableHead>{t("permissions.descriptionField")}</TableHead>
                      <TableHead>{t("permissions.canSendShort")}</TableHead>
                      <TableHead className="text-right">{t("permissions.dailySendQuotaShort")}</TableHead>
                      <TableHead className="text-right">{t("permissions.dailyReceiveQuotaShort")}</TableHead>
                      <TableHead className="text-right">{t("permissions.mailboxesShort")}</TableHead>
                      <TableHead>{t("permissions.allowedZones")}</TableHead>
                      <TableHead>{t("permissions.createdAt")}</TableHead>
                      <TableHead className="w-10" />
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {profiles.map((profile) => {
                      const scope = profileScope(profile);
                      return (
                        <TableRow key={profile.id}>
                          <TableCell className="font-medium">
                            <div className="flex items-center gap-2">
                              {profile.name}
                              {profile.is_system && (
                                <Badge
                                  variant="secondary"
                                  className="text-[10px] px-1.5"
                                >
                                  {t("permissions.scopeSystem")}
                                </Badge>
                              )}
                            </div>
                          </TableCell>
                          <TableCell>
                            <div className="flex flex-col gap-1">
                              <Badge
                                variant={scope === "tenant" ? "default" : "outline"}
                                className="w-fit text-[10px]"
                              >
                                {scope}
                              </Badge>
                              {scope === "tenant" && (
                                <span
                                  className="max-w-[140px] truncate text-[10px] text-muted-foreground"
                                  title={profile.tenant_id ?? ""}
                                >
                                  {tenantLabel(tenants, profile.tenant_id)}
                                </span>
                              )}
                            </div>
                          </TableCell>
                        <TableCell
                          className="max-w-[200px] truncate text-muted-foreground text-sm"
                          title={profile.description}
                        >
                          {profile.description || "-"}
                        </TableCell>
                        <TableCell>
                          {profile.can_send ? (
                            <Badge className="bg-green-600 hover:bg-green-700 text-[10px]">
                              {t("permissions.canSendBadge")}
                            </Badge>
                          ) : (
                            <Badge
                              variant="outline"
                              className="text-[10px] text-muted-foreground"
                            >
                              {t("permissions.cannotSendBadge")}
                            </Badge>
                          )}
                        </TableCell>
                        <TableCell className="text-right tabular-nums">
                          {formatQuota(profile.daily_send_quota)}
                        </TableCell>
                        <TableCell className="text-right tabular-nums">
                          {formatQuota(profile.daily_receive_quota)}
                        </TableCell>
                        <TableCell className="text-right tabular-nums">
                          {formatQuota(profile.max_mailboxes)}
                        </TableCell>
                        <TableCell>
                          <Badge variant="outline" className="text-[10px]">
                            {profile.allowed_zone_ids && profile.allowed_zone_ids.length > 0
                              ? profile.allowed_zone_ids
                                  .map((id) => zoneById.get(id)?.domain ?? shortId(id))
                                  .slice(0, 2)
                                  .join(", ")
                              : t("permissions.allDomains")}
                          </Badge>
                        </TableCell>
                        <TableCell className="text-sm text-muted-foreground">
                          {formatDistanceToNow(new Date(profile.created_at), {
                            addSuffix: true,
                          })}
                        </TableCell>
                        <TableCell>
                          <DropdownMenu>
                            <DropdownMenuTrigger
                              render={
                                <Button
                                  variant="ghost"
                                  size="icon"
                                  className="h-8 w-8"
                                />
                              }
                            >
                              <MoreHorizontal className="h-4 w-4" />
                            </DropdownMenuTrigger>
                            <DropdownMenuContent align="end">
                              <DropdownMenuItem
                                disabled={profile.is_system}
                                onClick={() => openEdit(profile)}
                              >
                                <Pencil className="h-4 w-4 mr-2" />
                                {t("permissions.edit")}
                              </DropdownMenuItem>
                              <DropdownMenuItem
                                onClick={() => {
                                  navigator.clipboard.writeText(profile.id);
                                  toast.success(t("permissions.idCopied"));
                                }}
                              >
                                <Copy className="h-4 w-4 mr-2" />
                                {t("permissions.copyId")}
                              </DropdownMenuItem>
                              <DropdownMenuSeparator />
                              <DropdownMenuItem
                                disabled={profile.is_system}
                                onClick={() => handleDelete(profile)}
                                className="text-destructive focus:text-destructive"
                              >
                                <Trash2 className="h-4 w-4 mr-2" />
                                {t("permissions.delete")}
                              </DropdownMenuItem>
                            </DropdownMenuContent>
                          </DropdownMenu>
                        </TableCell>
                      </TableRow>
                      );
                    })}
                  </TableBody>
                </Table>
              </div>
            )}
          </CardContent>
        </Card>
      </div>

      {editingProfile && (
        <Dialog open={editOpen} onOpenChange={setEditOpen}>
          <DialogContent className="sm:max-w-lg">
            <DialogHeader>
              <DialogTitle>{t("permissions.editTitle")}</DialogTitle>
              <DialogDescription>
                {t("permissions.editDesc")}
              </DialogDescription>
            </DialogHeader>
            <PermissionFormFields
              form={editForm}
              setForm={setEditForm}
              domainOptions={domainOptions}
              isPlatformAdmin={isPlatformAdmin}
              tenants={tenants}
              tenantScopeLocked
            />
            <DialogFooter>
              <Button
                onClick={handleEdit}
                disabled={saving || !editForm.name.trim()}
              >
                {saving ? t("permissions.saving") : t("permissions.save")}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      )}
    </div>
  );
}
