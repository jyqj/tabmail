"use client";

import { useId, useLayoutEffect, useRef, useState } from "react";
import { Gauge } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Skeleton } from "@/components/ui/skeleton";
import { useAPI } from "@/hooks/use-api";
import { getTenantConfig, getTenantOverrides, updateTenantOverrides } from "@/lib/api";
import { useI18n } from "@/lib/i18n";
import { sessionScope } from "@/lib/session";
import type { Tenant, TenantOverrideInput } from "@/lib/types";

const fields = ["max_domains", "max_mailboxes_per_domain", "max_messages_per_mailbox", "max_message_bytes", "retention_hours", "rpm_limit", "daily_quota"] as const;
type Field = typeof fields[number];

export function TenantOverridesDialog({ tenant, onClose }: { tenant: Tenant; onClose: () => void }) {
  const { t } = useI18n();
  const instance = useId();
  const [scope] = useState(sessionScope);
  const lifetime = useRef<object | null>(null);
  const operation = useRef(false);
  const read = useRef({ pending: false, generation: 0 });
  const [saving, setSaving] = useState(false);
  // Store only actual edits. Untouched fields follow each current raw snapshot,
  // so a refresh cannot turn an old inherited/default value into an override.
  const [edits, setEdits] = useState<Partial<Record<Field, string>>>({});
  const [invalid, setInvalid] = useState<Field[]>([]);
  const raw = useAPI(["tenant-overrides", tenant.id, instance], async () => {
    const generation = ++read.current.generation;
    read.current.pending = true;
    try { return await getTenantOverrides(tenant.id); }
    finally { if (generation === read.current.generation) read.current.pending = false; }
  }, { shouldRetryOnError: false });
  const effective = useAPI(["tenant-effective-config", tenant.id, instance], () => getTenantConfig(tenant.id), { shouldRetryOnError: false });
  const snapshot = raw.data?.data;
  const effectiveConfig = effective.data?.data;
  const reading = raw.isLoading || raw.isValidating;
  const ready = !!snapshot && !raw.error && !reading;

  useLayoutEffect(() => {
    lifetime.current = {};
    return () => { lifetime.current = null; };
  }, []);
  const owns = (owner: object) => lifetime.current === owner && scope === sessionScope();
  const current = () => lifetime.current !== null && scope === sessionScope();
  const value = (key: Field) => edits[key] ?? (snapshot?.[key] == null ? "" : String(snapshot[key]));
  const change = (key: Field, text: string) => {
    if (!current() || operation.current || !snapshot) return;
    const reverted = text === (snapshot[key] === null ? "" : String(snapshot[key]));
    setEdits(previous => {
      const next = { ...previous };
      if (reverted) delete next[key]; else next[key] = text;
      return next;
    });
    setInvalid(previous => previous.filter(field => field !== key));
  };
  const close = () => { lifetime.current = null; onClose(); };
  const reload = () => {
    if (!current() || operation.current || read.current.pending) return;
    void raw.mutate().catch(() => undefined);
  };
  const save = async () => {
    const owner = lifetime.current;
    if (!owner || !owns(owner) || operation.current || read.current.pending || !ready) return;
    const body: TenantOverrideInput = {};
    const errors: Field[] = [];
    for (const key of fields) {
      const text = value(key).trim();
      if (text === "") { body[key] = null; continue; }
      const number = Number(text);
      const [mantissa, exponent = "0"] = text.toLowerCase().split("e");
      const fractionLength = (mantissa.split(".")[1] ?? "").length - Number(exponent);
      const fractional = fractionLength > 0 && /[1-9]/.test(mantissa.replace(/[+.\-]/g, "").slice(-fractionLength));
      if (!/^[+-]?(?:\d+\.?\d*|\.\d+)(?:e[+-]?\d+)?$/i.test(text) || !Number.isInteger(number) || fractional || number < -2147483648 || number > 2147483647) errors.push(key);
      else body[key] = number;
    }
    setInvalid(errors);
    if (errors.length) return;
    operation.current = true;
    setSaving(true);
    try {
      try { await updateTenantOverrides(tenant.id, body); }
      catch (error: unknown) {
        if (owns(owner)) toast.error((error as { error?: { message?: string } })?.error?.message || t("tenants.overridesUpdateFailed"));
        return;
      }
      if (!owns(owner)) return;
      // PATCH replaces all seven values. Its legacy response omits nulls; use
      // the acknowledged command, then independently reload the raw snapshot.
      await raw.mutate({ data: { tenant_id: tenant.id, ...body } as NonNullable<typeof snapshot> }, { revalidate: false });
      if (!owns(owner)) return;
      setEdits({});
      toast.success(t("tenants.overridesUpdated"));
      // Read errors stay visible and cannot turn a committed write into a
      // failed PATCH, trigger a replay, or discard the acknowledged values.
      await Promise.allSettled([raw.mutate(), effective.mutate()]);
    } finally {
      if (owns(owner)) { operation.current = false; setSaving(false); }
    }
  };

  return <Dialog open onOpenChange={open => { if (!open) close(); }}>
    <DialogContent className="sm:max-w-2xl">
      <DialogHeader>
        <DialogTitle>{t("tenants.overridesTitle")}</DialogTitle>
        <DialogDescription>{t("tenants.overridesDesc", { name: tenant.name })}</DialogDescription>
      </DialogHeader>
      {raw.error && <div role="alert" className="space-y-2 rounded-lg border border-destructive p-3 text-sm">
        <p>{t("tenants.configLoadFailed")}</p>
        <Button variant="outline" size="sm" disabled={saving || reading} onClick={reload}>{t("error.retry")}</Button>
      </div>}
      <div className="grid gap-6 py-4 lg:grid-cols-[0.9fr_1.1fr]">
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2 text-base"><Gauge className="h-4 w-4 text-primary" />{t("tenants.effectiveConfig")}</CardTitle>
            <CardDescription>{t("tenants.effectiveConfigDesc")}</CardDescription>
          </CardHeader>
          <CardContent className="space-y-3">
            {effective.error ? <div role="alert" className="space-y-2 text-sm">
              <p>{t("tenants.configLoadFailed")}</p>
              <Button variant="outline" size="sm" disabled={saving || effective.isValidating} onClick={() => {
                if (current() && !operation.current) void effective.mutate().catch(() => undefined);
              }}>{t("error.retry")}</Button>
            </div> : effectiveConfig ? fields.map(key => <div key={key} className="flex items-center justify-between gap-3 text-sm">
              <span className="text-muted-foreground">{key}</span><span className="font-medium tabular-nums">{String(effectiveConfig[key])}</span>
            </div>) : <Skeleton className="h-32 w-full" />}
          </CardContent>
        </Card>
        <div className="space-y-4">
          {fields.map(key => <div key={key} className="space-y-2">
            <Label htmlFor={`${instance}-${key}`}>{key}</Label>
            <Input id={`${instance}-${key}`} type="number" placeholder={t("tenants.inherit")} value={value(key)}
              disabled={saving || !snapshot} aria-invalid={invalid.includes(key)}
              aria-describedby={invalid.includes(key) ? `${instance}-number-error` : undefined}
              onChange={event => change(key, event.target.value)} />
          </div>)}
          {invalid.length > 0 && <p id={`${instance}-number-error`} role="alert" className="text-sm text-destructive">{t("plans.integerRequired")}</p>}
        </div>
      </div>
      <DialogFooter>
        <Button variant="outline" onClick={close}>{t("tenants.close")}</Button>
        <Button onClick={save} disabled={saving || !ready}>{saving ? t("tenants.saving") : t("tenants.saveOverrides")}</Button>
      </DialogFooter>
    </DialogContent>
  </Dialog>;
}
