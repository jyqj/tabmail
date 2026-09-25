-- +goose Up
-- Browser autosave supplies a stable draft UUID. A lost first response can
-- replay that creation, but deletion/submission must never resurrect it.
CREATE TABLE draft_creation_receipts (
 id UUID PRIMARY KEY, tenant_id UUID NOT NULL, user_id UUID NOT NULL,
 mailbox_id UUID NOT NULL, payload_hash TEXT NOT NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 FOREIGN KEY(tenant_id,user_id) REFERENCES users(tenant_id,id)
);
CREATE INDEX draft_receipts_owner ON draft_creation_receipts(tenant_id,user_id);
INSERT INTO draft_creation_receipts(id,tenant_id,user_id,mailbox_id,payload_hash)
 SELECT id,tenant_id,user_id,mailbox_id,'legacy' FROM mail_drafts;
-- Receipts are tombstones, not content. No expiry is applied automatically:
-- expiring one could resurrect a consumed client UUID.
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'draft creation tombstones require coordinated restore'; END $$;
-- +goose StatementEnd
