"use client";
import Link from "next/link";
import { useEffect, useId, useLayoutEffect, useRef, useState } from "react";
import { company } from "@/lib/company";
import { sessionScope, useSessionScope } from "@/lib/session";
import {
  ActionButton,
  Field,
  inputClass,
  Section,
  useAction,
  useText,
} from "@/components/company/common";
export default function ActivatePage() {
  const scope = useSessionScope();
  const [invitation, setInvitation] = useState({ token: "", generation: 0 });
  const invitationOwner = useRef(invitation);
  useEffect(() => {
    const read = (event?: Event) => {
      const token = window.location.hash.slice(1);
      const current = invitationOwner.current;
      // Queued hash events may both observe the final URL after A -> B -> A.
      // Retire ownership from the event itself, before React commits a form.
      const navigated = event?.type === "popstate" ||
        (event instanceof HashChangeEvent && event.oldURL !== event.newURL);
      if (current.token === token && !navigated) return;
      const next = { token, generation: current.generation + 1 };
      invitationOwner.current = next;
      setInvitation(next);
    };
    read();
    window.addEventListener("hashchange", read);
    window.addEventListener("popstate", read);
    return () => {
      window.removeEventListener("hashchange", read);
      window.removeEventListener("popstate", read);
    };
  }, []);
  // A new invitation or session owns a new form. Keep the one-shot capability
  // out of component keys, and retire earlier forms even after away-and-back.
  return <ActivationForm key={JSON.stringify([invitation.generation, scope])} token={invitation.token} scope={scope}
    ownsInvitation={() => invitationOwner.current === invitation} />;
}

function ActivationForm({ token, scope, ownsInvitation }: { token: string; scope: string; ownsInvitation: () => boolean }) {
  const t = useText();
  const { busy, run } = useAction();
  const [password, setPassword] = useState("");
  const [confirm, setConfirm] = useState("");
  const [done, setDone] = useState(false);
  const passwordHint = useId();
  const passwordBytes = new TextEncoder().encode(password).length;
  const validPassword = passwordBytes >= 12 && passwordBytes <= 72;
  const view = useRef<object | null>(null);
  useLayoutEffect(() => {
    view.current = {};
    return () => { view.current = null; };
  }, []);
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
                const owner = view.current, url = window.location.href;
                const current = () => owner !== null && view.current === owner && ownsInvitation() &&
                  scope === sessionScope() && window.location.href === url && window.location.hash.slice(1) === token;
                try {
                  if (!current()) return;
                  if (!/^[a-f0-9]{64}$/.test(token))
                    throw new Error(
                      t(
                        "链接无效，请向管理员索取完整激活链接。",
                        "Invalid link. Ask your administrator for the complete activation link.",
                      ),
                    );
                  if (password !== confirm)
                    throw new Error(t("两次密码不一致", "Passwords do not match"));
                  if (!validPassword)
                    throw new Error(t("新密码必须为 12–72 个 UTF-8 字节", "New password must be 12–72 UTF-8 bytes"));
                  const response = await company<{ activated: boolean }>("/activate", {
                    method: "POST",
                    body: { token, password },
                  });
                  if (!current()) return;
                  if (response?.activated !== true)
                    throw new Error(t("无法确认激活结果，请重试或联系管理员。", "Activation could not be confirmed. Retry or contact your administrator."));
                  // Only remove this confirmed capability, preserving Next's
                  // history state and unrelated query parameters.
                  history.replaceState(history.state, "", window.location.pathname + window.location.search);
                  setPassword("");
                  setConfirm("");
                  setDone(true);
                } catch (error) {
                  if (current()) throw error;
                }
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
                  required
                  disabled={busy}
                  aria-describedby={passwordHint}
                  aria-invalid={password.length > 0 && !validPassword ? true : undefined}
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
                  disabled={busy}
                  value={confirm}
                  onChange={(e) => setConfirm(e.target.value)}
                />
              )}
            </Field>
            <p id={passwordHint} className="text-sm text-muted-foreground" aria-live="polite">
              {t(
                `新密码当前为 ${passwordBytes} 个 UTF-8 字节，需要 12–72 字节。`,
                `New password: ${passwordBytes} UTF-8 bytes. Enter 12–72 bytes.`,
              )}
            </p>
            <ActionButton
              type="submit"
              disabled={busy || !validPassword || password !== confirm}
            >
              {t("激活并开通邮箱", "Activate and provision mailbox")}
            </ActionButton>
          </form>
        )}
      </Section>
    </main>
  );
}
