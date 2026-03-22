export interface Plan {
  id: string;
  name: string;
  max_domains: number;
  max_mailboxes_per_domain: number;
  max_messages_per_mailbox: number;
  max_message_bytes: number;
  retention_hours: number;
  rpm_limit: number;
  daily_quota: number;
  created_at: string;
  updated_at: string;
}

export interface Tenant {
  id: string;
  name: string;
  plan_id: string;
  is_super: boolean;
  created_at: string;
}

export interface TenantOverride {
  max_domains?: number | null;
  max_mailboxes_per_domain?: number | null;
  max_messages_per_mailbox?: number | null;
  max_message_bytes?: number | null;
  retention_hours?: number | null;
  rpm_limit?: number | null;
  daily_quota?: number | null;
}

export interface TenantAPIKey {
  id: string;
  tenant_id: string;
  key_prefix: string;
  label: string;
  scopes: string[];
  expires_at: string | null;
  created_at: string;
}

export interface APIKeyCreated extends TenantAPIKey {
  key: string;
}

export interface EffectiveConfig {
  max_domains: number;
  max_mailboxes_per_domain: number;
  max_messages_per_mailbox: number;
  max_message_bytes: number;
  retention_hours: number;
  rpm_limit: number;
  daily_quota: number;
}

export interface DomainZone {
  id: string;
  tenant_id: string;
  domain: string;
  is_verified: boolean;
  mx_verified: boolean;
  txt_record: string;
  created_at: string;
  verified_at: string | null;
}

export type RouteType = "exact" | "wildcard" | "sequence";
export type AccessMode = "public" | "token" | "api_key";

export interface DomainRoute {
  id: string;
  zone_id: string;
  route_type: RouteType;
  match_value: string;
  range_start: number | null;
  range_end: number | null;
  auto_create_mailbox: boolean;
  retention_hours_override: number | null;
  access_mode_default: AccessMode;
  created_at: string;
}

export interface Mailbox {
  id: string;
  tenant_id: string;
  zone_id: string;
  local_part: string;
  resolved_domain: string;
  full_address: string;
  access_mode: AccessMode;
  retention_hours_override: number | null;
  expires_at: string | null;
  created_at: string;
}

export interface Message {
  id: string;
  tenant_id: string;
  mailbox_id: string;
  zone_id?: string;
  sender: string;
  recipients: string[];
  subject: string;
  size: number;
  seen: boolean;
  headers?: Record<string, string>;
  received_at: string;
  expires_at: string;
}

export interface MessageDetail extends Message {
  text_body: string;
  html_body: string;
}

export interface Meta {
  total: number;
  page: number;
  per_page: number;
}

export interface APIResponse<T> {
  data: T;
}

export interface APIListResponse<T> {
  data: T[];
  meta: Meta;
}

export interface APIError {
  error: {
    code: string;
    message: string;
  };
}

export interface DNSCheck {
  status: "pass" | "fail" | string;
  details?: string[];
}

export interface VerificationStatus {
  txt_expected: string;
  expected_mx: string;
  is_verified?: boolean;
  mx_verified?: boolean;
  checks: {
    txt: DNSCheck;
    mx: DNSCheck;
    spf: DNSCheck;
    dkim: DNSCheck;
    dmarc: DNSCheck;
  };
}

export interface SystemStats {
  tenants_count: number;
  plans_count: number;
  domains_count: number;
  mailboxes_count: number;
  messages_count: number;
  tenant_delivery: {
    key: string;
    accepted: number;
    rejected: number;
    deliveries_ok: number;
    deliveries_failed: number;
  }[];
  mailbox_delivery: {
    key: string;
    accepted: number;
    rejected: number;
    deliveries_ok: number;
    deliveries_failed: number;
  }[];
  dead_letters: {
    id: string;
    url: string;
    event_type: string;
    payload: unknown;
    attempts: number;
    last_error: string;
    created_at: string;
    last_tried_at: string;
  }[];
  metrics: {
    started_at: string;
    uptime_seconds: number;
    smtp: {
      sessions_opened: number;
      sessions_active: number;
      recipients_accepted: number;
      recipients_rejected: number;
      messages_accepted: number;
      messages_rejected: number;
      deliveries_succeeded: number;
      deliveries_failed: number;
      bytes_received: number;
    };
    webhooks: {
      enabled: boolean;
      configured: number;
      queued: number;
      delivered: number;
      failed: number;
      retried: number;
      dead_letter_size: number;
    };
    realtime: {
      subscribers_current: number;
      events_published: number;
    };
    time_series: {
      at: string;
      smtp_accepted: number;
      smtp_rejected: number;
      deliveries_ok: number;
      deliveries_failed: number;
      webhooks_delivered: number;
      webhooks_failed: number;
      realtime_published: number;
    }[];
  };
  recent_audit: {
    id: string;
    actor: string;
    action: string;
    resource_type: string;
    resource_id?: string | null;
    created_at: string;
  }[];
}

export interface MailboxTokenResponse {
  token: string;
  expires_in: number;
}

export interface MonitorEvent {
  type: string;
  mailbox: string;
  message_id?: string;
  sender?: string;
  subject?: string;
  size?: number;
  at: string;
}

export interface SMTPPolicy {
  default_accept: boolean;
  accept_domains: string[];
  reject_domains: string[];
  default_store: boolean;
  store_domains: string[];
  discard_domains: string[];
  reject_origin_domains: string[];
  updated_at?: string;
}
