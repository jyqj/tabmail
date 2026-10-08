"use client";

import { useId, useLayoutEffect, useRef, useState, type KeyboardEvent } from "react";
import { useAuth } from "@/contexts/auth-context";
import { useI18n } from "@/lib/i18n";
import { login, register } from "@/lib/api";
import { sessionScope } from "@/lib/session";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  LogIn,
  LogOut,
  Mail,
  Copy,
  Check,
  Lock,
  Eye,
  EyeOff,
  ArrowRight,
  Globe,
  Layers,
  Code2,
  User,
} from "lucide-react";
import { toast } from "sonner";
import { cn } from "@/lib/utils";
import { TabMailLogo } from "@/components/tabmail-logo";

type AuthMode = "login" | "register";
type AuthAttempt = { owner: object; before: string; remountScope?: string };

export function AuthDialog() {
  const { level, user, logout } = useAuth();
  const { t } = useI18n();
  const [open, setOpen] = useState(false);
  const [copied, setCopied] = useState(false);
  const [mode, setMode] = useState<AuthMode>("login");
  const [showPwd, setShowPwd] = useState(false);

  const [loginEmail, setLoginEmail] = useState("");
  const [loginPassword, setLoginPassword] = useState("");
  const [loginLoading, setLoginLoading] = useState(false);

  const [regEmail, setRegEmail] = useState("");
  const [regPassword, setRegPassword] = useState("");
  const [regName, setRegName] = useState("");
  const [regLoading, setRegLoading] = useState(false);
  const registrationHint = useId();
  // Match the server's bcrypt input policy, preserving every password byte.
  const registrationBytes = new TextEncoder().encode(regPassword).length;
  const validRegistrationPassword = registrationBytes >= 12 && registrationBytes <= 72;
  const submitting = loginLoading || regLoading;
  const pending = useRef<AuthAttempt | null>(null);
  const view = useRef<object | null>(null);
  useLayoutEffect(() => {
    view.current = {};
    return () => {
      const operation = pending.current;
      if (operation && view.current === operation.owner) {
        const after = sessionScope();
        // AuthProvider remounts its cache subtree when the request installs
        // identity. Preserve only this still-owned attempt's exact new scope;
        // ordinary departures and previously retired forms get no receipt.
        if (after !== operation.before) operation.remountScope = after;
      }
      view.current = null;
    };
  }, []);
  const retireView = () => { if (view.current) view.current = {}; };
  const changeOpen = (nextOpen: boolean) => {
    retireView();
    setOpen(nextOpen);
    if (nextOpen) {
      setShowPwd(false);
      setMode("login");
    }
  };
  const changeMode = (nextMode: AuthMode) => {
    retireView();
    setMode(nextMode);
    setShowPwd(false);
  };
  const handleEnter = (event: KeyboardEvent<HTMLInputElement>, submit: () => Promise<void>) => {
    if (event.key !== "Enter" || event.nativeEvent.isComposing || event.nativeEvent.keyCode === 229) return;
    event.preventDefault();
    void submit();
  };

  const submitAuth = async (kind: AuthMode) => {
    // State only disables the rendered button. A ref also excludes same-turn
    // clicks, Enter events and a different form while the cookie write settles.
    if (pending.current || !view.current) return;
    const email = kind === "login" ? loginEmail.trim() : regEmail.trim();
    const password = kind === "login" ? loginPassword : regPassword;
    if (!email || !password) return;
    if (kind === "register" && !validRegistrationPassword) {
      toast.error(t("auth.passwordMinLength"));
      return;
    }
    const owner = view.current, before = sessionScope();
    const operation: AuthAttempt = { owner, before };
    pending.current = operation;
    setLoginLoading(kind === "login");
    setRegLoading(kind === "register");
    try {
      const res = kind === "login"
        ? await login(email, password)
        : await register(email, password, regName.trim() || undefined);
      // The request layer installs the identity while holding the shared cookie
      // lock. Never reinstall it from a component continuation after release.
      const ownsView = view.current === owner;
      const ownsRemount = operation.remountScope !== undefined && operation.remountScope === sessionScope();
      if ((!ownsView && !ownsRemount) ||
          localStorage.getItem("tabmail_access_token") !== res.data.access_token ||
          localStorage.getItem("tabmail_user") !== JSON.stringify(res.data.user) ||
          localStorage.getItem("tabmail_tenant_id") !== res.data.user.tenant_id) return;
      if (ownsView) {
        if (kind === "login") {
          setLoginEmail("");
          setLoginPassword("");
        } else {
          setRegEmail("");
          setRegPassword("");
          setRegName("");
        }
        changeOpen(false);
      }
      toast.success(kind === "login"
        ? `Welcome, ${res.data.user.display_name || res.data.user.email}`
        : `Account created! Welcome, ${res.data.user.display_name}`);
    } catch (e: unknown) {
      if (view.current !== owner || before !== sessionScope()) return;
      const err = e as { error?: { message?: string } };
      toast.error(err?.error?.message || (kind === "login" ? "Login failed" : "Registration failed"));
    } finally {
      if (pending.current === operation) pending.current = null;
      if (view.current) {
        setLoginLoading(false);
        setRegLoading(false);
      }
    }
  };
  const handleLogin = () => submitAuth("login");
  const handleRegister = () => submitAuth("register");

  const handleLogout = () => logout();

  const handleCopy = () => {
    const value = user?.email || "";
    if (!value) return;
    navigator.clipboard.writeText(value);
    setCopied(true);
    toast.success(t("auth.keyCopied"));
    setTimeout(() => setCopied(false), 2000);
  };

  if (level !== "public") {
    return (
      <div className="flex items-center gap-1.5">
        <div className="flex items-center gap-2 rounded-lg bg-muted/60 px-2.5 py-1.5">
          {user ? (
            <>
              <span className="text-[11px] font-semibold uppercase tracking-wide text-primary">
                {user.role === "super_admin"
                  ? "Super Admin"
                  : user.role === "admin"
                    ? "Admin"
                    : "User"}
              </span>
              <span className="text-[11px] text-muted-foreground max-w-[100px] sm:max-w-[180px] truncate">
                {user.display_name || user.email}
              </span>
            </>
          ) : null}
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
        <Button
          variant="ghost"
          size="icon"
          onClick={handleLogout}
          className="h-8 w-8"
        >
          <LogOut className="h-3.5 w-3.5" />
        </Button>
      </div>
    );
  }

  const features = [
    { icon: Globe, text: t("auth.feat1") },
    { icon: Layers, text: t("auth.feat2") },
    { icon: Code2, text: t("auth.feat3") },
  ];

  const inputBase =
    "h-11 pl-10 text-sm bg-background border-border/80 rounded-lg focus-visible:ring-2 focus-visible:ring-primary/30 focus-visible:border-primary/50 transition-all";
  const iconBase =
    "absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground/60 pointer-events-none";

  return (
    <Dialog
      open={open}
      onOpenChange={changeOpen}
    >
      <DialogTrigger
        render={<Button variant="outline" size="sm" className="gap-1.5" />}
      >
        <LogIn className="h-3.5 w-3.5" />
        {t("auth.connect")}
      </DialogTrigger>

      <DialogContent
        showCloseButton={false}
        className="sm:max-w-[840px] p-0 overflow-hidden gap-0 ring-1 ring-border/60 shadow-2xl shadow-black/10"
      >
        <DialogTitle className="sr-only">{t("auth.title")}</DialogTitle>
        <DialogDescription className="sr-only">
          {t("auth.desc")}
        </DialogDescription>

        <div className="grid sm:grid-cols-[320px_1fr] min-h-[480px]">
          {/* ── Left: Brand panel ── */}
          <div className="hidden sm:flex flex-col justify-between p-8 relative overflow-hidden bg-gradient-to-br from-[hsl(174,100%,26%)] via-[hsl(174,80%,30%)] to-[hsl(168,55%,20%)]">
            <div
              className="absolute inset-0 opacity-[0.06]"
              style={{
                backgroundImage:
                  "linear-gradient(to bottom, white 1px, transparent 1px), linear-gradient(to right, white 1px, transparent 1px)",
                backgroundSize: "36px 36px",
              }}
            />
            <div className="absolute -top-12 -right-12 w-48 h-48 rounded-full bg-white/[0.06] blur-2xl" />
            <div className="absolute bottom-20 -left-10 w-36 h-36 rounded-full bg-white/[0.04] blur-2xl" />
            <div className="absolute top-1/2 right-4 w-20 h-20 rounded-full bg-emerald-300/[0.06] blur-xl" />

            <div className="relative z-10">
              <div className="flex items-center gap-2.5 mb-10">
                <div className="flex h-9 w-9 items-center justify-center rounded-lg bg-white/[0.12] backdrop-blur-sm">
                  <TabMailLogo size={20} />
                </div>
                <span className="font-semibold text-[17px] tracking-tight text-white">
                  TabMail
                </span>
              </div>
              <h2 className="text-[22px] font-bold leading-tight tracking-tight text-white mb-3">
                {t("auth.brandTitle")}
              </h2>
              <p className="text-[13px] text-white/55 leading-relaxed max-w-[260px]">
                {t("auth.brandDesc")}
              </p>
            </div>

            <div className="relative z-10 space-y-2.5">
              {features.map((feat) => (
                <div
                  key={feat.text}
                  className="flex items-center gap-3 rounded-lg bg-white/[0.06] px-3.5 py-2.5 backdrop-blur-sm"
                >
                  <div className="flex h-7 w-7 shrink-0 items-center justify-center rounded-md bg-white/[0.1]">
                    <feat.icon className="h-3.5 w-3.5 text-white/80" />
                  </div>
                  <span className="text-[13px] font-medium text-white/75">
                    {feat.text}
                  </span>
                </div>
              ))}
            </div>
          </div>

          {/* ── Right: Form panel ── */}
          <div className="flex flex-col p-6 sm:p-8 relative bg-gradient-to-b from-muted/30 to-transparent">
            <button
              onClick={() => changeOpen(false)}
              aria-label={t("auth.close")}
              className="absolute top-4 right-4 flex h-7 w-7 items-center justify-center rounded-md text-muted-foreground/50 hover:text-foreground hover:bg-muted/80 transition-colors"
            >
              <svg width="14" height="14" viewBox="0 0 14 14" fill="none">
                <path
                  d="M1 1l12 12M13 1L1 13"
                  stroke="currentColor"
                  strokeWidth="1.5"
                  strokeLinecap="round"
                />
              </svg>
            </button>

            <div className="absolute top-0 left-0 right-0 h-[2px] bg-gradient-to-r from-transparent via-primary/20 to-transparent" />

            {/* Title */}
            <div className="mb-7">
              <h3 className="text-xl font-semibold tracking-tight">
                {mode === "login"
                  ? t("auth.loginTitle")
                  : t("auth.registerTitle")}
              </h3>
              <p className="text-[13px] text-muted-foreground mt-1.5 leading-relaxed">
                {mode === "login"
                  ? t("auth.loginSubtitle")
                  : t("auth.registerSubtitle")}
              </p>
            </div>

            {/* ── Login form ── */}
            {mode === "login" && (
              <div className="flex-1 flex flex-col">
                <div className="space-y-4 flex-1">
                  <div className="space-y-1.5">
                    <label
                      htmlFor="auth-login-email"
                      className="text-[13px] font-medium text-foreground/80"
                    >
                      {t("auth.email")}
                    </label>
                    <div className="relative">
                      <Mail className={iconBase} />
                      <Input
                        id="auth-login-email"
                        className={inputBase}
                        type="email"
                        placeholder="you@example.com"
                        value={loginEmail}
                        onChange={(e) => { retireView(); setLoginEmail(e.target.value); }}
                        onKeyDown={(e) => handleEnter(e, handleLogin)}
                        autoFocus
                      />
                    </div>
                  </div>
                  <div className="space-y-1.5">
                    <label
                      htmlFor="auth-login-password"
                      className="text-[13px] font-medium text-foreground/80"
                    >
                      {t("auth.password")}
                    </label>
                    <div className="relative">
                      <Lock className={iconBase} />
                      <Input
                        id="auth-login-password"
                        className={cn(inputBase, "pr-10")}
                        type={showPwd ? "text" : "password"}
                        placeholder="••••••••"
                        value={loginPassword}
                        onChange={(e) => { retireView(); setLoginPassword(e.target.value); }}
                        onKeyDown={(e) => handleEnter(e, handleLogin)}
                      />
                      <button
                        type="button"
                        onClick={() => setShowPwd(!showPwd)}
                        className="absolute right-3 top-1/2 -translate-y-1/2 text-muted-foreground/40 hover:text-muted-foreground transition-colors"
                        tabIndex={-1}
                      >
                        {showPwd ? (
                          <EyeOff className="h-4 w-4" />
                        ) : (
                          <Eye className="h-4 w-4" />
                        )}
                      </button>
                    </div>
                  </div>
                  <Button
                    className="w-full h-11 mt-2 gap-2 text-[13px] font-semibold shadow-sm"
                    onClick={handleLogin}
                    disabled={
                      submitting ||
                      !loginEmail.trim() ||
                      !loginPassword
                    }
                  >
                    {loginLoading ? t("auth.loggingIn") : t("auth.loginBtn")}
                    {!loginLoading && <ArrowRight className="h-3.5 w-3.5" />}
                  </Button>
                </div>
                <div className="mt-6 pt-5 border-t border-border/40 text-center">
                  <p className="text-[13px] text-muted-foreground">
                    {t("auth.noAccount")}{" "}
                    <button
                      onClick={() => changeMode("register")}
                      className="text-primary hover:text-primary/80 font-medium transition-colors cursor-pointer"
                    >
                      {t("auth.registerLink")}
                    </button>
                  </p>
                </div>
              </div>
            )}

            {/* ── Register form ── */}
            {mode === "register" && (
              <div className="flex-1 flex flex-col">
                <div className="space-y-4 flex-1">
                  <div className="space-y-1.5">
                    <label
                      htmlFor="auth-register-email"
                      className="text-[13px] font-medium text-foreground/80"
                    >
                      {t("auth.email")}
                    </label>
                    <div className="relative">
                      <Mail className={iconBase} />
                      <Input
                        id="auth-register-email"
                        className={inputBase}
                        type="email"
                        placeholder="you@example.com"
                        value={regEmail}
                        onChange={(e) => { retireView(); setRegEmail(e.target.value); }}
                        onKeyDown={(e) => handleEnter(e, handleRegister)}
                        autoFocus
                      />
                    </div>
                  </div>
                  <div className="space-y-1.5">
                    <div className="flex items-baseline justify-between">
                      <label
                        htmlFor="auth-register-name"
                        className="text-[13px] font-medium text-foreground/80"
                      >
                        {t("auth.displayName")}
                      </label>
                      <span className="text-[11px] text-muted-foreground/50">
                        {t("auth.displayNameOpt")}
                      </span>
                    </div>
                    <div className="relative">
                      <User className={iconBase} />
                      <Input
                        id="auth-register-name"
                        className={inputBase}
                        placeholder={t("auth.displayNamePlaceholder")}
                        value={regName}
                        onChange={(e) => { retireView(); setRegName(e.target.value); }}
                        onKeyDown={(e) => handleEnter(e, handleRegister)}
                      />
                    </div>
                  </div>
                  <div className="space-y-1.5">
                    <div className="flex items-baseline justify-between">
                      <label
                        htmlFor="auth-register-password"
                        className="text-[13px] font-medium text-foreground/80"
                      >
                        {t("auth.password")}
                      </label>
                      <span className="text-[11px] text-muted-foreground/50">
                        {t("auth.passwordMin")}
                      </span>
                    </div>
                    <div className="relative">
                      <Lock className={iconBase} />
                      <Input
                        id="auth-register-password"
                        className={cn(inputBase, "pr-10")}
                        type={showPwd ? "text" : "password"}
                        placeholder="••••••••"
                        value={regPassword}
                        autoComplete="new-password"
                        aria-describedby={registrationHint}
                        aria-invalid={regPassword.length > 0 && !validRegistrationPassword ? true : undefined}
                        onChange={(e) => { retireView(); setRegPassword(e.target.value); }}
                        onKeyDown={(e) => handleEnter(e, handleRegister)}
                      />
                      <button
                        type="button"
                        onClick={() => setShowPwd(!showPwd)}
                        className="absolute right-3 top-1/2 -translate-y-1/2 text-muted-foreground/40 hover:text-muted-foreground transition-colors"
                        tabIndex={-1}
                      >
                        {showPwd ? (
                          <EyeOff className="h-4 w-4" />
                        ) : (
                          <Eye className="h-4 w-4" />
                        )}
                      </button>
                    </div>
                    <p id={registrationHint} className="text-xs text-muted-foreground" aria-live="polite">
                      {t("auth.passwordBytes", { bytes: registrationBytes })}
                    </p>
                  </div>
                  <Button
                    className="w-full h-11 mt-2 gap-2 text-[13px] font-semibold shadow-sm"
                    onClick={handleRegister}
                    disabled={
                      submitting || !regEmail.trim() || !validRegistrationPassword
                    }
                  >
                    {regLoading ? t("auth.registering") : t("auth.registerBtn")}
                    {!regLoading && <ArrowRight className="h-3.5 w-3.5" />}
                  </Button>
                </div>
                <div className="mt-6 pt-5 border-t border-border/40 text-center">
                  <p className="text-[13px] text-muted-foreground">
                    {t("auth.hasAccount")}{" "}
                    <button
                      onClick={() => changeMode("login")}
                      className="text-primary hover:text-primary/80 font-medium transition-colors cursor-pointer"
                    >
                      {t("auth.loginLink")}
                    </button>
                  </p>
                </div>
              </div>
            )}
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
}
