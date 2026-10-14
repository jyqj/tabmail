import { createServer, type ServerResponse } from "node:http";
import { once } from "node:events";
import { expect, test } from "vitest";
import { createR5FetchObserver } from "./r5-streaming-fetch-observer";
import { assertPermissionIntent } from "./r5-permission-contract-oracle";
import type { PermissionEditorSnapshot } from "@/lib/api/permission-editor-types";

// Independent transport/oracle controls, not business qualification. Transport
// bytes come from real loopback sockets; no fetch or Response implementation is mocked.
const tenant = "10000000-0000-4000-8000-000000000001";
const user = "20000000-0000-4000-8000-000000000001";
const zone = "30000000-0000-4000-8000-000000000001";
const eventID = "40000000-0000-4000-8000-000000000001";
function frame(event: string, value: unknown, id = "") {
  return `${id ? `id: ${id}\r\n` : ""}event: ${event}\r\ndata: ${JSON.stringify(value)}\r\n\r\n`;
}
async function until(predicate: () => boolean) {
  for (let attempt = 0; attempt < 300; attempt++) {
    if (predicate()) return;
    await new Promise(resolve => setTimeout(resolve, 5));
  }
  throw new Error("Independent observation timeout");
}
async function socketProbe(handler: (response: ServerResponse) => Promise<void> | void,
  probe: (origin: string) => Promise<void>) {
  const server = createServer((_request, response) => { void handler(response); });
  server.listen(0, "127.0.0.1");
  await once(server, "listening");
  const address = server.address();
  if (!address || typeof address === "string") throw new Error("Missing socket");
  try { await probe(`http://127.0.0.1:${address.port}`); }
  finally {
    server.closeAllConnections();
    await new Promise<void>((resolve, reject) => server.close(error => error ? reject(error) : resolve()));
    expect(server.listening).toBe(false);
  }
}
test("fragmented UTF8/CRLF metadata, EOF, reconnect, untouched JSON and closed observer", async () => {
  const changed = frame("company.admin.changed", { tenant_id: tenant, type: "company.admin.changed",
    metadata: { action: "permission.override.patch", resource_type: "user", resource_id: user }, ignored: "汉字" }, eventID);
  let requests = 0;
  await socketProbe(async response => {
    requests++;
    if (requests === 3) {
      response.writeHead(409, { "Content-Type": "application/json" });
      response.end('{"error":{"code":"CONFLICT"},"raw":[null,false,0,[]],"revision":"9007199254740993"}');
      return;
    }
    response.writeHead(200, { "Content-Type": "text/event-stream" });
    response.flushHeaders();
    for (const byte of Buffer.from(frame("ready", { tenant_id: tenant }) + changed)) {
      response.write(Buffer.from([byte]));
      await new Promise(resolve => setTimeout(resolve, 1));
    }
    response.end();
  }, async origin => {
    const calls: Parameters<typeof createR5FetchObserver>[2] = [];
    const observer = createR5FetchObserver(fetch, origin, calls);
    try {
      for (let index = 0; index < 2; index++) {
        const response = await observer.fetch(`${origin}/events`);
        expect(await response.text()).toContain("汉字");
        await until(() => observer.streams[index].ended);
        expect(observer.streams[index].frames).toEqual([{ event: "ready", tenant_id: tenant },
          { event: "company.admin.changed", id: eventID, tenant_id: tenant,
            action: "permission.override.patch", resource_type: "user", resource_id: user }]);
      }
      const response = await observer.fetch(`${origin}/json`);
      expect(response.status).toBe(409);
      expect(await response.text()).toBe('{"error":{"code":"CONFLICT"},"raw":[null,false,0,[]],"revision":"9007199254740993"}');
      expect(calls.at(-1)?.data).toEqual({ error: { code: "CONFLICT" }, raw: [null, false, 0, []], revision: "9007199254740993" });
      await observer.close();
      await expect(observer.fetch(`${origin}/events`)).rejects.toThrow("closed");
      expect(requests).toBe(3);
    } finally { await observer.close(); }
  });
});
test.each(["empty", "incomplete", "bad_scope", "bad_action", "bad_id"])("negative wire control %s cannot provide accepted ready/targeted event", async kind => {
  await socketProbe(response => {
    response.writeHead(200, { "Content-Type": "text/event-stream" });
    const metadata = { action: kind === "bad_action" ? "invented" : "permission.override.patch", resource_type: "user", resource_id: user };
    const body = kind === "empty" ? "" : kind === "incomplete" ? 'event: ready\ndata: {}' :
      frame("company.admin.changed", { tenant_id: kind === "bad_scope" ? "invalid" : tenant, type: "company.admin.changed", metadata }, kind === "bad_id" ? "invalid" : eventID);
    response.end(body);
  }, async origin => {
    const observer = createR5FetchObserver(fetch, origin, []);
    const response = await observer.fetch(`${origin}/events`);
    // Invalid metadata deliberately aborts the original tee as well.
    await response.text().catch(() => {});
    await until(() => observer.streams[0].ended || Boolean(observer.streams[0].error));
    expect(observer.streams[0].frames).toEqual([]);
    if (kind === "empty") await observer.close();
    else await expect(observer.close()).rejects.toThrow("observation failed");
  });
});
test.each(["caller", "owner"])("%s cancellation releases live unread tee and close is idempotent", async kind => {
  await socketProbe(response => {
    response.writeHead(200, { "Content-Type": "text/event-stream" });
    response.write(frame("ready", { tenant_id: tenant }));
  }, async origin => {
    const observer = createR5FetchObserver(fetch, origin, []);
    const consumer = new AbortController();
    await observer.fetch(`${origin}/events`, { signal: consumer.signal });
    await until(() => observer.streams[0].frames.length === 1);
    if (kind === "caller") consumer.abort();
    await observer.close();
    expect(observer.streams[0].aborted).toBe(true);
    expect(observer.streams[0].error).toBeUndefined();
    await observer.close();
  });
});
function snapshot(): PermissionEditorSnapshot {
  return { user_id: user, tenant_id: tenant, profile: null,
    revision: { user_id: user, tenant_id: tenant, user_revision: "9007199254740993", profile_id: null, profile_revision: null },
    overrides: { can_send: false, can_create_domains: null, can_create_routes: null, can_create_api_keys: null,
      daily_send_quota: 19, daily_receive_quota: null, max_mailboxes: null, max_domains: null,
      allowed_zone_ids: [zone], domain_access: { mode: "list", zone_ids: [zone] } },
    effective: { can_send: false, can_create_domains: false, can_create_routes: false, can_create_api_keys: false,
      daily_send_quota: 19, daily_receive_quota: 0, max_mailboxes: 0, max_domains: 0, allowed_zone_ids: [zone] },
    field_sources: { can_send: "override", can_create_domains: "default", can_create_routes: "default", can_create_api_keys: "default",
      daily_send_quota: "override", daily_receive_quota: "default", max_mailboxes: "default", max_domains: "default", domain_access: "override" },
    capabilities: { patch: true, assign_profile: false } };
}
test.each(["omitted", "null", "false", "0", "[]"])("independent raw %s corruption rejected despite unchanged effective permissions", variant => {
  const before = snapshot(), after = snapshot();
  after.revision.user_revision = "9007199254740994";
  const patch = variant === "omitted" ? { daily_send_quota: 25 } : variant === "null" ? { can_send: null } :
    variant === "false" ? { can_send: false } : variant === "0" ? { daily_send_quota: 0 } : { domain_access: { mode: "all" as const, zone_ids: [] } };
  if (variant === "false") before.overrides!.can_send = true;
  if (variant === "omitted" || variant === "0") after.overrides!.daily_send_quota = variant === "0" ? 0 : 25;
  if (variant === "null") { after.overrides!.can_send = null; after.field_sources.can_send = "default"; }
  if (variant === "[]") { after.overrides!.domain_access = { mode: "all", zone_ids: [] }; after.overrides!.allowed_zone_ids = []; }
  const command = { expected_revision: { ...before.revision }, patch };
  expect(() => assertPermissionIntent("PE02", variant, before, command, after)).not.toThrow();
  if (variant === "[]") { after.overrides!.domain_access = { mode: "none", zone_ids: [] }; }
  else if (variant === "0") { after.overrides!.daily_send_quota = null; after.field_sources.daily_send_quota = "default"; }
  else if (variant === "null") { after.overrides!.can_send = false; after.field_sources.can_send = "override"; }
  else { after.overrides!.can_send = null; after.field_sources.can_send = "default"; }
  expect(() => assertPermissionIntent("PE02", variant, before, command, after)).toThrow();
});
test.each(["user_id", "tenant_id", "user_revision", "profile_id", "profile_revision"])("independent compound CAS corruption %s rejected", field => {
  const before = snapshot(), after = snapshot();
  after.revision.user_revision = "9007199254740994";
  after.overrides!.daily_send_quota = 25;
  const command = { expected_revision: { ...before.revision }, patch: { daily_send_quota: 25 } };
  Object.assign(command.expected_revision, { [field]: field.endsWith("revision") ? "9007199254740992" : zone });
  expect(() => assertPermissionIntent("PE01", "default", before, command, after)).toThrow();
});
