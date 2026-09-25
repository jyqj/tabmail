"use client";
import { useState } from "react";
import { toast } from "sonner";
import { useAPI } from "@/hooks/use-api";
import { company, workPath, type WorkGrant, type WorkMailbox, type MailboxGrantSnapshot } from "@/lib/company";
import type { AdminUser } from "@/lib/types";
import { EmployeeField } from "./employee-field";
import { ActionButton, Field, inputClass, LoadError, useAction, useText } from "./common";

export function GrantEditor({
  mailbox,
  employees,
  refresh,
}: {
  mailbox: WorkMailbox;
  employees: AdminUser[];
  refresh: () => Promise<unknown>;
}) {
  const t = useText();
  const { busy, run } = useAction();
  const grants = useAPI(["mailbox-grants", mailbox.mailbox.id], () =>
    company<MailboxGrantSnapshot>(`${workPath(mailbox.mailbox.id)}/grants`),
  );
  const [grantRevision, setGrantRevision] = useState<number | null>(null);
  const [conflict, setConflict] = useState(false);
  const empty: WorkGrant = {
    user_id: "",
    can_read: false,
    can_organize: false,
    can_send: false,
    template_only: false,
  };
  const [grant, setGrant] = useState(empty);
  const stale = conflict || (grantRevision !== null && grants.data?.revision !== grantRevision);
  const [nextOwner, setNextOwner] = useState("");
  const [reason, setReason] = useState("");
  return (
    <div className="space-y-4">
      <LoadError error={grants.error} onRetry={() => void grants.mutate()} />
      <p className="text-sm text-muted-foreground">
        {t(
          "属主具有内建权限；其他成员必须显式授权。全不选即撤销授权。",
          "Owners have intrinsic rights. Other members need explicit grants; clearing all rights revokes access.",
        )}
      </p>
      <EmployeeField
        label={t("授权成员", "Grant to member")}
        value={grant.user_id}
        employees={employees.filter(
          (v) => v.id !== mailbox.mailbox.owner_user_id,
        )}
        onChange={(id) => {
          setGrant(grants.data?.grants.find((v) => v.user_id === id) ?? { ...empty, user_id: id });
          setGrantRevision(grants.data?.revision ?? null);
          setConflict(false);
        }}
      />
      <div className="flex flex-wrap gap-5">
        {(
          ["can_read", "can_organize", "can_send", "template_only"] as const
        ).map((key, i) => (
          <label key={key} className="flex items-center gap-2 text-sm">
            <input
              type="checkbox"
              checked={grant[key]}
              onChange={(e) => {
                const next = { ...grant, [key]: e.target.checked };
                if (key === "can_organize" && next.can_organize)
                  next.can_read = true;
                if (key === "can_read" && !next.can_read)
                  next.can_organize = false;
                if (key === "template_only" && next.template_only)
                  next.can_send = true;
                if (key === "can_send" && !next.can_send)
                  next.template_only = false;
                setGrant(next);
              }}
            />
            {
              [
                t("阅读", "Read"),
                t("整理 / 删除", "Organize / delete"),
                t("代发", "Send as"),
                t("仅使用模板发送", "Template-only sending"),
              ][i]
            }
          </label>
        ))}
      </div>
      {stale && <p role="alert">{t("权限已经变化，请重新加载并选择成员核对，旧表单不会自动覆盖。", "Permissions changed. Reload and select the member again; the old form will not overwrite them.")}</p>}
      <ActionButton disabled={busy} onClick={() => run(async () => {
        await grants.mutate();
        await refresh();
        setGrant(empty);
        setGrantRevision(null);
        setConflict(false);
      })}>{t("重新加载权限", "Reload permissions")}</ActionButton>
      <ActionButton
        disabled={busy || stale || grantRevision === null || !grant.user_id || Boolean(grants.error)}
        onClick={() =>
          run(async () => {
            try {
              await company(`${workPath(mailbox.mailbox.id)}/grants`, {
                method: "PUT",
                body: { ...grant, revision: grantRevision },
              });
            } catch (error) {
              if ((error as { error?: { code?: string } }).error?.code === "CONFLICT") setConflict(true);
              throw error;
            }
            setGrant(empty);
            setGrantRevision(null);
            await grants.mutate();
            await refresh();
            toast.success(t("授权已更新", "Grant updated"));
          })
        }
      >
        {t("保存邮箱授权", "Save mailbox grant")}
      </ActionButton>
      {(grants.data?.grants ?? []).map((v) => (
        <p key={v.user_id} className="text-sm">
          {employees.find((u) => u.id === v.user_id)?.email ?? v.user_id}:{" "}
          {v.can_read ? t("阅读 ", "read ") : ""}
          {v.can_organize ? t("整理 ", "organize ") : ""}
          {v.can_send ? t("代发 ", "send ") : ""}
          {v.template_only ? t("仅模板", "template only") : ""}
        </p>
      ))}
      {mailbox.mailbox.kind === "personal" && (
        <EmployeeField
          label={t("新属主", "New owner")}
          value={nextOwner}
          employees={employees.filter(
            (v) => v.id !== mailbox.mailbox.owner_user_id,
          )}
          onChange={setNextOwner}
        />
      )}
      {(mailbox.mailbox.kind === "personal" ||
        mailbox.mailbox.kind === "legacy") && (
        <>
          <Field
            label={t(
              "资源变更原因（至少 8 个字符）",
              "Resource-change reason (8+ characters)",
            )}
          >
            {(id) => (
              <input
                id={id}
                className={inputClass}
                value={reason}
                onChange={(e) => setReason(e.target.value)}
              />
            )}
          </Field>
          <ActionButton
            disabled={
              busy ||
              reason.trim().length < 8 ||
              (mailbox.mailbox.kind === "personal" && !nextOwner)
            }
            onClick={() =>
              run(async () => {
                if (mailbox.mailbox.kind === "personal")
                  await company(`${workPath(mailbox.mailbox.id)}/handover`, {
                    method: "POST",
                    body: {
                      owner_user_id: nextOwner,
                      revision: mailbox.revision,
                      reason,
                    },
                  });
                else
                  await company(
                    `${workPath(mailbox.mailbox.id)}/convert-shared`,
                    {
                      method: "POST",
                      body: { revision: mailbox.revision, reason },
                    },
                  );
                await refresh();
                toast.success(
                  t("邮箱生命周期已更新", "Mailbox lifecycle updated"),
                );
              })
            }
          >
            {mailbox.mailbox.kind === "personal"
              ? t(
                  "移交邮箱（不删除邮件）",
                  "Transfer mailbox (preserve messages)",
                )
              : t(
                  "迁移为私有共享邮箱并永久保留",
                  "Convert to private, permanently retained shared mailbox",
                )}
          </ActionButton>
        </>
      )}
    </div>
  );
}
