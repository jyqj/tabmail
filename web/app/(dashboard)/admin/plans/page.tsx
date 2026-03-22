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
import { listPlans, createPlan, deletePlan } from "@/lib/api";
import type { Plan } from "@/lib/types";
import { Plus, Trash2, CreditCard } from "lucide-react";
import { toast } from "sonner";
import { formatDistanceToNow } from "date-fns";

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

const fields: { key: keyof PlanFormData; label: string; type?: string }[] = [
  { key: "name", label: "Plan Name" },
  { key: "max_domains", label: "Max Domains", type: "number" },
  { key: "max_mailboxes_per_domain", label: "Max Mailboxes / Domain", type: "number" },
  { key: "max_messages_per_mailbox", label: "Max Messages / Mailbox", type: "number" },
  { key: "max_message_bytes", label: "Max Message Bytes", type: "number" },
  { key: "retention_hours", label: "Retention (hours)", type: "number" },
  { key: "rpm_limit", label: "RPM Limit", type: "number" },
  { key: "daily_quota", label: "Daily Quota", type: "number" },
];

export default function PlansPage() {
  const [plans, setPlans] = useState<Plan[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(true);
  const [dialogOpen, setDialogOpen] = useState(false);
  const [creating, setCreating] = useState(false);
  const [form, setForm] = useState<PlanFormData>(defaultForm);

  const fetchPlans = useCallback(async () => {
    try {
      const res = await listPlans();
      setPlans(res.data);
      setTotal(res.data.length);
    } catch {
      toast.error("Failed to load plans");
    } finally {
      setLoading(false);
    }
  }, []);

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
      toast.success("Plan created");
      fetchPlans();
    } catch (e: unknown) {
      const err = e as { error?: { message?: string } };
      toast.error(err?.error?.message || "Failed to create plan");
    } finally {
      setCreating(false);
    }
  };

  const handleDelete = async (id: string) => {
    try {
      await deletePlan(id);
      toast.success("Plan deleted");
      fetchPlans();
    } catch {
      toast.error("Failed to delete");
    }
  };

  return (
    <div className="flex flex-col">
      <PageHeader
        title="Plans"
        description={`${total} plan${total !== 1 ? "s" : ""}`}
        actions={
          <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
            <DialogTrigger render={<Button size="sm" className="gap-1.5" />}>
              <Plus className="h-3.5 w-3.5" />
              Create Plan
            </DialogTrigger>
            <DialogContent className="sm:max-w-md">
              <DialogHeader>
                <DialogTitle>Create Plan</DialogTitle>
                <DialogDescription>
                  Define default quotas and limits for tenants on this plan.
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
                  {creating ? "Creating..." : "Create"}
                </Button>
              </DialogFooter>
            </DialogContent>
          </Dialog>
        }
      />

      <div className="p-4">
        <Card>
          <CardHeader className="pb-3">
            <CardTitle className="text-base">All Plans</CardTitle>
            <CardDescription>
              Predefined quota templates for tenant assignment.
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
                <p className="text-sm">No plans yet</p>
              </div>
            ) : (
              <div className="overflow-x-auto">
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>Name</TableHead>
                      <TableHead className="text-right">Domains</TableHead>
                      <TableHead className="text-right">MB/Domain</TableHead>
                      <TableHead className="text-right">Msg/MB</TableHead>
                      <TableHead className="text-right">Retention</TableHead>
                      <TableHead className="text-right">RPM</TableHead>
                      <TableHead className="text-right">Daily</TableHead>
                      <TableHead>Created</TableHead>
                      <TableHead className="w-10" />
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
                          <Button
                            variant="ghost"
                            size="icon"
                            className="h-8 w-8 text-destructive hover:text-destructive hover:bg-destructive/10"
                            onClick={() => handleDelete(p.id)}
                          >
                            <Trash2 className="h-4 w-4" />
                          </Button>
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
    </div>
  );
}
