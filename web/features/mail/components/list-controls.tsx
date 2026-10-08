"use client";
import { useState } from "react";
import { ActionButton, Field, inputClass, LoadError, useText } from "@/components/company/common";
type ListRead = {
    data: unknown;
    error: unknown;
    isLoading: boolean;
    isValidating: boolean;
    mutate: () => Promise<unknown>;
};
export function listReady(list: ListRead) {
    return list.data !== undefined && !list.error && !list.isLoading && !list.isValidating;
}
// Feedback is separate from the rows: refreshing must not unmount selected
// readers or reset their disclosure state. Empty results and totals require a
// settled successful read, while a failed read must keep cached rows hidden.
export function ListFeedback({ list, loading, refreshing, empty }: {
    list: ListRead;
    loading: string;
    refreshing: string;
    empty?: string;
}) {
    if (list.isLoading || list.isValidating || (list.data === undefined && !list.error))
        return <p role="status" className="text-sm text-muted-foreground">{list.data === undefined ? loading : refreshing}</p>;
    if (list.error)
        return <LoadError error={list.error} onRetry={() => {
            if (!list.isValidating) void list.mutate().catch(() => undefined);
        }}/>;
    return listReady(list) && empty ? <p role="status" className="text-sm text-muted-foreground">{empty}</p> : null;
}
export function Pager({ page, total, onPage }: {
    page: number;
    total: number;
    onPage: (page: number) => void;
}) {
    const t = useText();
    return <nav aria-label={t("分页", "Pagination")} className="flex items-center justify-center gap-3 py-3">
  <ActionButton disabled={page <= 1} onClick={() => onPage(page - 1)}>{t("上一页", "Previous")}</ActionButton>
  <span className="text-sm">{page} / {Math.max(1, Math.ceil(total / 30))} · {total}</span>
  <ActionButton disabled={page * 30 >= total} onClick={() => onPage(page + 1)}>{t("下一页", "Next")}</ActionButton>
 </nav>;
}
export function SearchMail({ value, onSearch }: {
    value: string;
    onSearch: (q: string) => void;
}) {
    const t = useText();
    const [query, setQuery] = useState(value);
    return <form className="flex items-end gap-2" onSubmit={e => { e.preventDefault(); onSearch(query.trim()); }}>
  <div className="flex-1"><Field label={t("搜索主题、地址和已索引正文", "Search subject, addresses and indexed body")}>{id => <input id={id} className={inputClass} value={query} maxLength={200} onChange={e => setQuery(e.target.value)}/>}</Field></div>
  <ActionButton type="submit">{t("搜索", "Search")}</ActionButton>
 </form>;
}
