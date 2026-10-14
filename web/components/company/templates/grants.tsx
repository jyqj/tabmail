"use client";
import { useLayoutEffect, useRef, useState } from "react";
import { toast } from "sonner";
import { useAPI } from "@/hooks/use-api";
import { sessionScope, useSessionScope } from "@/lib/session";
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
type TemplateGrantsProps = {
  template: MailTemplate | null;
  mailboxes: WorkMailbox[];
  mailboxesReady: boolean;
  mailbox: string;
  setMailbox: (id: string) => void;
};

export function TemplateGrantsView(props: TemplateGrantsProps) {
  const scope = useSessionScope();
  return <TemplateGrantSession
    key={JSON.stringify([scope, props.template?.id])}
    {...props}
    scope={scope}
  />;
}

function TemplateGrantSession({
  template,
  mailboxes,
  mailboxesReady,
  mailbox,
  setMailbox,
  scope,
}: TemplateGrantsProps & { scope: string }) {
  const t = useText();
  const { busy, run } = useAction();
  const [grantee, setGrantee] = useState("");
  const users = useAPI(template ? "template-users" : null, allEmployees);
  const grants = useAPI(
    template?.id ? ["template-grants", template.id] : null,
    () => company<{ user_id: string; mailbox_id: string }[]>(
      `/templates/${template!.id}/grants`,
    ),
  );
  const grantsReady = !grants.error && !grants.isLoading &&
    !grants.isValidating && Array.isArray(grants.data);
  const usersReady = !users.error && !users.isLoading &&
    !users.isValidating && Array.isArray(users.data);
  const ready = grantsReady && usersReady && mailboxesReady;
  const activeUsers = (users.error ? [] : users.data ?? []).filter(user => user.is_active);
  const canGrant = ready && activeUsers.some(user => user.id === grantee) &&
    mailboxes.some(box => box.mailbox.id === mailbox);
  const lifetime = useRef<object | null>(null);
  useLayoutEffect(() => {
    lifetime.current = {};
    return () => { lifetime.current = null; };
  }, []);
  const owns = (owner: object) => lifetime.current === owner && scope === sessionScope();

  function changeGrant(userId: string, mailboxId: string, enabled: boolean) {
    const owner = lifetime.current;
    if (!template?.id || busy || !ready || !owner || !owns(owner)) return;
    void run(async () => {
      try {
        await company(`/templates/${template.id}/grants`, {
          method: "PUT",
          body: { user_id: userId, mailbox_id: mailboxId, enabled },
        });
        if (!owns(owner)) return;
        await grants.mutate();
        if (enabled && owns(owner)) {
          toast.success(t("模板使用授权已添加", "Template usage granted"));
        }
      } catch (error) {
        if (owns(owner)) throw error;
      }
    });
  }
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
        employees={activeUsers}
      />
      <ActionButton
        disabled={busy || !canGrant}
        onClick={() => {
          if (busy || !canGrant) return;
          changeGrant(grantee, mailbox, true);
        }}
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
      {(grants.isLoading || grants.isValidating) && (
        <p role="status" className="text-muted-foreground">
          {grants.data
            ? t("正在刷新使用授权…", "Refreshing usage grants…")
            : t("正在加载使用授权…", "Loading usage grants…")}
        </p>
      )}
      {(users.isLoading || users.isValidating) && (
        <p role="status" className="text-muted-foreground">
          {users.data
            ? t("正在刷新成员列表…", "Refreshing members…")
            : t("正在加载成员列表…", "Loading members…")}
        </p>
      )}
      {!grants.error && !grants.isLoading && !users.error && (grants.data ?? []).map((g) => (
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
            disabled={busy || !ready}
            onClick={() => {
              if (busy || !ready || !grants.data?.includes(g)) return;
              changeGrant(g.user_id, g.mailbox_id, false);
            }}
          >
            {t("撤销使用权", "Revoke usage")}
          </ActionButton>
        </div>
      ))}
      {grantsReady && grants.data?.length === 0 && (
        <p className="text-muted-foreground">
          {t("该模板尚无使用授权", "This template has no usage grants")}
        </p>
      )}
    </Section>
  );
}
