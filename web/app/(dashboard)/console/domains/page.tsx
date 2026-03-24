"use client";

import { useState, useEffect, useCallback } from "react";
import Link from "next/link";
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
  listDomains,
  createDomain,
  deleteDomain,
  verifyDomain,
  getVerificationStatus,
  suggestAddress,
} from "@/lib/api";
import type { DomainZone, VerificationStatus } from "@/lib/types";
import {
  Plus,
  MoreHorizontal,
  Trash2,
  Route,
  ShieldCheck,
  CheckCircle2,
  XCircle,
  Globe,
  Shuffle,
  Copy,
  Check,
  AlertCircle,
  RefreshCw,
  ChevronDown,
  ChevronUp,
  ExternalLink,
} from "lucide-react";
import { toast } from "sonner";
import { formatDistanceToNow } from "date-fns";
import { useI18n } from "@/lib/i18n";
import { cn } from "@/lib/utils";

function CopyButton({ value, className }: { value: string; className?: string }) {
  const [copied, setCopied] = useState(false);
  return (
    <button
      onClick={(e) => {
        e.stopPropagation();
        navigator.clipboard.writeText(value);
        setCopied(true);
        toast.success("已复制");
        setTimeout(() => setCopied(false), 2000);
      }}
      className={cn(
        "inline-flex items-center justify-center h-6 w-6 rounded hover:bg-muted transition-colors shrink-0",
        className
      )}
    >
      {copied ? <Check className="h-3 w-3 text-green-500" /> : <Copy className="h-3 w-3 text-muted-foreground" />}
    </button>
  );
}

function DNSCheckRow({ label, status, details, expected }: {
  label: string;
  status: string;
  details?: string[];
  expected?: string;
}) {
  const pass = status === "pass";
  return (
    <div className="flex items-start gap-3 py-2.5 border-b border-border/40 last:border-0">
      <div className="mt-0.5">
        {pass ? (
          <CheckCircle2 className="h-4 w-4 text-green-500" />
        ) : (
          <XCircle className="h-4 w-4 text-red-400" />
        )}
      </div>
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-2">
          <span className="text-sm font-medium">{label}</span>
          <Badge variant={pass ? "default" : "secondary"} className={cn("text-[10px]", pass && "bg-green-600")}>
            {pass ? "PASS" : "FAIL"}
          </Badge>
        </div>
        {expected && !pass && (
          <div className="mt-1.5 flex items-center gap-1.5">
            <span className="text-xs text-muted-foreground">期望:</span>
            <code className="text-xs bg-muted px-1.5 py-0.5 rounded font-mono">{expected}</code>
            <CopyButton value={expected} />
          </div>
        )}
        {details && details.length > 0 && (
          <div className="mt-1">
            <span className="text-xs text-muted-foreground">当前:</span>
            {details.map((d, i) => (
              <code key={i} className="block text-xs text-muted-foreground/80 font-mono mt-0.5 break-all">{d}</code>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}

function DNSSetupGuide({ zone, verificationStatus }: {
  zone: DomainZone;
  verificationStatus: VerificationStatus | null;
}) {
  const expectedMx = verificationStatus?.expected_mx || "your-server.example.com";
  const allVerified = zone.is_verified && zone.mx_verified;

  if (allVerified) return null;

  return (
    <div className="rounded-lg border border-amber-200 dark:border-amber-900/50 bg-amber-50/50 dark:bg-amber-950/20 p-4 space-y-4">
      <div className="flex items-start gap-2.5">
        <AlertCircle className="h-4 w-4 text-amber-500 mt-0.5 shrink-0" />
        <div>
          <p className="text-sm font-medium text-amber-800 dark:text-amber-200">请配置以下 DNS 记录</p>
          <p className="text-xs text-amber-600 dark:text-amber-400 mt-0.5">
            在你的 DNS 服务商（如 Cloudflare、阿里云等）添加以下记录，然后点击「验证 DNS」
          </p>
        </div>
      </div>

      <div className="space-y-3">
        <div className="rounded-md bg-background border border-border/60 p-3">
          <div className="flex items-center justify-between mb-1.5">
            <span className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">
              TXT 记录（验证域名所有权）
            </span>
            {zone.is_verified ? (
              <Badge variant="default" className="bg-green-600 text-[10px]">已通过</Badge>
            ) : (
              <Badge variant="secondary" className="text-[10px]">未通过</Badge>
            )}
          </div>
          <div className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 text-xs mt-2">
            <span className="text-muted-foreground">名称:</span>
            <div className="flex items-center gap-1.5">
              <code className="font-mono bg-muted px-1.5 py-0.5 rounded">{zone.domain}</code>
              <CopyButton value={zone.domain} />
            </div>
            <span className="text-muted-foreground">类型:</span>
            <code className="font-mono">TXT</code>
            <span className="text-muted-foreground">值:</span>
            <div className="flex items-center gap-1.5">
              <code className="font-mono bg-muted px-1.5 py-0.5 rounded break-all">{zone.txt_record}</code>
              <CopyButton value={zone.txt_record} />
            </div>
          </div>
        </div>

        <div className="rounded-md bg-background border border-border/60 p-3">
          <div className="flex items-center justify-between mb-1.5">
            <span className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">
              MX 记录（接收邮件）
            </span>
            {zone.mx_verified ? (
              <Badge variant="default" className="bg-green-600 text-[10px]">已通过</Badge>
            ) : (
              <Badge variant="secondary" className="text-[10px]">未通过</Badge>
            )}
          </div>
          <div className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 text-xs mt-2">
            <span className="text-muted-foreground">名称:</span>
            <div className="flex items-center gap-1.5">
              <code className="font-mono bg-muted px-1.5 py-0.5 rounded">{zone.domain}</code>
              <CopyButton value={zone.domain} />
            </div>
            <span className="text-muted-foreground">类型:</span>
            <code className="font-mono">MX</code>
            <span className="text-muted-foreground">优先级:</span>
            <code className="font-mono">10</code>
            <span className="text-muted-foreground">目标:</span>
            <div className="flex items-center gap-1.5">
              <code className="font-mono bg-muted px-1.5 py-0.5 rounded">{expectedMx}</code>
              <CopyButton value={expectedMx} />
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}

function VerificationDetail({ checks }: { checks: VerificationStatus }) {
  return (
    <div className="space-y-0">
      <DNSCheckRow label="TXT (所有权)" status={checks.checks.txt.status} details={checks.checks.txt.details} expected={checks.txt_expected} />
      <DNSCheckRow label="MX (邮件接收)" status={checks.checks.mx.status} details={checks.checks.mx.details} expected={checks.expected_mx} />
      <DNSCheckRow label="SPF (发信授权)" status={checks.checks.spf.status} details={checks.checks.spf.details} />
      <DNSCheckRow label="DKIM (签名)" status={checks.checks.dkim.status} details={checks.checks.dkim.details} />
      <DNSCheckRow label="DMARC (策略)" status={checks.checks.dmarc.status} details={checks.checks.dmarc.details} />
    </div>
  );
}

function DomainCard({ zone, onVerify, onDelete, onSuggest }: {
  zone: DomainZone;
  onVerify: () => void;
  onDelete: () => void;
  onSuggest: (subdomain: boolean) => void;
}) {
  const [expanded, setExpanded] = useState(!zone.is_verified || !zone.mx_verified);
  const [verStatus, setVerStatus] = useState<VerificationStatus | null>(null);
  const [verLoading, setVerLoading] = useState(false);
  const [verifying, setVerifying] = useState(false);

  const loadStatus = useCallback(async () => {
    setVerLoading(true);
    try {
      const res = await getVerificationStatus(zone.id);
      setVerStatus(res.data);
    } catch { /* ignore */ }
    finally { setVerLoading(false); }
  }, [zone.id]);

  useEffect(() => {
    if (expanded && !verStatus) {
      loadStatus();
    }
  }, [expanded, verStatus, loadStatus]);

  const handleVerify = async () => {
    setVerifying(true);
    try {
      await verifyDomain(zone.id);
      const status = await getVerificationStatus(zone.id);
      setVerStatus(status.data);
      const txt = status.data.checks.txt.status;
      const mx = status.data.checks.mx.status;
      if (txt === "pass" && mx === "pass") {
        toast.success("域名验证通过！");
      } else {
        toast.warning(`TXT: ${txt.toUpperCase()} · MX: ${mx.toUpperCase()}`);
      }
      onVerify();
    } catch {
      toast.error("验证请求失败");
    } finally {
      setVerifying(false);
    }
  };

  const allVerified = zone.is_verified && zone.mx_verified;

  return (
    <Card className="overflow-hidden">
      <div
        className="flex items-center gap-3 px-5 py-4 cursor-pointer hover:bg-muted/30 transition-colors"
        onClick={() => setExpanded(!expanded)}
      >
        <Globe className="h-5 w-5 text-muted-foreground shrink-0" />
        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-2.5">
            <span className="font-semibold text-sm">{zone.domain}</span>
            <div className="flex items-center gap-1.5">
              {allVerified ? (
                <Badge variant="default" className="gap-1 bg-green-600 hover:bg-green-700 text-[10px]">
                  <CheckCircle2 className="h-3 w-3" />
                  已验证
                </Badge>
              ) : (
                <Badge variant="secondary" className="gap-1 text-[10px]">
                  <AlertCircle className="h-3 w-3" />
                  待验证
                </Badge>
              )}
              {zone.is_verified && !zone.mx_verified && (
                <Badge variant="outline" className="text-[10px] text-amber-600 border-amber-300">
                  TXT OK · MX 未配置
                </Badge>
              )}
            </div>
          </div>
          <p className="text-xs text-muted-foreground mt-0.5">
            创建于 {formatDistanceToNow(new Date(zone.created_at), { addSuffix: true })}
          </p>
        </div>

        <div className="flex items-center gap-1.5">
          <DropdownMenu>
            <DropdownMenuTrigger
              render={<Button variant="ghost" size="icon" className="h-8 w-8" />}
              onClick={(e: React.MouseEvent) => e.stopPropagation()}
            >
              <MoreHorizontal className="h-4 w-4" />
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end">
              <DropdownMenuItem render={<Link href={`/console/domains/${zone.id}/routes`} />}>
                <Route className="h-4 w-4 mr-2" />
                路由规则
              </DropdownMenuItem>
              <DropdownMenuSeparator />
              <DropdownMenuItem onClick={() => onSuggest(false)}>
                <Shuffle className="h-4 w-4 mr-2" />
                生成地址
                <Copy className="h-3.5 w-3.5 ml-auto opacity-60" />
              </DropdownMenuItem>
              <DropdownMenuItem onClick={() => onSuggest(true)}>
                <Shuffle className="h-4 w-4 mr-2" />
                生成子域地址
                <Copy className="h-3.5 w-3.5 ml-auto opacity-60" />
              </DropdownMenuItem>
              <DropdownMenuSeparator />
              <DropdownMenuItem onClick={onDelete} className="text-destructive focus:text-destructive">
                <Trash2 className="h-4 w-4 mr-2" />
                删除域名
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
          {expanded ? <ChevronUp className="h-4 w-4 text-muted-foreground" /> : <ChevronDown className="h-4 w-4 text-muted-foreground" />}
        </div>
      </div>

      {expanded && (
        <div className="border-t border-border/40 px-5 py-4 space-y-4 bg-muted/5">
          <DNSSetupGuide zone={zone} verificationStatus={verStatus} />

          <div className="flex items-center justify-between">
            <span className="text-sm font-medium">DNS 检查详情</span>
            <div className="flex items-center gap-2">
              <Button
                variant="outline"
                size="sm"
                className="gap-1.5 text-xs"
                onClick={(e) => { e.stopPropagation(); loadStatus(); }}
                disabled={verLoading}
              >
                <RefreshCw className={cn("h-3 w-3", verLoading && "animate-spin")} />
                刷新状态
              </Button>
              <Button
                size="sm"
                className="gap-1.5 text-xs"
                onClick={(e) => { e.stopPropagation(); handleVerify(); }}
                disabled={verifying}
              >
                <ShieldCheck className="h-3 w-3" />
                {verifying ? "验证中..." : "验证 DNS"}
              </Button>
            </div>
          </div>

          {verLoading && !verStatus ? (
            <div className="space-y-2">
              <Skeleton className="h-8 w-full" />
              <Skeleton className="h-8 w-full" />
            </div>
          ) : verStatus ? (
            <VerificationDetail checks={verStatus} />
          ) : (
            <p className="text-sm text-muted-foreground text-center py-4">点击「刷新状态」查看 DNS 检查结果</p>
          )}

          {allVerified && (
            <div className="flex items-center gap-2 pt-2">
              <Button variant="outline" size="sm" className="gap-1.5 text-xs" render={<Link href={`/console/domains/${zone.id}/routes`} />}>
                <Route className="h-3 w-3" />
                配置路由规则
                <ExternalLink className="h-3 w-3 opacity-60" />
              </Button>
            </div>
          )}
        </div>
      )}
    </Card>
  );
}

export default function DomainsPage() {
  const { t } = useI18n();
  const [zones, setZones] = useState<DomainZone[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(true);
  const [dialogOpen, setDialogOpen] = useState(false);
  const [newDomain, setNewDomain] = useState("");
  const [creating, setCreating] = useState(false);

  const fetchDomains = useCallback(async () => {
    try {
      const res = await listDomains();
      setZones(res.data ?? []);
      setTotal(res.data?.length ?? 0);
    } catch {
      toast.error(t("domains.loadFailed"));
    } finally {
      setLoading(false);
    }
  }, [t]);

  useEffect(() => {
    fetchDomains();
  }, [fetchDomains]);

  const handleCreate = async () => {
    if (!newDomain.trim()) return;
    setCreating(true);
    try {
      await createDomain(newDomain.trim());
      setNewDomain("");
      setDialogOpen(false);
      toast.success("域名已添加，请按指引配置 DNS 记录");
      fetchDomains();
    } catch (e: unknown) {
      const err = e as { error?: { message?: string } };
      toast.error(err?.error?.message || t("domains.createFailed"));
    } finally {
      setCreating(false);
    }
  };

  const handleDelete = async (id: string) => {
    try {
      await deleteDomain(id);
      toast.success(t("domains.deleted"));
      fetchDomains();
    } catch {
      toast.error(t("domains.deleteFailed"));
    }
  };

  const handleSuggestAddress = async (id: string, subdomain = false) => {
    try {
      const res = await suggestAddress(id, { subdomain });
      await navigator.clipboard.writeText(res.data.address);
      toast.success("地址已复制", { description: res.data.address });
    } catch (e: unknown) {
      const err = e as { error?: { message?: string } };
      toast.error(err?.error?.message || "生成地址失败");
    }
  };

  return (
    <div className="flex flex-col">
      <PageHeader
        title={t("domains.title")}
        description={t("domains.count", { count: total })}
        actions={
          <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
            <DialogTrigger render={<Button size="sm" className="gap-1.5" />}>
              <Plus className="h-3.5 w-3.5" />
              {t("domains.addDomain")}
            </DialogTrigger>
            <DialogContent className="sm:max-w-md">
              <DialogHeader>
                <DialogTitle>{t("domains.addTitle")}</DialogTitle>
                <DialogDescription>
                  {t("domains.addDesc")}
                </DialogDescription>
              </DialogHeader>
              <div className="space-y-2 py-4">
                <Label htmlFor="domain">{t("domains.domain")}</Label>
                <Input
                  id="domain"
                  placeholder={t("domains.placeholder")}
                  value={newDomain}
                  onChange={(e) => setNewDomain(e.target.value)}
                  onKeyDown={(e) => e.key === "Enter" && handleCreate()}
                />
              </div>
              <DialogFooter>
                <Button onClick={handleCreate} disabled={creating || !newDomain.trim()}>
                  {creating ? t("domains.creating") : t("domains.create")}
                </Button>
              </DialogFooter>
            </DialogContent>
          </Dialog>
        }
      />

      <div className="p-4">
        <div className="space-y-3">
          {loading ? (
            <div className="space-y-3">
              {Array.from({ length: 2 }).map((_, i) => (
                <Card key={i}><CardContent className="p-4"><Skeleton className="h-16 w-full" /></CardContent></Card>
              ))}
            </div>
          ) : zones.length === 0 ? (
            <Card>
              <CardContent className="py-12">
                <div className="text-center text-muted-foreground">
                  <Globe className="h-10 w-10 mx-auto mb-3 opacity-30" />
                  <p className="text-sm">{t("domains.noDomains")}</p>
                  <p className="text-xs mt-1">{t("domains.noDomainsHint")}</p>
                </div>
              </CardContent>
            </Card>
          ) : (
            zones.map((zone) => (
              <DomainCard
                key={zone.id}
                zone={zone}
                onVerify={fetchDomains}
                onDelete={() => handleDelete(zone.id)}
                onSuggest={(subdomain) => handleSuggestAddress(zone.id, subdomain)}
              />
            ))
          )}
        </div>
      </div>
    </div>
  );
}
