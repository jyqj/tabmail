-- TabMail schema v1
-- Requires PostgreSQL 14+

CREATE EXTENSION IF NOT EXISTS "pgcrypto";

-- ============================================================
-- Enums
-- ============================================================

CREATE TYPE route_type  AS ENUM ('exact', 'wildcard', 'sequence');
CREATE TYPE access_mode AS ENUM ('public', 'token', 'api_key');

-- ============================================================
-- Plans  (管理员定义的默认套餐)
-- ============================================================

CREATE TABLE plans (
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

-- ============================================================
-- Tenants
-- ============================================================

CREATE TABLE tenants (
    id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    name       VARCHAR(255) NOT NULL,
    plan_id    UUID        NOT NULL REFERENCES plans(id),
    is_super   BOOLEAN     NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ============================================================
-- Tenant overrides  (租户级覆写, NULL = 继承 plan)
-- ============================================================

CREATE TABLE tenant_overrides (
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
-- Tenant API keys  (支持多 key / 轮换 / scope)
-- ============================================================

CREATE TABLE tenant_api_keys (
    id           UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    UUID         NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    key_hash     VARCHAR(128) NOT NULL,
    key_prefix   VARCHAR(16)  NOT NULL,
    label        VARCHAR(255) NOT NULL DEFAULT '',
    scopes       JSONB        NOT NULL DEFAULT '["*"]',
    expires_at   TIMESTAMPTZ,
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT now(),
    last_used_at TIMESTAMPTZ
);

CREATE INDEX idx_api_keys_prefix ON tenant_api_keys(key_prefix);
CREATE INDEX idx_api_keys_tenant ON tenant_api_keys(tenant_id);
CREATE UNIQUE INDEX idx_api_keys_hash ON tenant_api_keys(key_hash);

-- ============================================================
-- Domain zones  (已绑定并验证的根域/子域)
-- ============================================================

CREATE TABLE domain_zones (
    id          UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID         NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    domain      VARCHAR(255) NOT NULL UNIQUE,
    is_verified BOOLEAN      NOT NULL DEFAULT FALSE,
    mx_verified BOOLEAN      NOT NULL DEFAULT FALSE,
    txt_record  VARCHAR(255),
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    verified_at TIMESTAMPTZ
);

CREATE INDEX idx_zones_tenant ON domain_zones(tenant_id);
CREATE INDEX idx_zones_domain ON domain_zones(domain);

-- ============================================================
-- Domain routes  (子域路由规则)
--   exact:    a.mail.example.com
--   wildcard: *.mail.example.com
--   sequence: box-{n}.mail.example.com  n ∈ [range_start, range_end]
-- ============================================================

CREATE TABLE domain_routes (
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

CREATE INDEX idx_routes_zone ON domain_routes(zone_id);

-- ============================================================
-- Mailboxes  (内部统一叫 mailbox, API 可兼容 /accounts)
-- ============================================================

CREATE TABLE mailboxes (
    id                       UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id                UUID         NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    zone_id                  UUID         NOT NULL REFERENCES domain_zones(id) ON DELETE CASCADE,
    route_id                 UUID         REFERENCES domain_routes(id) ON DELETE SET NULL,
    local_part               VARCHAR(255) NOT NULL,
    resolved_domain          VARCHAR(255) NOT NULL,
    full_address             VARCHAR(512) NOT NULL UNIQUE,
    access_mode              access_mode  NOT NULL DEFAULT 'public',
    password_hash            VARCHAR(255),
    retention_hours_override INT,
    expires_at               TIMESTAMPTZ,
    created_at               TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE INDEX idx_mailboxes_tenant  ON mailboxes(tenant_id);
CREATE INDEX idx_mailboxes_zone    ON mailboxes(zone_id);
CREATE INDEX idx_mailboxes_address ON mailboxes(full_address);
CREATE INDEX idx_mailboxes_expires ON mailboxes(expires_at) WHERE expires_at IS NOT NULL;

-- ============================================================
-- Messages
-- ============================================================

CREATE TABLE messages (
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

CREATE INDEX idx_messages_mailbox_rcvd   ON messages(mailbox_id, received_at DESC);
CREATE INDEX idx_messages_tenant_expires ON messages(tenant_id, expires_at);
CREATE INDEX idx_messages_expires        ON messages(expires_at);

-- ============================================================
-- Audit log
-- ============================================================

CREATE TABLE audit_log (
    id            UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     UUID        REFERENCES tenants(id) ON DELETE SET NULL,
    actor         VARCHAR(255),
    action        VARCHAR(64) NOT NULL,
    resource_type VARCHAR(64) NOT NULL,
    resource_id   UUID,
    details       JSONB,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_audit_tenant_time ON audit_log(tenant_id, created_at DESC);

-- ============================================================
-- Seed data
-- ============================================================

INSERT INTO plans (id, name, max_domains, max_mailboxes_per_domain, max_messages_per_mailbox,
                   retention_hours, rpm_limit, daily_quota)
VALUES
    ('00000000-0000-0000-0000-000000000001', 'free', 1, 50, 100, 1, 20, 1000),
    ('00000000-0000-0000-0000-000000000002', 'pro',  20, 1000, 1000, 72, 300, 100000);

-- Public tenant: 无鉴权调用自动归属
INSERT INTO tenants (id, name, plan_id, is_super)
VALUES ('00000000-0000-0000-0000-000000000001', 'public',
        '00000000-0000-0000-0000-000000000001', FALSE);
