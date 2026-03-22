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
