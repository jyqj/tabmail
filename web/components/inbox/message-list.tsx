"use client";

import { formatDistanceToNow } from "date-fns";
import type { Message } from "@/lib/types";
import { cn } from "@/lib/utils";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Badge } from "@/components/ui/badge";
import { Mail, MailOpen } from "lucide-react";

interface Props {
  messages: Message[];
  selectedId: string | null;
  onSelect: (msg: Message) => void;
}

export function MessageList({ messages, selectedId, onSelect }: Props) {
  if (messages.length === 0) {
    return (
      <div className="flex flex-col items-center justify-center h-full py-20 text-center text-muted-foreground">
        <Mail className="h-10 w-10 mb-3 opacity-30" />
        <p className="text-sm font-medium">No messages yet</p>
        <p className="text-xs mt-1">Emails sent to this address will appear here</p>
      </div>
    );
  }

  return (
    <ScrollArea className="h-full">
      <div className="divide-y">
        {messages.map((msg) => (
          <button
            key={msg.id}
            onClick={() => onSelect(msg)}
            className={cn(
              "w-full text-left px-4 py-3 hover:bg-muted/50 transition-colors cursor-pointer",
              selectedId === msg.id && "bg-muted",
              !msg.seen && "bg-primary/[0.03]"
            )}
          >
            <div className="flex items-start justify-between gap-2">
              <div className="flex items-center gap-2 min-w-0">
                {msg.seen ? (
                  <MailOpen className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
                ) : (
                  <Mail className="h-3.5 w-3.5 shrink-0 text-primary" />
                )}
                <span
                  className={cn(
                    "text-sm truncate",
                    !msg.seen && "font-semibold"
                  )}
                >
                  {msg.sender || "(unknown)"}
                </span>
              </div>
              <span className="text-[11px] text-muted-foreground whitespace-nowrap shrink-0">
                {formatDistanceToNow(new Date(msg.received_at), { addSuffix: true })}
              </span>
            </div>
            <p
              className={cn(
                "text-sm mt-0.5 truncate pl-5.5",
                !msg.seen ? "text-foreground" : "text-muted-foreground"
              )}
            >
              {msg.subject || "(no subject)"}
            </p>
            <div className="flex items-center gap-2 mt-1 pl-5.5">
              {!msg.seen && (
                <Badge variant="default" className="h-4 text-[10px] px-1.5">
                  New
                </Badge>
              )}
              <span className="text-[11px] text-muted-foreground">
                {(msg.size / 1024).toFixed(1)} KB
              </span>
            </div>
          </button>
        ))}
      </div>
    </ScrollArea>
  );
}
