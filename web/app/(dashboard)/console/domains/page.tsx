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
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Skeleton } from "@/components/ui/skeleton";
import {
  listDomains,
  createDomain,
  deleteDomain,
  verifyDomain,
  getVerificationStatus,
} from "@/lib/api";
import type { DomainZone } from "@/lib/types";
import {
  Plus,
  MoreHorizontal,
  Trash2,
  Route,
  ShieldCheck,
  CheckCircle2,
  XCircle,
  Globe,
} from "lucide-react";
import { toast } from "sonner";
import { formatDistanceToNow } from "date-fns";
import { useI18n } from "@/lib/i18n";

export default function DomainsPage() {
  const { t } = useI18n();
  const [zones, setZones] = useState<DomainZone[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(true);
  const [dialogOpen, setDialogOpen] = useState(false);
  const [newDomain, setNewDomain] = useState("");
  const [creating, setCreating] = useState(false);

  const fetch = useCallback(async () => {
    try {
      const res = await listDomains();
      setZones(res.data);
      setTotal(res.data.length);
    } catch {
      toast.error(t("domains.loadFailed"));
    } finally {
      setLoading(false);
    }
  }, [t]);

  useEffect(() => {
    fetch();
  }, [fetch]);

  const handleCreate = async () => {
    if (!newDomain.trim()) return;
    setCreating(true);
    try {
      await createDomain(newDomain.trim());
      setNewDomain("");
      setDialogOpen(false);
      toast.success(t("domains.domainCreated"));
      fetch();
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
      fetch();
    } catch {
      toast.error(t("domains.deleteFailed"));
    }
  };

  const handleVerify = async (id: string) => {
    try {
      await verifyDomain(id);
      const status = await getVerificationStatus(id);
      const checks = status.data.checks;
      toast.success(
        `TXT ${checks.txt.status.toUpperCase()} · MX ${checks.mx.status.toUpperCase()}`
      );
      fetch();
    } catch {
      toast.error(t("domains.verifyFailed"));
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
        <Card>
          <CardHeader className="pb-3">
            <CardTitle className="text-base">{t("domains.domainZones")}</CardTitle>
            <CardDescription>
              {t("domains.zonesDesc")}
            </CardDescription>
          </CardHeader>
          <CardContent>
            {loading ? (
              <div className="space-y-3">
                {Array.from({ length: 3 }).map((_, i) => (
                  <Skeleton key={i} className="h-12 w-full" />
                ))}
              </div>
            ) : zones.length === 0 ? (
              <div className="text-center py-12 text-muted-foreground">
                <Globe className="h-10 w-10 mx-auto mb-3 opacity-30" />
                <p className="text-sm">{t("domains.noDomains")}</p>
                <p className="text-xs mt-1">{t("domains.noDomainsHint")}</p>
              </div>
            ) : (
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>{t("domains.domain")}</TableHead>
                    <TableHead>{t("domains.status")}</TableHead>
                    <TableHead>{t("domains.txtRecord")}</TableHead>
                    <TableHead>{t("domains.created")}</TableHead>
                    <TableHead className="w-10" />
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {zones.map((zone) => (
                    <TableRow key={zone.id}>
                      <TableCell>
                        <div className="flex items-center gap-2">
                          <Globe className="h-4 w-4 text-muted-foreground" />
                          <span className="font-medium">{zone.domain}</span>
                        </div>
                      </TableCell>
                      <TableCell>
                        <div className="flex items-center gap-1.5">
                          {zone.is_verified ? (
                            <Badge variant="default" className="gap-1 bg-green-600 hover:bg-green-700">
                              <CheckCircle2 className="h-3 w-3" />
                              {t("domains.verified")}
                            </Badge>
                          ) : (
                            <Badge variant="secondary" className="gap-1">
                              <XCircle className="h-3 w-3" />
                              {t("domains.pending")}
                            </Badge>
                          )}
                          {zone.mx_verified && (
                            <Badge variant="outline" className="gap-1 text-xs">
                              MX OK
                            </Badge>
                          )}
                        </div>
                      </TableCell>
                      <TableCell>
                        <code className="text-xs text-muted-foreground max-w-[200px] truncate block">
                          {zone.txt_record}
                        </code>
                      </TableCell>
                      <TableCell className="text-sm text-muted-foreground">
                        {formatDistanceToNow(new Date(zone.created_at), {
                          addSuffix: true,
                        })}
                      </TableCell>
                      <TableCell>
                        <DropdownMenu>
                          <DropdownMenuTrigger render={<Button variant="ghost" size="icon" className="h-8 w-8" />}>
                            <MoreHorizontal className="h-4 w-4" />
                          </DropdownMenuTrigger>
                          <DropdownMenuContent align="end">
                            <DropdownMenuItem render={<Link href={`/console/domains/${zone.id}/routes`} />}>
                              <Route className="h-4 w-4 mr-2" />
                              {t("domains.routes")}
                            </DropdownMenuItem>
                            <DropdownMenuItem onClick={() => handleVerify(zone.id)}>
                              <ShieldCheck className="h-4 w-4 mr-2" />
                              {t("domains.verifyDns")}
                            </DropdownMenuItem>
                            <DropdownMenuItem
                              onClick={() => handleDelete(zone.id)}
                              className="text-destructive focus:text-destructive"
                            >
                              <Trash2 className="h-4 w-4 mr-2" />
                              {t("domains.delete")}
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
