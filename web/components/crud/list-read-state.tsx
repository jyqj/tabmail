"use client";

import { LoadError, useText } from "@/components/company/common";

type ListRead = {
  data: unknown;
  error: unknown;
  isLoading: boolean;
  isValidating: boolean;
  mutate: () => Promise<unknown>;
};

export function listPending(list: ListRead) {
  return list.isLoading || list.isValidating || (list.data === undefined && !list.error);
}

export function listReady(list: ListRead) {
  return list.data !== undefined && !list.error && !listPending(list);
}

export function refreshList(list: ListRead) {
  // SWR retains its error for the persistent retry UI. A failed read must not
  // become an unhandled button promise or authorize the cached response.
  if (!list.isValidating) void list.mutate().catch(() => undefined);
}

export function ListReadFeedback({ list }: { list: ListRead }) {
  const t = useText();
  const pending = listPending(list);
  return <>
    <fieldset disabled={pending}>
      <LoadError error={list.error} onRetry={() => refreshList(list)} />
    </fieldset>
    {pending && <p role="status" className="mb-3 text-sm text-muted-foreground">
      {list.data === undefined ? t("正在加载记录…", "Loading records…") : t("正在刷新记录…", "Refreshing records…")}
    </p>}
  </>;
}
