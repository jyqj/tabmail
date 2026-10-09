"use client";
import { useLayoutEffect, useRef, useState } from "react";
import { toast } from "sonner";
import { useAPI } from "@/hooks/use-api";
import { company, workPath, type WorkGrantInput, type WorkMailbox, type MailboxGrantSnapshot } from "@/lib/company";
import type { AdminUser } from "@/lib/types";
import { isConflict } from "@/lib/error-code";
import { sessionScope, useSessionScope } from "@/lib/session";
import { EmployeeField } from "./employee-field";
import { ActionButton, LoadError, useAction, useText } from "./common";
import { MailboxLifecycleEditor } from "./mailbox-lifecycle";

type GrantEditorProps = {
  mailbox: WorkMailbox;
  employees: AdminUser[];
  refresh: () => Promise<unknown>;
};

export function GrantEditor(props: GrantEditorProps) {
  const scope = useSessionScope();
  return <GrantEditorSession key={`${scope}:${props.mailbox.mailbox.id}`} {...props} scope={scope} />;
}

function GrantEditorSession({
  mailbox,
  employees,
  refresh,
  scope,
}: GrantEditorProps & { scope: string }) {
  const t = useText();
  const { busy, run } = useAction();
  const grants = useAPI(["mailbox-grants", mailbox.mailbox.id], () =>
    company<MailboxGrantSnapshot>(`${workPath(mailbox.mailbox.id)}/grants`),
  );
  const [grantRevision, setGrantRevision] = useState<number | null>(null);
  const [conflictRevision, setConflictRevision] = useState<number | null>(null);
  const [savedRevision, setSavedRevision] = useState<number | null>(null);
  const [reviewError, setReviewError] = useState<unknown>(null);
  const lifetime = useRef<object | null>(null);
  const draftIntent = useRef<object>({});
  const highestRevision = useRef(0);
  useLayoutEffect(() => {
    lifetime.current = {};
    return () => { lifetime.current = null; };
  }, []);
  useLayoutEffect(() => {
    if (Number.isSafeInteger(grants.data?.revision))
      highestRevision.current = Math.max(highestRevision.current, grants.data!.revision);
  }, [grants.data]);
  const owns = (owner: object) => lifetime.current === owner && scope === sessionScope();
  const loadError = reviewError ?? grants.error;
  const readReady = Boolean(grants.data) && !loadError && !grants.isLoading && !grants.isValidating;
  const empty: WorkGrantInput = {
    user_id: "",
    can_read: false,
    can_organize: false,
    can_send: false,
    template_only: false,
  };
  const [grant, setGrant] = useState(empty);
  const stale = conflictRevision !== null || savedRevision !== null || (grantRevision !== null && grants.data?.revision !== grantRevision);
  const readSnapshot = (owner: object, minimumRevision: number) => grants.mutate(async () => {
    const snapshot = await company<MailboxGrantSnapshot>(`${workPath(mailbox.mailbox.id)}/grants`);
    if (!owns(owner)) throw new DOMException("Mailbox grant editor changed", "AbortError");
    if (!snapshot || !Number.isSafeInteger(snapshot.revision) ||
      snapshot.revision < Math.max(minimumRevision, highestRevision.current) || !Array.isArray(snapshot.grants)) {
      throw new Error(t("无法确认当前权限版本，请重新加载权限。", "Could not confirm the current permission version. Reload permissions."));
    }
    return snapshot;
  }, { revalidate: false });
  const reloadGrants = () => run(async () => {
    const owner = lifetime.current;
    if (!owner || !owns(owner)) return;
    setReviewError(null);
    const minimumRevision = Math.max(grantRevision ?? 0, conflictRevision ?? 0, savedRevision ?? 0, grants.data?.revision ?? 0, highestRevision.current);
    try {
      // mutate() without data may resolve with the old cache after a failed
      // revalidation. An explicit fetch mutation must succeed before review
      // can discard the form or release a conflict.
      const latest = await readSnapshot(owner, minimumRevision);
      if (!owns(owner)) return;
      if (!latest) throw new Error(t("权限读取未完成，请重新加载。", "Permission read did not complete. Reload permissions."));
      await refresh();
      if (!owns(owner)) return;
      draftIntent.current = {};
      setGrant(empty);
      setGrantRevision(null);
      // A fresh same-revision read is valid after a transient lock conflict.
      // Selecting a member from the pre-conflict cache is never a review.
      setConflictRevision(null);
      setSavedRevision(null);
    } catch (error) {
      if (!owns(owner)) return;
      setReviewError(error);
      throw error;
    }
  });
  return (
    <div className="space-y-4">
      <LoadError error={loadError} onRetry={reloadGrants} />
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
          if (scope !== sessionScope()) return;
          draftIntent.current = {};
          setGrant(grants.data?.grants.find((v) => v.user_id === id) ?? { ...empty, user_id: id });
          setGrantRevision(readReady ? grants.data?.revision ?? null : null);
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
                if (scope !== sessionScope()) return;
                draftIntent.current = {};
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
      <ActionButton disabled={busy} onClick={reloadGrants}>{t("重新加载权限", "Reload permissions")}</ActionButton>
      <ActionButton
        disabled={busy || stale || !readReady || grantRevision === null || !grant.user_id}
        onClick={() =>
          run(async () => {
            const owner = lifetime.current;
            if (!owner || !owns(owner) || stale || !readReady || grantRevision === null || !grant.user_id) return;
            const intent = draftIntent.current;
            try {
              await company(`${workPath(mailbox.mailbox.id)}/grants`, {
                method: "PUT",
                body: { user_id: grant.user_id, can_read: grant.can_read,
                  can_organize: grant.can_organize, can_send: grant.can_send,
                  template_only: grant.template_only, revision: grantRevision },
              });
            } catch (error) {
              if (!owns(owner)) return;
              if (isConflict(error)) setConflictRevision(grantRevision);
              if (error instanceof SyntaxError) {
                // Parsing can fail after a successful HTTP status. Preserve
                // the draft and require a real read before another write.
                const uncertain = new Error(t("无法确认授权保存结果，请重新加载权限核对后再试。", "Could not confirm the grant save result. Reload permissions before trying again."));
                setConflictRevision(grantRevision);
                setReviewError(uncertain);
                throw uncertain;
              }
              throw error;
            }
            if (!owns(owner)) return;
            // This PUT has committed even if a following GET fails. A newer
            // member/rights draft remains pinned to its observed revision.
            const minimumRevision = Math.max(grantRevision + 1, highestRevision.current);
            setSavedRevision(minimumRevision);
            if (draftIntent.current === intent) {
              draftIntent.current = {};
              setGrant(empty);
              setGrantRevision(null);
            }
            toast.success(t("授权已更新", "Grant updated"));
            try {
              const fresh = await readSnapshot(owner, minimumRevision);
              if (!owns(owner)) return;
              if (!fresh) throw new Error("Grant readback did not complete");
              await refresh();
              if (owns(owner)) { setSavedRevision(null); setReviewError(null); }
            } catch {
              if (owns(owner)) setReviewError(new Error(t("授权已保存，但无法刷新当前权限。请重新加载权限核对。", "The grant was saved, but current permissions could not be refreshed. Reload permissions to review.")));
            }
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
      <MailboxLifecycleEditor mailbox={mailbox} employees={employees} refresh={refresh} busy={busy} run={run} />
    </div>
  );
}
