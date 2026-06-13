"use client";

import { useState } from "react";
import { useAPI } from "@/hooks/use-api";
import { useCRUDPage } from "@/hooks/use-crud-page";
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
  listDomains,
} from "@/lib/api";
import type { SendIdentity, DomainZone } from "@/lib/types";
import {
  Plus,
  Trash2,
  Send,
  CheckCircle2,
  XCircle,
} from "lucide-react";
import { toast } from "sonner";
import { formatDistanceToNow } from "date-fns";
import { zhCN, enUS } from "date-fns/locale";
import { useI18n } from "@/lib/i18n";
import { useAuth } from "@/contexts/auth-context";
import { isAdminLevel } from "@/lib/permissions";
import { safeConfirm } from "@/lib/utils";

export default function SendIdentitiesPage() {
  const { locale, t } = useI18n();
  const dateFnsLocale = locale === "zh" ? zhCN : enUS;
  const { level } = useAuth();
  // UX-only gate; the backend authz seam is authoritative.
  const isAdmin = isAdminLevel(level);

  const { data: response, isLoading: loading, mutate } = useCRUDPage(
    "send-identities",
    () => listSendIdentities(),
    "grants.sendIdentity.loadFailed",
  );
  const identities = response?.data ?? [];
  const total = identities.length;

  // Load zones for the create dialog
  const { data: zonesResponse } = useAPI("domains", () => listDomains());
  const zones: DomainZone[] = zonesResponse?.data ?? [];

  const [dialogOpen, setDialogOpen] = useState(false);
  const [creating, setCreating] = useState(false);
  const [newZoneId, setNewZoneId] = useState("");
  const [newAddress, setNewAddress] = useState("");

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
      toast.success(t("grants.sendIdentity.created"));
      mutate();
    } catch (err: unknown) {
      const apiErr = err as { error?: { message?: string } };
      toast.error(apiErr?.error?.message || t("grants.sendIdentity.createFailed"));
    } finally {
      setCreating(false);
    }
  };

  const handleDelete = async (id: string) => {
    if (!safeConfirm(t("grants.sendIdentity.confirmDelete"))) return;
    try {
      await deleteSendIdentity(id);
      toast.success(t("grants.sendIdentity.deleted"));
      mutate();
    } catch {
      toast.error(t("grants.sendIdentity.deleteFailed"));
    }
  };

  const identityTypeLabels: Record<string, string> = {
    exact: t("grants.sendIdentity.type.exact"),
    domain_wildcard: t("grants.sendIdentity.type.domainWildcard"),
  };

  return (
    <div className="flex flex-col">
      <PageHeader
        title={t("grants.sendIdentity.title")}
        description={t("grants.sendIdentity.total", { count: total })}
        actions={
          isAdmin ? (
            <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
              <DialogTrigger render={<Button size="sm" className="gap-1.5" />}>
                <Plus className="h-3.5 w-3.5" />
                {t("grants.sendIdentity.create")}
              </DialogTrigger>
              <DialogContent className="sm:max-w-md">
                <DialogHeader>
                  <DialogTitle>{t("grants.sendIdentity.create")}</DialogTitle>
                  <DialogDescription>
                    {t("grants.sendIdentity.createDesc")}
                  </DialogDescription>
                </DialogHeader>
                <div className="space-y-4 py-4">
                  <div className="space-y-2">
                    <Label>{t("grants.sendIdentity.zone")}</Label>
                    <Select value={newZoneId} onValueChange={(v) => v && setNewZoneId(v)}>
                      <SelectTrigger>
                        <SelectValue placeholder={t("grants.sendIdentity.zonePlaceholder")} />
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
                    <Label>{t("grants.sendIdentity.address")}</Label>
                    <Input
                      placeholder={t("grants.sendIdentity.addressPlaceholder")}
                      value={newAddress}
                      onChange={(e) => setNewAddress(e.target.value)}
                      onKeyDown={(e) => e.key === "Enter" && handleCreate()}
                    />
                    <p className="text-xs text-muted-foreground">
                      {t("grants.sendIdentity.wildcardHint")}
                    </p>
                  </div>
                </div>
                <DialogFooter>
                  <Button onClick={handleCreate} disabled={creating || !newAddress.trim() || !newZoneId}>
                    {creating ? t("grants.creating") : t("grants.create")}
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
              {t("grants.sendIdentity.listTitle")}
            </CardTitle>
            <CardDescription>
              {t("grants.sendIdentity.listDesc")}
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
                <p className="text-sm">{t("grants.sendIdentity.empty")}</p>
                <p className="text-xs mt-1">
                  {isAdmin ? t("grants.sendIdentity.emptyAdminHint") : t("grants.sendIdentity.emptyUserHint")}
                </p>
              </div>
            ) : (
              <div className="space-y-2">
                {identities.map((identity) => {
                  const zone = zones.find((z) => z.id === identity.zone_id);
                  return (
                    <Card key={identity.id} className="overflow-hidden">
                      <div className="flex items-center gap-3 px-5 py-3">
                        <div className="flex-1 min-w-0">
                          <div className="flex items-center gap-2.5">
                            <code className="text-sm font-semibold">{identity.address}</code>
                            <Badge variant="outline" className="text-[10px]">
                              {identityTypeLabels[identity.identity_type] || identity.identity_type}
                            </Badge>
                            {identity.verified ? (
                              <Badge variant="default" className="gap-1 bg-green-600 hover:bg-green-700 text-[10px]">
                                <CheckCircle2 className="h-3 w-3" />
                                {t("grants.sendIdentity.verified")}
                              </Badge>
                            ) : (
                              <Badge variant="secondary" className="gap-1 text-[10px]">
                                <XCircle className="h-3 w-3" />
                                {t("grants.sendIdentity.unverified")}
                              </Badge>
                            )}
                            {zone && (
                              <Badge variant="outline" className="text-[10px]">
                                {zone.domain}
                              </Badge>
                            )}
                          </div>
                          <p className="text-xs text-muted-foreground mt-0.5">
                            {t("grants.sendIdentity.createdAt", {
                              time: formatDistanceToNow(new Date(identity.created_at), {
                                addSuffix: true,
                                locale: dateFnsLocale,
                              }),
                            })}
                          </p>
                        </div>
                        <div className="flex items-center gap-1.5">
                          {isAdmin && (
                            <Button
                              variant="ghost"
                              size="icon"
                              className="h-8 w-8 text-destructive hover:text-destructive hover:bg-destructive/10"
                              onClick={() => handleDelete(identity.id)}
                            >
                              <Trash2 className="h-4 w-4" />
                            </Button>
                          )}
                        </div>
                      </div>
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
