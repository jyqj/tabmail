"use client";

import { useState, useEffect, useCallback } from "react";
import { PageHeader } from "@/components/layout/page-header";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Badge } from "@/components/ui/badge";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
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
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Skeleton } from "@/components/ui/skeleton";
import { listMailboxes, createMailbox, deleteMailbox } from "@/lib/api";
import type { Mailbox, AccessMode } from "@/lib/types";
import {
  Plus,
  MoreHorizontal,
  Trash2,
  ExternalLink,
  Inbox,
  Copy,
} from "lucide-react";
import { toast } from "sonner";
import { formatDistanceToNow } from "date-fns";
import Link from "next/link";
import { useI18n } from "@/lib/i18n";

const accessColors: Record<AccessMode, string> = {
  public: "bg-green-100 text-green-800 dark:bg-green-900/30 dark:text-green-400",
  token: "bg-yellow-100 text-yellow-800 dark:bg-yellow-900/30 dark:text-yellow-400",
  api_key: "bg-blue-100 text-blue-800 dark:bg-blue-900/30 dark:text-blue-400",
};

export default function MailboxesPage() {
  const { t } = useI18n();
  const [mailboxes, setMailboxes] = useState<Mailbox[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [loading, setLoading] = useState(true);
  const [dialogOpen, setDialogOpen] = useState(false);
  const [creating, setCreating] = useState(false);

  const [newAddress, setNewAddress] = useState("");
  const [newAccessMode, setNewAccessMode] = useState<AccessMode>("public");
  const [newPassword, setNewPassword] = useState("");
  const [newRetentionHours, setNewRetentionHours] = useState("");
  const [newExpiresAt, setNewExpiresAt] = useState("");

  const fetchMailboxes = useCallback(async () => {
    try {
      const res = await listMailboxes(page);
      setMailboxes(res.data ?? []);
      setTotal(res.meta.total);
    } catch {
      toast.error(t("mailboxes.loadFailed"));
    } finally {
      setLoading(false);
    }
  }, [page, t]);

  useEffect(() => {
    fetchMailboxes();
  }, [fetchMailboxes]);

  const handleCreate = async () => {
    if (!newAddress.trim()) return;
    const retentionHours = Number(newRetentionHours);
    if (newRetentionHours.trim() && (Number.isNaN(retentionHours) || retentionHours <= 0)) {
      toast.error(t("mailboxes.retentionError"));
      return;
    }
    let expiresAtISO: string | undefined;
    if (newExpiresAt.trim()) {
      const parsed = new Date(newExpiresAt);
      if (Number.isNaN(parsed.getTime()) || parsed.getTime() <= Date.now()) {
        toast.error(t("mailboxes.expiresError"));
        return;
      }
      expiresAtISO = parsed.toISOString();
    }
    setCreating(true);
    try {
      await createMailbox({
        address: newAddress.trim(),
        access_mode: newAccessMode,
        password: newAccessMode === "token" ? newPassword : undefined,
        retention_hours_override: newRetentionHours.trim() ? retentionHours : undefined,
        expires_at: expiresAtISO,
      });
      setNewAddress("");
      setNewPassword("");
      setNewRetentionHours("");
      setNewExpiresAt("");
      setDialogOpen(false);
      toast.success(t("mailboxes.mailboxCreated"));
      fetchMailboxes();
    } catch (e: unknown) {
      const err = e as { error?: { message?: string } };
      toast.error(err?.error?.message || t("mailboxes.createFailed"));
    } finally {
      setCreating(false);
    }
  };

  const handleDelete = async (id: string) => {
    try {
      await deleteMailbox(id);
      toast.success(t("mailboxes.deleted"));
      fetchMailboxes();
    } catch {
      toast.error(t("mailboxes.deleteFailed"));
    }
  };

  const totalPages = Math.ceil(total / 30);

  return (
    <div className="flex flex-col">
      <PageHeader
        title={t("mailboxes.title")}
        description={t("mailboxes.count", { count: total })}
        actions={
          <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
            <DialogTrigger render={<Button size="sm" className="gap-1.5" />}>
              <Plus className="h-3.5 w-3.5" />
              {t("mailboxes.createMailbox")}
            </DialogTrigger>
            <DialogContent className="sm:max-w-md">
              <DialogHeader>
                <DialogTitle>{t("mailboxes.createTitle")}</DialogTitle>
                <DialogDescription>
                  {t("mailboxes.createDesc")}
                </DialogDescription>
              </DialogHeader>
              <div className="space-y-4 py-4">
                <div className="space-y-2">
                  <Label>{t("mailboxes.fullAddress")}</Label>
                  <Input
                    placeholder={t("domains.placeholder")}
                    value={newAddress}
                    onChange={(e) => setNewAddress(e.target.value)}
                    onKeyDown={(e) => e.key === "Enter" && handleCreate()}
                  />
                </div>
                <div className="space-y-2">
                  <Label>{t("mailboxes.accessMode")}</Label>
                  <Select
                    value={newAccessMode}
                    onValueChange={(v) => setNewAccessMode(v as AccessMode)}
                  >
                    <SelectTrigger>
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="public">{t("mailboxes.public")}</SelectItem>
                      <SelectItem value="token">{t("mailboxes.token")}</SelectItem>
                      <SelectItem value="api_key">{t("mailboxes.apiKeyOnly")}</SelectItem>
                    </SelectContent>
                  </Select>
                </div>
                {newAccessMode === "token" && (
                  <div className="space-y-2">
                    <Label>{t("mailboxes.password")}</Label>
                    <Input
                      type="password"
                      placeholder={t("mailboxes.passwordPlaceholder")}
                      value={newPassword}
                      onChange={(e) => setNewPassword(e.target.value)}
                    />
                  </div>
                )}
                <div className="space-y-2">
                  <Label>{t("mailboxes.retentionHoursOverride")}</Label>
                  <Input
                    type="number"
                    min="1"
                    placeholder={t("mailboxes.inheritDefault")}
                    value={newRetentionHours}
                    onChange={(e) => setNewRetentionHours(e.target.value)}
                  />
                </div>
                <div className="space-y-2">
                  <Label>{t("mailboxes.expiresAt")}</Label>
                  <Input
                    type="datetime-local"
                    placeholder={t("mailboxes.optional")}
                    value={newExpiresAt}
                    onChange={(e) => setNewExpiresAt(e.target.value)}
                  />
                </div>
              </div>
              <DialogFooter>
                <Button onClick={handleCreate} disabled={creating || !newAddress.trim()}>
                  {creating ? t("mailboxes.creating") : t("mailboxes.create")}
                </Button>
              </DialogFooter>
            </DialogContent>
          </Dialog>
        }
      />

      <div className="p-4">
        <Card>
          <CardHeader className="pb-3">
            <CardTitle className="text-base">{t("mailboxes.allMailboxes")}</CardTitle>
            <CardDescription>
              {t("mailboxes.allMailboxesDesc")}
            </CardDescription>
          </CardHeader>
          <CardContent>
            {loading ? (
              <div className="space-y-3">
                {Array.from({ length: 5 }).map((_, i) => (
                  <Skeleton key={i} className="h-12 w-full" />
                ))}
              </div>
            ) : mailboxes.length === 0 ? (
              <div className="text-center py-12 text-muted-foreground">
                <Inbox className="h-10 w-10 mx-auto mb-3 opacity-30" />
                <p className="text-sm">{t("mailboxes.noMailboxes")}</p>
                <p className="text-xs mt-1">
                  {t("mailboxes.noMailboxesHint")}
                </p>
              </div>
            ) : (
              <>
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>{t("mailboxes.address")}</TableHead>
                      <TableHead>{t("mailboxes.accessMode")}</TableHead>
                      <TableHead>{t("mailboxes.domain")}</TableHead>
                      <TableHead>{t("mailboxes.retention")}</TableHead>
                      <TableHead>{t("mailboxes.expiresAt")}</TableHead>
                      <TableHead>{t("mailboxes.created")}</TableHead>
                      <TableHead className="w-10" />
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {mailboxes.map((mb) => (
                      <TableRow key={mb.id}>
                        <TableCell>
                          <div className="flex items-center gap-2">
                            <code className="text-sm font-medium">
                              {mb.full_address}
                            </code>
                            <button
                              onClick={() => {
                                navigator.clipboard.writeText(mb.full_address);
                                toast.success(t("mailboxes.copied"));
                              }}
                              className="text-muted-foreground hover:text-foreground"
                            >
                              <Copy className="h-3 w-3" />
                            </button>
                          </div>
                        </TableCell>
                        <TableCell>
                          <Badge
                            variant="secondary"
                            className={accessColors[mb.access_mode]}
                          >
                            {mb.access_mode}
                          </Badge>
                        </TableCell>
                        <TableCell className="text-sm text-muted-foreground">
                          {mb.resolved_domain}
                        </TableCell>
                        <TableCell className="text-sm text-muted-foreground">
                          {mb.retention_hours_override != null
                            ? `${mb.retention_hours_override}h`
                            : t("mailboxes.inheritDefault")}
                        </TableCell>
                        <TableCell className="text-sm text-muted-foreground">
                          {mb.expires_at
                            ? formatDistanceToNow(new Date(mb.expires_at), { addSuffix: true })
                            : "—"}
                        </TableCell>
                        <TableCell className="text-sm text-muted-foreground">
                          {formatDistanceToNow(new Date(mb.created_at), {
                            addSuffix: true,
                          })}
                        </TableCell>
                        <TableCell>
                          <DropdownMenu>
                            <DropdownMenuTrigger render={<Button variant="ghost" size="icon" className="h-8 w-8" />}>
                              <MoreHorizontal className="h-4 w-4" />
                            </DropdownMenuTrigger>
                            <DropdownMenuContent align="end">
                              <DropdownMenuItem render={<Link href={`/inbox/${encodeURIComponent(mb.full_address)}`} />}>
                                <ExternalLink className="h-4 w-4 mr-2" />
                                {t("mailboxes.openInbox")}
                              </DropdownMenuItem>
                              <DropdownMenuItem
                                onClick={() => handleDelete(mb.id)}
                                className="text-destructive focus:text-destructive"
                              >
                                <Trash2 className="h-4 w-4 mr-2" />
                                {t("mailboxes.delete")}
                              </DropdownMenuItem>
                            </DropdownMenuContent>
                          </DropdownMenu>
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>

                {totalPages > 1 && (
                  <div className="flex justify-center gap-2 mt-4">
                    <Button
                      variant="outline"
                      size="sm"
                      disabled={page <= 1}
                      onClick={() => setPage((p) => p - 1)}
                    >
                      {t("mailboxes.previous")}
                    </Button>
                    <span className="flex items-center text-sm text-muted-foreground px-2">
                      {t("mailboxes.pageOf", { page, total: totalPages })}
                    </span>
                    <Button
                      variant="outline"
                      size="sm"
                      disabled={page >= totalPages}
                      onClick={() => setPage((p) => p + 1)}
                    >
                      {t("mailboxes.next")}
                    </Button>
                  </div>
                )}
              </>
            )}
          </CardContent>
        </Card>
      </div>
    </div>
  );
}
