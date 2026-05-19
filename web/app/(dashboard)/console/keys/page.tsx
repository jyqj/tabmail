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
import { Skeleton } from "@/components/ui/skeleton";
import { APIKeyScopePicker } from "@/components/api-key-scope-picker";
import { createUserAPIKey, listUserAPIKeys, revokeUserAPIKey } from "@/lib/api";
import { DEFAULT_API_KEY_SCOPES } from "@/lib/api-key-scopes";
import type { TenantAPIKey } from "@/lib/types";
import { Plus, Key, Trash2, Copy } from "lucide-react";
import { toast } from "sonner";
import { formatDistanceToNow } from "date-fns";
import { useI18n } from "@/lib/i18n";

function safeConfirm(message: string) {
  if (typeof window === "undefined" || typeof window.confirm !== "function") return true;
  try {
    return window.confirm(message) !== false;
  } catch {
    return true;
  }
}

export default function UserAPIKeysPage() {
  const { t } = useI18n();
  const [keys, setKeys] = useState<TenantAPIKey[]>([]);
  const [loading, setLoading] = useState(true);
  const [dialogOpen, setDialogOpen] = useState(false);
  const [creating, setCreating] = useState(false);
  const [newLabel, setNewLabel] = useState("");
  const [newScopes, setNewScopes] = useState<string[]>([...DEFAULT_API_KEY_SCOPES]);
  const [createdKey, setCreatedKey] = useState<string | null>(null);

  const fetchKeys = useCallback(async () => {
    try {
      const res = await listUserAPIKeys();
      setKeys(res.data ?? []);
    } catch {
      toast.error(t("apiKeys.loadFailed"));
    } finally {
      setLoading(false);
    }
  }, [t]);

  useEffect(() => {
    fetchKeys();
  }, [fetchKeys]);

  const handleCreate = async () => {
    setCreating(true);
    try {
      const res = await createUserAPIKey({
        label: newLabel.trim() || undefined,
        scopes: newScopes,
      });
      setCreatedKey(res.data.key);
      setNewLabel("");
      setNewScopes([...DEFAULT_API_KEY_SCOPES]);
      fetchKeys();
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
      fetchKeys();
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
                    <APIKeyScopePicker
                      value={newScopes}
                      onChange={setNewScopes}
                      disabled={creating}
                    />
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
                      <TableCell className="text-sm text-muted-foreground">
                        {k.last_used_at
                          ? formatDistanceToNow(new Date(k.last_used_at), {
                              addSuffix: true,
                            })
                          : t("apiKeys.never")}
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
