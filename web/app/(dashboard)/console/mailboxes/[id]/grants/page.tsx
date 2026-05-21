"use client";

import { useState, useEffect, useCallback } from "react";
import { useParams } from "next/navigation";
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
import { listMailboxGrants, createMailboxGrant, deleteMailboxGrant } from "@/lib/api";
import type { MailboxGrant, MailboxGrantRole } from "@/lib/types";
import { Plus, Trash2, Inbox } from "lucide-react";
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

const roleColors: Record<MailboxGrantRole, string> = {
  owner: "bg-amber-100 text-amber-800 dark:bg-amber-900/30 dark:text-amber-400",
  manager: "bg-red-100 text-red-800 dark:bg-red-900/30 dark:text-red-400",
  writer: "bg-blue-100 text-blue-800 dark:bg-blue-900/30 dark:text-blue-400",
  reader: "bg-green-100 text-green-800 dark:bg-green-900/30 dark:text-green-400",
};

export default function MailboxGrantsPage() {
  const { t } = useI18n();
  const { level } = useAuth();
  const isAdmin = level === "platform_admin" || level === "tenant_admin";
  const params = useParams();
  const mailboxId = params.id as string;

  const [grants, setGrants] = useState<MailboxGrant[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(false);

  const [dialogOpen, setDialogOpen] = useState(false);
  const [creating, setCreating] = useState(false);
  const [principalType, setPrincipalType] = useState("user");
  const [principalId, setPrincipalId] = useState("");
  const [role, setRole] = useState<MailboxGrantRole>("reader");

  const fetchGrants = useCallback(async () => {
    setLoading(true);
    try {
      const res = await listMailboxGrants(mailboxId);
      setGrants(res.data ?? []);
      setError(false);
    } catch {
      setError(true);
      toast.error("加载邮箱授权失败");
    } finally {
      setLoading(false);
    }
  }, [mailboxId]);

  useEffect(() => {
    fetchGrants();
  }, [fetchGrants]);

  const handleCreate = async () => {
    if (!principalId.trim()) return;
    setCreating(true);
    try {
      await createMailboxGrant(mailboxId, {
        principal_type: principalType,
        principal_id: principalId.trim(),
        role,
      });
      setPrincipalId("");
      setRole("reader");
      setDialogOpen(false);
      toast.success("邮箱授权已创建");
      fetchGrants();
    } catch (err: unknown) {
      const apiErr = err as { error?: { message?: string } };
      toast.error(apiErr?.error?.message || "创建邮箱授权失败");
    } finally {
      setCreating(false);
    }
  };

  const handleDelete = async (grantId: string) => {
    if (!safeConfirm("确定要删除此邮箱授权吗？")) return;
    try {
      await deleteMailboxGrant(mailboxId, grantId);
      toast.success("邮箱授权已删除");
      fetchGrants();
    } catch {
      toast.error("删除邮箱授权失败");
    }
  };

  return (
    <div className="flex flex-col">
      <PageHeader
        title="邮箱授权"
        description={`共 ${grants.length} 项授权`}
        actions={
          isAdmin ? (
            <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
              <DialogTrigger render={<Button size="sm" className="gap-1.5" />}>
                <Plus className="h-3.5 w-3.5" />
                添加授权
              </DialogTrigger>
              <DialogContent className="sm:max-w-md">
                <DialogHeader>
                  <DialogTitle>添加邮箱授权</DialogTitle>
                  <DialogDescription>
                    授予用户或 API Key 对此邮箱的访问权限
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
                      onKeyDown={(e) => e.key === "Enter" && handleCreate()}
                    />
                  </div>
                  <div className="space-y-2">
                    <Label>角色</Label>
                    <Select value={role} onValueChange={(v) => v && setRole(v as MailboxGrantRole)}>
                      <SelectTrigger>
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="owner">所有者 (owner)</SelectItem>
                        <SelectItem value="manager">管理员 (manager)</SelectItem>
                        <SelectItem value="writer">写入者 (writer)</SelectItem>
                        <SelectItem value="reader">读取者 (reader)</SelectItem>
                      </SelectContent>
                    </Select>
                    <p className="text-xs text-muted-foreground">
                      owner 拥有完全控制权，reader 仅可查看邮件
                    </p>
                  </div>
                </div>
                <DialogFooter>
                  <Button onClick={handleCreate} disabled={creating || !principalId.trim()}>
                    {creating ? "创建中..." : "创建"}
                  </Button>
                </DialogFooter>
              </DialogContent>
            </Dialog>
          ) : null
        }
      />

      <div className="p-4">
        <Card>
          <CardHeader className="pb-3">
            <CardTitle className="flex items-center gap-2 text-base">
              <Inbox className="h-4 w-4 text-primary" />
              邮箱授权列表
            </CardTitle>
            <CardDescription>
              管理此邮箱的访问授权。不同角色拥有不同的操作权限。
            </CardDescription>
          </CardHeader>
          <CardContent>
            {loading ? (
              <div className="space-y-3">
                {Array.from({ length: 3 }).map((_, i) => (
                  <Skeleton key={i} className="h-12 w-full" />
                ))}
              </div>
            ) : error ? (
              <div className="text-center py-12 text-muted-foreground">
                <p className="text-sm">加载失败，请稍后重试</p>
              </div>
            ) : grants.length === 0 ? (
              <div className="text-center py-12 text-muted-foreground">
                <Inbox className="h-10 w-10 mx-auto mb-3 opacity-30" />
                <p className="text-sm">暂无邮箱授权</p>
                <p className="text-xs mt-1">
                  {isAdmin ? "点击右上角按钮添加授权" : "请联系管理员添加授权"}
                </p>
              </div>
            ) : (
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>主体类型</TableHead>
                    <TableHead>主体 ID</TableHead>
                    <TableHead>角色</TableHead>
                    <TableHead>创建时间</TableHead>
                    {isAdmin && <TableHead className="w-10" />}
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
                      <TableCell>
                        <Badge
                          variant="secondary"
                          className={roleColors[grant.role]}
                        >
                          {grant.role}
                        </Badge>
                      </TableCell>
                      <TableCell className="text-sm text-muted-foreground">
                        {formatDistanceToNow(new Date(grant.created_at), { addSuffix: true })}
                      </TableCell>
                      {isAdmin && (
                        <TableCell>
                          <Button
                            variant="ghost"
                            size="icon"
                            className="h-8 w-8 text-destructive hover:text-destructive hover:bg-destructive/10"
                            onClick={() => handleDelete(grant.id)}
                          >
                            <Trash2 className="h-4 w-4" />
                          </Button>
                        </TableCell>
                      )}
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
