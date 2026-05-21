"use client";

import { useState, useEffect, useCallback } from "react";
import { useAPI } from "@/hooks/use-api";
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
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import {
  listSendIdentities,
  createSendIdentity,
  deleteSendIdentity,
  listSendAsGrants,
  createSendAsGrant,
  deleteSendAsGrant,
  listDomains,
} from "@/lib/api";
import type { SendIdentity, SendAsGrant, DomainZone } from "@/lib/types";
import {
  Plus,
  Trash2,
  Send,
  ChevronDown,
  ChevronUp,
  CheckCircle2,
  XCircle,
  Shield,
  UserPlus,
} from "lucide-react";
import { toast } from "sonner";
import { formatDistanceToNow } from "date-fns";
import { useI18n } from "@/lib/i18n";
import { useAuth } from "@/contexts/auth-context";

function safeConfirm(message: string) {
  if (typeof window === "undefined" || typeof window.confirm !== "function") return true;
  try {
    return window.confirm(message) !== false;
  } catch {
    return true;
  }
}

function IdentityGrantsPanel({ identity }: { identity: SendIdentity }) {
  const { t } = useI18n();
  const [grants, setGrants] = useState<SendAsGrant[]>([]);
  const [loading, setLoading] = useState(true);
  const [grantDialogOpen, setGrantDialogOpen] = useState(false);
  const [creating, setCreating] = useState(false);
  const [principalType, setPrincipalType] = useState("user");
  const [principalId, setPrincipalId] = useState("");
  const [dailyQuota, setDailyQuota] = useState("100");

  const fetchGrants = useCallback(async () => {
    setLoading(true);
    try {
      const res = await listSendAsGrants(identity.id);
      setGrants(res.data ?? []);
    } catch {
      toast.error("加载发件授权失败");
    } finally {
      setLoading(false);
    }
  }, [identity.id]);

  useEffect(() => {
    fetchGrants();
  }, [fetchGrants]);

  const handleCreateGrant = async () => {
    if (!principalId.trim()) return;
    const quota = Number(dailyQuota);
    if (Number.isNaN(quota) || quota < 0) {
      toast.error("每日配额必须为非负整数");
      return;
    }
    setCreating(true);
    try {
      await createSendAsGrant(identity.id, {
        principal_type: principalType,
        principal_id: principalId.trim(),
        daily_quota: quota,
      });
      setPrincipalId("");
      setDailyQuota("100");
      setGrantDialogOpen(false);
      toast.success("发件授权已创建");
      fetchGrants();
    } catch (err: unknown) {
      const apiErr = err as { error?: { message?: string } };
      toast.error(apiErr?.error?.message || "创建发件授权失败");
    } finally {
      setCreating(false);
    }
  };

  const handleDeleteGrant = async (grantId: string) => {
    if (!safeConfirm("确定要删除此发件授权吗？")) return;
    try {
      await deleteSendAsGrant(identity.id, grantId);
      toast.success("发件授权已删除");
      fetchGrants();
    } catch {
      toast.error("删除发件授权失败");
    }
  };

  return (
    <div className="border-t border-border/40 px-5 py-4 space-y-4 bg-muted/5">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <Shield className="h-4 w-4 text-muted-foreground" />
          <span className="text-sm font-medium">发件授权 (Send-As Grants)</span>
          <Badge variant="secondary" className="text-[10px]">{grants.length}</Badge>
        </div>
        <Dialog open={grantDialogOpen} onOpenChange={setGrantDialogOpen}>
          <DialogTrigger render={<Button variant="outline" size="sm" className="gap-1.5 text-xs" />}>
            <UserPlus className="h-3 w-3" />
            添加授权
          </DialogTrigger>
          <DialogContent className="sm:max-w-md">
            <DialogHeader>
              <DialogTitle>添加发件授权</DialogTitle>
              <DialogDescription>
                允许指定用户或 API Key 以此身份发送邮件
              </DialogDescription>
            </DialogHeader>
            <div className="space-y-4 py-4">
              <div className="space-y-2">
                <Label>主体类型</Label>
                <Select value={principalType} onValueChange={(v) => v && setPrincipalType(v)}>
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="user">用户 (user)</SelectItem>
                    <SelectItem value="api_key">API Key</SelectItem>
                  </SelectContent>
                </Select>
              </div>
              <div className="space-y-2">
                <Label>主体 ID</Label>
                <Input
                  placeholder={principalType === "user" ? "用户 ID" : "API Key ID"}
                  value={principalId}
                  onChange={(e) => setPrincipalId(e.target.value)}
                />
              </div>
              <div className="space-y-2">
                <Label>每日配额</Label>
                <Input
                  type="number"
                  min="0"
                  placeholder="100"
                  value={dailyQuota}
                  onChange={(e) => setDailyQuota(e.target.value)}
                />
                <p className="text-xs text-muted-foreground">0 表示不限制</p>
              </div>
            </div>
            <DialogFooter>
              <Button onClick={handleCreateGrant} disabled={creating || !principalId.trim()}>
                {creating ? "创建中..." : "创建"}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      </div>

      {loading ? (
        <div className="space-y-2">
          <Skeleton className="h-8 w-full" />
          <Skeleton className="h-8 w-full" />
        </div>
      ) : grants.length === 0 ? (
        <p className="text-sm text-muted-foreground text-center py-4">
          暂无发件授权，点击「添加授权」创建
        </p>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>主体类型</TableHead>
              <TableHead>主体 ID</TableHead>
              <TableHead>每日配额</TableHead>
              <TableHead>创建时间</TableHead>
              <TableHead className="w-10" />
            </TableRow>
          </TableHeader>
          <TableBody>
            {grants.map((grant) => (
              <TableRow key={grant.id}>
                <TableCell>
                  <Badge variant="outline" className="text-xs">
                    {grant.principal_type}
                  </Badge>
                </TableCell>
                <TableCell className="font-mono text-xs">
                  {grant.principal_id}
                </TableCell>
                <TableCell className="text-sm">
                  {grant.daily_quota === 0 ? "不限" : grant.daily_quota}
                </TableCell>
                <TableCell className="text-sm text-muted-foreground">
                  {formatDistanceToNow(new Date(grant.created_at), { addSuffix: true })}
                </TableCell>
                <TableCell>
                  <Button
                    variant="ghost"
                    size="icon"
                    className="h-8 w-8 text-destructive hover:text-destructive hover:bg-destructive/10"
                    onClick={() => handleDeleteGrant(grant.id)}
                  >
                    <Trash2 className="h-4 w-4" />
                  </Button>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
    </div>
  );
}

export default function SendIdentitiesPage() {
  const { t } = useI18n();
  const { level } = useAuth();
  const isAdmin = level === "platform_admin" || level === "tenant_admin";

  const { data: response, isLoading: loading, error, mutate } = useAPI(
    "send-identities",
    () => listSendIdentities(),
  );
  const identities = response?.data ?? [];
  const total = identities.length;

  // Load zones for the create dialog
  const { data: zonesResponse } = useAPI("domains", () => listDomains());
  const zones: DomainZone[] = zonesResponse?.data ?? [];

  useEffect(() => {
    if (error) toast.error("加载发件身份失败");
  }, [error]);

  const [dialogOpen, setDialogOpen] = useState(false);
  const [creating, setCreating] = useState(false);
  const [newZoneId, setNewZoneId] = useState("");
  const [newAddress, setNewAddress] = useState("");

  // Track which identity rows are expanded
  const [expandedIds, setExpandedIds] = useState<Set<string>>(new Set());

  const toggleExpand = (id: string) => {
    setExpandedIds((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  };

  const handleCreate = async () => {
    if (!newAddress.trim() || !newZoneId) return;
    setCreating(true);
    try {
      await createSendIdentity({
        zone_id: newZoneId,
        address: newAddress.trim(),
      });
      setNewAddress("");
      setNewZoneId("");
      setDialogOpen(false);
      toast.success("发件身份已创建");
      mutate();
    } catch (err: unknown) {
      const apiErr = err as { error?: { message?: string } };
      toast.error(apiErr?.error?.message || "创建发件身份失败");
    } finally {
      setCreating(false);
    }
  };

  const handleDelete = async (id: string) => {
    if (!safeConfirm("确定要删除此发件身份吗？关联的所有发件授权也会被删除。")) return;
    try {
      await deleteSendIdentity(id);
      toast.success("发件身份已删除");
      mutate();
    } catch {
      toast.error("删除发件身份失败");
    }
  };

  const identityTypeLabels: Record<string, string> = {
    exact: "精确",
    domain_wildcard: "域名通配",
  };

  return (
    <div className="flex flex-col">
      <PageHeader
        title="发件身份"
        description={`共 ${total} 个发件身份`}
        actions={
          isAdmin ? (
            <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
              <DialogTrigger render={<Button size="sm" className="gap-1.5" />}>
                <Plus className="h-3.5 w-3.5" />
                创建发件身份
              </DialogTrigger>
              <DialogContent className="sm:max-w-md">
                <DialogHeader>
                  <DialogTitle>创建发件身份</DialogTitle>
                  <DialogDescription>
                    在指定域名下创建发件身份，用于控制谁可以以此地址发送邮件
                  </DialogDescription>
                </DialogHeader>
                <div className="space-y-4 py-4">
                  <div className="space-y-2">
                    <Label>域名</Label>
                    <Select value={newZoneId} onValueChange={(v) => v && setNewZoneId(v)}>
                      <SelectTrigger>
                        <SelectValue placeholder="选择域名" />
                      </SelectTrigger>
                      <SelectContent>
                        {zones.map((zone) => (
                          <SelectItem key={zone.id} value={zone.id}>
                            {zone.domain}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  </div>
                  <div className="space-y-2">
                    <Label>发件地址</Label>
                    <Input
                      placeholder="noreply@example.com 或 *@example.com"
                      value={newAddress}
                      onChange={(e) => setNewAddress(e.target.value)}
                      onKeyDown={(e) => e.key === "Enter" && handleCreate()}
                    />
                    <p className="text-xs text-muted-foreground">
                      使用 *@domain 创建域名通配身份，匹配该域名下所有地址
                    </p>
                  </div>
                </div>
                <DialogFooter>
                  <Button onClick={handleCreate} disabled={creating || !newAddress.trim() || !newZoneId}>
                    {creating ? "创建中..." : "创建"}
                  </Button>
                </DialogFooter>
              </DialogContent>
            </Dialog>
          ) : null
        }
      />

      <div className="p-4">
        <Card className="tm-reveal tm-reveal-1">
          <CardHeader className="pb-3">
            <CardTitle className="flex items-center gap-2 text-base">
              <Send className="h-4 w-4 text-primary" />
              全部发件身份
            </CardTitle>
            <CardDescription>
              管理发件身份及其授权。展开行可查看和管理发件授权 (Send-As Grants)。
            </CardDescription>
          </CardHeader>
          <CardContent>
            {loading ? (
              <div className="space-y-3">
                {Array.from({ length: 3 }).map((_, i) => (
                  <Skeleton key={i} className="h-12 w-full" />
                ))}
              </div>
            ) : identities.length === 0 ? (
              <div className="text-center py-12 text-muted-foreground">
                <Send className="h-10 w-10 mx-auto mb-3 opacity-30" />
                <p className="text-sm">暂无发件身份</p>
                <p className="text-xs mt-1">
                  {isAdmin ? "点击右上角按钮创建发件身份" : "请联系管理员创建发件身份"}
                </p>
              </div>
            ) : (
              <div className="space-y-2">
                {identities.map((identity) => {
                  const expanded = expandedIds.has(identity.id);
                  const zone = zones.find((z) => z.id === identity.zone_id);
                  return (
                    <Card key={identity.id} className="overflow-hidden">
                      <div
                        className="flex items-center gap-3 px-5 py-3 cursor-pointer hover:bg-muted/30 transition-colors"
                        onClick={() => toggleExpand(identity.id)}
                      >
                        <div className="flex-1 min-w-0">
                          <div className="flex items-center gap-2.5">
                            <code className="text-sm font-semibold">{identity.address}</code>
                            <Badge variant="outline" className="text-[10px]">
                              {identityTypeLabels[identity.identity_type] || identity.identity_type}
                            </Badge>
                            {identity.verified ? (
                              <Badge variant="default" className="gap-1 bg-green-600 hover:bg-green-700 text-[10px]">
                                <CheckCircle2 className="h-3 w-3" />
                                已验证
                              </Badge>
                            ) : (
                              <Badge variant="secondary" className="gap-1 text-[10px]">
                                <XCircle className="h-3 w-3" />
                                未验证
                              </Badge>
                            )}
                            {zone && (
                              <Badge variant="outline" className="text-[10px]">
                                {zone.domain}
                              </Badge>
                            )}
                          </div>
                          <p className="text-xs text-muted-foreground mt-0.5">
                            创建于 {formatDistanceToNow(new Date(identity.created_at), { addSuffix: true })}
                          </p>
                        </div>
                        <div className="flex items-center gap-1.5">
                          {isAdmin && (
                            <Button
                              variant="ghost"
                              size="icon"
                              className="h-8 w-8 text-destructive hover:text-destructive hover:bg-destructive/10"
                              onClick={(e) => {
                                e.stopPropagation();
                                handleDelete(identity.id);
                              }}
                            >
                              <Trash2 className="h-4 w-4" />
                            </Button>
                          )}
                          {expanded ? (
                            <ChevronUp className="h-4 w-4 text-muted-foreground" />
                          ) : (
                            <ChevronDown className="h-4 w-4 text-muted-foreground" />
                          )}
                        </div>
                      </div>

                      {expanded && <IdentityGrantsPanel identity={identity} />}
                    </Card>
                  );
                })}
              </div>
            )}
          </CardContent>
        </Card>
      </div>
    </div>
  );
}
