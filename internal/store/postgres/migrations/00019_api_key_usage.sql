-- +goose Up
-- Deployment boundary: stop/drain ALL old key writers before applying this
-- migration, then start only usage-aware binaries. An already-running old
-- binary still updates tenant_api_keys and still conflicts with authority
-- FOR SHARE NOWAIT. Neither this backfill nor a rolling mixed deployment fixes
-- that old writer. No metadata dual-write trigger is installed.
-- Goose runs this migration transactionally. Serialize the finite backfill
-- with key lifecycle writes; this lock is not a substitute for stopping old
-- binaries, which could resume legacy writes after the migration commits.
LOCK TABLE tenant_api_keys IN SHARE ROW EXCLUSIVE MODE;

CREATE TABLE tenant_api_key_usage (
 api_key_id UUID PRIMARY KEY,
 last_used_at TIMESTAMPTZ,
 last_used_ip INET,
 CONSTRAINT tenant_api_key_usage_key_fk FOREIGN KEY (api_key_id)
  REFERENCES tenant_api_keys(id) ON DELETE CASCADE
);

-- Preserve old bytes/values, including NULLs. Historical last-used fields were
-- not necessarily paired observations; backfill does not retroactively prove
-- they were. New writes replace the entire pair using the observation clock.
INSERT INTO tenant_api_key_usage(api_key_id,last_used_at,last_used_ip)
 SELECT id,last_used_at,last_used_ip FROM tenant_api_keys;

-- Legacy metadata columns remain display fallback, not live write owners.
-- Authority remains solely tenant_api_keys, including owner/scope/zone/expiry.

-- +goose Down
-- Roll back application code only to a usage-compatible binary. Returning to
-- old writers requires a separately reviewed offline data/backup procedure;
-- silently dropping usage would lose observations and reintroduce contention.
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'API key usage migration is irreversible; use a usage-compatible binary or an audited offline restore'; END $$;
-- +goose StatementEnd
