"use client";
import { useLayoutEffect, useRef, useState } from "react";
import { toast } from "sonner";
import {
  setMailboxSendPolicy,
  workMailboxes,
  type MailSendPolicy,
  type WorkMailbox,
} from "@/lib/company";
import { isConflict } from "@/lib/error-code";
import { sessionScope, useSessionScope } from "@/lib/session";
import { ActionButton, Field, inputClass, useAction, useText } from "./common";

type Text = (zh: string, en: string) => string;

const POLICY_VALUES: MailSendPolicy[] = [
  "free",
  "template_required",
  "disabled",
];

// One vocabulary for the three policy values, shared by the company-default
// selector and the per-mailbox override editor so the wording cannot drift.
export function sendPolicyLabel(t: Text, policy: MailSendPolicy): string {
  switch (policy) {
    case "template_required":
      return t("仅限已发布模板", "Published templates only");
    case "disabled":
      return t("暂停发送", "Sending disabled");
    default:
      return t("自由撰写", "Free-form writing");
  }
}

export function sendPolicyDescription(t: Text, policy: MailSendPolicy): string {
  switch (policy) {
    case "template_required":
      return t(
        "只能用已发布模板发送",
        "Sending is only allowed through published templates",
      );
    case "disabled":
      return t(
        "暂停该域名下邮箱发送",
        "Mailbox sending under this domain is paused",
      );
    default:
      return t("自由撰写；任何有代发权的成员可直接写信", "Free-form writing; any member with send-as rights may compose directly");
  }
}

// CompanyMailSendPolicyField is the company-default selector inside the
// company settings form. There is no "inherit" option here: the tenant value
// IS the default every non-overriding mailbox inherits.
export function CompanyMailSendPolicyField({
  value,
  onChange,
}: {
  value: MailSendPolicy;
  onChange: (policy: MailSendPolicy) => void;
}) {
  const t = useText();
  return (
    <Field label={t("公司默认发送策略", "Company default send policy")}>
      {(id) => (
        <select
          id={id}
          className={inputClass}
          value={value}
          onChange={(e) => onChange(e.target.value as MailSendPolicy)}
        >
          {POLICY_VALUES.map((p) => (
            <option key={p} value={p}>
              {sendPolicyLabel(t, p)}
            </option>
          ))}
        </select>
      )}
    </Field>
  );
}

type PolicyReview = { minimumRevision: number; newerThan?: number };

function policySnapshot(value: unknown, id: string, minimum: number, newerThan?: number): WorkMailbox | null {
  if (!Array.isArray(value)) return null;
  const matches = value.filter(row => row?.mailbox?.id === id);
  if (matches.length !== 1) return null;
  const snapshot = matches[0] as WorkMailbox;
  if (!Number.isSafeInteger(snapshot.revision) || snapshot.revision < 1 ||
    snapshot.revision < minimum || (newerThan !== undefined && snapshot.revision <= newerThan) ||
    !POLICY_VALUES.some(policy => policy === snapshot.mailbox.send_policy)) return null;
  return snapshot;
}

type MailboxPolicyProps = {
  mailbox: WorkMailbox;
  refresh: () => Promise<unknown>;
};

// MailboxSendPolicyEditor shows the effective policy of one mailbox and lets
// an administrator store or clear the per-mailbox override. The effective
// value arrives on mailbox.send_policy (COALESCE of override and company
// default), so it stays truthful even while the select itself cannot tell an
// explicit override apart from inheritance.
export function MailboxSendPolicyEditor(props: MailboxPolicyProps) {
  const scope = useSessionScope();
  return <MailboxPolicyForm key={`${scope}:${props.mailbox.mailbox.id}`} {...props} />;
}

function MailboxPolicyForm({
  mailbox,
  refresh,
}: MailboxPolicyProps) {
  const t = useText();
  const { busy, run } = useAction();
  const [scope] = useState(sessionScope);
  const lifetime = useRef<object | null>(null);
  const [choice, setChoice] = useState<"" | MailSendPolicy>("");
  const [revision, setRevision] = useState(mailbox.revision);
  const [reviewRequired, setReviewRequired] = useState<PolicyReview | null>(null);
  const [reviewed, setReviewed] = useState<{ value: WorkMailbox; parent: WorkMailbox } | null>(null);
  // A failed parent refresh can leave older props after our explicit GET.
  // Equal-version *new* parent data still wins: a company default can change
  // the effective policy without incrementing the mailbox's own revision.
  const current = reviewed && (reviewed.value.revision > mailbox.revision ||
    (reviewed.value.revision === mailbox.revision && reviewed.parent === mailbox))
    ? reviewed.value : mailbox;
  // Preserve the highest version observed during this editor lifetime. A
  // later stale parent read must not make an older form or response current.
  const [highestRevision, setHighestRevision] = useState(current.revision);
  if (current.revision > highestRevision) setHighestRevision(current.revision);
  const committed = useRef({ mailbox, highestRevision });
  useLayoutEffect(() => {
    lifetime.current = {};
    return () => { lifetime.current = null; };
  }, []);
  useLayoutEffect(() => { committed.current = { mailbox, highestRevision }; }, [mailbox, highestRevision]);
  const owns = (owner: object) => lifetime.current === owner && scope === sessionScope();
  const stale = reviewRequired !== null || revision !== current.revision || revision < highestRevision;
  const effective = current.mailbox.send_policy ?? "free";
  const adopt = (snapshot: WorkMailbox) => {
    setReviewed({ value: snapshot, parent: committed.current.mailbox });
    setRevision(snapshot.revision);
    setReviewRequired(null);
  };
  const review = () => {
    const owner = lifetime.current;
    if (!owner || !owns(owner) || busy) return;
    if (!reviewRequired && current.revision >= Math.max(revision, highestRevision)) {
      // Ordinary external prop changes already carry the parent's snapshot.
      // A conflict or unconfirmed post-write read always takes the GET path.
      setChoice("");
      setRevision(current.revision);
      return;
    }
    return run(async () => {
      try {
        const result = await workMailboxes();
        if (!owns(owner)) return;
        const snapshot = policySnapshot(result, mailbox.mailbox.id,
          Math.max(reviewRequired?.minimumRevision ?? revision, committed.current.highestRevision),
          reviewRequired?.newerThan);
        if (!snapshot) throw new Error(t("无法确认该邮箱的当前策略，请重新读取后核对。", "Could not confirm this mailbox's current policy. Read and review again."));
        adopt(snapshot);
        setChoice("");
      } catch (error) {
        if (owns(owner)) throw error;
      }
    });
  };
  return (
    <div className="space-y-3 border-t pt-3">
      <p className="text-sm text-muted-foreground">
        {t("当前生效发送策略", "Effective send policy")}
        {": "}
        {sendPolicyLabel(t, effective)}
        {" — "}
        {sendPolicyDescription(t, effective)}
      </p>
      <Field label={t("发送策略覆盖", "Send policy override")}>
        {(id) => (
          <select
            id={id}
            className={inputClass}
            value={choice}
            disabled={busy && reviewRequired !== null}
            onChange={(e) => setChoice(e.target.value as "" | MailSendPolicy)}
          >
            <option value="">{t("继承公司默认", "Inherit company default")}</option>
            {POLICY_VALUES.map((p) => (
              <option key={p} value={p}>
                {sendPolicyLabel(t, p)}
              </option>
            ))}
          </select>
        )}
      </Field>
      {stale && <p role="alert">{t("邮箱已变化，请核对最新策略后重新选择。", "Mailbox changed. Review the latest policy before choosing again.")}</p>}
      {stale && <ActionButton disabled={busy} onClick={review}>{t("核对最新版本", "Review latest version")}</ActionButton>}
      <ActionButton
        disabled={busy || stale}
        onClick={() =>
          run(async () => {
            const owner = lifetime.current;
            if (!owner || !owns(owner) || stale) return;
            try {
              await setMailboxSendPolicy(mailbox.mailbox.id, choice, revision);
            } catch (error) {
              if (!owns(owner)) return;
              if (isConflict(error)) {
                setReviewRequired({ minimumRevision: Math.max(revision, committed.current.highestRevision) });
                try { await refresh(); } catch { /* The explicit review remains required. */ }
                if (!owns(owner)) return;
              }
              throw error;
            }
            if (!owns(owner)) return;
            // Successful writes increment the mailbox revision transactionally.
            // This is only a lower bound: never synthesize a writable version.
            const required = { minimumRevision: Math.max(revision, committed.current.highestRevision), newerThan: revision };
            setReviewRequired(required);
            toast.success(t("发送策略已更新", "Send policy updated"));
            try {
              const result = await refresh();
              if (!owns(owner)) return;
              const snapshot = policySnapshot(result, mailbox.mailbox.id,
                Math.max(required.minimumRevision, committed.current.highestRevision), required.newerThan);
              if (snapshot) adopt(snapshot);
            } catch {
              // The PUT succeeded. Keep the choice and require a real GET;
              // neither report it as a failed write nor replay the mutation.
            }
          })
        }
      >
        {t("保存发送策略覆盖", "Save send policy override")}
      </ActionButton>
    </div>
  );
}
