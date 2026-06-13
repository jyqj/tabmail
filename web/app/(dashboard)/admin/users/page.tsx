"use client";

import { useState, useEffect, useCallback } from "react";
import { PageHeader } from "@/components/layout/page-header";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Badge } from "@/components/ui/badge";
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
import { Switch } from "@/components/ui/switch";
import { Skeleton } from "@/components/ui/skeleton";
import {
  listUsers,
  inviteAdmin,
  updateUser,
  deleteUser,
  listPermissionProfiles,
  listDomains,
  getUserPermission,
  setUserPermissionOverride,
  deleteUserPermissionOverride,
} from "@/lib/api";
import type {
  AdminUser,
  DomainZone,
  PermissionProfile,
  EffectivePermission,
  UserPermissionOverride,
} from "@/lib/types";
import {
  Plus,
  MoreHorizontal,
  Trash2,
  Users,
  Copy,
  Shield,
  UserCheck,
  SlidersHorizontal,
  Gauge,
} from "lucide-react";
import { toast } from "sonner";
import { formatDistanceToNow } from "date-fns";
import { useI18n } from "@/lib/i18n";
import { useAuth } from "@/contexts/auth-context";
import { canManageTenantUsers } from "@/lib/permissions";

const NONE_PROFILE = "__none__";

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

function confirmAction(message: string) {
  if (typeof window === "undefined" || typeof window.confirm !== "function") return true;
  try {
    return window.confirm(message) !== false;
  } catch {
    return true;
  }
}

export default function UsersPage() {
  const { t } = useI18n();
  const { level } = useAuth();
  // UX-only gate; the backend authz seam is authoritative.
  const isPlatformAdmin = canManageTenantUsers(level);
  const [users, setUsers] = useState<AdminUser[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(true);

  const [inviteOpen, setInviteOpen] = useState(false);
  const [inviting, setInviting] = useState(false);
  const [inviteEmail, setInviteEmail] = useState("");
  const [inviteResult, setInviteResult] = useState<{ invite_code: string; email: string } | null>(null);

  // Permission profiles list
  const [profiles, setProfiles] = useState<PermissionProfile[]>([]);
  const [domains, setDomains] = useState<DomainZone[]>([]);

  // Permission management dialog
  const [permUser, setPermUser] = useState<AdminUser | null>(null);
  const [permEffective, setPermEffective] = useState<EffectivePermission | null>(null);
  const [permForm, setPermForm] = useState<PermOverrideForm>(emptyOverrideForm);
  const [permProfileId, setPermProfileId] = useState<string>(NONE_PROFILE);
  const [permSaving, setPermSaving] = useState(false);
  const [permResetting, setPermResetting] = useState(false);

  const fetchUsers = useCallback(async () => {
    try {
      const res = await listUsers();
      setUsers(res.data ?? []);
      setTotal(res.meta?.total ?? res.data?.length ?? 0);
    } catch {
      toast.error(t("admin.usersLoadFailed"));
    } finally {
      setLoading(false);
    }
  }, [t]);

  const fetchProfiles = useCallback(async () => {
    try {
      const res = await listPermissionProfiles();
      setProfiles(res.data ?? []);
    } catch {
      toast.error(t("admin.permProfilesLoadFailed"));
    }
  }, [t]);

  const fetchDomains = useCallback(async () => {
    try {
      const res = await listDomains();
      setDomains(res.data ?? []);
    } catch {
      toast.error(t("domains.loadFailed"));
    }
  }, [t]);

  useEffect(() => {
    fetchUsers();
    fetchProfiles();
    fetchDomains();
  }, [fetchUsers, fetchProfiles, fetchDomains]);

  const profileName = (profileId?: string) => {
    if (!profileId) return null;
    return profiles.find((p) => p.id === profileId)?.name ?? null;
  };

  const domainLabel = (id: string) =>
    domains.find((domain) => domain.id === id)?.domain ?? id.slice(0, 8);

  const handleInvite = async () => {
    if (!inviteEmail.trim()) return;
    setInviting(true);
    try {
      const res = await inviteAdmin(inviteEmail.trim());
      setInviteResult({ invite_code: res.data.invite_code, email: res.data.email });
      setInviteEmail("");
      toast.success(t("admin.inviteSent"));
    } catch (e: unknown) {
      const err = e as { error?: { message?: string } };
      toast.error(err?.error?.message || t("admin.inviteFailed"));
    } finally {
      setInviting(false);
    }
  };

  const handleToggleActive = async (user: AdminUser) => {
    try {
      await updateUser(user.id, { is_active: !user.is_active });
      toast.success(
        user.is_active ? t("admin.userDeactivated") : t("admin.userActivated")
      );
      fetchUsers();
    } catch {
      toast.error(t("admin.updateFailed"));
    }
  };

  const handleDelete = async (id: string) => {
    if (!confirmAction(t("admin.confirmDeleteUser"))) return;
    try {
      await deleteUser(id);
      toast.success(t("admin.userDeleted"));
      fetchUsers();
    } catch (e: unknown) {
      const err = e as { error?: { message?: string } };
      toast.error(err?.error?.message || t("admin.deleteFailed"));
    }
  };

  // Permission management
  const openPermDialog = async (user: AdminUser) => {
    setPermUser(user);
    setPermEffective(null);
    setPermForm(emptyOverrideForm);
    setPermProfileId(user.permission_profile_id || NONE_PROFILE);
    try {
      const res = await getUserPermission(user.id);
      setPermEffective(res.data);
    } catch {
      toast.error(t("admin.permLoadFailed"));
    }
  };

  const handlePermProfileChange = async (value: string | null) => {
    if (!permUser) return;
    const effectiveValue = value ?? NONE_PROFILE;
    const newProfileId = effectiveValue === NONE_PROFILE ? null : effectiveValue;
    setPermProfileId(effectiveValue);
    try {
      await updateUser(permUser.id, { permission_profile_id: newProfileId });
      toast.success(t("admin.permProfileUpdated"));
      fetchUsers();
      // Refresh effective permissions
      const res = await getUserPermission(permUser.id);
      setPermEffective(res.data);
      // Update the local permUser to reflect the change
      setPermUser((prev) => prev ? { ...prev, permission_profile_id: newProfileId ?? undefined } : null);
    } catch (e: unknown) {
      const err = e as { error?: { message?: string } };
      toast.error(err?.error?.message || t("admin.permProfileUpdateFailed"));
      // Revert selection
      setPermProfileId(permUser.permission_profile_id || NONE_PROFILE);
    }
  };

  const handleSaveOverrides = async () => {
    if (!permUser) return;
    setPermSaving(true);
    try {
      const body: Partial<UserPermissionOverride> = {};
      if (permForm.can_send !== null) body.can_send = permForm.can_send;
      if (permForm.daily_send_quota.trim() !== "") body.daily_send_quota = Number(permForm.daily_send_quota);
      if (permForm.daily_receive_quota.trim() !== "") body.daily_receive_quota = Number(permForm.daily_receive_quota);
      if (permForm.max_mailboxes.trim() !== "") body.max_mailboxes = Number(permForm.max_mailboxes);
      if (permForm.max_domains.trim() !== "") body.max_domains = Number(permForm.max_domains);
      if (permForm.allowed_zone_ids !== null) body.allowed_zone_ids = permForm.allowed_zone_ids;
      if (permForm.can_create_domains !== null) body.can_create_domains = permForm.can_create_domains;
      if (permForm.can_create_routes !== null) body.can_create_routes = permForm.can_create_routes;
      if (permForm.can_create_api_keys !== null) body.can_create_api_keys = permForm.can_create_api_keys;

      await setUserPermissionOverride(permUser.id, body);
      toast.success(t("admin.permSaved"));
      // Refresh effective permissions
      const res = await getUserPermission(permUser.id);
      setPermEffective(res.data);
    } catch (e: unknown) {
      const err = e as { error?: { message?: string } };
      toast.error(err?.error?.message || t("admin.permSaveFailed"));
    } finally {
      setPermSaving(false);
    }
  };

  const handleResetOverrides = async () => {
    if (!permUser) return;
    setPermResetting(true);
    try {
      await deleteUserPermissionOverride(permUser.id);
      toast.success(t("admin.permResetSuccess"));
      setPermForm(emptyOverrideForm);
      // Refresh effective permissions
      const res = await getUserPermission(permUser.id);
      setPermEffective(res.data);
    } catch (e: unknown) {
      const err = e as { error?: { message?: string } };
      toast.error(err?.error?.message || t("admin.permResetFailed"));
    } finally {
      setPermResetting(false);
    }
  };

  const effectiveEntries: { key: string; label: string; value: string }[] = permEffective
    ? [
        { key: "can_send", label: t("admin.permCanSend"), value: permEffective.can_send ? "true" : "false" },
        { key: "daily_send_quota", label: t("admin.permDailySendQuota"), value: String(permEffective.daily_send_quota) },
        { key: "daily_receive_quota", label: t("admin.permDailyReceiveQuota"), value: String(permEffective.daily_receive_quota) },
        { key: "max_mailboxes", label: t("admin.permMaxMailboxes"), value: String(permEffective.max_mailboxes) },
        { key: "max_domains", label: t("admin.permMaxDomains"), value: String(permEffective.max_domains) },
        {
          key: "allowed_zone_ids",
          label: t("admin.permAllowedZoneScope"),
          value: permEffective.allowed_zone_ids?.length
            ? permEffective.allowed_zone_ids.map(domainLabel).join(", ")
            : t("admin.permAllDomains"),
        },
        { key: "can_create_domains", label: t("admin.permCanCreateDomains"), value: permEffective.can_create_domains ? "true" : "false" },
        { key: "can_create_routes", label: t("admin.permCanCreateRoutes"), value: permEffective.can_create_routes ? "true" : "false" },
        { key: "can_create_api_keys", label: t("admin.permCanCreateApiKeys"), value: permEffective.can_create_api_keys ? "true" : "false" },
      ]
    : [];

  return (
    <div className="flex flex-col">
      <PageHeader
        title={t("admin.usersTitle")}
        description={t("admin.usersCount", { count: total })}
        actions={
          isPlatformAdmin ? (
          <Dialog
            open={inviteOpen}
            onOpenChange={(open) => {
              setInviteOpen(open);
              if (!open) {
                setInviteResult(null);
                setInviteEmail("");
              }
            }}
          >
            <DialogTrigger render={<Button size="sm" className="gap-1.5" />}>
              <Plus className="h-3.5 w-3.5" />
              {t("admin.inviteAdmin")}
            </DialogTrigger>
            <DialogContent className="sm:max-w-md">
              <DialogHeader>
                <DialogTitle>{t("admin.inviteTitle")}</DialogTitle>
                <DialogDescription>
                  {t("admin.inviteDesc")}
                </DialogDescription>
              </DialogHeader>

              {inviteResult ? (
                <div className="space-y-4 py-4">
                  <div className="rounded-lg border border-green-200 bg-green-50 dark:border-green-800 dark:bg-green-950 p-3">
                    <p className="text-sm font-medium text-green-800 dark:text-green-200 mb-1">
                      {t("admin.inviteCreated")}
                    </p>
                    <p className="text-xs text-green-700 dark:text-green-300 mb-2">
                      {inviteResult.email}
                    </p>
                    <div className="flex items-center gap-2">
                      <code className="flex-1 text-xs break-all bg-white dark:bg-black/20 p-2 rounded">
                        {inviteResult.invite_code}
                      </code>
                      <Button
                        variant="outline"
                        size="icon"
                        className="h-8 w-8 shrink-0"
                        onClick={() => {
                          navigator.clipboard.writeText(inviteResult.invite_code);
                          toast.success(t("admin.copied"));
                        }}
                      >
                        <Copy className="h-3.5 w-3.5" />
                      </Button>
                    </div>
                  </div>
                  <DialogFooter>
                    <Button variant="outline" onClick={() => setInviteOpen(false)}>
                      {t("admin.close")}
                    </Button>
                  </DialogFooter>
                </div>
              ) : (
                <div className="space-y-4 py-4">
                  <div className="space-y-2">
                    <Label>{t("admin.email")}</Label>
                    <Input
                      type="email"
                      placeholder={t("admin.emailPlaceholder")}
                      value={inviteEmail}
                      onChange={(e) => setInviteEmail(e.target.value)}
                      onKeyDown={(e) => e.key === "Enter" && handleInvite()}
                    />
                  </div>
                  <DialogFooter>
                    <Button
                      onClick={handleInvite}
                      disabled={inviting || !inviteEmail.trim()}
                    >
                      {inviting ? t("admin.inviting") : t("admin.sendInvite")}
                    </Button>
                  </DialogFooter>
                </div>
              )}
            </DialogContent>
          </Dialog>
          ) : null
        }
      />

      <div className="p-4 space-y-4">
        <Card>
          <CardHeader className="pb-3">
            <CardTitle className="text-base">{t("admin.allUsers")}</CardTitle>
            <CardDescription>
              {t("admin.allUsersDesc")}
            </CardDescription>
          </CardHeader>
          <CardContent>
            {loading ? (
              <div className="space-y-3">
                {Array.from({ length: 3 }).map((_, i) => (
                  <Skeleton key={i} className="h-12 w-full" />
                ))}
              </div>
            ) : users.length === 0 ? (
              <div className="text-center py-12 text-muted-foreground">
                <Users className="h-10 w-10 mx-auto mb-3 opacity-30" />
                <p className="text-sm">{t("admin.noUsers")}</p>
              </div>
            ) : (
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>{t("admin.email")}</TableHead>
                    <TableHead>{t("admin.displayName")}</TableHead>
                    <TableHead>{t("admin.role")}</TableHead>
                    <TableHead>{t("admin.permProfile")}</TableHead>
                    <TableHead>{t("admin.status")}</TableHead>
                    <TableHead>{t("admin.lastLogin")}</TableHead>
                    <TableHead className="w-10" />
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {users.map((user) => (
                    <TableRow key={user.id}>
                      <TableCell className="font-medium">{user.email}</TableCell>
                      <TableCell>{user.display_name}</TableCell>
                      <TableCell>
                        {user.role === "super_admin" ? (
                          <Badge className="gap-1 bg-amber-600 hover:bg-amber-700">
                            <Shield className="h-3 w-3" />
                            {t("admin.roleSuperAdmin")}
                          </Badge>
                        ) : user.role === "admin" ? (
                          <Badge className="gap-1 bg-blue-600 hover:bg-blue-700">
                            <Shield className="h-3 w-3" />
                            {t("admin.roleAdmin")}
                          </Badge>
                        ) : (
                          <Badge variant="outline">
                            <UserCheck className="h-3 w-3 mr-1" />
                            {t("admin.roleUser")}
                          </Badge>
                        )}
                      </TableCell>
                      <TableCell>
                        {profileName(user.permission_profile_id) ? (
                          <Badge variant="secondary">
                            {profileName(user.permission_profile_id)}
                          </Badge>
                        ) : (
                          <Badge variant="outline">{t("admin.permDefault")}</Badge>
                        )}
                      </TableCell>
                      <TableCell>
                        <div className="flex items-center gap-2">
                          <Switch
                            size="sm"
                            checked={user.is_active}
                            onCheckedChange={() => handleToggleActive(user)}
                          />
                          <span className="text-xs text-muted-foreground">
                            {user.is_active ? t("admin.active") : t("admin.inactive")}
                          </span>
                        </div>
                      </TableCell>
                      <TableCell className="text-sm text-muted-foreground">
                        {user.last_login_at
                          ? formatDistanceToNow(new Date(user.last_login_at), {
                              addSuffix: true,
                            })
                          : t("admin.never")}
                      </TableCell>
                      <TableCell>
                        <DropdownMenu>
                          <DropdownMenuTrigger render={<Button variant="ghost" size="icon" className="h-8 w-8" />}>
                            <MoreHorizontal className="h-4 w-4" />
                          </DropdownMenuTrigger>
                          <DropdownMenuContent align="end">
                            <DropdownMenuItem onClick={() => openPermDialog(user)}>
                              <SlidersHorizontal className="h-4 w-4 mr-2" />
                              {t("admin.permManage")}
                            </DropdownMenuItem>
                            <DropdownMenuItem
                              onClick={() => {
                                navigator.clipboard.writeText(user.id);
                                toast.success(t("admin.idCopied"));
                              }}
                            >
                              <Copy className="h-4 w-4 mr-2" />
                              {t("admin.copyId")}
                            </DropdownMenuItem>
                            <DropdownMenuSeparator />
                            <DropdownMenuItem
                              onClick={() => handleDelete(user.id)}
                              className="text-destructive focus:text-destructive"
                            >
                              <Trash2 className="h-4 w-4 mr-2" />
                              {t("admin.deleteUser")}
                            </DropdownMenuItem>
                          </DropdownMenuContent>
                        </DropdownMenu>
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            )}
          </CardContent>
        </Card>
      </div>

      {/* Permission Management Dialog */}
      <Dialog open={permUser !== null} onOpenChange={(open) => { if (!open) setPermUser(null); }}>
        <DialogContent className="sm:max-w-3xl">
          <DialogHeader>
            <DialogTitle>{t("admin.permTitle")}</DialogTitle>
            <DialogDescription>
              {t("admin.permDesc", { name: permUser?.email ?? "" })}
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
                {permEffective ? (
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
              {/* Profile selector */}
              <div className="space-y-2">
                <Label>{t("admin.permProfileLabel")}</Label>
                <Select value={permProfileId} onValueChange={handlePermProfileChange}>
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

                {/* Boolean switches */}
                <div className="flex items-center justify-between">
                  <Label>{t("admin.permCanSend")}</Label>
                  <div className="flex items-center gap-2">
                    <span className="text-xs text-muted-foreground">{t("admin.permInherit")}</span>
                    <Switch
                      size="sm"
                      checked={permForm.can_send === null ? false : permForm.can_send}
                      onCheckedChange={(checked) => setPermForm((prev) => ({ ...prev, can_send: checked }))}
                    />
                    {permForm.can_send !== null && (
                      <Button
                        variant="ghost"
                        size="icon"
                        className="h-6 w-6"
                        onClick={() => setPermForm((prev) => ({ ...prev, can_send: null }))}
                      >
                        <Trash2 className="h-3 w-3" />
                      </Button>
                    )}
                  </div>
                </div>

                <div className="flex items-center justify-between">
                  <Label>{t("admin.permCanCreateDomains")}</Label>
                  <div className="flex items-center gap-2">
                    <span className="text-xs text-muted-foreground">{t("admin.permInherit")}</span>
                    <Switch
                      size="sm"
                      checked={permForm.can_create_domains === null ? false : permForm.can_create_domains}
                      onCheckedChange={(checked) => setPermForm((prev) => ({ ...prev, can_create_domains: checked }))}
                    />
                    {permForm.can_create_domains !== null && (
                      <Button
                        variant="ghost"
                        size="icon"
                        className="h-6 w-6"
                        onClick={() => setPermForm((prev) => ({ ...prev, can_create_domains: null }))}
                      >
                        <Trash2 className="h-3 w-3" />
                      </Button>
                    )}
                  </div>
                </div>

                <div className="flex items-center justify-between">
                  <Label>{t("admin.permCanCreateRoutes")}</Label>
                  <div className="flex items-center gap-2">
                    <span className="text-xs text-muted-foreground">{t("admin.permInherit")}</span>
                    <Switch
                      size="sm"
                      checked={permForm.can_create_routes === null ? false : permForm.can_create_routes}
                      onCheckedChange={(checked) => setPermForm((prev) => ({ ...prev, can_create_routes: checked }))}
                    />
                    {permForm.can_create_routes !== null && (
                      <Button
                        variant="ghost"
                        size="icon"
                        className="h-6 w-6"
                        onClick={() => setPermForm((prev) => ({ ...prev, can_create_routes: null }))}
                      >
                        <Trash2 className="h-3 w-3" />
                      </Button>
                    )}
                  </div>
                </div>

                <div className="flex items-center justify-between">
                  <Label>{t("admin.permCanCreateApiKeys")}</Label>
                  <div className="flex items-center gap-2">
                    <span className="text-xs text-muted-foreground">{t("admin.permInherit")}</span>
                    <Switch
                      size="sm"
                      checked={permForm.can_create_api_keys === null ? false : permForm.can_create_api_keys}
                      onCheckedChange={(checked) => setPermForm((prev) => ({ ...prev, can_create_api_keys: checked }))}
                    />
                    {permForm.can_create_api_keys !== null && (
                      <Button
                        variant="ghost"
                        size="icon"
                        className="h-6 w-6"
                        onClick={() => setPermForm((prev) => ({ ...prev, can_create_api_keys: null }))}
                      >
                        <Trash2 className="h-3 w-3" />
                      </Button>
                    )}
                  </div>
                </div>

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
                        variant={permForm.allowed_zone_ids === null ? "default" : "outline"}
                        size="sm"
                        onClick={() => setPermForm((prev) => ({ ...prev, allowed_zone_ids: null }))}
                      >
                        {t("admin.permInheritShort")}
                      </Button>
                      <Button
                        type="button"
                        variant={permForm.allowed_zone_ids !== null && permForm.allowed_zone_ids.length === 0 ? "default" : "outline"}
                        size="sm"
                        onClick={() => setPermForm((prev) => ({ ...prev, allowed_zone_ids: [] }))}
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
                        const selected = permForm.allowed_zone_ids?.includes(domain.id) ?? false;
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
                                setPermForm((prev) => {
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

                {/* Number inputs */}
                <div className="space-y-2">
                  <Label>{t("admin.permDailySendQuota")}</Label>
                  <Input
                    type="number"
                    placeholder={t("admin.permInherit")}
                    value={permForm.daily_send_quota}
                    onChange={(e) => setPermForm((prev) => ({ ...prev, daily_send_quota: e.target.value }))}
                  />
                  <p className="text-xs text-muted-foreground">{t("admin.permZeroUnlimited")}</p>
                </div>

                <div className="space-y-2">
                  <Label>{t("admin.permDailyReceiveQuota")}</Label>
                  <Input
                    type="number"
                    placeholder={t("admin.permInherit")}
                    value={permForm.daily_receive_quota}
                    onChange={(e) => setPermForm((prev) => ({ ...prev, daily_receive_quota: e.target.value }))}
                  />
                  <p className="text-xs text-muted-foreground">{t("admin.permZeroUnlimited")}</p>
                </div>

                <div className="space-y-2">
                  <Label>{t("admin.permMaxMailboxes")}</Label>
                  <Input
                    type="number"
                    placeholder={t("admin.permInherit")}
                    value={permForm.max_mailboxes}
                    onChange={(e) => setPermForm((prev) => ({ ...prev, max_mailboxes: e.target.value }))}
                  />
                  <p className="text-xs text-muted-foreground">{t("admin.permZeroUnlimited")}</p>
                </div>

                <div className="space-y-2">
                  <Label>{t("admin.permMaxDomains")}</Label>
                  <Input
                    type="number"
                    placeholder={t("admin.permInherit")}
                    value={permForm.max_domains}
                    onChange={(e) => setPermForm((prev) => ({ ...prev, max_domains: e.target.value }))}
                  />
                  <p className="text-xs text-muted-foreground">{t("admin.permZeroUnlimited")}</p>
                </div>
              </div>
            </div>
          </div>

          <DialogFooter className="gap-2">
            <Button
              variant="outline"
              onClick={handleResetOverrides}
              disabled={permResetting || !permUser}
            >
              {permResetting ? t("admin.permResetting") : t("admin.permReset")}
            </Button>
            <Button
              onClick={handleSaveOverrides}
              disabled={permSaving || !permUser}
            >
              {permSaving ? t("admin.permSaving") : t("admin.permSave")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
