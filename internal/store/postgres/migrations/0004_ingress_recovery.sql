-- Version 3 is reserved for the separately reviewed, not-yet-merged outbound
-- recovery change. Versions 1 and 2 are immutable; this migration stands alone.
ALTER TABLE ingest_jobs ADD COLUMN recovery_managed BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE ingest_jobs ADD COLUMN claim_token UUID;
ALTER TABLE ingest_jobs ADD COLUMN raw_sha256 TEXT;
ALTER TABLE ingest_jobs ADD COLUMN raw_size BIGINT CHECK(raw_size>=0);

CREATE TABLE ingress_targets (
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
CREATE INDEX idx_ingress_targets_tenant ON ingress_targets(tenant_id,job_id);
CREATE INDEX idx_ingress_managed_ready ON ingest_jobs(next_attempt_at,created_at)
 WHERE recovery_managed AND state IN ('pending','retry','processing');

-- Previous jobs have no trustworthy per-mailbox completion ledger. Keep their
-- original bytes for operator review rather than risk silently replaying them.
UPDATE ingest_jobs SET state='dead', claimed_at=NULL, lease_until=NULL,
 last_error='Legacy ingress requires review: no per-mailbox completion ledger',
 updated_at=now()
 WHERE state IN ('pending','retry','processing');
