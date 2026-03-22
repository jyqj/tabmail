-- Safe incremental patch for existing databases.
-- Fresh databases already get this from 001_init.sql.

CREATE UNIQUE INDEX IF NOT EXISTS idx_api_keys_hash ON tenant_api_keys(key_hash);
