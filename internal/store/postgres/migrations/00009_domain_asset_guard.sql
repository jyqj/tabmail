-- +goose Up
-- A domain is an address configuration, not authority to erase mailboxes.
-- NO ACTION also closes the race with an in-flight mailbox INSERT: either the
-- insert or delete loses, never a successful insert followed by cascade loss.
ALTER TABLE mailboxes DROP CONSTRAINT mailboxes_zone_id_fkey;
ALTER TABLE mailboxes ADD CONSTRAINT mailboxes_zone_id_fkey
 FOREIGN KEY(zone_id) REFERENCES domain_zones(id) ON DELETE NO ACTION;

-- +goose Down
-- Restoring CASCADE would silently weaken the asset-protection invariant.
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'domain asset protection requires a coordinated restore; do not downgrade'; END $$;
-- +goose StatementEnd
