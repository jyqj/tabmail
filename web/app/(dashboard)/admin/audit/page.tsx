"use client";

import { useId, useReducer, useState } from "react";
import { formatDistanceToNow } from "date-fns";
import { ClipboardList, RefreshCw } from "lucide-react";

import { listAudit } from "@/lib/api";
import { useI18n } from "@/lib/i18n";
import { useSessionScope } from "@/lib/session";
import { useCRUDPage } from "@/hooks/use-crud-page";
import { useText } from "@/components/company/common";
import { ListReadFeedback, listPending, listReady, refreshList } from "@/components/crud/list-read-state";
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
  const scope = useSessionScope();
  return <AuditSession key={scope} />;
}

function AuditSession() {
  const { t } = useI18n();
  const text = useText();
  const reader = useId();
  const [page, setPage] = useState(1);
  // A return to an earlier page still requires a current read of that page.
  const [readGeneration, advanceRead] = useReducer((generation: number) => generation + 1, 0);
  const changePage = (nextPage: number) => { setPage(nextPage); advanceRead(); };
  const perPage = 30;

  const list = useCRUDPage(
    ["audit", reader, page, perPage, readGeneration],
    () => listAudit({ page, per_page: perPage }),
    "audit.loadFailed",
  );
  const loading = listPending(list);
  const ready = listReady(list);
  const response = ready ? list.data : undefined;
  const entries = response?.data ?? [];
  const total = response?.meta?.total ?? 0;

  const totalPages = Math.max(1, Math.ceil(total / perPage));

  return (
    <div className="flex flex-col">
      <PageHeader
        title={t("audit.title")}
        description={t("audit.desc")}
        actions={
          <Button variant="outline" size="sm" className="gap-1.5" disabled={!ready} onClick={() => refreshList(list)}>
            <RefreshCw className="h-3.5 w-3.5" />
            {text("刷新", "Refresh")}
          </Button>
        }
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
            <ListReadFeedback list={list} />
            {loading ? (
              <div className="space-y-3">
                {Array.from({ length: 6 }).map((_, i) => (
                  <Skeleton key={i} className="h-10 w-full" />
                ))}
              </div>
            ) : !ready ? null : entries.length === 0 ? (
              <div className="text-center py-12 text-muted-foreground">
                <ClipboardList className="h-10 w-10 mx-auto mb-3 opacity-30" />
                <p className="text-sm">{t("audit.noEntries")}</p>
              </div>
            ) : (
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
            )}
            {ready && (totalPages > 1 || page > 1) && (
              <nav aria-label={text("分页", "Pagination")} className="flex items-center justify-between mt-4">
                <div className="text-xs text-muted-foreground">
                  {total.toLocaleString()} {text("条记录", "total")}
                </div>
                <div className="flex gap-2">
                  <Button variant="outline" size="sm" disabled={page <= 1} onClick={() => changePage(page - 1)}>
                    {t("audit.previous")}
                  </Button>
                  <Button variant="outline" size="sm" disabled={page >= totalPages} onClick={() => changePage(page + 1)}>
                    {t("audit.next")}
                  </Button>
                </div>
              </nav>
            )}
          </CardContent>
        </Card>
      </div>
    </div>
  );
}
