"use client";

import { useEffect, useRef, useState } from "react";
import { useSWRConfig } from "swr";
import { request } from "@/lib/api/base";
import { streamCompanyEvents, type CompanyInvalidation } from "@/lib/api/company-events";
import { sessionScope, useSessionScope } from "@/lib/session";

interface CompanyEventConsumerOptions {
  tenantId: string | null;
  enabled: boolean;
  cacheKeys: readonly string[];
  onInvalidate: (event: CompanyInvalidation) => void;
  revalidate: () => Promise<unknown>;
  onRevoked: () => void;
}

function denied(error: unknown) {
  if (error instanceof DOMException && error.name === "NotAllowedError") return true;
  const code = (error as { error?: { code?: string } })?.error?.code;
  return code === "UNAUTHORIZED" || code === "FORBIDDEN";
}

// Reuses the existing identity owner and authenticated SSE transport. Callers
// retain ownership of CAS drafts: invalidate only marks them stale; neither
// event data nor a background GET rebases/replays an unsaved command.
export function useCompanyEventConsumer(options: CompanyEventConsumerOptions) {
  const scope = useSessionScope();
  const { mutate } = useSWRConfig();
  const callbacks = useRef(options);
  callbacks.current = options;
  const [revokedScope, setRevokedScope] = useState<string | null>(null);
  const { tenantId, enabled } = options;
  useEffect(() => {
    if (!enabled || !tenantId || revokedScope === scope || scope !== sessionScope() ||
        localStorage.getItem("tabmail_tenant_id") !== tenantId) return;
    const abort = new AbortController();
    let alive = true, refreshing = false, pending = false;
    const current = () => alive && !abort.signal.aborted && scope === sessionScope() && localStorage.getItem("tabmail_tenant_id") === tenantId;
    const revoke = () => {
      if (!current()) return;
      // Scope-bound SWR mutation invalidates older in-flight cache writes too.
      const ownedKeys = [...callbacks.current.cacheKeys];
      callbacks.current.onRevoked();
      setRevokedScope(scope);
      void mutate(key => Array.isArray(key) && key[0] === "session" && key[1] === scope &&
        ownedKeys.includes(Array.isArray(key[2]) ? key[2][0] : key[2]), undefined, { revalidate: false });
      abort.abort();
    };
    const refresh = async () => {
      if (refreshing || !current()) return;
      refreshing = true;
      try {
        do {
          pending = false;
          // Same selected-tenant administrative authority as the stream; this
          // read also detects EOF after a live role/freeze revocation.
          await request("/api/v1/company/overview", { signal: abort.signal });
          if (!current()) return;
          await callbacks.current.revalidate();
        } while (pending && current());
      } catch (error) {
        if (current() && denied(error)) revoke();
        // Transient failures keep drafts stale. A periodic/reconnect resync
        // retries the authoritative read, never a business write.
      } finally { refreshing = false; }
    };
    void streamCompanyEvents(tenantId, { signal: abort.signal, onInvalidate: event => {
      if (!current()) return;
      callbacks.current.onInvalidate(event);
      pending = true;
      void refresh();
    } }).catch(error => { if (current() && denied(error)) revoke(); });
    return () => { alive = false; abort.abort(); };
  }, [scope, tenantId, enabled, revokedScope, mutate]);
  return revokedScope === scope;
}
