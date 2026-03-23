"use client";

import { useState } from "react";
import { useAuth } from "@/contexts/auth-context";
import { useI18n } from "@/lib/i18n";
import { issueToken } from "@/lib/api";
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
import { KeyRound, Shield, LogOut, Mail, Copy, Check } from "lucide-react";
import { toast } from "sonner";

export function AuthDialog() {
  const {
    level,
    adminKey,
    apiKey,
    tenantId,
    mailboxAddress,
    setAdminKey,
    setApiKey,
    setTenantId,
    setMailboxAuth,
    clearMailboxAuth,
    logout,
  } = useAuth();
  const { t } = useI18n();
  const [open, setOpen] = useState(false);
  const [inputAdminKey, setInputAdminKey] = useState("");
  const [inputApiKey, setInputApiKey] = useState("");
  const [inputTenantId, setInputTenantId] = useState(tenantId || "");
  const [mailboxAddressInput, setMailboxAddressInput] = useState(mailboxAddress || "");
  const [mailboxPassword, setMailboxPassword] = useState("");
  const [mailboxLoading, setMailboxLoading] = useState(false);
  const [copied, setCopied] = useState(false);

  const handleAdminLogin = () => {
    if (!inputAdminKey.trim()) return;
    clearMailboxAuth();
    setAdminKey(inputAdminKey.trim());
    setApiKey(null);
    setTenantId(inputTenantId.trim() || null);
    setInputAdminKey("");
    setOpen(false);
    toast.success(t("toast.adminOk"));
  };

  const handleTenantLogin = () => {
    if (!inputApiKey.trim()) return;
    clearMailboxAuth();
    setAdminKey(null);
    setApiKey(inputApiKey.trim());
    setInputApiKey("");
    setOpen(false);
    toast.success(t("toast.apiKeyOk"));
  };

  const handleMailboxLogin = async () => {
    if (!mailboxAddressInput.trim() || !mailboxPassword.trim()) return;
    setMailboxLoading(true);
    try {
      const res = await issueToken(mailboxAddressInput.trim(), mailboxPassword);
      setAdminKey(null);
      setApiKey(null);
      setTenantId(null);
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

  const handleCopy = () => {
    const value = mailboxAddress || adminKey || apiKey || "";
    if (!value) return;
    navigator.clipboard.writeText(value);
    setCopied(true);
    toast.success(t("auth.keyCopied"));
    setTimeout(() => setCopied(false), 2000);
  };

  if (level !== "public") {
    const levelLabel =
      level === "admin" ? t("auth.level.admin")
        : level === "tenant" ? t("auth.level.tenant")
          : t("auth.level.mailbox");
    const displayValue = mailboxAddress || adminKey || apiKey || "";

    return (
      <div className="flex items-center gap-1.5">
        <div className="flex items-center gap-2 rounded-lg bg-muted/60 px-2.5 py-1.5">
          <span className="text-[11px] font-semibold uppercase tracking-wide text-primary">
            {levelLabel}
          </span>
          <code className="text-[11px] text-muted-foreground max-w-[100px] sm:max-w-[140px] truncate">
            {displayValue.slice(0, 24)}
          </code>
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
        <Button variant="ghost" size="icon" onClick={logout} className="h-8 w-8">
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
        <Tabs defaultValue="mailbox" className="mt-2">
          <TabsList className="grid w-full grid-cols-3">
            <TabsTrigger value="mailbox" className="gap-1.5">
              <Mail className="h-3.5 w-3.5" />
              {t("auth.mailbox")}
            </TabsTrigger>
            <TabsTrigger value="apikey" className="gap-1.5">
              <KeyRound className="h-3.5 w-3.5" />
              {t("auth.apiKey")}
            </TabsTrigger>
            <TabsTrigger value="admin" className="gap-1.5">
              <Shield className="h-3.5 w-3.5" />
              {t("auth.admin")}
            </TabsTrigger>
          </TabsList>
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
          <TabsContent value="apikey" className="space-y-4 pt-4">
            <div className="space-y-2">
              <Label htmlFor="api-key">{t("auth.tenantKey")}</Label>
              <Input id="api-key" type="password" placeholder={t("auth.tenantKeyPh")} value={inputApiKey} onChange={(e) => setInputApiKey(e.target.value)} onKeyDown={(e) => e.key === "Enter" && handleTenantLogin()} />
            </div>
            <DialogFooter>
              <Button onClick={handleTenantLogin} disabled={!inputApiKey.trim()}>{t("auth.connect")}</Button>
            </DialogFooter>
          </TabsContent>
          <TabsContent value="admin" className="space-y-4 pt-4">
            <div className="space-y-2">
              <Label htmlFor="admin-key">{t("auth.adminKey")}</Label>
              <Input id="admin-key" type="password" placeholder={t("auth.adminKeyPh")} value={inputAdminKey} onChange={(e) => setInputAdminKey(e.target.value)} onKeyDown={(e) => e.key === "Enter" && handleAdminLogin()} />
            </div>
            <div className="space-y-2">
              <Label htmlFor="tenant-id">{t("auth.tenantId")}</Label>
              <Input id="tenant-id" placeholder={t("auth.tenantIdPh")} value={inputTenantId} onChange={(e) => setInputTenantId(e.target.value)} />
            </div>
            <DialogFooter>
              <Button onClick={handleAdminLogin} disabled={!inputAdminKey.trim()}>{t("auth.connect")}</Button>
            </DialogFooter>
          </TabsContent>
        </Tabs>
      </DialogContent>
    </Dialog>
  );
}
