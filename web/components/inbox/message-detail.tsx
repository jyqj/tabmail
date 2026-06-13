"use client";

import { useState } from "react";
import { formatDistanceToNow, format } from "date-fns";
import { zhCN, enUS } from "date-fns/locale";
import type { MessageDetail as MessageDetailType } from "@/lib/types";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Separator } from "@/components/ui/separator";
import { useI18n } from "@/lib/i18n";
import { useSettings } from "@/lib/settings";
import {
  Trash2,
  FileText,
  Code2,
  Mail,
  Clock,
  ArrowLeft,
  ShieldAlert,
} from "lucide-react";

interface Props {
  message: MessageDetailType;
  rawSource: string | null;
  rawSourceError?: string | null;
  onDelete?: () => void;
  onBack?: () => void;
  loading?: boolean;
  onBreakGlass?: (reason: string) => Promise<void>;
}

export function MessageDetail({
  message,
  rawSource,
  rawSourceError,
  onDelete,
  onBack,
  loading,
  onBreakGlass,
}: Props) {
  const { settings } = useSettings();
  const [activeTab, setActiveTab] = useState(settings.defaultTab);
  const [breakGlassReason, setBreakGlassReason] = useState("");
  const [breakGlassLoading, setBreakGlassLoading] = useState(false);
  const [remoteImagesAllowedFor, setRemoteImagesAllowedFor] = useState<string | null>(null);
  const { locale, t } = useI18n();
  const dateFnsLocale = locale === "zh" ? zhCN : enUS;
  const allowRemoteImages = remoteImagesAllowedFor === message.id;

  const htmlCSP = `default-src 'none'; style-src 'unsafe-inline'; img-src data:${
    allowRemoteImages ? " https:" : ""
  };`;

  if (loading) {
    return (
      <div className="flex items-center justify-center h-full text-muted-foreground">
        <div className="animate-pulse text-sm">{t("msgDetail.loading")}</div>
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
                {t("msgDetail.back")}
              </Button>
            )}
            <h2 className="text-lg font-semibold leading-tight truncate">
              {message.subject || t("msgList.noSubject")}
            </h2>
            <div className="flex flex-wrap items-center gap-x-3 gap-y-1 mt-1.5 text-sm text-muted-foreground">
              <span className="flex items-center gap-1">
                <Mail className="h-3.5 w-3.5" />
                {message.sender}
              </span>
              <span className="flex items-center gap-1">
                <Clock className="h-3.5 w-3.5" />
                {format(new Date(message.received_at), "PPp", { locale: dateFnsLocale })}
                <span className="text-xs">
                  ({formatDistanceToNow(new Date(message.received_at), { addSuffix: true, locale: dateFnsLocale })})
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
          {onDelete && (
            <Button
              variant="ghost"
              size="icon"
              onClick={onDelete}
              className="shrink-0 text-destructive hover:text-destructive hover:bg-destructive/10"
            >
              <Trash2 className="h-4 w-4" />
            </Button>
          )}
        </div>
      </div>

      {/* Body */}
      {message.body_redacted ? (
        <div className="flex-1 flex items-center justify-center p-8">
          <div className="max-w-md text-center space-y-4">
            <div className="flex h-14 w-14 items-center justify-center rounded-2xl bg-amber-500/10 mx-auto">
              <ShieldAlert className="h-7 w-7 text-amber-500" />
            </div>
            <h3 className="text-sm font-semibold">{t("msgDetail.bodyRedacted")}</h3>
            <p className="text-sm text-muted-foreground">{t("msgDetail.bodyRedactedDesc")}</p>
            {onBreakGlass && (
              <div className="space-y-3 pt-2">
                <Input
                  placeholder={t("msgDetail.breakGlassReason")}
                  value={breakGlassReason}
                  onChange={(e) => setBreakGlassReason(e.target.value)}
                  onKeyDown={(e) => {
                    if (e.key === "Enter" && breakGlassReason.trim()) {
                      setBreakGlassLoading(true);
                      onBreakGlass(breakGlassReason).finally(() => setBreakGlassLoading(false));
                    }
                  }}
                />
                <Button
                  onClick={() => {
                    if (!breakGlassReason.trim()) return;
                    setBreakGlassLoading(true);
                    onBreakGlass(breakGlassReason).finally(() => setBreakGlassLoading(false));
                  }}
                  disabled={!breakGlassReason.trim() || breakGlassLoading}
                  variant="outline"
                  className="gap-1.5"
                >
                  <ShieldAlert className="h-3.5 w-3.5" />
                  {t("msgDetail.breakGlassConfirm")}
                </Button>
                <p className="text-xs text-muted-foreground">{t("msgDetail.breakGlassAudit")}</p>
              </div>
            )}
          </div>
        </div>
      ) : (
      <Tabs
        value={activeTab}
        onValueChange={setActiveTab}
        className="flex-1 flex flex-col min-h-0"
      >
        <div className="shrink-0 px-4 pt-2">
          <TabsList className="h-8">
            <TabsTrigger value="html" className="text-xs gap-1 px-3">
              <FileText className="h-3 w-3" />
              {t("msgDetail.html")}
            </TabsTrigger>
            <TabsTrigger value="text" className="text-xs gap-1 px-3">
              <FileText className="h-3 w-3" />
              {t("msgDetail.text")}
            </TabsTrigger>
            <TabsTrigger value="source" className="text-xs gap-1 px-3">
              <Code2 className="h-3 w-3" />
              {t("msgDetail.source")}
            </TabsTrigger>
          </TabsList>
        </div>
        <Separator className="mt-2" />
        <div className="flex-1 min-h-0">
          <TabsContent value="html" className="h-full m-0">
            {message.html_body ? (
              <div className="flex h-full flex-col">
                {!allowRemoteImages && (
                  <div className="flex shrink-0 flex-col gap-2 border-b bg-amber-500/5 px-4 py-2 text-xs text-muted-foreground sm:flex-row sm:items-center sm:justify-between">
                    <span>{t("msgDetail.remoteImagesBlocked")}</span>
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={() => setRemoteImagesAllowedFor(message.id)}
                      className="h-7 self-start text-xs sm:self-auto"
                    >
                      {t("msgDetail.loadRemoteImages")}
                    </Button>
                  </div>
                )}
                <iframe
                  srcDoc={`<html><head><meta http-equiv="Content-Security-Policy" content="${htmlCSP}"></head><body>${message.html_body}</body></html>`}
                  className="w-full flex-1 border-0"
                  sandbox=""
                  title="Email HTML content"
                />
              </div>
            ) : (
              <div className="p-4 text-sm text-muted-foreground">
                {t("msgDetail.noHtml")}
              </div>
            )}
          </TabsContent>
          <TabsContent value="text" className="h-full m-0">
            <ScrollArea className="h-full">
              <pre className="p-4 text-sm whitespace-pre-wrap font-mono leading-relaxed">
                {message.text_body || t("msgDetail.noText")}
              </pre>
            </ScrollArea>
          </TabsContent>
          <TabsContent value="source" className="h-full m-0">
            <ScrollArea className="h-full">
              {rawSource ? (
                <pre className="p-4 text-xs whitespace-pre-wrap font-mono leading-relaxed text-muted-foreground">
                  {rawSource}
                </pre>
              ) : rawSourceError ? (
                <div className="p-4 text-sm text-destructive">
                  <p className="font-medium">{t("msgDetail.sourceLoadFailed")}</p>
                  <p className="mt-1 text-xs text-muted-foreground">{rawSourceError}</p>
                </div>
              ) : (
                <div className="p-4 text-sm text-muted-foreground">
                  {t("msgDetail.loadingSource")}
                </div>
              )}
            </ScrollArea>
          </TabsContent>
        </div>
      </Tabs>
      )}
    </div>
  );
}
