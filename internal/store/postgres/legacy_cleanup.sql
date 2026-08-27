-- One-off cleanup for pre-launch databases created by an older schema.sql.
--
-- This file is NOT embedded and NOT executed by postgres.New. schema.sql is a
-- declarative snapshot applied on every start, so it must contain only
-- statements that are correct on a fresh database; these are not. Apply this
-- by hand once against a long-lived development database, or just drop and
-- recreate the database, which is the supported path pre-launch.
--
--   psql "$TABMAIL_DB_DSN" -f internal/store/postgres/legacy_cleanup.sql
--
-- Every statement is idempotent and safe to run more than once.

-- permission_profiles.tenant_id is declared inline in schema.sql now; older
-- snapshots created the table without it.
ALTER TABLE permission_profiles ADD COLUMN IF NOT EXISTS tenant_id UUID REFERENCES tenants(id) ON DELETE CASCADE;

-- The unique constraint on name was replaced by the two partial indexes that
-- scope uniqueness to system-wide vs per-tenant profiles.
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'permission_profiles_name_key') THEN
        ALTER TABLE permission_profiles DROP CONSTRAINT permission_profiles_name_key;
    END IF;
END $$;

-- mailboxes.message_count was added after messages already existed.
UPDATE mailboxes m
SET message_count = sub.count
FROM (
    SELECT mailbox_id, COUNT(*)::BIGINT AS count
    FROM messages
    GROUP BY mailbox_id
) sub
WHERE m.id = sub.mailbox_id
  AND m.message_count = 0;

-- Zones created before send identities existed have no wildcard identity.
INSERT INTO send_identities (tenant_id, zone_id, address, identity_type, verified)
SELECT tenant_id, id, '*@' || domain, 'domain_wildcard', (is_verified AND mx_verified)
FROM domain_zones
ON CONFLICT DO NOTHING;

-- The grant system was removed; authorization now runs through authz.
DROP TABLE IF EXISTS send_as_grants CASCADE;
DROP TABLE IF EXISTS mailbox_grants CASCADE;
DROP TABLE IF EXISTS zone_grants CASCADE;

-- idx_messages_mailbox_rcvd was superseded by idx_messages_mailbox_rcvd_id,
-- which adds id so keyset pagination can walk the index directly.
DROP INDEX IF EXISTS idx_messages_mailbox_rcvd;
