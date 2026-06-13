"use client";

import { useState } from "react";
import { PageHeader } from "@/components/layout/page-header";
import { useCRUDPage } from "@/hooks/use-crud-page";
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
import { useI18n } from "@/lib/i18n";

function canEnableRandomSubdomains(zone: DomainZone) {
  return zone.is_verified && zone.mx_verified;
}

export default function AdminDomainsPage() {
  const { t } = useI18n();
  const { data: response, isLoading, mutate } = useCRUDPage(
    "admin-domains",
    () => listAdminDomains(),
    "adminDomains.loadFailed",
  );
  const zones = response?.data ?? [];
  const [saving, setSaving] = useState<Record<string, boolean>>({});
  const visibilityLabels: Record<ResourceVisibility, string> = {
    private: t("adminDomains.visibilityPrivate"),
    authenticated: t("adminDomains.visibilityAuthenticated"),
    public: t("adminDomains.visibilityPublic"),
  };

  const patchZone = async (zone: DomainZone, patch: { visibility?: ResourceVisibility; allow_random_subdomains?: boolean }) => {
    setSaving((prev) => ({ ...prev, [zone.id]: true }));
    try {
      await updateAdminDomainAccess(zone.id, patch);
      toast.success(t("adminDomains.updated"));
      mutate();
    } catch (e: unknown) {
      const err = e as { error?: { message?: string } };
      toast.error(err?.error?.message || t("adminDomains.updateFailed"));
    } finally {
      setSaving((prev) => ({ ...prev, [zone.id]: false }));
    }
  };

  return (
    <div className="flex flex-col">
      <PageHeader
        title={t("adminDomains.title")}
        description={t("adminDomains.description")}
        actions={
          <Button variant="outline" size="sm" onClick={() => mutate()} className="gap-1.5">
            <RefreshCw className="h-3.5 w-3.5" />
            {t("adminDomains.refresh")}
          </Button>
        }
      />

      <div className="p-4">
        <Card>
          <CardHeader className="pb-3">
            <CardTitle className="flex items-center gap-2 text-base">
              <Globe className="h-4 w-4 text-primary" />
              {t("adminDomains.allResources")}
            </CardTitle>
            <CardDescription>
              {t("adminDomains.allResourcesDesc")}
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
              <div className="py-12 text-center text-sm text-muted-foreground">{t("adminDomains.empty")}</div>
            ) : (
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>{t("adminDomains.domain")}</TableHead>
                    <TableHead>{t("adminDomains.tenant")}</TableHead>
                    <TableHead>{t("adminDomains.parent")}</TableHead>
                    <TableHead>{t("adminDomains.verification")}</TableHead>
                    <TableHead>{t("adminDomains.visibility")}</TableHead>
                    <TableHead>{t("adminDomains.randomSubdomains")}</TableHead>
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
                            TXT {zone.is_verified ? "OK" : t("adminDomains.pending")}
                          </Badge>
                          <Badge variant={zone.mx_verified ? "default" : "secondary"} className="text-[10px]">
                            MX {zone.mx_verified ? "OK" : t("adminDomains.pending")}
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
                            {canEnableRandomSubdomains(zone) ? t("adminDomains.configurable") : t("adminDomains.needTxtMx")}
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
