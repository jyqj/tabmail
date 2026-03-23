"use client";

import { formatDistanceToNow, format } from "date-fns";
import { zhCN, enUS } from "date-fns/locale";
import type { Message } from "@/lib/types";
import { cn } from "@/lib/utils";
import { useI18n } from "@/lib/i18n";
import { useSettings } from "@/lib/settings";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Avatar, AvatarFallback } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import { Inbox } from "lucide-react";

interface Props {
  messages: Message[];
  selectedId: string | null;
  onSelect: (msg: Message) => void;
}

function senderInitials(sender: string): string {
  if (!sender || sender === "(unknown)") return "?";
  const name = sender.split("@")[0].split(/[._-]/);
  if (name.length >= 2) return (name[0][0] + name[1][0]).toUpperCase();
  return sender.slice(0, 2).toUpperCase();
}

function senderColor(sender: string): string {
  if (!sender) return "bg-muted";
  const hash = sender.split("").reduce((acc, c) => acc + c.charCodeAt(0), 0);
  const palette = [
    "bg-blue-500/15 text-blue-700 dark:text-blue-400",
    "bg-emerald-500/15 text-emerald-700 dark:text-emerald-400",
    "bg-violet-500/15 text-violet-700 dark:text-violet-400",
    "bg-amber-500/15 text-amber-700 dark:text-amber-400",
    "bg-rose-500/15 text-rose-700 dark:text-rose-400",
    "bg-cyan-500/15 text-cyan-700 dark:text-cyan-400",
    "bg-pink-500/15 text-pink-700 dark:text-pink-400",
  ];
  return palette[hash % palette.length];
}

export function MessageList({ messages, selectedId, onSelect }: Props) {
  const { locale, t } = useI18n();
  const { settings } = useSettings();
  const dateFnsLocale = locale === "zh" ? zhCN : enUS;

  const formatTime = (dateStr: string) => {
    const d = new Date(dateStr);
    if (settings.timeFormat === "absolute") {
      return format(d, locale === "zh" ? "MM/dd HH:mm" : "MMM d, HH:mm", { locale: dateFnsLocale });
    }
    return formatDistanceToNow(d, { addSuffix: true, locale: dateFnsLocale });
  };

  if (messages.length === 0) {
    return (
      <div className="flex flex-col items-center justify-center h-full py-20 px-6 text-center">
        <div className="flex h-16 w-16 items-center justify-center rounded-2xl bg-muted mb-5">
          <Inbox className="h-7 w-7 text-muted-foreground/50" />
        </div>
        <p className="font-medium text-sm">{t("msgList.empty")}</p>
        <p className="text-xs text-muted-foreground mt-1 max-w-[220px] leading-relaxed">
          {t("msgList.emptyDesc")}
        </p>
      </div>
    );
  }

  return (
    <ScrollArea className="h-full">
      <div className="p-2 space-y-0.5">
        {messages.map((msg) => {
          const isSelected = selectedId === msg.id;
          const isUnread = !msg.seen;

          return (
            <button
              key={msg.id}
              onClick={() => onSelect(msg)}
              className={cn(
                "w-full text-left rounded-lg px-3 py-3 transition-colors cursor-pointer",
                "hover:bg-muted/60",
                isSelected && "bg-muted",
                isUnread && !isSelected && "bg-primary/[0.04]"
              )}
            >
              <div className="flex items-start gap-3">
                <Avatar size="sm" className="mt-0.5 shrink-0">
                  <AvatarFallback className={cn("text-[10px] font-semibold", senderColor(msg.sender))}>
                    {senderInitials(msg.sender)}
                  </AvatarFallback>
                </Avatar>

                <div className="flex-1 min-w-0">
                  <div className="flex items-center justify-between gap-2">
                    <span className={cn(
                      "text-sm truncate",
                      isUnread ? "font-semibold text-foreground" : "text-muted-foreground"
                    )}>
                      {msg.sender || t("msgList.unknown")}
                    </span>
                    <span className="text-[11px] text-muted-foreground whitespace-nowrap shrink-0">
                      {formatTime(msg.received_at)}
                    </span>
                  </div>

                  <p className={cn(
                    "text-[13px] mt-0.5 truncate",
                    isUnread ? "text-foreground" : "text-muted-foreground"
                  )}>
                    {msg.subject || t("msgList.noSubject")}
                  </p>

                  <div className="flex items-center gap-2 mt-1.5">
                    {isUnread && (
                      <Badge variant="default" className="h-[18px] text-[10px] px-1.5 rounded-md">
                        {t("msgList.new")}
                      </Badge>
                    )}
                    <span className="text-[11px] text-muted-foreground">
                      {(msg.size / 1024).toFixed(1)} KB
                    </span>
                  </div>
                </div>

                {isUnread && (
                  <span className="h-2 w-2 rounded-full bg-primary shrink-0 mt-2" />
                )}
              </div>
            </button>
          );
        })}
      </div>
    </ScrollArea>
  );
}
