// Test-only observation of actual HTTP responses. SSE never waits for EOF and
// never publishes response payloads. The caller owns cleanup through close().
export interface R5ObservedCall {
  method: string; path: string; body?: Record<string, unknown>; status: number; data?: unknown;
}
export interface R5StreamFrame {
  event: "ready" | "resync" | "company.admin.changed";
  id?: string; tenant_id: string; action?: string; resource_type?: string; resource_id?: string;
}
export interface R5ObservedStream {
  path: string; status: number; frames: R5StreamFrame[];
  ended: boolean; aborted: boolean; error?: string;
}
const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
const actions = new Set(["company.configure", "company.index_retry", "employee.invite", "employee.invite_revoke", "employee.activate", "employee.offboard.preview", "employee.offboard", "permission.override.patch", "permission.profile.assign", "mailbox.provision", "mailbox.handover", "mailbox.grant", "mailbox.send_policy", "mailbox.convert_shared", "template.save", "template.publish", "template.retire", "template.version.revoke", "template.grant", "permission.profile.update", "permission.profile.delete", "ingress.inspect", "ingress.retry", "outbound.retry", "outbound.reconcile", "outbound.break_glass"]);
function frameMetadata(frame: string): R5StreamFrame | undefined {
  let event = "", id = "";
  const data: string[] = [];
  for (const line of frame.split("\n")) {
    if (line.startsWith("event:")) event = line.slice(6).trim();
    if (line.startsWith("id:")) id = line.slice(3).trim();
    if (line.startsWith("data:")) data.push(line.slice(5).trimStart());
  }
  if (!["ready", "resync", "company.admin.changed"].includes(event)) return;
  const value = JSON.parse(data.join("\n")) as Record<string, unknown>;
  if (typeof value.tenant_id !== "string" || !uuid.test(value.tenant_id)) throw new Error("Invalid SSE scope");
  if (event === "ready" || event === "resync") return { event, tenant_id: value.tenant_id };
  const metadata = value.metadata as Record<string, unknown> | undefined;
  if (!uuid.test(id) || value.type !== event || !metadata || typeof metadata.action !== "string" || !actions.has(metadata.action) ||
      typeof metadata.resource_type !== "string" || !/^[a-z_]{1,32}$/.test(metadata.resource_type) ||
      typeof metadata.resource_id !== "string" || !uuid.test(metadata.resource_id)) throw new Error("Invalid SSE invalidation");
  return { event: "company.admin.changed", id, tenant_id: value.tenant_id,
    action: metadata.action, resource_type: metadata.resource_type, resource_id: metadata.resource_id };
}
export function createR5FetchObserver(fetchReal: typeof fetch, fixtureOrigin: string, calls: R5ObservedCall[]) {
  const streams: R5ObservedStream[] = [];
  const active = new Set<{ abort: AbortController; done: Promise<void> }>();
  let closed = false;
  const observedFetch: typeof fetch = async (input, init) => {
    if (closed) throw new Error("Fixture observer closed");
    const target = new URL(typeof input === "string" ? input : input instanceof URL ? input.href : input.url);
    if (target.origin !== fixtureOrigin) throw new Error("Component attempted non-fixture network I/O");
    const sourceSignal = init?.signal ?? (typeof input === "object" && "signal" in input ? input.signal : undefined);
    const abort = new AbortController();
    const forwardAbort = () => abort.abort(sourceSignal?.reason);
    if (sourceSignal?.aborted) forwardAbort();
    else sourceSignal?.addEventListener("abort", forwardAbort, { once: true });
    let finish!: () => void;
    const owned = { abort, done: new Promise<void>(resolve => { finish = resolve; }) };
    active.add(owned);
    const release = () => { sourceSignal?.removeEventListener("abort", forwardAbort); active.delete(owned); finish(); };
    try {
      const response = await fetchReal(input, { ...init, signal: abort.signal });
      const call: R5ObservedCall = { method: init?.method ?? (typeof input === "object" && "method" in input ? input.method : "GET"),
        path: target.pathname, body: typeof init?.body === "string" ? JSON.parse(init.body) : undefined, status: response.status };
      if (response.headers.get("content-type")?.split(";")[0].trim().toLowerCase() !== "text/event-stream") {
        try { call.data = await response.clone().json(); } catch { /* actual 204/non-JSON */ }
        calls.push(call); release(); return response;
      }
      calls.push(call);
      const stream: R5ObservedStream = { path: target.pathname, status: response.status, frames: [], ended: false, aborted: false };
      streams.push(stream);
      const reader = response.clone().body?.getReader();
      if (!reader) { release(); throw new Error("Actual SSE body missing"); }
      // Run independently: return the original live response at headers.
      void (async () => {
        const decoder = new TextDecoder();
        let pending = "";
        try {
          for (;;) {
            const part = await reader.read();
            if (part.done) { stream.ended = true; break; }
            pending += decoder.decode(part.value, { stream: true });
            pending = pending.replace(/\r\n/g, "\n");
            if (pending.length > 65536) throw new Error("SSE frame observation bound exceeded");
            let boundary: number;
            while ((boundary = pending.indexOf("\n\n")) !== -1) {
              const metadata = frameMetadata(pending.slice(0, boundary));
              pending = pending.slice(boundary + 2);
              if (metadata) {
                if (stream.frames.length >= 128) throw new Error("SSE metadata observation bound exceeded");
                stream.frames.push(metadata);
              }
            }
          }
          if (pending.trim()) throw new Error("Incomplete SSE frame");
        } catch { stream.aborted = abort.signal.aborted; if (!stream.aborted) stream.error = "SSE observation failed"; }
        finally {
          // Aborting the underlying HTTP request terminates both tee branches;
          // awaiting cancellation of only one live tee branch can deadlock.
          if (!stream.ended) abort.abort();
          await reader.cancel().catch(() => {});
          reader.releaseLock(); release();
        }
      })();
      return response;
    } catch (error) { abort.abort(); release(); throw error; }
  };
  return { fetch: observedFetch, streams, async close() {
    closed = true;
    const owned = [...active];
    for (const request of owned) request.abort.abort();
    await Promise.all(owned.map(request => request.done));
    if (streams.some(stream => stream.error)) throw new Error("Actual SSE observation failed");
  } };
}
