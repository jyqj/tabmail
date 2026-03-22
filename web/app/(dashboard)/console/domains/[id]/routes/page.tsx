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

const routeTypeColors: Record<RouteType, string> = {
  exact: "bg-blue-100 text-blue-800 dark:bg-blue-900/30 dark:text-blue-400",
  wildcard: "bg-purple-100 text-purple-800 dark:bg-purple-900/30 dark:text-purple-400",
  sequence: "bg-amber-100 text-amber-800 dark:bg-amber-900/30 dark:text-amber-400",
};

export default function RoutesPage() {
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
  const [accessMode, setAccessMode] = useState<AccessMode>("public");

  const fetchRoutes = useCallback(async () => {
    try {
      const res = await listRoutes(domainId);
      setRoutes(res.data);
      setTotal(res.data.length);
    } catch {
      toast.error("Failed to load routes");
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
    if (
      routeType === "sequence" &&
      (!rangeStart.trim() ||
        !rangeEnd.trim() ||
        Number.isNaN(sequenceStart) ||
        Number.isNaN(sequenceEnd) ||
        sequenceStart > sequenceEnd)
    ) {
      toast.error("Sequence routes require a valid start/end range");
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
        access_mode_default: accessMode,
      });
      setMatchValue("");
      setRangeStart("");
      setRangeEnd("");
      setDialogOpen(false);
      toast.success("Route created");
      fetchRoutes();
    } catch (e: unknown) {
      const err = e as { error?: { message?: string } };
      toast.error(err?.error?.message || "Failed to create route");
    } finally {
      setCreating(false);
    }
  };

  const handleDelete = async (routeId: string) => {
    try {
      await deleteRoute(domainId, routeId);
      toast.success("Route deleted");
      fetchRoutes();
    } catch {
      toast.error("Failed to delete");
    }
  };

  return (
    <div className="flex flex-col">
      <PageHeader
        title="Routes"
        description={`${total} route${total !== 1 ? "s" : ""} for this domain`}
        actions={
          <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
            <DialogTrigger render={<Button size="sm" className="gap-1.5" />}>
              <Plus className="h-3.5 w-3.5" />
              Add Route
            </DialogTrigger>
            <DialogContent className="sm:max-w-lg">
              <DialogHeader>
                <DialogTitle>Add Route</DialogTitle>
                <DialogDescription>
                  Define a routing rule to match incoming email addresses.
                </DialogDescription>
              </DialogHeader>
              <div className="space-y-4 py-4">
                <div className="space-y-2">
                  <Label>Route Type</Label>
                  <Select value={routeType} onValueChange={(v) => setRouteType(v as RouteType)}>
                    <SelectTrigger>
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="exact">Exact &mdash; single address</SelectItem>
                      <SelectItem value="wildcard">Wildcard &mdash; *.domain</SelectItem>
                      <SelectItem value="sequence">Sequence &mdash; prefix-&#123;n&#125;.domain</SelectItem>
                    </SelectContent>
                  </Select>
                </div>
                <div className="space-y-2">
                  <Label>Match Value</Label>
                  <Input
                    placeholder={
                      routeType === "exact"
                        ? "user@mail.example.com"
                        : routeType === "wildcard"
                        ? "*.mail.example.com"
                        : "box-{n}.mail.example.com"
                    }
                    value={matchValue}
                    onChange={(e) => setMatchValue(e.target.value)}
                  />
                </div>
                {routeType === "sequence" && (
                  <div className="grid grid-cols-2 gap-4">
                    <div className="space-y-2">
                      <Label>Range Start</Label>
                      <Input
                        type="number"
                        placeholder="1"
                        value={rangeStart}
                        onChange={(e) => setRangeStart(e.target.value)}
                      />
                    </div>
                    <div className="space-y-2">
                      <Label>Range End</Label>
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
                  <Label htmlFor="auto-create">Auto-create mailbox on receive</Label>
                  <Switch
                    id="auto-create"
                    checked={autoCreate}
                    onCheckedChange={setAutoCreate}
                  />
                </div>
                <div className="space-y-2">
                  <Label>Default Access Mode</Label>
                  <Select value={accessMode} onValueChange={(v) => setAccessMode(v as AccessMode)}>
                    <SelectTrigger>
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="public">Public</SelectItem>
                      <SelectItem value="token">Token</SelectItem>
                      <SelectItem value="api_key">API Key</SelectItem>
                    </SelectContent>
                  </Select>
                </div>
              </div>
              <DialogFooter>
                <Button onClick={handleCreate} disabled={creating || !matchValue.trim()}>
                  {creating ? "Creating..." : "Create"}
                </Button>
              </DialogFooter>
            </DialogContent>
          </Dialog>
        }
      />

      <div className="p-4">
        <Card>
          <CardHeader className="pb-3">
            <CardTitle className="text-base">Domain Routes</CardTitle>
            <CardDescription>
              Rules that map incoming email addresses to mailboxes.
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
                <p className="text-sm">No routes yet</p>
                <p className="text-xs mt-1">Add a route to start matching emails</p>
              </div>
            ) : (
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Type</TableHead>
                    <TableHead>Match Value</TableHead>
                    <TableHead>Range</TableHead>
                    <TableHead>Access</TableHead>
                    <TableHead>Auto-create</TableHead>
                    <TableHead>Created</TableHead>
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
                        {route.auto_create_mailbox ? "Yes" : "No"}
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
