-- Company- and mailbox-level outbound send policy. The effective policy for a
-- mailbox is COALESCE(mailboxes.send_policy, tenants.mail_send_policy): a
-- mailbox-level override wins, the tenant default ('free') fills the rest.
--   free              — any exact send right may be exercised as-is
--   template_required — every send (owner and admins included) must go through
--                       a published template version
--   disabled          — no mailbox-path sends at all, owner and admins included
-- Storage only: the interpretation lives in authz (EvaluateMailboxAccess /
-- CheckMailboxSender), which read the effective value carried on the Mailbox
-- row by the store's canonical mailbox select.

-- +goose Up
ALTER TABLE tenants ADD COLUMN mail_send_policy TEXT NOT NULL DEFAULT 'free'
	CONSTRAINT tenants_mail_send_policy_check CHECK (mail_send_policy IN ('free','template_required','disabled'));
ALTER TABLE mailboxes ADD COLUMN send_policy TEXT
	CONSTRAINT mailboxes_send_policy_check CHECK (send_policy IN ('free','template_required','disabled'));

-- +goose Down
-- Destructive downgrade would erase company send-policy configuration.
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'send policy configuration requires a coordinated restore; do not downgrade'; END $$;
-- +goose StatementEnd
