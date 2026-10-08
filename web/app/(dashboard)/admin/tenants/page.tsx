"use client";

import { useState, useEffect, useRef } from "react";
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
import { TableCell, TableRow } from "@/components/ui/table";
import { DataTable } from "@/components/crud/data-table";
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
import {
  listTenants,
  createTenant,
  deleteTenant,
  listPlans,
} from "@/lib/api";
import type { Tenant } from "@/lib/types";
import {
  Plus,
  MoreHorizontal,
  Trash2,
  KeyRound,
  Copy,
  Users,
  Shield,
  SlidersHorizontal,
} from "lucide-react";
import { toast } from "sonner";
import { formatDistanceToNow } from "date-fns";
import { useI18n } from "@/lib/i18n";
import { safeConfirm } from "@/lib/utils";
import { useAPI } from "@/hooks/use-api";
import { sessionScope, useSessionScope } from "@/lib/session";
import { TenantAPIKeysDialog } from "./api-keys-dialog";
import { TenantOverridesDialog } from "./overrides-dialog";

export default function TenantsPage() {
  const { t } = useI18n();

  const { data: tenantsRes, isLoading: tenantsLoading, error: tenantsError, mutate: mutateTenants } = useAPI(
    "tenants",
    () => listTenants(),
  );
  const { data: plansRes, isLoading: plansLoading, error: plansError } = useAPI(
    "plans-for-tenants",
    () => listPlans(),
  );

  const tenants = tenantsRes?.data ?? [];
  const plans = plansRes?.data ?? [];
  const total = tenants.length;
  const loading = tenantsLoading || plansLoading;

  useEffect(() => {
    if (tenantsError) toast.error(t("tenants.loadFailed"));
    if (plansError) toast.error(t("tenants.loadFailed"));
  }, [tenantsError, plansError, t]);

  const [createOpen, setCreateOpen] = useState(false);
  const [creating, setCreating] = useState(false);
  const [newName, setNewName] = useState("");
  const [newPlanId, setNewPlanId] = useState("");

  const scope = useSessionScope();
  const keysSequence = useRef(0);
  const [keysDialog, setKeysDialog] = useState<{
    tenantId: string; scope: string; instance: number;
  } | null>(null);

  const overrideSequence = useRef(0);
  const [overrideDialog, setOverrideDialog] = useState<{
    tenant: Tenant; scope: string; instance: number;
  } | null>(null);

  const handleCreate = async () => {
    if (!newName.trim() || !newPlanId) return;
    setCreating(true);
    try {
      await createTenant({ name: newName.trim(), plan_id: newPlanId });
      setNewName("");
      setNewPlanId("");
      setCreateOpen(false);
      toast.success(t("tenants.tenantCreated"));
      mutateTenants();
    } catch (e: unknown) {
      const err = e as { error?: { message?: string } };
      toast.error(err?.error?.message || t("tenants.createFailed"));
    } finally {
      setCreating(false);
    }
  };

  const handleDelete = async (id: string) => {
    if (!safeConfirm(t("tenants.confirmDelete"))) return;
    try {
      await deleteTenant(id);
      toast.success(t("tenants.tenantDeleted"));
      mutateTenants();
    } catch {
      toast.error(t("tenants.deleteFailed"));
    }
  };

  const openKeys = (tenantId: string) => {
    setKeysDialog({ tenantId, scope: sessionScope(), instance: ++keysSequence.current });
  };

  const planName = (id: string) => plans.find((p) => p.id === id)?.name ?? "—";

  const openOverrides = (tenant: Tenant) => {
    setOverrideDialog({ tenant, scope: sessionScope(), instance: ++overrideSequence.current });
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
            <DataTable
              loading={loading}
              isEmpty={tenants.length === 0}
              emptyIcon={Users}
              emptyText={t("tenants.noTenants")}
              columns={[
                { key: "name", header: t("tenants.name") },
                { key: "plan", header: t("tenants.plan") },
                { key: "role", header: t("tenants.role") },
                { key: "created", header: t("common.created") },
                { key: "actions", className: "w-10" },
              ]}
            >
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
            </DataTable>
          </CardContent>
        </Card>
      </div>

      {keysDialog && keysDialog.scope === scope && (
        <TenantAPIKeysDialog key={keysDialog.instance} tenantId={keysDialog.tenantId}
          onClose={() => setKeysDialog(null)} />
      )}

      {overrideDialog && overrideDialog.scope === scope && (
        <TenantOverridesDialog key={overrideDialog.instance} tenant={overrideDialog.tenant}
          onClose={() => setOverrideDialog(null)} />
      )}
    </div>
  );
}
