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
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import {
  listTenants,
  createTenant,
  deleteTenant,
  listPlans,
  updateTenantOverrides,
  getTenantConfig,
  createAPIKey,
  listAPIKeys,
  revokeAPIKey,
} from "@/lib/api";
import type { Tenant, Plan, TenantAPIKey, APIKeyCreated, TenantOverride, EffectiveConfig } from "@/lib/types";
import {
  Plus,
  MoreHorizontal,
  Trash2,
  KeyRound,
  Copy,
  Users,
  Shield,
   SlidersHorizontal,
   Gauge,
} from "lucide-react";
import { toast } from "sonner";
import { formatDistanceToNow } from "date-fns";
import { useI18n } from "@/lib/i18n";

const overrideFields = [
  "max_domains",
  "max_mailboxes_per_domain",
  "max_messages_per_mailbox",
  "max_message_bytes",
  "retention_hours",
  "rpm_limit",
  "daily_quota",
] as const;

type TenantOverrideEditableKey = (typeof overrideFields)[number];
type TenantOverrideForm = Record<TenantOverrideEditableKey, string>;

export default function TenantsPage() {
  const { t } = useI18n();
  const [tenants, setTenants] = useState<Tenant[]>([]);
  const [plans, setPlans] = useState<Plan[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(true);

  const [createOpen, setCreateOpen] = useState(false);
  const [creating, setCreating] = useState(false);
  const [newName, setNewName] = useState("");
  const [newPlanId, setNewPlanId] = useState("");

  const [keysOpen, setKeysOpen] = useState(false);
  const [keysTenantId, setKeysTenantId] = useState("");
  const [keys, setKeys] = useState<TenantAPIKey[]>([]);
  const [keysLoading, setKeysLoading] = useState(false);
  const [newKeyCreated, setNewKeyCreated] = useState<APIKeyCreated | null>(null);

  const [overrideOpen, setOverrideOpen] = useState(false);
  const [overrideTenant, setOverrideTenant] = useState<Tenant | null>(null);
  const [overrideSaving, setOverrideSaving] = useState(false);
  const [effectiveConfig, setEffectiveConfig] = useState<EffectiveConfig | null>(null);
  const [overrideForm, setOverrideForm] = useState<TenantOverrideForm>({
    max_domains: "",
    max_mailboxes_per_domain: "",
    max_messages_per_mailbox: "",
    max_message_bytes: "",
    retention_hours: "",
    rpm_limit: "",
    daily_quota: "",
  });

  const fetchTenants = useCallback(async () => {
    try {
      const [tRes, pRes] = await Promise.all([listTenants(), listPlans()]);
      setTenants(tRes.data);
      setTotal(tRes.data.length);
      setPlans(pRes.data);
    } catch {
      toast.error(t("tenants.loadFailed"));
    } finally {
      setLoading(false);
    }
  }, [t]);

  useEffect(() => {
    fetchTenants();
  }, [fetchTenants]);

  const handleCreate = async () => {
    if (!newName.trim() || !newPlanId) return;
    setCreating(true);
    try {
      await createTenant({ name: newName.trim(), plan_id: newPlanId });
      setNewName("");
      setNewPlanId("");
      setCreateOpen(false);
      toast.success(t("tenants.tenantCreated"));
      fetchTenants();
    } catch (e: unknown) {
      const err = e as { error?: { message?: string } };
      toast.error(err?.error?.message || t("tenants.createFailed"));
    } finally {
      setCreating(false);
    }
  };

  const handleDelete = async (id: string) => {
    try {
      await deleteTenant(id);
      toast.success(t("tenants.tenantDeleted"));
      fetchTenants();
    } catch {
      toast.error(t("tenants.deleteFailed"));
    }
  };

  const openKeys = async (tenantId: string) => {
    setKeysTenantId(tenantId);
    setKeysOpen(true);
    setKeysLoading(true);
    setNewKeyCreated(null);
    try {
      const res = await listAPIKeys(tenantId);
      setKeys(res.data);
    } catch {
      toast.error(t("tenants.keysLoadFailed"));
    } finally {
      setKeysLoading(false);
    }
  };

  const handleCreateKey = async () => {
    try {
      const res = await createAPIKey(keysTenantId, { scopes: ["*"] });
      setNewKeyCreated(res.data);
      const keysRes = await listAPIKeys(keysTenantId);
      setKeys(keysRes.data);
      toast.success(t("tenants.apiKeyCreated"));
    } catch {
      toast.error(t("tenants.apiKeyCreateFailed"));
    }
  };

  const handleRevokeKey = async (keyId: string) => {
    try {
      await revokeAPIKey(keysTenantId, keyId);
      setKeys((prev) => prev.filter((k) => k.id !== keyId));
      toast.success(t("tenants.keyRevoked"));
    } catch {
      toast.error(t("tenants.revokeFailed"));
    }
  };

  const planName = (id: string) => plans.find((p) => p.id === id)?.name ?? "—";

  const openOverrides = async (tenant: Tenant) => {
    setOverrideTenant(tenant);
    setOverrideOpen(true);
    setEffectiveConfig(null);
    setOverrideForm({
      max_domains: "",
      max_mailboxes_per_domain: "",
      max_messages_per_mailbox: "",
      max_message_bytes: "",
      retention_hours: "",
      rpm_limit: "",
      daily_quota: "",
    });
    try {
      const res = await getTenantConfig(tenant.id);
      setEffectiveConfig(res.data);
    } catch {
      toast.error(t("tenants.configLoadFailed"));
    }
  };

  const handleSaveOverrides = async () => {
    if (!overrideTenant) return;
    const body = Object.fromEntries(
      Object.entries(overrideForm).map(([key, value]) => [
        key,
        value.trim() === "" ? null : Number(value),
      ])
    ) as Pick<TenantOverride, TenantOverrideEditableKey>;
    setOverrideSaving(true);
    try {
      await updateTenantOverrides(overrideTenant.id, body);
      const res = await getTenantConfig(overrideTenant.id);
      setEffectiveConfig(res.data);
      toast.success(t("tenants.overridesUpdated"));
    } catch (e: unknown) {
      const err = e as { error?: { message?: string } };
      toast.error(err?.error?.message || t("tenants.overridesUpdateFailed"));
    } finally {
      setOverrideSaving(false);
    }
  };

  return (
    <div className="flex flex-col">
      <PageHeader
        title={t("tenants.title")}
        description={t("tenants.count", { count: total })}
        actions={
          <Dialog open={createOpen} onOpenChange={setCreateOpen}>
            <DialogTrigger render={<Button size="sm" className="gap-1.5" />}>
              <Plus className="h-3.5 w-3.5" />
              {t("tenants.createTenant")}
            </DialogTrigger>
            <DialogContent className="sm:max-w-md">
              <DialogHeader>
                <DialogTitle>{t("tenants.createTitle")}</DialogTitle>
                <DialogDescription>
                  {t("tenants.createDesc")}
                </DialogDescription>
              </DialogHeader>
              <div className="space-y-4 py-4">
                <div className="space-y-2">
                  <Label>{t("tenants.name")}</Label>
                  <Input
                    placeholder={t("tenants.placeholder")}
                    value={newName}
                    onChange={(e) => setNewName(e.target.value)}
                  />
                </div>
                <div className="space-y-2">
                  <Label>{t("tenants.plan")}</Label>
                  <Select value={newPlanId} onValueChange={(v) => v && setNewPlanId(v)}>
                    <SelectTrigger>
                      <SelectValue placeholder={t("tenants.selectPlan")} />
                    </SelectTrigger>
                    <SelectContent>
                      {plans.map((p) => (
                        <SelectItem key={p.id} value={p.id}>
                          {p.name}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
              </div>
              <DialogFooter>
                <Button
                  onClick={handleCreate}
                  disabled={creating || !newName.trim() || !newPlanId}
                >
                  {creating ? t("tenants.creating") : t("tenants.create")}
                </Button>
              </DialogFooter>
            </DialogContent>
          </Dialog>
        }
      />

      <div className="p-4 space-y-4">
        <Card>
          <CardHeader className="pb-3">
            <CardTitle className="text-base">{t("tenants.allTenants")}</CardTitle>
            <CardDescription>
              {t("tenants.allTenantsDesc")}
            </CardDescription>
          </CardHeader>
          <CardContent>
            {loading ? (
              <div className="space-y-3">
                {Array.from({ length: 3 }).map((_, i) => (
                  <Skeleton key={i} className="h-12 w-full" />
                ))}
              </div>
            ) : tenants.length === 0 ? (
              <div className="text-center py-12 text-muted-foreground">
                <Users className="h-10 w-10 mx-auto mb-3 opacity-30" />
                <p className="text-sm">{t("tenants.noTenants")}</p>
              </div>
            ) : (
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>{t("tenants.name")}</TableHead>
                    <TableHead>{t("tenants.plan")}</TableHead>
                    <TableHead>{t("tenants.role")}</TableHead>
                    <TableHead>{t("common.created")}</TableHead>
                    <TableHead className="w-10" />
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {tenants.map((tenant) => (
                    <TableRow key={tenant.id}>
                      <TableCell className="font-medium">{tenant.name}</TableCell>
                      <TableCell>
                        <Badge variant="secondary">{planName(tenant.plan_id)}</Badge>
                      </TableCell>
                      <TableCell>
                        {tenant.is_super ? (
                          <Badge className="gap-1 bg-amber-600 hover:bg-amber-700">
                            <Shield className="h-3 w-3" />
                            {t("tenants.super")}
                          </Badge>
                        ) : (
                          <Badge variant="outline">{t("tenants.tenant")}</Badge>
                        )}
                      </TableCell>
                      <TableCell className="text-sm text-muted-foreground">
                        {formatDistanceToNow(new Date(tenant.created_at), {
                          addSuffix: true,
                        })}
                      </TableCell>
                      <TableCell>
                        <DropdownMenu>
                          <DropdownMenuTrigger render={<Button variant="ghost" size="icon" className="h-8 w-8" />}>
                            <MoreHorizontal className="h-4 w-4" />
                          </DropdownMenuTrigger>
                          <DropdownMenuContent align="end">
                            <DropdownMenuItem onClick={() => openKeys(tenant.id)}>
                              <KeyRound className="h-4 w-4 mr-2" />
                              {t("tenants.apiKeys")}
                            </DropdownMenuItem>
                            <DropdownMenuItem onClick={() => openOverrides(tenant)}>
                              <SlidersHorizontal className="h-4 w-4 mr-2" />
                              {t("tenants.overrides")}
                            </DropdownMenuItem>
                            <DropdownMenuItem
                              onClick={() => {
                                navigator.clipboard.writeText(tenant.id);
                                toast.success(t("tenants.idCopied"));
                              }}
                            >
                              <Copy className="h-4 w-4 mr-2" />
                              {t("tenants.copyId")}
                            </DropdownMenuItem>
                            <DropdownMenuSeparator />
                            <DropdownMenuItem
                              onClick={() => handleDelete(tenant.id)}
                              className="text-destructive focus:text-destructive"
                              disabled={tenant.is_super}
                            >
                              <Trash2 className="h-4 w-4 mr-2" />
                              {t("tenants.delete")}
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

      {/* API Keys Dialog */}
      <Dialog open={keysOpen} onOpenChange={setKeysOpen}>
        <DialogContent className="sm:max-w-lg">
          <DialogHeader>
            <DialogTitle>{t("tenants.apiKeysTitle")}</DialogTitle>
            <DialogDescription>
              {t("tenants.apiKeysDesc")}
            </DialogDescription>
          </DialogHeader>

          {newKeyCreated && (
            <div className="rounded-lg border border-green-200 bg-green-50 dark:border-green-800 dark:bg-green-950 p-3">
              <p className="text-sm font-medium text-green-800 dark:text-green-200 mb-1">
                {t("tenants.newKeyCreated")}
              </p>
              <div className="flex items-center gap-2">
                <code className="flex-1 text-xs break-all bg-white dark:bg-black/20 p-2 rounded">
                  {newKeyCreated.key}
                </code>
                <Button
                  variant="outline"
                  size="icon"
                  className="h-8 w-8 shrink-0"
                  onClick={() => {
                    navigator.clipboard.writeText(newKeyCreated.key);
                    toast.success(t("tenants.copied"));
                  }}
                >
                  <Copy className="h-3.5 w-3.5" />
                </Button>
              </div>
            </div>
          )}

          <div className="space-y-2">
            {keysLoading ? (
              <div className="space-y-2">
                <Skeleton className="h-10 w-full" />
                <Skeleton className="h-10 w-full" />
              </div>
            ) : keys.length === 0 ? (
              <p className="text-sm text-muted-foreground text-center py-4">
                {t("tenants.noApiKeys")}
              </p>
            ) : (
              keys.map((k) => (
                <div
                  key={k.id}
                  className="flex items-center justify-between rounded-lg border px-3 py-2"
                >
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-2">
                      <code className="text-sm">{k.key_prefix}...</code>
                      {k.label && (
                        <Badge variant="secondary" className="text-xs">
                          {k.label}
                        </Badge>
                      )}
                    </div>
                    <p className="text-xs text-muted-foreground mt-0.5">
                      {t("tenants.scopes")}: {k.scopes.join(", ")}
                    </p>
                  </div>
                  <Button
                    variant="ghost"
                    size="icon"
                    className="h-8 w-8 text-destructive hover:text-destructive shrink-0"
                    onClick={() => handleRevokeKey(k.id)}
                  >
                    <Trash2 className="h-3.5 w-3.5" />
                  </Button>
                </div>
              ))
            )}
          </div>

          <DialogFooter>
            <Button size="sm" className="gap-1.5" onClick={handleCreateKey}>
              <Plus className="h-3.5 w-3.5" />
              {t("tenants.generateKey")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={overrideOpen} onOpenChange={setOverrideOpen}>
        <DialogContent className="sm:max-w-2xl">
          <DialogHeader>
            <DialogTitle>{t("tenants.overridesTitle")}</DialogTitle>
            <DialogDescription>
              {t("tenants.overridesDesc", { name: overrideTenant?.name ?? "" })}
            </DialogDescription>
          </DialogHeader>

          <div className="grid gap-6 py-4 lg:grid-cols-[0.9fr_1.1fr]">
            <Card className="border-primary/10 bg-[radial-gradient(circle_at_top,rgba(99,102,241,0.08),transparent_35%),var(--card)]">
              <CardHeader>
                <CardTitle className="flex items-center gap-2 text-base">
                  <Gauge className="h-4 w-4 text-primary" />
                  {t("tenants.effectiveConfig")}
                </CardTitle>
                <CardDescription>{t("tenants.effectiveConfigDesc")}</CardDescription>
              </CardHeader>
              <CardContent className="space-y-3">
                {effectiveConfig ? (
                  <>
                    {Object.entries(effectiveConfig).map(([key, value]) => (
                      <div key={key} className="flex items-center justify-between gap-3 text-sm">
                        <span className="text-muted-foreground">{key}</span>
                        <span className="font-medium tabular-nums">{String(value)}</span>
                      </div>
                    ))}
                  </>
                ) : (
                  <div className="space-y-3">
                    {Array.from({ length: 5 }).map((_, i) => (
                      <Skeleton key={i} className="h-6 w-full" />
                    ))}
                  </div>
                )}
              </CardContent>
            </Card>

            <div className="space-y-4">
              {overrideFields.map((field) => (
                <div key={field} className="space-y-2">
                  <Label>{field}</Label>
                  <Input
                    type="number"
                    placeholder={t("tenants.inherit")}
                    value={overrideForm[field]}
                    onChange={(e) => setOverrideForm((prev) => ({ ...prev, [field]: e.target.value }))}
                  />
                </div>
              ))}
            </div>
          </div>

          <DialogFooter>
            <Button variant="outline" onClick={() => setOverrideOpen(false)}>
              {t("tenants.close")}
            </Button>
            <Button onClick={handleSaveOverrides} disabled={overrideSaving || !overrideTenant}>
              {overrideSaving ? t("tenants.saving") : t("tenants.saveOverrides")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
