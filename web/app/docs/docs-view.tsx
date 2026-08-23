"use client";

// The docs page renders on the server; what stays here is the part that needs
// the browser — which tab is open, and the clipboard. Anything that ends up in
// the initial markup arrives as an already translated label so the server HTML
// and the hydrated tree agree on the language. Toast copy is looked up through
// useI18n instead, because it is only ever produced after a click.

import { createContext, useContext, useState, type ReactNode } from "react";
import dynamic from "next/dynamic";
import { useI18n } from "@/lib/i18n";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { BookOpen, ExternalLink, Copy, FileCode2 } from "lucide-react";
import { toast } from "sonner";

export type TabId = "swagger" | "redoc" | "quickstart" | "deploy" | "domains" | "api" | "ops";

export interface DocsLinks {
  docs: string;
  redoc: string;
  openapi: string;
  health: string;
}

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

const DocsViewContext = createContext<{ view: TabId; setView: (v: TabId) => void } | null>(null);

function useDocsView() {
  const ctx = useContext(DocsViewContext);
  if (!ctx) throw new Error("docs view components must be used within DocsViewProvider");
  return ctx;
}

// Wraps the whole page so the hero buttons and the tab strip — which the server
// renders in different sections — can share the open tab.
export function DocsViewProvider({ children }: { children: ReactNode }) {
  const [view, setView] = useState<TabId>("swagger");
  return (
    <DocsViewContext value={{ view, setView }}>
      {children}
    </DocsViewContext>
  );
}

function useCopy() {
  const { t } = useI18n();
  return async (text: string, label: string) => {
    try {
      await navigator.clipboard.writeText(text);
      toast.success(t("docs.copied", { label }));
    } catch {
      toast.error(t("docs.copyFailed", { label }));
    }
  };
}

export function DocsHeroActions({
  labels,
  openapiHref,
}: {
  labels: { openSwagger: string; switchRedoc: string; rawOpenapi: string };
  openapiHref: string;
}) {
  const { setView } = useDocsView();
  return (
    <div className="flex flex-wrap gap-3">
      <Button className="gap-2" onClick={() => setView("swagger")}><BookOpen className="h-4 w-4" />{labels.openSwagger}</Button>
      <Button variant="outline" className="gap-2" onClick={() => setView("redoc")}><FileCode2 className="h-4 w-4" />{labels.switchRedoc}</Button>
      <Button variant="ghost" className="gap-2" render={<a href={openapiHref} target="_blank" rel="noreferrer" />}><ExternalLink className="h-4 w-4" />{labels.rawOpenapi}</Button>
    </div>
  );
}

export function DocsEndpoints({
  origin,
  links,
  labels,
}: {
  origin: string;
  links: DocsLinks;
  labels: { baseUrl: string; swaggerUi: string; redoc: string; openapi: string; health: string };
}) {
  const copy = useCopy();
  return (
    <>
      <EndpointRow label={labels.baseUrl} value={origin} onCopy={() => copy(origin, labels.baseUrl)} />
      <EndpointRow label={labels.swaggerUi} value={`${origin}${links.docs}`} onCopy={() => copy(`${origin}${links.docs}`, labels.swaggerUi)} href={links.docs} />
      <EndpointRow label={labels.redoc} value={`${origin}${links.redoc}`} onCopy={() => copy(`${origin}${links.redoc}`, labels.redoc)} href={links.redoc} />
      <EndpointRow label={labels.openapi} value={`${origin}${links.openapi}`} onCopy={() => copy(`${origin}${links.openapi}`, labels.openapi)} href={links.openapi} />
      <EndpointRow label={labels.health} value={`${origin}${links.health}`} onCopy={() => copy(`${origin}${links.health}`, labels.health)} href={links.health} />
    </>
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

export interface DocsTabLabels {
  swaggerUi: string;
  redoc: string;
  quickstart: string;
  deploy: string;
  domains: string;
  api: string;
  ops: string;
  liveRendered: string;
}

export function DocsTabs({
  origin,
  links,
  labels,
}: {
  origin: string;
  links: DocsLinks;
  labels: DocsTabLabels;
}) {
  const { view, setView } = useDocsView();
  const { t } = useI18n();
  const copy = useCopy();

  return (
    <Tabs value={view} onValueChange={(v) => setView(v as TabId)} className="gap-4">
      <TabsList variant="line" className="rounded-2xl border bg-background p-1 flex-wrap">
        <TabsTrigger value="swagger">{labels.swaggerUi}</TabsTrigger>
        <TabsTrigger value="redoc">{labels.redoc}</TabsTrigger>
        <TabsTrigger value="quickstart">{labels.quickstart}</TabsTrigger>
        <TabsTrigger value="deploy">{labels.deploy}</TabsTrigger>
        <TabsTrigger value="domains">{labels.domains}</TabsTrigger>
        <TabsTrigger value="api">{labels.api}</TabsTrigger>
        <TabsTrigger value="ops">{labels.ops}</TabsTrigger>
      </TabsList>

      <TabsContent value="swagger" className="m-0"><DocFrame title="Swagger UI" src={`${origin}${links.docs}`} caption={labels.liveRendered} /></TabsContent>
      <TabsContent value="redoc" className="m-0"><DocFrame title="ReDoc" src={`${origin}${links.redoc}`} caption={labels.liveRendered} /></TabsContent>
      <TabsContent value="quickstart" className="m-0"><QuickstartTab t={t} copy={copy} /></TabsContent>
      <TabsContent value="deploy" className="m-0"><DeployTab t={t} copy={copy} /></TabsContent>
      <TabsContent value="domains" className="m-0"><DomainsTab t={t} copy={copy} /></TabsContent>
      <TabsContent value="api" className="m-0"><ApiTab t={t} copy={copy} /></TabsContent>
      <TabsContent value="ops" className="m-0"><OpsTab t={t} copy={copy} /></TabsContent>
    </Tabs>
  );
}

function DocFrame({ title, src, caption }: { title: string; src: string; caption: string }) {
  return (
    <Card className="overflow-hidden border-primary/10 bg-background shadow-lg">
      <CardHeader className="border-b bg-muted/30">
        <CardTitle className="flex items-center gap-2 text-base"><BookOpen className="h-4 w-4 text-primary" />{title}</CardTitle>
        <CardDescription>{caption}</CardDescription>
      </CardHeader>
      <CardContent className="p-0"><iframe src={src} className="h-[calc(100vh-17rem)] w-full border-0" title={title} /></CardContent>
    </Card>
  );
}
