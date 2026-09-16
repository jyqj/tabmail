-- Draft submission becomes a server-side business transaction: enqueueing an
-- outbound job from a mail draft consumes (deletes) the draft inside the same
-- transaction, so outbound_jobs carries the draft provenance.
--
-- draft_id deliberately has NO foreign key: the referenced mail_drafts row is
-- deleted by the very transaction that creates the job (consume-by-delete), so
-- the column is an intentionally dangling provenance marker. ON DELETE SET NULL
-- would erase the provenance we are recording; a restrictive FK would make the
-- atomic consume impossible.

-- +goose Up
ALTER TABLE outbound_jobs ADD COLUMN draft_id UUID;
CREATE INDEX outbound_jobs_draft ON outbound_jobs(tenant_id,draft_id) WHERE draft_id IS NOT NULL;

-- +goose Down
-- Destructive downgrade would erase draft provenance on submission evidence.
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'draft submission provenance requires a coordinated restore; do not downgrade'; END $$;
-- +goose StatementEnd
