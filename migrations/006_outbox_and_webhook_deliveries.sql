CREATE TABLE IF NOT EXISTS outbox_events (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    event_type      VARCHAR(64) NOT NULL,
    payload         JSONB       NOT NULL,
    occurred_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    state           VARCHAR(16) NOT NULL DEFAULT 'pending',
    attempts        INT         NOT NULL DEFAULT 0,
    last_error      TEXT,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_outbox_events_pending
    ON outbox_events (state, next_attempt_at, created_at);

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
    last_tried_at   TIMESTAMPTZ,
    delivered_at    TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_webhook_deliveries_event_url
    ON webhook_deliveries (event_id, url);

CREATE INDEX IF NOT EXISTS idx_webhook_deliveries_pending
    ON webhook_deliveries (state, next_attempt_at, created_at);
