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
import { Switch } from "@/components/ui/switch";
import { Skeleton } from "@/components/ui/skeleton";
import {
  listUsers,
  inviteAdmin,
  updateUser,
  deleteUser,
} from "@/lib/api";
import type { AdminUser } from "@/lib/types";
import {
  Plus,
  MoreHorizontal,
  Trash2,
  Users,
  Copy,
  Shield,
  UserCheck,
} from "lucide-react";
import { toast } from "sonner";
import { formatDistanceToNow } from "date-fns";
import { useI18n } from "@/lib/i18n";

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
  const [users, setUsers] = useState<AdminUser[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(true);

  const [inviteOpen, setInviteOpen] = useState(false);
  const [inviting, setInviting] = useState(false);
  const [inviteEmail, setInviteEmail] = useState("");
  const [inviteResult, setInviteResult] = useState<{ invite_code: string; email: string } | null>(null);

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

  useEffect(() => {
    fetchUsers();
  }, [fetchUsers]);

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

  return (
    <div className="flex flex-col">
      <PageHeader
        title={t("admin.usersTitle")}
        description={t("admin.usersCount", { count: total })}
        actions={
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
                        {user.role === "admin" ? (
                          <Badge className="gap-1 bg-amber-600 hover:bg-amber-700">
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
    </div>
  );
}
