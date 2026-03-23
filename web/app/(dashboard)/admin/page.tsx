"use client";

import { useState, useEffect, type ReactNode } from "react";
import { formatDistanceToNow } from "date-fns";
import { toast } from "sonner";
import {
  Activity,
  CreditCard,
  Globe,
  Inbox,
  Mail,
  RadioTower,
  Users,
  Webhook,
  TriangleAlert,
} from "lucide-react";

import { getStats } from "@/lib/api";
import type { SystemStats } from "@/lib/types";
import { useI18n } from "@/lib/i18n";
import { PageHeader } from "@/components/layout/page-header";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";

export default function AdminPage() {
  const [stats, setStats] = useState<SystemStats | null>(null);
  const [loading, setLoading] = useState(true);
  const { t } = useI18n();

  const statCards = [
    { key: "tenants_count" as const, label: t("admin.tenants"), icon: Users, color: "text-blue-500" },
    { key: "plans_count" as const, label: t("admin.plans"), icon: CreditCard, color: "text-emerald-500" },
    { key: "domains_count" as const, label: t("admin.domains"), icon: Globe, color: "text-violet-500" },
    { key: "mailboxes_count" as const, label: t("admin.mailboxes"), icon: Inbox, color: "text-amber-500" },
    { key: "messages_count" as const, label: t("admin.messages"), icon: Mail, color: "text-rose-500" },
  ];

  useEffect(() => {
    getStats()
      .then((res) => setStats(res.data))
      .catch(() => toast.error(t("admin.loadFailed")))
      .finally(() => setLoading(false));
  }, [t]);

  const series = stats?.metrics.time_series ?? [];

  return (
    <div className="flex flex-col">
      <PageHeader title={t("admin.title")} description={t("admin.desc")} />

      <div className="space-y-4 p-4">
        <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-5">
          {statCards.map((s) => (
            <Card key={s.key} className="overflow-hidden border-primary/10 bg-[linear-gradient(180deg,rgba(99,102,241,0.05),transparent_55%),var(--card)]">
              <CardHeader className="flex flex-row items-center justify-between pb-2">
                <CardTitle className="text-sm font-medium text-muted-foreground">{s.label}</CardTitle>
                <s.icon className={`h-4 w-4 ${s.color}`} />
              </CardHeader>
              <CardContent>
                {loading ? (
                  <Skeleton className="h-8 w-24" />
                ) : (
                  <div className="text-3xl font-bold tracking-tight">{stats?.[s.key]?.toLocaleString() ?? 0}</div>
                )}
              </CardContent>
            </Card>
          ))}
        </div>

        <div className="grid gap-4 xl:grid-cols-[1.25fr_0.75fr]">
          <Card className="overflow-hidden border-primary/10">
            <CardHeader>
              <CardTitle className="flex items-center gap-2">
                <Activity className="h-4 w-4 text-primary" />
                {t("admin.deliveryTimeline")}
              </CardTitle>
              <CardDescription>{t("admin.deliveryTimelineDesc")}</CardDescription>
            </CardHeader>
            <CardContent className="space-y-6">
              {loading ? (
                <Skeleton className="h-64 w-full" />
              ) : (
                <div className="grid gap-4 lg:grid-cols-2">
                  <TimelineCard
                    title="SMTP"
                    points={series.map((p) => ({
                      label: p.at,
                      primary: p.smtp_accepted,
                      secondary: p.smtp_rejected,
                    }))}
                    primaryLabel={t("admin.accepted")}
                    secondaryLabel={t("admin.rejected")}
                    accent="emerald"
                  />
                  <TimelineCard
                    title={t("admin.deliveryHooks")}
                    points={series.map((p) => ({
                      label: p.at,
                      primary: p.deliveries_ok + p.webhooks_delivered,
                      secondary: p.deliveries_failed + p.webhooks_failed,
                    }))}
                    primaryLabel={t("admin.successful")}
                    secondaryLabel={t("admin.failed")}
                    accent="violet"
                  />
                </div>
              )}
            </CardContent>
          </Card>

          <div className="grid gap-4">
            <MetricsCard
              title="SMTP"
              icon={<Mail className="h-4 w-4 text-primary" />}
              rows={[
                [t("admin.sessionsOpened"), stats?.metrics.smtp.sessions_opened ?? 0],
                [t("admin.sessionsActive"), stats?.metrics.smtp.sessions_active ?? 0],
                [t("admin.recipientsAccepted"), stats?.metrics.smtp.recipients_accepted ?? 0],
                [t("admin.recipientsRejected"), stats?.metrics.smtp.recipients_rejected ?? 0],
                [t("admin.messagesAccepted"), stats?.metrics.smtp.messages_accepted ?? 0],
                [t("admin.messagesRejected"), stats?.metrics.smtp.messages_rejected ?? 0],
              ]}
              footer={`${t("admin.bytesReceived")}: ${(stats?.metrics.smtp.bytes_received ?? 0).toLocaleString()}`}
              loading={loading}
            />
            <MetricsCard
              title={t("admin.realtime")}
              icon={<RadioTower className="h-4 w-4 text-primary" />}
              rows={[
                [t("admin.subscribers"), stats?.metrics.realtime.subscribers_current ?? 0],
                [t("admin.eventsPublished"), stats?.metrics.realtime.events_published ?? 0],
              ]}
              footer={`${t("admin.uptime")}: ${stats?.metrics.uptime_seconds?.toLocaleString() ?? 0}s`}
              loading={loading}
            />
            <MetricsCard
              title={t("admin.webhook")}
              icon={<Webhook className="h-4 w-4 text-primary" />}
              rows={[
                [t("admin.enabled"), stats?.metrics.webhooks.enabled ? t("admin.yes") : t("admin.no")],
                [t("admin.configuredUrls"), stats?.metrics.webhooks.configured ?? 0],
                [t("admin.queued"), stats?.metrics.webhooks.queued ?? 0],
                [t("admin.delivered"), stats?.metrics.webhooks.delivered ?? 0],
                [t("admin.failed"), stats?.metrics.webhooks.failed ?? 0],
                [t("admin.retried"), stats?.metrics.webhooks.retried ?? 0],
                [t("admin.deadLetters"), stats?.metrics.webhooks.dead_letter_size ?? 0],
              ]}
              loading={loading}
            />
          </div>
        </div>

        <div className="grid gap-4 xl:grid-cols-2">
          <DeliveryTable
            title={t("admin.topTenantDelivery")}
            description={t("admin.topTenantDeliveryDesc")}
            rows={stats?.tenant_delivery ?? []}
            loading={loading}
          />
          <DeliveryTable
            title={t("admin.topMailboxDelivery")}
            description={t("admin.topMailboxDeliveryDesc")}
            rows={stats?.mailbox_delivery ?? []}
            loading={loading}
          />
        </div>

        <div className="grid gap-4 xl:grid-cols-[1.1fr_0.9fr]">
          <Card>
            <CardHeader>
              <CardTitle>{t("admin.recentAudit")}</CardTitle>
              <CardDescription>{t("admin.recentAuditDesc")}</CardDescription>
            </CardHeader>
            <CardContent>
              {loading ? (
                <div className="space-y-3">
                  {Array.from({ length: 4 }).map((_, i) => (
                    <Skeleton key={i} className="h-10 w-full" />
                  ))}
                </div>
              ) : !stats?.recent_audit?.length ? (
                <div className="text-sm text-muted-foreground">{t("admin.noAuditYet")}</div>
              ) : (
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>{t("admin.action")}</TableHead>
                      <TableHead>{t("admin.actor")}</TableHead>
                      <TableHead>{t("admin.resource")}</TableHead>
                      <TableHead>{t("admin.when")}</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {stats.recent_audit.map((entry) => (
                      <TableRow key={entry.id}>
                        <TableCell>
                          <Badge variant="outline" className="font-mono text-[11px]">
                            {entry.action}
                          </Badge>
                        </TableCell>
                        <TableCell className="text-sm text-muted-foreground">{entry.actor || t("admin.system")}</TableCell>
                        <TableCell className="text-sm">{entry.resource_type}</TableCell>
                        <TableCell className="text-sm text-muted-foreground">
                          {formatDistanceToNow(new Date(entry.created_at), { addSuffix: true })}
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              )}
            </CardContent>
          </Card>

          <Card className="border-amber-500/20 bg-[linear-gradient(180deg,rgba(245,158,11,0.08),transparent_55%),var(--card)]">
            <CardHeader>
              <CardTitle className="flex items-center gap-2">
                <TriangleAlert className="h-4 w-4 text-amber-500" />
                {t("admin.deadLetterQueue")}
              </CardTitle>
              <CardDescription>{t("admin.deadLetterDesc")}</CardDescription>
            </CardHeader>
            <CardContent>
              {loading ? (
                <div className="space-y-3">
                  {Array.from({ length: 4 }).map((_, i) => (
                    <Skeleton key={i} className="h-12 w-full" />
                  ))}
                </div>
              ) : !stats?.dead_letters?.length ? (
                <div className="text-sm text-muted-foreground">{t("admin.noDeadLetters")}</div>
              ) : (
                <div className="space-y-3">
                  {stats.dead_letters.map((item) => (
                    <div key={item.id} className="rounded-xl border bg-background/85 p-3">
                      <div className="mb-2 flex items-center justify-between gap-3">
                        <Badge variant="outline" className="font-mono text-[11px]">
                          {item.event_type}
                        </Badge>
                        <span className="text-xs text-muted-foreground">{t("admin.attempts", { n: item.attempts })}</span>
                      </div>
                      <div className="space-y-1 text-xs text-muted-foreground">
                        <div className="truncate">
                          <span className="font-medium text-foreground">URL:</span> {item.url}
                        </div>
                        <div className="truncate">
                          <span className="font-medium text-foreground">Error:</span> {item.last_error}
                        </div>
                        <div>
                          {formatDistanceToNow(new Date(item.last_tried_at), { addSuffix: true })}
                        </div>
                      </div>
                    </div>
                  ))}
                </div>
              )}
            </CardContent>
          </Card>
        </div>
      </div>
    </div>
  );
}

function MetricsCard({
  title,
  icon,
  rows,
  loading,
  footer,
}: {
  title: string;
  icon: ReactNode;
  rows: [string, string | number][];
  loading: boolean;
  footer?: string;
}) {
  const { t } = useI18n();
  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between">
        <div>
          <CardTitle className="text-base">{title}</CardTitle>
          <CardDescription>{t("admin.liveMetrics")}</CardDescription>
        </div>
        {icon}
      </CardHeader>
      <CardContent className="space-y-3">
        {loading ? (
          <div className="space-y-3">
            {Array.from({ length: 4 }).map((_, i) => (
              <Skeleton key={i} className="h-6 w-full" />
            ))}
          </div>
        ) : (
          <>
            {rows.map(([label, value]) => (
              <div key={label} className="flex items-center justify-between gap-3 text-sm">
                <span className="text-muted-foreground">{label}</span>
                <span className="font-medium tabular-nums">{value}</span>
              </div>
            ))}
            {footer && <div className="border-t pt-3 text-xs text-muted-foreground">{footer}</div>}
          </>
        )}
      </CardContent>
    </Card>
  );
}

function DeliveryTable({
  title,
  description,
  rows,
  loading,
}: {
  title: string;
  description: string;
  rows: SystemStats["tenant_delivery"];
  loading: boolean;
}) {
  const { t } = useI18n();
  return (
    <Card>
      <CardHeader>
        <CardTitle>{title}</CardTitle>
        <CardDescription>{description}</CardDescription>
      </CardHeader>
      <CardContent>
        {loading ? (
          <div className="space-y-3">
            {Array.from({ length: 4 }).map((_, i) => (
              <Skeleton key={i} className="h-10 w-full" />
            ))}
          </div>
        ) : !rows.length ? (
          <div className="text-sm text-muted-foreground">{t("admin.noDeliveryYet")}</div>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t("admin.key")}</TableHead>
                <TableHead className="text-right">{t("admin.accepted")}</TableHead>
                <TableHead className="text-right">{t("admin.rejected")}</TableHead>
                <TableHead className="text-right">{t("admin.ok")}</TableHead>
                <TableHead className="text-right">{t("admin.failed")}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {rows.map((row) => (
                <TableRow key={row.key}>
                  <TableCell className="max-w-[280px] truncate font-mono text-xs">{row.key}</TableCell>
                  <TableCell className="text-right tabular-nums">{row.accepted}</TableCell>
                  <TableCell className="text-right tabular-nums">{row.rejected}</TableCell>
                  <TableCell className="text-right tabular-nums">{row.deliveries_ok}</TableCell>
                  <TableCell className="text-right tabular-nums">{row.deliveries_failed}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </CardContent>
    </Card>
  );
}

function TimelineCard({
  title,
  points,
  primaryLabel,
  secondaryLabel,
  accent,
}: {
  title: string;
  points: { label: string; primary: number; secondary: number }[];
  primaryLabel: string;
  secondaryLabel: string;
  accent: "emerald" | "violet";
}) {
  const primaryColor = accent === "emerald" ? "#10b981" : "#8b5cf6";
  const secondaryColor = accent === "emerald" ? "#f97316" : "#f43f5e";
  const gradientId = `${title.toLowerCase().replace(/\s+/g, "-")}-grid`;
  const max = Math.max(1, ...points.flatMap((p) => [p.primary, p.secondary]));
  const width = 520;
  const height = 180;
  const toPath = (key: "primary" | "secondary") =>
    points
      .map((point, index) => {
        const x = points.length === 1 ? 0 : (index / (points.length - 1)) * width;
        const y = height - (point[key] / max) * (height - 12) - 6;
        return `${index === 0 ? "M" : "L"}${x},${y}`;
      })
      .join(" ");

  return (
    <div className="rounded-2xl border bg-background/80 p-4">
      <div className="mb-3 flex items-center justify-between">
        <div>
          <div className="font-medium">{title}</div>
          <div className="text-xs text-muted-foreground">Last {points.length || 0} minute samples</div>
        </div>
        <div className="flex items-center gap-3 text-xs">
          <LegendDot color={primaryColor} label={primaryLabel} />
          <LegendDot color={secondaryColor} label={secondaryLabel} />
        </div>
      </div>
      {points.length < 2 ? (
        <div className="flex h-[180px] items-center justify-center text-sm text-muted-foreground">
          Waiting for more samples…
        </div>
      ) : (
        <div className="overflow-x-auto">
          <svg viewBox={`0 0 ${width} ${height}`} className="h-[180px] min-w-full">
            <defs>
              <linearGradient id={gradientId} x1="0" y1="0" x2="0" y2="1">
                <stop offset="0%" stopColor="rgba(148,163,184,0.10)" />
                <stop offset="100%" stopColor="rgba(148,163,184,0.02)" />
              </linearGradient>
            </defs>
            {[0.25, 0.5, 0.75].map((f) => (
              <line
                key={f}
                x1="0"
                y1={height * f}
                x2={width}
                y2={height * f}
                stroke={`url(#${gradientId})`}
                strokeDasharray="4 4"
                opacity="0.5"
              />
            ))}
            <path d={toPath("primary")} fill="none" stroke={primaryColor} strokeWidth="3" strokeLinejoin="round" strokeLinecap="round" />
            <path d={toPath("secondary")} fill="none" stroke={secondaryColor} strokeWidth="2.5" strokeLinejoin="round" strokeLinecap="round" />
          </svg>
        </div>
      )}
    </div>
  );
}

function LegendDot({ color, label }: { color: string; label: string }) {
  return (
    <div className="flex items-center gap-1.5">
      <span className="h-2.5 w-2.5 rounded-full" style={{ backgroundColor: color }} />
      <span className="text-muted-foreground">{label}</span>
    </div>
  );
}
