"use client";

import { useState, type Dispatch, type SetStateAction } from "react";
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
import { TableCell, TableRow } from "@/components/ui/table";
import { DataTable } from "@/components/crud/data-table";
import { DialogTrigger } from "@/components/ui/dialog";
import { FormDialog } from "@/components/crud/form-dialog";
import { listPlans, createPlan, deletePlan, updatePlan } from "@/lib/api";
import type { Plan } from "@/lib/types";
import { Plus, Trash2, CreditCard, Pencil } from "lucide-react";
import { toast } from "sonner";
import { formatDistanceToNow } from "date-fns";
import { useI18n } from "@/lib/i18n";
import { safeConfirm } from "@/lib/utils";
import { useCRUDPage } from "@/hooks/use-crud-page";

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

  const { data: plansRes, isLoading: loading, mutate: mutatePlans } = useCRUDPage(
    "plans",
    () => listPlans(),
    "plans.loadFailed",
  );
  const plans = plansRes?.data ?? [];
  const total = plans.length;

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
      mutatePlans();
    } catch (e: unknown) {
      const err = e as { error?: { message?: string } };
      toast.error(err?.error?.message || t("plans.createFailed"));
    } finally {
      setCreating(false);
    }
  };

  const handleDelete = async (id: string) => {
    if (!safeConfirm(t("plans.confirmDelete"))) return;
    try {
      await deletePlan(id);
      toast.success(t("plans.deleted"));
      mutatePlans();
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
      mutatePlans();
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
          <FormDialog
            open={dialogOpen}
            onOpenChange={setDialogOpen}
            trigger={
              <DialogTrigger render={<Button size="sm" className="gap-1.5" />}>
                <Plus className="h-3.5 w-3.5" />
                {t("plans.createPlan")}
              </DialogTrigger>
            }
            title={t("plans.createTitle")}
            description={t("plans.createDesc")}
            footer={
              <Button
                onClick={handleCreate}
                disabled={creating || !form.name.trim()}
              >
                {creating ? t("plans.creating") : t("plans.create")}
              </Button>
            }
          >
            <PlanFormFields fields={fields} value={form} onChange={setForm} />
          </FormDialog>
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
            <DataTable
              loading={loading}
              isEmpty={plans.length === 0}
              emptyIcon={CreditCard}
              emptyText={t("plans.noPlans")}
              skeletonRows={2}
              columns={[
                { key: "name", header: t("tenants.name") },
                { key: "domains", header: t("admin.domains"), className: "text-right" },
                { key: "mbPerDomain", header: t("plans.colMbPerDomain"), className: "text-right" },
                { key: "msgPerMb", header: t("plans.colMsgPerMb"), className: "text-right" },
                { key: "retention", header: t("plans.retention"), className: "text-right" },
                { key: "rpm", header: t("plans.colRpm"), className: "text-right" },
                { key: "daily", header: t("plans.colDaily"), className: "text-right" },
                { key: "created", header: t("plans.created") },
                { key: "actions", className: "w-20" },
              ]}
            >
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
            </DataTable>
          </CardContent>
        </Card>
      </div>

      {editingPlan && (
        <FormDialog
          open={editOpen}
          onOpenChange={setEditOpen}
          title={t("plans.editPlan")}
          description={t("plans.editDesc")}
          footer={
            <Button
              onClick={handleEdit}
              disabled={saving || !editForm.name.trim()}
            >
              {saving ? t("plans.saving") : t("plans.save")}
            </Button>
          }
        >
          <PlanFormFields fields={fields} value={editForm} onChange={setEditForm} />
        </FormDialog>
      )}
    </div>
  );
}

function PlanFormFields({
  fields,
  value,
  onChange,
}: {
  fields: { key: keyof PlanFormData; label: string; type?: string }[];
  value: PlanFormData;
  onChange: Dispatch<SetStateAction<PlanFormData>>;
}) {
  return (
    <div className="space-y-3 py-4 max-h-[60vh] overflow-y-auto">
      {fields.map((f) => (
        <div key={f.key} className="space-y-1.5">
          <Label className="text-xs">{f.label}</Label>
          <Input
            type={f.type || "text"}
            value={value[f.key]}
            onChange={(e) =>
              onChange((prev) => ({ ...prev, [f.key]: e.target.value }))
            }
            placeholder={f.label}
          />
        </div>
      ))}
    </div>
  );
}
