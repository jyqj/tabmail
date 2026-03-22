"use client";

import { useMemo, useState, type ReactNode } from "react";
import { SiteHeader } from "@/components/site-header";
import { getBaseUrl } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import {
  BookOpen,
  ExternalLink,
  Copy,
  Shield,
  KeyRound,
  Mail,
  FileCode2,
  Sparkles,
} from "lucide-react";
import { toast } from "sonner";

const curlExamples = {
  health: `curl "$BASE_URL/health"`,
  mailboxToken: `curl -X POST "$BASE_URL/api/v1/token" \\
  -H 'Content-Type: application/json' \\
  -d '{
    "address": "secure@mail.example.com",
    "password": "Passw0rd!"
  }'`,
  domainCreate: `curl -X POST "$BASE_URL/api/v1/domains" \\
  -H "X-API-Key: $TENANT_API_KEY" \\
  -H 'Content-Type: application/json' \\
  -d '{
    "domain": "mail.example.com"
  }'`,
};

export default function DocsPage() {
  const [view, setView] = useState<"swagger" | "redoc" | "quickstart">("swagger");
  const baseUrl = getBaseUrl() || "http://localhost:8080";

  const links = useMemo(
    () => ({
      docs: `${baseUrl}/docs`,
      redoc: `${baseUrl}/redoc`,
      openapi: `${baseUrl}/openapi.yaml`,
      health: `${baseUrl}/health`,
    }),
    [baseUrl]
  );

  const copy = async (text: string, label: string) => {
    try {
      await navigator.clipboard.writeText(text);
      toast.success(`${label} copied`);
    } catch {
      toast.error(`Failed to copy ${label.toLowerCase()}`);
    }
  };

  return (
    <div className="flex min-h-screen flex-col bg-[radial-gradient(circle_at_top,rgba(99,102,241,0.10),transparent_30%),linear-gradient(180deg,rgba(15,23,42,0.03),transparent_30%)]">
      <SiteHeader />
      <main className="flex-1">
        <section className="border-b bg-background/70">
          <div className="mx-auto grid max-w-7xl gap-6 px-4 py-10 lg:grid-cols-[1.3fr_0.7fr]">
            <div className="space-y-5">
              <div className="inline-flex items-center gap-2 rounded-full border bg-background px-3 py-1 text-xs text-muted-foreground shadow-sm">
                <Sparkles className="h-3.5 w-3.5 text-primary" />
                TabMail API Portal
              </div>
              <div className="space-y-3">
                <h1 className="max-w-3xl text-4xl font-semibold tracking-tight sm:text-5xl">
                  One place for the spec, the console, and the auth model.
                </h1>
                <p className="max-w-2xl text-base leading-7 text-muted-foreground">
                  Explore Swagger, switch to ReDoc, inspect the raw OpenAPI file, and copy ready-to-run
                  examples without leaving the product.
                </p>
              </div>

              <div className="flex flex-wrap gap-3">
                <Button className="gap-2" onClick={() => setView("swagger")}>
                  <BookOpen className="h-4 w-4" />
                  Open Swagger
                </Button>
                <Button variant="outline" className="gap-2" onClick={() => setView("redoc")}>
                  <FileCode2 className="h-4 w-4" />
                  Switch to ReDoc
                </Button>
                <Button variant="ghost" className="gap-2" render={<a href={links.openapi} target="_blank" rel="noreferrer" />}>
                  <ExternalLink className="h-4 w-4" />
                  Raw OpenAPI
                </Button>
              </div>

              <div className="grid gap-3 sm:grid-cols-3">
                <PortalInfoCard
                  icon={<Shield className="h-4 w-4 text-emerald-500" />}
                  title="Admin"
                  description="X-Admin-Key + optional X-Tenant-ID for tenant-scoped impersonation."
                />
                <PortalInfoCard
                  icon={<KeyRound className="h-4 w-4 text-sky-500" />}
                  title="Tenant"
                  description="X-API-Key for domains, routes, mailboxes, and protected message access."
                />
                <PortalInfoCard
                  icon={<Mail className="h-4 w-4 text-amber-500" />}
                  title="Mailbox"
                  description="Bearer mailbox token for token-mode inbox access."
                />
              </div>
            </div>

            <Card className="border-primary/15 bg-[linear-gradient(180deg,rgba(99,102,241,0.10),transparent_55%),var(--card)] shadow-lg">
              <CardHeader>
                <CardTitle className="flex items-center gap-2">
                  <FileCode2 className="h-4 w-4 text-primary" />
                  Endpoints
                </CardTitle>
                <CardDescription>Copy or open the exact URLs your frontend and scripts use.</CardDescription>
              </CardHeader>
              <CardContent className="space-y-3">
                <EndpointRow label="Base URL" value={baseUrl} onCopy={() => copy(baseUrl, "Base URL")} />
                <EndpointRow label="Swagger UI" value={links.docs} onCopy={() => copy(links.docs, "Swagger URL")} href={links.docs} />
                <EndpointRow label="ReDoc" value={links.redoc} onCopy={() => copy(links.redoc, "ReDoc URL")} href={links.redoc} />
                <EndpointRow label="OpenAPI" value={links.openapi} onCopy={() => copy(links.openapi, "OpenAPI URL")} href={links.openapi} />
                <EndpointRow label="Health" value={links.health} onCopy={() => copy(links.health, "Health URL")} href={links.health} />
              </CardContent>
            </Card>
          </div>
        </section>

        <section className="mx-auto max-w-7xl px-4 py-8">
          <Tabs value={view} onValueChange={(v) => setView(v as typeof view)} className="gap-4">
            <TabsList variant="line" className="rounded-2xl border bg-background p-1">
              <TabsTrigger value="swagger">Swagger</TabsTrigger>
              <TabsTrigger value="redoc">ReDoc</TabsTrigger>
              <TabsTrigger value="quickstart">Quickstart</TabsTrigger>
            </TabsList>

            <TabsContent value="swagger" className="m-0">
              <DocFrame title="Swagger UI" src={links.docs} />
            </TabsContent>

            <TabsContent value="redoc" className="m-0">
              <DocFrame title="ReDoc" src={links.redoc} />
            </TabsContent>

            <TabsContent value="quickstart" className="m-0">
              <div className="grid gap-6 lg:grid-cols-[0.8fr_1.2fr]">
                <Card className="bg-background/90 shadow-sm">
                  <CardHeader>
                    <CardTitle>Auth matrix</CardTitle>
                    <CardDescription>Choose the right credential for the job.</CardDescription>
                  </CardHeader>
                  <CardContent className="space-y-3">
                    <QuickRow
                      badge="Public"
                      description="No header. Works only for public inbox reads."
                    />
                    <QuickRow
                      badge="X-API-Key"
                      description="Tenant-scoped console operations and protected mailbox reads."
                    />
                    <QuickRow
                      badge="X-Admin-Key"
                      description="System-wide admin routes. Add X-Tenant-ID when acting like a tenant."
                    />
                    <QuickRow
                      badge="Bearer token"
                      description="Mailbox-scoped token for token-mode inboxes."
                    />
                  </CardContent>
                </Card>

                <div className="grid gap-4">
                  <CodeCard
                    title="Health"
                    description="Fastest smoke check after deploy."
                    code={curlExamples.health}
                    onCopy={() => copy(curlExamples.health, "Health curl")}
                  />
                  <CodeCard
                    title="Mailbox token"
                    description="Exchange mailbox password for a bearer token."
                    code={curlExamples.mailboxToken}
                    onCopy={() => copy(curlExamples.mailboxToken, "Mailbox token curl")}
                  />
                  <CodeCard
                    title="Create domain"
                    description="Tenant-scoped domain binding request."
                    code={curlExamples.domainCreate}
                    onCopy={() => copy(curlExamples.domainCreate, "Create domain curl")}
                  />
                </div>
              </div>
            </TabsContent>
          </Tabs>
        </section>
      </main>
    </div>
  );
}

function PortalInfoCard({
  icon,
  title,
  description,
}: {
  icon: ReactNode;
  title: string;
  description: string;
}) {
  return (
    <div className="rounded-2xl border bg-background/85 p-4 shadow-sm backdrop-blur">
      <div className="mb-3 flex h-9 w-9 items-center justify-center rounded-xl bg-muted">
        {icon}
      </div>
      <div className="space-y-1">
        <div className="font-medium">{title}</div>
        <p className="text-sm leading-6 text-muted-foreground">{description}</p>
      </div>
    </div>
  );
}

function EndpointRow({
  label,
  value,
  onCopy,
  href,
}: {
  label: string;
  value: string;
  onCopy: () => void;
  href?: string;
}) {
  return (
    <div className="rounded-xl border bg-background/80 p-3">
      <div className="mb-1 text-xs font-medium uppercase tracking-[0.18em] text-muted-foreground">{label}</div>
      <div className="flex items-center gap-2">
        <code className="min-w-0 flex-1 truncate rounded-md bg-muted px-2 py-1 text-xs">{value}</code>
        <Button variant="ghost" size="icon-sm" onClick={onCopy}>
          <Copy className="h-3.5 w-3.5" />
        </Button>
        {href && (
          <Button variant="ghost" size="icon-sm" render={<a href={href} target="_blank" rel="noreferrer" />}>
            <ExternalLink className="h-3.5 w-3.5" />
          </Button>
        )}
      </div>
    </div>
  );
}

function DocFrame({ title, src }: { title: string; src: string }) {
  return (
    <Card className="overflow-hidden border-primary/10 bg-background shadow-lg">
      <CardHeader className="border-b bg-muted/30">
        <CardTitle className="flex items-center gap-2 text-base">
          <BookOpen className="h-4 w-4 text-primary" />
          {title}
        </CardTitle>
        <CardDescription>Rendered from the live backend endpoint.</CardDescription>
      </CardHeader>
      <CardContent className="p-0">
        <iframe src={src} className="h-[calc(100vh-17rem)] w-full border-0" title={title} />
      </CardContent>
    </Card>
  );
}

function QuickRow({ badge, description }: { badge: string; description: string }) {
  return (
    <div className="rounded-xl border bg-background px-4 py-3">
      <div className="mb-1">
        <Badge variant="outline" className="font-mono text-[11px]">
          {badge}
        </Badge>
      </div>
      <p className="text-sm leading-6 text-muted-foreground">{description}</p>
    </div>
  );
}

function CodeCard({
  title,
  description,
  code,
  onCopy,
}: {
  title: string;
  description: string;
  code: string;
  onCopy: () => void;
}) {
  return (
    <Card className="overflow-hidden border-primary/10 bg-[#0b1020] text-slate-100 shadow-lg">
      <CardHeader className="border-b border-white/10">
        <CardTitle className="text-base text-white">{title}</CardTitle>
        <CardDescription className="text-slate-400">{description}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-4 p-4">
        <pre className="overflow-x-auto rounded-xl bg-black/30 p-4 text-xs leading-6 text-slate-200">
          <code>{code}</code>
        </pre>
        <div className="flex justify-end">
          <Button variant="secondary" className="gap-2" onClick={onCopy}>
            <Copy className="h-3.5 w-3.5" />
            Copy
          </Button>
        </div>
      </CardContent>
    </Card>
  );
}
