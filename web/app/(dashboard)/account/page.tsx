"use client";
import { useState } from "react";
import { useRouter } from "next/navigation";
import { toast } from "sonner";
import { changePassword } from "@/lib/api/auth";
import { clearSessionCredentials } from "@/lib/session";
import {
  ActionButton,
  Field,
  inputClass,
  Section,
  useAction,
  useText,
} from "@/components/company/common";
export default function AccountPage() {
  const t = useText();
  const router = useRouter();
  const { busy, run } = useAction();
  const [old, setOld] = useState("");
  const [next, setNext] = useState("");
  const [confirm, setConfirm] = useState("");
  return (
    <main className="mx-auto max-w-xl w-full p-6 space-y-5">
      <h1 className="text-2xl font-semibold">
        {t("账号安全", "Account security")}
      </h1>
      <Section
        title={t(
          "修改密码并撤销全部会话",
          "Change password and revoke all sessions",
        )}
      >
        <p className="text-sm text-muted-foreground">
          {t(
            "修改成功后，现有 access token 和刷新会话会全部失效。请重新登录。",
            "Changing your password invalidates existing access tokens and refresh sessions. Sign in again afterwards.",
          )}
        </p>
        <form
          className="space-y-4"
          onSubmit={(e) => {
            e.preventDefault();
            void run(async () => {
              if (next !== confirm)
                throw new Error(t("两次密码不一致", "Passwords do not match"));
              await changePassword(old, next);
              clearSessionCredentials();
              toast.success(
                t(
                  "密码已修改，请重新登录",
                  "Password changed. Please sign in again.",
                ),
              );
              router.replace("/");
            });
          }}
        >
          {[
            {
              label: t("当前密码", "Current password"),
              value: old,
              set: setOld,
              auto: "current-password",
            },
            {
              label: t("新密码（12–72 字节）", "New password (12–72 bytes)"),
              value: next,
              set: setNext,
              auto: "new-password",
            },
            {
              label: t("确认新密码", "Confirm new password"),
              value: confirm,
              set: setConfirm,
              auto: "new-password",
            },
          ].map((v, i) => (
            <Field label={v.label} key={i}>
              {(id) => (
                <input
                  id={id}
                  className={inputClass}
                  type="password"
                  autoComplete={v.auto}
                  required
                  maxLength={72}
                  minLength={i ? 12 : 1}
                  value={v.value}
                  onChange={(e) => v.set(e.target.value)}
                />
              )}
            </Field>
          ))}
          <ActionButton
            type="submit"
            disabled={busy || !old || next.length < 12 || next !== confirm}
          >
            {t("更新密码", "Update password")}
          </ActionButton>
        </form>
      </Section>
    </main>
  );
}
