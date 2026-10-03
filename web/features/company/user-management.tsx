"use client";
import { useEffect, useRef, useState } from "react";
import { PageHeader } from "@/components/layout/page-header";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardDescription, CardHeader, CardTitle, } from "@/components/ui/card";
import { TableCell, TableRow } from "@/components/ui/table";
import { DataTable, DataTablePagination } from "@/components/crud/data-table";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle, DialogTrigger, } from "@/components/ui/dialog";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator, DropdownMenuTrigger, } from "@/components/ui/dropdown-menu";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue, } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { Skeleton } from "@/components/ui/skeleton";
import { listUsers, inviteAdmin, updateUser, deleteUser, listPermissionProfiles, listDomains, } from "@/lib/api";
import type { AdminUser, EffectivePermission, } from "@/lib/types";
import { Plus, MoreHorizontal, Trash2, Users, Copy, Shield, UserCheck, SlidersHorizontal, Gauge, } from "lucide-react";
import { toast } from "sonner";
import { formatDistanceToNow } from "date-fns";
import { useI18n } from "@/lib/i18n";
import { useAuth } from "@/contexts/auth-context";
import { canManageTenantUsers } from "@/lib/permissions";
import { safeConfirm } from "@/lib/utils";
import { useCRUDPage } from "@/hooks/use-crud-page";
import { getUserPermissionEditor, patchUserPermissionEditor, assignUserPermissionEditor } from "@/lib/api/permissions";
import { buildPermissionEditorCommand, permissionEditorFormFromSnapshot, validateObservedPermissionProfile } from "@/lib/api/permission-editor-types";
import type { PermissionEditorForm, PermissionEditorSnapshot, PermissionField } from "@/lib/api/permission-editor-types";
import { useCompanyEventConsumer } from "./company-event-consumer";
import { useSessionScope } from "@/lib/session";
const USERS_PER_PAGE = 20;
const NONE_PROFILE = "__none__";
const emptyOverrideForm: PermissionEditorForm = {
    can_send: null,
    daily_send_quota: "",
    daily_receive_quota: "",
    max_mailboxes: "",
    max_domains: "",
    domain_access: { mode: "inherit", zone_ids: [] },
    can_create_domains: null,
    can_create_routes: null,
    can_create_api_keys: null,
};
export default function UsersPage() {
    const { t } = useI18n();
    const { level, tenantId: activeTenantId } = useAuth();
    // UX-only gate; the backend authz seam is authoritative.
    const isPlatformAdmin = canManageTenantUsers(level);
    const [page, setPage] = useState(1);
    const { data: usersRes, isLoading: loading, mutate: mutateUsers, } = useCRUDPage(["admin-users", page], () => listUsers({ page, per_page: USERS_PER_PAGE }), "admin.usersLoadFailed");
    const { data: profilesRes, mutate: mutateProfiles } = useCRUDPage("admin-users-permission-profiles", () => listPermissionProfiles(), "admin.permProfilesLoadFailed");
    const { data: domainsRes, mutate: mutateDomains } = useCRUDPage("admin-users-domains", () => listDomains(), "domains.loadFailed");
    const users = usersRes?.data ?? [];
    const total = usersRes?.meta?.total ?? users.length;
    const profiles = profilesRes?.data ?? [];
    const domains = domainsRes?.data ?? [];
    const [inviteOpen, setInviteOpen] = useState(false);
    const [inviting, setInviting] = useState(false);
    const [inviteEmail, setInviteEmail] = useState("");
    const [inviteResult, setInviteResult] = useState<{
        invite_code: string;
        email: string;
    } | null>(null);
    // Permission management dialog
    const [permUser, setPermUser] = useState<AdminUser | null>(null);
    const [permEffective, setPermEffective] = useState<EffectivePermission | null>(null);
    const [permForm, setPermForm] = useState<PermissionEditorForm>(emptyOverrideForm);
    const [permSnapshot, setPermSnapshot] = useState<PermissionEditorSnapshot | null>(null);
    const [permLoading, setPermLoading] = useState(false);
    const [permNeedsReload, setPermNeedsReload] = useState(false);
    const [permConflict, setPermConflict] = useState(false);
    const permEpoch = useRef(0);
    const permRequest = useRef<AbortController | null>(null);
    const [permProfileId, setPermProfileId] = useState(NONE_PROFILE);
    const [permSelectedProfileRevision, setPermSelectedProfileRevision] = useState<string | null>(null);
    useEffect(() => () => { permEpoch.current++; permRequest.current?.abort(); }, []);
    const [permSaving, setPermSaving] = useState(false);
    const [permResetting, setPermResetting] = useState(false);
    const session = useSessionScope();
    useEffect(() => {
        ++permEpoch.current; permRequest.current?.abort();
        setPermUser(null); setPermSnapshot(null); setPermEffective(null); setPermForm(emptyOverrideForm);
        setPermLoading(false); setPermSaving(false); setPermResetting(false); setPermNeedsReload(false); setPermConflict(false);
        setInviteOpen(false); setInviteResult(null); setInviteEmail("");
    }, [session]);
    const profileName = (profileId?: string) => {
        if (!profileId)
            return null;
        return profiles.find((p) => p.id === profileId)?.name ?? null;
    };
    const domainLabel = (id: string) => domains.find((domain) => domain.id === id)?.domain ?? id.slice(0, 8);
    const handleInvite = async () => {
        if (!inviteEmail.trim())
            return;
        setInviting(true);
        try {
            const res = await inviteAdmin(inviteEmail.trim());
            setInviteResult({ invite_code: res.data.invite_code, email: res.data.email });
            setInviteEmail("");
            toast.success(t("admin.inviteSent"));
        }
        catch (e: unknown) {
            const err = e as {
                error?: {
                    message?: string;
                };
            };
            toast.error(err?.error?.message || t("admin.inviteFailed"));
        }
        finally {
            setInviting(false);
        }
    };
    const handleToggleActive = async (user: AdminUser) => {
        try {
            await updateUser(user.id, { is_active: !user.is_active });
            toast.success(user.is_active ? t("admin.userDeactivated") : t("admin.userActivated"));
            mutateUsers();
        }
        catch {
            toast.error(t("admin.updateFailed"));
        }
    };
    const handleDelete = async (id: string) => {
        if (!safeConfirm(t("admin.confirmDeleteUser")))
            return;
        try {
            await deleteUser(id);
            toast.success(t("admin.userDeleted"));
            mutateUsers();
        }
        catch (e: unknown) {
            const err = e as {
                error?: {
                    message?: string;
                };
            };
            toast.error(err?.error?.message || t("admin.deleteFailed"));
        }
    };
    // Permission management
    const assertPermTenant = (snapshot: PermissionEditorSnapshot, user: AdminUser) => {
        if (!activeTenantId || user.tenant_id !== activeTenantId || snapshot.tenant_id !== activeTenantId ||
            snapshot.revision.tenant_id !== activeTenantId || snapshot.user_id !== user.id) {
            throw new Error("Permission editor belongs to a different active tenant");
        }
    };
    const applyPermSnapshot = (snapshot: PermissionEditorSnapshot) => {
        setPermSnapshot(snapshot);
        setPermProfileId(snapshot.revision.profile_id ?? NONE_PROFILE);
        setPermSelectedProfileRevision(snapshot.revision.profile_revision);
        setPermEffective(snapshot.effective);
        setPermForm(permissionEditorFormFromSnapshot(snapshot));
        setPermConflict(false);
        setPermNeedsReload(false);
    };
    const loadPermSnapshot = async (user: AdminUser) => {
        const epoch = ++permEpoch.current;
        permRequest.current?.abort();
        const controller = new AbortController();
        permRequest.current = controller;
        setPermLoading(true);
        setPermSnapshot(null);
        setPermEffective(null);
        setPermForm(emptyOverrideForm);
        setPermNeedsReload(false);
        setPermConflict(false);
        try {
            if (!activeTenantId || user.tenant_id !== activeTenantId) throw new Error("Permission target belongs to a different active tenant");
            // Explicit reload discards the old assignment draft AND obtains a
            // new destination-profile observation; do not rebase old intent.
            const [response] = await Promise.all([
                getUserPermissionEditor(user.id, { signal: controller.signal }), mutateProfiles(),
            ]);
            assertPermTenant(response.data, user);
            if (epoch === permEpoch.current && !controller.signal.aborted) applyPermSnapshot(response.data);
        } catch (error: unknown) {
            if (epoch !== permEpoch.current || controller.signal.aborted) return;
            const failure = error as { error?: { message?: string } };
            toast.error(failure?.error?.message || t("admin.permLoadFailed"));
        } finally {
            if (epoch === permEpoch.current) setPermLoading(false);
        }
    };
    const openPermDialog = (user: AdminUser) => {
        setPermUser(user);
        setPermSaving(false);
        setPermResetting(false);
        void loadPermSnapshot(user);
    };
    const closePermDialog = () => {
        ++permEpoch.current;
        permRequest.current?.abort();
        setPermUser(null);
        setPermSnapshot(null);
        setPermEffective(null);
        setPermSaving(false);
        setPermResetting(false);
    };
    const eventRevoked = useCompanyEventConsumer({
        tenantId: activeTenantId, enabled: level === "admin" || level === "super_admin",
        cacheKeys: ["admin-users", "admin-users-permission-profiles", "admin-users-domains"],
        onInvalidate: () => {
            if (!permUser) return;
            ++permEpoch.current; permRequest.current?.abort();
            setPermLoading(false); setPermSaving(false); setPermResetting(false);
            setPermNeedsReload(true); setPermConflict(true); setPermEffective(null);
            // Keep permForm/assignment intent and observed revision. Only an
            // explicit reload may discard/review a dirty permission draft.
        },
        revalidate: () => Promise.all([mutateUsers(), mutateProfiles(), mutateDomains()]),
        onRevoked: () => {
            closePermDialog(); setPermForm(emptyOverrideForm);
            setInviteOpen(false); setInviteResult(null); setInviteEmail("");
        },
    });
    const editorProfiles = permSnapshot?.profile
        ? [permSnapshot.profile, ...profiles.filter(profile => profile.id !== permSnapshot.profile?.id)] : profiles;
    const knownProfile = (id: string) => {
        const profile = editorProfiles.find(item => item.id === id);
        if (!profile || (profile.tenant_id != null && profile.tenant_id !== permSnapshot?.tenant_id)) return null;
        try { validateObservedPermissionProfile(profile); return profile; } catch { return null; }
    };
    const editorTenantBound = !eventRevoked && !!activeTenantId && permSnapshot?.tenant_id === activeTenantId && permUser?.tenant_id === activeTenantId;
    const editorBusy = permLoading || permNeedsReload || permSaving || permResetting;
    const canAssign = editorTenantBound && !!permSnapshot?.capabilities.assign_profile && !editorBusy;
    const assignmentChanged = !!permSnapshot && (permProfileId === NONE_PROFILE ? null : permProfileId) !== permSnapshot.revision.profile_id;
    const observedDestination = permProfileId === NONE_PROFILE || (permSelectedProfileRevision !== null && knownProfile(permProfileId)?.revision === permSelectedProfileRevision);
    const handlePermProfileChange = (value: string | null) => {
        if (!canAssign) return;
        const id = value ?? NONE_PROFILE;
        if (id === NONE_PROFILE) { setPermProfileId(id); setPermSelectedProfileRevision(null); return; }
        const destination = knownProfile(id);
        if (!destination) return;
        setPermProfileId(destination.id); setPermSelectedProfileRevision(destination.revision);
    };
    const canPatch = editorTenantBound && !!permSnapshot?.capabilities.patch && !permLoading && !permNeedsReload && !permSaving && !permResetting;
    let pendingCommand: ReturnType<typeof buildPermissionEditorCommand> | null = null;
    if (permSnapshot) {
        try { pendingCommand = buildPermissionEditorCommand(permSnapshot, permForm); } catch { /* Invalid intent never enables a write. */ }
    }
    const hasChangedFields = !!pendingCommand && Object.keys(pendingCommand.patch).length > 0;
    const canSave = !!pendingCommand && !editorBusy && (assignmentChanged
        ? canAssign && observedDestination && (!hasChangedFields || !!permSnapshot?.capabilities.patch)
        : canPatch && hasChangedFields);
    const failPermWrite = (error: unknown) => {
        const failure = error as { error?: { code?: string; message?: string } };
        setPermConflict(failure?.error?.code === "CONFLICT" || failure?.error?.code === "REVISION_CONFLICT");
        // Preserve all input and the observed revision. An uncertain or rejected
        // write requires an explicit reload; never auto-replay stale intent.
        setPermNeedsReload(true);
        toast.error(failure?.error?.message || t("admin.permSaveFailed"));
    };
    const handleSaveOverrides = async () => {
        if (!permUser || !permSnapshot || !canSave || !pendingCommand) return;
        const epoch = permEpoch.current;
        const controller = new AbortController();
        permRequest.current = controller;
        setPermSaving(true);
        try {
            const response = assignmentChanged
                ? await assignUserPermissionEditor(permUser.id, {
                    ...pendingCommand, profile_id: permProfileId === NONE_PROFILE ? null : permProfileId,
                    profile_revision: permSelectedProfileRevision,
                }, { signal: controller.signal })
                : await patchUserPermissionEditor(permUser.id, pendingCommand, { signal: controller.signal });
            if (epoch !== permEpoch.current || controller.signal.aborted) return;
            assertPermTenant(response.data, permUser);
            applyPermSnapshot(response.data);
            toast.success(t("admin.permSaved"));
        } catch (error: unknown) {
            if (epoch === permEpoch.current && !controller.signal.aborted) failPermWrite(error);
        } finally {
            if (epoch === permEpoch.current) setPermSaving(false);
        }
    };
    const handleResetOverrides = async () => {
        if (!permUser || !permSnapshot || !canPatch || assignmentChanged) return;
        const epoch = permEpoch.current;
        const controller = new AbortController();
        permRequest.current = controller;
        setPermResetting(true);
        try {
            const response = await patchUserPermissionEditor(permUser.id, {
                expected_revision: { ...permSnapshot.revision },
                patch: { can_send: null, daily_send_quota: null, daily_receive_quota: null,
                    max_mailboxes: null, max_domains: null, can_create_domains: null,
                    can_create_routes: null, can_create_api_keys: null, domain_access: { mode: "inherit", zone_ids: [] } },
            }, { signal: controller.signal });
            if (epoch !== permEpoch.current || controller.signal.aborted) return;
            assertPermTenant(response.data, permUser);
            applyPermSnapshot(response.data);
            toast.success(t("admin.permResetSuccess"));
        } catch (error: unknown) {
            if (epoch === permEpoch.current && !controller.signal.aborted) failPermWrite(error);
        } finally {
            if (epoch === permEpoch.current) setPermResetting(false);
        }
    };
    const sourceLabel = (field: PermissionField) => {
        const source = permSnapshot?.field_sources[field];
        return source === "override" ? t("admin.permSourceOverride") : source === "profile" ? t("admin.permSourceProfile") : t("admin.permSourceDefault");
    };
    const effectiveEntries: {
        key: PermissionField;
        label: string;
        value: string;
    }[] = permEffective
        ? [
            { key: "can_send", label: t("admin.permCanSend"), value: permEffective.can_send ? "true" : "false" },
            { key: "daily_send_quota", label: t("admin.permDailySendQuota"), value: String(permEffective.daily_send_quota) },
            { key: "daily_receive_quota", label: t("admin.permDailyReceiveQuota"), value: String(permEffective.daily_receive_quota) },
            { key: "max_mailboxes", label: t("admin.permMaxMailboxes"), value: String(permEffective.max_mailboxes) },
            { key: "max_domains", label: t("admin.permMaxDomains"), value: String(permEffective.max_domains) },
            {
                key: "domain_access",
                label: t("admin.permAllowedZoneScope"),
                value: permSnapshot?.effective.domain_access_mode === "none" || permSnapshot?.overrides?.domain_access.mode === "none"
                    ? t("admin.permNoDomainsAllowed")
                    : permEffective.allowed_zone_ids?.length
                    ? permEffective.allowed_zone_ids.map(domainLabel).join(", ")
                    : t("admin.permAllDomains"),
            },
            { key: "can_create_domains", label: t("admin.permCanCreateDomains"), value: permEffective.can_create_domains ? "true" : "false" },
            { key: "can_create_routes", label: t("admin.permCanCreateRoutes"), value: permEffective.can_create_routes ? "true" : "false" },
            { key: "can_create_api_keys", label: t("admin.permCanCreateApiKeys"), value: permEffective.can_create_api_keys ? "true" : "false" },
        ]
        : [];
    return (<div className="flex flex-col">
      <PageHeader title={t("admin.usersTitle")} description={t("admin.usersCount", { count: total })} actions={isPlatformAdmin ? (<Dialog open={inviteOpen} onOpenChange={(open) => {
                setInviteOpen(open);
                if (!open) {
                    setInviteResult(null);
                    setInviteEmail("");
                }
            }}>
            <DialogTrigger render={<Button size="sm" className="gap-1.5"/>}>
              <Plus className="h-3.5 w-3.5"/>
              {t("admin.inviteAdmin")}
            </DialogTrigger>
            <DialogContent className="sm:max-w-md">
              <DialogHeader>
                <DialogTitle>{t("admin.inviteTitle")}</DialogTitle>
                <DialogDescription>
                  {t("admin.inviteDesc")}
                </DialogDescription>
              </DialogHeader>

              {inviteResult ? (<div className="space-y-4 py-4">
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
                      <Button variant="outline" size="icon" className="h-8 w-8 shrink-0" onClick={() => {
                    navigator.clipboard.writeText(inviteResult.invite_code);
                    toast.success(t("admin.copied"));
                }}>
                        <Copy className="h-3.5 w-3.5"/>
                      </Button>
                    </div>
                  </div>
                  <DialogFooter>
                    <Button variant="outline" onClick={() => setInviteOpen(false)}>
                      {t("admin.close")}
                    </Button>
                  </DialogFooter>
                </div>) : (<div className="space-y-4 py-4">
                  <div className="space-y-2">
                    <Label>{t("admin.email")}</Label>
                    <Input type="email" placeholder={t("admin.emailPlaceholder")} value={inviteEmail} onChange={(e) => setInviteEmail(e.target.value)} onKeyDown={(e) => e.key === "Enter" && handleInvite()}/>
                  </div>
                  <DialogFooter>
                    <Button onClick={handleInvite} disabled={inviting || !inviteEmail.trim()}>
                      {inviting ? t("admin.inviting") : t("admin.sendInvite")}
                    </Button>
                  </DialogFooter>
                </div>)}
            </DialogContent>
          </Dialog>) : null}/>

      <div className="p-4 space-y-4">
        <Card>
          <CardHeader className="pb-3">
            <CardTitle className="text-base">{t("admin.allUsers")}</CardTitle>
            <CardDescription>
              {t("admin.allUsersDesc")}
            </CardDescription>
          </CardHeader>
          <CardContent>
            <DataTable loading={loading} isEmpty={users.length === 0} emptyIcon={Users} emptyText={t("admin.noUsers")} columns={[
            { key: "email", header: t("admin.email") },
            { key: "displayName", header: t("admin.displayName") },
            { key: "role", header: t("admin.role") },
            { key: "profile", header: t("admin.permProfile") },
            { key: "status", header: t("admin.status") },
            { key: "lastLogin", header: t("admin.lastLogin") },
            { key: "actions", className: "w-10" },
        ]}>
              {users.map((user) => (<TableRow key={user.id}>
                      <TableCell className="font-medium">{user.email}</TableCell>
                      <TableCell>{user.display_name}</TableCell>
                      <TableCell>
                        {user.role === "super_admin" ? (<Badge className="gap-1 bg-amber-600 hover:bg-amber-700">
                            <Shield className="h-3 w-3"/>
                            {t("admin.roleSuperAdmin")}
                          </Badge>) : user.role === "admin" ? (<Badge className="gap-1 bg-blue-600 hover:bg-blue-700">
                            <Shield className="h-3 w-3"/>
                            {t("admin.roleAdmin")}
                          </Badge>) : (<Badge variant="outline">
                            <UserCheck className="h-3 w-3 mr-1"/>
                            {t("admin.roleUser")}
                          </Badge>)}
                      </TableCell>
                      <TableCell>
                        {profileName(user.permission_profile_id) ? (<Badge variant="secondary">
                            {profileName(user.permission_profile_id)}
                          </Badge>) : (<Badge variant="outline">{t("admin.permDefault")}</Badge>)}
                      </TableCell>
                      <TableCell>
                        <div className="flex items-center gap-2">
                          <Switch size="sm" checked={user.is_active} onCheckedChange={() => handleToggleActive(user)}/>
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
                          <DropdownMenuTrigger render={<Button variant="ghost" size="icon" className="h-8 w-8"/>}>
                            <MoreHorizontal className="h-4 w-4"/>
                          </DropdownMenuTrigger>
                          <DropdownMenuContent align="end">
                            <DropdownMenuItem onClick={() => openPermDialog(user)}>
                              <SlidersHorizontal className="h-4 w-4 mr-2"/>
                              {t("admin.permManage")}
                            </DropdownMenuItem>
                            <DropdownMenuItem onClick={() => {
                navigator.clipboard.writeText(user.id);
                toast.success(t("admin.idCopied"));
            }}>
                              <Copy className="h-4 w-4 mr-2"/>
                              {t("admin.copyId")}
                            </DropdownMenuItem>
                            <DropdownMenuSeparator />
                            <DropdownMenuItem onClick={() => handleDelete(user.id)} className="text-destructive focus:text-destructive">
                              <Trash2 className="h-4 w-4 mr-2"/>
                              {t("admin.deleteUser")}
                            </DropdownMenuItem>
                          </DropdownMenuContent>
                        </DropdownMenu>
                      </TableCell>
                    </TableRow>))}
            </DataTable>
            {total > USERS_PER_PAGE && (<DataTablePagination page={page} perPage={USERS_PER_PAGE} total={total} onPageChange={setPage} label={t("admin.pageOf", { page, total })} previousText={t("admin.previous")} nextText={t("admin.next")}/>)}
          </CardContent>
        </Card>
      </div>

      {/* Permission Management Dialog */}
      <Dialog open={permUser !== null} onOpenChange={(open) => { if (!open)
        closePermDialog(); }}>
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
                  <Gauge className="h-4 w-4 text-primary"/>
                  {t("admin.permEffective")}
                </CardTitle>
                <CardDescription>{t("admin.permEffectiveDesc")}</CardDescription>
              </CardHeader>
              <CardContent className="space-y-3">
                {permEffective ? (effectiveEntries.map((entry) => (<div key={entry.key} className="flex items-center justify-between gap-3 text-sm">
                      <span className="text-muted-foreground">{entry.label}<Badge variant="outline" className="ml-2 text-[10px]">{sourceLabel(entry.key)}</Badge></span>
                      <span className="font-medium tabular-nums">{entry.value}</span>
                    </div>))) : (<div className="space-y-3">
                    {Array.from({ length: 8 }).map((_, i) => (<Skeleton key={i} className="h-6 w-full"/>))}
                  </div>)}
              </CardContent>
            </Card>

            {/* Right: Override form */}
            <div className="space-y-4">
              {/* Profile selector */}
              <div className="space-y-2">
                <Label>{t("admin.permProfileLabel")}</Label>
                <Select value={permProfileId} onValueChange={handlePermProfileChange} disabled={!canAssign}>
                  <SelectTrigger aria-label={t("admin.permProfileLabel")}>
                    <SelectValue placeholder={t("admin.permSelectProfile")} >
                      {permProfileId === NONE_PROFILE ? t("admin.permDefault") : editorProfiles.find(profile => profile.id === permProfileId)?.name ?? t("admin.permSelectProfile")}
                    </SelectValue>
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value={NONE_PROFILE}>{t("admin.permDefault")}</SelectItem>
                    {editorProfiles.map((profile) => (<SelectItem key={profile.id} value={profile.id} disabled={!knownProfile(profile.id)}>
                        {profile.name}
                      </SelectItem>))}
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
                    <Switch disabled={!canPatch} size="sm" checked={permForm.can_send === null ? false : permForm.can_send} onCheckedChange={(checked) => setPermForm((prev) => ({ ...prev, can_send: checked }))}/>
                    {permForm.can_send !== null && (<Button disabled={!canPatch} variant="ghost" size="icon" className="h-6 w-6" onClick={() => setPermForm((prev) => ({ ...prev, can_send: null }))}>
                        <Trash2 className="h-3 w-3"/>
                      </Button>)}
                  </div>
                </div>

                <div className="flex items-center justify-between">
                  <Label>{t("admin.permCanCreateDomains")}</Label>
                  <div className="flex items-center gap-2">
                    <span className="text-xs text-muted-foreground">{t("admin.permInherit")}</span>
                    <Switch disabled={!canPatch} size="sm" checked={permForm.can_create_domains === null ? false : permForm.can_create_domains} onCheckedChange={(checked) => setPermForm((prev) => ({ ...prev, can_create_domains: checked }))}/>
                    {permForm.can_create_domains !== null && (<Button disabled={!canPatch} variant="ghost" size="icon" className="h-6 w-6" onClick={() => setPermForm((prev) => ({ ...prev, can_create_domains: null }))}>
                        <Trash2 className="h-3 w-3"/>
                      </Button>)}
                  </div>
                </div>

                <div className="flex items-center justify-between">
                  <Label>{t("admin.permCanCreateRoutes")}</Label>
                  <div className="flex items-center gap-2">
                    <span className="text-xs text-muted-foreground">{t("admin.permInherit")}</span>
                    <Switch disabled={!canPatch} size="sm" checked={permForm.can_create_routes === null ? false : permForm.can_create_routes} onCheckedChange={(checked) => setPermForm((prev) => ({ ...prev, can_create_routes: checked }))}/>
                    {permForm.can_create_routes !== null && (<Button disabled={!canPatch} variant="ghost" size="icon" className="h-6 w-6" onClick={() => setPermForm((prev) => ({ ...prev, can_create_routes: null }))}>
                        <Trash2 className="h-3 w-3"/>
                      </Button>)}
                  </div>
                </div>

                <div className="flex items-center justify-between">
                  <Label>{t("admin.permCanCreateApiKeys")}</Label>
                  <div className="flex items-center gap-2">
                    <span className="text-xs text-muted-foreground">{t("admin.permInherit")}</span>
                    <Switch disabled={!canPatch} size="sm" checked={permForm.can_create_api_keys === null ? false : permForm.can_create_api_keys} onCheckedChange={(checked) => setPermForm((prev) => ({ ...prev, can_create_api_keys: checked }))}/>
                    {permForm.can_create_api_keys !== null && (<Button disabled={!canPatch} variant="ghost" size="icon" className="h-6 w-6" onClick={() => setPermForm((prev) => ({ ...prev, can_create_api_keys: null }))}>
                        <Trash2 className="h-3 w-3"/>
                      </Button>)}
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
                      <Button type="button" disabled={!canPatch} variant={permForm.domain_access.mode === "inherit" ? "default" : "outline"} size="sm" onClick={() => setPermForm((prev) => ({ ...prev, domain_access: { mode: "inherit", zone_ids: [] } }))}>
                        {t("admin.permInheritShort")}
                      </Button>
                      <Button type="button" disabled={!canPatch} variant={permForm.domain_access.mode === "all" ? "default" : "outline"} size="sm" onClick={() => setPermForm((prev) => ({ ...prev, domain_access: { mode: "all", zone_ids: [] } }))}>
                        {t("admin.permAll")}
                      </Button>
                      <Button type="button" disabled={!canPatch} variant={permForm.domain_access.mode === "none" ? "default" : "outline"} size="sm" onClick={() => setPermForm((prev) => ({ ...prev, domain_access: { mode: "none", zone_ids: [] } }))}>
                        {t("admin.permNoDomainsAllowed")}
                      </Button>
                    </div>
                  </div>

                  {domains.length === 0 ? (<p className="text-xs text-muted-foreground">{t("admin.permNoDomains")}</p>) : (<div className="grid gap-2">
                      {domains.map((domain) => {
                const selected = permForm.domain_access.mode === "list" && permForm.domain_access.zone_ids.includes(domain.id);
                return (<label key={domain.id} className="flex items-center justify-between rounded border px-3 py-2 text-sm">
                            <span className="truncate">{domain.domain}</span>
                            <Switch disabled={!canPatch} size="sm" checked={selected} onCheckedChange={(checked) => setPermForm((prev) => {
                        const current = prev.domain_access.mode === "list" ? prev.domain_access.zone_ids : [];
                        const zone_ids = checked ? Array.from(new Set([...current, domain.id])) : current.filter((id) => id !== domain.id);
                        return {
                            ...prev,
                            domain_access: { mode: zone_ids.length ? "list" : "none", zone_ids },
                        };
                    })}/>
                          </label>);
            })}
                    </div>)}
                </div>

                {/* Number inputs */}
                <div className="space-y-2">
                  <Label>{t("admin.permDailySendQuota")}</Label>
                  <Input disabled={!canPatch} min={0} step={1} type="number" placeholder={t("admin.permInherit")} value={permForm.daily_send_quota} onChange={(e) => setPermForm((prev) => ({ ...prev, daily_send_quota: e.target.value }))}/>
                  <p className="text-xs text-muted-foreground">{t("admin.permZeroUnlimited")}</p>
                </div>

                <div className="space-y-2">
                  <Label>{t("admin.permDailyReceiveQuota")}</Label>
                  <Input disabled={!canPatch} min={0} step={1} type="number" placeholder={t("admin.permInherit")} value={permForm.daily_receive_quota} onChange={(e) => setPermForm((prev) => ({ ...prev, daily_receive_quota: e.target.value }))}/>
                  <p className="text-xs text-muted-foreground">{t("admin.permZeroUnlimited")}</p>
                </div>

                <div className="space-y-2">
                  <Label>{t("admin.permMaxMailboxes")}</Label>
                  <Input disabled={!canPatch} min={0} step={1} type="number" placeholder={t("admin.permInherit")} value={permForm.max_mailboxes} onChange={(e) => setPermForm((prev) => ({ ...prev, max_mailboxes: e.target.value }))}/>
                  <p className="text-xs text-muted-foreground">{t("admin.permZeroUnlimited")}</p>
                </div>

                <div className="space-y-2">
                  <Label>{t("admin.permMaxDomains")}</Label>
                  <Input disabled={!canPatch} min={0} step={1} type="number" placeholder={t("admin.permInherit")} value={permForm.max_domains} onChange={(e) => setPermForm((prev) => ({ ...prev, max_domains: e.target.value }))}/>
                  <p className="text-xs text-muted-foreground">{t("admin.permZeroUnlimited")}</p>
                </div>
              </div>
            </div>
          </div>

          {permConflict && <p role="alert" className="text-sm text-destructive">{t("admin.permConflict")}</p>}
          {permSnapshot && !permSnapshot.capabilities.patch && <p role="status" className="text-sm text-muted-foreground">{t("admin.permReadOnly")}</p>}
          <DialogFooter className="gap-2">
            <Button variant="outline" aria-label={t("admin.permReload")} onClick={() => { if (permUser) void loadPermSnapshot(permUser); }} disabled={permLoading || permSaving || permResetting || !permUser}>
              {t("admin.permReload")}
            </Button>
            <Button variant="outline" onClick={handleResetOverrides} disabled={!canPatch || assignmentChanged || !permUser}>
              {permResetting ? t("admin.permResetting") : t("admin.permReset")}
            </Button>
            <Button onClick={handleSaveOverrides} disabled={!canSave || !permUser}>
              {permSaving ? t("admin.permSaving") : t("admin.permSave")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>);
}
