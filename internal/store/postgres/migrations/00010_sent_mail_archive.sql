-- +goose Up
-- A sent mailbox item is an employee asset, not a delivery job. IDs keep
-- submission provenance but deliberately have NO FK back to outbound_jobs.
CREATE EXTENSION IF NOT EXISTS pg_trgm;
CREATE TABLE sent_mail_assets (
 id UUID PRIMARY KEY, tenant_id UUID NOT NULL, sender_mailbox_id UUID NOT NULL,
 zone_id UUID NOT NULL, mail_from TEXT NOT NULL, to_addrs TEXT[] NOT NULL,
 cc_addrs TEXT[] NOT NULL, subject TEXT NOT NULL, text_body TEXT NOT NULL,
 html_body TEXT NOT NULL, headers_json JSONB, template_version_id UUID,
 created_at TIMESTAMPTZ NOT NULL, search_text TEXT NOT NULL,
 UNIQUE(tenant_id,id),
 FOREIGN KEY(tenant_id,sender_mailbox_id) REFERENCES mailboxes(tenant_id,id),
 FOREIGN KEY(tenant_id,zone_id) REFERENCES domain_zones(tenant_id,id)
);
CREATE INDEX sent_assets_search ON sent_mail_assets USING gin(search_text gin_trgm_ops);
CREATE TABLE sent_mail_items (
 tenant_id UUID NOT NULL, asset_id UUID PRIMARY KEY, mailbox_id UUID NOT NULL,
 archived_at TIMESTAMPTZ, deleted_at TIMESTAMPTZ, purge_after TIMESTAMPTZ, expires_at TIMESTAMPTZ,
 revision BIGINT NOT NULL DEFAULT 1, created_at TIMESTAMPTZ NOT NULL,
 CHECK((deleted_at IS NULL)=(purge_after IS NULL)),
 FOREIGN KEY(tenant_id,asset_id) REFERENCES sent_mail_assets(tenant_id,id),
 FOREIGN KEY(tenant_id,mailbox_id) REFERENCES mailboxes(tenant_id,id)
);
CREATE INDEX sent_items_mailbox ON sent_mail_items(tenant_id,mailbox_id,created_at DESC,asset_id DESC);
CREATE INDEX sent_items_expiry ON sent_mail_items(expires_at) WHERE expires_at IS NOT NULL;
CREATE INDEX sent_items_purge ON sent_mail_items(purge_after) WHERE purge_after IS NOT NULL;
CREATE TABLE sent_asset_attachments (
 tenant_id UUID NOT NULL, asset_id UUID NOT NULL, attachment_id UUID NOT NULL,
 PRIMARY KEY(asset_id,attachment_id),
 FOREIGN KEY(tenant_id,asset_id) REFERENCES sent_mail_assets(tenant_id,id) ON DELETE CASCADE,
 FOREIGN KEY(tenant_id,attachment_id) REFERENCES mail_attachments(tenant_id,id)
);
CREATE INDEX sent_asset_attachment_references ON sent_asset_attachments(attachment_id);

INSERT INTO sent_mail_assets(id,tenant_id,sender_mailbox_id,zone_id,mail_from,to_addrs,cc_addrs,subject,text_body,html_body,headers_json,template_version_id,created_at,search_text)
 SELECT j.id,j.tenant_id,j.sender_mailbox_id,j.zone_id,j.mail_from,COALESCE(j.to_addrs,'{}'),COALESCE(j.cc_addrs,'{}'),j.subject,COALESCE(j.text_body,''),COALESCE(j.html_body,''),j.headers_json,j.template_version_id,j.created_at,
 concat_ws(E'\n',j.subject,j.mail_from,array_to_string(j.to_addrs,','),array_to_string(j.cc_addrs,','),j.text_body)
 FROM outbound_jobs j JOIN mailboxes m ON m.id=j.sender_mailbox_id AND m.tenant_id=j.tenant_id
 JOIN domain_zones z ON z.id=j.zone_id AND z.tenant_id=j.tenant_id;
INSERT INTO sent_mail_items(tenant_id,asset_id,mailbox_id,created_at,expires_at)
 SELECT a.tenant_id,a.id,a.sender_mailbox_id,a.created_at,
 CASE WHEN m.mailbox_kind='shared' AND m.retention_hours_override>0 THEN a.created_at+make_interval(hours=>m.retention_hours_override) ELSE NULL END
 FROM sent_mail_assets a JOIN mailboxes m ON m.tenant_id=a.tenant_id AND m.id=a.sender_mailbox_id;
INSERT INTO sent_asset_attachments(tenant_id,asset_id,attachment_id)
 SELECT x.tenant_id,x.job_id,x.attachment_id FROM outbound_attachments x JOIN sent_mail_assets a ON a.tenant_id=x.tenant_id AND a.id=x.job_id;

-- The existing enqueue transaction includes these triggers. Draft consumption,
-- quota reservation, immutable content and attachment pins commit together.
-- This also covers adapters using CreateOutboundJob rather than draft submit.
-- +goose StatementBegin
CREATE FUNCTION archive_outbound_mail() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.sender_mailbox_id IS NULL THEN RETURN NEW; END IF;
 -- Out-of-scope legacy fixture/provenance never becomes a readable asset.
 IF NOT EXISTS(SELECT 1 FROM mailboxes m JOIN domain_zones z ON z.id=NEW.zone_id AND z.tenant_id=NEW.tenant_id
     WHERE m.id=NEW.sender_mailbox_id AND m.tenant_id=NEW.tenant_id) THEN RETURN NEW; END IF;
 INSERT INTO sent_mail_assets(id,tenant_id,sender_mailbox_id,zone_id,mail_from,to_addrs,cc_addrs,subject,text_body,html_body,headers_json,template_version_id,created_at,search_text)
 VALUES(NEW.id,NEW.tenant_id,NEW.sender_mailbox_id,NEW.zone_id,NEW.mail_from,COALESCE(NEW.to_addrs,'{}'),COALESCE(NEW.cc_addrs,'{}'),NEW.subject,COALESCE(NEW.text_body,''),COALESCE(NEW.html_body,''),NEW.headers_json,NEW.template_version_id,NEW.created_at,
 concat_ws(E'\n',NEW.subject,NEW.mail_from,array_to_string(NEW.to_addrs,','),array_to_string(NEW.cc_addrs,','),NEW.text_body));
 INSERT INTO sent_mail_items(tenant_id,asset_id,mailbox_id,created_at,expires_at)
 SELECT NEW.tenant_id,NEW.id,NEW.sender_mailbox_id,NEW.created_at,
 CASE WHEN m.mailbox_kind='shared' AND m.retention_hours_override>0 THEN NEW.created_at+make_interval(hours=>m.retention_hours_override) ELSE NULL END
 FROM mailboxes m WHERE m.id=NEW.sender_mailbox_id AND m.tenant_id=NEW.tenant_id;
 INSERT INTO mailbox_event_log(tenant_id,mailbox_id,event_type,message_id) VALUES(NEW.tenant_id,NEW.sender_mailbox_id,'sent.created',NEW.id);
 RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER archive_outbound_mail AFTER INSERT ON outbound_jobs FOR EACH ROW EXECUTE FUNCTION archive_outbound_mail();
-- +goose StatementBegin
CREATE FUNCTION pin_sent_attachment() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
 INSERT INTO sent_asset_attachments(tenant_id,asset_id,attachment_id)
 SELECT NEW.tenant_id,NEW.job_id,NEW.attachment_id WHERE EXISTS(SELECT 1 FROM sent_mail_assets WHERE tenant_id=NEW.tenant_id AND id=NEW.job_id)
 ON CONFLICT DO NOTHING;
 RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER pin_sent_attachment AFTER INSERT ON outbound_attachments FOR EACH ROW EXECUTE FUNCTION pin_sent_attachment();
-- +goose StatementBegin
CREATE FUNCTION protect_sent_asset() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
 IF NEW IS DISTINCT FROM OLD THEN RAISE EXCEPTION 'sent mail content is immutable'; END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER protect_sent_asset BEFORE UPDATE ON sent_mail_assets FOR EACH ROW EXECUTE FUNCTION protect_sent_asset();

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'sent assets outlive delivery jobs; use a coordinated restore'; END $$;
-- +goose StatementEnd
