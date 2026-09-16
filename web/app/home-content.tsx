"use client";
import Link from "next/link";
import { useState } from "react";
import { useRouter } from "next/navigation";
import { useAuth } from "@/contexts/auth-context";
import { login } from "@/lib/api/auth";
import { ThemeToggle } from "@/components/theme-toggle";
import {
  ActionButton,
  Field,
  inputClass,
  Section,
  useAction,
  useText,
} from "@/components/company/common";
export function HomeContent() {
  const t = useText();
  const auth = useAuth();
  const router = useRouter();
  const { busy, run } = useAction();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  return (
    <main className="min-h-screen bg-background">
      <header className="mx-auto flex max-w-6xl items-center justify-between p-6">
        <span className="text-xl font-semibold tracking-tight">TabMail</span>
        <ThemeToggle />
      </header>
      <div className="mx-auto grid max-w-6xl gap-10 px-6 py-12 md:grid-cols-2 md:py-24">
        <section className="space-y-6">
          <p className="text-sm font-medium uppercase tracking-widest text-muted-foreground">
            COMPANY MAIL
          </p>
          <h1 className="text-4xl font-semibold tracking-tight leading-tight">
            {t("公司的邮件，\n各尽其责。", "Company mail.\nClear ownership.")}
          </h1>
          <p className="text-lg leading-8 text-muted-foreground">
            {t(
              "个人与共享邮箱、明确的多级授权、管理员维护的发送模板，以及可以追溯的投递与恢复。",
              "Personal and shared mailboxes, explicit permissions, administrator-managed templates and traceable delivery and recovery.",
            )}
          </p>
          <p className="text-sm text-muted-foreground">
            {t(
              "仅限已开通的员工账号。新员工请使用管理员提供的一次性激活链接。",
              "For provisioned employee accounts. New employees should use the single-use activation link from their administrator.",
            )}
          </p>
        </section>
        <Section
          title={
            auth.user
              ? t("工作台", "Workspace")
              : t("员工登录", "Employee sign-in")
          }
        >
          {!auth.hydrated ? (
            <p>{t("检查会话…", "Checking session…")}</p>
          ) : auth.user ? (
            <div className="space-y-4">
              <p>{auth.user.display_name || auth.user.email}</p>
              <Link
                className="block rounded-md bg-primary p-3 text-center text-primary-foreground"
                href="/mail"
              >
                {t("进入邮箱", "Open mail")}
              </Link>
              {auth.level !== "user" && (
                <Link
                  className="block rounded-md border p-3 text-center"
                  href="/company"
                >
                  {t("公司管理", "Company administration")}
                </Link>
              )}
              <ActionButton onClick={() => void auth.logout()}>
                {t("退出账号", "Sign out")}
              </ActionButton>
            </div>
          ) : (
            <form
              className="space-y-5"
              onSubmit={(e) => {
                e.preventDefault();
                void run(async () => {
                  const res = await login(email.trim(), password);
                  auth.loginWithTokens(res.data.access_token, res.data.user);
                  setPassword("");
                  router.push("/mail");
                });
              }}
            >
              <Field label={t("登录邮箱", "Login email")}>
                {(id) => (
                  <input
                    id={id}
                    className={inputClass}
                    type="email"
                    autoComplete="username"
                    required
                    value={email}
                    onChange={(e) => setEmail(e.target.value)}
                  />
                )}
              </Field>
              <Field label={t("密码", "Password")}>
                {(id) => (
                  <input
                    id={id}
                    className={inputClass}
                    type="password"
                    autoComplete="current-password"
                    required
                    value={password}
                    onChange={(e) => setPassword(e.target.value)}
                  />
                )}
              </Field>
              <ActionButton
                type="submit"
                disabled={busy || !email || !password}
                className="w-full bg-primary text-primary-foreground"
              >
                {busy ? t("登录中…", "Signing in…") : t("登录", "Sign in")}
              </ActionButton>
              <p className="text-xs text-muted-foreground">
                {t(
                  "没有账号或无法登录？请联系公司管理员。",
                  "No account or unable to sign in? Contact your company administrator.",
                )}
              </p>
            </form>
          )}
        </Section>
      </div>
    </main>
  );
}
