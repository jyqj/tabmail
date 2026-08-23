"use client";

import { useState } from "react";
import {
  MoreHorizontal,
  Trash2,
  Users,
  Copy,
  Shield,
  UserCheck,
  SlidersHorizontal,
} from "lucide-react";
import { toast } from "sonner";
import { formatDistanceToNow } from "date-fns";

import { PageHeader } from "@/components/layout/page-header";
import { Button } from "@/components/ui/button";
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
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Switch } from "@/components/ui/switch";
import { Skeleton } from "@/components/ui/skeleton";
import { listUsers, updateUser, deleteUser, listPermissionProfiles, listDomains } from "@/lib/api";
import type { AdminUser } from "@/lib/types";
import { useI18n } from "@/lib/i18n";
import { useCRUDPage } from "@/hooks/use-crud-page";
import { useAuth } from "@/contexts/auth-context";
import { canManageTenantUsers } from "@/lib/permissions";
import { safeConfirm } from "@/lib/utils";
import { InviteAdminDialog } from "./invite-admin-dialog";
import { UserPermissionDialog } from "./user-permission-dialog";

export default function UsersPage() {
  const { t } = useI18n();
  const { level } = useAuth();
  // UX-only gate; the backend authz seam is authoritative.
  const isPlatformAdmin = canManageTenantUsers(level);

  const { data: usersRes, isLoading: loading, mutate } = useCRUDPage(
    "admin-users",
    () => listUsers(),
    "admin.usersLoadFailed",
  );
  const { data: profilesRes } = useCRUDPage(
    "permission-profiles",
    () => listPermissionProfiles(),
    "admin.permProfilesLoadFailed",
  );
  const { data: domainsRes } = useCRUDPage(
    "admin-user-domains",
    () => listDomains(),
    "domains.loadFailed",
  );

  const users = usersRes?.data ?? [];
  const total = usersRes?.meta?.total ?? users.length;
  const profiles = profilesRes?.data ?? [];
  const domains = domainsRes?.data ?? [];

  const [permUser, setPermUser] = useState<AdminUser | null>(null);

  const profileName = (profileId?: string) => {
    if (!profileId) return null;
    return profiles.find((p) => p.id === profileId)?.name ?? null;
  };

  const handleToggleActive = async (user: AdminUser) => {
    try {
      await updateUser(user.id, { is_active: !user.is_active });
      toast.success(
        user.is_active ? t("admin.userDeactivated") : t("admin.userActivated")
      );
      mutate();
    } catch {
      toast.error(t("admin.updateFailed"));
    }
  };

  const handleDelete = async (id: string) => {
    if (!safeConfirm(t("admin.confirmDeleteUser"))) return;
    try {
      await deleteUser(id);
      toast.success(t("admin.userDeleted"));
      mutate();
    } catch (e: unknown) {
      const err = e as { error?: { message?: string } };
      toast.error(err?.error?.message || t("admin.deleteFailed"));
    }
  };

  return (
    <div className="flex flex-col">
      <PageHeader
        title={t("admin.usersTitle")}
        description={t("admin.usersCount", { count: total })}
        actions={isPlatformAdmin ? <InviteAdminDialog /> : null}
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
                            <DropdownMenuItem onClick={() => setPermUser(user)}>
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

      <UserPermissionDialog
        user={permUser}
        profiles={profiles}
        domains={domains}
        onClose={() => setPermUser(null)}
        onUserChanged={() => mutate()}
      />
    </div>
  );
}
