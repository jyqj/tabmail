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
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_ingest_jobs_pending
    ON ingest_jobs (state, next_attempt_at, created_at);
