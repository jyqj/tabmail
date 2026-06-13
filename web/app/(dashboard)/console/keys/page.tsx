"use client";

import { useState } from "react";
import { useAPI } from "@/hooks/use-api";
import { useCRUDPage } from "@/hooks/use-crud-page";
import { PageHeader } from "@/components/layout/page-header";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Badge } from "@/components/ui/badge";
import { Switch } from "@/components/ui/switch";
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
import { APIKeyScopePicker } from "@/components/api-key-scope-picker";
import { createUserAPIKey, listDomains, listUserAPIKeys, revokeUserAPIKey } from "@/lib/api";
import { DEFAULT_API_KEY_SCOPES, API_KEY_TEMPLATES } from "@/lib/api-key-scopes";
import type { DomainZone } from "@/lib/types";
import { Plus, Key, Trash2, Copy } from "lucide-react";
import { toast } from "sonner";
import { formatDistanceToNow } from "date-fns";
import { useI18n } from "@/lib/i18n";
import { safeConfirm } from "@/lib/utils";

function zoneLabel(zone: DomainZone) {
  return zone.domain || `${zone.id.slice(0, 8)}…`;
}

export default function UserAPIKeysPage() {
  const { t } = useI18n();
  const { data: response, isLoading: loading, mutate } = useCRUDPage(
    "apiKeys",
    () => listUserAPIKeys(),
    "apiKeys.loadFailed",
  );
  const { data: domainsResponse } = useAPI("api-key-domains", () => listDomains());
  const keys = response?.data ?? [];
  const domainOptions = domainsResponse?.data ?? [];
  const domainById = new Map(domainOptions.map((zone) => [zone.id, zone]));

  const [dialogOpen, setDialogOpen] = useState(false);
  const [creating, setCreating] = useState(false);
  const [newLabel, setNewLabel] = useState("");
  const [newScopes, setNewScopes] = useState<string[]>([...DEFAULT_API_KEY_SCOPES]);
  const [newAllowedZoneIds, setNewAllowedZoneIds] = useState<string[]>([]);
  const [createdKey, setCreatedKey] = useState<string | null>(null);

  const handleCreate = async () => {
    setCreating(true);
    try {
      const res = await createUserAPIKey({
        label: newLabel.trim() || undefined,
        scopes: newScopes,
        allowed_zone_ids: newAllowedZoneIds,
      });
      setCreatedKey(res.data.key);
      setNewLabel("");
      setNewScopes([...DEFAULT_API_KEY_SCOPES]);
      setNewAllowedZoneIds([]);
      mutate();
    } catch (e: unknown) {
      const err = e as { error?: { message?: string } };
      toast.error(err?.error?.message || t("apiKeys.createFailed"));
      setCreatedKey(null);
    } finally {
      setCreating(false);
    }
  };

  const handleRevoke = async (keyId: string) => {
    if (!safeConfirm(t("apiKeys.confirmRevoke"))) return;
    try {
      await revokeUserAPIKey(keyId);
      toast.success(t("apiKeys.revoked"));
      mutate();
    } catch {
      toast.error(t("apiKeys.revokeFailed"));
    }
  };

  return (
    <div className="flex flex-col">
      <PageHeader
        title={t("apiKeys.title")}
        description={t("apiKeys.description")}
        actions={
          <Dialog
            open={dialogOpen}
            onOpenChange={(open) => {
              setDialogOpen(open);
              if (!open) {
                setCreatedKey(null);
                setNewScopes([...DEFAULT_API_KEY_SCOPES]);
                setNewAllowedZoneIds([]);
              }
            }}
          >
            <DialogTrigger render={<Button size="sm" className="gap-1.5" />}>
              <Plus className="h-3.5 w-3.5" />
              {t("apiKeys.create")}
            </DialogTrigger>
            <DialogContent className="sm:max-w-md">
              <DialogHeader>
                <DialogTitle>{t("apiKeys.createTitle")}</DialogTitle>
                <DialogDescription>{t("apiKeys.createDesc")}</DialogDescription>
              </DialogHeader>
              {createdKey ? (
                <div className="space-y-3 py-4">
                  <p className="text-sm text-muted-foreground">
                    {t("apiKeys.copyWarning")}
                  </p>
                  <div className="flex items-center gap-2">
                    <code className="flex-1 rounded bg-muted px-3 py-2 text-xs break-all font-mono">
                      {createdKey}
                    </code>
                    <Button
                      variant="outline"
                      size="icon"
                      onClick={() => {
                        navigator.clipboard.writeText(createdKey);
                        toast.success(t("apiKeys.copied"));
                      }}
                    >
                      <Copy className="h-4 w-4" />
                    </Button>
                  </div>
                  <DialogFooter>
                    <Button
                      onClick={() => {
                        setDialogOpen(false);
                        setCreatedKey(null);
                      }}
                    >
                      {t("apiKeys.done")}
                    </Button>
                  </DialogFooter>
                </div>
              ) : (
                <>
                  <div className="space-y-4 py-4">
                    <div className="space-y-2">
                      <Label>{t("apiKeys.label")}</Label>
                      <Input
                        placeholder={t("apiKeys.labelPlaceholder")}
                        value={newLabel}
                        onChange={(e) => setNewLabel(e.target.value)}
                        onKeyDown={(e) => e.key === "Enter" && handleCreate()}
                      />
                    </div>
                    <div className="space-y-2">
                      <Label>{t("apiKeys.template")}</Label>
                      <div className="flex flex-wrap gap-2">
                        {API_KEY_TEMPLATES.map((tpl) => (
                          <Button
                            key={tpl.id}
                            type="button"
                            variant="outline"
                            size="sm"
                            className="text-xs"
                            onClick={() => {
                              if (tpl.id !== "custom") {
                                setNewScopes([...tpl.scopes]);
                              }
                            }}
                          >
                            {t(`apiKeys.tpl.${tpl.id}` as Parameters<typeof t>[0])}
                          </Button>
                        ))}
                      </div>
                    </div>
                    <APIKeyScopePicker
                      value={newScopes}
                      onChange={setNewScopes}
                      disabled={creating}
                    />
                    <div className="space-y-2">
                      <div className="flex items-center justify-between gap-3">
                        <Label>{t("apiKeys.allowedZones")}</Label>
                        <span className="text-xs text-muted-foreground">
                          {newAllowedZoneIds.length === 0
                            ? t("apiKeys.allowedZonesAll")
                            : t("apiKeys.allowedZonesCount", { count: newAllowedZoneIds.length })}
                        </span>
                      </div>
                      <p className="text-xs text-muted-foreground">
                        {t("apiKeys.allowedZonesHint")}
                      </p>
                      <div className="rounded-md border p-3 space-y-2">
                        <Button
                          type="button"
                          variant="ghost"
                          size="sm"
                          className="h-7 px-2 text-xs"
                          onClick={() => setNewAllowedZoneIds([])}
                          disabled={creating}
                        >
                          {t("apiKeys.allowAllZones")}
                        </Button>
                        {domainOptions.length === 0 ? (
                          <p className="text-xs text-muted-foreground">
                            {t("apiKeys.noDomainsForAllowlist")}
                          </p>
                        ) : (
                          <div className="grid gap-2">
                            {domainOptions.map((zone) => {
                              const checked = newAllowedZoneIds.includes(zone.id);
                              return (
                                <label
                                  key={zone.id}
                                  className="flex items-center justify-between gap-3 rounded border px-2 py-1.5 text-xs"
                                >
                                  <span className="truncate" title={zone.domain}>
                                    {zoneLabel(zone)}
                                  </span>
                                  <Switch
                                    size="sm"
                                    checked={checked}
                                    disabled={creating}
                                    onCheckedChange={(next: boolean) =>
                                      setNewAllowedZoneIds((prev) =>
                                        next
                                          ? Array.from(new Set([...prev, zone.id]))
                                          : prev.filter((id) => id !== zone.id),
                                      )
                                    }
                                  />
                                </label>
                              );
                            })}
                          </div>
                        )}
                      </div>
                    </div>
                  </div>
                  <DialogFooter>
                    <Button onClick={handleCreate} disabled={creating || newScopes.length === 0}>
                      {creating ? t("apiKeys.creating") : t("apiKeys.create")}
                    </Button>
                  </DialogFooter>
                </>
              )}
            </DialogContent>
          </Dialog>
        }
      />

      <div className="p-4">
        <Card>
          <CardHeader className="pb-3">
            <CardTitle className="text-base">{t("apiKeys.allKeys")}</CardTitle>
            <CardDescription>{t("apiKeys.allKeysDesc")}</CardDescription>
          </CardHeader>
          <CardContent>
            {loading ? (
              <div className="space-y-3">
                {Array.from({ length: 3 }).map((_, i) => (
                  <Skeleton key={i} className="h-12 w-full" />
                ))}
              </div>
            ) : keys.length === 0 ? (
              <div className="text-center py-12 text-muted-foreground">
                <Key className="h-10 w-10 mx-auto mb-3 opacity-30" />
                <p className="text-sm">{t("apiKeys.noKeys")}</p>
                <p className="text-xs mt-1">{t("apiKeys.noKeysHint")}</p>
              </div>
            ) : (
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>{t("apiKeys.label")}</TableHead>
                    <TableHead>{t("apiKeys.prefix")}</TableHead>
                    <TableHead>{t("apiKeys.scopes")}</TableHead>
                    <TableHead>{t("apiKeys.allowedZones")}</TableHead>
                    <TableHead>{t("apiKeys.lastUsed")}</TableHead>
                    <TableHead>{t("apiKeys.created")}</TableHead>
                    <TableHead className="w-10" />
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {keys.map((k) => (
                    <TableRow key={k.id}>
                      <TableCell className="font-medium">
                        {k.label || "—"}
                      </TableCell>
                      <TableCell>
                        <code className="text-xs">{k.key_prefix}…</code>
                      </TableCell>
                      <TableCell>
                        <div className="flex flex-wrap gap-1">
                          {k.scopes.map((s) => (
                            <Badge key={s} variant="secondary" className="text-xs">
                              {s}
                            </Badge>
                          ))}
                        </div>
                      </TableCell>
                      <TableCell>
                        {!k.allowed_zone_ids || k.allowed_zone_ids.length === 0 ? (
                          <Badge variant="outline" className="text-xs">
                            {t("apiKeys.allowedZonesAll")}
                          </Badge>
                        ) : (
                          <div className="flex max-w-[220px] flex-wrap gap-1">
                            {k.allowed_zone_ids.slice(0, 3).map((id) => {
                              const zone = domainById.get(id);
                              return (
                                <Badge key={id} variant="secondary" className="max-w-full truncate text-xs" title={id}>
                                  {zone ? zoneLabel(zone) : `${id.slice(0, 8)}…`}
                                </Badge>
                              );
                            })}
                            {k.allowed_zone_ids.length > 3 && (
                              <Badge variant="outline" className="text-xs">
                                +{k.allowed_zone_ids.length - 3}
                              </Badge>
                            )}
                          </div>
                        )}
                      </TableCell>
                      <TableCell className="text-sm text-muted-foreground">
                        <div>
                          {k.last_used_at
                            ? formatDistanceToNow(new Date(k.last_used_at), {
                                addSuffix: true,
                              })
                            : t("apiKeys.never")}
                        </div>
                        {k.last_used_ip && (
                          <div className="text-xs text-muted-foreground/60 font-mono">{k.last_used_ip}</div>
                        )}
                      </TableCell>
                      <TableCell className="text-sm text-muted-foreground">
                        {formatDistanceToNow(new Date(k.created_at), {
                          addSuffix: true,
                        })}
                      </TableCell>
                      <TableCell>
                        <Button
                          variant="ghost"
                          size="icon"
                          className="h-8 w-8 text-destructive hover:text-destructive hover:bg-destructive/10"
                          onClick={() => handleRevoke(k.id)}
                        >
                          <Trash2 className="h-4 w-4" />
                        </Button>
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
