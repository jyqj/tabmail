-- +goose Up
-- Existing sessions become independent families. Deploy all auth writers
-- together; older binaries do not preserve a family when rotating a token.
ALTER TABLE refresh_tokens ADD COLUMN family_id UUID NOT NULL DEFAULT gen_random_uuid();
UPDATE refresh_tokens SET family_id = id;
CREATE INDEX refresh_tokens_family_idx ON refresh_tokens(family_id);

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Refresh-family downgrade would erase revocation boundaries; use a coordinated restore'; END $$;
-- +goose StatementEnd
