"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import { formatDistanceToNow } from "date-fns";
import { RefreshCw, Search, Webhook } from "lucide-react";
import { toast } from "sonner";

import { listWebhookDeliveries } from "@/lib/api";
import type { WebhookDelivery } from "@/lib/types";
import { useI18n } from "@/lib/i18n";
import { PageHeader } from "@/components/layout/page-header";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";

const PAGE_SIZE = 30;

const stateStyles: Record<string, string> = {
  pending: "border-slate-500/20 bg-slate-500/10 text-slate-700",
  processing: "border-sky-500/20 bg-sky-500/10 text-sky-700",
  retry: "border-amber-500/20 bg-amber-500/10 text-amber-700",
  delivered: "border-emerald-500/20 bg-emerald-500/10 text-emerald-700",
  dead: "border-rose-500/20 bg-rose-500/10 text-rose-700",
};

export default function AdminWebhooksPage() {
  const { t } = useI18n();
  const [items, setItems] = useState<WebhookDelivery[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [loading, setLoading] = useState(true);
  const [state, setState] = useState("all");
  const [eventType, setEventType] = useState("");
  const [url, setUrl] = useState("");

  const fetchItems = useCallback(async () => {
    setLoading(true);
    try {
      const res = await listWebhookDeliveries({
        page,
        per_page: PAGE_SIZE,
        state: state === "all" ? undefined : state,
        event_type: eventType || undefined,
        url: url || undefined,
      });
      setItems(res.data ?? []);
      setTotal(res.meta.total);
    } catch {
      toast.error(t("webhooks.loadFailed"));
    } finally {
      setLoading(false);
    }
  }, [eventType, page, state, url, t]);

  useEffect(() => {
    fetchItems();
  }, [fetchItems]);

  const stats = useMemo(() => {
    return items.reduce<Record<string, number>>((acc, item) => {
      acc[item.state] = (acc[item.state] || 0) + 1;
      return acc;
    }, {});
  }, [items]);

  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE));

  return (
    <div className="flex flex-col">
      <PageHeader
        title={t("webhooks.title")}
        description={t("webhooks.desc")}
        actions={
          <Button variant="outline" size="sm" className="gap-1.5" onClick={fetchItems}>
            <RefreshCw className="h-3.5 w-3.5" />
            {t("webhooks.refresh")}
          </Button>
        }
      />

      <div className="space-y-4 p-4">
        <div className="grid gap-4 md:grid-cols-4">
          <StatCard title={t("webhooks.currentPage")} value={items.length} />
          <StatCard title={t("webhooks.total")} value={total} />
          <StatCard title={t("webhooks.delivered")} value={stats.delivered || 0} />
          <StatCard title={t("webhooks.dead")} value={stats.dead || 0} />
        </div>

        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <Search className="h-4 w-4 text-primary" />
              {t("webhooks.filter")}
            </CardTitle>
            <CardDescription>{t("webhooks.filterDesc")}</CardDescription>
          </CardHeader>
          <CardContent className="grid gap-3 md:grid-cols-3">
            <Select value={state} onValueChange={(value) => { setPage(1); setState(value || "all"); }}>
              <SelectTrigger>
                <SelectValue placeholder={t("webhooks.statePlaceholder")} />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">{t("webhooks.allStates")}</SelectItem>
                <SelectItem value="pending">pending</SelectItem>
                <SelectItem value="processing">processing</SelectItem>
                <SelectItem value="retry">retry</SelectItem>
                <SelectItem value="delivered">delivered</SelectItem>
                <SelectItem value="dead">dead</SelectItem>
              </SelectContent>
            </Select>
            <Input
              placeholder="message.received"
              value={eventType}
              onChange={(e) => {
                setPage(1);
                setEventType(e.target.value);
              }}
            />
            <Input
              placeholder="https://example.com/hook"
              value={url}
              onChange={(e) => {
                setPage(1);
                setUrl(e.target.value);
              }}
            />
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <Webhook className="h-4 w-4 text-primary" />
              {t("webhooks.detail")}
            </CardTitle>
            <CardDescription>{t("webhooks.detailDesc")}</CardDescription>
          </CardHeader>
          <CardContent>
            {loading ? (
              <div className="space-y-3">
                {Array.from({ length: 8 }).map((_, i) => (
                  <Skeleton key={i} className="h-10 w-full" />
                ))}
              </div>
            ) : items.length === 0 ? (
              <div className="py-12 text-center text-sm text-muted-foreground">{t("webhooks.noDeliveries")}</div>
            ) : (
              <>
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>{t("webhooks.state")}</TableHead>
                      <TableHead>{t("webhooks.eventType")}</TableHead>
                      <TableHead>{t("webhooks.url")}</TableHead>
                      <TableHead>{t("webhooks.attempts")}</TableHead>
                      <TableHead>{t("webhooks.lastTried")}</TableHead>
                      <TableHead>{t("webhooks.createdAt")}</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {items.map((item) => (
                      <TableRow key={item.id}>
                        <TableCell>
                          <Badge variant="outline" className={stateStyles[item.state] || ""}>
                            {item.state}
                          </Badge>
                        </TableCell>
                        <TableCell className="font-mono text-xs">{item.event_type}</TableCell>
                        <TableCell className="max-w-[320px] truncate text-sm">{item.url}</TableCell>
                        <TableCell className="text-sm">{item.attempts}</TableCell>
                        <TableCell className="text-sm text-muted-foreground">
                          {item.last_tried_at
                            ? formatDistanceToNow(new Date(item.last_tried_at), { addSuffix: true })
                            : "—"}
                        </TableCell>
                        <TableCell className="text-sm text-muted-foreground">
                          {formatDistanceToNow(new Date(item.created_at), { addSuffix: true })}
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
                <div className="mt-4 space-y-2">
                  {items.filter((item) => item.last_error).slice(0, 5).map((item) => (
                    <div key={`${item.id}-err`} className="rounded-lg border border-rose-500/20 bg-rose-500/5 p-3 text-xs">
                      <div className="font-medium text-foreground">{item.url}</div>
                      <div className="mt-1 break-all text-muted-foreground">{item.last_error}</div>
                    </div>
                  ))}
                </div>
                <div className="mt-4 flex items-center justify-between">
                  <div className="text-xs text-muted-foreground">
                    {t("webhooks.pageOf", { page, total: totalPages })}
                  </div>
                  <div className="flex gap-2">
                    <Button variant="outline" size="sm" disabled={page <= 1} onClick={() => setPage(page - 1)}>
                      {t("webhooks.previous")}
                    </Button>
                    <Button variant="outline" size="sm" disabled={page >= totalPages} onClick={() => setPage(page + 1)}>
                      {t("webhooks.next")}
                    </Button>
                  </div>
                </div>
              </>
            )}
          </CardContent>
        </Card>
      </div>
    </div>
  );
}

function StatCard({ title, value }: { title: string; value: number }) {
  return (
    <Card>
      <CardHeader className="pb-2">
        <CardTitle className="text-sm font-medium text-muted-foreground">{title}</CardTitle>
      </CardHeader>
      <CardContent>
        <div className="text-3xl font-bold tracking-tight">{value.toLocaleString()}</div>
      </CardContent>
    </Card>
  );
}
