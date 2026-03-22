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
import {
  RefreshCw,
  Copy,
  Trash2,
  Mail,
  CheckCheck,
  KeyRound,
} from "lucide-react";
import { toast } from "sonner";
import { cn } from "@/lib/utils";

export default function InboxPage() {
  const params = useParams();
  const address = decodeURIComponent(params.address as string);
  const { mailboxAddress, mailboxToken, setMailboxAuth, clearMailboxAuth } = useAuth();

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
  const intervalRef = useRef<ReturnType<typeof setInterval>>(null);
  const mailboxTokenMatches =
    !!mailboxToken && mailboxAddress?.toLowerCase() === address.toLowerCase();

  const fetchMessages = useCallback(
    async (silent = false) => {
      if (!silent) setLoading(true);
      else setRefreshing(true);
      try {
        const res = await listMessages(address);
        setMessages(res.data);
        setTotal(res.meta.total);
        setAuthRequired(false);
      } catch {
        if (!silent) setAuthRequired(true);
        if (!silent) toast.error("Failed to load messages");
      } finally {
        setLoading(false);
        setRefreshing(false);
      }
    },
    [address]
  );

  useEffect(() => {
    fetchMessages();
    intervalRef.current = setInterval(() => fetchMessages(true), 10_000);
    return () => {
      if (intervalRef.current) clearInterval(intervalRef.current);
    };
  }, [fetchMessages]);

  useEffect(() => {
    const controller = new AbortController();
    streamMailboxEvents(address, {
      signal: controller.signal,
      onEvent: (event) => {
        if (event.type === "ready" || event.type === "ping") return;
        fetchMessages(true);
      },
    }).catch(() => {
      // SSE is best-effort; polling remains the fallback.
    });
    return () => controller.abort();
  }, [address, fetchMessages]);

  const handleMailboxLogin = async () => {
    if (!mailboxPassword.trim()) return;
    setAuthenticating(true);
    try {
      const res = await issueToken(address, mailboxPassword);
      setMailboxAuth(address, res.data.token);
      setMailboxPassword("");
      setAuthRequired(false);
      toast.success("Mailbox token issued");
      await fetchMessages();
    } catch (e: unknown) {
      const err = e as { error?: { message?: string } };
      toast.error(err?.error?.message || "Failed to authenticate mailbox");
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
      toast.error("Failed to load message");
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
      setTotal((t) => t - 1);
      toast.success("Message deleted");
    } catch {
      toast.error("Failed to delete");
    }
  };

  const handlePurge = async () => {
    try {
      await purgeMailbox(address);
      setMessages([]);
      setTotal(0);
      setSelectedMsg(null);
      setSelectedId(null);
      toast.success("All messages deleted");
    } catch {
      toast.error("Failed to purge");
    }
  };

  const copyAddress = () => {
    navigator.clipboard.writeText(address);
    setCopied(true);
    toast.success("Address copied");
    setTimeout(() => setCopied(false), 2000);
  };

  const showDetail = !!selectedMsg && !!selectedId;

  return (
    <div className="flex min-h-screen flex-col">
      <SiteHeader />

      {/* Inbox header */}
      <div className="shrink-0 border-b bg-background">
        <div className="container mx-auto max-w-6xl flex items-center justify-between px-4 py-3">
          <div className="flex items-center gap-3 min-w-0">
            <Mail className="h-5 w-5 text-primary shrink-0" />
            <div className="min-w-0">
              <div className="flex items-center gap-2">
                <code className="text-sm font-semibold truncate">{address}</code>
                <button
                  onClick={copyAddress}
                  className="shrink-0 text-muted-foreground hover:text-foreground transition-colors"
                >
                  {copied ? (
                    <CheckCheck className="h-3.5 w-3.5 text-green-500" />
                  ) : (
                    <Copy className="h-3.5 w-3.5" />
                  )}
                </button>
              </div>
              <p className="text-xs text-muted-foreground">
                {total} message{total !== 1 ? "s" : ""} &middot; Auto-refreshing every 10s
              </p>
            </div>
          </div>
          <div className="flex items-center gap-2">
            <Button
              variant="outline"
              size="sm"
              onClick={() => fetchMessages(true)}
              disabled={refreshing}
              className="gap-1.5"
            >
              <RefreshCw
                className={cn("h-3.5 w-3.5", refreshing && "animate-spin")}
              />
              <span className="hidden sm:inline">Refresh</span>
            </Button>
            {messages.length > 0 && (
              <Button
                variant="outline"
                size="sm"
                onClick={handlePurge}
                className="gap-1.5 text-destructive hover:text-destructive"
              >
                <Trash2 className="h-3.5 w-3.5" />
                <span className="hidden sm:inline">Purge</span>
              </Button>
            )}
            {mailboxTokenMatches && (
              <Button
                variant="ghost"
                size="sm"
                onClick={() => {
                  clearMailboxAuth();
                  setAuthRequired(true);
                }}
                className="gap-1.5"
              >
                <KeyRound className="h-3.5 w-3.5" />
                <span className="hidden sm:inline">Logout Mailbox</span>
              </Button>
            )}
          </div>
        </div>
      </div>

      {/* Split pane */}
      <div className="flex-1 container mx-auto max-w-6xl">
        {authRequired && (
          <div className="mx-4 mt-4 rounded-xl border bg-muted/30 p-4">
            <div className="flex flex-col gap-3 md:flex-row md:items-end md:justify-between">
              <div>
                <h2 className="text-sm font-semibold">Mailbox authentication may be required</h2>
                <p className="mt-1 text-sm text-muted-foreground">
                  If this inbox is configured as <code>token</code>, enter the mailbox password to fetch a mailbox token.
                </p>
              </div>
              <div className="flex w-full max-w-md flex-col gap-2 sm:flex-row">
                <Input
                  type="password"
                  placeholder="Mailbox password"
                  value={mailboxPassword}
                  onChange={(e) => setMailboxPassword(e.target.value)}
                  onKeyDown={(e) => e.key === "Enter" && handleMailboxLogin()}
                />
                <Button onClick={handleMailboxLogin} disabled={authenticating || !mailboxPassword.trim()}>
                  {authenticating ? "Connecting..." : "Unlock"}
                </Button>
              </div>
            </div>
          </div>
        )}
        {loading ? (
          <div className="flex items-center justify-center h-64 text-muted-foreground">
            <RefreshCw className="h-5 w-5 animate-spin mr-2" />
            Loading...
          </div>
        ) : (
          <div className="flex h-[calc(100vh-8rem)] min-h-0">
            {/* List pane */}
            <div
              className={cn(
                "w-full md:w-[360px] md:shrink-0 md:border-r",
                showDetail && "hidden md:block"
              )}
            >
              <MessageList
                messages={messages}
                selectedId={selectedId}
                onSelect={handleSelect}
              />
            </div>

            {/* Detail pane */}
            <div
              className={cn(
                "flex-1 min-w-0",
                !showDetail && "hidden md:flex md:items-center md:justify-center"
              )}
            >
              {showDetail ? (
                <MessageDetail
                  message={selectedMsg}
                  rawSource={rawSource}
                  onDelete={handleDelete}
                  onBack={() => {
                    setSelectedMsg(null);
                    setSelectedId(null);
                  }}
                  loading={detailLoading}
                />
              ) : (
                <div className="text-center text-muted-foreground">
                  <Mail className="h-10 w-10 mx-auto mb-3 opacity-30" />
                  <p className="text-sm">Select a message to read</p>
                </div>
              )}
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
