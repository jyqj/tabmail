"use client";

import { useId, useLayoutEffect, useRef, useState } from "react";
import { Copy, Plus, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { APIKeyScopePicker } from "@/components/api-key-scope-picker";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Skeleton } from "@/components/ui/skeleton";
import { useAPI } from "@/hooks/use-api";
import { createAPIKey, listAPIKeys, revokeAPIKey } from "@/lib/api";
import { DEFAULT_API_KEY_SCOPES } from "@/lib/api-key-scopes";
import { useI18n } from "@/lib/i18n";
import { sessionScope } from "@/lib/session";
import type { APIKeyCreated } from "@/lib/types";
import { safeConfirm } from "@/lib/utils";

export function TenantAPIKeysDialog({ tenantId, onClose }: {
  tenantId: string;
  onClose: () => void;
}) {
  const { t } = useI18n();
  const instance = useId();
  const [scope] = useState(sessionScope);
  const lifetime = useRef<object | null>(null);
  const running = useRef(false);
  const [busy, setBusy] = useState(false);
  const [created, setCreated] = useState<APIKeyCreated | null>(null);
  const [scopes, setScopes] = useState<string[]>([...DEFAULT_API_KEY_SCOPES]);
  // A reopened dialog must not join the previous lifetime's in-flight GET.
  const { data, error, isLoading, isValidating, mutate } = useAPI(
    ["tenant-api-keys", tenantId, instance],
    () => listAPIKeys(tenantId),
    { shouldRetryOnError: false },
  );
  const keys = data?.data ?? [];
  const readFailed = Boolean(error) || (data !== undefined && !Array.isArray(data.data));
  const reading = isLoading || isValidating;
  const ready = data !== undefined && !readFailed && !reading;

  useLayoutEffect(() => {
    lifetime.current = {};
    return () => { lifetime.current = null; };
  }, []);
  const owns = (owner: object) => lifetime.current === owner && scope === sessionScope();

  const create = async () => {
    const owner = lifetime.current;
    if (!owner || !owns(owner) || running.current || !ready || scopes.length === 0) return;
    running.current = true;
    setBusy(true);
    try {
      let response;
      try {
        response = await createAPIKey(tenantId, { scopes: [...scopes] });
      } catch {
        if (owns(owner)) toast.error(t("tenants.apiKeyCreateFailed"));
        return;
      }
      if (!owns(owner)) return;
      setCreated(response.data);
      setScopes([...DEFAULT_API_KEY_SCOPES]);
      toast.success(t("tenants.apiKeyCreated"));
      // The credential is already created. A failed read is displayed by SWR
      // and retried using GET, without reclassifying or replaying the POST.
      await mutate().catch(() => undefined);
    } finally {
      if (owns(owner)) {
        running.current = false;
        setBusy(false);
      }
    }
  };

  const revoke = async (keyId: string) => {
    const owner = lifetime.current;
    if (!owner || !owns(owner) || running.current || !ready || !keys.some(key => key.id === keyId)) return;
    if (!safeConfirm(t("tenants.confirmRevokeKey"))) return;
    if (!owns(owner) || running.current) return;
    running.current = true;
    setBusy(true);
    try {
      await revokeAPIKey(tenantId, keyId);
      if (!owns(owner)) return;
      await mutate(current => current && ({
        ...current, data: current.data.filter(key => key.id !== keyId),
      }), { revalidate: false });
      if (owns(owner)) toast.success(t("tenants.keyRevoked"));
    } catch {
      if (owns(owner)) toast.error(t("tenants.revokeFailed"));
    } finally {
      if (owns(owner)) {
        running.current = false;
        setBusy(false);
      }
    }
  };

  return (
    <Dialog open onOpenChange={open => {
      if (!open) {
        lifetime.current = null;
        onClose();
      }
    }}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{t("tenants.apiKeysTitle")}</DialogTitle>
          <DialogDescription>{t("tenants.apiKeysDesc")}</DialogDescription>
        </DialogHeader>
        {created && (
          <div className="rounded-lg border border-green-200 bg-green-50 dark:border-green-800 dark:bg-green-950 p-3">
            <p className="text-sm font-medium text-green-800 dark:text-green-200 mb-1">
              {t("tenants.newKeyCreated")}
            </p>
            <div className="flex items-center gap-2">
              <code className="flex-1 text-xs break-all bg-white dark:bg-black/20 p-2 rounded">{created.key}</code>
              <Button variant="outline" size="icon" className="h-8 w-8 shrink-0" onClick={() => {
                navigator.clipboard.writeText(created.key);
                toast.success(t("tenants.copied"));
              }}>
                <Copy className="h-3.5 w-3.5" />
              </Button>
            </div>
          </div>
        )}
        <div className="space-y-2">
          {readFailed ? (
            <div role="alert" className="space-y-2 rounded-lg border border-destructive p-3 text-sm">
              <p>{t("tenants.keysLoadFailed")}</p>
              <Button variant="outline" size="sm" disabled={busy || reading}
                onClick={() => { void mutate().catch(() => undefined); }}>
                {t("error.retry")}
              </Button>
            </div>
          ) : reading ? (
            <div className="space-y-2">
              <Skeleton className="h-10 w-full" /><Skeleton className="h-10 w-full" />
            </div>
          ) : keys.length === 0 ? (
            <p className="text-sm text-muted-foreground text-center py-4">{t("tenants.noApiKeys")}</p>
          ) : keys.map(key => (
            <div key={key.id} className="flex items-center justify-between rounded-lg border px-3 py-2">
              <div className="min-w-0 flex-1">
                <div className="flex items-center gap-2">
                  <code className="text-sm">{key.key_prefix}...</code>
                  {key.label && <Badge variant="secondary" className="text-xs">{key.label}</Badge>}
                </div>
                <p className="text-xs text-muted-foreground mt-0.5">{t("tenants.scopes")}: {key.scopes.join(", ")}</p>
              </div>
              <Button variant="ghost" size="icon"
                className="h-8 w-8 text-destructive hover:text-destructive shrink-0"
                disabled={busy || !ready} onClick={() => revoke(key.id)}>
                <Trash2 className="h-3.5 w-3.5" />
              </Button>
            </div>
          ))}
        </div>
        <DialogFooter>
          <div className="w-full space-y-3">
            <APIKeyScopePicker value={scopes} onChange={setScopes} disabled={busy} />
            <div className="flex justify-end">
              <Button size="sm" className="gap-1.5" onClick={create}
                disabled={busy || !ready || scopes.length === 0}>
                <Plus className="h-3.5 w-3.5" />{t("tenants.generateKey")}
              </Button>
            </div>
          </div>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
