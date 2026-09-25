-- +goose Up
-- Original EML remains canonical. Documents/manifests are versioned, rebuildable
-- derived data; decoded attachments are NOT copied into PostgreSQL or a second
-- durable object hierarchy.
CREATE TABLE mail_documents (
 tenant_id UUID NOT NULL, message_id UUID PRIMARY KEY, source_key TEXT NOT NULL,
 source_sha256 TEXT NOT NULL CHECK(length(source_sha256)=64), parser_version INT NOT NULL,
 text_body TEXT NOT NULL, html_body TEXT NOT NULL, body_access TEXT NOT NULL,
 parts JSONB NOT NULL, thread_key TEXT NOT NULL, search_text TEXT NOT NULL,
 indexed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 FOREIGN KEY(tenant_id,message_id) REFERENCES messages(tenant_id,id) ON DELETE CASCADE
);
CREATE INDEX mail_documents_search ON mail_documents USING gin(search_text gin_trgm_ops);
CREATE INDEX mail_documents_thread ON mail_documents(tenant_id,thread_key);
CREATE TABLE mail_index_jobs (
 tenant_id UUID NOT NULL, message_id UUID PRIMARY KEY, source_key TEXT NOT NULL,
 state TEXT NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','processing','ready','failed')),
 attempts INT NOT NULL DEFAULT 0, next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 lease_until TIMESTAMPTZ, lease_token UUID, last_error TEXT NOT NULL DEFAULT '',
 FOREIGN KEY(tenant_id,message_id) REFERENCES messages(tenant_id,id) ON DELETE CASCADE
);
CREATE INDEX mail_index_claim ON mail_index_jobs(state,next_attempt_at);
INSERT INTO mail_index_jobs(tenant_id,message_id,source_key)
 SELECT tenant_id,id,raw_object_key FROM messages WHERE COALESCE(raw_object_key,'')<>'';
-- +goose StatementBegin
CREATE FUNCTION enqueue_mail_index() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='UPDATE' AND NEW.raw_object_key IS NOT DISTINCT FROM OLD.raw_object_key THEN RETURN NEW; END IF;
 DELETE FROM mail_documents WHERE tenant_id=NEW.tenant_id AND message_id=NEW.id;
 IF COALESCE(NEW.raw_object_key,'')='' THEN
  DELETE FROM mail_index_jobs WHERE message_id=NEW.id; RETURN NEW;
 END IF;
 INSERT INTO mail_index_jobs(tenant_id,message_id,source_key) VALUES(NEW.tenant_id,NEW.id,NEW.raw_object_key)
 ON CONFLICT(message_id) DO UPDATE SET source_key=EXCLUDED.source_key,state='pending',attempts=0,next_attempt_at=now(),lease_token=NULL,lease_until=NULL,last_error='';
 RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER enqueue_mail_index AFTER INSERT OR UPDATE OF raw_object_key ON messages FOR EACH ROW EXECUTE FUNCTION enqueue_mail_index();
-- +goose Down
DROP TRIGGER enqueue_mail_index ON messages;
DROP FUNCTION enqueue_mail_index();
DROP TABLE mail_index_jobs;
DROP TABLE mail_documents;
