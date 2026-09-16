"use client";
import Link from "next/link";
import { useState } from "react";
import { company } from "@/lib/company";
import {
  ActionButton,
  Field,
  inputClass,
  Section,
  useAction,
  useText,
} from "@/components/company/common";
export default function ActivatePage() {
  const t = useText();
  const { busy, run } = useAction();
  const [password, setPassword] = useState("");
  const [confirm, setConfirm] = useState("");
  const [done, setDone] = useState(false);
  return (
    <main className="mx-auto max-w-xl p-6 py-16">
      <Section title={t("激活员工账号", "Activate employee account")}>
        {done ? (
          <div className="space-y-4">
            <p role="status">
              {t(
                "账号与个人邮箱已开通。请使用邀请中指定的登录邮箱登录。",
                "Your account and personal mailbox are ready. Sign in with the login email specified in your invitation.",
              )}
            </p>
            <Link href="/" className="underline">
              {t("返回登录", "Return to sign-in")}
            </Link>
          </div>
        ) : (
          <form
            className="space-y-4"
            onSubmit={(e) => {
              e.preventDefault();
              void run(async () => {
                const token = window.location.hash.slice(1);
                if (!/^[a-f0-9]{64}$/.test(token))
                  throw new Error(
                    t(
                      "链接无效，请向管理员索取完整激活链接。",
                      "Invalid link. Ask your administrator for the complete activation link.",
                    ),
                  );
                if (password !== confirm)
                  throw new Error(
                    t("两次密码不一致", "Passwords do not match"),
                  );
                await company("/activate", {
                  method: "POST",
                  body: { token, password },
                });
                history.replaceState(null, "", window.location.pathname);
                setPassword("");
                setConfirm("");
                setDone(true);
              });
            }}
          >
            <p className="text-sm text-muted-foreground">
              {t(
                "激活链接为一次性凭据，不会发送在 URL 查询参数中。请输入 12–72 字节的密码。",
                "The activation token is single-use and is not sent in the URL query. Choose a 12–72 byte password.",
              )}
            </p>
            <Field label={t("新密码", "New password")}>
              {(id) => (
                <input
                  id={id}
                  className={inputClass}
                  type="password"
                  autoComplete="new-password"
                  minLength={12}
                  maxLength={72}
                  required
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                />
              )}
            </Field>
            <Field label={t("确认密码", "Confirm password")}>
              {(id) => (
                <input
                  id={id}
                  className={inputClass}
                  type="password"
                  autoComplete="new-password"
                  required
                  value={confirm}
                  onChange={(e) => setConfirm(e.target.value)}
                />
              )}
            </Field>
            <ActionButton
              type="submit"
              disabled={busy || password.length < 12 || password !== confirm}
            >
              {t("激活并开通邮箱", "Activate and provision mailbox")}
            </ActionButton>
          </form>
        )}
      </Section>
    </main>
  );
}
