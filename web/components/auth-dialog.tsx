"use client";

import { useState } from "react";
import { useAuth } from "@/contexts/auth-context";
import { useI18n } from "@/lib/i18n";
import { issueToken, login, register, logoutSession } from "@/lib/api";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { KeyRound, LogOut, Mail, Copy, Check, User, UserPlus } from "lucide-react";
import { toast } from "sonner";

export function AuthDialog() {
  const {
    level,
    user,
    mailboxAddress,
    refreshToken,
    loginWithTokens,
    setMailboxAuth,
    logout,
  } = useAuth();
  const { t } = useI18n();
  const [open, setOpen] = useState(false);
  const [copied, setCopied] = useState(false);

  // Login form state
  const [loginEmail, setLoginEmail] = useState("");
  const [loginPassword, setLoginPassword] = useState("");
  const [loginLoading, setLoginLoading] = useState(false);

  // Register form state
  const [regEmail, setRegEmail] = useState("");
  const [regPassword, setRegPassword] = useState("");
  const [regName, setRegName] = useState("");
  const [regLoading, setRegLoading] = useState(false);

  const [mailboxAddressInput, setMailboxAddressInput] = useState(mailboxAddress || "");
  const [mailboxPassword, setMailboxPassword] = useState("");
  const [mailboxLoading, setMailboxLoading] = useState(false);

  const handleLogin = async () => {
    if (!loginEmail.trim() || !loginPassword.trim()) return;
    setLoginLoading(true);
    try {
      const res = await login(loginEmail.trim(), loginPassword);
      loginWithTokens(res.data.access_token, res.data.refresh_token, res.data.user);
      setLoginEmail("");
      setLoginPassword("");
      setOpen(false);
      toast.success(`Welcome, ${res.data.user.display_name || res.data.user.email}`);
    } catch (e: unknown) {
      const err = e as { error?: { message?: string } };
      toast.error(err?.error?.message || "Login failed");
    } finally {
      setLoginLoading(false);
    }
  };

  const handleRegister = async () => {
    if (!regEmail.trim() || !regPassword.trim()) return;
    if (regPassword.length < 8) {
      toast.error("Password must be at least 8 characters");
      return;
    }
    setRegLoading(true);
    try {
      const res = await register(regEmail.trim(), regPassword, regName.trim() || undefined);
      loginWithTokens(res.data.access_token, res.data.refresh_token, res.data.user);
      setRegEmail("");
      setRegPassword("");
      setRegName("");
      setOpen(false);
      toast.success(`Account created! Welcome, ${res.data.user.display_name}`);
    } catch (e: unknown) {
      const err = e as { error?: { message?: string } };
      toast.error(err?.error?.message || "Registration failed");
    } finally {
      setRegLoading(false);
    }
  };

  const handleMailboxLogin = async () => {
    if (!mailboxAddressInput.trim() || !mailboxPassword.trim()) return;
    setMailboxLoading(true);
    try {
      const res = await issueToken(mailboxAddressInput.trim(), mailboxPassword);
      setMailboxAuth(mailboxAddressInput.trim(), res.data.token);
      setMailboxPassword("");
      setOpen(false);
      toast.success(t("toast.tokenIssued"));
    } catch (e: unknown) {
      const err = e as { error?: { message?: string } };
      toast.error(err?.error?.message || t("toast.authFailed"));
    } finally {
      setMailboxLoading(false);
    }
  };

  const handleLogout = async () => {
    try {
      if (refreshToken) {
        await logoutSession(refreshToken).catch(() => {});
      }
    } finally {
      logout();
    }
  };

  const handleCopy = () => {
    const value = user?.email || mailboxAddress || "";
    if (!value) return;
    navigator.clipboard.writeText(value);
    setCopied(true);
    toast.success(t("auth.keyCopied"));
    setTimeout(() => setCopied(false), 2000);
  };

  if (level !== "public") {
    const levelLabel =
      level === "admin" ? t("auth.level.admin")
        : level === "user" ? (user?.display_name || user?.email || "User")
          : t("auth.level.mailbox");
    const displayValue = user?.email || mailboxAddress || "";

    return (
      <div className="flex items-center gap-1.5">
        <div className="flex items-center gap-2 rounded-lg bg-muted/60 px-2.5 py-1.5">
          {user ? (
            <>
              <span className="text-[11px] font-semibold uppercase tracking-wide text-primary">
                {user.role === "admin" ? "Admin" : "User"}
              </span>
              <span className="text-[11px] text-muted-foreground max-w-[100px] sm:max-w-[180px] truncate">
                {user.display_name || user.email}
              </span>
            </>
          ) : (
            <>
              <span className="text-[11px] font-semibold uppercase tracking-wide text-primary">
                {levelLabel}
              </span>
              <code className="text-[11px] text-muted-foreground max-w-[100px] sm:max-w-[140px] truncate">
                {displayValue.slice(0, 24)}
              </code>
            </>
          )}
          <button
            onClick={handleCopy}
            className="shrink-0 text-muted-foreground hover:text-foreground transition-colors"
          >
            {copied ? (
              <Check className="h-3 w-3 text-emerald-500" />
            ) : (
              <Copy className="h-3 w-3" />
            )}
          </button>
        </div>
        <Button variant="ghost" size="icon" onClick={handleLogout} className="h-8 w-8">
          <LogOut className="h-3.5 w-3.5" />
        </Button>
      </div>
    );
  }

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger render={<Button variant="outline" size="sm" className="gap-1.5" />}>
        <KeyRound className="h-3.5 w-3.5" />
        {t("auth.connect")}
      </DialogTrigger>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{t("auth.title")}</DialogTitle>
          <DialogDescription>{t("auth.desc")}</DialogDescription>
        </DialogHeader>
        <Tabs defaultValue="login" className="mt-2">
          <TabsList className="grid w-full grid-cols-3">
            <TabsTrigger value="login" className="gap-1">
              <User className="h-3.5 w-3.5" />
              Login
            </TabsTrigger>
            <TabsTrigger value="register" className="gap-1">
              <UserPlus className="h-3.5 w-3.5" />
              Register
            </TabsTrigger>
            <TabsTrigger value="mailbox" className="gap-1">
              <Mail className="h-3.5 w-3.5" />
              {t("auth.mailbox")}
            </TabsTrigger>
          </TabsList>

          {/* Login tab */}
          <TabsContent value="login" className="space-y-4 pt-4">
            <div className="space-y-2">
              <Label htmlFor="login-email">Email</Label>
              <Input id="login-email" type="email" placeholder="you@example.com" value={loginEmail} onChange={(e) => setLoginEmail(e.target.value)} onKeyDown={(e) => e.key === "Enter" && handleLogin()} />
            </div>
            <div className="space-y-2">
              <Label htmlFor="login-password">Password</Label>
              <Input id="login-password" type="password" placeholder="••••••••" value={loginPassword} onChange={(e) => setLoginPassword(e.target.value)} onKeyDown={(e) => e.key === "Enter" && handleLogin()} />
            </div>
            <DialogFooter>
              <Button onClick={handleLogin} disabled={loginLoading || !loginEmail.trim() || !loginPassword.trim()}>
                {loginLoading ? "Logging in..." : "Login"}
              </Button>
            </DialogFooter>
          </TabsContent>

          {/* Register tab */}
          <TabsContent value="register" className="space-y-4 pt-4">
            <div className="space-y-2">
              <Label htmlFor="reg-email">Email</Label>
              <Input id="reg-email" type="email" placeholder="you@example.com" value={regEmail} onChange={(e) => setRegEmail(e.target.value)} />
            </div>
            <div className="space-y-2">
              <Label htmlFor="reg-name">Display Name (optional)</Label>
              <Input id="reg-name" placeholder="Your name" value={regName} onChange={(e) => setRegName(e.target.value)} />
            </div>
            <div className="space-y-2">
              <Label htmlFor="reg-password">Password (min 8 chars)</Label>
              <Input id="reg-password" type="password" placeholder="••••••••" value={regPassword} onChange={(e) => setRegPassword(e.target.value)} onKeyDown={(e) => e.key === "Enter" && handleRegister()} />
            </div>
            <DialogFooter>
              <Button onClick={handleRegister} disabled={regLoading || !regEmail.trim() || regPassword.length < 8}>
                {regLoading ? "Creating account..." : "Register"}
              </Button>
            </DialogFooter>
          </TabsContent>

          {/* Mailbox tab */}
          <TabsContent value="mailbox" className="space-y-4 pt-4">
            <div className="space-y-2">
              <Label htmlFor="mailbox-address">{t("auth.mailboxAddr")}</Label>
              <Input id="mailbox-address" placeholder={t("auth.mailboxAddrPh")} value={mailboxAddressInput} onChange={(e) => setMailboxAddressInput(e.target.value)} />
            </div>
            <div className="space-y-2">
              <Label htmlFor="mailbox-password">{t("auth.mailboxPwd")}</Label>
              <Input id="mailbox-password" type="password" placeholder={t("auth.mailboxPwdPh")} value={mailboxPassword} onChange={(e) => setMailboxPassword(e.target.value)} onKeyDown={(e) => e.key === "Enter" && handleMailboxLogin()} />
            </div>
            <DialogFooter>
              <Button onClick={handleMailboxLogin} disabled={mailboxLoading || !mailboxAddressInput.trim() || !mailboxPassword.trim()}>
                {mailboxLoading ? t("inbox.connecting") : t("auth.connect")}
              </Button>
            </DialogFooter>
          </TabsContent>

        </Tabs>
      </DialogContent>
    </Dialog>
  );
}
