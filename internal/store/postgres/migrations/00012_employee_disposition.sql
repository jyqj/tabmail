-- +goose Up
ALTER TYPE outbound_state ADD VALUE IF NOT EXISTS 'cancelled';
ALTER TABLE mail_drafts ADD COLUMN sealed_at TIMESTAMPTZ;
CREATE TABLE employee_offboarding_plans(
 id UUID PRIMARY KEY,tenant_id UUID NOT NULL REFERENCES tenants(id),created_by UUID NOT NULL,
 target_id UUID NOT NULL,successor_id UUID NOT NULL,options JSONB NOT NULL,
 impact JSONB NOT NULL,fingerprint TEXT NOT NULL,reason TEXT NOT NULL,
 state TEXT NOT NULL DEFAULT 'preview' CHECK(state IN ('preview','executed')),
 expires_at TIMESTAMPTZ NOT NULL,created_at TIMESTAMPTZ NOT NULL DEFAULT now(),executed_at TIMESTAMPTZ,
 FOREIGN KEY(tenant_id,target_id) REFERENCES users(tenant_id,id),
 FOREIGN KEY(tenant_id,successor_id) REFERENCES users(tenant_id,id)
);
CREATE INDEX offboarding_plan_company ON employee_offboarding_plans(tenant_id,created_at DESC);
-- A company enqueue cannot slip in after offboarding's user row lock. Legacy
-- jobs without an employee principal retain their existing transport behavior.
-- +goose StatementBegin
CREATE FUNCTION fence_employee_enqueue() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE enabled BOOLEAN;
BEGIN
 IF NEW.sender_user_id IS NOT NULL AND NEW.sender_mailbox_id IS NOT NULL THEN
  SELECT is_active INTO enabled FROM users WHERE tenant_id=NEW.tenant_id AND id=NEW.sender_user_id FOR SHARE;
  IF enabled IS DISTINCT FROM TRUE THEN RAISE EXCEPTION 'employee sender is inactive' USING ERRCODE='42501'; END IF;
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER fence_employee_enqueue BEFORE INSERT ON outbound_jobs FOR EACH ROW EXECUTE FUNCTION fence_employee_enqueue();
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'offboarding preserves sealed drafts and disposition evidence; use a coordinated restore'; END $$;
-- +goose StatementEnd
