"use client";

import { useState } from "react";
import { useCRUDPage } from "@/hooks/use-crud-page";
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
import { Skeleton } from "@/components/ui/skeleton";
import {
  listWebhookEndpoints,
  createWebhookEndpoint,
  updateWebhookEndpoint,
  deleteWebhookEndpoint,
} from "@/lib/api";
import type { WebhookEndpoint } from "@/lib/api";
import { Plus, Trash2, Webhook, Power, PowerOff } from "lucide-react";
import { toast } from "sonner";
import { formatDistanceToNow } from "date-fns";
import { useI18n } from "@/lib/i18n";
import { safeConfirm } from "@/lib/utils";

export default function WebhookEndpointsPage() {
  const { t } = useI18n();
  const { data: response, isLoading: loading, mutate } = useCRUDPage(
    "webhookEndpoints",
    () => listWebhookEndpoints(),
    "webhookEp.loadFailed",
  );
  const endpoints: WebhookEndpoint[] = response?.data ?? [];

  const [dialogOpen, setDialogOpen] = useState(false);
  const [creating, setCreating] = useState(false);
  const [newUrl, setNewUrl] = useState("");
  const [newSecret, setNewSecret] = useState("");
  const [newEvents, setNewEvents] = useState("");

  const handleCreate = async () => {
    if (!newUrl.trim()) return;
    setCreating(true);
    try {
      const eventTypes = newEvents.trim()
        ? newEvents.split(",").map((s) => s.trim()).filter(Boolean)
        : [];
      await createWebhookEndpoint({
        url: newUrl.trim(),
        secret: newSecret.trim() || undefined,
        event_types: eventTypes,
      });
      setNewUrl("");
      setNewSecret("");
      setNewEvents("");
      setDialogOpen(false);
      toast.success(t("webhookEp.created"));
      mutate();
    } catch (e: unknown) {
      const err = e as { error?: { message?: string } };
      toast.error(err?.error?.message || t("webhookEp.createFailed"));
    } finally {
      setCreating(false);
    }
  };

  const handleToggle = async (ep: WebhookEndpoint) => {
    try {
      await updateWebhookEndpoint(ep.id, { is_active: !ep.is_active });
      toast.success(ep.is_active ? t("webhookEp.disabled") : t("webhookEp.enabled"));
      mutate();
    } catch {
      toast.error(t("webhookEp.updateFailed"));
    }
  };

  const handleDelete = async (id: string) => {
    if (!safeConfirm(t("webhookEp.confirmDelete"))) return;
    try {
      await deleteWebhookEndpoint(id);
      toast.success(t("webhookEp.deleted"));
      mutate();
    } catch {
      toast.error(t("webhookEp.deleteFailed"));
    }
  };

  return (
    <div className="flex flex-col">
      <PageHeader
        title={t("webhookEp.title")}
        description={t("webhookEp.description")}
        actions={
          <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
            <DialogTrigger render={<Button size="sm" className="gap-1.5" />}>
              <Plus className="h-3.5 w-3.5" />
              {t("webhookEp.add")}
            </DialogTrigger>
            <DialogContent className="sm:max-w-lg">
              <DialogHeader>
                <DialogTitle>{t("webhookEp.addTitle")}</DialogTitle>
                <DialogDescription>{t("webhookEp.addDesc")}</DialogDescription>
              </DialogHeader>
              <div className="space-y-4 py-4">
                <div className="space-y-2">
                  <Label>URL</Label>
                  <Input
                    placeholder="https://example.com/webhook"
                    value={newUrl}
                    onChange={(e) => setNewUrl(e.target.value)}
                  />
                </div>
                <div className="space-y-2">
                  <Label>{t("webhookEp.secret")}</Label>
                  <Input
                    type="password"
                    placeholder={t("webhookEp.secretPlaceholder")}
                    value={newSecret}
                    onChange={(e) => setNewSecret(e.target.value)}
                  />
                </div>
                <div className="space-y-2">
                  <Label>{t("webhookEp.eventTypes")}</Label>
                  <Input
                    placeholder="message.received, message.deleted"
                    value={newEvents}
                    onChange={(e) => setNewEvents(e.target.value)}
                  />
                  <p className="text-xs text-muted-foreground">{t("webhookEp.eventTypesHint")}</p>
                </div>
              </div>
              <DialogFooter>
                <Button onClick={handleCreate} disabled={creating || !newUrl.trim()}>
                  {creating ? t("webhookEp.creating") : t("webhookEp.create")}
                </Button>
              </DialogFooter>
            </DialogContent>
          </Dialog>
        }
      />

      <div className="p-4">
        <Card>
          <CardHeader className="pb-3">
            <CardTitle className="text-base">{t("webhookEp.endpoints")}</CardTitle>
            <CardDescription>{t("webhookEp.endpointsDesc")}</CardDescription>
          </CardHeader>
          <CardContent>
            {loading ? (
              <div className="space-y-3">
                {Array.from({ length: 3 }).map((_, i) => (
                  <Skeleton key={i} className="h-12 w-full" />
                ))}
              </div>
            ) : endpoints.length === 0 ? (
              <div className="text-center py-12 text-muted-foreground">
                <Webhook className="h-10 w-10 mx-auto mb-3 opacity-30" />
                <p className="text-sm">{t("webhookEp.noEndpoints")}</p>
              </div>
            ) : (
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>{t("webhookEp.status")}</TableHead>
                    <TableHead>URL</TableHead>
                    <TableHead>{t("webhookEp.events")}</TableHead>
                    <TableHead>{t("webhookEp.createdAt")}</TableHead>
                    <TableHead className="w-20" />
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {endpoints.map((ep) => (
                    <TableRow key={ep.id}>
                      <TableCell>
                        <Badge variant={ep.is_active ? "default" : "secondary"} className="text-xs">
                          {ep.is_active ? t("webhookEp.active") : t("webhookEp.inactive")}
                        </Badge>
                      </TableCell>
                      <TableCell>
                        <code className="text-xs break-all">{ep.url}</code>
                      </TableCell>
                      <TableCell>
                        {ep.event_types.length > 0 ? (
                          <div className="flex flex-wrap gap-1">
                            {ep.event_types.map((e) => (
                              <Badge key={e} variant="outline" className="text-xs">{e}</Badge>
                            ))}
                          </div>
                        ) : (
                          <span className="text-xs text-muted-foreground">{t("webhookEp.allEvents")}</span>
                        )}
                      </TableCell>
                      <TableCell className="text-sm text-muted-foreground">
                        {formatDistanceToNow(new Date(ep.created_at), { addSuffix: true })}
                      </TableCell>
                      <TableCell>
                        <div className="flex items-center gap-1">
                          <Button
                            variant="ghost"
                            size="icon"
                            className="h-8 w-8"
                            onClick={() => handleToggle(ep)}
                          >
                            {ep.is_active ? <PowerOff className="h-4 w-4" /> : <Power className="h-4 w-4" />}
                          </Button>
                          <Button
                            variant="ghost"
                            size="icon"
                            className="h-8 w-8 text-destructive hover:text-destructive hover:bg-destructive/10"
                            onClick={() => handleDelete(ep.id)}
                          >
                            <Trash2 className="h-4 w-4" />
                          </Button>
                        </div>
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            )}
          </CardContent>
        </Card>
      </div>
    </div>
  );
}
