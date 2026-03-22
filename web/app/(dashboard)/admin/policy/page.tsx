"use client";

import { useEffect, useState, type ReactNode } from "react";
import { toast } from "sonner";
import { SlidersHorizontal } from "lucide-react";

import { getSMTPPolicy, updateSMTPPolicy } from "@/lib/api";
import type { SMTPPolicy } from "@/lib/types";
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

export default function AdminPolicyPage() {
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [form, setForm] = useState({
    default_accept: true,
    accept_domains: "",
    reject_domains: "",
    default_store: true,
    store_domains: "",
    discard_domains: "",
    reject_origin_domains: "",
  });

  useEffect(() => {
    getSMTPPolicy()
      .then((res) => {
        const p = res.data;
        setForm({
          default_accept: p.default_accept,
          accept_domains: (p.accept_domains || []).join(", "),
          reject_domains: (p.reject_domains || []).join(", "),
          default_store: p.default_store,
          store_domains: (p.store_domains || []).join(", "),
          discard_domains: (p.discard_domains || []).join(", "),
          reject_origin_domains: (p.reject_origin_domains || []).join(", "),
        });
      })
      .catch(() => toast.error("Failed to load SMTP policy"))
      .finally(() => setLoading(false));
  }, []);

  const handleSave = async () => {
    setSaving(true);
    try {
      const payload: SMTPPolicy = {
        default_accept: form.default_accept,
        accept_domains: parseList(form.accept_domains),
        reject_domains: parseList(form.reject_domains),
        default_store: form.default_store,
        store_domains: parseList(form.store_domains),
        discard_domains: parseList(form.discard_domains),
        reject_origin_domains: parseList(form.reject_origin_domains),
      };
      await updateSMTPPolicy(payload);
      toast.success("SMTP policy updated");
    } catch (e: unknown) {
      const err = e as { error?: { message?: string } };
      toast.error(err?.error?.message || "Failed to update policy");
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="flex flex-col">
      <PageHeader
        title="SMTP Policy"
        description="Manage recipient acceptance and storage behavior without redeploying."
        actions={
          <Button onClick={handleSave} disabled={loading || saving}>
            {saving ? "Saving..." : "Save Policy"}
          </Button>
        }
      />

      <div className="space-y-4 p-4">
        <Card className="border-primary/10 bg-[radial-gradient(circle_at_top,rgba(99,102,241,0.08),transparent_35%),var(--card)]">
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <SlidersHorizontal className="h-4 w-4 text-primary" />
              Delivery rules
            </CardTitle>
            <CardDescription>
              Use wildcard patterns such as <code>*.example.com</code>. Empty lists mean no explicit overrides.
            </CardDescription>
          </CardHeader>
          <CardContent className="grid gap-6 lg:grid-cols-2">
            {loading ? (
              <div className="space-y-4 lg:col-span-2">
                {Array.from({ length: 6 }).map((_, i) => (
                  <Skeleton key={i} className="h-12 w-full" />
                ))}
              </div>
            ) : (
              <>
                <PolicyBlock
                  title="Recipient acceptance"
                  description="Accept everything by default, or switch to explicit allow-list mode."
                >
                  <ToggleRow
                    label="Default accept"
                    checked={form.default_accept}
                    onCheckedChange={(checked) => setForm((prev) => ({ ...prev, default_accept: checked }))}
                  />
                  <ListField
                    label="Accept domains"
                    placeholder="example.com, *.internal"
                    value={form.accept_domains}
                    onChange={(value) => setForm((prev) => ({ ...prev, accept_domains: value }))}
                  />
                  <ListField
                    label="Reject domains"
                    placeholder="gmail.com, *.blocked.test"
                    value={form.reject_domains}
                    onChange={(value) => setForm((prev) => ({ ...prev, reject_domains: value }))}
                  />
                </PolicyBlock>

                <PolicyBlock
                  title="Storage policy"
                  description="Accept a recipient but decide whether inbound mail should be stored."
                >
                  <ToggleRow
                    label="Default store"
                    checked={form.default_store}
                    onCheckedChange={(checked) => setForm((prev) => ({ ...prev, default_store: checked }))}
                  />
                  <ListField
                    label="Store domains"
                    placeholder="example.com, *.persisted"
                    value={form.store_domains}
                    onChange={(value) => setForm((prev) => ({ ...prev, store_domains: value }))}
                  />
                  <ListField
                    label="Discard domains"
                    placeholder="devnull.example.com"
                    value={form.discard_domains}
                    onChange={(value) => setForm((prev) => ({ ...prev, discard_domains: value }))}
                  />
                </PolicyBlock>

                <div className="lg:col-span-2">
                  <PolicyBlock
                    title="Origin filtering"
                    description="Reject inbound MAIL FROM domains before the message proceeds."
                  >
                    <ListField
                      label="Reject origin domains"
                      placeholder="spam.test, *.blocked.sender"
                      value={form.reject_origin_domains}
                      onChange={(value) => setForm((prev) => ({ ...prev, reject_origin_domains: value }))}
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
  onCheckedChange,
}: {
  label: string;
  checked: boolean;
  onCheckedChange: (checked: boolean) => void;
}) {
  return (
    <div className="flex items-center justify-between rounded-xl border bg-background px-4 py-3">
      <Label>{label}</Label>
      <Switch checked={checked} onCheckedChange={onCheckedChange} />
    </div>
  );
}

function ListField({
  label,
  placeholder,
  value,
  onChange,
}: {
  label: string;
  placeholder: string;
  value: string;
  onChange: (value: string) => void;
}) {
  return (
    <div className="space-y-2">
      <Label>{label}</Label>
      <Input placeholder={placeholder} value={value} onChange={(e) => onChange(e.target.value)} />
    </div>
  );
}
