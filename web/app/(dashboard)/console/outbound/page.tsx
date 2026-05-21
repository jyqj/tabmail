"use client";

import { useEffect, useState } from "react";
import { formatDistanceToNow } from "date-fns";
import { ChevronDown, ChevronRight, RefreshCw, Send } from "lucide-react";
import { toast } from "sonner";

import { listOutboundJobs, sendEmail } from "@/lib/api";
import type { OutboundJob } from "@/lib/types";
import { useI18n } from "@/lib/i18n";
import { useAPI } from "@/hooks/use-api";
import { PageHeader } from "@/components/layout/page-header";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";

const PAGE_SIZE = 30;

const stateStyles: Record<string, string> = {
  pending: "border-slate-500/20 bg-slate-500/10 text-slate-700",
  processing: "border-sky-500/20 bg-sky-500/10 text-sky-700",
  sent: "border-emerald-500/20 bg-emerald-500/10 text-emerald-700",
  retry: "border-amber-500/20 bg-amber-500/10 text-amber-700",
  failed: "border-red-500/20 bg-red-500/10 text-red-700",
  dead: "border-rose-700/20 bg-rose-700/10 text-rose-800",
};

export default function OutboundPage() {
  const { t } = useI18n();
  const [page, setPage] = useState(1);
  const [dialogOpen, setDialogOpen] = useState(false);
  const [sending, setSending] = useState(false);
  const [detailJob, setDetailJob] = useState<OutboundJob | null>(null);
  const [showHtml, setShowHtml] = useState(false);

  // Form state
  const [formFrom, setFormFrom] = useState("");
  const [formTo, setFormTo] = useState("");
  const [formSubject, setFormSubject] = useState("");
  const [formTextBody, setFormTextBody] = useState("");
  const [formHtmlBody, setFormHtmlBody] = useState("");

  const {
    data: response,
    isLoading: loading,
    error,
    mutate,
  } = useAPI(["outbound", page], () =>
    listOutboundJobs({ page, per_page: PAGE_SIZE }),
  );
  const jobs = response?.data ?? [];
  const total = response?.meta?.total ?? 0;
  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE));

  useEffect(() => {
    if (error) toast.error(t("outbound.loadFailed"));
  }, [error, t]);

  const resetForm = () => {
    setFormFrom("");
    setFormTo("");
    setFormSubject("");
    setFormTextBody("");
    setFormHtmlBody("");
    setShowHtml(false);
  };

  const handleSend = async () => {
    if (!formFrom.trim() || !formTo.trim() || !formSubject.trim()) return;

    const recipients = formTo
      .split(/[\n,]+/)
      .map((addr) => addr.trim())
      .filter(Boolean);

    if (recipients.length === 0) {
      toast.error(t("outbound.noRecipients"));
      return;
    }

    setSending(true);
    try {
      await sendEmail({
        from: formFrom.trim(),
        to: recipients,
        subject: formSubject.trim(),
        text_body: formTextBody.trim() || undefined,
        html_body: formHtmlBody.trim() || undefined,
      });
      toast.success(t("outbound.sendSuccess"));
      setDialogOpen(false);
      resetForm();
      mutate();
    } catch (err: unknown) {
      const apiErr = err as { error?: { message?: string } };
      toast.error(apiErr?.error?.message || t("outbound.sendFailed"));
    } finally {
      setSending(false);
    }
  };

  return (
    <div className="flex flex-col">
      <PageHeader
        title={t("outbound.title")}
        description={t("outbound.count", { count: total })}
        actions={
          <div className="flex items-center gap-2">
            <Button
              variant="outline"
              size="sm"
              className="gap-1.5"
              onClick={() => mutate()}
            >
              <RefreshCw className="h-3.5 w-3.5" />
              {t("outbound.refresh")}
            </Button>
            <Dialog
              open={dialogOpen}
              onOpenChange={(open) => {
                setDialogOpen(open);
                if (!open) resetForm();
              }}
            >
              <DialogTrigger render={<Button size="sm" className="gap-1.5" />}>
                <Send className="h-3.5 w-3.5" />
                {t("outbound.sendEmail")}
              </DialogTrigger>
              <DialogContent className="sm:max-w-lg">
                <DialogHeader>
                  <DialogTitle>{t("outbound.sendTitle")}</DialogTitle>
                  <DialogDescription>
                    {t("outbound.sendDesc")}
                  </DialogDescription>
                </DialogHeader>
                <div className="space-y-4 py-4">
                  <div className="space-y-2">
                    <Label>{t("outbound.from")}</Label>
                    <Input
                      placeholder="noreply@your-domain.com"
                      value={formFrom}
                      onChange={(e) => setFormFrom(e.target.value)}
                    />
                  </div>
                  <div className="space-y-2">
                    <Label>{t("outbound.subject")}</Label>
                    <Input
                      placeholder={t("outbound.subjectPlaceholder")}
                      value={formSubject}
                      onChange={(e) => setFormSubject(e.target.value)}
                    />
                  </div>
                  <div className="space-y-2">
                    <Label>{t("outbound.to")}</Label>
                    <Textarea
                      placeholder={t("outbound.toPlaceholder")}
                      value={formTo}
                      onChange={(e) => setFormTo(e.target.value)}
                      className="min-h-[60px]"
                    />
                  </div>
                  <div className="space-y-2">
                    <Label>{t("outbound.textBody")}</Label>
                    <Textarea
                      placeholder={t("outbound.textBodyPlaceholder")}
                      value={formTextBody}
                      onChange={(e) => setFormTextBody(e.target.value)}
                      className="min-h-[120px]"
                    />
                  </div>
                  <div>
                    <button
                      type="button"
                      className="flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground transition-colors"
                      onClick={() => setShowHtml(!showHtml)}
                    >
                      {showHtml ? (
                        <ChevronDown className="h-3.5 w-3.5" />
                      ) : (
                        <ChevronRight className="h-3.5 w-3.5" />
                      )}
                      {t("outbound.htmlBody")}
                      <span className="text-xs">
                        ({t("outbound.optional")})
                      </span>
                    </button>
                    {showHtml && (
                      <Textarea
                        placeholder={t("outbound.htmlBodyPlaceholder")}
                        value={formHtmlBody}
                        onChange={(e) => setFormHtmlBody(e.target.value)}
                        className="mt-2 min-h-[100px] font-mono text-xs"
                      />
                    )}
                  </div>
                </div>
                <DialogFooter>
                  <Button
                    onClick={handleSend}
                    disabled={
                      sending ||
                      !formFrom.trim() ||
                      !formTo.trim() ||
                      !formSubject.trim()
                    }
                  >
                    {sending ? t("outbound.sending") : t("outbound.send")}
                  </Button>
                </DialogFooter>
              </DialogContent>
            </Dialog>
          </div>
        }
      />

      <div className="p-4 space-y-4">
        <Card className="tm-reveal tm-reveal-1">
          <CardHeader className="pb-3">
            <CardTitle className="text-base">
              {t("outbound.listTitle")}
            </CardTitle>
            <CardDescription>{t("outbound.listDesc")}</CardDescription>
          </CardHeader>
          <CardContent>
            {loading ? (
              <div className="space-y-3">
                {Array.from({ length: 8 }).map((_, i) => (
                  <Skeleton key={i} className="h-10 w-full" />
                ))}
              </div>
            ) : jobs.length === 0 ? (
              <div className="text-center py-12 text-muted-foreground">
                <Send className="h-10 w-10 mx-auto mb-3 opacity-30" />
                <p className="text-sm">{t("outbound.noJobs")}</p>
                <p className="text-xs mt-1">{t("outbound.noJobsHint")}</p>
              </div>
            ) : (
              <>
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>{t("outbound.state")}</TableHead>
                      <TableHead>{t("outbound.mailFrom")}</TableHead>
                      <TableHead>{t("outbound.rcptTo")}</TableHead>
                      <TableHead>{t("outbound.subjectCol")}</TableHead>
                      <TableHead>{t("outbound.attempts")}</TableHead>
                      <TableHead>{t("outbound.messageId")}</TableHead>
                      <TableHead>{t("outbound.createdAt")}</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {jobs.map((job) => (
                      <TableRow
                        key={job.id}
                        className="cursor-pointer hover:bg-muted/50"
                        onClick={() => setDetailJob(job)}
                      >
                        <TableCell>
                          <Badge
                            variant="outline"
                            className={stateStyles[job.state] || ""}
                          >
                            {job.state}
                          </Badge>
                        </TableCell>
                        <TableCell className="font-mono text-xs max-w-[180px] truncate">
                          {job.mail_from || "—"}
                        </TableCell>
                        <TableCell className="text-sm max-w-[200px]">
                          <span className="truncate block">
                            {job.rcpt_to[0] || "—"}
                          </span>
                          {job.rcpt_to.length > 1 && (
                            <Badge
                              variant="secondary"
                              className="ml-1 text-xs"
                            >
                              +{job.rcpt_to.length - 1}
                            </Badge>
                          )}
                        </TableCell>
                        <TableCell className="max-w-[200px] truncate text-sm">
                          {job.subject || "—"}
                        </TableCell>
                        <TableCell className="text-sm">
                          {job.attempts}/{job.max_attempts}
                        </TableCell>
                        <TableCell className="font-mono text-xs max-w-[160px] truncate text-muted-foreground">
                          {job.message_id_header || "—"}
                        </TableCell>
                        <TableCell className="text-sm text-muted-foreground">
                          {formatDistanceToNow(new Date(job.created_at), {
                            addSuffix: true,
                          })}
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>

                {/* Error details */}
                <div className="mt-4 space-y-2">
                  {jobs
                    .filter((job) => job.last_error)
                    .slice(0, 5)
                    .map((job) => (
                      <div
                        key={`${job.id}-err`}
                        className="rounded-lg border border-amber-500/20 bg-amber-500/5 p-3 text-xs"
                      >
                        <div className="font-medium text-foreground">
                          {job.id}
                        </div>
                        <div className="mt-1 break-all text-muted-foreground">
                          {job.last_error}
                        </div>
                      </div>
                    ))}
                </div>

                {/* Pagination */}
                {totalPages > 1 && (
                  <div className="flex justify-center gap-2 mt-4">
                    <Button
                      variant="outline"
                      size="sm"
                      disabled={page <= 1}
                      onClick={() => setPage((p) => p - 1)}
                    >
                      {t("outbound.previous")}
                    </Button>
                    <span className="flex items-center text-sm text-muted-foreground px-2">
                      {t("outbound.pageOf", { page, total: totalPages })}
                    </span>
                    <Button
                      variant="outline"
                      size="sm"
                      disabled={page >= totalPages}
                      onClick={() => setPage((p) => p + 1)}
                    >
                      {t("outbound.next")}
                    </Button>
                  </div>
                )}
              </>
            )}
          </CardContent>
        </Card>

        {/* Detail dialog */}
        <Dialog
          open={detailJob !== null}
          onOpenChange={(open) => {
            if (!open) setDetailJob(null);
          }}
        >
          <DialogContent className="sm:max-w-lg">
            <DialogHeader>
              <DialogTitle>{t("outbound.detailTitle")}</DialogTitle>
              <DialogDescription>{detailJob?.id}</DialogDescription>
            </DialogHeader>
            {detailJob && (
              <div className="space-y-3 py-2 text-sm">
                <DetailRow
                  label={t("outbound.state")}
                  value={
                    <Badge
                      variant="outline"
                      className={stateStyles[detailJob.state] || ""}
                    >
                      {detailJob.state}
                    </Badge>
                  }
                />
                <DetailRow
                  label={t("outbound.mailFrom")}
                  value={
                    <span className="font-mono text-xs">
                      {detailJob.mail_from}
                    </span>
                  }
                />
                <DetailRow
                  label={t("outbound.rcptTo")}
                  value={
                    <div className="space-y-0.5">
                      {detailJob.rcpt_to.map((addr) => (
                        <div key={addr} className="font-mono text-xs">
                          {addr}
                        </div>
                      ))}
                    </div>
                  }
                />
                <DetailRow
                  label={t("outbound.subjectCol")}
                  value={detailJob.subject}
                />
                <DetailRow
                  label={t("outbound.attempts")}
                  value={`${detailJob.attempts}/${detailJob.max_attempts}`}
                />
                {detailJob.message_id_header && (
                  <DetailRow
                    label={t("outbound.messageId")}
                    value={
                      <span className="font-mono text-xs break-all">
                        {detailJob.message_id_header}
                      </span>
                    }
                  />
                )}
                {detailJob.smtp_code != null && (
                  <DetailRow
                    label={t("outbound.smtpCode")}
                    value={detailJob.smtp_code}
                  />
                )}
                {detailJob.smtp_response && (
                  <DetailRow
                    label={t("outbound.smtpResponse")}
                    value={
                      <span className="break-all">{detailJob.smtp_response}</span>
                    }
                  />
                )}
                {detailJob.last_error && (
                  <DetailRow
                    label={t("outbound.error")}
                    value={
                      <span className="text-destructive break-all">
                        {detailJob.last_error}
                      </span>
                    }
                  />
                )}
                <DetailRow
                  label={t("outbound.createdAt")}
                  value={formatDistanceToNow(new Date(detailJob.created_at), {
                    addSuffix: true,
                  })}
                />
              </div>
            )}
            <DialogFooter>
              <Button
                variant="outline"
                onClick={() => setDetailJob(null)}
              >
                {t("outbound.close")}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      </div>
    </div>
  );
}

function DetailRow({
  label,
  value,
}: {
  label: string;
  value: React.ReactNode;
}) {
  return (
    <div className="flex gap-3">
      <div className="w-24 shrink-0 text-muted-foreground">{label}</div>
      <div className="min-w-0 flex-1">{value}</div>
    </div>
  );
}
