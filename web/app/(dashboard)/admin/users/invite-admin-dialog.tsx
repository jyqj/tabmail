"use client";

import { useState } from "react";
import { Copy, Plus } from "lucide-react";
import { toast } from "sonner";

import { inviteAdmin } from "@/lib/api";
import { useI18n } from "@/lib/i18n";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";

export function InviteAdminDialog() {
  const { t } = useI18n();
  const [open, setOpen] = useState(false);
  const [inviting, setInviting] = useState(false);
  const [email, setEmail] = useState("");
  const [result, setResult] = useState<{ invite_code: string; email: string } | null>(null);

  const handleInvite = async () => {
    if (!email.trim()) return;
    setInviting(true);
    try {
      const res = await inviteAdmin(email.trim());
      setResult({ invite_code: res.data.invite_code, email: res.data.email });
      setEmail("");
      toast.success(t("admin.inviteSent"));
    } catch (e: unknown) {
      const err = e as { error?: { message?: string } };
      toast.error(err?.error?.message || t("admin.inviteFailed"));
    } finally {
      setInviting(false);
    }
  };

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        setOpen(next);
        if (!next) {
          setResult(null);
          setEmail("");
        }
      }}
    >
      <DialogTrigger render={<Button size="sm" className="gap-1.5" />}>
        <Plus className="h-3.5 w-3.5" />
        {t("admin.inviteAdmin")}
      </DialogTrigger>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{t("admin.inviteTitle")}</DialogTitle>
          <DialogDescription>
            {t("admin.inviteDesc")}
          </DialogDescription>
        </DialogHeader>

        {result ? (
          <div className="space-y-4 py-4">
            <div className="rounded-lg border border-green-200 bg-green-50 dark:border-green-800 dark:bg-green-950 p-3">
              <p className="text-sm font-medium text-green-800 dark:text-green-200 mb-1">
                {t("admin.inviteCreated")}
              </p>
              <p className="text-xs text-green-700 dark:text-green-300 mb-2">
                {result.email}
              </p>
              <div className="flex items-center gap-2">
                <code className="flex-1 text-xs break-all bg-white dark:bg-black/20 p-2 rounded">
                  {result.invite_code}
                </code>
                <Button
                  variant="outline"
                  size="icon"
                  className="h-8 w-8 shrink-0"
                  onClick={() => {
                    navigator.clipboard.writeText(result.invite_code);
                    toast.success(t("admin.copied"));
                  }}
                >
                  <Copy className="h-3.5 w-3.5" />
                </Button>
              </div>
            </div>
            <DialogFooter>
              <Button variant="outline" onClick={() => setOpen(false)}>
                {t("admin.close")}
              </Button>
            </DialogFooter>
          </div>
        ) : (
          <div className="space-y-4 py-4">
            <div className="space-y-2">
              <Label>{t("admin.email")}</Label>
              <Input
                type="email"
                placeholder={t("admin.emailPlaceholder")}
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                onKeyDown={(e) => e.key === "Enter" && handleInvite()}
              />
            </div>
            <DialogFooter>
              <Button onClick={handleInvite} disabled={inviting || !email.trim()}>
                {inviting ? t("admin.inviting") : t("admin.sendInvite")}
              </Button>
            </DialogFooter>
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}
