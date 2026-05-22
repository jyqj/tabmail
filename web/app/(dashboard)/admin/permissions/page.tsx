"use client";

import { useState, useEffect, type Dispatch, type SetStateAction } from "react";
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
import { Skeleton } from "@/components/ui/skeleton";
import {
  listPermissionProfiles,
  createPermissionProfile,
  updatePermissionProfile,
  deletePermissionProfile,
  listDomains,
} from "@/lib/api";
import type { DomainZone, PermissionProfile } from "@/lib/types";
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

interface PermissionFormData {
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

function PermissionFormFields({
  form,
  setForm,
  domainOptions,
}: {
  form: PermissionFormData;
  setForm: Dispatch<SetStateAction<PermissionFormData>>;
  domainOptions: DomainZone[];
}) {
  const switchFields: { key: keyof PermissionFormData; label: string }[] = [
    { key: "can_send", label: "可发件" },
    { key: "can_create_domains", label: "可创建域名" },
    { key: "can_create_routes", label: "可创建路由" },
    { key: "can_create_api_keys", label: "可创建 API Key" },
  ];

  const numberFields: { key: keyof PermissionFormData; label: string }[] = [
    { key: "daily_send_quota", label: "每日发件配额" },
    { key: "daily_receive_quota", label: "每日收件配额" },
    { key: "max_mailboxes", label: "最大邮箱数" },
    { key: "max_domains", label: "最大域名数" },
  ];

  return (
    <div className="space-y-4 py-4 max-h-[60vh] overflow-y-auto">
      <div className="space-y-1.5">
        <Label className="text-xs">{"名称"}</Label>
        <Input
          value={form.name}
          onChange={(e) =>
            setForm((prev) => ({ ...prev, name: e.target.value }))
          }
          placeholder={"模板名称"}
        />
      </div>
      <div className="space-y-1.5">
        <Label className="text-xs">{"描述"}</Label>
        <Input
          value={form.description}
          onChange={(e) =>
            setForm((prev) => ({ ...prev, description: e.target.value }))
          }
          placeholder={"模板描述（可选）"}
        />
      </div>

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
              {"0 = 无限制"}
            </p>
          </div>
        ))}
      </div>

      <div className="space-y-2">
        <div className="flex items-center justify-between">
          <Label className="text-xs">{"允许域名范围"}</Label>
          <span className="text-[10px] text-muted-foreground">
            {form.allowed_zone_ids.length === 0 ? "全部域名" : `${form.allowed_zone_ids.length} 个域名`}
          </span>
        </div>
        <div className="rounded-md border p-3 space-y-2">
          <Button
            type="button"
            variant="ghost"
            size="sm"
            className="h-7 px-2 text-xs"
            onClick={() => setForm((prev) => ({ ...prev, allowed_zone_ids: [] }))}
          >
            {"允许全部域名"}
          </Button>
          {domainOptions.length === 0 ? (
            <p className="text-xs text-muted-foreground">{"暂无可选域名；留空表示全部域名。"}</p>
          ) : (
            <div className="grid gap-2">
              {domainOptions.map((zone) => {
                const checked = form.allowed_zone_ids.includes(zone.id);
                return (
                  <label key={zone.id} className="flex items-center justify-between gap-3 rounded border px-2 py-1.5 text-xs">
                    <span className="truncate" title={zone.domain}>{zone.domain}</span>
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
  const {
    data: profilesRes,
    isLoading: loading,
    error: profilesError,
    mutate: mutateProfiles,
  } = useAPI("permission-profiles", () => listPermissionProfiles());
  const profiles = profilesRes?.data ?? [];
  const total = profiles.length;
  const { data: domainsRes } = useAPI("permission-profile-domains", () => listDomains());
  const domainOptions = domainsRes?.data ?? [];

  useEffect(() => {
    if (profilesError) toast.error("加载权限模板失败");
  }, [profilesError]);

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
      toast.success("权限模板已创建");
      mutateProfiles();
    } catch (e: unknown) {
      const err = e as { error?: { message?: string } };
      toast.error(err?.error?.message || "创建失败");
    } finally {
      setCreating(false);
    }
  };

  const openEdit = (profile: PermissionProfile) => {
    if (profile.is_system) {
      toast.error("系统模板不可编辑");
      return;
    }
    setEditingProfile(profile);
    setEditForm({
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
      toast.success("权限模板已更新");
      mutateProfiles();
    } catch (e: unknown) {
      const err = e as { error?: { message?: string } };
      toast.error(err?.error?.message || "更新失败");
    } finally {
      setSaving(false);
    }
  };

  const handleDelete = async (profile: PermissionProfile) => {
    if (profile.is_system) {
      toast.error("系统模板不可删除");
      return;
    }
    if (!confirmAction("确定要删除该权限模板吗？此操作不可撤销。"))
      return;
    try {
      await deletePermissionProfile(profile.id);
      toast.success("权限模板已删除");
      mutateProfiles();
    } catch (e: unknown) {
      const err = e as { error?: { message?: string } };
      toast.error(err?.error?.message || "删除失败");
    }
  };

  return (
    <div className="flex flex-col">
      <PageHeader
        title={"权限模板"}
        description={`共 ${total} 个模板`}
        actions={
          <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
            <DialogTrigger render={<Button size="sm" className="gap-1.5" />}>
              <Plus className="h-3.5 w-3.5" />
              {"创建模板"}
            </DialogTrigger>
            <DialogContent className="sm:max-w-lg">
              <DialogHeader>
                <DialogTitle>{"创建权限模板"}</DialogTitle>
                <DialogDescription>
                  {"配置权限模板的各项参数，创建后可分配给用户。"}
                </DialogDescription>
              </DialogHeader>
              <PermissionFormFields
                form={form}
                setForm={setForm}
                domainOptions={domainOptions}
              />
              <DialogFooter>
                <Button
                  onClick={handleCreate}
                  disabled={creating || !form.name.trim()}
                >
                  {creating ? "创建中..." : "创建"}
                </Button>
              </DialogFooter>
            </DialogContent>
          </Dialog>
        }
      />

      <div className="p-4">
        <Card>
          <CardHeader className="pb-3">
            <CardTitle className="text-base">{"所有模板"}</CardTitle>
            <CardDescription>
              {"管理权限模板，控制用户的发件、收件、域名和邮箱权限。"}
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
                <p className="text-sm">{"暂无权限模板"}</p>
              </div>
            ) : (
              <div className="overflow-x-auto">
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>{"名称"}</TableHead>
                      <TableHead>{"描述"}</TableHead>
                      <TableHead>{"发件"}</TableHead>
                      <TableHead className="text-right">{"日发件额"}</TableHead>
                      <TableHead className="text-right">{"日收件额"}</TableHead>
                      <TableHead className="text-right">{"邮箱数"}</TableHead>
                      <TableHead>{"域名范围"}</TableHead>
                      <TableHead>{"创建时间"}</TableHead>
                      <TableHead className="w-10" />
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {profiles.map((profile) => (
                      <TableRow key={profile.id}>
                        <TableCell className="font-medium">
                          <div className="flex items-center gap-2">
                            {profile.name}
                            {profile.is_system && (
                              <Badge
                                variant="secondary"
                                className="text-[10px] px-1.5"
                              >
                                {"system"}
                              </Badge>
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
                              {"可发件"}
                            </Badge>
                          ) : (
                            <Badge
                              variant="outline"
                              className="text-[10px] text-muted-foreground"
                            >
                              {"不可发件"}
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
                              ? `${profile.allowed_zone_ids.length} 个域名`
                              : "全部域名"}
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
                                {"编辑"}
                              </DropdownMenuItem>
                              <DropdownMenuItem
                                onClick={() => {
                                  navigator.clipboard.writeText(profile.id);
                                  toast.success("ID 已复制");
                                }}
                              >
                                <Copy className="h-4 w-4 mr-2" />
                                {"复制 ID"}
                              </DropdownMenuItem>
                              <DropdownMenuSeparator />
                              <DropdownMenuItem
                                disabled={profile.is_system}
                                onClick={() => handleDelete(profile)}
                                className="text-destructive focus:text-destructive"
                              >
                                <Trash2 className="h-4 w-4 mr-2" />
                                {"删除"}
                              </DropdownMenuItem>
                            </DropdownMenuContent>
                          </DropdownMenu>
                        </TableCell>
                      </TableRow>
                    ))}
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
              <DialogTitle>{"编辑权限模板"}</DialogTitle>
              <DialogDescription>
                {"修改权限模板的配置参数。"}
              </DialogDescription>
            </DialogHeader>
            <PermissionFormFields
              form={editForm}
              setForm={setEditForm}
              domainOptions={domainOptions}
            />
            <DialogFooter>
              <Button
                onClick={handleEdit}
                disabled={saving || !editForm.name.trim()}
              >
                {saving ? "保存中..." : "保存"}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      )}
    </div>
  );
}
