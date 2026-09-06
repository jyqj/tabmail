-- Company mode is opt-in. Existing tenants and mailbox grants are not inferred.
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_tenant_id_id ON users(tenant_id,id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_zones_tenant_id_id ON domain_zones(tenant_id,id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_mailboxes_tenant_id_id ON mailboxes(tenant_id,id);

CREATE TABLE company_settings (
 tenant_id UUID PRIMARY KEY REFERENCES tenants(id) ON DELETE CASCADE,
 primary_zone_id UUID NOT NULL,
 name TEXT NOT NULL,
 owner_id UUID NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 FOREIGN KEY (tenant_id,primary_zone_id) REFERENCES domain_zones(tenant_id,id),
 FOREIGN KEY (tenant_id,owner_id) REFERENCES users(tenant_id,id)
);
CREATE TABLE company_members (
 tenant_id UUID NOT NULL REFERENCES company_settings(tenant_id) ON DELETE CASCADE,
 user_id UUID NOT NULL,
 company_role TEXT NOT NULL CHECK(company_role IN ('admin','employee','restricted','viewer')),
 daily_send_quota INT NOT NULL CHECK(daily_send_quota BETWEEN 1 AND 10000),
 PRIMARY KEY(tenant_id,user_id),
 FOREIGN KEY(tenant_id,user_id) REFERENCES users(tenant_id,id) ON DELETE CASCADE
);
CREATE TABLE company_mailbox_grants (
 tenant_id UUID NOT NULL,
 mailbox_id UUID NOT NULL,
 user_id UUID NOT NULL,
 can_read BOOLEAN NOT NULL DEFAULT FALSE,
 can_organize BOOLEAN NOT NULL DEFAULT FALSE,
 can_send BOOLEAN NOT NULL DEFAULT FALSE,
 template_only BOOLEAN NOT NULL DEFAULT FALSE,
 PRIMARY KEY(tenant_id,mailbox_id,user_id),
 CHECK(NOT can_organize OR can_read),
 FOREIGN KEY(tenant_id,mailbox_id) REFERENCES mailboxes(tenant_id,id) ON DELETE CASCADE,
 FOREIGN KEY(tenant_id,user_id) REFERENCES company_members(tenant_id,user_id) ON DELETE CASCADE
);
CREATE INDEX company_grants_user ON company_mailbox_grants(tenant_id,user_id);
CREATE TABLE company_invitations (
 token_hash TEXT PRIMARY KEY,
 tenant_id UUID NOT NULL,
 user_id UUID NOT NULL,
 expires_at TIMESTAMPTZ NOT NULL,
 consumed_at TIMESTAMPTZ,
 FOREIGN KEY(tenant_id,user_id) REFERENCES company_members(tenant_id,user_id) ON DELETE CASCADE
);
CREATE TABLE company_templates (
 id UUID PRIMARY KEY,
 tenant_id UUID NOT NULL REFERENCES company_settings(tenant_id),
 family_id UUID NOT NULL,
 version INT NOT NULL CHECK(version>0),
 name TEXT NOT NULL,
 subject TEXT NOT NULL,
 text_body TEXT NOT NULL,
 html_body TEXT NOT NULL,
 variables JSONB NOT NULL,
 status TEXT NOT NULL DEFAULT 'draft' CHECK(status IN ('draft','published','retired')),
 created_by UUID NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 UNIQUE(tenant_id,id), UNIQUE(tenant_id,family_id,version),
 FOREIGN KEY(tenant_id,created_by) REFERENCES company_members(tenant_id,user_id)
);
CREATE TABLE company_template_mailboxes (
 tenant_id UUID NOT NULL,
 template_id UUID NOT NULL,
 mailbox_id UUID NOT NULL,
 PRIMARY KEY(tenant_id,template_id,mailbox_id),
 FOREIGN KEY(tenant_id,template_id) REFERENCES company_templates(tenant_id,id) ON DELETE CASCADE,
 FOREIGN KEY(tenant_id,mailbox_id) REFERENCES mailboxes(tenant_id,id)
);
CREATE UNIQUE INDEX idx_outbound_tenant_id_id ON outbound_jobs(tenant_id,id);
CREATE TABLE company_submissions (
 job_id UUID PRIMARY KEY,
 tenant_id UUID NOT NULL REFERENCES company_settings(tenant_id),
 mailbox_id UUID NOT NULL,
 template_id UUID,
 content_hash TEXT NOT NULL,
 FOREIGN KEY(tenant_id,job_id) REFERENCES outbound_jobs(tenant_id,id) ON DELETE CASCADE,
 FOREIGN KEY(tenant_id,mailbox_id) REFERENCES mailboxes(tenant_id,id),
 FOREIGN KEY(tenant_id,template_id) REFERENCES company_templates(tenant_id,id)
);
-- A separate flag avoids reinterpreting the legacy expires_at or 0-hour semantics.
ALTER TABLE messages ADD COLUMN retention_exempt BOOLEAN NOT NULL DEFAULT FALSE;
CREATE FUNCTION protect_company_message() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
 IF EXISTS (SELECT 1 FROM company_settings WHERE tenant_id=NEW.tenant_id) THEN
  NEW.retention_exempt := TRUE;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER company_message_retention BEFORE INSERT ON messages
 FOR EACH ROW EXECUTE FUNCTION protect_company_message();
