"use client";
import { useState } from "react";
import { toast } from "sonner";
import {
  setMailboxSendPolicy,
  type MailSendPolicy,
  type WorkMailbox,
} from "@/lib/company";
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

// MailboxSendPolicyEditor shows the effective policy of one mailbox and lets
// an administrator store or clear the per-mailbox override. The effective
// value arrives on mailbox.send_policy (COALESCE of override and company
// default), so it stays truthful even while the select itself cannot tell an
// explicit override apart from inheritance.
export function MailboxSendPolicyEditor({
  mailbox,
  refresh,
}: {
  mailbox: WorkMailbox;
  refresh: () => Promise<unknown>;
}) {
  const t = useText();
  const { busy, run } = useAction();
  const [choice, setChoice] = useState<"" | MailSendPolicy>("");
  const [revision, setRevision] = useState(mailbox.revision);
  const [conflict, setConflict] = useState(false);
  const stale = conflict || revision !== mailbox.revision;
  const effective = mailbox.mailbox.send_policy ?? "free";
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
      {stale && <ActionButton disabled={busy} onClick={() => {
        setChoice(""); setRevision(mailbox.revision); setConflict(false);
      }}>{t("核对最新版本", "Review latest version")}</ActionButton>}
      <ActionButton
        disabled={busy || stale}
        onClick={() =>
          run(async () => {
            try {
              await setMailboxSendPolicy(mailbox.mailbox.id, choice, revision);
            } catch (error) {
              if ((error as { error?: { code?: string } }).error?.code === "CONFLICT") {
                setConflict(true);
                await refresh();
              }
              throw error;
            }
            setRevision(revision + 1);
            await refresh();
            toast.success(t("发送策略已更新", "Send policy updated"));
          })
        }
      >
        {t("保存发送策略覆盖", "Save send policy override")}
      </ActionButton>
    </div>
  );
}
