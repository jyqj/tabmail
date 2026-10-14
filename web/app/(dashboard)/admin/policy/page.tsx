"use client";

import { useEffect, useRef, useState, type ReactNode } from "react";
import { toast } from "sonner";
import { SlidersHorizontal } from "lucide-react";

import { getSMTPPolicy, updateSMTPPolicy } from "@/lib/api";
import type { SMTPPolicyInput } from "@/lib/types";
import { useI18n } from "@/lib/i18n";
import { useAPI } from "@/hooks/use-api";
import { sessionScope, useSessionScope } from "@/lib/session";
import { useText } from "@/components/company/common";
import { PageHeader } from "@/components/layout/page-header";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { Skeleton } from "@/components/ui/skeleton";

function parseList(input: string): string[] {
  return input
    .split(",")
    .map((v) => v.trim())
    .filter(Boolean);
}

type PolicyForm = {
  default_accept: boolean;
  accept_domains: string;
  reject_domains: string;
  default_store: boolean;
  store_domains: string;
  discard_domains: string;
  reject_origin_domains: string;
};

function policyForm(value: unknown): PolicyForm {
  if (!value || typeof value !== "object") throw new Error("Invalid SMTP policy snapshot");
  const policy = value as Record<string, unknown>;
  if (typeof policy.default_accept !== "boolean" || typeof policy.default_store !== "boolean")
    throw new Error("Invalid SMTP policy snapshot");
  const list = (key: string) => {
    const values = policy[key];
    // Go encodes a nil list as null. Missing or wrongly typed fields are not
    // an authoritative empty policy and must not enable a replacement write.
    if (values === null) return "";
    if (!Array.isArray(values) || !values.every(value => typeof value === "string"))
      throw new Error("Invalid SMTP policy snapshot");
    return values.join(", ");
  };
  return {
    default_accept: policy.default_accept,
    accept_domains: list("accept_domains"),
    reject_domains: list("reject_domains"),
    default_store: policy.default_store,
    store_domains: list("store_domains"),
    discard_domains: list("discard_domains"),
    reject_origin_domains: list("reject_origin_domains"),
  };
}

export default function AdminPolicyPage() {
  const scope = useSessionScope();
  return <PolicyEditor key={scope} scope={scope} />;
}

function PolicyEditor({ scope }: { scope: string }) {
  const { t } = useI18n();
  const c = useText();
  const mounted = useRef(true);
  const operation = useRef(false);
  const read = useRef({ generation: 0, pending: false });
  const current = () => mounted.current && scope === sessionScope();
  useEffect(() => {
    mounted.current = true;
    return () => { mounted.current = false; };
  }, []);
  const policy = useAPI("smtp-policy", async () => {
    const generation = ++read.current.generation;
    read.current.pending = true;
    try { return policyForm((await getSMTPPolicy()).data); }
    finally { if (generation === read.current.generation) read.current.pending = false; }
  });
  const [saving, setSaving] = useState(false);
  // Preserve only explicit local intentions across a successful refresh. Fields
  // the user never changed must come from the latest complete server snapshot.
  const [edits, setEdits] = useState<Partial<PolicyForm>>({});
  const form = policy.data ? { ...policy.data, ...edits } : null;
  const blocked = !form || Boolean(policy.error) || policy.isLoading || policy.isValidating || saving;
  function change<K extends keyof PolicyForm>(key: K, value: PolicyForm[K]) {
    if (!current() || blocked || operation.current || read.current.pending) return;
    setEdits(previous => ({ ...previous, [key]: value }));
  }
  const reload = async () => {
    if (!current() || operation.current || read.current.pending) return;
    // The SWR error remains visible; retry is a read, never an automatic PATCH.
    await policy.mutate().catch(() => {});
  };

  const handleSave = async () => {
    if (!current() || blocked || !form || operation.current || read.current.pending) return;
    operation.current = true;
    setSaving(true);
    try {
      const payload: SMTPPolicyInput = {
        default_accept: form.default_accept,
        accept_domains: parseList(form.accept_domains),
        reject_domains: parseList(form.reject_domains),
        default_store: form.default_store,
        store_domains: parseList(form.store_domains),
        discard_domains: parseList(form.discard_domains),
        reject_origin_domains: parseList(form.reject_origin_domains),
      };
      await updateSMTPPolicy(payload);
      if (!current()) return;
      // The acknowledged replacement is the new baseline. Then independently
      // re-read; a failed read blocks further writes without calling this PATCH
      // a failure or restoring the initial defaults.
      await policy.mutate(form, { revalidate: false });
      if (!current()) return;
      setEdits({});
      toast.success(t("policy.updated"));
      await policy.mutate().catch(() => {});
    } catch (e: unknown) {
      if (!current()) return;
      const err = e as { error?: { message?: string } };
      toast.error(err?.error?.message || t("policy.updateFailed"));
    } finally {
      operation.current = false;
      if (current()) setSaving(false);
    }
  };

  return (
    <div className="flex flex-col">
      <PageHeader
        title={t("policy.title")}
        description={t("policy.desc")}
        actions={
          <Button onClick={handleSave} disabled={blocked}>
            {saving ? t("policy.saving") : t("policy.save")}
          </Button>
        }
      />

      <div className="space-y-4 p-4">
        <div className="space-y-2">
          {policy.error && <p role="alert" className="text-sm text-destructive">{t("policy.loadFailed")}</p>}
          <Button variant="outline" onClick={() => void reload()} disabled={saving || policy.isValidating}>
            {policy.error ? c("重试加载", "Retry loading") : c("重新加载策略", "Reload policy")}
          </Button>
        </div>
        <Card className="border-primary/10 bg-[radial-gradient(circle_at_top,rgba(99,102,241,0.08),transparent_35%),var(--card)]">
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <SlidersHorizontal className="h-4 w-4 text-primary" />
              {t("policy.deliveryRules")}
            </CardTitle>
            <CardDescription>
              {t("policy.deliveryRulesDesc")}
            </CardDescription>
          </CardHeader>
          <CardContent className="grid gap-6 lg:grid-cols-2">
            {!form ? (policy.isLoading || policy.isValidating ? (
              <div className="space-y-4 lg:col-span-2">
                {Array.from({ length: 6 }).map((_, i) => (
                  <Skeleton key={i} className="h-12 w-full" />
                ))}
              </div>
            ) : <p role="status">{c("尚未读取当前策略，暂时无法编辑。", "No current policy snapshot is available for editing.")}</p>) : (
              <>
                <PolicyBlock
                  title={t("policy.recipientAcceptance")}
                  description={t("policy.recipientAcceptanceDesc")}
                >
                  <ToggleRow
                    label={t("policy.defaultAccept")}
                    checked={form.default_accept}
                    disabled={blocked}
                    onCheckedChange={(checked) => change("default_accept", checked)}
                  />
                  <ListField
                    label={t("policy.acceptDomains")}
                    placeholder={t("policy.acceptPlaceholder")}
                    value={form.accept_domains}
                    disabled={blocked}
                    onChange={(value) => change("accept_domains", value)}
                  />
                  <ListField
                    label={t("policy.rejectDomains")}
                    placeholder={t("policy.rejectPlaceholder")}
                    value={form.reject_domains}
                    disabled={blocked}
                    onChange={(value) => change("reject_domains", value)}
                  />
                </PolicyBlock>

                <PolicyBlock
                  title={t("policy.storagePolicy")}
                  description={t("policy.storagePolicyDesc")}
                >
                  <ToggleRow
                    label={t("policy.defaultStore")}
                    checked={form.default_store}
                    disabled={blocked}
                    onCheckedChange={(checked) => change("default_store", checked)}
                  />
                  <ListField
                    label={t("policy.storeDomains")}
                    placeholder={t("policy.storePlaceholder")}
                    value={form.store_domains}
                    disabled={blocked}
                    onChange={(value) => change("store_domains", value)}
                  />
                  <ListField
                    label={t("policy.discardDomains")}
                    placeholder={t("policy.discardPlaceholder")}
                    value={form.discard_domains}
                    disabled={blocked}
                    onChange={(value) => change("discard_domains", value)}
                  />
                </PolicyBlock>

                <div className="lg:col-span-2">
                  <PolicyBlock
                    title={t("policy.originFiltering")}
                    description={t("policy.originFilteringDesc")}
                  >
                    <ListField
                      label={t("policy.rejectOriginDomains")}
                      placeholder={t("policy.originPlaceholder")}
                      value={form.reject_origin_domains}
                      disabled={blocked}
                      onChange={(value) => change("reject_origin_domains", value)}
                    />
                  </PolicyBlock>
                </div>
              </>
            )}
          </CardContent>
        </Card>
      </div>
    </div>
  );
}

function PolicyBlock({
  title,
  description,
  children,
}: {
  title: string;
  description: string;
  children: ReactNode;
}) {
  return (
    <div className="rounded-2xl border bg-background/85 p-4 shadow-sm">
      <div className="mb-4">
        <div className="font-medium">{title}</div>
        <p className="mt-1 text-sm text-muted-foreground">{description}</p>
      </div>
      <div className="space-y-4">{children}</div>
    </div>
  );
}

function ToggleRow({
  label,
  checked,
  disabled,
  onCheckedChange,
}: {
  label: string;
  checked: boolean;
  disabled: boolean;
  onCheckedChange: (checked: boolean) => void;
}) {
  return (
    <div className="flex items-center justify-between rounded-xl border bg-background px-4 py-3">
      <Label>{label}</Label>
      <Switch checked={checked} disabled={disabled} onCheckedChange={onCheckedChange} />
    </div>
  );
}

function ListField({
  label,
  placeholder,
  value,
  disabled,
  onChange,
}: {
  label: string;
  placeholder: string;
  value: string;
  disabled: boolean;
  onChange: (value: string) => void;
}) {
  return (
    <div className="space-y-2">
      <Label>{label}</Label>
      <Input placeholder={placeholder} value={value} disabled={disabled} onChange={(e) => onChange(e.target.value)} />
    </div>
  );
}
