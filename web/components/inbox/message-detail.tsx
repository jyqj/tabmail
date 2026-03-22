"use client";

import { useState } from "react";
import { formatDistanceToNow, format } from "date-fns";
import type { MessageDetail as MessageDetailType } from "@/lib/types";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Separator } from "@/components/ui/separator";
import {
  Trash2,
  FileText,
  Code2,
  Mail,
  Clock,
  ArrowLeft,
} from "lucide-react";

interface Props {
  message: MessageDetailType;
  rawSource: string | null;
  onDelete: () => void;
  onBack?: () => void;
  loading?: boolean;
}

export function MessageDetail({
  message,
  rawSource,
  onDelete,
  onBack,
  loading,
}: Props) {
  const [activeTab, setActiveTab] = useState("html");

  if (loading) {
    return (
      <div className="flex items-center justify-center h-full text-muted-foreground">
        <div className="animate-pulse text-sm">Loading message...</div>
      </div>
    );
  }

  return (
    <div className="flex flex-col h-full">
      {/* Header */}
      <div className="shrink-0 border-b p-4">
        <div className="flex items-start justify-between gap-4">
          <div className="min-w-0 flex-1">
            {onBack && (
              <Button
                variant="ghost"
                size="sm"
                onClick={onBack}
                className="mb-2 -ml-2 gap-1 text-muted-foreground md:hidden"
              >
                <ArrowLeft className="h-3.5 w-3.5" />
                Back
              </Button>
            )}
            <h2 className="text-lg font-semibold leading-tight truncate">
              {message.subject || "(no subject)"}
            </h2>
            <div className="flex flex-wrap items-center gap-x-3 gap-y-1 mt-1.5 text-sm text-muted-foreground">
              <span className="flex items-center gap-1">
                <Mail className="h-3.5 w-3.5" />
                {message.sender}
              </span>
              <span className="flex items-center gap-1">
                <Clock className="h-3.5 w-3.5" />
                {format(new Date(message.received_at), "PPp")}
                <span className="text-xs">
                  ({formatDistanceToNow(new Date(message.received_at), { addSuffix: true })})
                </span>
              </span>
            </div>
            {message.recipients.length > 0 && (
              <div className="flex flex-wrap gap-1 mt-2">
                {message.recipients.map((r) => (
                  <Badge key={r} variant="secondary" className="text-xs font-normal">
                    {r}
                  </Badge>
                ))}
              </div>
            )}
          </div>
          <Button
            variant="ghost"
            size="icon"
            onClick={onDelete}
            className="shrink-0 text-destructive hover:text-destructive hover:bg-destructive/10"
          >
            <Trash2 className="h-4 w-4" />
          </Button>
        </div>
      </div>

      {/* Body */}
      <Tabs
        value={activeTab}
        onValueChange={setActiveTab}
        className="flex-1 flex flex-col min-h-0"
      >
        <div className="shrink-0 px-4 pt-2">
          <TabsList className="h-8">
            <TabsTrigger value="html" className="text-xs gap-1 px-3">
              <FileText className="h-3 w-3" />
              HTML
            </TabsTrigger>
            <TabsTrigger value="text" className="text-xs gap-1 px-3">
              <FileText className="h-3 w-3" />
              Text
            </TabsTrigger>
            <TabsTrigger value="source" className="text-xs gap-1 px-3">
              <Code2 className="h-3 w-3" />
              Source
            </TabsTrigger>
          </TabsList>
        </div>
        <Separator className="mt-2" />
        <div className="flex-1 min-h-0">
          <TabsContent value="html" className="h-full m-0">
            {message.html_body ? (
              <iframe
                srcDoc={message.html_body}
                className="w-full h-full border-0"
                sandbox="allow-same-origin"
                title="Email HTML content"
              />
            ) : (
              <div className="p-4 text-sm text-muted-foreground">
                No HTML content
              </div>
            )}
          </TabsContent>
          <TabsContent value="text" className="h-full m-0">
            <ScrollArea className="h-full">
              <pre className="p-4 text-sm whitespace-pre-wrap font-mono leading-relaxed">
                {message.text_body || "No text content"}
              </pre>
            </ScrollArea>
          </TabsContent>
          <TabsContent value="source" className="h-full m-0">
            <ScrollArea className="h-full">
              <pre className="p-4 text-xs whitespace-pre-wrap font-mono leading-relaxed text-muted-foreground">
                {rawSource || "Loading source..."}
              </pre>
            </ScrollArea>
          </TabsContent>
        </div>
      </Tabs>
    </div>
  );
}
