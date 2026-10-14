import { streamEvents } from "./base";

export const COMPANY_EVENTS_PATH = "/api/v1/company/events";
const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
const validId = (value: string) => uuid.test(value) && value !== "00000000-0000-0000-0000-000000000000";
const resources: Record<string, string> = {
  "company.configure": "company", "company.index_retry": "company",
  "employee.invite": "invitation", "employee.invite_revoke": "invitation",
  "employee.activate": "user", "employee.offboard.preview": "user", "employee.offboard": "user",
  "permission.override.patch": "user", "permission.profile.assign": "user",
  "mailbox.provision": "mailbox", "mailbox.handover": "mailbox", "mailbox.grant": "mailbox",
  "mailbox.send_policy": "mailbox", "mailbox.convert_shared": "mailbox",
  "template.save": "template", "template.publish": "template", "template.retire": "template",
  "template.version.revoke": "template", "template.grant": "template",
  "permission.profile.update": "permission_profile", "permission.profile.delete": "permission_profile",
  "ingress.inspect": "ingest_job", "ingress.retry": "ingest_job",
  "outbound.retry": "outbound_job", "outbound.reconcile": "outbound_job", "outbound.break_glass": "outbound_job",
};

export type CompanyInvalidation = { kind: "resync" } | {
  kind: "changed";
  metadata: { action: string; resource_type: string; resource_id: string };
};

function record(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}
function keys(value: Record<string, unknown>, expected: string[]) {
  const actual = Object.keys(value);
  return actual.length === expected.length && expected.every(key => Object.hasOwn(value, key));
}

// The event is an invalidation, not a permission snapshot. No unknown fields,
// cross-tenant metadata or inferred audience may enter the consumer.
export function parseCompanyEvent(type: string, data: unknown, tenantId: string): CompanyInvalidation | null {
  if (!validId(tenantId) || !record(data) || data.tenant_id !== tenantId) return null;
  if (type === "ready" || type === "resync") {
    return keys(data, ["tenant_id"]) ? { kind: "resync" } : null;
  }
  if (type !== "company.admin.changed" || !keys(data, ["type", "tenant_id", "occurred_at", "metadata"]) || data.type !== type) return null;
  if (typeof data.occurred_at !== "string" || !/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?(?:Z|[+-]\d{2}:\d{2})$/.test(data.occurred_at) || !Number.isFinite(Date.parse(data.occurred_at))) return null;
  const metadata = data.metadata;
  if (!record(metadata) || !keys(metadata, ["action", "resource_type", "resource_id"]) ||
      typeof metadata.action !== "string" || !Object.hasOwn(resources, metadata.action) ||
      resources[metadata.action] !== metadata.resource_type || typeof metadata.resource_id !== "string" || !validId(metadata.resource_id)) return null;
  return { kind: "changed", metadata: { action: metadata.action, resource_type: metadata.resource_type as string, resource_id: metadata.resource_id } };
}

export function streamCompanyEvents(tenantId: string, options: { signal: AbortSignal; onInvalidate: (event: CompanyInvalidation) => void }) {
  if (!validId(tenantId)) return Promise.reject(new Error("Selected company tenant is required"));
  return streamEvents(COMPANY_EVENTS_PATH, {
    signal: options.signal,
    onEvent(event) {
      // base.ts synthesizes a resync on every accepted connection. Tag wire
      // data separately so a server null/tenant-less resync is not trusted.
      if (event.type === "resync" && event.data === null) {
        options.onInvalidate({ kind: "resync" });
        return;
      }
      if (!record(event.data) || !Object.hasOwn(event.data, "wire")) return;
      const parsed = parseCompanyEvent(event.type, event.data.wire, tenantId);
      if (parsed) options.onInvalidate(parsed);
    },
  }, wire => ({ wire }));
}
