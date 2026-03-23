"use client";

import { useState, useEffect, useCallback } from "react";
import { formatDistanceToNow } from "date-fns";
import { ClipboardList } from "lucide-react";
import { toast } from "sonner";

import { listAudit } from "@/lib/api";
import type { AuditEntry } from "@/lib/types";
import { useI18n } from "@/lib/i18n";
import { PageHeader } from "@/components/layout/page-header";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";

export default function AuditPage() {
  const { t } = useI18n();
  const [entries, setEntries] = useState<AuditEntry[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [loading, setLoading] = useState(true);

  const fetchAudit = useCallback(async () => {
    try {
      const res = await listAudit({ page, per_page: 30 });
      setEntries(res.data);
      setTotal(res.meta.total);
    } catch {
      toast.error(t("audit.loadFailed"));
    } finally {
      setLoading(false);
    }
  }, [page, t]);

  useEffect(() => {
    fetchAudit();
  }, [fetchAudit]);

  const totalPages = Math.ceil(total / 30);

  return (
    <div className="flex flex-col">
      <PageHeader
        title={t("audit.title")}
        description={t("audit.desc")}
      />

      <div className="p-4">
        <Card>
          <CardHeader className="pb-3">
            <CardTitle className="flex items-center gap-2 text-base">
              <ClipboardList className="h-4 w-4 text-primary" />
              {t("audit.title")}
            </CardTitle>
            <CardDescription>{t("audit.desc")}</CardDescription>
          </CardHeader>
          <CardContent>
            {loading ? (
              <div className="space-y-3">
                {Array.from({ length: 6 }).map((_, i) => (
                  <Skeleton key={i} className="h-10 w-full" />
                ))}
              </div>
            ) : entries.length === 0 ? (
              <div className="text-center py-12 text-muted-foreground">
                <ClipboardList className="h-10 w-10 mx-auto mb-3 opacity-30" />
                <p className="text-sm">{t("audit.noEntries")}</p>
              </div>
            ) : (
              <>
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>{t("audit.action")}</TableHead>
                      <TableHead>{t("audit.actor")}</TableHead>
                      <TableHead>{t("audit.resourceType")}</TableHead>
                      <TableHead>{t("audit.resourceId")}</TableHead>
                      <TableHead>{t("audit.time")}</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {entries.map((entry) => (
                      <TableRow key={entry.id}>
                        <TableCell>
                          <Badge variant="outline" className="font-mono text-[11px]">
                            {entry.action}
                          </Badge>
                        </TableCell>
                        <TableCell className="text-sm text-muted-foreground">
                          {entry.actor || t("audit.system")}
                        </TableCell>
                        <TableCell className="text-sm">{entry.resource_type}</TableCell>
                        <TableCell className="text-sm font-mono text-muted-foreground max-w-[200px] truncate">
                          {entry.resource_id || "—"}
                        </TableCell>
                        <TableCell className="text-sm text-muted-foreground">
                          {formatDistanceToNow(new Date(entry.created_at), { addSuffix: true })}
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>

                {totalPages > 1 && (
                  <div className="flex items-center justify-between mt-4">
                    <div className="text-xs text-muted-foreground">
                      {total.toLocaleString()} total
                    </div>
                    <div className="flex gap-2">
                      <Button
                        variant="outline"
                        size="sm"
                        disabled={page <= 1}
                        onClick={() => setPage((p) => p - 1)}
                      >
                        {t("audit.previous")}
                      </Button>
                      <Button
                        variant="outline"
                        size="sm"
                        disabled={page >= totalPages}
                        onClick={() => setPage((p) => p + 1)}
                      >
                        {t("audit.next")}
                      </Button>
                    </div>
                  </div>
                )}
              </>
            )}
          </CardContent>
        </Card>
      </div>
    </div>
  );
}
