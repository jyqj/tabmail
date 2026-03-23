"use client";

import { useState, useEffect, useCallback } from "react";
import { PageHeader } from "@/components/layout/page-header";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
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
import { Skeleton } from "@/components/ui/skeleton";
import { listPlans, createPlan, deletePlan, updatePlan } from "@/lib/api";
import type { Plan } from "@/lib/types";
import { Plus, Trash2, CreditCard, Pencil } from "lucide-react";
import { toast } from "sonner";
import { formatDistanceToNow } from "date-fns";
import { useI18n } from "@/lib/i18n";

interface PlanFormData {
  name: string;
  max_domains: string;
  max_mailboxes_per_domain: string;
  max_messages_per_mailbox: string;
  max_message_bytes: string;
  retention_hours: string;
  rpm_limit: string;
  daily_quota: string;
}

const defaultForm: PlanFormData = {
  name: "",
  max_domains: "5",
  max_mailboxes_per_domain: "100",
  max_messages_per_mailbox: "200",
  max_message_bytes: "10485760",
  retention_hours: "48",
  rpm_limit: "60",
  daily_quota: "1000",
};

export default function PlansPage() {
  const { t } = useI18n();
  const [plans, setPlans] = useState<Plan[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(true);
  const [dialogOpen, setDialogOpen] = useState(false);
  const [creating, setCreating] = useState(false);
  const [form, setForm] = useState<PlanFormData>(defaultForm);

  const [editOpen, setEditOpen] = useState(false);
  const [editingPlan, setEditingPlan] = useState<Plan | null>(null);
  const [editForm, setEditForm] = useState<PlanFormData>(defaultForm);
  const [saving, setSaving] = useState(false);

  const fields: { key: keyof PlanFormData; label: string; type?: string }[] = [
    { key: "name", label: t("plans.name") },
    { key: "max_domains", label: t("plans.maxDomains"), type: "number" },
    { key: "max_mailboxes_per_domain", label: t("plans.maxMailboxesPerDomain"), type: "number" },
    { key: "max_messages_per_mailbox", label: t("plans.maxMessagesPerMailbox"), type: "number" },
    { key: "max_message_bytes", label: t("plans.maxMessageBytes"), type: "number" },
    { key: "retention_hours", label: t("plans.retentionHours"), type: "number" },
    { key: "rpm_limit", label: t("plans.rpmLimit"), type: "number" },
    { key: "daily_quota", label: t("plans.dailyQuota"), type: "number" },
  ];

  const fetchPlans = useCallback(async () => {
    try {
      const res = await listPlans();
      setPlans(res.data);
      setTotal(res.data.length);
    } catch {
      toast.error(t("plans.loadFailed"));
    } finally {
      setLoading(false);
    }
  }, [t]);

  useEffect(() => {
    fetchPlans();
  }, [fetchPlans]);

  const handleCreate = async () => {
    if (!form.name.trim()) return;
    setCreating(true);
    try {
      await createPlan({
        name: form.name.trim(),
        max_domains: Number(form.max_domains),
        max_mailboxes_per_domain: Number(form.max_mailboxes_per_domain),
        max_messages_per_mailbox: Number(form.max_messages_per_mailbox),
        max_message_bytes: Number(form.max_message_bytes),
        retention_hours: Number(form.retention_hours),
        rpm_limit: Number(form.rpm_limit),
        daily_quota: Number(form.daily_quota),
      });
      setForm(defaultForm);
      setDialogOpen(false);
      toast.success(t("plans.planCreated"));
      fetchPlans();
    } catch (e: unknown) {
      const err = e as { error?: { message?: string } };
      toast.error(err?.error?.message || t("plans.createFailed"));
    } finally {
      setCreating(false);
    }
  };

  const handleDelete = async (id: string) => {
    try {
      await deletePlan(id);
      toast.success(t("plans.deleted"));
      fetchPlans();
    } catch {
      toast.error(t("plans.deleteFailed"));
    }
  };

  const openEdit = (plan: Plan) => {
    setEditingPlan(plan);
    setEditForm({
      name: plan.name,
      max_domains: String(plan.max_domains),
      max_mailboxes_per_domain: String(plan.max_mailboxes_per_domain),
      max_messages_per_mailbox: String(plan.max_messages_per_mailbox),
      max_message_bytes: String(plan.max_message_bytes),
      retention_hours: String(plan.retention_hours),
      rpm_limit: String(plan.rpm_limit),
      daily_quota: String(plan.daily_quota),
    });
    setEditOpen(true);
  };

  const handleEdit = async () => {
    if (!editingPlan || !editForm.name.trim()) return;
    setSaving(true);
    try {
      await updatePlan(editingPlan.id, {
        name: editForm.name.trim(),
        max_domains: Number(editForm.max_domains),
        max_mailboxes_per_domain: Number(editForm.max_mailboxes_per_domain),
        max_messages_per_mailbox: Number(editForm.max_messages_per_mailbox),
        max_message_bytes: Number(editForm.max_message_bytes),
        retention_hours: Number(editForm.retention_hours),
        rpm_limit: Number(editForm.rpm_limit),
        daily_quota: Number(editForm.daily_quota),
      });
      setEditOpen(false);
      setEditingPlan(null);
      toast.success(t("plans.updated"));
      fetchPlans();
    } catch (e: unknown) {
      const err = e as { error?: { message?: string } };
      toast.error(err?.error?.message || t("plans.updateFailed"));
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="flex flex-col">
      <PageHeader
        title={t("plans.title")}
        description={t("plans.count", { count: total })}
        actions={
          <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
            <DialogTrigger render={<Button size="sm" className="gap-1.5" />}>
              <Plus className="h-3.5 w-3.5" />
              {t("plans.createPlan")}
            </DialogTrigger>
            <DialogContent className="sm:max-w-md">
              <DialogHeader>
                <DialogTitle>{t("plans.createTitle")}</DialogTitle>
                <DialogDescription>
                  {t("plans.createDesc")}
                </DialogDescription>
              </DialogHeader>
              <div className="space-y-3 py-4 max-h-[60vh] overflow-y-auto">
                {fields.map((f) => (
                  <div key={f.key} className="space-y-1.5">
                    <Label className="text-xs">{f.label}</Label>
                    <Input
                      type={f.type || "text"}
                      value={form[f.key]}
                      onChange={(e) =>
                        setForm((prev) => ({ ...prev, [f.key]: e.target.value }))
                      }
                      placeholder={f.label}
                    />
                  </div>
                ))}
              </div>
              <DialogFooter>
                <Button
                  onClick={handleCreate}
                  disabled={creating || !form.name.trim()}
                >
                  {creating ? t("plans.creating") : t("plans.create")}
                </Button>
              </DialogFooter>
            </DialogContent>
          </Dialog>
        }
      />

      <div className="p-4">
        <Card>
          <CardHeader className="pb-3">
            <CardTitle className="text-base">{t("plans.allPlans")}</CardTitle>
            <CardDescription>
              {t("plans.allPlansDesc")}
            </CardDescription>
          </CardHeader>
          <CardContent>
            {loading ? (
              <div className="space-y-3">
                {Array.from({ length: 2 }).map((_, i) => (
                  <Skeleton key={i} className="h-12 w-full" />
                ))}
              </div>
            ) : plans.length === 0 ? (
              <div className="text-center py-12 text-muted-foreground">
                <CreditCard className="h-10 w-10 mx-auto mb-3 opacity-30" />
                <p className="text-sm">{t("plans.noPlans")}</p>
              </div>
            ) : (
              <div className="overflow-x-auto">
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>{t("tenants.name")}</TableHead>
                      <TableHead className="text-right">{t("admin.domains")}</TableHead>
                      <TableHead className="text-right">MB/Domain</TableHead>
                      <TableHead className="text-right">Msg/MB</TableHead>
                      <TableHead className="text-right">{t("plans.retention")}</TableHead>
                      <TableHead className="text-right">RPM</TableHead>
                      <TableHead className="text-right">Daily</TableHead>
                      <TableHead>{t("plans.created")}</TableHead>
                      <TableHead className="w-20" />
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {plans.map((p) => (
                      <TableRow key={p.id}>
                        <TableCell className="font-medium">{p.name}</TableCell>
                        <TableCell className="text-right tabular-nums">
                          {p.max_domains}
                        </TableCell>
                        <TableCell className="text-right tabular-nums">
                          {p.max_mailboxes_per_domain}
                        </TableCell>
                        <TableCell className="text-right tabular-nums">
                          {p.max_messages_per_mailbox}
                        </TableCell>
                        <TableCell className="text-right tabular-nums">
                          {p.retention_hours}h
                        </TableCell>
                        <TableCell className="text-right tabular-nums">
                          {p.rpm_limit}
                        </TableCell>
                        <TableCell className="text-right tabular-nums">
                          {p.daily_quota}
                        </TableCell>
                        <TableCell className="text-sm text-muted-foreground">
                          {formatDistanceToNow(new Date(p.created_at), {
                            addSuffix: true,
                          })}
                        </TableCell>
                        <TableCell>
                          <div className="flex items-center gap-1">
                            <Button
                              variant="ghost"
                              size="icon"
                              className="h-8 w-8"
                              onClick={() => openEdit(p)}
                            >
                              <Pencil className="h-4 w-4" />
                            </Button>
                            <Button
                              variant="ghost"
                              size="icon"
                              className="h-8 w-8 text-destructive hover:text-destructive hover:bg-destructive/10"
                              onClick={() => handleDelete(p.id)}
                            >
                              <Trash2 className="h-4 w-4" />
                            </Button>
                          </div>
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </div>
            )}
          </CardContent>
        </Card>
      </div>

      {editingPlan && (
        <Dialog open={editOpen} onOpenChange={setEditOpen}>
          <DialogContent className="sm:max-w-md">
            <DialogHeader>
              <DialogTitle>{t("plans.editPlan")}</DialogTitle>
              <DialogDescription>
                {t("plans.editDesc")}
              </DialogDescription>
            </DialogHeader>
            <div className="space-y-3 py-4 max-h-[60vh] overflow-y-auto">
              {fields.map((f) => (
                <div key={f.key} className="space-y-1.5">
                  <Label className="text-xs">{f.label}</Label>
                  <Input
                    type={f.type || "text"}
                    value={editForm[f.key]}
                    onChange={(e) =>
                      setEditForm((prev) => ({ ...prev, [f.key]: e.target.value }))
                    }
                    placeholder={f.label}
                  />
                </div>
              ))}
            </div>
            <DialogFooter>
              <Button
                onClick={handleEdit}
                disabled={saving || !editForm.name.trim()}
              >
                {saving ? t("plans.saving") : t("plans.save")}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      )}
    </div>
  );
}
