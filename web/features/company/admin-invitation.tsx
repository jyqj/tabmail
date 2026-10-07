"use client";

import { useLayoutEffect, useRef, useState } from "react";
import { Copy, Plus } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle, DialogTrigger } from "@/components/ui/dialog";
import { inviteAdmin } from "@/lib/api";
import { useI18n } from "@/lib/i18n";
import { sessionScope } from "@/lib/session";

type Draft = { email: string };
type InvitationRequest = { dialog: object; draft: Draft };
type InvitationResult = { invite_code: string; email: string };

// The parent keys this component by session and unmounts it when authority is
// revoked. Requests also check the live scope before React receives that event.
export function AdminInvitation({ scope }: { scope: string }) {
  const { t } = useI18n();
  const [open, setOpen] = useState(false);
  const [email, setEmail] = useState("");
  const [inviting, setInviting] = useState(false);
  const [result, setResult] = useState<InvitationResult | null>(null);
  const mounted = useRef(false);
  const dialogOwner = useRef<object | null>(null);
  const draft = useRef<Draft>({ email: "" });
  const activeRequest = useRef<InvitationRequest | null>(null);

  useLayoutEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
      dialogOwner.current = null;
      activeRequest.current = null;
    };
  }, []);

  const currentSession = () => mounted.current && scope === sessionScope();
  const changeOpen = (nextOpen: boolean) => {
    if (!mounted.current || (nextOpen && !currentSession())) return;
    // Closing abandons UI ownership, not the server-side invitation: the POST
    // may already have committed. Reopening requires a new explicit submit.
    dialogOwner.current = nextOpen ? {} : null;
    activeRequest.current = null;
    draft.current = { email: "" };
    setOpen(nextOpen); setEmail(""); setResult(null); setInviting(false);
  };

  const handleInvite = async () => {
    const dialog = dialogOwner.current;
    const submittedDraft = draft.current;
    const submittedEmail = submittedDraft.email.trim();
    if (!currentSession() || !dialog || activeRequest.current || !submittedEmail) return;
    const request = { dialog, draft: submittedDraft };
    // A synchronous owner prevents rapid Enter/click events from issuing
    // duplicate non-idempotent writes before the busy state has rendered.
    activeRequest.current = request;
    setInviting(true);
    const ownsRequest = () => currentSession() && dialogOwner.current === request.dialog && activeRequest.current === request;
    const ownsDraft = () => ownsRequest() && draft.current === request.draft;
    try {
      const response = await inviteAdmin(submittedEmail);
      if (!ownsDraft()) return;
      setResult({ invite_code: response.data.invite_code, email: response.data.email });
      draft.current = { email: "" }; setEmail("");
      toast.success(t("admin.inviteSent"));
    } catch (error: unknown) {
      if (!ownsDraft()) return;
      const failure = error as { error?: { message?: string } };
      toast.error(failure?.error?.message || t("admin.inviteFailed"));
    } finally {
      if (ownsRequest()) {
        activeRequest.current = null;
        setInviting(false);
      }
    }
  };

  return <Dialog open={open} onOpenChange={changeOpen}>
    <DialogTrigger render={<Button size="sm" className="gap-1.5" />}>
      <Plus className="h-3.5 w-3.5" />{t("admin.inviteAdmin")}
    </DialogTrigger>
    <DialogContent className="sm:max-w-md">
      <DialogHeader>
        <DialogTitle>{t("admin.inviteTitle")}</DialogTitle>
        <DialogDescription>{t("admin.inviteDesc")}</DialogDescription>
      </DialogHeader>
      {result ? <div className="space-y-4 py-4">
        <div className="rounded-lg border border-green-200 bg-green-50 dark:border-green-800 dark:bg-green-950 p-3">
          <p className="text-sm font-medium text-green-800 dark:text-green-200 mb-1">{t("admin.inviteCreated")}</p>
          <p className="text-xs text-green-700 dark:text-green-300 mb-2">{result.email}</p>
          <div className="flex items-center gap-2">
            <code className="flex-1 text-xs break-all bg-white dark:bg-black/20 p-2 rounded">{result.invite_code}</code>
            <Button variant="outline" size="icon" className="h-8 w-8 shrink-0" onClick={() => {
              if (!currentSession() || !dialogOwner.current) return;
              navigator.clipboard.writeText(result.invite_code);
              toast.success(t("admin.copied"));
            }}><Copy className="h-3.5 w-3.5" /></Button>
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => changeOpen(false)}>{t("admin.close")}</Button>
        </DialogFooter>
      </div> : <div className="space-y-4 py-4">
        <div className="space-y-2">
          <Label>{t("admin.email")}</Label>
          <Input type="email" placeholder={t("admin.emailPlaceholder")} value={email} onChange={event => {
            if (!currentSession() || !dialogOwner.current) return;
            // Each edit owns new intent even if A -> B -> A restores the text.
            draft.current = { email: event.target.value };
            setEmail(event.target.value);
          }} onKeyDown={event => {
            if (event.key === "Enter") { event.preventDefault(); void handleInvite(); }
          }} />
        </div>
        <DialogFooter>
          <Button onClick={handleInvite} disabled={inviting || !email.trim()}>
            {inviting ? t("admin.inviting") : t("admin.sendInvite")}
          </Button>
        </DialogFooter>
      </div>}
    </DialogContent>
  </Dialog>;
}
