-- TabMail baseline schema.
-- Pre-launch: executed by postgres.New on fresh databases.
-- All columns declared inline; no ALTER TABLE patches.

CREATE EXTENSION IF NOT EXISTS "pgcrypto";

-- ============================================================
-- Enum types
-- ============================================================

DO $$ BEGIN
    CREATE TYPE route_type AS ENUM ('exact', 'wildcard', 'sequence', 'deep_wildcard');
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;
DO $$ BEGIN
    ALTER TYPE route_type ADD VALUE IF NOT EXISTS 'deep_wildcard';
EXCEPTION WHEN undefined_object THEN NULL;
END $$;

DO $$ BEGIN
    CREATE TYPE access_mode AS ENUM ('public', 'token', 'api_key');
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

DO $$ BEGIN
    CREATE TYPE user_role AS ENUM ('admin', 'user');
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;
DO $$ BEGIN
    ALTER TYPE user_role ADD VALUE IF NOT EXISTS 'platform_admin';
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;
DO $$ BEGIN
    ALTER TYPE user_role ADD VALUE IF NOT EXISTS 'tenant_admin';
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

DO $$ BEGIN
    CREATE TYPE outbound_state AS ENUM ('pending','processing','sent','retry','failed','dead');
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

DO $$ BEGIN
    CREATE TYPE zone_grant_role AS ENUM ('owner', 'admin', 'editor', 'viewer');
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

DO $$ BEGIN
    CREATE TYPE send_identity_type AS ENUM ('exact', 'domain_wildcard');
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

DO $$ BEGIN
    CREATE TYPE mailbox_grant_role AS ENUM ('owner', 'manager', 'writer', 'reader');
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

-- ============================================================
-- Plans & tenants
-- ============================================================

CREATE TABLE IF NOT EXISTS plans (
    id                        UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    name                      VARCHAR(64) UNIQUE NOT NULL,
    max_domains               INT         NOT NULL DEFAULT 5,
    max_mailboxes_per_domain  INT         NOT NULL DEFAULT 100,
    max_messages_per_mailbox  INT         NOT NULL DEFAULT 500,
    max_message_bytes         INT         NOT NULL DEFAULT 10485760,
    retention_hours           INT         NOT NULL DEFAULT 24,
    rpm_limit                 INT         NOT NULL DEFAULT 60,
    daily_quota               INT         NOT NULL DEFAULT 10000,
    created_at                TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at                TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS tenants (
    id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    name       VARCHAR(255) NOT NULL,
    plan_id    UUID        NOT NULL REFERENCES plans(id),
    is_super   BOOLEAN     NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS tenant_overrides (
    id                        UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id                 UUID        NOT NULL UNIQUE REFERENCES tenants(id) ON DELETE CASCADE,
    max_domains               INT,
    max_mailboxes_per_domain  INT,
    max_messages_per_mailbox  INT,
    max_message_bytes         INT,
    retention_hours           INT,
    rpm_limit                 INT,
    daily_quota               INT,
    updated_at                TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ============================================================
-- Tenant API keys
-- ============================================================

CREATE TABLE IF NOT EXISTS tenant_api_keys (
    id              UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID         NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    key_hash        VARCHAR(128) NOT NULL,
    key_prefix      VARCHAR(16)  NOT NULL,
    label           VARCHAR(255) NOT NULL DEFAULT '',
    scopes          JSONB        NOT NULL DEFAULT '["domains:read","routes:read","mailboxes:read","messages:read"]',
    owner_user_id   UUID,
    allowed_zone_ids UUID[],
    expires_at      TIMESTAMPTZ,
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),
    last_used_at    TIMESTAMPTZ,
    CONSTRAINT tenant_api_keys_scopes_check CHECK (
        jsonb_typeof(scopes) = 'array'
        AND jsonb_array_length(scopes) > 0
        AND scopes <@ '["domains:read","domains:write","routes:read","routes:write","mailboxes:read","mailboxes:write","messages:read","messages:write","send:read","send:write"]'::jsonb
    )
);
CREATE INDEX IF NOT EXISTS idx_api_keys_prefix ON tenant_api_keys(key_prefix);
CREATE INDEX IF NOT EXISTS idx_api_keys_tenant ON tenant_api_keys(tenant_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_api_keys_hash ON tenant_api_keys(key_hash);
CREATE INDEX IF NOT EXISTS idx_api_keys_owner ON tenant_api_keys(owner_user_id) WHERE owner_user_id IS NOT NULL;

-- ============================================================
-- Permission profiles & user overrides
-- ============================================================

CREATE TABLE IF NOT EXISTS permission_profiles (
    id                  UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id           UUID         REFERENCES tenants(id) ON DELETE CASCADE,
    name                VARCHAR(64)  NOT NULL,
    description         TEXT         NOT NULL DEFAULT '',
    can_send            BOOLEAN      NOT NULL DEFAULT FALSE,
    daily_send_quota    INT          NOT NULL DEFAULT 0,
    daily_receive_quota INT          NOT NULL DEFAULT 1000,
    max_mailboxes       INT          NOT NULL DEFAULT 10,
    max_domains         INT          NOT NULL DEFAULT 1,
    allowed_zone_ids    UUID[],
    can_create_domains  BOOLEAN      NOT NULL DEFAULT FALSE,
    can_create_routes   BOOLEAN      NOT NULL DEFAULT FALSE,
    can_create_api_keys BOOLEAN      NOT NULL DEFAULT TRUE,
    is_system           BOOLEAN      NOT NULL DEFAULT FALSE,
    created_at          TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ  NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_perm_profiles_name_system
    ON permission_profiles(name) WHERE tenant_id IS NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_perm_profiles_name_tenant
    ON permission_profiles(tenant_id, name) WHERE tenant_id IS NOT NULL;

-- ============================================================
-- Users & auth
-- ============================================================

CREATE TABLE IF NOT EXISTS users (
    id                    UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id             UUID         NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    email                 VARCHAR(255) NOT NULL,
    password_hash         VARCHAR(255) NOT NULL,
    display_name          VARCHAR(255) NOT NULL DEFAULT '',
    role                  user_role    NOT NULL DEFAULT 'user',
    is_active             BOOLEAN      NOT NULL DEFAULT TRUE,
    permission_profile_id UUID         REFERENCES permission_profiles(id) ON DELETE SET NULL,
    created_at            TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at            TIMESTAMPTZ  NOT NULL DEFAULT now(),
    last_login_at         TIMESTAMPTZ
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_email ON users(LOWER(email));
CREATE INDEX IF NOT EXISTS idx_users_tenant ON users(tenant_id);

-- FK for tenant_api_keys.owner_user_id (declared after users table exists)
DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_api_keys_owner_user') THEN
        ALTER TABLE tenant_api_keys ADD CONSTRAINT fk_api_keys_owner_user
            FOREIGN KEY (owner_user_id) REFERENCES users(id) ON DELETE SET NULL NOT VALID;
    END IF;
END $$;

CREATE TABLE IF NOT EXISTS user_permission_overrides (
    id                  UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id             UUID         NOT NULL UNIQUE REFERENCES users(id) ON DELETE CASCADE,
    can_send            BOOLEAN,
    daily_send_quota    INT,
    daily_receive_quota INT,
    max_mailboxes       INT,
    max_domains         INT,
    allowed_zone_ids    UUID[],
    can_create_domains  BOOLEAN,
    can_create_routes   BOOLEAN,
    can_create_api_keys BOOLEAN,
    updated_at          TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS refresh_tokens (
    id          UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID         NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash  VARCHAR(128) NOT NULL UNIQUE,
    expires_at  TIMESTAMPTZ  NOT NULL,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    revoked_at  TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_refresh_tokens_user ON refresh_tokens(user_id);
CREATE INDEX IF NOT EXISTS idx_refresh_tokens_expires ON refresh_tokens(expires_at);

CREATE TABLE IF NOT EXISTS admin_invitations (
    id          UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    email       VARCHAR(255) NOT NULL,
    invite_code VARCHAR(128) NOT NULL UNIQUE,
    invited_by  UUID         REFERENCES users(id) ON DELETE SET NULL,
    expires_at  TIMESTAMPTZ  NOT NULL,
    accepted_at TIMESTAMPTZ,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_invitations_code ON admin_invitations(invite_code);
CREATE INDEX IF NOT EXISTS idx_invitations_email ON admin_invitations(LOWER(email));

-- ============================================================
-- Domain zones & routes
-- ============================================================

CREATE TABLE IF NOT EXISTS domain_zones (
    id                       UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id                UUID         NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    owner_user_id            UUID         REFERENCES users(id) ON DELETE SET NULL,
    parent_zone_id           UUID         REFERENCES domain_zones(id) ON DELETE SET NULL,
    domain                   VARCHAR(255) NOT NULL UNIQUE,
    visibility               VARCHAR(16)  NOT NULL DEFAULT 'private',
    allow_random_subdomains  BOOLEAN      NOT NULL DEFAULT FALSE,
    is_verified              BOOLEAN      NOT NULL DEFAULT FALSE,
    mx_verified              BOOLEAN      NOT NULL DEFAULT FALSE,
    txt_record               VARCHAR(255),
    dkim_private_key_pem     TEXT,
    dkim_selector            VARCHAR(63)  NOT NULL DEFAULT 'default',
    dkim_enabled             BOOLEAN      NOT NULL DEFAULT FALSE,
    dkim_required_for_send   BOOLEAN      NOT NULL DEFAULT FALSE,
    created_at               TIMESTAMPTZ  NOT NULL DEFAULT now(),
    verified_at              TIMESTAMPTZ,
    CONSTRAINT domain_zones_visibility_check CHECK (visibility IN ('private','authenticated','public'))
);
CREATE INDEX IF NOT EXISTS idx_zones_tenant ON domain_zones(tenant_id);
CREATE INDEX IF NOT EXISTS idx_zones_domain ON domain_zones(domain);
CREATE INDEX IF NOT EXISTS idx_zones_owner ON domain_zones(owner_user_id) WHERE owner_user_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_zones_parent ON domain_zones(parent_zone_id) WHERE parent_zone_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_zones_visibility ON domain_zones(visibility);

CREATE TABLE IF NOT EXISTS domain_routes (
    id                       UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    zone_id                  UUID        NOT NULL REFERENCES domain_zones(id) ON DELETE CASCADE,
    route_type               route_type  NOT NULL,
    match_value              TEXT        NOT NULL,
    range_start              INT,
    range_end                INT,
    auto_create_mailbox      BOOLEAN     NOT NULL DEFAULT TRUE,
    retention_hours_override INT,
    access_mode_default      access_mode NOT NULL DEFAULT 'public',
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_routes_zone ON domain_routes(zone_id);

-- ============================================================
-- Mailboxes & messages
-- ============================================================

CREATE TABLE IF NOT EXISTS mailboxes (
    id                       UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id                UUID         NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    zone_id                  UUID         NOT NULL REFERENCES domain_zones(id) ON DELETE CASCADE,
    route_id                 UUID         REFERENCES domain_routes(id) ON DELETE SET NULL,
    local_part               VARCHAR(255) NOT NULL,
    resolved_domain          VARCHAR(255) NOT NULL,
    full_address             VARCHAR(512) NOT NULL UNIQUE,
    access_mode              access_mode  NOT NULL DEFAULT 'public',
    password_hash            VARCHAR(255),
    message_count            BIGINT       NOT NULL DEFAULT 0,
    retention_hours_override INT,
    expires_at               TIMESTAMPTZ,
    created_at               TIMESTAMPTZ  NOT NULL DEFAULT now(),
    CONSTRAINT mailboxes_access_password_check CHECK (
        (access_mode = 'token' AND password_hash IS NOT NULL)
        OR (access_mode <> 'token' AND password_hash IS NULL)
    )
);
CREATE INDEX IF NOT EXISTS idx_mailboxes_tenant  ON mailboxes(tenant_id);
CREATE INDEX IF NOT EXISTS idx_mailboxes_zone    ON mailboxes(zone_id);
CREATE INDEX IF NOT EXISTS idx_mailboxes_address ON mailboxes(full_address);
CREATE INDEX IF NOT EXISTS idx_mailboxes_expires ON mailboxes(expires_at) WHERE expires_at IS NOT NULL;

CREATE TABLE IF NOT EXISTS messages (
    id              UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID         NOT NULL REFERENCES tenants(id),
    mailbox_id      UUID         NOT NULL REFERENCES mailboxes(id) ON DELETE CASCADE,
    zone_id         UUID         NOT NULL REFERENCES domain_zones(id),
    sender          VARCHAR(512) NOT NULL,
    recipients      TEXT[]       NOT NULL,
    subject         TEXT         NOT NULL DEFAULT '',
    size            BIGINT       NOT NULL DEFAULT 0,
    seen            BOOLEAN      NOT NULL DEFAULT FALSE,
    raw_object_key  VARCHAR(512),
    headers_json    JSONB,
    received_at     TIMESTAMPTZ  NOT NULL DEFAULT now(),
    expires_at      TIMESTAMPTZ  NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_messages_mailbox_rcvd   ON messages(mailbox_id, received_at DESC);
CREATE INDEX IF NOT EXISTS idx_messages_tenant_expires ON messages(tenant_id, expires_at);
CREATE INDEX IF NOT EXISTS idx_messages_expires        ON messages(expires_at);
CREATE INDEX IF NOT EXISTS idx_messages_raw_object_key ON messages(raw_object_key) WHERE raw_object_key IS NOT NULL;

-- ============================================================
-- Outbound jobs (send queue)
-- ============================================================

CREATE TABLE IF NOT EXISTS outbound_jobs (
    id                UUID           PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id         UUID           NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    user_id           UUID           REFERENCES users(id) ON DELETE SET NULL,
    api_key_id        UUID           REFERENCES tenant_api_keys(id) ON DELETE SET NULL,
    mail_from         VARCHAR(512)   NOT NULL,
    rcpt_to           TEXT[]         NOT NULL,
    subject           TEXT           NOT NULL DEFAULT '',
    text_body         TEXT,
    html_body         TEXT,
    headers_json      JSONB,
    raw_mime          BYTEA,
    zone_id           UUID           NOT NULL REFERENCES domain_zones(id),
    state             outbound_state NOT NULL DEFAULT 'pending',
    attempts          INT            NOT NULL DEFAULT 0,
    max_attempts      INT            NOT NULL DEFAULT 5,
    last_error        TEXT,
    next_attempt_at   TIMESTAMPTZ    NOT NULL DEFAULT now(),
    claimed_at        TIMESTAMPTZ,
    lease_until       TIMESTAMPTZ,
    smtp_code         INT,
    smtp_response     TEXT,
    message_id_header VARCHAR(512),
    delivery_token    UUID,
    created_at        TIMESTAMPTZ    NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ    NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_outbound_pending ON outbound_jobs (state, next_attempt_at, created_at) WHERE state IN ('pending','retry');
CREATE INDEX IF NOT EXISTS idx_outbound_tenant_date ON outbound_jobs (tenant_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_outbound_user_date ON outbound_jobs (user_id, created_at DESC) WHERE user_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_outbound_lease ON outbound_jobs (state, lease_until) WHERE state = 'processing';

-- ============================================================
-- Zone grants (team-level domain permissions)
-- ============================================================

CREATE TABLE IF NOT EXISTS zone_grants (
    id              UUID            PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID            NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    zone_id         UUID            NOT NULL REFERENCES domain_zones(id) ON DELETE CASCADE,
    principal_type  VARCHAR(16)     NOT NULL CHECK (principal_type IN ('user', 'api_key')),
    principal_id    UUID            NOT NULL,
    role            zone_grant_role NOT NULL DEFAULT 'viewer',
    created_by      UUID            REFERENCES users(id) ON DELETE SET NULL,
    created_at      TIMESTAMPTZ     NOT NULL DEFAULT now(),
    UNIQUE (zone_id, principal_type, principal_id)
);
CREATE INDEX IF NOT EXISTS idx_zone_grants_zone ON zone_grants(zone_id);
CREATE INDEX IF NOT EXISTS idx_zone_grants_principal ON zone_grants(principal_type, principal_id);
CREATE INDEX IF NOT EXISTS idx_zone_grants_tenant ON zone_grants(tenant_id);

-- ============================================================
-- Send identities & send-as grants
-- ============================================================

CREATE TABLE IF NOT EXISTS send_identities (
    id            UUID               PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     UUID               NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    zone_id       UUID               NOT NULL REFERENCES domain_zones(id) ON DELETE CASCADE,
    mailbox_id    UUID               REFERENCES mailboxes(id) ON DELETE SET NULL,
    address       VARCHAR(512)       NOT NULL,
    identity_type send_identity_type NOT NULL DEFAULT 'exact',
    verified      BOOLEAN            NOT NULL DEFAULT FALSE,
    created_at    TIMESTAMPTZ        NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, address, identity_type)
);
CREATE INDEX IF NOT EXISTS idx_send_identities_zone ON send_identities(zone_id);
CREATE INDEX IF NOT EXISTS idx_send_identities_tenant ON send_identities(tenant_id);
CREATE INDEX IF NOT EXISTS idx_send_identities_address ON send_identities(address);

CREATE TABLE IF NOT EXISTS send_as_grants (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    identity_id     UUID        NOT NULL REFERENCES send_identities(id) ON DELETE CASCADE,
    principal_type  VARCHAR(16) NOT NULL CHECK (principal_type IN ('user', 'api_key')),
    principal_id    UUID        NOT NULL,
    daily_quota     INT         NOT NULL DEFAULT 0,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (identity_id, principal_type, principal_id)
);
CREATE INDEX IF NOT EXISTS idx_send_as_grants_identity ON send_as_grants(identity_id);
CREATE INDEX IF NOT EXISTS idx_send_as_grants_principal ON send_as_grants(principal_type, principal_id);

-- ============================================================
-- Mailbox grants (per-mailbox access control)
-- ============================================================

CREATE TABLE IF NOT EXISTS mailbox_grants (
    id              UUID               PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID               NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    mailbox_id      UUID               NOT NULL REFERENCES mailboxes(id) ON DELETE CASCADE,
    principal_type  VARCHAR(16)        NOT NULL CHECK (principal_type IN ('user', 'api_key')),
    principal_id    UUID               NOT NULL,
    role            mailbox_grant_role NOT NULL DEFAULT 'reader',
    created_at      TIMESTAMPTZ        NOT NULL DEFAULT now(),
    UNIQUE (mailbox_id, principal_type, principal_id)
);
CREATE INDEX IF NOT EXISTS idx_mailbox_grants_mailbox ON mailbox_grants(mailbox_id);
CREATE INDEX IF NOT EXISTS idx_mailbox_grants_principal ON mailbox_grants(principal_type, principal_id);
CREATE INDEX IF NOT EXISTS idx_mailbox_grants_tenant ON mailbox_grants(tenant_id);

-- ============================================================
-- System operations
-- ============================================================

CREATE TABLE IF NOT EXISTS audit_log (
    id            UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     UUID        REFERENCES tenants(id) ON DELETE SET NULL,
    actor         VARCHAR(255),
    action        VARCHAR(64) NOT NULL,
    resource_type VARCHAR(64) NOT NULL,
    resource_id   UUID,
    details       JSONB,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_audit_tenant_time ON audit_log(tenant_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_audit_created_at ON audit_log(created_at DESC);

CREATE TABLE IF NOT EXISTS smtp_policies (
    id                    BOOLEAN PRIMARY KEY DEFAULT TRUE,
    default_accept        BOOLEAN NOT NULL DEFAULT TRUE,
    accept_domains        TEXT[]   NOT NULL DEFAULT '{}',
    reject_domains        TEXT[]   NOT NULL DEFAULT '{}',
    default_store         BOOLEAN NOT NULL DEFAULT TRUE,
    store_domains         TEXT[]   NOT NULL DEFAULT '{}',
    discard_domains       TEXT[]   NOT NULL DEFAULT '{}',
    reject_origin_domains TEXT[]   NOT NULL DEFAULT '{}',
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (id = TRUE)
);

CREATE TABLE IF NOT EXISTS monitor_events (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    type       VARCHAR(32) NOT NULL,
    mailbox    VARCHAR(512) NOT NULL DEFAULT '',
    message_id VARCHAR(64) DEFAULT '',
    sender     VARCHAR(512) DEFAULT '',
    subject    TEXT DEFAULT '',
    size       BIGINT NOT NULL DEFAULT 0,
    at         TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_monitor_events_at ON monitor_events(at DESC);
CREATE INDEX IF NOT EXISTS idx_monitor_events_type ON monitor_events(type);
CREATE INDEX IF NOT EXISTS idx_monitor_events_mailbox ON monitor_events(mailbox);
CREATE INDEX IF NOT EXISTS idx_monitor_events_sender ON monitor_events(sender);

CREATE TABLE IF NOT EXISTS system_settings (
    key         VARCHAR(128) PRIMARY KEY,
    value       TEXT         NOT NULL,
    description TEXT         NOT NULL DEFAULT '',
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT now()
);

-- ============================================================
-- Async job queues
-- ============================================================

CREATE TABLE IF NOT EXISTS outbox_events (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    event_type      VARCHAR(64) NOT NULL,
    payload         JSONB       NOT NULL,
    occurred_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    state           VARCHAR(16) NOT NULL DEFAULT 'pending',
    attempts        INT         NOT NULL DEFAULT 0,
    last_error      TEXT,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    claimed_at      TIMESTAMPTZ,
    lease_until     TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT outbox_events_state_check CHECK (state IN ('pending','processing','retry','done'))
);
CREATE INDEX IF NOT EXISTS idx_outbox_events_pending ON outbox_events (state, next_attempt_at, created_at);
CREATE INDEX IF NOT EXISTS idx_outbox_events_lease ON outbox_events (state, lease_until) WHERE state = 'processing';

CREATE TABLE IF NOT EXISTS webhook_deliveries (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    event_id        UUID        NOT NULL REFERENCES outbox_events(id) ON DELETE CASCADE,
    url             TEXT        NOT NULL,
    event_type      VARCHAR(64) NOT NULL,
    payload         JSONB       NOT NULL,
    state           VARCHAR(16) NOT NULL DEFAULT 'pending',
    attempts        INT         NOT NULL DEFAULT 0,
    last_error      TEXT,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    claimed_at      TIMESTAMPTZ,
    lease_until     TIMESTAMPTZ,
    last_tried_at   TIMESTAMPTZ,
    delivered_at    TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT webhook_deliveries_state_check CHECK (state IN ('pending','processing','retry','delivered','dead'))
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_webhook_deliveries_event_url ON webhook_deliveries (event_id, url);
CREATE INDEX IF NOT EXISTS idx_webhook_deliveries_pending ON webhook_deliveries (state, next_attempt_at, created_at);
CREATE INDEX IF NOT EXISTS idx_webhook_deliveries_lease ON webhook_deliveries (state, lease_until) WHERE state = 'processing';

CREATE TABLE IF NOT EXISTS ingest_jobs (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    source          VARCHAR(32) NOT NULL,
    remote_ip       VARCHAR(128) NOT NULL DEFAULT '',
    mail_from       VARCHAR(512) NOT NULL DEFAULT '',
    recipients      TEXT[]      NOT NULL,
    raw_object_key  VARCHAR(512) NOT NULL,
    metadata        JSONB,
    state           VARCHAR(16) NOT NULL DEFAULT 'pending',
    attempts        INT         NOT NULL DEFAULT 0,
    last_error      TEXT,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    claimed_at      TIMESTAMPTZ,
    lease_until     TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT ingest_jobs_state_check CHECK (state IN ('pending','processing','retry','done','dead'))
);
CREATE INDEX IF NOT EXISTS idx_ingest_jobs_pending ON ingest_jobs (state, next_attempt_at, created_at);
CREATE INDEX IF NOT EXISTS idx_ingest_jobs_raw_state ON ingest_jobs (raw_object_key, state);
CREATE INDEX IF NOT EXISTS idx_ingest_jobs_lease ON ingest_jobs (state, lease_until) WHERE state = 'processing';

-- ============================================================
-- Seed data
-- ============================================================

INSERT INTO plans (id, name, max_domains, max_mailboxes_per_domain, max_messages_per_mailbox,
                   retention_hours, rpm_limit, daily_quota)
VALUES
    ('00000000-0000-0000-0000-000000000001', 'free', 1, 50, 100, 1, 20, 1000),
    ('00000000-0000-0000-0000-000000000002', 'pro',  20, 1000, 1000, 72, 300, 100000)
ON CONFLICT (id) DO NOTHING;

INSERT INTO tenants (id, name, plan_id, is_super)
VALUES ('00000000-0000-0000-0000-000000000001', 'public',
        '00000000-0000-0000-0000-000000000001', FALSE)
ON CONFLICT (id) DO NOTHING;

INSERT INTO permission_profiles (id, name, description, can_send, daily_send_quota, daily_receive_quota, max_mailboxes, max_domains, can_create_domains, can_create_routes, can_create_api_keys, is_system)
VALUES
    ('00000000-0000-0000-0000-000000000010', 'admin', 'Full access, no limits', TRUE, 0, 0, 0, 0, TRUE, TRUE, TRUE, TRUE),
    ('00000000-0000-0000-0000-000000000011', 'default', 'Standard user with receive-only access', FALSE, 0, 500, 10, 1, FALSE, FALSE, TRUE, TRUE)
ON CONFLICT (id) DO NOTHING;

-- ============================================================
-- Data migrations (idempotent, for existing databases)
-- ============================================================

-- Add tenant_id to permission_profiles for tenant scoping
ALTER TABLE permission_profiles ADD COLUMN IF NOT EXISTS tenant_id UUID REFERENCES tenants(id) ON DELETE CASCADE;
-- Drop legacy unique constraint on name (replaced by partial indexes)
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'permission_profiles_name_key') THEN
        ALTER TABLE permission_profiles DROP CONSTRAINT permission_profiles_name_key;
    END IF;
END $$;

-- Migrate legacy 'admin' role to 'platform_admin'
UPDATE users SET role = 'platform_admin' WHERE role = 'admin';

-- Backfill mailbox message counts
UPDATE mailboxes m
SET message_count = sub.count
FROM (
    SELECT mailbox_id, COUNT(*)::BIGINT AS count
    FROM messages
    GROUP BY mailbox_id
) sub
WHERE m.id = sub.mailbox_id
  AND m.message_count = 0;

-- Backfill zone grants from owner_user_id
INSERT INTO zone_grants (tenant_id, zone_id, principal_type, principal_id, role)
SELECT tenant_id, id, 'user', owner_user_id, 'owner'
FROM domain_zones
WHERE owner_user_id IS NOT NULL
ON CONFLICT DO NOTHING;

-- Backfill send identities for existing zones
INSERT INTO send_identities (tenant_id, zone_id, address, identity_type, verified)
SELECT tenant_id, id, '*@' || domain, 'domain_wildcard', (is_verified AND mx_verified)
FROM domain_zones
ON CONFLICT DO NOTHING;

-- Backfill send-as grants for zone owners
INSERT INTO send_as_grants (tenant_id, identity_id, principal_type, principal_id)
SELECT si.tenant_id, si.id, 'user', dz.owner_user_id
FROM send_identities si
JOIN domain_zones dz ON dz.id = si.zone_id
WHERE dz.owner_user_id IS NOT NULL AND si.identity_type = 'domain_wildcard'
ON CONFLICT DO NOTHING;
