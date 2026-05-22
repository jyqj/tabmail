"use client";

import { useEffect, useState } from "react";
import { PageHeader } from "@/components/layout/page-header";
import { useAPI } from "@/hooks/use-api";
import { listAdminDomains, updateAdminDomainAccess } from "@/lib/api";
import type { DomainZone, ResourceVisibility } from "@/lib/types";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Globe, RefreshCw } from "lucide-react";
import { toast } from "sonner";

const visibilityLabels: Record<ResourceVisibility, string> = {
  private: "仅所有者",
  authenticated: "登录用户可用",
  public: "未登录可用",
};

function canEnableRandomSubdomains(zone: DomainZone) {
  return zone.is_verified && zone.mx_verified;
}

export default function AdminDomainsPage() {
  const { data: response, isLoading, error, mutate } = useAPI(
    "admin-domains",
    () => listAdminDomains(),
  );
  const zones = response?.data ?? [];
  const [saving, setSaving] = useState<Record<string, boolean>>({});

  useEffect(() => {
    if (error) toast.error("加载域名资源失败");
  }, [error]);

  const patchZone = async (zone: DomainZone, patch: { visibility?: ResourceVisibility; allow_random_subdomains?: boolean }) => {
    setSaving((prev) => ({ ...prev, [zone.id]: true }));
    try {
      await updateAdminDomainAccess(zone.id, patch);
      toast.success("域名资源策略已更新");
      mutate();
    } catch (e: unknown) {
      const err = e as { error?: { message?: string } };
      toast.error(err?.error?.message || "更新失败");
    } finally {
      setSaving((prev) => ({ ...prev, [zone.id]: false }));
    }
  };

  return (
    <div className="flex flex-col">
      <PageHeader
        title="域名资源"
        description="统一管理 tenant 私有域名、子域名资源、公开可用范围与随机子域名能力。"
        actions={
          <Button variant="outline" size="sm" onClick={() => mutate()} className="gap-1.5">
            <RefreshCw className="h-3.5 w-3.5" />
            刷新
          </Button>
        }
      />

      <div className="p-4">
        <Card>
          <CardHeader className="pb-3">
            <CardTitle className="flex items-center gap-2 text-base">
              <Globe className="h-4 w-4 text-primary" />
              全部域名资源
            </CardTitle>
            <CardDescription>
              public 资源会出现在未登录随机地址入口；authenticated 仅对登录用户开放。随机子域名必须先通过 TXT 与 MX 校验。
            </CardDescription>
          </CardHeader>
          <CardContent>
            {isLoading ? (
              <div className="space-y-3">
                {Array.from({ length: 6 }).map((_, i) => (
                  <Skeleton key={i} className="h-12 w-full" />
                ))}
              </div>
            ) : zones.length === 0 ? (
              <div className="py-12 text-center text-sm text-muted-foreground">暂无域名资源</div>
            ) : (
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>域名</TableHead>
                    <TableHead>租户</TableHead>
                    <TableHead>父资源</TableHead>
                    <TableHead>校验</TableHead>
                    <TableHead>开放范围</TableHead>
                    <TableHead>随机子域名</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {zones.map((zone) => (
                    <TableRow key={zone.id}>
                      <TableCell>
                        <div className="font-medium">{zone.domain}</div>
                        <div className="text-xs text-muted-foreground">{zone.id}</div>
                      </TableCell>
                      <TableCell className="font-mono text-xs text-muted-foreground">
                        {zone.tenant_id}
                      </TableCell>
                      <TableCell className="font-mono text-xs text-muted-foreground">
                        {zone.parent_zone_id || "—"}
                      </TableCell>
                      <TableCell>
                        <div className="flex gap-1.5">
                          <Badge variant={zone.is_verified ? "default" : "secondary"} className="text-[10px]">
                            TXT {zone.is_verified ? "OK" : "待校验"}
                          </Badge>
                          <Badge variant={zone.mx_verified ? "default" : "secondary"} className="text-[10px]">
                            MX {zone.mx_verified ? "OK" : "待校验"}
                          </Badge>
                        </div>
                      </TableCell>
                      <TableCell>
                        <Select
                          value={zone.visibility}
                          disabled={saving[zone.id]}
                          onValueChange={(value) => patchZone(zone, { visibility: value as ResourceVisibility })}
                        >
                          <SelectTrigger className="h-8 w-[150px]">
                            <SelectValue />
                          </SelectTrigger>
                          <SelectContent>
                            <SelectItem value="private">{visibilityLabels.private}</SelectItem>
                            <SelectItem value="authenticated">{visibilityLabels.authenticated}</SelectItem>
                            <SelectItem value="public">{visibilityLabels.public}</SelectItem>
                          </SelectContent>
                        </Select>
                      </TableCell>
                      <TableCell>
                        <div className="flex items-center gap-2">
                          <Switch
                            size="sm"
                            checked={zone.allow_random_subdomains}
                            disabled={saving[zone.id] || !canEnableRandomSubdomains(zone)}
                            onCheckedChange={(checked) => patchZone(zone, { allow_random_subdomains: checked })}
                          />
                          <span className="text-xs text-muted-foreground">
                            {canEnableRandomSubdomains(zone) ? "可配置" : "需先完成 TXT/MX"}
                          </span>
                        </div>
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
