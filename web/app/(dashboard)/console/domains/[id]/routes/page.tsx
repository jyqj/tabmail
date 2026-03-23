"use client";

import { useState, useEffect, useCallback } from "react";
import { useParams } from "next/navigation";
import { PageHeader } from "@/components/layout/page-header";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Badge } from "@/components/ui/badge";
import { Switch } from "@/components/ui/switch";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { listRoutes, createRoute, deleteRoute } from "@/lib/api";
import type { DomainRoute, RouteType, AccessMode } from "@/lib/types";
import { Plus, Trash2, Route } from "lucide-react";
import { toast } from "sonner";
import { formatDistanceToNow } from "date-fns";
import { useI18n } from "@/lib/i18n";

const routeTypeColors: Record<RouteType, string> = {
  exact: "bg-blue-100 text-blue-800 dark:bg-blue-900/30 dark:text-blue-400",
  wildcard: "bg-purple-100 text-purple-800 dark:bg-purple-900/30 dark:text-purple-400",
  deep_wildcard: "bg-fuchsia-100 text-fuchsia-800 dark:bg-fuchsia-900/30 dark:text-fuchsia-400",
  sequence: "bg-amber-100 text-amber-800 dark:bg-amber-900/30 dark:text-amber-400",
};

export default function RoutesPage() {
  const { t } = useI18n();
  const params = useParams();
  const domainId = params.id as string;

  const [routes, setRoutes] = useState<DomainRoute[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(true);
  const [dialogOpen, setDialogOpen] = useState(false);
  const [creating, setCreating] = useState(false);

  const [routeType, setRouteType] = useState<RouteType>("wildcard");
  const [matchValue, setMatchValue] = useState("");
  const [rangeStart, setRangeStart] = useState("");
  const [rangeEnd, setRangeEnd] = useState("");
  const [autoCreate, setAutoCreate] = useState(true);
  const [retentionHoursOverride, setRetentionHoursOverride] = useState("");
  const [accessMode, setAccessMode] = useState<AccessMode>("public");

  const fetchRoutes = useCallback(async () => {
    try {
      const res = await listRoutes(domainId);
      setRoutes(res.data);
      setTotal(res.data.length);
    } catch {
      toast.error(t("routes.loadFailed"));
    } finally {
      setLoading(false);
    }
  }, [domainId]);

  useEffect(() => {
    fetchRoutes();
  }, [fetchRoutes]);

  const handleCreate = async () => {
    if (!matchValue.trim()) return;
    const sequenceStart = Number(rangeStart);
    const sequenceEnd = Number(rangeEnd);
    const retentionHours = Number(retentionHoursOverride);
    if (
      routeType === "sequence" &&
      (!rangeStart.trim() ||
        !rangeEnd.trim() ||
        Number.isNaN(sequenceStart) ||
        Number.isNaN(sequenceEnd) ||
        sequenceStart > sequenceEnd)
    ) {
      toast.error(t("routes.seqError"));
      return;
    }
    if (
      retentionHoursOverride.trim() &&
      (Number.isNaN(retentionHours) || retentionHours <= 0)
    ) {
      toast.error(t("routes.retentionError"));
      return;
    }
    setCreating(true);
    try {
      await createRoute(domainId, {
        route_type: routeType,
        match_value: matchValue.trim(),
        range_start: routeType === "sequence" ? sequenceStart : undefined,
        range_end: routeType === "sequence" ? sequenceEnd : undefined,
        auto_create_mailbox: autoCreate,
        retention_hours_override: retentionHoursOverride.trim() ? retentionHours : undefined,
        access_mode_default: accessMode,
      });
      setMatchValue("");
      setRangeStart("");
      setRangeEnd("");
      setRetentionHoursOverride("");
      setDialogOpen(false);
      toast.success(t("routes.routeCreated"));
      fetchRoutes();
    } catch (e: unknown) {
      const err = e as { error?: { message?: string } };
      toast.error(err?.error?.message || t("routes.createFailed"));
    } finally {
      setCreating(false);
    }
  };

  const handleDelete = async (routeId: string) => {
    try {
      await deleteRoute(domainId, routeId);
      toast.success(t("routes.deleted"));
      fetchRoutes();
    } catch {
      toast.error(t("routes.deleteFailed"));
    }
  };

  return (
    <div className="flex flex-col">
      <PageHeader
        title={t("routes.title")}
        description={t("routes.count", { count: total })}
        actions={
          <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
            <DialogTrigger render={<Button size="sm" className="gap-1.5" />}>
              <Plus className="h-3.5 w-3.5" />
              {t("routes.addRoute")}
            </DialogTrigger>
            <DialogContent className="sm:max-w-lg">
              <DialogHeader>
                <DialogTitle>{t("routes.addTitle")}</DialogTitle>
                <DialogDescription>
                  {t("routes.addDesc")}
                </DialogDescription>
              </DialogHeader>
              <div className="space-y-4 py-4">
                <div className="space-y-2">
                  <Label>{t("routes.routeType")}</Label>
                  <Select value={routeType} onValueChange={(v) => setRouteType(v as RouteType)}>
                    <SelectTrigger>
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="exact">{t("routes.exact")}</SelectItem>
                      <SelectItem value="wildcard">{t("routes.wildcard")}</SelectItem>
                      <SelectItem value="deep_wildcard">{t("routes.deepWildcard")}</SelectItem>
                      <SelectItem value="sequence">{t("routes.sequence")}</SelectItem>
                    </SelectContent>
                  </Select>
                </div>
                <div className="space-y-2">
                  <Label>{t("routes.matchValue")}</Label>
                  <Input
                    placeholder={
                      routeType === "exact"
                        ? "user@mail.example.com"
                        : routeType === "wildcard"
                        ? "*.mail.example.com"
                        : routeType === "deep_wildcard"
                        ? "**.mail.example.com"
                        : "box-{n}.mail.example.com"
                    }
                    value={matchValue}
                    onChange={(e) => setMatchValue(e.target.value)}
                  />
                  <p className="text-xs text-muted-foreground">
                    {routeType === "deep_wildcard"
                      ? t("routes.deepWildcardHint")
                      : t("routes.matchHint")}
                  </p>
                </div>
                {routeType === "sequence" && (
                  <div className="grid grid-cols-2 gap-4">
                    <div className="space-y-2">
                      <Label>{t("routes.rangeStart")}</Label>
                      <Input
                        type="number"
                        placeholder="1"
                        value={rangeStart}
                        onChange={(e) => setRangeStart(e.target.value)}
                      />
                    </div>
                    <div className="space-y-2">
                      <Label>{t("routes.rangeEnd")}</Label>
                      <Input
                        type="number"
                        placeholder="5000"
                        value={rangeEnd}
                        onChange={(e) => setRangeEnd(e.target.value)}
                      />
                    </div>
                  </div>
                )}
                <div className="flex items-center justify-between">
                  <Label htmlFor="auto-create">{t("routes.autoCreate")}</Label>
                  <Switch
                    id="auto-create"
                    checked={autoCreate}
                    onCheckedChange={setAutoCreate}
                  />
                </div>
                <div className="space-y-2">
                  <Label>{t("routes.retentionHoursOverride")}</Label>
                  <Input
                    type="number"
                    min="1"
                    placeholder={t("routes.inheritDefault")}
                    value={retentionHoursOverride}
                    onChange={(e) => setRetentionHoursOverride(e.target.value)}
                  />
                </div>
                <div className="space-y-2">
                  <Label>{t("routes.defaultAccess")}</Label>
                  <Select value={accessMode} onValueChange={(v) => setAccessMode(v as AccessMode)}>
                    <SelectTrigger>
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="public">{t("routes.public")}</SelectItem>
                      <SelectItem value="token">{t("routes.token")}</SelectItem>
                      <SelectItem value="api_key">{t("routes.apiKey")}</SelectItem>
                    </SelectContent>
                  </Select>
                </div>
              </div>
              <DialogFooter>
                <Button onClick={handleCreate} disabled={creating || !matchValue.trim()}>
                  {creating ? t("routes.creating") : t("routes.create")}
                </Button>
              </DialogFooter>
            </DialogContent>
          </Dialog>
        }
      />

      <div className="p-4">
        <Card>
          <CardHeader className="pb-3">
            <CardTitle className="text-base">{t("routes.domainRoutes")}</CardTitle>
            <CardDescription>
              {t("routes.routesDesc")}
            </CardDescription>
          </CardHeader>
          <CardContent>
            {loading ? (
              <div className="space-y-3">
                {Array.from({ length: 3 }).map((_, i) => (
                  <Skeleton key={i} className="h-12 w-full" />
                ))}
              </div>
            ) : routes.length === 0 ? (
              <div className="text-center py-12 text-muted-foreground">
                <Route className="h-10 w-10 mx-auto mb-3 opacity-30" />
                <p className="text-sm">{t("routes.noRoutes")}</p>
                <p className="text-xs mt-1">{t("routes.noRoutesHint")}</p>
              </div>
            ) : (
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>{t("routes.type")}</TableHead>
                    <TableHead>{t("routes.matchValue")}</TableHead>
                    <TableHead>{t("routes.range")}</TableHead>
                    <TableHead>{t("routes.access")}</TableHead>
                    <TableHead>{t("routes.autoCreateCol")}</TableHead>
                    <TableHead>{t("routes.retention")}</TableHead>
                    <TableHead>{t("routes.created")}</TableHead>
                    <TableHead className="w-10" />
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {routes.map((route) => (
                    <TableRow key={route.id}>
                      <TableCell>
                        <Badge
                          variant="secondary"
                          className={routeTypeColors[route.route_type]}
                        >
                          {route.route_type}
                        </Badge>
                      </TableCell>
                      <TableCell>
                        <code className="text-sm">{route.match_value}</code>
                      </TableCell>
                      <TableCell className="text-sm text-muted-foreground">
                        {route.range_start != null && route.range_end != null
                          ? `${route.range_start}–${route.range_end}`
                          : "—"}
                      </TableCell>
                      <TableCell>
                        <Badge variant="outline" className="text-xs">
                          {route.access_mode_default}
                        </Badge>
                      </TableCell>
                      <TableCell className="text-sm">
                        {route.auto_create_mailbox ? t("routes.yes") : t("routes.no")}
                      </TableCell>
                      <TableCell className="text-sm text-muted-foreground">
                        {route.retention_hours_override != null
                          ? `${route.retention_hours_override}h`
                          : t("routes.inheritDefault")}
                      </TableCell>
                      <TableCell className="text-sm text-muted-foreground">
                        {formatDistanceToNow(new Date(route.created_at), {
                          addSuffix: true,
                        })}
                      </TableCell>
                      <TableCell>
                        <Button
                          variant="ghost"
                          size="icon"
                          className="h-8 w-8 text-destructive hover:text-destructive hover:bg-destructive/10"
                          onClick={() => handleDelete(route.id)}
                        >
                          <Trash2 className="h-4 w-4" />
                        </Button>
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            )}
          </CardContent>
        </Card>
      </div>
    </div>
  );
}
