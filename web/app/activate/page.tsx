"use client";
import { useState } from "react";
import Link from "next/link";
import { activateEmployee, companyError } from "@/lib/api/company";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
export default function ActivatePage() {
  const [token, setToken] = useState("");
  const [password, setPassword] = useState("");
  const [confirmation, setConfirmation] = useState("");
  const [busy, setBusy] = useState(false);
  const [done, setDone] = useState(false);
  const [error, setError] = useState("");
  async function activate() {
    if (password !== confirmation) {
      setError("两次密码不一致");
      return;
    }
    setBusy(true);
    setError("");
    try {
      await activateEmployee(token.trim(), password);
      setToken("");
      setPassword("");
      setConfirmation("");
      setDone(true);
    } catch (e) {
      setError(companyError(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <main className="mx-auto mt-20 max-w-md space-y-6 rounded-xl border p-8">
      <h1 className="text-2xl font-semibold">激活公司账号</h1>
      {done ? (
        <>
          <p>账号已激活，请使用公司邮箱和新密码登录。</p>
          <Link className="underline" href="/">
            前往登录
          </Link>
        </>
      ) : (
        <>
          <p className="text-sm text-muted-foreground">
            请输入管理员通过可信渠道提供的一次性激活码。激活码不会放进 URL
            或浏览器存储。
          </p>
          <label className="block space-y-2">
            激活码
            <Input
              type="password"
              value={token}
              onChange={(e) => setToken(e.target.value)}
              autoComplete="off"
            />
          </label>
          <label className="block space-y-2">
            新密码
            <Input
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              autoComplete="new-password"
            />
          </label>
          <p className="text-xs text-muted-foreground">
            12–72 字节。中文等字符会占用多个字节。
          </p>
          <label className="block space-y-2">
            确认密码
            <Input
              type="password"
              value={confirmation}
              onChange={(e) => setConfirmation(e.target.value)}
              autoComplete="new-password"
            />
          </label>
          {error && (
            <p role="alert" className="text-destructive">
              {error}
            </p>
          )}
          <Button disabled={busy || !token || !password} onClick={activate}>
            {busy ? "正在激活…" : "激活账号"}
          </Button>
        </>
      )}
    </main>
  );
}
