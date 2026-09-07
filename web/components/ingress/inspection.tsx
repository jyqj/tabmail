"use client";
import { useCallback, useEffect, useRef, useState } from "react";
import { useI18n } from "@/lib/i18n";
import {
  canRetryInspection,
  IngressRequestError,
  inspectIngress,
  isIngressSessionCurrent,
  retryIngress,
  type IngressInspection,
  type IngressSession,
} from "@/lib/api/ingress-recovery";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import {
  ingressCopy,
  localTime,
  receiptStateLabel,
  type IngressCopy,
} from "./copy";
import { useInspectionResource } from "./use-inspection-resource";

export function RecoveryError({
  error,
  refresh,
  copy,
}: {
  error: unknown;
  refresh: () => void;
  copy: IngressCopy;
}) {
  const sessionError =
    error instanceof IngressRequestError && [401, 403].includes(error.status);
  return (
    <div className="space-y-3 rounded border border-destructive/40 p-4">
      <p role="alert">{sessionError ? copy.session : copy.loadFailure}</p>
      {!sessionError && (
        <Button variant="outline" onClick={refresh}>
          {copy.refresh}
        </Button>
      )}
    </div>
  );
}

export function IngressInspectionPanel({
  id,
  session,
  onChanged,
  onClose,
}: {
  id: string;
  session: IngressSession;
  onChanged: () => void;
  onClose: () => void;
}) {
  const { locale } = useI18n();
  const copy = ingressCopy(locale);
  const load = useCallback(
    (signal: AbortSignal) => inspectIngress(session, id, signal),
    [session, id],
  );
  const snapshot = useInspectionResource(id, load);
  const [notice, setNotice] = useState<
    "submitted" | "conflict" | "uncertain" | "session" | null
  >(null);
  function attempted(
    outcome: "submitted" | "conflict" | "uncertain" | "session",
  ) {
    setNotice(outcome);
    snapshot.refresh();
    onChanged();
  }
  const receipt = snapshot.data?.data;
  return (
    <section
      className="space-y-4 rounded-xl border p-4 md:p-6"
      aria-label={copy.targets}
    >
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h2 className="font-semibold">{copy.targets}</h2>
        <div className="flex gap-2">
          <Button variant="outline" onClick={snapshot.refresh}>
            {copy.refresh}
          </Button>
          <Button variant="outline" onClick={onClose}>
            {copy.close}
          </Button>
        </div>
      </div>
      <p className="break-all font-mono text-xs">{id}</p>
      {notice && (
        <p role="status" className="rounded bg-muted p-3 text-sm">
          {copy[notice]}
        </p>
      )}
      {snapshot.error ? (
        <RecoveryError
          error={snapshot.error}
          refresh={snapshot.refresh}
          copy={copy}
        />
      ) : snapshot.loading ? (
        <p role="status">{copy.loading}</p>
      ) : receipt ? (
        <>
          <dl className="grid gap-3 text-sm sm:grid-cols-2">
            <div>
              <dt className="text-muted-foreground">{copy.state}</dt>
              <dd>{receiptStateLabel(receipt.state, copy)}</dd>
            </div>
            <div>
              <dt className="text-muted-foreground">{copy.updated}</dt>
              <dd>{localTime(receipt.updated_at, locale)}</dd>
            </div>
            <div>
              <dt className="text-muted-foreground">{copy.attempts}</dt>
              <dd>{receipt.attempts}</dd>
            </div>
            <div>
              <dt className="text-muted-foreground">{copy.bytes}</dt>
              <dd>
                {receipt.expected_bytes?.toLocaleString() ?? copy.unknown}
              </dd>
            </div>
          </dl>
          <p className="text-xs text-muted-foreground">{copy.metadataOnly}</p>
          {receipt.last_error && (
            <p className="break-words rounded border p-3 text-sm">
              {receipt.last_error}
            </p>
          )}
          {receipt.targets.length === 0 && <p>{copy.noTargets}</p>}
          <ul className="space-y-3">
            {receipt.targets.map((target) => (
              <li
                key={target.mailbox_id}
                className="space-y-2 rounded border p-3"
              >
                <div className="flex flex-wrap justify-between gap-3">
                  <strong className="break-all">{target.address}</strong>
                  <span>
                    {target.state === "delivered"
                      ? copy.delivered
                      : target.state === "held"
                        ? copy.held
                        : copy.targetPending}
                  </span>
                </div>
                <p className="text-xs text-muted-foreground break-all">
                  {copy.tenant}: {target.tenant_id} · {copy.mailbox}:{" "}
                  {target.mailbox_id}
                </p>
                <p className="text-sm">
                  {copy.attempts}: {target.attempts}
                </p>
                <p className="break-words text-sm">
                  {target.last_error || copy.lastErrorEmpty}
                </p>
              </li>
            ))}
          </ul>
          {canRetryInspection(receipt) ? (
            <ReviewedRetry
              key={receipt.updated_at}
              receipt={receipt}
              session={session}
              copy={copy}
              onAttempted={attempted}
            />
          ) : (
            <p role="status" className="rounded bg-muted p-3 text-sm">
              {(
                {
                  legacy_receipt: copy.legacy_receipt,
                  missing_ledger: copy.missing_ledger,
                  not_held: copy.not_held,
                  no_held_targets: copy.no_held_targets,
                } as Record<string, string>
              )[receipt.retry_block_reason] ?? copy.blocked}
            </p>
          )}
        </>
      ) : null}
    </section>
  );
}
function ReviewedRetry({
  receipt,
  session,
  copy,
  onAttempted,
}: {
  receipt: IngressInspection;
  session: IngressSession;
  copy: IngressCopy;
  onAttempted: (
    outcome: "submitted" | "conflict" | "uncertain" | "session",
  ) => void;
}) {
  const [reason, setReason] = useState("");
  const [confirmed, setConfirmed] = useState(false);
  const [busy, setBusy] = useState(false);
  const lock = useRef(false);
  const active = useRef(true);
  const controller = useRef<AbortController | null>(null);
  useEffect(() => {
    active.current = true;
    return () => {
      active.current = false;
      controller.current?.abort();
    };
  }, []);
  const length = [...reason].length;
  async function submit(event: React.FormEvent) {
    event.preventDefault();
    if (
      lock.current ||
      !confirmed ||
      !reason.trim() ||
      length > 2000 ||
      !isIngressSessionCurrent(session)
    )
      return;
    lock.current = true;
    setBusy(true);
    controller.current = new AbortController();
    let outcome: "submitted" | "conflict" | "uncertain" | "session" =
      "uncertain";
    try {
      const result = await retryIngress(
        session,
        receipt,
        reason,
        controller.current.signal,
      );
      if (result.data?.requeued === true) outcome = "submitted";
    } catch (error) {
      if (error instanceof IngressRequestError) {
        if (error.status === 409) outcome = "conflict";
        else if ([401, 403].includes(error.status)) outcome = "session";
      }
    }
    // A mutation is attempted at most once per mounted inspection. All outcomes
    // invalidate the snapshot; never auto-replay after a network/authorization error.
    if (active.current && isIngressSessionCurrent(session))
      onAttempted(outcome);
  }
  return (
    <form onSubmit={submit} className="space-y-3 rounded border p-4">
      <label className="block space-y-2">
        <span>{copy.reason}</span>
        <Textarea
          aria-label={copy.reason}
          disabled={busy}
          value={reason}
          onChange={(e) => setReason(e.target.value)}
          aria-describedby="ingress-reason-help"
        />
      </label>
      <p id="ingress-reason-help" className="text-xs text-muted-foreground">
        {copy.reasonHint} ({length}/2000)
      </p>
      <label className="flex items-start gap-2 text-sm">
        <input
          type="checkbox"
          disabled={busy}
          checked={confirmed}
          onChange={(e) => setConfirmed(e.target.checked)}
        />
        <span>{copy.confirm}</span>
      </label>
      <Button
        type="submit"
        disabled={busy || !confirmed || !reason.trim() || length > 2000}
      >
        {busy ? copy.busy : copy.retry}
      </Button>
    </form>
  );
}
