"use client";
import { useId, useState } from "react";
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
  const passwordHint = useId();
  // The API and bcrypt policy measure UTF-8 bytes, not UTF-16 input length.
  // Preserve the exact password; never trim, normalize or truncate it.
  const passwordBytes = new TextEncoder().encode(next).length;
  const validPassword = passwordBytes >= 12 && passwordBytes <= 72;
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
              if (!old)
                throw new Error(t("请输入当前密码", "Enter your current password"));
              if (next !== confirm)
                throw new Error(t("两次密码不一致", "Passwords do not match"));
              if (!validPassword)
                throw new Error(t("新密码必须为 12–72 个 UTF-8 字节", "New password must be 12–72 UTF-8 bytes"));
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
                  aria-describedby={i === 1 ? passwordHint : undefined}
                  aria-invalid={i === 1 && next.length > 0 && !validPassword ? true : undefined}
                  value={v.value}
                  onChange={(e) => v.set(e.target.value)}
                />
              )}
            </Field>
          ))}
          <p id={passwordHint} className="text-sm text-muted-foreground" aria-live="polite">
            {t(
              `新密码当前为 ${passwordBytes} 个 UTF-8 字节，需要 12–72 字节。`,
              `New password: ${passwordBytes} UTF-8 bytes. Enter 12–72 bytes.`,
            )}
          </p>
          <ActionButton
            type="submit"
            disabled={busy || !old || !validPassword || next !== confirm}
          >
            {t("更新密码", "Update password")}
          </ActionButton>
        </form>
      </Section>
    </main>
  );
}
