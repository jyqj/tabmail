"use client";

import { useState, useEffect, useCallback, useRef } from "react";
import { useParams } from "next/navigation";
import { SiteHeader } from "@/components/site-header";
import { MessageList } from "@/components/inbox/message-list";
import { MessageDetail } from "@/components/inbox/message-detail";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  listMessages,
  getMessage,
  markMessageSeen,
  deleteMessage,
  purgeMailbox,
  getMessageSource,
  issueToken,
  streamMailboxEvents,
} from "@/lib/api";
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
  Loader2,
} from "lucide-react";
import { toast } from "sonner";
import { cn } from "@/lib/utils";

export default function InboxPage() {
  const params = useParams();
  const address = decodeURIComponent(params.address as string);
  const { mailboxAddress, mailboxToken, setMailboxAuth, clearMailboxAuth } = useAuth();
  const { t } = useI18n();
  const { settings } = useSettings();

  const [messages, setMessages] = useState<Message[]>([]);
  const [total, setTotal] = useState(0);
  const [selectedMsg, setSelectedMsg] = useState<MsgDetail | null>(null);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [rawSource, setRawSource] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const [detailLoading, setDetailLoading] = useState(false);
  const [refreshing, setRefreshing] = useState(false);
  const [copied, setCopied] = useState(false);
  const [mailboxPassword, setMailboxPassword] = useState("");
  const [authenticating, setAuthenticating] = useState(false);
  const [authRequired, setAuthRequired] = useState(false);
  const [notFound, setNotFound] = useState(false);
  const [sseConnected, setSseConnected] = useState(false);
  const intervalRef = useRef<ReturnType<typeof setInterval>>(null);
  const mailboxTokenMatches =
    !!mailboxToken && mailboxAddress?.toLowerCase() === address.toLowerCase();

  const fetchMessages = useCallback(
    async (silent = false) => {
      if (!silent) setLoading(true);
      else setRefreshing(true);
      try {
        const res = await listMessages(address);
        setMessages(res.data ?? []);
        setTotal(res.meta?.total ?? res.data?.length ?? 0);
        setAuthRequired(false);
        setNotFound(false);
      } catch (e: unknown) {
        const err = e as { error?: { code?: string; message?: string } };
        const code = err?.error?.code ?? "";
        const msg = err?.error?.message ?? "";
        const isNotFound = code === "NOT_FOUND" || msg.includes("not found");
        const isAuthError = code === "UNAUTHORIZED" || code === "FORBIDDEN" || msg.includes("unauthorized");
        if (!silent) {
          if (isNotFound) {
            setNotFound(true);
            setAuthRequired(false);
          } else if (isAuthError) {
            setAuthRequired(true);
            setNotFound(false);
          } else {
            setNotFound(false);
            setAuthRequired(false);
            toast.error(t("toast.loadFailed"));
          }
        }
      } finally {
        setLoading(false);
        setRefreshing(false);
      }
    },
    [address, t]
  );

  useEffect(() => {
    fetchMessages();
    if (settings.autoRefresh) {
      intervalRef.current = setInterval(
        () => fetchMessages(true),
        settings.refreshInterval * 1000,
      );
    }
    return () => {
      if (intervalRef.current) clearInterval(intervalRef.current);
    };
  }, [fetchMessages, settings.autoRefresh, settings.refreshInterval]);

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
        fetchMessages(true);
      },
    }).catch(() => { setSseConnected(false); });
    return () => { controller.abort(); setSseConnected(false); };
  }, [address, fetchMessages, settings.preferSSE]);

  const handleMailboxLogin = async () => {
    if (!mailboxPassword.trim()) return;
    setAuthenticating(true);
    try {
      const res = await issueToken(address, mailboxPassword);
      setMailboxAuth(address, res.data.token);
      setMailboxPassword("");
      setAuthRequired(false);
      toast.success(t("toast.tokenIssued"));
      await fetchMessages();
    } catch (e: unknown) {
      const err = e as { error?: { message?: string } };
      toast.error(err?.error?.message || t("toast.authFailed"));
    } finally {
      setAuthenticating(false);
    }
  };

  const handleSelect = async (msg: Message) => {
    setSelectedId(msg.id);
    setDetailLoading(true);
    setRawSource(null);
    try {
      const res = await getMessage(address, msg.id);
      setSelectedMsg(res.data);
      if (!msg.seen) {
        await markMessageSeen(address, msg.id);
        setMessages((prev) =>
          prev.map((m) => (m.id === msg.id ? { ...m, seen: true } : m))
        );
      }
    } catch {
      toast.error(t("toast.loadFailed"));
    } finally {
      setDetailLoading(false);
    }
    getMessageSource(address, msg.id)
      .then((src) => setRawSource(src))
      .catch(() => {});
  };

  const handleDelete = async () => {
    if (!selectedId) return;
    try {
      await deleteMessage(address, selectedId);
      setMessages((prev) => prev.filter((m) => m.id !== selectedId));
      setSelectedMsg(null);
      setSelectedId(null);
      setTotal((prev) => prev - 1);
      toast.success(t("toast.deleted"));
    } catch {
      toast.error(t("toast.deleteFailed"));
    }
  };

  const handlePurge = async () => {
    try {
      await purgeMailbox(address);
      setMessages([]);
      setTotal(0);
      setSelectedMsg(null);
      setSelectedId(null);
      toast.success(t("toast.allDeleted"));
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
              onClick={() => fetchMessages(true)}
              disabled={refreshing}
              className="gap-1.5"
            >
              <RefreshCw className={cn("h-3.5 w-3.5", refreshing && "animate-spin")} />
              <span className="hidden sm:inline">{t("inbox.refresh")}</span>
            </Button>
            {messages.length > 0 && (
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
              <div className="flex w-full max-w-md flex-col gap-2 sm:flex-row">
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
            </div>
          </div>
        )}

        {loading ? (
          <div className="flex flex-col items-center justify-center h-64 text-muted-foreground">
            <Loader2 className="h-6 w-6 animate-spin mb-3" />
            <p className="text-sm">{t("inbox.loading")}</p>
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
                  onDelete={handleDelete}
                  onBack={() => { setSelectedMsg(null); setSelectedId(null); }}
                  loading={detailLoading}
                />
              ) : (
                <div className="text-center text-muted-foreground px-4">
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
