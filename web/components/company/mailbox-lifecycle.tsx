"use client";
import { useLayoutEffect, useRef, useState } from "react";
import { toast } from "sonner";
import { company, workMailboxes, workPath, type WorkMailbox } from "@/lib/company";
import { sessionScope, useSessionScope } from "@/lib/session";
import type { AdminUser } from "@/lib/types";
import { ActionButton, Field, inputClass, LoadError, useText } from "./common";
import { EmployeeField } from "./employee-field";

function validSnapshot(value: WorkMailbox | undefined, id: string, tenant: string): value is WorkMailbox {
  return !!value && value.mailbox?.id === id && value.mailbox.tenant_id === tenant &&
    typeof value.mailbox.full_address === "string" && !!value.mailbox.full_address &&
    Number.isSafeInteger(value.revision) && value.revision > 0 &&
    ["personal", "legacy", "shared"].includes(value.mailbox.kind ?? "") &&
    (value.mailbox.kind !== "personal" || (typeof value.mailbox.owner_user_id === "string" && !!value.mailbox.owner_user_id));
}
function signature(value: WorkMailbox) {
  return JSON.stringify([value.mailbox.id, value.mailbox.tenant_id, value.revision, value.mailbox.kind, value.mailbox.owner_user_id]);
}
type Props = { mailbox: WorkMailbox; employees: AdminUser[]; refresh: () => Promise<unknown>;
  busy: boolean; run: (action: () => Promise<void>) => Promise<void> };

export function MailboxLifecycleEditor(props: Props) {
  const scope = useSessionScope();
  return <LifecycleSession key={`${scope}:${props.mailbox.mailbox.id}`} {...props} scope={scope} />;
}

function LifecycleSession({ mailbox, employees, refresh, busy, run, scope }: Props & { scope: string }) {
  const t = useText();
  const [tenant] = useState(() => typeof window === "undefined" ? "" : localStorage.getItem("tabmail_tenant_id") ?? "");
  const [snapshot, setSnapshot] = useState(mailbox);
  const [dirty, setDirty] = useState(false);
  const [nextOwner, setNextOwner] = useState("");
  const [reason, setReason] = useState("");
  const [reviewRequired, setReviewRequired] = useState(false);
  const [reviewed, setReviewed] = useState(false);
  const [acknowledged, setAcknowledged] = useState(false);
  const [completed, setCompleted] = useState(false);
  const [readError, setReadError] = useState<unknown>(null);
  const validObserved = validSnapshot(mailbox, mailbox.mailbox.id, tenant);
  const [highestRevision, setHighestRevision] = useState(validObserved ? mailbox.revision : 0);
  const minimumRevision = Math.max(highestRevision, validObserved ? mailbox.revision : 0);
  if (minimumRevision > highestRevision) setHighestRevision(minimumRevision);
  // An untouched form may follow live data. Its first input pins the actual
  // resource revision and owner; later cache refreshes cannot rebase intent.
  const current = dirty ? snapshot : mailbox;
  const currentValid = validSnapshot(current, mailbox.mailbox.id, tenant);
  const stale = !currentValid || current.revision < minimumRevision ||
    (mailbox.revision === current.revision && signature(mailbox) !== signature(current));
  if (dirty && stale && !reviewRequired) setReviewRequired(true);
  const currentSignature = signature(mailbox);
  const [observation, setObservation] = useState({ signature: currentSignature });
  if (observation.signature !== currentSignature) setObservation({ signature: currentSignature });
  const committed = useRef<object | null>(null);
  const lifetime = useRef<object | null>(null);
  const intent = useRef<object>({});
  useLayoutEffect(() => {
    lifetime.current = {};
    return () => { lifetime.current = null; };
  }, []);
  useLayoutEffect(() => {
    committed.current = observation;
    return () => { committed.current = null; };
  }, [observation]);
  const owns = (owner: object) => lifetime.current === owner && scope === sessionScope();
  const edit = (change: () => void) => {
    if (scope !== sessionScope()) return;
    intent.current = {};
    if (!dirty) { setSnapshot(current); setDirty(true); }
    change();
  };
  const eligible = employees.filter(employee => employee.is_active && employee.tenant_id === tenant && employee.id !== current.mailbox.owner_user_id);
  const personal = current.mailbox.kind === "personal";
  const actionable = personal || current.mailbox.kind === "legacy";
  const canSubmit = actionable && !busy && !stale && !reviewRequired &&
    (!reviewed || acknowledged) && reason.trim().length >= 8 &&
    (!personal || eligible.some(employee => employee.id === nextOwner));

  const reload = () => run(async () => {
    const owner = lifetime.current, generation = committed.current, draft = intent.current;
    if (!owner || !owns(owner)) return;
    setReadError(null);
    // A failed review stays latched even after an unrelated cache recovery.
    setReviewRequired(true); setAcknowledged(false);
    try {
      const boxes = await workMailboxes();
      if (!owns(owner)) return;
      if (generation !== committed.current || draft !== intent.current)
        throw new Error(t("邮箱或表单在读取期间变化，请重新核对。", "The mailbox or draft changed while loading. Review again."));
      const matches = Array.isArray(boxes) ? boxes.filter(value => value?.mailbox?.id === mailbox.mailbox.id) : [];
      const fresh = matches[0];
      if (matches.length !== 1 || !validSnapshot(fresh, mailbox.mailbox.id, tenant) || fresh.revision < minimumRevision)
        throw new Error(t("无法确认当前邮箱及版本，请重新读取。", "Could not confirm the current mailbox and revision. Reload it."));
      setSnapshot(fresh); setHighestRevision(fresh.revision); setDirty(true);
      setReviewRequired(false); setReviewed(true); setCompleted(false);
      if (fresh.mailbox.owner_user_id === nextOwner) setNextOwner("");
    } catch (error) {
      if (owns(owner)) setReadError(error);
    }
  });
  const submit = () => run(async () => {
    const owner = lifetime.current, draft = intent.current;
    if (!owner || !owns(owner) || !canSubmit) return;
    setReadError(null);
    try {
      if (personal)
        await company(`${workPath(current.mailbox.id)}/handover`, {
          method: "POST", body: { owner_user_id: nextOwner, revision: current.revision, reason },
        });
      else
        await company(`${workPath(current.mailbox.id)}/convert-shared`, {
          method: "POST", body: { revision: current.revision, reason },
        });
    } catch (error) {
      if (owns(owner)) {
        setSnapshot(current); setDirty(true); setReviewRequired(true); setAcknowledged(false);
        throw error;
      }
      return;
    }
    if (!owns(owner)) return;
    // The acknowledged write is complete even when the following GET fails.
    // Retire its form immediately, and require a strictly newer snapshot.
    setSnapshot(current); setDirty(true); setReviewRequired(true); setAcknowledged(false); setCompleted(true);
    setHighestRevision(value => Math.max(value, current.revision + 1));
    if (intent.current === draft) { setNextOwner(""); setReason(""); }
    toast.success(t("邮箱生命周期已更新", "Mailbox lifecycle updated"));
    try { await refresh(); } catch {
      if (owns(owner)) setReadError(new Error(t("变更已完成，但邮箱列表刷新失败。请重新读取核对。", "The change completed, but the mailbox list could not be refreshed. Reload it to review.")));
    }
  });

  if (!actionable && !reviewRequired && !completed) return null;
  return <section className="space-y-3 border-t pt-3" aria-label={t("邮箱生命周期", "Mailbox lifecycle")}>
    <LoadError error={readError} onRetry={() => void reload()} />
    {(reviewRequired || stale) && <p role="status" className="text-sm">{completed
      ? t("邮箱变更已完成。再次操作前，请读取并核对当前邮箱。", "The mailbox change completed. Reload and review the current mailbox before another change.")
      : t("邮箱或变更结果需要核对；当前输入已保留，请读取最新邮箱后再确认操作。", "The mailbox or change result needs review. Your input is preserved; reload the current mailbox before confirming another change.")}</p>}
    {reviewed && <div className="rounded border p-3 text-sm space-y-2">
      <p>{current.mailbox.full_address} · {current.mailbox.kind} · v{current.revision}</p>
      {personal && <p>{t("当前属主", "Current owner")}: {employees.find(employee => employee.id === current.mailbox.owner_user_id)?.email ?? current.mailbox.owner_user_id}</p>}
      <label className="flex gap-2"><input type="checkbox" checked={acknowledged} disabled={busy || stale || reviewRequired}
        onChange={event => setAcknowledged(event.target.checked)} />{t("我已核对当前邮箱，再执行这项变更", "I reviewed the current mailbox before applying this change")}</label>
    </div>}
    <ActionButton disabled={busy} onClick={() => void reload()}>{t("重新读取邮箱并核对", "Reload mailbox for review")}</ActionButton>
    {personal && <EmployeeField label={t("新属主", "New owner")} value={nextOwner} employees={eligible}
      onChange={value => edit(() => setNextOwner(value))} />}
    <Field label={t("资源变更原因（至少 8 个字符）", "Resource-change reason (8+ characters)")}>
      {id => <input id={id} className={inputClass} value={reason} onChange={event => edit(() => setReason(event.target.value))} />}
    </Field>
    <ActionButton disabled={!canSubmit} onClick={() => void submit()}>{personal
      ? t("移交邮箱（不删除邮件）", "Transfer mailbox (preserve messages)")
      : t("迁移为私有共享邮箱并永久保留", "Convert to private, permanently retained shared mailbox")}</ActionButton>
  </section>;
}
