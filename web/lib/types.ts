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
  id?: string;
  tenant_id?: string;
  max_domains?: number | null;
  max_mailboxes_per_domain?: number | null;
  max_messages_per_mailbox?: number | null;
  max_message_bytes?: number | null;
  retention_hours?: number | null;
  rpm_limit?: number | null;
  daily_quota?: number | null;
  updated_at?: string;
}

export interface TenantAPIKey {
  id: string;
  tenant_id: string;
  key_prefix: string;
  label: string;
  scopes: string[];
  owner_user_id?: string | null;
  allowed_zone_ids?: string[] | null;
  expires_at: string | null;
  created_at: string;
  last_used_at?: string | null;
  last_used_ip?: string | null;
}

export interface APIKeyCreated {
  id: string;
  key: string;
  key_prefix: string;
  label: string;
  scopes: string[];
  allowed_zone_ids?: string[] | null;
  created_at: string;
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

export type ResourceVisibility = "private" | "authenticated" | "public";

export interface DomainZone {
  id: string;
  tenant_id: string;
  owner_user_id?: string | null;
  parent_zone_id?: string | null;
  domain: string;
  visibility: ResourceVisibility;
  allow_random_subdomains: boolean;
  is_verified: boolean;
  mx_verified: boolean;
  txt_record: string;
  dkim_selector: string;
  dkim_enabled: boolean;
  dkim_required_for_send: boolean;
  created_at: string;
  verified_at: string | null;
}

export type RouteType = "exact" | "wildcard" | "deep_wildcard" | "sequence";
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

export interface SuggestedAddress {
  zone_id: string;
  base_domain: string;
  domain: string;
  subdomain_label?: string;
  local_part: string;
  address: string;
  mode: "mailbox" | "subdomain";
  algorithm: string;
}

export interface Mailbox {
  id: string;
  tenant_id: string;
  zone_id: string;
  route_id?: string | null;
  local_part: string;
  resolved_domain: string;
  full_address: string;
  access_mode: AccessMode;
  retention_hours_override: number | null;
  expires_at: string | null;
  created_at: string;
}

export interface MailboxCreateInput {
  address: string;
  access_mode?: AccessMode;
  password?: string;
  retention_hours_override?: number;
  expires_at?: string;
}

export interface Message {
  id: string;
  tenant_id: string;
  mailbox_id: string;
  zone_id: string;
  sender: string;
  recipients: string[];
  subject: string;
  size: number;
  seen: boolean;
  headers?: Record<string, string>;
  raw_object_key?: string;
  received_at: string;
  expires_at: string;
}

export interface MessageDetail extends Message {
  text_body: string;
  html_body: string;
  body_redacted?: boolean;
  body_access?: string;
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

export interface VerificationChecks {
  txt: DNSCheck;
  mx: DNSCheck;
  spf: DNSCheck;
  dkim: DNSCheck;
  dmarc: DNSCheck;
}

export interface VerificationStatus {
  txt_expected: string;
  expected_mx: string;
  is_verified?: boolean;
  mx_verified?: boolean;
  dkim_record?: string;
  dkim_host?: string;
  dkim_enabled?: boolean;
  checks: VerificationChecks;
}

export interface DomainVerificationResult {
  id: string;
  domain: string;
  txt_record: string;
  is_verified: boolean;
  mx_verified: boolean;
  checks: VerificationChecks;
  hint: string;
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

export interface AuditEntry {
  id: string;
  tenant_id?: string | null;
  actor: string;
  action: string;
  resource_type: string;
  resource_id?: string | null;
  details?: unknown;
  created_at: string;
}

export interface MailboxTokenResponse {
  token: string;
  expires_in: number;
}

export type UserRole = "super_admin" | "admin" | "user";

export interface AuthUser {
  id: string;
  email: string;
  display_name: string;
  role: UserRole;
  permission_profile_id?: string;
  tenant_id: string;
}

export interface AdminUser {
  id: string;
  tenant_id: string;
  email: string;
  display_name: string;
  role: UserRole;
  permission_profile_id?: string;
  is_active: boolean;
  created_at: string;
  updated_at: string;
  last_login_at?: string | null;
}

export interface UpdateUserRequest {
  role?: UserRole;
  is_active?: boolean;
  display_name?: string;
  permission_profile_id?: string | null;
}

export interface LoginResponse {
  access_token: string;
  refresh_token: string;
  token_type: string;
  expires_in: number;
  user: AuthUser;
}

export interface RefreshResponse {
  access_token: string;
  refresh_token: string;
  token_type: string;
  expires_in: number;
}

export interface MarkSeenResponse {
  seen: boolean;
}

export interface SystemSetting {
  key: string;
  value: string;
  description: string;
  updated_at: string;
}

export interface MonitorEvent {
  id: string;
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

export interface IngestJob {
  id: string;
  source: string;
  remote_ip: string;
  mail_from: string;
  recipients: string[];
  raw_object_key: string;
  metadata?: unknown;
  state: string;
  attempts: number;
  last_error?: string;
  next_attempt_at: string;
  claimed_at?: string | null;
  lease_until?: string | null;
  created_at: string;
  updated_at: string;
}

export interface WebhookDelivery {
  id: string;
  event_id: string;
  url: string;
  event_type: string;
  payload?: unknown;
  state: string;
  attempts: number;
  last_error?: string;
  next_attempt_at: string;
  claimed_at?: string | null;
  lease_until?: string | null;
  last_tried_at?: string | null;
  delivered_at?: string | null;
  created_at: string;
  updated_at: string;
}

// ============================================================
// Permission Profile
// ============================================================

export interface PermissionProfile {
  id: string;
  tenant_id?: string | null;
  name: string;
  description: string;
  can_send: boolean;
  daily_send_quota: number;
  daily_receive_quota: number;
  max_mailboxes: number;
  max_domains: number;
  allowed_zone_ids: string[] | null;
  can_create_domains: boolean;
  can_create_routes: boolean;
  can_create_api_keys: boolean;
  is_system: boolean;
  created_at: string;
  updated_at: string;
}

export interface UserPermissionOverride {
  id: string;
  user_id: string;
  can_send?: boolean | null;
  daily_send_quota?: number | null;
  daily_receive_quota?: number | null;
  max_mailboxes?: number | null;
  max_domains?: number | null;
  allowed_zone_ids?: string[] | null;
  can_create_domains?: boolean | null;
  can_create_routes?: boolean | null;
  can_create_api_keys?: boolean | null;
  updated_at: string;
}

export interface EffectivePermission {
  can_send: boolean;
  daily_send_quota: number;
  daily_receive_quota: number;
  max_mailboxes: number;
  max_domains: number;
  allowed_zone_ids: string[] | null;
  can_create_domains: boolean;
  can_create_routes: boolean;
  can_create_api_keys: boolean;
}

// ============================================================
// Outbound
// ============================================================

export type OutboundState = "pending" | "processing" | "sent" | "retry" | "failed" | "dead";

export interface OutboundJob {
  id: string;
  tenant_id: string;
  user_id?: string;
  api_key_id?: string;
  mail_from: string;
  rcpt_to: string[];
  subject: string;
  text_body?: string;
  html_body?: string;
  headers?: Record<string, string>;
  zone_id: string;
  state: OutboundState;
  attempts: number;
  max_attempts: number;
  last_error?: string;
  next_attempt_at: string;
  claimed_at?: string | null;
  lease_until?: string | null;
  smtp_code?: number;
  smtp_response?: string;
  message_id_header?: string;
  created_at: string;
  updated_at: string;
}

export interface SendEmailRequest {
  from: string;
  to: string[];
  cc?: string[];
  bcc?: string[];
  subject: string;
  text_body?: string;
  html_body?: string;
  headers?: Record<string, string>;
}

export interface SendEmailResponse {
  id: string;
  message_id: string;
  state: OutboundState;
  created_at: string;
}

export type SendIdentityType = "exact" | "domain_wildcard";

export interface SendIdentity {
  id: string;
  tenant_id: string;
  zone_id: string;
  mailbox_id?: string;
  address: string;
  identity_type: SendIdentityType;
  verified: boolean;
  created_at: string;
}
