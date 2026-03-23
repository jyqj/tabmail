"use client";

import { useEffect, useMemo, useState } from "react";
import { formatDistanceToNow } from "date-fns";
import { Radar, Mail, Trash2, Eraser, Activity, RefreshCw, Search } from "lucide-react";
import { toast } from "sonner";

import { listMonitorHistory, streamAdminMonitorEvents } from "@/lib/api";
import type { MonitorEvent } from "@/lib/types";
import { useI18n } from "@/lib/i18n";
import { PageHeader } from "@/components/layout/page-header";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";

const MAX_EVENTS = 200;

export default function AdminMonitorPage() {
  const { t } = useI18n();
  const [events, setEvents] = useState<MonitorEvent[]>([]);
  const [history, setHistory] = useState<MonitorEvent[]>([]);
  const [connected, setConnected] = useState(false);
  const [reconnectKey, setReconnectKey] = useState(0);
  const [historyPage, setHistoryPage] = useState(1);
  const [historyTotal, setHistoryTotal] = useState(0);
  const [filterType, setFilterType] = useState("all");
  const [filterMailbox, setFilterMailbox] = useState("");
  const [filterSender, setFilterSender] = useState("");

  const eventStyles: Record<string, { label: string; className: string; icon: typeof Mail }> = {
    message: { label: t("monitor.message"), className: "bg-emerald-500/10 text-emerald-600 border-emerald-500/20", icon: Mail },
    delete: { label: t("monitor.delete"), className: "bg-rose-500/10 text-rose-600 border-rose-500/20", icon: Trash2 },
    purge: { label: t("monitor.purge"), className: "bg-amber-500/10 text-amber-600 border-amber-500/20", icon: Eraser },
    ping: { label: t("monitor.ping"), className: "bg-slate-500/10 text-slate-600 border-slate-500/20", icon: Activity },
    ready: { label: t("monitor.ready"), className: "bg-sky-500/10 text-sky-600 border-sky-500/20", icon: Activity },
  };

  useEffect(() => {
    const controller = new AbortController();
    streamAdminMonitorEvents({
      signal: controller.signal,
      onEvent: ({ type, data }) => {
        if (type === "ready") {
          setConnected(true);
          return;
        }
        if (type === "ping") return;
        const event = data as MonitorEvent;
        setEvents((prev) => [event, ...prev].slice(0, MAX_EVENTS));
      },
    }).catch((e) => {
      const err = e as { error?: { message?: string } };
      if (!controller.signal.aborted) {
        setConnected(false);
        toast.error(err?.error?.message || t("monitor.streamDisconnected"));
      }
    });
    return () => controller.abort();
  }, [reconnectKey, t]);

  useEffect(() => {
    listMonitorHistory({
      page: historyPage,
      per_page: 30,
      type: filterType === "all" ? undefined : filterType,
      mailbox: filterMailbox || undefined,
      sender: filterSender || undefined,
    })
      .then((res) => {
        setHistory(res.data);
        setHistoryTotal(res.meta.total);
      })
      .catch(() => toast.error(t("monitor.loadHistoryFailed")));
  }, [historyPage, filterType, filterMailbox, filterSender, t]);

  const stats = useMemo(() => {
    const counters = { message: 0, delete: 0, purge: 0 };
    for (const event of events) {
      if (event.type === "message") counters.message++;
      if (event.type === "delete") counters.delete++;
      if (event.type === "purge") counters.purge++;
    }
    return counters;
  }, [events]);

  const filteredEvents = useMemo(() => {
    return events.filter((event) => {
      if (filterType !== "all" && event.type !== filterType) return false;
      if (filterMailbox && !event.mailbox.toLowerCase().includes(filterMailbox.toLowerCase())) return false;
      if (filterSender && !(event.sender || "").toLowerCase().includes(filterSender.toLowerCase())) return false;
      return true;
    });
  }, [events, filterType, filterMailbox, filterSender]);

  return (
    <div className="flex flex-col">
      <PageHeader
        title={t("monitor.title")}
        description={t("monitor.desc")}
        actions={
          <div className="flex items-center gap-2">
            <Badge variant="outline" className={connected ? "border-emerald-500/30 text-emerald-600" : "border-amber-500/30 text-amber-600"}>
              {connected ? t("monitor.live") : t("monitor.disconnected")}
            </Badge>
            <Button
              variant="outline"
              size="sm"
              className="gap-1.5"
              onClick={() => {
                setConnected(false);
                setReconnectKey((v) => v + 1);
              }}
            >
              <RefreshCw className="h-3.5 w-3.5" />
              {t("monitor.reconnect")}
            </Button>
          </div>
        }
      />

      <div className="space-y-4 p-4">
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <Search className="h-4 w-4 text-primary" />
              {t("monitor.filters")}
            </CardTitle>
            <CardDescription>{t("monitor.filtersDesc")}</CardDescription>
          </CardHeader>
          <CardContent className="grid gap-3 md:grid-cols-3">
            <Select value={filterType} onValueChange={(value) => setFilterType(value || "all")}>
              <SelectTrigger>
                <SelectValue placeholder={t("monitor.eventType")} />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">{t("monitor.allEvents")}</SelectItem>
                <SelectItem value="message">{t("monitor.message")}</SelectItem>
                <SelectItem value="delete">{t("monitor.delete")}</SelectItem>
                <SelectItem value="purge">{t("monitor.purge")}</SelectItem>
              </SelectContent>
            </Select>
            <Input
              placeholder={t("monitor.filterMailbox")}
              value={filterMailbox}
              onChange={(e) => {
                setHistoryPage(1);
                setFilterMailbox(e.target.value);
              }}
            />
            <Input
              placeholder={t("monitor.filterSender")}
              value={filterSender}
              onChange={(e) => {
                setHistoryPage(1);
                setFilterSender(e.target.value);
              }}
            />
          </CardContent>
        </Card>

        <div className="grid gap-4 md:grid-cols-4">
          <MonitorCard title={t("monitor.buffered")} value={filteredEvents.length} hint={t("monitor.bufferedHint")} />
          <MonitorCard title={t("monitor.messages")} value={stats.message} hint={t("monitor.messagesHint")} />
          <MonitorCard title={t("monitor.deletes")} value={stats.delete} hint={t("monitor.deletesHint")} />
          <MonitorCard title={t("monitor.purges")} value={stats.purge} hint={t("monitor.purgesHint")} />
        </div>

        <Card className="overflow-hidden border-primary/10 bg-[radial-gradient(circle_at_top,rgba(14,165,233,0.10),transparent_35%),var(--card)]">
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <Radar className="h-4 w-4 text-primary" />
              {t("monitor.eventStream")}
            </CardTitle>
            <CardDescription>
              {t("monitor.eventStreamDesc")}
            </CardDescription>
          </CardHeader>
          <CardContent className="p-0">
            {filteredEvents.length === 0 ? (
              <div className="flex h-[28rem] items-center justify-center text-sm text-muted-foreground">
                {t("monitor.waiting")}
              </div>
            ) : (
              <ScrollArea className="h-[28rem]">
                <div className="space-y-3 p-4">
                  {filteredEvents.map((event, index) => {
                    const meta = eventStyles[event.type] ?? eventStyles.message;
                    const Icon = meta.icon;
                    return (
                      <div
                        key={`${event.at}-${event.message_id || "none"}-${index}`}
                        className="rounded-2xl border bg-background/85 p-4 shadow-sm"
                      >
                        <div className="mb-3 flex items-start justify-between gap-3">
                          <div className="flex items-center gap-3">
                            <div className="flex h-10 w-10 items-center justify-center rounded-xl bg-muted">
                              <Icon className="h-4 w-4 text-primary" />
                            </div>
                            <div>
                              <Badge variant="outline" className={meta.className}>
                                {meta.label}
                              </Badge>
                              <div className="mt-2 font-mono text-xs text-muted-foreground">
                                {formatDistanceToNow(new Date(event.at), { addSuffix: true })}
                              </div>
                            </div>
                          </div>
                          {event.size ? (
                            <div className="text-xs text-muted-foreground">{(event.size / 1024).toFixed(1)} KB</div>
                          ) : null}
                        </div>

                        <div className="grid gap-2 text-sm md:grid-cols-[minmax(0,1.2fr)_minmax(0,0.8fr)]">
                          <div className="space-y-1">
                            <div className="truncate">
                              <span className="text-muted-foreground">{t("monitor.mailbox")}</span>{" "}
                              <span className="font-medium">{event.mailbox || "—"}</span>
                            </div>
                            <div className="truncate">
                              <span className="text-muted-foreground">{t("monitor.subject")}</span>{" "}
                              <span>{event.subject || "—"}</span>
                            </div>
                          </div>
                          <div className="space-y-1">
                            <div className="truncate">
                              <span className="text-muted-foreground">{t("monitor.sender")}</span>{" "}
                              <span>{event.sender || "—"}</span>
                            </div>
                            <div className="truncate">
                              <span className="text-muted-foreground">{t("monitor.messageId")}</span>{" "}
                              <span className="font-mono text-xs">{event.message_id || "—"}</span>
                            </div>
                          </div>
                        </div>
                      </div>
                    );
                  })}
                </div>
              </ScrollArea>
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>{t("monitor.persistedHistory")}</CardTitle>
            <CardDescription>{t("monitor.persistedHistoryDesc")}</CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            {!history.length ? (
              <div className="text-sm text-muted-foreground">{t("monitor.noStored")}</div>
            ) : (
              <div className="space-y-3">
                {history.map((event) => {
                  const meta = eventStyles[event.type] ?? eventStyles.message;
                  return (
                    <div key={`${event.at}-${event.message_id || "none"}`} className="rounded-xl border bg-background/80 p-3">
                      <div className="mb-2 flex items-center justify-between gap-3">
                        <Badge variant="outline" className={meta.className}>{meta.label}</Badge>
                        <span className="text-xs text-muted-foreground">
                          {formatDistanceToNow(new Date(event.at), { addSuffix: true })}
                        </span>
                      </div>
                      <div className="grid gap-2 text-sm md:grid-cols-3">
                        <div className="truncate"><span className="text-muted-foreground">{t("monitor.mailbox")}</span> {event.mailbox || "—"}</div>
                        <div className="truncate"><span className="text-muted-foreground">{t("monitor.sender")}</span> {event.sender || "—"}</div>
                        <div className="truncate"><span className="text-muted-foreground">{t("monitor.subject")}</span> {event.subject || "—"}</div>
                      </div>
                    </div>
                  );
                })}
              </div>
            )}
            <div className="flex items-center justify-between">
              <div className="text-xs text-muted-foreground">
                Page {historyPage} · {historyTotal.toLocaleString()} total
              </div>
              <div className="flex gap-2">
                <Button variant="outline" size="sm" disabled={historyPage <= 1} onClick={() => setHistoryPage((p) => p - 1)}>
                  {t("monitor.previous")}
                </Button>
                <Button
                  variant="outline"
                  size="sm"
                  disabled={historyPage * 30 >= historyTotal}
                  onClick={() => setHistoryPage((p) => p + 1)}
                >
                  {t("monitor.next")}
                </Button>
              </div>
            </div>
          </CardContent>
        </Card>
      </div>
    </div>
  );
}

function MonitorCard({ title, value, hint }: { title: string; value: number; hint: string }) {
  return (
    <Card className="border-primary/10">
      <CardHeader className="pb-2">
        <CardTitle className="text-sm font-medium text-muted-foreground">{title}</CardTitle>
      </CardHeader>
      <CardContent>
        <div className="text-3xl font-semibold tracking-tight">{value.toLocaleString()}</div>
        <div className="mt-1 text-xs text-muted-foreground">{hint}</div>
      </CardContent>
    </Card>
  );
}
