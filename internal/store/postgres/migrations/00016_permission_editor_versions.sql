-- +goose Up
-- One noncycling durable allocator fences identity deletion/recreation too.
CREATE SEQUENCE permission_editor_revision_seq AS bigint NO CYCLE;
ALTER TABLE users ADD COLUMN permission_revision bigint NOT NULL DEFAULT nextval('permission_editor_revision_seq') CHECK (permission_revision > 0);
ALTER TABLE permission_profiles ADD COLUMN permission_revision bigint NOT NULL DEFAULT nextval('permission_editor_revision_seq') CHECK (permission_revision > 0);
ALTER TABLE user_permission_overrides ADD COLUMN domain_access_mode text CHECK (domain_access_mode IS NULL OR domain_access_mode IN ('inherit','all','list','none'));
ALTER TABLE user_permission_overrides ADD CONSTRAINT user_permission_domain_intent CHECK (
 domain_access_mode IS NULL OR
 (domain_access_mode='inherit' AND allowed_zone_ids IS NULL) OR
 (domain_access_mode IN ('all','none') AND allowed_zone_ids IS NOT NULL AND cardinality(allowed_zone_ids)=0) OR
 (domain_access_mode='list' AND allowed_zone_ids IS NOT NULL AND cardinality(allowed_zone_ids)>0)
);
-- Existing NULL/empty/list values retain their exact legacy interpretation.
-- +goose StatementBegin
CREATE FUNCTION permission_user_assignment_revision() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.permission_profile_id IS DISTINCT FROM OLD.permission_profile_id THEN
  NEW.permission_revision := nextval('permission_editor_revision_seq');
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER permission_user_assignment_revision BEFORE UPDATE OF permission_profile_id ON users FOR EACH ROW EXECUTE FUNCTION permission_user_assignment_revision();
-- +goose StatementBegin
CREATE FUNCTION permission_profile_revision() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 NEW.permission_revision := nextval('permission_editor_revision_seq');
 RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER permission_profile_revision BEFORE UPDATE ON permission_profiles FOR EACH ROW EXECUTE FUNCTION permission_profile_revision();
-- +goose StatementBegin
CREATE FUNCTION permission_override_revision() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='UPDATE' AND NEW.user_id IS DISTINCT FROM OLD.user_id THEN
  RAISE EXCEPTION 'permission override identity is immutable' USING ERRCODE='23514';
 END IF;
 IF TG_OP='DELETE' THEN
  UPDATE users SET permission_revision=nextval('permission_editor_revision_seq') WHERE id=OLD.user_id;
  RETURN OLD;
 END IF;
 UPDATE users SET permission_revision=nextval('permission_editor_revision_seq') WHERE id=NEW.user_id;
 RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER permission_override_revision AFTER INSERT OR UPDATE OR DELETE ON user_permission_overrides FOR EACH ROW EXECUTE FUNCTION permission_override_revision();

-- +goose Down
-- Do not roll back the persistent version owner once introduced: an old writer
-- would remove ABA fencing and misinterpret explicit domain denial as all.
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'permission editor version migration is irreversible; restore an audited backup instead'; END $$;
-- +goose StatementEnd
