-- +goose Up
-- Deploy all writers together. No previously released migration is rewritten.
ALTER TABLE users ADD COLUMN session_version BIGINT NOT NULL DEFAULT 0;
ALTER TABLE mailboxes ADD COLUMN mailbox_kind TEXT NOT NULL DEFAULT 'legacy'
 CHECK(mailbox_kind IN ('legacy','personal','shared'));
ALTER TABLE mailboxes ADD COLUMN lifecycle_revision BIGINT NOT NULL DEFAULT 1;
UPDATE mailboxes SET mailbox_kind='personal' WHERE owner_user_id IS NOT NULL;
ALTER TABLE mailboxes ADD CONSTRAINT personal_mailbox_has_owner CHECK(mailbox_kind<>'personal' OR owner_user_id IS NOT NULL);
ALTER TABLE mailboxes DROP CONSTRAINT mailboxes_access_password_check;
ALTER TABLE mailboxes ADD CONSTRAINT mailboxes_access_password_check CHECK(
 (access_mode='token' AND (password_hash IS NOT NULL OR owner_user_id IS NOT NULL OR mailbox_kind='shared')) OR
 (access_mode<>'token' AND password_hash IS NULL));
ALTER TABLE mailboxes ADD CONSTRAINT shared_mailbox_private CHECK(mailbox_kind<>'shared' OR access_mode='token');

ALTER TABLE messages ADD COLUMN deleted_at TIMESTAMPTZ;
ALTER TABLE messages ADD COLUMN purge_after TIMESTAMPTZ;
ALTER TABLE messages ADD COLUMN archived_at TIMESTAMPTZ;
ALTER TABLE messages ADD CONSTRAINT trash_has_deadline CHECK((deleted_at IS NULL)=(purge_after IS NULL));
CREATE INDEX messages_visible_mailbox ON messages(mailbox_id,received_at DESC,id) WHERE deleted_at IS NULL;
CREATE INDEX messages_trash_cleanup ON messages(purge_after) WHERE deleted_at IS NOT NULL;

CREATE UNIQUE INDEX domain_zones_tenant_identity ON domain_zones(tenant_id,id);
CREATE TABLE company_settings (
 tenant_id UUID PRIMARY KEY REFERENCES tenants(id) ON DELETE CASCADE,
 primary_zone_id UUID NOT NULL,
 name TEXT NOT NULL CHECK(length(name) BETWEEN 1 AND 120),
 revision INT NOT NULL DEFAULT 1,
 updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 FOREIGN KEY(tenant_id,primary_zone_id) REFERENCES domain_zones(tenant_id,id)
);
CREATE TABLE employee_invitations (
 id UUID PRIMARY KEY, tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
 email TEXT NOT NULL, display_name TEXT NOT NULL, mailbox_address TEXT NOT NULL,
 permission_profile_id UUID REFERENCES permission_profiles(id),
 token_hash TEXT UNIQUE NOT NULL CHECK(length(token_hash)=64),
 invited_by UUID NOT NULL, expires_at TIMESTAMPTZ NOT NULL,
 consumed_at TIMESTAMPTZ, revoked_at TIMESTAMPTZ, created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX employee_invitation_tenant ON employee_invitations(tenant_id,created_at DESC);

CREATE TABLE mail_templates (
 id UUID PRIMARY KEY, tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
 name TEXT NOT NULL CHECK(length(name) BETWEEN 1 AND 120),
 draft JSONB NOT NULL, revision INT NOT NULL DEFAULT 1, retired BOOLEAN NOT NULL DEFAULT FALSE,
 created_by UUID NOT NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT now(), updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 UNIQUE(tenant_id,id), UNIQUE(tenant_id,name)
);
CREATE TABLE mail_template_versions (
 id UUID PRIMARY KEY, tenant_id UUID NOT NULL, template_id UUID NOT NULL,
 version INT NOT NULL CHECK(version>0), snapshot JSONB NOT NULL, content_hash TEXT NOT NULL,
 published_by UUID NOT NULL, published_at TIMESTAMPTZ NOT NULL DEFAULT now(), revoked_at TIMESTAMPTZ,
 UNIQUE(tenant_id,id), UNIQUE(template_id,version),
 FOREIGN KEY(tenant_id,template_id) REFERENCES mail_templates(tenant_id,id)
);
-- Published content is immutable even through accidental direct SQL updates.
-- +goose StatementBegin
CREATE FUNCTION protect_mail_template_version() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
 IF (NEW.id,NEW.tenant_id,NEW.template_id,NEW.version,NEW.snapshot,NEW.content_hash,NEW.published_by,NEW.published_at)
 IS DISTINCT FROM (OLD.id,OLD.tenant_id,OLD.template_id,OLD.version,OLD.snapshot,OLD.content_hash,OLD.published_by,OLD.published_at) THEN
  RAISE EXCEPTION 'published template content is immutable';
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER immutable_mail_template_version BEFORE UPDATE ON mail_template_versions
 FOR EACH ROW EXECUTE FUNCTION protect_mail_template_version();
CREATE TABLE mail_template_grants (
 tenant_id UUID NOT NULL, template_id UUID NOT NULL, mailbox_id UUID NOT NULL, user_id UUID NOT NULL,
 granted_by UUID NOT NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 PRIMARY KEY(template_id,mailbox_id,user_id),
 FOREIGN KEY(tenant_id,template_id) REFERENCES mail_templates(tenant_id,id),
 FOREIGN KEY(tenant_id,mailbox_id) REFERENCES mailboxes(tenant_id,id) ON DELETE CASCADE,
 FOREIGN KEY(tenant_id,user_id) REFERENCES users(tenant_id,id) ON DELETE CASCADE
);

CREATE TABLE mail_attachments (
 id UUID PRIMARY KEY, tenant_id UUID NOT NULL, mailbox_id UUID NOT NULL, user_id UUID NOT NULL,
 object_key TEXT UNIQUE NOT NULL, filename TEXT NOT NULL, content_type TEXT NOT NULL,
 size BIGINT NOT NULL CHECK(size BETWEEN 0 AND 20971520), sha256 TEXT NOT NULL DEFAULT '',
 state TEXT NOT NULL DEFAULT 'uploading' CHECK(state IN ('uploading','ready')),
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(), expires_at TIMESTAMPTZ NOT NULL DEFAULT now()+interval '7 days',
 UNIQUE(tenant_id,id),
 FOREIGN KEY(tenant_id,mailbox_id) REFERENCES mailboxes(tenant_id,id),
 FOREIGN KEY(tenant_id,user_id) REFERENCES users(tenant_id,id)
);
CREATE TABLE mail_drafts (
 id UUID PRIMARY KEY, tenant_id UUID NOT NULL, user_id UUID NOT NULL, mailbox_id UUID NOT NULL,
 payload JSONB NOT NULL, revision INT NOT NULL DEFAULT 1 CHECK(revision>0),
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(), updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 FOREIGN KEY(tenant_id,user_id) REFERENCES users(tenant_id,id) ON DELETE CASCADE,
 FOREIGN KEY(tenant_id,mailbox_id) REFERENCES mailboxes(tenant_id,id) ON DELETE CASCADE
);
CREATE INDEX mail_drafts_user ON mail_drafts(tenant_id,user_id,updated_at DESC);
ALTER TABLE outbound_jobs ADD COLUMN template_version_id UUID;
ALTER TABLE outbound_jobs ADD COLUMN content_digest TEXT NOT NULL DEFAULT '';
ALTER TABLE outbound_jobs ADD COLUMN submit_actor TEXT NOT NULL DEFAULT '';
ALTER TABLE outbound_jobs ADD COLUMN idempotency_key TEXT NOT NULL DEFAULT '' CHECK(length(idempotency_key)<=128);
ALTER TABLE outbound_jobs ADD COLUMN request_hash TEXT NOT NULL DEFAULT '';
ALTER TABLE outbound_jobs ADD COLUMN attachment_ids UUID[];
ALTER TABLE outbound_jobs ADD COLUMN recipient_ledger BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE outbound_jobs ADD CONSTRAINT outbound_template_same_tenant
 FOREIGN KEY(tenant_id,template_version_id) REFERENCES mail_template_versions(tenant_id,id);
CREATE UNIQUE INDEX outbound_submission_idempotency ON outbound_jobs(tenant_id,submit_actor,idempotency_key) WHERE idempotency_key<>'';
CREATE UNIQUE INDEX outbound_tenant_identity ON outbound_jobs(tenant_id,id);
CREATE TABLE outbound_attachments (
 tenant_id UUID NOT NULL, job_id UUID NOT NULL, attachment_id UUID NOT NULL,
 PRIMARY KEY(job_id,attachment_id),
 FOREIGN KEY(tenant_id,job_id) REFERENCES outbound_jobs(tenant_id,id) ON DELETE CASCADE,
 FOREIGN KEY(tenant_id,attachment_id) REFERENCES mail_attachments(tenant_id,id)
);
CREATE TABLE outbound_recipients (
 tenant_id UUID NOT NULL, job_id UUID NOT NULL, address TEXT NOT NULL,
 state TEXT NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','accepted','temporary','permanent','uncertain')),
 smtp_code INT NOT NULL DEFAULT 0, diagnostic TEXT NOT NULL DEFAULT '', attempts INT NOT NULL DEFAULT 0,
 updated_at TIMESTAMPTZ NOT NULL DEFAULT now(), PRIMARY KEY(job_id,address),
 FOREIGN KEY(tenant_id,job_id) REFERENCES outbound_jobs(tenant_id,id) ON DELETE CASCADE
);

-- Durable mailbox notifications: API and workers read the same commit log.
CREATE TABLE mailbox_event_log (
 sequence BIGSERIAL PRIMARY KEY, tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
 mailbox_id UUID NOT NULL, event_type TEXT NOT NULL, message_id UUID,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX mailbox_event_cursor ON mailbox_event_log(tenant_id,mailbox_id,sequence);
CREATE INDEX mailbox_event_expiry ON mailbox_event_log(created_at);
CREATE TABLE runtime_instances (
 id TEXT PRIMARY KEY, role TEXT NOT NULL, last_seen TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- The log is an invalidation feed, never the source of mail truth. Clients also
-- resync periodically, covering disconnects, pruning and commit-order gaps.
-- +goose StatementBegin
CREATE FUNCTION log_mailbox_change() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN
  INSERT INTO mailbox_event_log(tenant_id,mailbox_id,event_type,message_id) VALUES(OLD.tenant_id,OLD.mailbox_id,'delete',OLD.id);
  RETURN OLD;
 END IF;
 INSERT INTO mailbox_event_log(tenant_id,mailbox_id,event_type,message_id) VALUES(NEW.tenant_id,NEW.mailbox_id,CASE WHEN TG_OP='INSERT' THEN 'message' ELSE 'changed' END,NEW.id);
 RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER mailbox_change AFTER INSERT OR UPDATE OR DELETE ON messages FOR EACH ROW EXECUTE FUNCTION log_mailbox_change();

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'company workflows contain durable identity and delivery evidence; restore a coordinated backup'; END $$;
-- +goose StatementEnd
