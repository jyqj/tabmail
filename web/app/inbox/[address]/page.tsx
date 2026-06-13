"use client";

import { useState, useEffect, useCallback } from "react";
import { useParams } from "next/navigation";
import { useAPI } from "@/hooks/use-api";
import { SiteHeader } from "@/components/site-header";
import { MessageList } from "@/components/inbox/message-list";
import { MessageDetail } from "@/components/inbox/message-detail";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import {
  listMessages,
  getMessage,
  markMessageSeen,
  deleteMessage,
  purgeMailbox,
  getMessageSource,
  breakGlassRead,
  breakGlassSource,
  issueToken,
  streamMailboxEvents,
} from "@/lib/api";
import {
  clearMailboxAPIKeyAuth,
  getMailboxAPIKeySnapshot,
  setMailboxAPIKeyAuth,
} from "@/lib/api/base";
import type { Message, MessageDetail as MsgDetail } from "@/lib/types";
import { useAuth } from "@/contexts/auth-context";
import { useI18n } from "@/lib/i18n";
import { useSettings } from "@/lib/settings";
import {
  RefreshCw,
  Copy,
  Trash2,
  Mail,
  CheckCheck,
  KeyRound,
} from "lucide-react";
import { toast } from "sonner";
import { cn, safeConfirm } from "@/lib/utils";
import { isAdminLevel } from "@/lib/permissions";

export default function InboxPage() {
  const params = useParams();
  const address = decodeURIComponent(params.address as string);
  const { level, mailboxAddress, mailboxToken, setMailboxAuth, clearMailboxAuth } = useAuth();
  const { t } = useI18n();
  const { settings } = useSettings();

  const [selectedMsg, setSelectedMsg] = useState<MsgDetail | null>(null);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [rawSource, setRawSource] = useState<string | null>(null);
  const [rawSourceError, setRawSourceError] = useState<string | null>(null);
  const [detailLoading, setDetailLoading] = useState(false);
  const [copied, setCopied] = useState(false);
  const [mailboxPassword, setMailboxPassword] = useState("");
  const [mailboxAPIKey, setMailboxAPIKey] = useState("");
  const [mailboxAPIKeyMatches, setMailboxAPIKeyMatches] = useState(false);
  const [authenticating, setAuthenticating] = useState(false);
  const [authRequired, setAuthRequired] = useState(false);
  const [notFound, setNotFound] = useState(false);
  const [sseConnected, setSseConnected] = useState(false);
  const mailboxTokenMatches =
    !!mailboxToken && mailboxAddress?.toLowerCase() === address.toLowerCase();
  // UX-only gate; the backend authz seam is authoritative.
  const canWriteRecords = isAdminLevel(level);

  const { data: response, isLoading: loading, isValidating: refreshing, error, mutate } = useAPI(
    address ? ["inbox", address] : null,
    () => listMessages(address),
    {
      refreshInterval: settings.autoRefresh && !authRequired ? settings.refreshInterval * 1000 : 0,
    },
  );
  const messages = response?.data ?? [];
  const total = response?.meta?.total ?? response?.data?.length ?? 0;

  // Process SWR errors to determine auth/notFound state
  useEffect(() => {
    if (!error) {
      return;
    }
    const err = error as { error?: { code?: string; message?: string } };
    const code = err?.error?.code ?? "";
    const msg = err?.error?.message ?? (error instanceof Error ? error.message : "");
    const normalizedMsg = msg.toLowerCase();
    const isNotFound = code === "NOT_FOUND" || normalizedMsg.includes("not found");
    const isAuthError = code === "UNAUTHORIZED" || code === "FORBIDDEN" || normalizedMsg.includes("unauthorized") || normalizedMsg.includes("forbidden");
    if (isNotFound) {
      setNotFound(true);
      setAuthRequired(false);
    } else if (isAuthError) {
      setAuthRequired(true);
      setNotFound(false);
      toast.error(msg || t("toast.authFailed"));
    } else {
      setNotFound(false);
      setAuthRequired(false);
      toast.error(t("toast.loadFailed"));
    }
  }, [error, t]);

  // On successful data load, clear error states
  useEffect(() => {
    if (response) {
      setAuthRequired(false);
      setNotFound(false);
    }
  }, [response]);

  useEffect(() => {
    const updateStoredAPIKeyState = () => {
      const { address: keyAddress, key } = getMailboxAPIKeySnapshot();
      setMailboxAPIKeyMatches(
        !!key && keyAddress?.toLowerCase() === address.toLowerCase(),
      );
    };
    updateStoredAPIKeyState();
    window.addEventListener("storage", updateStoredAPIKeyState);
    window.addEventListener("tabmail-auth-change", updateStoredAPIKeyState);
    return () => {
      window.removeEventListener("storage", updateStoredAPIKeyState);
      window.removeEventListener("tabmail-auth-change", updateStoredAPIKeyState);
    };
  }, [address]);

  // SSE streaming
  const handleSseEvent = useCallback(() => {
    mutate();
  }, [mutate]);

  useEffect(() => {
    if (!settings.preferSSE) {
      setSseConnected(false);
      return;
    }
    const controller = new AbortController();
    setSseConnected(false);
    streamMailboxEvents(address, {
      signal: controller.signal,
      onEvent: (event) => {
        if (event.type === "ready") { setSseConnected(true); return; }
        if (event.type === "ping") return;
        handleSseEvent();
      },
    }).catch(() => { setSseConnected(false); });
    return () => { controller.abort(); setSseConnected(false); };
  }, [address, handleSseEvent, settings.preferSSE]);

  const handleMailboxLogin = async () => {
    if (!mailboxPassword.trim()) return;
    setAuthenticating(true);
    try {
      const res = await issueToken(address, mailboxPassword);
      setMailboxAuth(address, res.data.token);
      setMailboxPassword("");
      setAuthRequired(false);
      toast.success(t("toast.tokenIssued"));
      mutate();
    } catch (e: unknown) {
      const err = e as { error?: { message?: string } };
      toast.error(err?.error?.message || t("toast.authFailed"));
    } finally {
      setAuthenticating(false);
    }
  };

  const handleAPIKeyLogin = async () => {
    const key = mailboxAPIKey.trim();
    if (!key) return;
    setMailboxAPIKeyAuth(address, key);
    setMailboxAPIKey("");
    setAuthRequired(false);
    setNotFound(false);
    setMailboxAPIKeyMatches(true);
    toast.success(t("toast.apiKeyOk"));
    mutate();
  };

  const handleClearMailboxAPIKey = () => {
    clearMailboxAPIKeyAuth();
    setMailboxAPIKeyMatches(false);
    setAuthRequired(true);
    mutate();
  };

  const handleSelect = async (msg: Message) => {
    setSelectedId(msg.id);
    setDetailLoading(true);
    setRawSource(null);
    setRawSourceError(null);
    try {
      const res = await getMessage(address, msg.id);
      setSelectedMsg(res.data);
      if (canWriteRecords && !msg.seen) {
        try {
          await markMessageSeen(address, msg.id);
          // Optimistically update the cached messages list
          mutate(
            (current) => {
              if (!current) return current;
              return {
                ...current,
                data: current.data?.map((m: Message) =>
                  m.id === msg.id ? { ...m, seen: true } : m
                ),
              };
            },
            { revalidate: false },
          );
        } catch {
          // Read-only views can still open the message; leave seen state unchanged.
        }
      }
    } catch {
      toast.error(t("toast.loadFailed"));
    } finally {
      setDetailLoading(false);
    }
    getMessageSource(address, msg.id)
      .then((src) => setRawSource(src))
      .catch((e: unknown) => {
        const err = e as { error?: { message?: string } };
        setRawSourceError(err?.error?.message || t("msgDetail.sourceLoadFailed"));
      });
  };

  const handleDelete = async () => {
    if (!selectedId) return;
    if (!safeConfirm(t("inbox.confirmDeleteMessage"))) return;
    try {
      await deleteMessage(address, selectedId);
      setSelectedMsg(null);
      setSelectedId(null);
      toast.success(t("toast.deleted"));
      mutate();
    } catch {
      toast.error(t("toast.deleteFailed"));
    }
  };

  const handleBreakGlass = async (reason: string) => {
    if (!selectedId) return;
    try {
      const res = await breakGlassRead(address, selectedId, reason);
      setSelectedMsg(res.data);
      setRawSourceError(null);
      breakGlassSource(address, selectedId, reason)
        .then((src) => setRawSource(src))
        .catch((e: unknown) => {
          const err = e as { error?: { message?: string } };
          setRawSourceError(err?.error?.message || t("msgDetail.sourceLoadFailed"));
        });
      toast.success(t("msgDetail.breakGlass"));
    } catch {
      toast.error(t("toast.loadFailed"));
    }
  };

  const handlePurge = async () => {
    if (!safeConfirm(t("inbox.confirmPurge"))) return;
    try {
      await purgeMailbox(address);
      setSelectedMsg(null);
      setSelectedId(null);
      toast.success(t("toast.allDeleted"));
      mutate();
    } catch {
      toast.error(t("toast.purgeFailed"));
    }
  };

  const copyAddress = () => {
    navigator.clipboard.writeText(address);
    setCopied(true);
    toast.success(t("toast.copied"));
    setTimeout(() => setCopied(false), 2000);
  };

  const showDetail = !!selectedMsg && !!selectedId;
  const unseenCount = messages.filter((m) => !m.seen).length;

  return (
    <div className="flex min-h-screen flex-col bg-muted/30">
      <SiteHeader />

      {/* Toolbar */}
      <div className="shrink-0 border-b bg-background">
        <div className="container mx-auto max-w-6xl flex flex-col sm:flex-row sm:items-center sm:justify-between gap-3 px-4 py-3">
          <div className="flex items-center gap-3 min-w-0">
            <div className="flex h-10 w-10 items-center justify-center rounded-xl bg-primary/10 shrink-0">
              <Mail className="h-5 w-5 text-primary" />
            </div>
            <div className="min-w-0">
              <div className="flex items-center gap-2">
                <code className="text-sm font-semibold truncate max-w-[260px] sm:max-w-none">
                  {address}
                </code>
                <button
                  onClick={copyAddress}
                  className="shrink-0 text-muted-foreground hover:text-foreground transition-colors"
                >
                  {copied ? (
                    <CheckCheck className="h-3.5 w-3.5 text-emerald-500" />
                  ) : (
                    <Copy className="h-3.5 w-3.5" />
                  )}
                </button>
              </div>
              <div className="flex items-center gap-3 mt-0.5">
                <span className="text-xs text-muted-foreground">
                  {t("inbox.messages", { count: total })}
                  {unseenCount > 0 && (
                    <span className="text-primary font-medium ml-1">
                      {t("inbox.newCount", { count: unseenCount })}
                    </span>
                  )}
                </span>
                <div className="flex items-center gap-1.5">
                  <span className={cn(
                    "h-1.5 w-1.5 rounded-full transition-colors",
                    !settings.autoRefresh && !sseConnected
                      ? "bg-slate-400"
                      : sseConnected
                        ? "bg-emerald-500 shadow-[0_0_4px_rgba(16,185,129,0.6)]"
                        : "bg-amber-500"
                  )} />
                  <span className="text-[11px] text-muted-foreground">
                    {sseConnected
                      ? t("inbox.live")
                      : settings.autoRefresh
                        ? t("inbox.everyNSec", { sec: settings.refreshInterval })
                        : t("inbox.paused")}
                  </span>
                </div>
              </div>
            </div>
          </div>

          <div className="flex items-center gap-2 shrink-0">
            <Button
              variant="outline"
              size="sm"
              onClick={() => mutate()}
              disabled={refreshing}
              className="gap-1.5"
            >
              <RefreshCw className={cn("h-3.5 w-3.5", refreshing && "animate-spin")} />
              <span className="hidden sm:inline">{t("inbox.refresh")}</span>
            </Button>
            {canWriteRecords && messages.length > 0 && (
              <Button
                variant="outline"
                size="sm"
                onClick={handlePurge}
                className="gap-1.5 text-destructive hover:text-destructive"
              >
                <Trash2 className="h-3.5 w-3.5" />
                <span className="hidden sm:inline">{t("inbox.purge")}</span>
              </Button>
            )}
            {mailboxTokenMatches && (
              <Button
                variant="ghost"
                size="sm"
                onClick={() => { clearMailboxAuth(); setAuthRequired(true); }}
                className="gap-1.5"
              >
                <KeyRound className="h-3.5 w-3.5" />
                <span className="hidden sm:inline">{t("inbox.logout")}</span>
              </Button>
            )}
            {mailboxAPIKeyMatches && (
              <Button
                variant="ghost"
                size="sm"
                onClick={handleClearMailboxAPIKey}
                className="gap-1.5"
              >
                <KeyRound className="h-3.5 w-3.5" />
                <span className="hidden sm:inline">{t("inbox.forgetApiKey")}</span>
              </Button>
            )}
          </div>
        </div>
      </div>

      {/* Main */}
      <div className="flex-1 container mx-auto max-w-6xl">
        {notFound && (
          <div className="mx-4 mt-4 rounded-xl border border-amber-500/30 bg-amber-500/5 p-5">
            <div className="flex items-start gap-3">
              <Mail className="h-5 w-5 text-amber-500 shrink-0 mt-0.5" />
              <div>
                <h2 className="text-sm font-semibold">{t("inbox.notFoundTitle")}</h2>
                <p className="text-sm text-muted-foreground mt-1">{t("inbox.notFoundDesc")}</p>
              </div>
            </div>
          </div>
        )}

        {authRequired && !notFound && (
          <div className="mx-4 mt-4 rounded-xl border bg-background p-5">
            <div className="flex flex-col gap-4 md:flex-row md:items-end md:justify-between">
              <div>
                <div className="flex items-center gap-2 mb-1">
                  <KeyRound className="h-4 w-4 text-primary" />
                  <h2 className="text-sm font-semibold">{t("inbox.authTitle")}</h2>
                </div>
                <p className="text-sm text-muted-foreground">
                  {t("inbox.authDesc")}
                </p>
              </div>
              <div className="grid w-full max-w-md gap-3">
                <div className="flex flex-col gap-2 sm:flex-row">
                  <Input
                    type="password"
                    placeholder={t("inbox.password")}
                    value={mailboxPassword}
                    onChange={(e) => setMailboxPassword(e.target.value)}
                    onKeyDown={(e) => e.key === "Enter" && handleMailboxLogin()}
                  />
                  <Button onClick={handleMailboxLogin} disabled={authenticating || !mailboxPassword.trim()}>
                    {authenticating ? t("inbox.connecting") : t("inbox.unlock")}
                  </Button>
                </div>
                <div className="flex flex-col gap-2 sm:flex-row">
                  <Input
                    type="password"
                    placeholder={t("inbox.apiKey")}
                    value={mailboxAPIKey}
                    onChange={(e) => setMailboxAPIKey(e.target.value)}
                    onKeyDown={(e) => e.key === "Enter" && handleAPIKeyLogin()}
                  />
                  <Button
                    variant="outline"
                    onClick={handleAPIKeyLogin}
                    disabled={!mailboxAPIKey.trim()}
                  >
                    {t("inbox.useApiKey")}
                  </Button>
                </div>
                <p className="text-xs text-muted-foreground">{t("inbox.apiKeyDesc")}</p>
              </div>
            </div>
          </div>
        )}

        {loading ? (
          <div className="p-4 space-y-3">
            {Array.from({ length: 5 }).map((_, i) => (
              <div key={i} className="flex items-start gap-3 p-3 rounded-lg">
                <Skeleton className="h-10 w-10 rounded-full shrink-0" />
                <div className="flex-1 space-y-2">
                  <Skeleton className="h-4 w-1/3" />
                  <Skeleton className="h-3 w-2/3" />
                  <Skeleton className="h-3 w-1/2" />
                </div>
                <Skeleton className="h-3 w-16 shrink-0" />
              </div>
            ))}
          </div>
        ) : (
          <div className="flex h-[calc(100vh-8rem)] min-h-0">
            <div className={cn(
              "w-full md:w-[380px] md:shrink-0 md:border-r bg-background",
              showDetail && "hidden md:block"
            )}>
              <MessageList messages={messages} selectedId={selectedId} onSelect={handleSelect} />
            </div>

            <div className={cn(
              "flex-1 min-w-0 bg-background",
              !showDetail && "hidden md:flex md:items-center md:justify-center"
            )}>
              {showDetail ? (
                <MessageDetail
                  message={selectedMsg}
                  rawSource={rawSource}
                  rawSourceError={rawSourceError}
                  onDelete={canWriteRecords ? handleDelete : undefined}
                  onBack={() => { setSelectedMsg(null); setSelectedId(null); }}
                  loading={detailLoading}
                  onBreakGlass={selectedMsg?.body_redacted && canWriteRecords ? handleBreakGlass : undefined}
                />
              ) : (
                <div className="text-center text-muted-foreground px-4 tm-reveal tm-reveal-1">
                  <div className="flex h-16 w-16 items-center justify-center rounded-2xl bg-muted mx-auto mb-4">
                    <Mail className="h-7 w-7 opacity-40" />
                  </div>
                  <p className="text-sm font-medium">{t("inbox.selectMsg")}</p>
                  <p className="text-xs mt-1">{t("inbox.selectMsgDesc")}</p>
                </div>
              )}
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
