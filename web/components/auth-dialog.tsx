"use client";

import { useState } from "react";
import { useAuth } from "@/contexts/auth-context";
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
import { KeyRound, Shield, LogOut, Mail } from "lucide-react";
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
  const [open, setOpen] = useState(false);
  const [inputAdminKey, setInputAdminKey] = useState("");
  const [inputApiKey, setInputApiKey] = useState("");
  const [inputTenantId, setInputTenantId] = useState(tenantId || "");
  const [mailboxAddressInput, setMailboxAddressInput] = useState(mailboxAddress || "");
  const [mailboxPassword, setMailboxPassword] = useState("");
  const [mailboxLoading, setMailboxLoading] = useState(false);

  const handleAdminLogin = () => {
    if (!inputAdminKey.trim()) return;
    clearMailboxAuth();
    setAdminKey(inputAdminKey.trim());
    setApiKey(null);
    setTenantId(inputTenantId.trim() || null);
    setInputAdminKey("");
    setOpen(false);
    toast.success("Admin key configured");
  };

  const handleTenantLogin = () => {
    if (!inputApiKey.trim()) return;
    clearMailboxAuth();
    setAdminKey(null);
    setApiKey(inputApiKey.trim());
    setInputApiKey("");
    setOpen(false);
    toast.success("API key configured");
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
      toast.success("Mailbox token issued");
    } catch (e: unknown) {
      const err = e as { error?: { message?: string } };
      toast.error(err?.error?.message || "Failed to authenticate mailbox");
    } finally {
      setMailboxLoading(false);
    }
  };

  if (level !== "public") {
    return (
      <div className="flex items-center gap-2">
        <span className="text-xs font-medium px-2 py-1 rounded-md bg-primary/10 text-primary">
          {level === "admin" ? "Admin" : level === "tenant" ? "Tenant" : "Mailbox"}
        </span>
        <code className="text-xs text-muted-foreground max-w-[120px] truncate">
          {(adminKey || apiKey || mailboxAddress || "").slice(0, 18)}
        </code>
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
        Connect
      </DialogTrigger>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Authenticate</DialogTitle>
          <DialogDescription>
            Connect with a mailbox token flow, tenant API key, or admin key.
          </DialogDescription>
        </DialogHeader>
        <Tabs defaultValue="mailbox" className="mt-2">
          <TabsList className="grid w-full grid-cols-3">
            <TabsTrigger value="mailbox" className="gap-1.5">
              <Mail className="h-3.5 w-3.5" />
              Mailbox
            </TabsTrigger>
            <TabsTrigger value="apikey" className="gap-1.5">
              <KeyRound className="h-3.5 w-3.5" />
              API Key
            </TabsTrigger>
            <TabsTrigger value="admin" className="gap-1.5">
              <Shield className="h-3.5 w-3.5" />
              Admin
            </TabsTrigger>
          </TabsList>
          <TabsContent value="mailbox" className="space-y-4 pt-4">
            <div className="space-y-2">
              <Label htmlFor="mailbox-address">Mailbox Address</Label>
              <Input
                id="mailbox-address"
                placeholder="secure@mail.example.com"
                value={mailboxAddressInput}
                onChange={(e) => setMailboxAddressInput(e.target.value)}
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="mailbox-password">Mailbox Password</Label>
              <Input
                id="mailbox-password"
                type="password"
                placeholder="Enter mailbox password"
                value={mailboxPassword}
                onChange={(e) => setMailboxPassword(e.target.value)}
                onKeyDown={(e) => e.key === "Enter" && handleMailboxLogin()}
              />
            </div>
            <DialogFooter>
              <Button
                onClick={handleMailboxLogin}
                disabled={mailboxLoading || !mailboxAddressInput.trim() || !mailboxPassword.trim()}
              >
                {mailboxLoading ? "Connecting..." : "Connect"}
              </Button>
            </DialogFooter>
          </TabsContent>
          <TabsContent value="apikey" className="space-y-4 pt-4">
            <div className="space-y-2">
              <Label htmlFor="api-key">Tenant API Key</Label>
              <Input
                id="api-key"
                type="password"
                placeholder="tbm_..."
                value={inputApiKey}
                onChange={(e) => setInputApiKey(e.target.value)}
                onKeyDown={(e) => e.key === "Enter" && handleTenantLogin()}
              />
            </div>
            <DialogFooter>
              <Button onClick={handleTenantLogin} disabled={!inputApiKey.trim()}>
                Connect
              </Button>
            </DialogFooter>
          </TabsContent>
          <TabsContent value="admin" className="space-y-4 pt-4">
            <div className="space-y-2">
              <Label htmlFor="admin-key">Admin Key</Label>
              <Input
                id="admin-key"
                type="password"
                placeholder="Enter admin key"
                value={inputAdminKey}
                onChange={(e) => setInputAdminKey(e.target.value)}
                onKeyDown={(e) => e.key === "Enter" && handleAdminLogin()}
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="tenant-id">Tenant ID for Console/Admin proxy</Label>
              <Input
                id="tenant-id"
                placeholder="optional tenant uuid"
                value={inputTenantId}
                onChange={(e) => setInputTenantId(e.target.value)}
              />
            </div>
            <DialogFooter>
              <Button onClick={handleAdminLogin} disabled={!inputAdminKey.trim()}>
                Connect
              </Button>
            </DialogFooter>
          </TabsContent>
        </Tabs>
      </DialogContent>
    </Dialog>
  );
}
