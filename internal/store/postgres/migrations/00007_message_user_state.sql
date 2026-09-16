-- +goose Up
-- Per-user message state (read/star) for shared mailboxes. Rows are written
-- sparsely, only when a member expresses a personal preference; absence of a
-- row means "read the shared baseline in messages.seen". Historical shared
-- state in messages.seen is preserved as the baseline and never rewritten by
-- personal toggles.
-- Tenant-scoped identity lets the new table carry tenant-scoped foreign keys.
CREATE UNIQUE INDEX messages_tenant_identity ON messages(tenant_id,id);
CREATE TABLE message_user_states (
 tenant_id UUID NOT NULL,
 mailbox_id UUID NOT NULL,
 message_id UUID NOT NULL,
 user_id UUID NOT NULL,
 seen BOOLEAN NOT NULL DEFAULT FALSE,
 starred BOOLEAN NOT NULL DEFAULT FALSE,
 updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 PRIMARY KEY(mailbox_id,message_id,user_id),
 UNIQUE(tenant_id,mailbox_id,message_id,user_id),
 FOREIGN KEY(tenant_id,mailbox_id) REFERENCES mailboxes(tenant_id,id) ON DELETE CASCADE,
 FOREIGN KEY(tenant_id,message_id) REFERENCES messages(tenant_id,id) ON DELETE CASCADE,
 FOREIGN KEY(tenant_id,user_id) REFERENCES users(tenant_id,id) ON DELETE CASCADE
);
CREATE INDEX message_user_states_mailbox_user ON message_user_states(mailbox_id,user_id);

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'message_user_states holds per-member read state; restore a coordinated backup'; END $$;
-- +goose StatementEnd
