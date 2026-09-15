-- +goose Up
-- Employee-owned mailboxes authenticate through their owner's account / grants.
-- Ownerless token mailboxes still require a separate password. Explicit public
-- mailboxes remain supported for the legacy temporary-mail use case.
ALTER TABLE mailboxes DROP CONSTRAINT mailboxes_access_password_check;
ALTER TABLE mailboxes ADD CONSTRAINT mailboxes_access_password_check CHECK (
    (access_mode = 'token' AND (password_hash IS NOT NULL OR owner_user_id IS NOT NULL))
    OR (access_mode <> 'token' AND password_hash IS NULL)
);
ALTER TABLE mailboxes ALTER COLUMN access_mode SET DEFAULT 'token';

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Owned private mailboxes require a coordinated restore; do not downgrade live authorization'; END $$;
-- +goose StatementEnd
