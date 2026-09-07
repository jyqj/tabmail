import type {
  IngressInspection,
  IngressSession,
} from "@/lib/api/ingress-recovery";
import type { IngestJob } from "@/lib/types";
export const session: IngressSession = {
  userId: "operator-a",
  accessToken: "operator-token-a",
  tenantId: "tenant-a",
};
export function storeSession(s = session, role = "super_admin") {
  localStorage.setItem("tabmail_user", JSON.stringify({ id: s.userId, role }));
  localStorage.setItem("tabmail_access_token", s.accessToken);
  if (s.tenantId) localStorage.setItem("tabmail_tenant_id", s.tenantId);
  else localStorage.removeItem("tabmail_tenant_id");
}
export const receipt: IngressInspection = {
  id: "00000000-0000-0000-0000-000000000001",
  state: "dead",
  recovery_managed: true,
  attempts: 2,
  last_error: "storage unavailable",
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:02:00.123456Z",
  next_attempt_at: "2026-01-01T00:03:00Z",
  expected_bytes: 50,
  can_retry: true,
  retry_block_reason: "",
  targets: [
    {
      job_id: "00000000-0000-0000-0000-000000000001",
      mailbox_id: "mailbox-a",
      tenant_id: "tenant-a",
      zone_id: "zone-a",
      address: "alice@example.test",
      state: "delivered",
      message_id: "message-a",
      attempts: 1,
      last_error: "",
    },
    {
      job_id: "00000000-0000-0000-0000-000000000001",
      mailbox_id: "mailbox-b",
      tenant_id: "tenant-b",
      zone_id: "zone-b",
      address: "bob@example.test",
      state: "held",
      attempts: 2,
      last_error: "quota full",
    },
  ],
};
export const job: IngestJob = {
  id: receipt.id,
  source: "smtp",
  remote_ip: "127.0.0.1",
  mail_from: "sender@example.test",
  recipients: ["alice@example.test", "bob@example.test"],
  raw_object_key: "private.eml",
  state: "dead",
  attempts: 2,
  last_error: receipt.last_error,
  next_attempt_at: receipt.next_attempt_at,
  created_at: receipt.created_at,
  updated_at: receipt.updated_at,
};
export function listed(total = 1) {
  return { data: [job], meta: { page: 1, per_page: 30, total } };
}
export function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (error: unknown) => void;
  const promise = new Promise<T>((yes, no) => {
    resolve = yes;
    reject = no;
  });
  return { promise, resolve, reject };
}
