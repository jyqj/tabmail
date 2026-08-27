"use client";

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Wrench, AlertTriangle, CheckCircle2, ChevronRight } from "lucide-react";
import { CodeCard, Collapsible, NoteBox, SectionHeading, SubSection, type CopyFn, type TFn } from "./shared";

export function OpsTab({ t, copy }: { t: TFn; copy: CopyFn }) {
  const c = (code: string, label: string) => () => copy(code, label);
  const healthCmd = `curl http://127.0.0.1:8080/health`;
  const statsCmd = `curl http://127.0.0.1:8080/api/v1/admin/stats \\
  -H "Authorization: Bearer <admin-jwt-access-token>"`;
  const smtpCheck = `nc -vz 127.0.0.1 2525`;
  const logsCmd = `docker compose logs -f tabmail`;
  const metricsCmd = `curl http://127.0.0.1:8080/metrics`;
  const monitorCmd = `curl -N "http://127.0.0.1:8080/api/v1/admin/monitor/events" \\
  -H "Authorization: Bearer <admin-jwt-access-token>"`;
  const backupDb = `make backup-db`;
  const restoreDb = `make restore-db FILE=backups/postgres-xxxx.dump`;
  const backupFs = `make backup-obj`;
  const backupS3 = `TABMAIL_OBJECTSTORE=s3 make backup-obj`;
  const schemaTablesCmd = `psql "$TABMAIL_DB_DSN" -c '\dt'`;
  const schemaDescribeCmd = `psql "$TABMAIL_DB_DSN" -c '\d messages'`;
  const schemaResetCmd = `docker compose down -v && docker compose up -d --build`;
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

      <SubSection title={t("guide.ops.schemaTitle")} desc={t("guide.ops.schemaDesc")}>
        <div className="grid gap-4 lg:grid-cols-3">
          <CodeCard t={t} title={t("guide.ops.schemaTables")} description="" code={schemaTablesCmd} onCopy={c(schemaTablesCmd, "schema tables")} />
          <CodeCard t={t} title={t("guide.ops.schemaDescribe")} description="" code={schemaDescribeCmd} onCopy={c(schemaDescribeCmd, "schema describe")} />
          <CodeCard t={t} title={t("guide.ops.schemaReset")} description="" code={schemaResetCmd} onCopy={c(schemaResetCmd, "schema reset")} />
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
