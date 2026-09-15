-- +goose Up
-- P0 company-mail foundation, ported from the archived company/ingress model.
-- Stop all old writers and retention processes before upgrading. This migration
-- never guesses how to convert an older, incompatible same-name grant table.
-- +goose StatementBegin
DO $$ BEGIN
 IF to_regclass('mailbox_grants') IS NOT NULL THEN
  RAISE EXCEPTION 'mailbox_grants already exists: preserve/export historical grants and review schema before P0 migration; automatic conversion is unsupported';
 END IF;
END $$;
-- +goose StatementEnd
CREATE UNIQUE INDEX IF NOT EXISTS users_tenant_identity ON users(tenant_id,id);
CREATE UNIQUE INDEX IF NOT EXISTS mailboxes_tenant_identity ON mailboxes(tenant_id,id);
ALTER TABLE mailboxes ADD COLUMN owner_user_id UUID;
ALTER TABLE mailboxes ADD CONSTRAINT mailbox_owner_same_tenant
 FOREIGN KEY(tenant_id,owner_user_id) REFERENCES users(tenant_id,id);
CREATE TABLE mailbox_grants (
 tenant_id UUID NOT NULL REFERENCES tenants(id),
 mailbox_id UUID NOT NULL,
 user_id UUID NOT NULL,
 can_read BOOLEAN NOT NULL DEFAULT FALSE,
 can_organize BOOLEAN NOT NULL DEFAULT FALSE,
 can_send BOOLEAN NOT NULL DEFAULT FALSE,
 template_only BOOLEAN NOT NULL DEFAULT FALSE,
 granted_by UUID REFERENCES users(id) ON DELETE SET NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 UNIQUE(mailbox_id,user_id),
 FOREIGN KEY(tenant_id,mailbox_id) REFERENCES mailboxes(tenant_id,id) ON DELETE CASCADE,
 FOREIGN KEY(tenant_id,user_id) REFERENCES users(tenant_id,id) ON DELETE CASCADE,
 CHECK(NOT can_organize OR can_read),
 CHECK(NOT template_only OR can_send)
);
CREATE INDEX mailbox_grants_user ON mailbox_grants(tenant_id,user_id);
ALTER TABLE messages ALTER COLUMN expires_at DROP NOT NULL;
ALTER TABLE outbound_jobs ADD COLUMN delivered_domains TEXT[] NOT NULL DEFAULT '{}';
ALTER TABLE outbound_jobs ADD COLUMN in_flight_domain TEXT NOT NULL DEFAULT '';
-- Immutable provenance, intentionally not ON DELETE SET NULL foreign keys.
ALTER TABLE outbound_jobs ADD COLUMN sender_user_id UUID;
ALTER TABLE outbound_jobs ADD COLUMN sender_key_id UUID;
ALTER TABLE outbound_jobs ADD COLUMN sender_mailbox_id UUID;
ALTER TABLE outbound_jobs ADD COLUMN template_name TEXT;
UPDATE outbound_jobs SET sender_user_id=user_id,sender_key_id=api_key_id;
-- Old processing jobs have no durable domain checkpoints; do not guess whether
-- remote DATA was accepted. Operators must reconcile before replay.
UPDATE outbound_jobs SET state='failed',in_flight_domain='legacy-unknown',
 last_error='Legacy in-flight delivery requires review; acceptance is unknown',
 delivery_token=NULL,claimed_at=NULL,lease_until=NULL,updated_at=now()
 WHERE state='processing';
-- Ingress ledger semantics are ported from archived migration 0004, but use
-- the current Goose v2 foundation; archived migration metadata is not imported.
ALTER TABLE ingest_jobs ADD COLUMN recovery_managed BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE ingest_jobs ADD COLUMN claim_token UUID;
ALTER TABLE ingest_jobs ADD COLUMN raw_sha256 TEXT;
ALTER TABLE ingest_jobs ADD COLUMN raw_size BIGINT CHECK(raw_size>=0);

CREATE TABLE ingest_recipient_outcomes (
 job_id UUID NOT NULL REFERENCES ingest_jobs(id) ON DELETE CASCADE,
 mailbox_id UUID NOT NULL,
 tenant_id UUID NOT NULL,
 zone_id UUID NOT NULL,
 address TEXT NOT NULL,
 state TEXT NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','delivered','held')),
 message_id UUID,
 attempts INT NOT NULL DEFAULT 0 CHECK(attempts>=0),
 last_error TEXT NOT NULL DEFAULT '',
 updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 PRIMARY KEY(job_id,mailbox_id),
 CHECK ((state='delivered') = (message_id IS NOT NULL))
);
-- Destination UUIDs are immutable snapshots, deliberately NOT cascading foreign
-- keys. Deleting/recreating an address must never redirect an accepted receipt.
-- A delivered tombstone survives deletion of the user's message until job GC.
CREATE TABLE ingress_daily_usage (
 tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
 day DATE NOT NULL,
 used BIGINT NOT NULL CHECK(used>=0),
 PRIMARY KEY(tenant_id,day)
);
CREATE INDEX idx_ingest_recipient_outcomes_tenant ON ingest_recipient_outcomes(tenant_id,job_id);
CREATE INDEX idx_ingress_managed_ready ON ingest_jobs(next_attempt_at,created_at)
 WHERE recovery_managed AND state IN ('pending','retry','processing');

-- Previous jobs have no trustworthy per-mailbox completion ledger. Keep their
-- original bytes for operator review rather than risk silently replaying them.
UPDATE ingest_jobs SET state='dead', claimed_at=NULL, lease_until=NULL,
 last_error='Legacy ingress requires review: no per-mailbox completion ledger',
 updated_at=now()
 WHERE state IN ('pending','retry','processing');

-- +goose Down
-- Destructive downgrade would erase mailbox permissions and delivery evidence.
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'P0 migration is intentionally irreversible; restore a coordinated backup'; END $$;
-- +goose StatementEnd
