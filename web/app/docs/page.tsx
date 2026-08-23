"use client";

import { useMemo, useState, type ReactNode } from "react";
import dynamic from "next/dynamic";
import { SiteHeader } from "@/components/site-header";
import { useI18n } from "@/lib/i18n";
import { getBaseUrl } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import {
  BookOpen, ExternalLink, Copy, Shield, KeyRound, Mail, FileCode2, Sparkles,
} from "lucide-react";
import { toast } from "sonner";

type TabId = "swagger" | "redoc" | "quickstart" | "deploy" | "domains" | "api" | "ops";

// The tab panels unmount when inactive and the page opens on the Swagger frame,
// so the written guides — by far the bulk of this route — are fetched only once
// a reader actually opens one.
function TabSkeleton() {
  return <div className="h-96 animate-pulse rounded-2xl border bg-muted/30" />;
}

const guideTab = <K extends "QuickstartTab" | "DeployTab" | "DomainsTab" | "ApiTab" | "OpsTab">(name: K) =>
  dynamic(() => import("./guide-tabs").then((m) => m[name]), { loading: TabSkeleton });

const QuickstartTab = guideTab("QuickstartTab");
const DeployTab = guideTab("DeployTab");
const DomainsTab = guideTab("DomainsTab");
const ApiTab = guideTab("ApiTab");
const OpsTab = guideTab("OpsTab");

export default function DocsPage() {
  const { t } = useI18n();
  const [view, setView] = useState<TabId>("swagger");
  const { origin, links } = useMemo(() => {
    const configured = getBaseUrl().replace(/\/+$/, "");
    const base = configured || (typeof window === "undefined" ? "" : window.location.origin);
    return {
      origin: base,
      links: {
        docs: configured ? "/docs" : "/backend-docs",
        redoc: configured ? "/redoc" : "/backend-redoc",
        openapi: "/openapi.yaml",
        health: "/health",
      },
    };
  }, []);

  const copy = async (text: string, label: string) => {
    try {
      await navigator.clipboard.writeText(text);
      toast.success(t("docs.copied", { label }));
    } catch { toast.error(t("docs.copyFailed", { label })); }
  };

  return (
    <div className="flex min-h-screen flex-col bg-[radial-gradient(circle_at_top,rgba(99,102,241,0.10),transparent_30%),linear-gradient(180deg,rgba(15,23,42,0.03),transparent_30%)]">
      <SiteHeader />
      <main className="flex-1">
        {/* Hero */}
        <section className="border-b bg-background/70">
          <div className="mx-auto grid max-w-7xl gap-6 px-4 py-10 lg:grid-cols-[1.3fr_0.7fr]">
            <div className="space-y-5">
              <div className="inline-flex items-center gap-2 rounded-full border bg-background px-3 py-1 text-xs text-muted-foreground shadow-sm">
                <Sparkles className="h-3.5 w-3.5 text-primary" />
                {t("docs.badge")}
              </div>
              <div className="space-y-3">
                <h1 className="max-w-3xl text-4xl font-semibold tracking-tight sm:text-5xl">{t("docs.title")}</h1>
                <p className="max-w-2xl text-base leading-7 text-muted-foreground">{t("docs.desc")}</p>
              </div>
              <div className="flex flex-wrap gap-3">
                <Button className="gap-2" onClick={() => setView("swagger")}><BookOpen className="h-4 w-4" />{t("docs.openSwagger")}</Button>
                <Button variant="outline" className="gap-2" onClick={() => setView("redoc")}><FileCode2 className="h-4 w-4" />{t("docs.switchRedoc")}</Button>
                <Button variant="ghost" className="gap-2" render={<a href={links.openapi} target="_blank" rel="noreferrer" />}><ExternalLink className="h-4 w-4" />{t("docs.rawOpenapi")}</Button>
              </div>
              <div className="grid gap-3 sm:grid-cols-3">
                <InfoCard icon={<Shield className="h-4 w-4 text-emerald-500" />} title={t("docs.admin")} description={t("docs.adminDesc")} />
                <InfoCard icon={<KeyRound className="h-4 w-4 text-sky-500" />} title={t("docs.tenant")} description={t("docs.tenantDesc")} />
                <InfoCard icon={<Mail className="h-4 w-4 text-amber-500" />} title={t("docs.mailbox")} description={t("docs.mailboxDesc")} />
              </div>
            </div>
            <Card className="border-primary/15 bg-[linear-gradient(180deg,rgba(99,102,241,0.10),transparent_55%),var(--card)] shadow-lg">
              <CardHeader>
                <CardTitle className="flex items-center gap-2"><FileCode2 className="h-4 w-4 text-primary" />{t("docs.endpoints")}</CardTitle>
                <CardDescription>{t("docs.endpointsDesc")}</CardDescription>
              </CardHeader>
              <CardContent className="space-y-3">
                <EndpointRow label={t("docs.baseUrl")} value={origin} onCopy={() => copy(origin, t("docs.baseUrl"))} />
                <EndpointRow label={t("docs.swaggerUi")} value={`${origin}${links.docs}`} onCopy={() => copy(`${origin}${links.docs}`, t("docs.swaggerUi"))} href={links.docs} />
                <EndpointRow label={t("docs.redoc")} value={`${origin}${links.redoc}`} onCopy={() => copy(`${origin}${links.redoc}`, t("docs.redoc"))} href={links.redoc} />
                <EndpointRow label={t("docs.openapi")} value={`${origin}${links.openapi}`} onCopy={() => copy(`${origin}${links.openapi}`, t("docs.openapi"))} href={links.openapi} />
                <EndpointRow label={t("docs.health")} value={`${origin}${links.health}`} onCopy={() => copy(`${origin}${links.health}`, t("docs.health"))} href={links.health} />
              </CardContent>
            </Card>
          </div>
        </section>

        {/* Tabs */}
        <section className="mx-auto max-w-7xl px-4 py-8">
          <Tabs value={view} onValueChange={(v) => setView(v as TabId)} className="gap-4">
            <TabsList variant="line" className="rounded-2xl border bg-background p-1 flex-wrap">
              <TabsTrigger value="swagger">{t("docs.swaggerUi")}</TabsTrigger>
              <TabsTrigger value="redoc">{t("docs.redoc")}</TabsTrigger>
              <TabsTrigger value="quickstart">{t("docs.quickstart")}</TabsTrigger>
              <TabsTrigger value="deploy">{t("guide.deploy")}</TabsTrigger>
              <TabsTrigger value="domains">{t("guide.domains")}</TabsTrigger>
              <TabsTrigger value="api">{t("guide.api")}</TabsTrigger>
              <TabsTrigger value="ops">{t("guide.ops")}</TabsTrigger>
            </TabsList>

            <TabsContent value="swagger" className="m-0"><DocFrame title="Swagger UI" src={`${origin}${links.docs}`} /></TabsContent>
            <TabsContent value="redoc" className="m-0"><DocFrame title="ReDoc" src={`${origin}${links.redoc}`} /></TabsContent>
            <TabsContent value="quickstart" className="m-0"><QuickstartTab t={t} copy={copy} /></TabsContent>
            <TabsContent value="deploy" className="m-0"><DeployTab t={t} copy={copy} /></TabsContent>
            <TabsContent value="domains" className="m-0"><DomainsTab t={t} copy={copy} /></TabsContent>
            <TabsContent value="api" className="m-0"><ApiTab t={t} copy={copy} /></TabsContent>
            <TabsContent value="ops" className="m-0"><OpsTab t={t} copy={copy} /></TabsContent>
          </Tabs>
        </section>
      </main>
    </div>
  );
}

/* ─── Shared helpers ─── */
function InfoCard({ icon, title, description }: { icon: ReactNode; title: string; description: string }) {
  return (
    <div className="rounded-2xl border bg-background/85 p-4 shadow-sm backdrop-blur">
      <div className="mb-3 flex h-9 w-9 items-center justify-center rounded-xl bg-muted">{icon}</div>
      <div className="space-y-1">
        <div className="font-medium">{title}</div>
        <p className="text-sm leading-6 text-muted-foreground">{description}</p>
      </div>
    </div>
  );
}

function EndpointRow({ label, value, onCopy, href }: { label: string; value: string; onCopy: () => void; href?: string }) {
  return (
    <div className="rounded-xl border bg-background/80 p-3">
      <div className="mb-1 text-xs font-medium uppercase tracking-[0.18em] text-muted-foreground">{label}</div>
      <div className="flex items-center gap-2">
        <code className="min-w-0 flex-1 truncate rounded-md bg-muted px-2 py-1 text-xs">{value}</code>
        <Button variant="ghost" size="icon-sm" onClick={onCopy}><Copy className="h-3.5 w-3.5" /></Button>
        {href && <Button variant="ghost" size="icon-sm" render={<a href={href} target="_blank" rel="noreferrer" />}><ExternalLink className="h-3.5 w-3.5" /></Button>}
      </div>
    </div>
  );
}

function DocFrame({ title, src }: { title: string; src: string }) {
  const { t } = useI18n();
  return (
    <Card className="overflow-hidden border-primary/10 bg-background shadow-lg">
      <CardHeader className="border-b bg-muted/30">
        <CardTitle className="flex items-center gap-2 text-base"><BookOpen className="h-4 w-4 text-primary" />{title}</CardTitle>
        <CardDescription>{t("docs.liveRendered")}</CardDescription>
      </CardHeader>
      <CardContent className="p-0"><iframe src={src} className="h-[calc(100vh-17rem)] w-full border-0" title={title} /></CardContent>
    </Card>
  );
}