"use client";

import type { ComponentType, ReactNode } from "react";
import {
  Table,
  TableBody,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Skeleton } from "@/components/ui/skeleton";
import { Button } from "@/components/ui/button";

export interface DataTableColumn {
  key: string;
  header?: ReactNode;
  className?: string;
}

interface DataTableProps {
  columns: DataTableColumn[];
  loading?: boolean;
  isEmpty: boolean;
  emptyIcon?: ComponentType<{ className?: string }>;
  emptyText: ReactNode;
  emptyHint?: ReactNode;
  skeletonRows?: number;
  /** Table rows (`<TableRow>` elements). */
  children: ReactNode;
}

/**
 * Shared CRUD list-table skeleton: loading skeletons, an empty state, and a
 * header row, so pages only render their own `<TableRow>`s.
 */
export function DataTable({
  columns,
  loading = false,
  isEmpty,
  emptyIcon: EmptyIcon,
  emptyText,
  emptyHint,
  skeletonRows = 3,
  children,
}: DataTableProps) {
  if (loading) {
    return (
      <div className="space-y-3">
        {Array.from({ length: skeletonRows }).map((_, i) => (
          <Skeleton key={i} className="h-12 w-full" />
        ))}
      </div>
    );
  }

  if (isEmpty) {
    return (
      <div className="text-center py-12 text-muted-foreground">
        {EmptyIcon && <EmptyIcon className="h-10 w-10 mx-auto mb-3 opacity-30" />}
        <p className="text-sm">{emptyText}</p>
        {emptyHint && <p className="text-xs mt-1">{emptyHint}</p>}
      </div>
    );
  }

  return (
    <div className="overflow-x-auto">
      <Table>
        <TableHeader>
          <TableRow>
            {columns.map((col) => (
              <TableHead key={col.key} className={col.className}>
                {col.header}
              </TableHead>
            ))}
          </TableRow>
        </TableHeader>
        <TableBody>{children}</TableBody>
      </Table>
    </div>
  );
}

interface DataTablePaginationProps {
  page: number;
  perPage: number;
  total: number;
  onPageChange: (page: number) => void;
  label: ReactNode;
  previousText: ReactNode;
  nextText: ReactNode;
}

export function DataTablePagination({
  page,
  perPage,
  total,
  onPageChange,
  label,
  previousText,
  nextText,
}: DataTablePaginationProps) {
  return (
    <div className="flex items-center justify-between pt-3">
      <div className="text-xs text-muted-foreground">{label}</div>
      <div className="flex gap-2">
        <Button
          variant="outline"
          size="sm"
          disabled={page <= 1}
          onClick={() => onPageChange(page - 1)}
        >
          {previousText}
        </Button>
        <Button
          variant="outline"
          size="sm"
          disabled={page * perPage >= total}
          onClick={() => onPageChange(page + 1)}
        >
          {nextText}
        </Button>
      </div>
    </div>
  );
}
