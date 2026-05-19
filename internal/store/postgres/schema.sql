-- TabMail current schema snapshot.
-- 未上线阶段不做版本化数据库迁移；新环境启动时由 postgres.New 自动执行本文件。

CREATE EXTENSION IF NOT EXISTS "pgcrypto";

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

CREATE TABLE IF NOT EXISTS tenant_api_keys (
    id           UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    UUID         NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    key_hash     VARCHAR(128) NOT NULL,
    key_prefix   VARCHAR(16)  NOT NULL,
    label        VARCHAR(255) NOT NULL DEFAULT '',
    scopes       JSONB        NOT NULL DEFAULT '["domains:read","routes:read","mailboxes:read","messages:read"]',
    expires_at   TIMESTAMPTZ,
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT now(),
    last_used_at TIMESTAMPTZ,
    CONSTRAINT tenant_api_keys_scopes_check CHECK (
        jsonb_typeof(scopes) = 'array'
        AND jsonb_array_length(scopes) > 0
        AND scopes <@ '["domains:read","domains:write","routes:read","routes:write","mailboxes:read","mailboxes:write","messages:read","messages:write"]'::jsonb
    )
);
CREATE INDEX IF NOT EXISTS idx_api_keys_prefix ON tenant_api_keys(key_prefix);
CREATE INDEX IF NOT EXISTS idx_api_keys_tenant ON tenant_api_keys(tenant_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_api_keys_hash ON tenant_api_keys(key_hash);

CREATE TABLE IF NOT EXISTS domain_zones (
    id          UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID         NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    domain      VARCHAR(255) NOT NULL UNIQUE,
    is_verified BOOLEAN      NOT NULL DEFAULT FALSE,
    mx_verified BOOLEAN      NOT NULL DEFAULT FALSE,
    txt_record  VARCHAR(255),
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    verified_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_zones_tenant ON domain_zones(tenant_id);
CREATE INDEX IF NOT EXISTS idx_zones_domain ON domain_zones(domain);

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
    CONSTRAINT mailboxes_access_password_check CHECK (
        (access_mode = 'token' AND password_hash IS NOT NULL)
        OR (access_mode <> 'token' AND password_hash IS NULL)
    ),
    retention_hours_override INT,
    expires_at               TIMESTAMPTZ,
    created_at               TIMESTAMPTZ  NOT NULL DEFAULT now()
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

CREATE TABLE IF NOT EXISTS users (
    id             UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id      UUID         NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    email          VARCHAR(255) NOT NULL,
    password_hash  VARCHAR(255) NOT NULL,
    display_name   VARCHAR(255) NOT NULL DEFAULT '',
    role           user_role    NOT NULL DEFAULT 'user',
    is_active      BOOLEAN      NOT NULL DEFAULT TRUE,
    created_at     TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ  NOT NULL DEFAULT now(),
    last_login_at  TIMESTAMPTZ
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_email ON users(LOWER(email));
CREATE INDEX IF NOT EXISTS idx_users_tenant ON users(tenant_id);

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

-- Idempotent current-state hardening for existing local/dev databases.
ALTER TABLE mailboxes ADD COLUMN IF NOT EXISTS password_hash VARCHAR(255);
ALTER TABLE outbox_events ADD COLUMN IF NOT EXISTS claimed_at TIMESTAMPTZ;
ALTER TABLE outbox_events ADD COLUMN IF NOT EXISTS lease_until TIMESTAMPTZ;
ALTER TABLE webhook_deliveries ADD COLUMN IF NOT EXISTS claimed_at TIMESTAMPTZ;
ALTER TABLE webhook_deliveries ADD COLUMN IF NOT EXISTS lease_until TIMESTAMPTZ;
ALTER TABLE ingest_jobs ADD COLUMN IF NOT EXISTS claimed_at TIMESTAMPTZ;
ALTER TABLE ingest_jobs ADD COLUMN IF NOT EXISTS lease_until TIMESTAMPTZ;

DO $$ BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'admin_invitations_invited_by_fkey'
          AND confdeltype = 'n'
    ) THEN
        ALTER TABLE admin_invitations DROP CONSTRAINT IF EXISTS admin_invitations_invited_by_fkey;
        ALTER TABLE admin_invitations
            ADD CONSTRAINT admin_invitations_invited_by_fkey
            FOREIGN KEY (invited_by) REFERENCES users(id) ON DELETE SET NULL NOT VALID;
    END IF;
END $$;

DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'mailboxes_access_password_check') THEN
        ALTER TABLE mailboxes ADD CONSTRAINT mailboxes_access_password_check CHECK (
            (access_mode = 'token' AND password_hash IS NOT NULL)
            OR (access_mode <> 'token' AND password_hash IS NULL)
        ) NOT VALID;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'tenant_api_keys_scopes_check') THEN
        ALTER TABLE tenant_api_keys ADD CONSTRAINT tenant_api_keys_scopes_check CHECK (
            jsonb_typeof(scopes) = 'array'
            AND jsonb_array_length(scopes) > 0
            AND scopes <@ '["domains:read","domains:write","routes:read","routes:write","mailboxes:read","mailboxes:write","messages:read","messages:write"]'::jsonb
        ) NOT VALID;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'outbox_events_state_check') THEN
        ALTER TABLE outbox_events ADD CONSTRAINT outbox_events_state_check
            CHECK (state IN ('pending','processing','retry','done')) NOT VALID;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'webhook_deliveries_state_check') THEN
        ALTER TABLE webhook_deliveries ADD CONSTRAINT webhook_deliveries_state_check
            CHECK (state IN ('pending','processing','retry','delivered','dead')) NOT VALID;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'ingest_jobs_state_check') THEN
        ALTER TABLE ingest_jobs ADD CONSTRAINT ingest_jobs_state_check
            CHECK (state IN ('pending','processing','retry','done','dead')) NOT VALID;
    END IF;
END $$;

CREATE TABLE IF NOT EXISTS system_settings (
    key         VARCHAR(128) PRIMARY KEY,
    value       TEXT         NOT NULL,
    description TEXT         NOT NULL DEFAULT '',
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT now()
);

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

UPDATE mailboxes m
SET message_count = sub.count
FROM (
    SELECT mailbox_id, COUNT(*)::BIGINT AS count
    FROM messages
    GROUP BY mailbox_id
) sub
WHERE m.id = sub.mailbox_id;
