"use client";
import { useState } from "react";
import { toast } from "sonner";
import { useAPI } from "@/hooks/use-api";
import {
  allEmployees,
  company,
  type MailTemplate,
  type WorkMailbox,
} from "@/lib/company";
import {
  ActionButton,
  Field,
  inputClass,
  LoadError,
  Section,
  useAction,
  useText,
} from "@/components/company/common";
import { EmployeeField } from "@/components/company/employee-field";

// TemplateGrantsView manages the per template usage grants: an employee may
// use the template to send from a specific mailbox. Grants never confer
// additional sender identities.
export function TemplateGrantsView({
  template,
  mailboxes,
  mailbox,
  setMailbox,
}: {
  template: MailTemplate | null;
  mailboxes: WorkMailbox[];
  mailbox: string;
  setMailbox: (id: string) => void;
}) {
  const t = useText();
  const { busy, run } = useAction();
  const [grantee, setGrantee] = useState("");
  const users = useAPI("template-users", allEmployees);
  const grants = useAPI(
    template?.id ? ["template-grants", template.id] : null,
    () => company<{ user_id: string; mailbox_id: string }[]>(
      `/templates/${template!.id}/grants`,
    ),
  );
  if (!template) {
    return (
      <Section title={t("使用授权", "Usage grants")}>
        <p className="text-muted-foreground">
          {t(
            "从模板库选择一个模板后管理其使用授权。",
            "Pick a template from the library to manage its usage grants.",
          )}
        </p>
      </Section>
    );
  }
  return (
    <Section title={t("使用授权", "Usage grants")}>
      <Field label={t("授权所用邮箱", "Grant mailbox")}>
        {(id) => (
          <select
            id={id}
            className={inputClass}
            value={mailbox}
            onChange={(e) => setMailbox(e.target.value)}
          >
            <option value="" />
            {mailboxes.map((v) => (
              <option key={v.mailbox.id} value={v.mailbox.id}>
                {v.mailbox.full_address}
              </option>
            ))}
          </select>
        )}
      </Field>
      <EmployeeField
        label={t(
          "允许使用模板的成员",
          "Member allowed to use the template",
        )}
        value={grantee}
        onChange={setGrantee}
        employees={(users.data ?? []).filter((u) => u.is_active)}
      />
      <ActionButton
        disabled={busy || !grantee || !mailbox}
        onClick={() =>
          run(async () => {
            await company(`/templates/${template.id}/grants`, {
              method: "PUT",
              body: {
                user_id: grantee,
                mailbox_id: mailbox,
                enabled: true,
              },
            });
            await grants.mutate();
            toast.success(
              t("模板使用授权已添加", "Template usage granted"),
            );
          })
        }
      >
        {t(
          "授权此成员在所选邮箱使用",
          "Grant usage on the selected mailbox",
        )}
      </ActionButton>
      <LoadError
        error={grants.error || users.error}
        onRetry={() => {
          void grants.mutate();
          void users.mutate();
        }}
      />
      {(grants.data ?? []).map((g) => (
        <div
          key={`${g.user_id}:${g.mailbox_id}`}
          className="flex flex-wrap items-center justify-between gap-3 text-sm"
        >
          <span>
            {users.data?.find((u) => u.id === g.user_id)?.email ??
              g.user_id}{" "}
            ·{" "}
            {mailboxes.find((b) => b.mailbox.id === g.mailbox_id)
              ?.mailbox.full_address ?? g.mailbox_id}
          </span>
          <ActionButton
            disabled={busy}
            onClick={() =>
              run(async () => {
                await company(`/templates/${template.id}/grants`, {
                  method: "PUT",
                  body: { ...g, enabled: false },
                });
                await grants.mutate();
              })
            }
          >
            {t("撤销使用权", "Revoke usage")}
          </ActionButton>
        </div>
      ))}
    </Section>
  );
}
