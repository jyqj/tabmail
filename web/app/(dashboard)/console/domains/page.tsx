"use client";

import { useState } from "react";
import { useCRUDPage } from "@/hooks/use-crud-page";
import { PageHeader } from "@/components/layout/page-header";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Card,
  CardContent,
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
import { Skeleton } from "@/components/ui/skeleton";
import {
  listDomains,
  createDomain,
  deleteDomain,
  suggestAddress,
} from "@/lib/api";
import { Plus, Globe } from "lucide-react";
import { toast } from "sonner";
import { useI18n } from "@/lib/i18n";
import { cn, safeConfirm } from "@/lib/utils";
import { useAuth, usePermissions } from "@/contexts/auth-context";
import { canCreateDomains } from "@/lib/permissions";
import { DomainCard } from "./domain-card";

export default function DomainsPage() {
  const { t } = useI18n();
  const { level } = useAuth();
  const permissions = usePermissions();
  const { data: response, isLoading: loading, mutate } = useCRUDPage(
    "domains",
    () => listDomains(),
    "domains.loadFailed",
  );
  const zones = response?.data ?? [];
  const total = zones.length;
  // UX-only gate; the backend authz seam is authoritative.
  const canCreate = canCreateDomains(level, permissions);
  const hasQuota = permissions != null && permissions.max_domains > 0;
  const atLimit = hasQuota && total >= permissions.max_domains;

  const [dialogOpen, setDialogOpen] = useState(false);
  const [newDomain, setNewDomain] = useState("");
  const [creating, setCreating] = useState(false);

  const handleCreate = async () => {
    if (!canCreate || atLimit) return;
    if (!newDomain.trim()) return;
    setCreating(true);
    try {
      await createDomain(newDomain.trim());
      setNewDomain("");
      setDialogOpen(false);
      toast.success(t("domains.domainCreated"));
      mutate();
    } catch (e: unknown) {
      const err = e as { error?: { message?: string } };
      toast.error(err?.error?.message || t("domains.createFailed"));
    } finally {
      setCreating(false);
    }
  };

  const handleDelete = async (id: string) => {
    if (!safeConfirm(t("domains.confirmDelete"))) return;
    try {
      await deleteDomain(id);
      toast.success(t("domains.deleted"));
      mutate();
    } catch {
      toast.error(t("domains.deleteFailed"));
    }
  };

  const handleSuggestAddress = async (id: string, subdomain = false) => {
    try {
      const res = await suggestAddress(id, { subdomain });
      await navigator.clipboard.writeText(res.data.address);
      toast.success(t("domains.addressGenerated"), { description: res.data.address });
    } catch (e: unknown) {
      const err = e as { error?: { message?: string } };
      toast.error(err?.error?.message || t("domains.addressGenerateFailed"));
    }
  };

  return (
    <div className="flex flex-col">
      <PageHeader
        title={t("domains.title")}
        description={t("domains.count", { count: total })}
        actions={
          <div className="flex items-center gap-3">
            {hasQuota && (
              <span className={cn(
                "text-sm tabular-nums",
                atLimit ? "text-destructive font-medium" : "text-muted-foreground",
              )}>
                {total} / {permissions.max_domains}
              </span>
            )}
            {canCreate && (
              <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
                <DialogTrigger render={<Button size="sm" className="gap-1.5" disabled={atLimit} />}>
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
            )}
          </div>
        }
      />

      <div className="p-4">
        <div className="space-y-3 tm-reveal tm-reveal-1">
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
                onVerify={() => mutate()}
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
