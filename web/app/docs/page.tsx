"use client";

import { useMemo, useState, type ReactNode } from "react";
import { SiteHeader } from "@/components/site-header";
import { getBaseUrl } from "@/lib/api";
import { useI18n } from "@/lib/i18n";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import {
  BookOpen, ExternalLink, Copy, Shield, KeyRound, Mail, FileCode2, Sparkles,
  Server, Globe, Terminal, Wrench, AlertTriangle, CheckCircle2, ChevronRight,
  Database, Lock, Network,
} from "lucide-react";
import { toast } from "sonner";

type TabId = "swagger" | "redoc" | "quickstart" | "deploy" | "domains" | "api" | "ops";

export default function DocsPage() {
  const { t } = useI18n();
  const [view, setView] = useState<TabId>("swagger");
  const baseUrl = getBaseUrl() || "http://localhost:8080";

  const links = useMemo(() => ({
    docs: `${baseUrl}/docs`,
    redoc: `${baseUrl}/redoc`,
    openapi: `${baseUrl}/openapi.yaml`,
    health: `${baseUrl}/health`,
  }), [baseUrl]);

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
                <EndpointRow label={t("docs.baseUrl")} value={baseUrl} onCopy={() => copy(baseUrl, t("docs.baseUrl"))} />
                <EndpointRow label={t("docs.swaggerUi")} value={links.docs} onCopy={() => copy(links.docs, t("docs.swaggerUi"))} href={links.docs} />
                <EndpointRow label={t("docs.redoc")} value={links.redoc} onCopy={() => copy(links.redoc, t("docs.redoc"))} href={links.redoc} />
                <EndpointRow label={t("docs.openapi")} value={links.openapi} onCopy={() => copy(links.openapi, t("docs.openapi"))} href={links.openapi} />
                <EndpointRow label={t("docs.health")} value={links.health} onCopy={() => copy(links.health, t("docs.health"))} href={links.health} />
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

            <TabsContent value="swagger" className="m-0"><DocFrame title="Swagger UI" src={links.docs} /></TabsContent>
            <TabsContent value="redoc" className="m-0"><DocFrame title="ReDoc" src={links.redoc} /></TabsContent>
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
type TFn = (key: string, params?: Record<string, string | number>) => string;
type CopyFn = (text: string, label: string) => void;

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

function CodeCard({ title, description, code, onCopy, t }: { title: string; description: string; code: string; onCopy: () => void; t: TFn }) {
  return (
    <Card className="overflow-hidden border-primary/10 bg-[#0b1020] text-slate-100 shadow-lg">
      <CardHeader className="border-b border-white/10">
        <CardTitle className="text-base text-white">{title}</CardTitle>
        <CardDescription className="text-slate-400">{description}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-4 p-4">
        <pre className="overflow-x-auto rounded-xl bg-black/30 p-4 text-xs leading-6 text-slate-200"><code>{code}</code></pre>
        <div className="flex justify-end"><Button variant="secondary" className="gap-2" onClick={onCopy}><Copy className="h-3.5 w-3.5" />{t("docs.copy")}</Button></div>
      </CardContent>
    </Card>
  );
}

function SectionHeading({ icon, title, desc }: { icon: ReactNode; title: string; desc: string }) {
  return (
    <div className="mb-8">
      <div className="flex items-center gap-3 mb-2">{icon}<h2 className="text-2xl font-bold tracking-tight">{title}</h2></div>
      <p className="text-muted-foreground max-w-3xl">{desc}</p>
    </div>
  );
}

function SubSection({ title, desc, children }: { title: string; desc?: string; children: ReactNode }) {
  return (
    <div className="mb-8">
      <h3 className="text-lg font-semibold mb-1">{title}</h3>
      {desc && <p className="text-sm text-muted-foreground mb-4">{desc}</p>}
      {children}
    </div>
  );
}

function StepCard({ step, title, desc, children }: { step: number; title: string; desc: string; children?: ReactNode }) {
  return (
    <div className="rounded-xl border bg-background/90 p-5 shadow-sm">
      <div className="flex items-start gap-3">
        <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg bg-primary text-primary-foreground text-sm font-bold">{step}</div>
        <div className="flex-1 space-y-2">
          <h4 className="font-semibold">{title}</h4>
          <p className="text-sm text-muted-foreground">{desc}</p>
          {children}
        </div>
      </div>
    </div>
  );
}

function EnvTable({ rows }: { rows: [string, string, string][] }) {
  return (
    <div className="overflow-x-auto rounded-xl border">
      <table className="w-full text-xs">
        <thead><tr className="border-b bg-muted/30"><th className="p-2 text-left font-semibold">Variable</th><th className="p-2 text-left font-semibold">Default</th><th className="p-2 text-left font-semibold">Description</th></tr></thead>
        <tbody>{rows.map(([v, d, n]) => <tr key={v} className="border-b last:border-0 hover:bg-muted/20"><td className="p-2 font-mono text-primary/90">{v}</td><td className="p-2 text-muted-foreground">{d}</td><td className="p-2">{n}</td></tr>)}</tbody>
      </table>
    </div>
  );
}

function NoteBox({ children, variant = "info" }: { children: ReactNode; variant?: "info" | "warn" }) {
  const cls = variant === "warn" ? "border-amber-500/30 bg-amber-500/5" : "border-primary/20 bg-primary/5";
  const Icon = variant === "warn" ? AlertTriangle : CheckCircle2;
  const iconCls = variant === "warn" ? "text-amber-500" : "text-primary";
  return (
    <div className={`rounded-xl border p-4 ${cls}`}>
      <div className="flex gap-3"><Icon className={`h-5 w-5 shrink-0 mt-0.5 ${iconCls}`} /><div className="text-sm leading-relaxed">{children}</div></div>
    </div>
  );
}

/* ─── Quickstart Tab ─── */
function QuickstartTab({ t, copy }: { t: TFn; copy: CopyFn }) {
  const curl = {
    health: `curl "$BASE_URL/health"`,
    token: `curl -X POST "$BASE_URL/api/v1/token" \\
  -H 'Content-Type: application/json' \\
  -d '{
    "address": "secure@mail.example.com",
    "password": "Passw0rd!"
  }'`,
    domain: `curl -X POST "$BASE_URL/api/v1/domains" \\
  -H "X-API-Key: $TENANT_API_KEY" \\
  -H 'Content-Type: application/json' \\
  -d '{ "domain": "mail.example.com" }'`,
    deep: `curl -X POST "$BASE_URL/api/v1/domains/$DOMAIN_ID/routes" \\
  -H "X-API-Key: $TENANT_API_KEY" \\
  -H 'Content-Type: application/json' \\
  -d '{
    "route_type": "deep_wildcard",
    "match_value": "**.mail.example.com",
    "auto_create_mailbox": true,
    "access_mode_default": "public"
  }'`,
  };
  return (
    <div className="grid gap-6 lg:grid-cols-[0.8fr_1.2fr]">
      <Card className="bg-background/90 shadow-sm">
        <CardHeader><CardTitle>{t("docs.authMatrix")}</CardTitle><CardDescription>{t("docs.authMatrixDesc")}</CardDescription></CardHeader>
        <CardContent className="space-y-3">
          {[["Public", "docs.publicDesc"], ["X-API-Key", "docs.apiKeyDesc"], ["X-Admin-Key", "docs.adminKeyDesc"], ["Bearer token", "docs.bearerDesc"]].map(([b, k]) => (
            <div key={b} className="rounded-xl border bg-background px-4 py-3"><div className="mb-1"><Badge variant="outline" className="font-mono text-[11px]">{b}</Badge></div><p className="text-sm leading-6 text-muted-foreground">{t(k)}</p></div>
          ))}
        </CardContent>
      </Card>
      <div className="grid gap-4">
        <CodeCard t={t} title={t("docs.health")} description={t("docs.healthDesc")} code={curl.health} onCopy={() => copy(curl.health, `${t("docs.health")} curl`)} />
        <CodeCard t={t} title={t("docs.mailboxTokenTitle")} description={t("docs.mailboxTokenDesc")} code={curl.token} onCopy={() => copy(curl.token, `${t("docs.mailboxTokenTitle")} curl`)} />
        <CodeCard t={t} title={t("docs.createDomainTitle")} description={t("docs.createDomainDesc")} code={curl.domain} onCopy={() => copy(curl.domain, `${t("docs.createDomainTitle")} curl`)} />
        <CodeCard t={t} title={t("docs.deepWildcardTitle")} description={t("docs.deepWildcardDesc")} code={curl.deep} onCopy={() => copy(curl.deep, `${t("docs.deepWildcardTitle")} curl`)} />
      </div>
    </div>
  );
}

/* ─── Deploy Tab ─── */
function DeployTab({ t, copy }: { t: TFn; copy: CopyFn }) {
  const c = (code: string, label: string) => () => copy(code, label);
  const dockerCmd = `cp .env.example .env\n# edit .env with real secrets\ndocker compose up -d --build`;
  const prodCmd = `cp .env.example .env\n# edit .env\ndocker compose -f docker-compose.prod.yml up -d --build`;
  const verifyCmd = `curl http://127.0.0.1:8080/health\ncurl http://127.0.0.1:8080/openapi.yaml\ncurl http://127.0.0.1:8080/metrics`;
  const starttlsConf = `TABMAIL_SMTP_TLSENABLED=true\nTABMAIL_SMTP_TLSCERT=/etc/ssl/tabmail.crt\nTABMAIL_SMTP_TLSKEY=/etc/ssl/tabmail.key\nTABMAIL_SMTP_FORCETLS=false`;
  const implicitConf = `TABMAIL_SMTP_TLSENABLED=true\nTABMAIL_SMTP_FORCETLS=true`;
  const tlsVerify = `openssl s_client -starttls smtp -crlf -connect 127.0.0.1:2525`;
  const nginxConf = `server {
    listen 443 ssl http2;
    server_name tabmail.example.com;

    ssl_certificate     /etc/ssl/tabmail.crt;
    ssl_certificate_key /etc/ssl/tabmail.key;

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    # SSE endpoints — must disable buffering
    location ~ ^/api/v1/(admin/monitor/events|mailbox/.+/events) {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_buffering off;
        proxy_cache off;
        proxy_read_timeout 86400s;
    }
}`;
  const fsConf = `TABMAIL_OBJECTSTORE=fs\nTABMAIL_DATADIR=/data`;
  const s3Conf = `TABMAIL_OBJECTSTORE=s3\nTABMAIL_S3_ENDPOINT=minio:9000\nTABMAIL_S3_REGION=us-east-1\nTABMAIL_S3_BUCKET=tabmail\nTABMAIL_S3_ACCESS_KEY=minioadmin\nTABMAIL_S3_SECRET_KEY=your-secret\nTABMAIL_S3_USE_TLS=false\nTABMAIL_S3_FORCE_PATH_STYLE=true`;
  const migrateExec = `make migrate`;
  const migrateStatus = `make migrate-status`;
  const migrateDown = `make migrate-down STEPS=1`;
  const manualRun = `go run ./cmd/tabmail`;
  const manualBuild = `make build\n./bin/tabmail`;
  const monitorCmd = `docker compose -f docker-compose.monitoring.yml up -d`;

  return (
    <div className="space-y-10">
      <SectionHeading icon={<Server className="h-6 w-6 text-primary" />} title={t("guide.deploy.title")} desc={t("guide.deploy.desc")} />

      <SubSection title={t("guide.deploy.dockerTitle")} desc={t("guide.deploy.dockerDesc")}>
        <div className="space-y-4">
          <CodeCard t={t} title="Docker Compose" description="" code={dockerCmd} onCopy={c(dockerCmd, "Docker Compose")} />
          <NoteBox>{t("guide.deploy.dockerNote")}</NoteBox>
          <CodeCard t={t} title={t("guide.deploy.dockerVerify")} description="" code={verifyCmd} onCopy={c(verifyCmd, "verify")} />
        </div>
      </SubSection>

      <SubSection title={t("guide.deploy.prodTitle")} desc={t("guide.deploy.prodDesc")}>
        <div className="space-y-4">
          <CodeCard t={t} title="Production Compose" description="" code={prodCmd} onCopy={c(prodCmd, "prod compose")} />
          <Card className="bg-background/90"><CardHeader><CardTitle className="text-base">{t("guide.deploy.prodRoles")}</CardTitle></CardHeader><CardContent className="space-y-2">
            {(["migrate", "api", "smtp", "worker", "retention"] as const).map(r => (
              <div key={r} className="flex items-center gap-2 text-sm"><ChevronRight className="h-4 w-4 text-primary" />{t(`guide.deploy.prodRole.${r}`)}</div>
            ))}
          </CardContent></Card>
        </div>
      </SubSection>

      <SubSection title={t("guide.deploy.envTitle")} desc={t("guide.deploy.envDesc")}>
        <div className="space-y-4">
          <h4 className="text-sm font-semibold text-destructive">{t("guide.deploy.envRequired")}</h4>
          <EnvTable rows={[
            ["TABMAIL_ADMINKEY", "—", "Super admin X-Admin-Key"],
            ["TABMAIL_MAILBOX_TOKEN_SECRET", "—", "Mailbox bearer token signing secret"],
            ["POSTGRES_PASSWORD", "—", "PostgreSQL password"],
            ["TABMAIL_REDIS_PASSWORD", "—", "Redis password"],
          ]} />
          <h4 className="text-sm font-semibold">{t("guide.deploy.envCommon")}</h4>
          <EnvTable rows={[
            ["TABMAIL_ROLE", "all", "Process role: all / api / smtp / worker / retention"],
            ["TABMAIL_DB_DSN", "postgres://...", "PostgreSQL connection string"],
            ["TABMAIL_REDIS_ADDR", "redis:6379", "Redis address"],
            ["TABMAIL_HTTP_ADDR", "0.0.0.0:8080", "HTTP listen address"],
            ["TABMAIL_SMTP_ADDR", "0.0.0.0:2525", "SMTP listen address"],
            ["TABMAIL_SMTP_DOMAIN", "mail.example.com", "SMTP banner / expected MX hostname"],
            ["TABMAIL_OBJECTSTORE", "fs", "Object storage backend: fs / s3"],
            ["TABMAIL_DATADIR", "/data", "Local .eml storage directory"],
            ["TABMAIL_HTTP_ALLOWED_ORIGINS", "*", "CORS allowed origins"],
            ["TABMAIL_HTTP_TRUSTED_PROXIES", "127.0.0.1/32", "Trusted reverse proxy CIDRs"],
            ["TABMAIL_MAILBOXNAMING", "full", "Mailbox key: full / local / domain"],
            ["TABMAIL_STRIPPLUSTAG", "true", "Strip +tag from address"],
            ["TABMAIL_MONITORHISTORY", "50", "Monitor history buffer size"],
            ["TABMAIL_WEBHOOK_URLS", "—", "Webhook endpoint URLs (comma-separated)"],
            ["TABMAIL_WEBHOOK_SECRET", "—", "Webhook HMAC signing secret"],
            ["TABMAIL_INGEST_DURABLE", "true", "Enable durable ingest queue"],
          ]} />
          <h4 className="text-sm font-semibold">{t("guide.deploy.envS3")}</h4>
          <EnvTable rows={[
            ["TABMAIL_S3_ENDPOINT", "minio:9000", "S3-compatible endpoint"],
            ["TABMAIL_S3_REGION", "us-east-1", "S3 region"],
            ["TABMAIL_S3_BUCKET", "tabmail", "Bucket name"],
            ["TABMAIL_S3_ACCESS_KEY", "—", "Access key"],
            ["TABMAIL_S3_SECRET_KEY", "—", "Secret key"],
            ["TABMAIL_S3_USE_TLS", "false", "Use TLS for S3"],
            ["TABMAIL_S3_FORCE_PATH_STYLE", "true", "Force path-style access"],
          ]} />
        </div>
      </SubSection>

      <SubSection title={t("guide.deploy.tlsTitle")} desc={t("guide.deploy.tlsDesc")}>
        <div className="grid gap-4 lg:grid-cols-2">
          <CodeCard t={t} title={t("guide.deploy.tlsStarttls")} description={t("guide.deploy.tlsStarttlsDesc")} code={starttlsConf} onCopy={c(starttlsConf, "STARTTLS")} />
          <CodeCard t={t} title={t("guide.deploy.tlsImplicit")} description={t("guide.deploy.tlsImplicitDesc")} code={implicitConf} onCopy={c(implicitConf, "Implicit TLS")} />
        </div>
        <div className="mt-4"><CodeCard t={t} title={t("guide.deploy.tlsVerify")} description="" code={tlsVerify} onCopy={c(tlsVerify, "STARTTLS verify")} /></div>
      </SubSection>

      <SubSection title={t("guide.deploy.proxyTitle")} desc={t("guide.deploy.proxyDesc")}>
        <CodeCard t={t} title={t("guide.deploy.proxyNginx")} description="" code={nginxConf} onCopy={c(nginxConf, "Nginx config")} />
        <div className="mt-4 space-y-3">
          <NoteBox variant="warn">{t("guide.deploy.proxySse")}</NoteBox>
          <NoteBox>{t("guide.deploy.proxyTrustDesc")}</NoteBox>
        </div>
      </SubSection>

      <SubSection title={t("guide.deploy.storageTitle")} desc={t("guide.deploy.storageDesc")}>
        <div className="grid gap-4 lg:grid-cols-2">
          <CodeCard t={t} title={t("guide.deploy.storageFs")} description="" code={fsConf} onCopy={c(fsConf, "fs config")} />
          <CodeCard t={t} title={t("guide.deploy.storageS3")} description="" code={s3Conf} onCopy={c(s3Conf, "S3 config")} />
        </div>
        <div className="mt-3"><NoteBox>{t("guide.deploy.storageNote")}</NoteBox></div>
      </SubSection>

      <SubSection title={t("guide.deploy.migrateTitle")} desc={t("guide.deploy.migrateDesc")}>
        <div className="grid gap-4 lg:grid-cols-3">
          <CodeCard t={t} title={t("guide.deploy.migrateExec")} description="" code={migrateExec} onCopy={c(migrateExec, "migrate")} />
          <CodeCard t={t} title={t("guide.deploy.migrateStatus")} description="" code={migrateStatus} onCopy={c(migrateStatus, "migrate status")} />
          <CodeCard t={t} title={t("guide.deploy.migrateDown")} description="" code={migrateDown} onCopy={c(migrateDown, "migrate down")} />
        </div>
      </SubSection>

      <SubSection title={t("guide.deploy.manualTitle")} desc={t("guide.deploy.manualDesc")}>
        <div className="grid gap-4 lg:grid-cols-2">
          <CodeCard t={t} title="go run" description="" code={manualRun} onCopy={c(manualRun, "go run")} />
          <CodeCard t={t} title="make build" description="" code={manualBuild} onCopy={c(manualBuild, "make build")} />
        </div>
      </SubSection>

      <SubSection title={t("guide.deploy.monitorTitle")} desc={t("guide.deploy.monitorDesc")}>
        <CodeCard t={t} title="Monitoring Compose" description="Prometheus: :9090 · Alertmanager: :9093 · Grafana: :3001" code={monitorCmd} onCopy={c(monitorCmd, "monitoring")} />
      </SubSection>

      <SubSection title={t("guide.deploy.dataTitle")} desc={t("guide.deploy.dataDesc")}>
        <div className="space-y-2">
          {(["dataPg", "dataObj", "dataRetention"] as const).map(k => (
            <div key={k} className="flex items-center gap-3 rounded-lg border bg-background/90 p-3 text-sm">
              <ChevronRight className="h-4 w-4 text-primary shrink-0" />
              {t(`guide.deploy.${k}`)}
            </div>
          ))}
        </div>
      </SubSection>
    </div>
  );
}

/* ─── Domains Tab ─── */
function DomainsTab({ t, copy }: { t: TFn; copy: CopyFn }) {
  const c = (code: string, label: string) => () => copy(code, label);
  const bindCmd = `curl -X POST "$BASE_URL/api/v1/domains" \\
  -H "X-API-Key: $TENANT_API_KEY" \\
  -H 'Content-Type: application/json' \\
  -d '{ "domain": "mail.example.com" }'`;
  const verifyCmd = `curl -X POST "$BASE_URL/api/v1/domains/$DOMAIN_ID/verify" \\
  -H "X-API-Key: $TENANT_API_KEY"`;
  const suggestCmd = `curl "$BASE_URL/api/v1/domains/$DOMAIN_ID/suggest-address" \\
  -H "X-API-Key: $TENANT_API_KEY"`;
  const suggestSubdomainCmd = `curl "$BASE_URL/api/v1/domains/$DOMAIN_ID/suggest-address?subdomain=true" \\
  -H "X-API-Key: $TENANT_API_KEY"`;
  const testSmtp = `nc 127.0.0.1 2525
EHLO localhost
MAIL FROM:<sender@test.com>
RCPT TO:<test@mail.example.com>
DATA
Subject: test
hello tabmail
.
QUIT`;

  return (
    <div className="space-y-10">
      <SectionHeading icon={<Globe className="h-6 w-6 text-primary" />} title={t("guide.domains.title")} desc={t("guide.domains.desc")} />

      <SubSection title={t("guide.domains.flowTitle")} desc={t("guide.domains.flowDesc")}>
        <div className="grid gap-3">
          {([1,2,3,4,5,6] as const).map(n => (
            <StepCard key={n} step={n} title={t(`guide.domains.step${n}`)} desc={t(`guide.domains.step${n}Desc`)}>
              {n === 2 && <div className="mt-3"><CodeCard t={t} title={t("guide.api.bindDomain")} description="" code={bindCmd} onCopy={c(bindCmd, "bind domain")} /></div>}
              {n === 4 && <div className="mt-3"><CodeCard t={t} title={t("guide.api.verifyDomain")} description="" code={verifyCmd} onCopy={c(verifyCmd, "verify")} /></div>}
              {n === 6 && <div className="mt-3"><CodeCard t={t} title="SMTP Test" description="" code={testSmtp} onCopy={c(testSmtp, "smtp test")} /></div>}
            </StepCard>
          ))}
        </div>
      </SubSection>

      <SubSection title={t("guide.domains.dnsTitle")} desc={t("guide.domains.dnsDesc")}>
        <div className="space-y-4">
          <Card className="bg-background/90"><CardHeader><CardTitle className="text-base flex items-center gap-2"><Database className="h-4 w-4 text-primary" />{t("guide.domains.dnsTxt")}</CardTitle><CardDescription>{t("guide.domains.dnsTxtDesc")}</CardDescription></CardHeader>
            <CardContent><pre className="rounded-xl bg-muted p-4 text-xs leading-6 overflow-x-auto"><code>{`mail.example.com.  IN  TXT  "tabmail-verify=<verification_token>"`}</code></pre></CardContent>
          </Card>
          <Card className="bg-background/90"><CardHeader><CardTitle className="text-base flex items-center gap-2"><Network className="h-4 w-4 text-primary" />{t("guide.domains.dnsMx")}</CardTitle><CardDescription>{t("guide.domains.dnsMxDesc")}</CardDescription></CardHeader>
            <CardContent><pre className="rounded-xl bg-muted p-4 text-xs leading-6 overflow-x-auto"><code>{`mail.example.com.  IN  MX  10  mail.example.com.`}</code></pre></CardContent>
          </Card>
          <Card className="bg-background/90"><CardHeader><CardTitle className="text-base flex items-center gap-2"><Lock className="h-4 w-4 text-primary" />{t("guide.domains.dnsSpf")}</CardTitle><CardDescription>{t("guide.domains.dnsSpfDesc")}</CardDescription></CardHeader>
            <CardContent><pre className="rounded-xl bg-muted p-4 text-xs leading-6 overflow-x-auto"><code>{`mail.example.com.  IN  TXT  "v=spf1 a mx -all"`}</code></pre></CardContent>
          </Card>
        </div>
      </SubSection>

      <SubSection title={t("guide.domains.routeTitle")} desc={t("guide.domains.routeDesc")}>
        <div className="grid gap-4 lg:grid-cols-2">
          {(["Exact", "Wildcard", "Deep", "Seq"] as const).map(r => {
            const key = r.toLowerCase() as "exact" | "wildcard" | "deep" | "seq";
            const colors: Record<string, string> = { exact: "border-emerald-500/30", wildcard: "border-sky-500/30", deep: "border-violet-500/30", seq: "border-amber-500/30" };
            return (
              <Card key={r} className={`bg-background/90 ${colors[key]}`}>
                <CardHeader><CardTitle className="text-base">{t(`guide.domains.route${r}`)}</CardTitle><CardDescription>{t(`guide.domains.route${r}Desc`)}</CardDescription></CardHeader>
                <CardContent><code className="block rounded-lg bg-muted p-3 text-xs">{t(`guide.domains.route${r}Example`)}</code></CardContent>
              </Card>
            );
          })}
        </div>
        <div className="mt-4"><NoteBox>{t("guide.domains.routePriority")}</NoteBox></div>
      </SubSection>

      <SubSection title={t("guide.domains.scenarioTitle")}>
        <div className="grid gap-4 lg:grid-cols-3">
          {([1,2,3] as const).map(n => (
            <Card key={n} className="bg-background/90"><CardHeader><CardTitle className="text-base">{t(`guide.domains.scenario${n}`)}</CardTitle></CardHeader>
              <CardContent><p className="text-sm text-muted-foreground">{t(`guide.domains.scenario${n}Desc`)}</p></CardContent>
            </Card>
          ))}
        </div>
      </SubSection>

      <SubSection title={t("guide.domains.randomTitle")} desc={t("guide.domains.randomDesc")}>
        <div className="space-y-4">
          <CodeCard t={t} title={t("guide.api.suggestAddress")} description="" code={suggestCmd} onCopy={c(suggestCmd, "suggest address")} />
          <NoteBox>{t("guide.domains.randomNote")}</NoteBox>
          <CodeCard t={t} title={t("guide.api.suggestSubdomainAddress")} description="" code={suggestSubdomainCmd} onCopy={c(suggestSubdomainCmd, "suggest subdomain address")} />
          <NoteBox>{t("guide.domains.randomSubdomainNote")}</NoteBox>
        </div>
      </SubSection>
    </div>
  );
}

/* ─── API Tab ─── */
function ApiTab({ t, copy }: { t: TFn; copy: CopyFn }) {
  const c = (code: string, label: string) => () => copy(code, label);
  const [activeSec, setActiveSec] = useState("setup");
  const setup = `export BASE_URL='http://127.0.0.1:8080'\nexport ADMIN_KEY='changeme'`;
  const cmds: Record<string, string> = {
    createPlan: `curl -X POST "$BASE_URL/api/v1/admin/plans" \\
  -H "X-Admin-Key: $ADMIN_KEY" \\
  -H 'Content-Type: application/json' \\
  -d '{
    "name": "starter", "max_domains": 5,
    "max_mailboxes_per_domain": 200, "max_messages_per_mailbox": 500,
    "max_message_bytes": 10485760, "retention_hours": 24,
    "rpm_limit": 120, "daily_quota": 20000
  }'`,
    createTenant: `curl -X POST "$BASE_URL/api/v1/admin/tenants" \\
  -H "X-Admin-Key: $ADMIN_KEY" \\
  -H 'Content-Type: application/json' \\
  -d '{ "name": "tenant-a", "plan_id": "<plan-id>" }'`,
    createApiKey: `curl -X POST "$BASE_URL/api/v1/admin/tenants/$TENANT_ID/keys" \\
  -H "X-Admin-Key: $ADMIN_KEY" \\
  -H 'Content-Type: application/json' \\
  -d '{ "label": "default key", "scopes": ["domains:read","domains:write","routes:read","routes:write","mailboxes:read","mailboxes:write","messages:read","messages:write"] }'`,
    bindDomain: `curl -X POST "$BASE_URL/api/v1/domains" \\
  -H "X-API-Key: $TENANT_API_KEY" \\
  -H 'Content-Type: application/json' \\
  -d '{ "domain": "mail.example.com" }'`,
    listDomains: `curl "$BASE_URL/api/v1/domains" \\
  -H "X-API-Key: $TENANT_API_KEY"`,
    verifyDomain: `curl -X POST "$BASE_URL/api/v1/domains/$DOMAIN_ID/verify" \\
  -H "X-API-Key: $TENANT_API_KEY"`,
    checkVerification: `curl "$BASE_URL/api/v1/domains/$DOMAIN_ID/verification-status" \\
  -H "X-API-Key: $TENANT_API_KEY"`,
    suggestAddress: `curl "$BASE_URL/api/v1/domains/$DOMAIN_ID/suggest-address" \\
  -H "X-API-Key: $TENANT_API_KEY"`,
    suggestSubdomainAddress: `curl "$BASE_URL/api/v1/domains/$DOMAIN_ID/suggest-address?subdomain=true" \\
  -H "X-API-Key: $TENANT_API_KEY"`,
    createWildcard: `curl -X POST "$BASE_URL/api/v1/domains/$DOMAIN_ID/routes" \\
  -H "X-API-Key: $TENANT_API_KEY" \\
  -H 'Content-Type: application/json' \\
  -d '{
    "route_type": "wildcard",
    "match_value": "*.mail.example.com",
    "auto_create_mailbox": true,
    "access_mode_default": "public"
  }'`,
    createSequence: `curl -X POST "$BASE_URL/api/v1/domains/$DOMAIN_ID/routes" \\
  -H "X-API-Key: $TENANT_API_KEY" \\
  -H 'Content-Type: application/json' \\
  -d '{
    "route_type": "sequence",
    "match_value": "box-{n}.mail.example.com",
    "range_start": 1, "range_end": 1000,
    "auto_create_mailbox": true,
    "access_mode_default": "token"
  }'`,
    createDeepWildcard: `curl -X POST "$BASE_URL/api/v1/domains/$DOMAIN_ID/routes" \\
  -H "X-API-Key: $TENANT_API_KEY" \\
  -H 'Content-Type: application/json' \\
  -d '{
    "route_type": "deep_wildcard",
    "match_value": "**.mail.example.com",
    "auto_create_mailbox": true,
    "access_mode_default": "public"
  }'`,
    listRoutes: `curl "$BASE_URL/api/v1/domains/$DOMAIN_ID/routes" \\
  -H "X-API-Key: $TENANT_API_KEY"`,
    createPublicMailbox: `curl -X POST "$BASE_URL/api/v1/mailboxes" \\
  -H "X-API-Key: $TENANT_API_KEY" \\
  -H 'Content-Type: application/json' \\
  -d '{ "address": "demo@mail.example.com", "access_mode": "public" }'`,
    createTokenMailbox: `curl -X POST "$BASE_URL/api/v1/mailboxes" \\
  -H "X-API-Key: $TENANT_API_KEY" \\
  -H 'Content-Type: application/json' \\
  -d '{
    "address": "secure@mail.example.com",
    "password": "Passw0rd!",
    "access_mode": "token"
  }'`,
    getToken: `curl -X POST "$BASE_URL/api/v1/token" \\
  -H 'Content-Type: application/json' \\
  -d '{ "address": "secure@mail.example.com", "password": "Passw0rd!" }'`,
    listMailboxes: `curl "$BASE_URL/api/v1/mailboxes" \\
  -H "X-API-Key: $TENANT_API_KEY"`,
    listMessages: `curl "$BASE_URL/api/v1/mailbox/demo@mail.example.com"`,
    listMessagesByToken: `curl "$BASE_URL/api/v1/mailbox/secure@mail.example.com" \\
  -H "Authorization: Bearer $MAILBOX_TOKEN"`,
    listMessagesByApiKey: `curl "$BASE_URL/api/v1/mailbox/secure@mail.example.com" \\
  -H "X-API-Key: $TENANT_API_KEY"`,
    viewMessage: `curl "$BASE_URL/api/v1/mailbox/secure@mail.example.com/$MESSAGE_ID" \\
  -H "Authorization: Bearer $MAILBOX_TOKEN"`,
    viewSource: `curl "$BASE_URL/api/v1/mailbox/secure@mail.example.com/$MESSAGE_ID/source" \\
  -H "Authorization: Bearer $MAILBOX_TOKEN"`,
    markRead: `curl -X PATCH "$BASE_URL/api/v1/mailbox/secure@mail.example.com/$MESSAGE_ID" \\
  -H "Authorization: Bearer $MAILBOX_TOKEN"`,
    deleteMessage: `curl -X DELETE "$BASE_URL/api/v1/mailbox/secure@mail.example.com/$MESSAGE_ID" \\
  -H "Authorization: Bearer $MAILBOX_TOKEN"`,
    purgeMailbox: `curl -X DELETE "$BASE_URL/api/v1/mailbox/secure@mail.example.com" \\
  -H "Authorization: Bearer $MAILBOX_TOKEN"`,
    systemStats: `curl "$BASE_URL/api/v1/admin/stats" \\
  -H "X-Admin-Key: $ADMIN_KEY"`,
    monitorHistory: `curl "$BASE_URL/api/v1/admin/monitor/history?page=1&per_page=20&type=message" \\
  -H "X-Admin-Key: $ADMIN_KEY"`,
    monitorSse: `curl -N "$BASE_URL/api/v1/admin/monitor/events" \\
  -H "X-Admin-Key: $ADMIN_KEY"`,
    getPolicy: `curl "$BASE_URL/api/v1/admin/policy" \\
  -H "X-Admin-Key: $ADMIN_KEY"`,
    updatePolicy: `curl -X PATCH "$BASE_URL/api/v1/admin/policy" \\
  -H "X-Admin-Key: $ADMIN_KEY" \\
  -H 'Content-Type: application/json' \\
  -d '{
    "default_accept": true,
    "accept_domains": [],
    "reject_domains": ["blocked.example.com", "*.trash.test"],
    "default_store": true,
    "store_domains": [],
    "discard_domains": ["devnull.example.com"],
    "reject_origin_domains": ["*.spam.test"]
  }'`,
    impersonate: `curl "$BASE_URL/api/v1/domains" \\
  -H "X-Admin-Key: $ADMIN_KEY" \\
  -H "X-Tenant-ID: $TENANT_ID"`,
    smtpTest: `nc 127.0.0.1 2525\nEHLO localhost\nMAIL FROM:<sender@example.org>\nRCPT TO:<demo@mail.example.com>\nDATA\nSubject: hello\nFrom: sender@example.org\nTo: demo@mail.example.com\n\nhello tabmail\n.\nQUIT`,
    smtpTlsTest: `openssl s_client -starttls smtp -crlf -connect 127.0.0.1:2525`,
  };

  type Section = { id: string; titleKey: string; descKey: string; sidebarKey: string; items: { key: string; cmdKey: string; note?: string }[] };
  const sections: Section[] = [
    { id: "s1", sidebarKey: "guide.api.sidebarS1", titleKey: "guide.api.s1Title", descKey: "guide.api.s1Desc", items: [
      { key: "guide.api.createPlan", cmdKey: "createPlan" },
      { key: "guide.api.createTenant", cmdKey: "createTenant" },
      { key: "guide.api.createApiKey", cmdKey: "createApiKey", note: "guide.api.createApiKeyNote" },
    ]},
    { id: "s2", sidebarKey: "guide.api.sidebarS2", titleKey: "guide.api.s2Title", descKey: "guide.api.s2Desc", items: [
      { key: "guide.api.bindDomain", cmdKey: "bindDomain" },
      { key: "guide.api.listDomains", cmdKey: "listDomains" },
      { key: "guide.api.verifyDomain", cmdKey: "verifyDomain" },
      { key: "guide.api.checkVerification", cmdKey: "checkVerification" },
      { key: "guide.api.suggestAddress", cmdKey: "suggestAddress" },
      { key: "guide.api.suggestSubdomainAddress", cmdKey: "suggestSubdomainAddress" },
      { key: "guide.api.createWildcard", cmdKey: "createWildcard" },
      { key: "guide.api.createSequence", cmdKey: "createSequence" },
      { key: "guide.api.createDeepWildcard", cmdKey: "createDeepWildcard" },
      { key: "guide.api.listRoutes", cmdKey: "listRoutes" },
    ]},
    { id: "s3", sidebarKey: "guide.api.sidebarS3", titleKey: "guide.api.s3Title", descKey: "guide.api.s3Desc", items: [
      { key: "guide.api.createPublicMailbox", cmdKey: "createPublicMailbox" },
      { key: "guide.api.createTokenMailbox", cmdKey: "createTokenMailbox" },
      { key: "guide.api.getToken", cmdKey: "getToken" },
      { key: "guide.api.listMailboxes", cmdKey: "listMailboxes" },
      { key: "guide.api.listMessages", cmdKey: "listMessages" },
      { key: "guide.api.listMessagesByToken", cmdKey: "listMessagesByToken" },
      { key: "guide.api.listMessagesByApiKey", cmdKey: "listMessagesByApiKey" },
      { key: "guide.api.viewMessage", cmdKey: "viewMessage" },
      { key: "guide.api.viewSource", cmdKey: "viewSource" },
      { key: "guide.api.markRead", cmdKey: "markRead" },
      { key: "guide.api.deleteMessage", cmdKey: "deleteMessage" },
      { key: "guide.api.purgeMailbox", cmdKey: "purgeMailbox" },
    ]},
    { id: "s4", sidebarKey: "guide.api.sidebarS4", titleKey: "guide.api.s4Title", descKey: "guide.api.s4Desc", items: [
      { key: "guide.api.systemStats", cmdKey: "systemStats" },
      { key: "guide.api.monitorHistory", cmdKey: "monitorHistory" },
      { key: "guide.api.monitorSse", cmdKey: "monitorSse" },
      { key: "guide.api.getPolicy", cmdKey: "getPolicy" },
      { key: "guide.api.updatePolicy", cmdKey: "updatePolicy" },
      { key: "guide.api.impersonate", cmdKey: "impersonate" },
    ]},
    { id: "s5", sidebarKey: "guide.api.sidebarS5", titleKey: "guide.api.s5Title", descKey: "guide.api.s5Desc", items: [
      { key: "guide.api.smtpTest", cmdKey: "smtpTest" },
      { key: "guide.api.smtpTlsTest", cmdKey: "smtpTlsTest" },
    ]},
  ];

  const scrollTo = (id: string) => {
    setActiveSec(id);
    document.getElementById(`api-${id}`)?.scrollIntoView({ behavior: "smooth", block: "start" });
  };

  return (
    <div className="space-y-10">
      <SectionHeading icon={<Terminal className="h-6 w-6 text-primary" />} title={t("guide.api.title")} desc={t("guide.api.desc")} />
      <div className="grid gap-8 lg:grid-cols-[200px_1fr]">
        {/* Sidebar nav */}
        <nav className="hidden lg:block sticky top-24 self-start space-y-1">
          <button onClick={() => scrollTo("setup")} className={`w-full text-left rounded-lg px-3 py-2 text-sm transition-colors ${activeSec === "setup" ? "bg-primary/10 text-primary font-medium" : "text-muted-foreground hover:text-foreground hover:bg-muted/50"}`}>{t("guide.api.sidebarSetup")}</button>
          {sections.map(s => (
            <button key={s.id} onClick={() => scrollTo(s.id)} className={`w-full text-left rounded-lg px-3 py-2 text-sm transition-colors ${activeSec === s.id ? "bg-primary/10 text-primary font-medium" : "text-muted-foreground hover:text-foreground hover:bg-muted/50"}`}>{t(s.sidebarKey)}</button>
          ))}
        </nav>
        {/* Content */}
        <div className="space-y-10 min-w-0">
          <div id="api-setup"><CodeCard t={t} title={t("guide.api.setupTitle")} description={t("guide.api.setupDesc")} code={setup} onCopy={c(setup, "setup")} /></div>
          {sections.map(s => (
            <div key={s.id} id={`api-${s.id}`}>
              <SubSection title={t(s.titleKey)} desc={t(s.descKey)}>
                <div className="grid gap-4">{s.items.map(item => (
                  <div key={item.cmdKey}>
                    <CodeCard t={t} title={t(item.key)} description={item.note ? t(item.note) : ""} code={cmds[item.cmdKey]} onCopy={c(cmds[item.cmdKey], t(item.key))} />
                  </div>
                ))}</div>
              </SubSection>
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}

/* ─── Collapsible Section ─── */
function Collapsible({ title, children, defaultOpen = false }: { title: string; children: ReactNode; defaultOpen?: boolean }) {
  const [open, setOpen] = useState(defaultOpen);
  return (
    <div className="rounded-xl border bg-background/90 overflow-hidden">
      <button onClick={() => setOpen(o => !o)} className="flex w-full items-center gap-3 p-4 text-left text-sm font-medium hover:bg-muted/30 transition-colors">
        <ChevronRight className={`h-4 w-4 text-primary shrink-0 transition-transform ${open ? "rotate-90" : ""}`} />
        {title}
      </button>
      {open && <div className="border-t px-4 pb-4 pt-3">{children}</div>}
    </div>
  );
}

/* ─── Ops Tab ─── */
function OpsTab({ t, copy }: { t: TFn; copy: CopyFn }) {
  const c = (code: string, label: string) => () => copy(code, label);
  const healthCmd = `curl http://127.0.0.1:8080/health`;
  const statsCmd = `curl http://127.0.0.1:8080/api/v1/admin/stats \\
  -H "X-Admin-Key: <admin-key>"`;
  const smtpCheck = `nc -vz 127.0.0.1 2525`;
  const logsCmd = `docker compose logs -f tabmail`;
  const metricsCmd = `curl http://127.0.0.1:8080/metrics`;
  const monitorCmd = `curl -N "http://127.0.0.1:8080/api/v1/admin/monitor/events" \\
  -H "X-Admin-Key: <admin-key>"`;
  const backupDb = `make backup-db`;
  const restoreDb = `make restore-db FILE=backups/postgres-xxxx.dump`;
  const backupFs = `make backup-obj`;
  const backupS3 = `TABMAIL_OBJECTSTORE=s3 make backup-obj`;
  const migrateStatusCmd = `make migrate-status`;
  const migrateRunCmd = `make migrate`;
  const migrateSqlCmd = `psql "$TABMAIL_DB_DSN" -c "SELECT version, name, applied_at FROM schema_migrations ORDER BY version;"`;
  const dbTablesCmd = `psql "$TABMAIL_DB_DSN" -c '\\dt'`;
  const dbMonitorCmd = `psql "$TABMAIL_DB_DSN" -c 'SELECT type, mailbox, sender, subject, at FROM monitor_events ORDER BY at DESC LIMIT 20;'`;
  const dbAuditCmd = `psql "$TABMAIL_DB_DSN" -c 'SELECT action, actor, resource_type, created_at FROM audit_log ORDER BY created_at DESC LIMIT 20;'`;
  const starttlsVerify = `openssl s_client -starttls smtp -crlf -connect 127.0.0.1:2525`;

  const metrics: [string, string, string][] = [
    ["sessions_active", "Active SMTP sessions", "Client disconnect / stuck connections"],
    ["recipients_rejected", "RCPT TO rejections", "Domain not verified / policy / no route"],
    ["messages_rejected", "DATA stage rejections", "All recipients failed / quota / size limit"],
    ["deliveries_failed", "Final delivery failures", "DB write / object store error"],
    ["deliveries_succeeded", "Successful deliveries", "Baseline tracking"],
    ["bytes_received", "Total SMTP bytes", "Traffic baseline"],
    ["subscribers_current", "SSE subscribers", "Connection leaks"],
    ["events_published", "SSE events broadcast", "Realtime throughput"],
    ["webhooks.queued", "Webhook events queued", "Backlog monitoring"],
    ["webhooks.delivered", "Webhook delivered", "Success rate"],
    ["webhooks.failed", "Webhook final failures", "Target reliability"],
    ["webhooks.dead_letter_size", "Dead letter count", "Webhook target down"],
  ];

  return (
    <div className="space-y-10">
      <SectionHeading icon={<Wrench className="h-6 w-6 text-primary" />} title={t("guide.ops.title")} desc={t("guide.ops.desc")} />

      <SubSection title={t("guide.ops.checklistTitle")} desc={t("guide.ops.checklistDesc")}>
        <div className="space-y-2">
          {([1,2,3,4,5,6,7] as const).map(n => (
            <div key={n} className="flex items-center gap-3 rounded-lg border bg-background/90 p-3 text-sm">
              <div className="flex h-6 w-6 shrink-0 items-center justify-center rounded-md bg-primary text-primary-foreground text-xs font-bold">{n}</div>
              {t(`guide.ops.check${n}`)}
            </div>
          ))}
        </div>
        <div className="mt-4 grid gap-4 lg:grid-cols-2">
          <CodeCard t={t} title="Health Check" description="" code={healthCmd} onCopy={c(healthCmd, "health")} />
          <CodeCard t={t} title="Admin Stats" description="" code={statsCmd} onCopy={c(statsCmd, "stats")} />
          <CodeCard t={t} title="SMTP Port Check" description="" code={smtpCheck} onCopy={c(smtpCheck, "smtp check")} />
          <CodeCard t={t} title="View Logs" description="" code={logsCmd} onCopy={c(logsCmd, "logs")} />
          <CodeCard t={t} title="Monitor SSE" description="" code={monitorCmd} onCopy={c(monitorCmd, "monitor")} />
          <CodeCard t={t} title="Prometheus Metrics" description="" code={metricsCmd} onCopy={c(metricsCmd, "metrics")} />
        </div>
      </SubSection>

      <SubSection title={t("guide.ops.metricsTitle")} desc={t("guide.ops.metricsDesc")}>
        <div className="overflow-x-auto rounded-xl border">
          <table className="w-full text-xs">
            <thead><tr className="border-b bg-muted/30"><th className="p-2 text-left font-semibold">{t("guide.ops.metric")}</th><th className="p-2 text-left font-semibold">{t("guide.ops.meaning")}</th><th className="p-2 text-left font-semibold">{t("guide.ops.action")}</th></tr></thead>
            <tbody>{metrics.map(([m, mean, act]) => <tr key={m} className="border-b last:border-0 hover:bg-muted/20"><td className="p-2 font-mono text-primary/90">{m}</td><td className="p-2">{mean}</td><td className="p-2 text-muted-foreground">{act}</td></tr>)}</tbody>
          </table>
        </div>
      </SubSection>

      <SubSection title={t("guide.ops.smtpTitle")} desc={t("guide.ops.smtpDesc")}>
        <div className="space-y-2">
          {([1,2,3,4] as const).map(n => (
            <Collapsible key={n} title={t(`guide.ops.smtp${n}`)}>
              <p className="text-sm text-muted-foreground leading-relaxed">{t(`guide.ops.smtp${n}Desc`)}</p>
              {n === 3 && <div className="mt-3"><CodeCard t={t} title="STARTTLS Verify" description="" code={starttlsVerify} onCopy={c(starttlsVerify, "starttls verify")} /></div>}
            </Collapsible>
          ))}
        </div>
      </SubSection>

      <SubSection title={t("guide.ops.monitorFaqTitle")} desc={t("guide.ops.monitorFaqDesc")}>
        <div className="space-y-2">
          {([1,2] as const).map(n => (
            <Collapsible key={n} title={t(`guide.ops.monitorFaq${n}`)}>
              <p className="text-sm text-muted-foreground leading-relaxed">{t(`guide.ops.monitorFaq${n}Desc`)}</p>
              {n === 2 && <div className="mt-3"><CodeCard t={t} title="Check monitor_events table" description="" code={`psql "$TABMAIL_DB_DSN" -c '\\d monitor_events'`} onCopy={c(`psql "$TABMAIL_DB_DSN" -c '\\d monitor_events'`, "psql")} /></div>}
            </Collapsible>
          ))}
        </div>
      </SubSection>

      <SubSection title={t("guide.ops.policyFaqTitle")} desc={t("guide.ops.policyFaqDesc")}>
        <div className="space-y-2">
          {([1,2,3] as const).map(n => (
            <Collapsible key={n} title={t(`guide.ops.policyFaq${n}`)}>
              <p className="text-sm text-muted-foreground leading-relaxed">{t(`guide.ops.policyFaq${n}Desc`)}</p>
            </Collapsible>
          ))}
        </div>
      </SubSection>

      <SubSection title={t("guide.ops.namingTitle")} desc={t("guide.ops.namingDesc")}>
        <div className="space-y-2">
          {(["namingFull", "namingLocal", "namingDomain"] as const).map(k => (
            <div key={k} className="flex items-center gap-3 rounded-lg border bg-background/90 p-3 text-sm font-mono">
              <ChevronRight className="h-4 w-4 text-primary shrink-0" />
              {t(`guide.ops.${k}`)}
            </div>
          ))}
        </div>
        <div className="mt-3"><NoteBox variant="warn">{t("guide.ops.namingWarn")}</NoteBox></div>
      </SubSection>

      <SubSection title={t("guide.ops.webhookTitle")} desc={t("guide.ops.webhookDesc")}>
        <Card className="bg-background/90"><CardHeader><CardTitle className="text-base">{t("guide.ops.webhookHeaders")}</CardTitle></CardHeader>
          <CardContent><div className="space-y-1 text-sm font-mono">{["Content-Type: application/json", "X-TabMail-Event", "X-TabMail-Attempt", "X-TabMail-Signature (if secret configured)"].map(h => <div key={h} className="text-muted-foreground">{h}</div>)}</div></CardContent>
        </Card>
        <Card className="mt-4 bg-background/90"><CardHeader><CardTitle className="text-base">{t("guide.ops.webhookDead")}</CardTitle></CardHeader>
          <CardContent><div className="space-y-2">{([1,2,3,4,5] as const).map(n => <div key={n} className="flex items-center gap-2 text-sm"><AlertTriangle className="h-4 w-4 text-amber-500" />{t(`guide.ops.webhookDead${n}`)}</div>)}</div></CardContent>
        </Card>
      </SubSection>

      <SubSection title={t("guide.ops.migrateTitle")} desc={t("guide.ops.migrateDesc")}>
        <div className="grid gap-4 lg:grid-cols-3">
          <CodeCard t={t} title={t("guide.ops.migrateCheck")} description="" code={migrateStatusCmd} onCopy={c(migrateStatusCmd, "migrate status")} />
          <CodeCard t={t} title={t("guide.ops.migrateRun")} description="" code={migrateRunCmd} onCopy={c(migrateRunCmd, "migrate")} />
          <CodeCard t={t} title={t("guide.ops.migrateSql")} description="" code={migrateSqlCmd} onCopy={c(migrateSqlCmd, "migrate sql")} />
        </div>
      </SubSection>

      <SubSection title={t("guide.ops.dbTitle")} desc={t("guide.ops.dbDesc")}>
        <div className="grid gap-4 lg:grid-cols-3">
          <CodeCard t={t} title={t("guide.ops.dbTables")} description="" code={dbTablesCmd} onCopy={c(dbTablesCmd, "tables")} />
          <CodeCard t={t} title={t("guide.ops.dbMonitor")} description="" code={dbMonitorCmd} onCopy={c(dbMonitorCmd, "monitor")} />
          <CodeCard t={t} title={t("guide.ops.dbAudit")} description="" code={dbAuditCmd} onCopy={c(dbAuditCmd, "audit")} />
        </div>
      </SubSection>

      <SubSection title={t("guide.ops.backupTitle")} desc={t("guide.ops.backupDesc")}>
        <div className="grid gap-4 lg:grid-cols-2">
          <CodeCard t={t} title={t("guide.ops.backupDb")} description="" code={backupDb} onCopy={c(backupDb, "backup db")} />
          <CodeCard t={t} title={t("guide.ops.restoreDb")} description="" code={restoreDb} onCopy={c(restoreDb, "restore db")} />
          <CodeCard t={t} title={t("guide.ops.backupFs")} description="" code={backupFs} onCopy={c(backupFs, "backup fs")} />
          <CodeCard t={t} title={t("guide.ops.backupS3")} description="" code={backupS3} onCopy={c(backupS3, "backup s3")} />
        </div>
        <div className="mt-3"><NoteBox variant="warn">{t("guide.ops.backupNote")}</NoteBox></div>
      </SubSection>

      <SubSection title={t("guide.ops.alertTitle")} desc={t("guide.ops.alertDesc")}>
        <div className="overflow-x-auto rounded-xl border">
          <table className="w-full text-xs">
            <thead><tr className="border-b bg-muted/30"><th className="p-2 text-left font-semibold">{t("guide.ops.alertRule")}</th><th className="p-2 text-left font-semibold">{t("guide.ops.alertMetric")}</th><th className="p-2 text-left font-semibold">{t("guide.ops.alertThreshold")}</th></tr></thead>
            <tbody>{([1,2,3,4,5] as const).map(n => <tr key={n} className="border-b last:border-0 hover:bg-muted/20"><td className="p-2">{t(`guide.ops.alert${n}`)}</td><td className="p-2 font-mono text-primary/90">{t(`guide.ops.alert${n}Metric`)}</td><td className="p-2 text-muted-foreground">{t(`guide.ops.alert${n}Threshold`)}</td></tr>)}</tbody>
          </table>
        </div>
      </SubSection>

      <SubSection title={t("guide.ops.bestTitle")} desc={t("guide.ops.bestDesc")}>
        <div className="space-y-2">
          {([1,2,3,4] as const).map(n => (
            <div key={n} className="flex items-center gap-3 rounded-lg border bg-background/90 p-3 text-sm">
              <CheckCircle2 className="h-4 w-4 text-emerald-500 shrink-0" />
              {t(`guide.ops.best${n}`)}
            </div>
          ))}
        </div>
      </SubSection>
    </div>
  );
}
