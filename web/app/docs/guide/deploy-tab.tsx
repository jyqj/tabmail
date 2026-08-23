"use client";

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Server, ChevronRight } from "lucide-react";
import { CodeCard, EnvTable, NoteBox, SectionHeading, SubSection, type CopyFn, type TFn } from "./shared";

export function DeployTab({ t, copy }: { t: TFn; copy: CopyFn }) {
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
  const schemaStartup = `go run ./cmd/tabmail`;
  const schemaTables = `psql "$TABMAIL_DB_DSN" -c '\dt'`;
  const schemaReset = `docker compose down -v
docker compose up -d --build`;
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
            {(["api", "smtp", "worker", "retention"] as const).map(r => (
              <div key={r} className="flex items-center gap-2 text-sm"><ChevronRight className="h-4 w-4 text-primary" />{t(`guide.deploy.prodRole.${r}`)}</div>
            ))}
          </CardContent></Card>
        </div>
      </SubSection>

      <SubSection title={t("guide.deploy.envTitle")} desc={t("guide.deploy.envDesc")}>
        <div className="space-y-4">
          <h4 className="text-sm font-semibold text-destructive">{t("guide.deploy.envRequired")}</h4>
          <EnvTable rows={[
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

      <SubSection title={t("guide.deploy.schemaTitle")} desc={t("guide.deploy.schemaDesc")}>
        <div className="grid gap-4 lg:grid-cols-3">
          <CodeCard t={t} title={t("guide.deploy.schemaStartup")} description="" code={schemaStartup} onCopy={c(schemaStartup, "schema startup")} />
          <CodeCard t={t} title={t("guide.deploy.schemaTables")} description="" code={schemaTables} onCopy={c(schemaTables, "schema tables")} />
          <CodeCard t={t} title={t("guide.deploy.schemaReset")} description="" code={schemaReset} onCopy={c(schemaReset, "schema reset")} />
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
