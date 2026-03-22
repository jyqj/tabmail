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

export default function TenantsPage() {
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
  const [overrideForm, setOverrideForm] = useState<Record<keyof TenantOverride, string>>({
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
      toast.error("Failed to load tenants");
    } finally {
      setLoading(false);
    }
  }, []);

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
      toast.success("Tenant created");
      fetchTenants();
    } catch (e: unknown) {
      const err = e as { error?: { message?: string } };
      toast.error(err?.error?.message || "Failed to create tenant");
    } finally {
      setCreating(false);
    }
  };

  const handleDelete = async (id: string) => {
    try {
      await deleteTenant(id);
      toast.success("Tenant deleted");
      fetchTenants();
    } catch {
      toast.error("Failed to delete");
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
      toast.error("Failed to load keys");
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
      toast.success("API key created");
    } catch {
      toast.error("Failed to create key");
    }
  };

  const handleRevokeKey = async (keyId: string) => {
    try {
      await revokeAPIKey(keysTenantId, keyId);
      setKeys((prev) => prev.filter((k) => k.id !== keyId));
      toast.success("Key revoked");
    } catch {
      toast.error("Failed to revoke");
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
      toast.error("Failed to load effective config");
    }
  };

  const handleSaveOverrides = async () => {
    if (!overrideTenant) return;
    const body: TenantOverride = Object.fromEntries(
      Object.entries(overrideForm).map(([key, value]) => [
        key,
        value.trim() === "" ? null : Number(value),
      ])
    ) as TenantOverride;
    setOverrideSaving(true);
    try {
      await updateTenantOverrides(overrideTenant.id, body);
      const res = await getTenantConfig(overrideTenant.id);
      setEffectiveConfig(res.data);
      toast.success("Tenant overrides updated");
    } catch (e: unknown) {
      const err = e as { error?: { message?: string } };
      toast.error(err?.error?.message || "Failed to update overrides");
    } finally {
      setOverrideSaving(false);
    }
  };

  return (
    <div className="flex flex-col">
      <PageHeader
        title="Tenants"
        description={`${total} tenant${total !== 1 ? "s" : ""}`}
        actions={
          <Dialog open={createOpen} onOpenChange={setCreateOpen}>
            <DialogTrigger render={<Button size="sm" className="gap-1.5" />}>
              <Plus className="h-3.5 w-3.5" />
              Create Tenant
            </DialogTrigger>
            <DialogContent className="sm:max-w-md">
              <DialogHeader>
                <DialogTitle>Create Tenant</DialogTitle>
                <DialogDescription>
                  Create a new tenant with a plan assignment.
                </DialogDescription>
              </DialogHeader>
              <div className="space-y-4 py-4">
                <div className="space-y-2">
                  <Label>Name</Label>
                  <Input
                    placeholder="Acme Corp"
                    value={newName}
                    onChange={(e) => setNewName(e.target.value)}
                  />
                </div>
                <div className="space-y-2">
                  <Label>Plan</Label>
                  <Select value={newPlanId} onValueChange={(v) => v && setNewPlanId(v)}>
                    <SelectTrigger>
                      <SelectValue placeholder="Select a plan" />
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
                  {creating ? "Creating..." : "Create"}
                </Button>
              </DialogFooter>
            </DialogContent>
          </Dialog>
        }
      />

      <div className="p-4 space-y-4">
        <Card>
          <CardHeader className="pb-3">
            <CardTitle className="text-base">All Tenants</CardTitle>
            <CardDescription>
              Manage tenants, their plans, and API keys.
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
                <p className="text-sm">No tenants</p>
              </div>
            ) : (
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Name</TableHead>
                    <TableHead>Plan</TableHead>
                    <TableHead>Role</TableHead>
                    <TableHead>Created</TableHead>
                    <TableHead className="w-10" />
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {tenants.map((t) => (
                    <TableRow key={t.id}>
                      <TableCell className="font-medium">{t.name}</TableCell>
                      <TableCell>
                        <Badge variant="secondary">{planName(t.plan_id)}</Badge>
                      </TableCell>
                      <TableCell>
                        {t.is_super ? (
                          <Badge className="gap-1 bg-amber-600 hover:bg-amber-700">
                            <Shield className="h-3 w-3" />
                            Super
                          </Badge>
                        ) : (
                          <Badge variant="outline">Tenant</Badge>
                        )}
                      </TableCell>
                      <TableCell className="text-sm text-muted-foreground">
                        {formatDistanceToNow(new Date(t.created_at), {
                          addSuffix: true,
                        })}
                      </TableCell>
                      <TableCell>
                        <DropdownMenu>
                          <DropdownMenuTrigger render={<Button variant="ghost" size="icon" className="h-8 w-8" />}>
                            <MoreHorizontal className="h-4 w-4" />
                          </DropdownMenuTrigger>
                          <DropdownMenuContent align="end">
                            <DropdownMenuItem onClick={() => openKeys(t.id)}>
                              <KeyRound className="h-4 w-4 mr-2" />
                              API Keys
                            </DropdownMenuItem>
                            <DropdownMenuItem onClick={() => openOverrides(t)}>
                              <SlidersHorizontal className="h-4 w-4 mr-2" />
                              Overrides
                            </DropdownMenuItem>
                            <DropdownMenuItem
                              onClick={() => {
                                navigator.clipboard.writeText(t.id);
                                toast.success("Tenant ID copied");
                              }}
                            >
                              <Copy className="h-4 w-4 mr-2" />
                              Copy ID
                            </DropdownMenuItem>
                            <DropdownMenuSeparator />
                            <DropdownMenuItem
                              onClick={() => handleDelete(t.id)}
                              className="text-destructive focus:text-destructive"
                              disabled={t.is_super}
                            >
                              <Trash2 className="h-4 w-4 mr-2" />
                              Delete
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
            <DialogTitle>API Keys</DialogTitle>
            <DialogDescription>
              Manage API keys for this tenant.
            </DialogDescription>
          </DialogHeader>

          {newKeyCreated && (
            <div className="rounded-lg border border-green-200 bg-green-50 dark:border-green-800 dark:bg-green-950 p-3">
              <p className="text-sm font-medium text-green-800 dark:text-green-200 mb-1">
                New key created (shown only once):
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
                    toast.success("Copied");
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
                No API keys
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
                      Scopes: {k.scopes.join(", ")}
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
              Generate Key
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={overrideOpen} onOpenChange={setOverrideOpen}>
        <DialogContent className="sm:max-w-2xl">
          <DialogHeader>
            <DialogTitle>Tenant Overrides</DialogTitle>
            <DialogDescription>
              Override plan defaults for {overrideTenant?.name ?? "this tenant"}.
              Leave a field empty to inherit from the assigned plan.
            </DialogDescription>
          </DialogHeader>

          <div className="grid gap-6 py-4 lg:grid-cols-[0.9fr_1.1fr]">
            <Card className="border-primary/10 bg-[radial-gradient(circle_at_top,rgba(99,102,241,0.08),transparent_35%),var(--card)]">
              <CardHeader>
                <CardTitle className="flex items-center gap-2 text-base">
                  <Gauge className="h-4 w-4 text-primary" />
                  Effective Config
                </CardTitle>
                <CardDescription>Resolved values after plan + override merge.</CardDescription>
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
              {(
                [
                  "max_domains",
                  "max_mailboxes_per_domain",
                  "max_messages_per_mailbox",
                  "max_message_bytes",
                  "retention_hours",
                  "rpm_limit",
                  "daily_quota",
                ] as const
              ).map((field) => (
                <div key={field} className="space-y-2">
                  <Label>{field}</Label>
                  <Input
                    type="number"
                    placeholder="inherit"
                    value={overrideForm[field]}
                    onChange={(e) => setOverrideForm((prev) => ({ ...prev, [field]: e.target.value }))}
                  />
                </div>
              ))}
            </div>
          </div>

          <DialogFooter>
            <Button variant="outline" onClick={() => setOverrideOpen(false)}>
              Close
            </Button>
            <Button onClick={handleSaveOverrides} disabled={overrideSaving || !overrideTenant}>
              {overrideSaving ? "Saving..." : "Save Overrides"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
