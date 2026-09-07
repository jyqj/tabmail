"use client";
import { useCallback, useMemo, useState, type FormEvent } from "react";
import { useAuth } from "@/contexts/auth-context";
import { useI18n } from "@/lib/i18n";
import {
  listIngress,
  type IngressFilters,
  type IngressSession,
} from "@/lib/api/ingress-recovery";
import { PageHeader } from "@/components/layout/page-header";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  ingressCopy,
  localTime,
  receiptStateLabel,
} from "@/components/ingress/copy";
import {
  IngressInspectionPanel,
  RecoveryError,
} from "@/components/ingress/inspection";
import { useInspectionResource } from "@/components/ingress/use-inspection-resource";

export default function AdminIngestPage() {
  const { level, hydrated, user, accessToken, tenantId } = useAuth();
  const { locale } = useI18n();
  const session = useMemo(
    () => ({
      userId: user?.id ?? "",
      accessToken: accessToken ?? "",
      tenantId,
    }),
    [user?.id, accessToken, tenantId],
  );
  // The layout's redirect is not sufficient: do not mount operator fetchers for
  // a company admin, or leave a previous account's inspector/confirmation visible.
  if (!hydrated)
    return (
      <p role="status" className="p-6">
        {ingressCopy(locale).loading}
      </p>
    );
  if (level !== "super_admin" || !session.userId || !session.accessToken)
    return (
      <p role="alert" className="p-6">
        {ingressCopy(locale).restricted}
      </p>
    );
  return <OperatorConsole key={JSON.stringify(session)} session={session} />;
}

function OperatorConsole({ session }: { session: IngressSession }) {
  const { locale } = useI18n();
  const copy = ingressCopy(locale);
  const [filters, setFilters] = useState<IngressFilters>({
    page: 1,
    per_page: 30,
  });
  const [state, setState] = useState("");
  const [source, setSource] = useState("");
  const [recipient, setRecipient] = useState("");
  const [selected, setSelected] = useState("");
  const load = useCallback(
    (signal: AbortSignal) => listIngress(session, filters, signal),
    [session, filters],
  );
  const query = useInspectionResource(JSON.stringify(filters), load);
  const jobs = query.data?.data ?? [];
  const total = query.data?.meta.total ?? 0;
  const pages = Math.max(1, Math.ceil(total / filters.per_page));
  function apply(event: FormEvent) {
    event.preventDefault();
    setSelected("");
    setFilters({
      page: 1,
      per_page: 30,
      state,
      source,
      recipient: recipient.trim(),
    });
    query.refresh();
  }
  function page(number: number) {
    setSelected("");
    setFilters({ ...filters, page: number });
  }
  return (
    <main>
      <PageHeader
        title={copy.title}
        description={copy.description}
        actions={
          <Button variant="outline" onClick={query.refresh}>
            {copy.refresh}
          </Button>
        }
      />
      <div className="space-y-5 p-4 md:p-6">
        <p className="rounded-lg border p-3 text-sm text-muted-foreground">
          {copy.retention}
        </p>
        <form
          onSubmit={apply}
          className="grid items-end gap-3 rounded-xl border p-4 md:grid-cols-[1fr_1fr_2fr_auto]"
        >
          <label className="space-y-1 text-sm">
            <span className="block">{copy.state}</span>
            <select
              aria-label={copy.state}
              className="w-full rounded border bg-background p-2"
              value={state}
              onChange={(e) => setState(e.target.value)}
            >
              <option value="">{copy.all}</option>
              {["pending", "processing", "retry", "done", "dead"].map((s) => (
                <option key={s} value={s}>
                  {receiptStateLabel(s, copy)}
                </option>
              ))}
            </select>
          </label>
          <label className="space-y-1 text-sm">
            <span className="block">{copy.source}</span>
            <select
              aria-label={copy.source}
              className="w-full rounded border bg-background p-2"
              value={source}
              onChange={(e) => setSource(e.target.value)}
            >
              <option value="">{copy.anySource}</option>
              <option value="smtp">{copy.smtp}</option>
            </select>
          </label>
          <label className="space-y-1 text-sm">
            <span className="block">{copy.recipient}</span>
            <Input
              aria-label={copy.recipient}
              value={recipient}
              onChange={(e) => setRecipient(e.target.value)}
              placeholder="recipient@example.test"
            />
          </label>
          <Button type="submit">{copy.apply}</Button>
        </form>
        {query.error ? (
          <RecoveryError
            error={query.error}
            refresh={query.refresh}
            copy={copy}
          />
        ) : (
          <>
            {query.loading ? (
              <p role="status">{copy.loading}</p>
            ) : (
              <>
                <dl className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
                  {[
                    [copy.total, total],
                    [copy.pageCount, jobs.length],
                    [
                      copy.processingCount,
                      jobs.filter((j) => j.state === "processing").length,
                    ],
                    [
                      copy.heldCount,
                      jobs.filter((j) => j.state === "dead").length,
                    ],
                  ].map(([name, value]) => (
                    <div key={name} className="rounded-lg border p-4">
                      <dt className="text-xs text-muted-foreground">{name}</dt>
                      <dd className="mt-1 text-2xl font-semibold">{value}</dd>
                    </div>
                  ))}
                </dl>
                {jobs.length === 0 ? (
                  <p className="p-6 text-center">{copy.empty}</p>
                ) : (
                  <div className="overflow-x-auto rounded-xl border">
                    <table className="w-full text-left text-sm">
                      <thead>
                        <tr className="border-b bg-muted/40">
                          {[
                            copy.state,
                            copy.sender,
                            copy.destinations,
                            copy.attempts,
                            copy.created,
                            copy.inspect,
                          ].map((title) => (
                            <th scope="col" key={title} className="p-3">
                              {title}
                            </th>
                          ))}
                        </tr>
                      </thead>
                      <tbody>
                        {jobs.map((job) => (
                          <tr
                            key={job.id}
                            className="border-b align-top last:border-0"
                          >
                            <td className="p-3 whitespace-nowrap">
                              {receiptStateLabel(job.state, copy)}
                            </td>
                            <td className="max-w-xs break-words p-3">
                              {job.mail_from || "—"}
                            </td>
                            <td className="max-w-sm break-words p-3">
                              {job.recipients.join(", ")}
                            </td>
                            <td className="p-3">{job.attempts}</td>
                            <td className="p-3 whitespace-nowrap">
                              {localTime(job.created_at, locale)}
                            </td>
                            <td className="p-3">
                              <Button
                                variant="outline"
                                size="sm"
                                aria-label={`${copy.inspect} ${job.id}`}
                                onClick={() => setSelected(job.id)}
                              >
                                {copy.inspect}
                              </Button>
                            </td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                )}
                <nav
                  aria-label={copy.page}
                  className="flex flex-wrap items-center justify-between gap-3 text-sm"
                >
                  <span>
                    {copy.page} {filters.page} / {pages} · {total}
                  </span>
                  <div className="flex gap-2">
                    <Button
                      variant="outline"
                      disabled={filters.page <= 1}
                      onClick={() => page(filters.page - 1)}
                    >
                      {copy.previous}
                    </Button>
                    <Button
                      variant="outline"
                      disabled={filters.page >= pages}
                      onClick={() => page(filters.page + 1)}
                    >
                      {copy.next}
                    </Button>
                  </div>
                </nav>
              </>
            )}
            {selected && (
              <IngressInspectionPanel
                key={selected}
                id={selected}
                session={session}
                onChanged={query.refresh}
                onClose={() => setSelected("")}
              />
            )}
          </>
        )}
      </div>
    </main>
  );
}
