-- +goose Up
-- Existing sessions become independent families. Deploy all auth writers
-- together; older binaries do not preserve a family when rotating a token.
ALTER TABLE refresh_tokens ADD COLUMN family_id UUID NOT NULL DEFAULT gen_random_uuid();
UPDATE refresh_tokens SET family_id = id;
CREATE INDEX refresh_tokens_family_idx ON refresh_tokens(family_id);

-- A company mailbox with an owner authenticates through that user's session
-- and exact mailbox grants; it need not create a second shared password.
-- Ownerless token mailboxes still require a password. Non-token modes must not
-- retain misleading credentials. The original same-tenant owner FK remains.
ALTER TABLE mailboxes DROP CONSTRAINT mailboxes_access_password_check;
ALTER TABLE mailboxes ADD CONSTRAINT mailboxes_access_password_check CHECK (
    (access_mode = 'token' AND (password_hash IS NOT NULL OR owner_user_id IS NOT NULL))
    OR (access_mode <> 'token' AND password_hash IS NULL)
);

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Refresh-family downgrade would erase revocation boundaries; use a coordinated restore'; END $$;
-- +goose StatementEnd
