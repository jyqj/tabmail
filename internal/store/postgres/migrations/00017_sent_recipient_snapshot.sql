-- +goose Up
-- Missing structured BCC is UNKNOWN, never known-empty. Existing immutable
-- assets retain their original columns and receive only this additive marker.
ALTER TABLE sent_mail_assets
 ADD COLUMN bcc_addrs TEXT[],
 ADD COLUMN recipient_completeness TEXT NOT NULL DEFAULT 'legacy_unknown',
 ADD COLUMN recipient_snapshot_version SMALLINT NOT NULL DEFAULT 0,
 ADD CONSTRAINT sent_recipient_snapshot_shape CHECK(
  (recipient_completeness='legacy_unknown' AND bcc_addrs IS NULL AND recipient_snapshot_version=0)
  OR (recipient_completeness='complete' AND bcc_addrs IS NOT NULL AND recipient_snapshot_version=1)
 );

CREATE INDEX sent_recipient_unknown_v1 ON sent_mail_assets(tenant_id,id)
 WHERE recipient_completeness='legacy_unknown';

-- The ONLY admitted old source is a still-present structured job with exact
-- original provenance and every immutable archive field. RcptTo, ledger state,
-- current mailbox address/owner and delivery state are deliberately not inputs.
-- +goose StatementBegin
CREATE FUNCTION sent_recipient_source_matches_v1(a sent_mail_assets,j outbound_jobs)
RETURNS BOOLEAN LANGUAGE sql IMMUTABLE AS $$
 SELECT a.tenant_id=j.tenant_id AND a.id=j.id
  AND a.sender_mailbox_id=j.sender_mailbox_id AND a.zone_id=j.zone_id
  AND a.mail_from=j.mail_from
  AND a.to_addrs=COALESCE(j.to_addrs,'{}'::text[])
  AND a.cc_addrs=COALESCE(j.cc_addrs,'{}'::text[])
  AND a.subject=j.subject
  AND a.text_body=COALESCE(j.text_body,'')
  AND a.html_body=COALESCE(j.html_body,'')
  AND a.headers_json IS NOT DISTINCT FROM j.headers_json
  AND a.template_version_id IS NOT DISTINCT FROM j.template_version_id
  AND a.created_at=j.created_at
  AND a.search_text=concat_ws(E'\n',j.subject,j.mail_from,array_to_string(j.to_addrs,','),array_to_string(j.cc_addrs,','),j.text_body)
  AND j.bcc_addrs IS NOT NULL
$$;
-- +goose StatementEnd

-- This runs INSIDE 00010's existing archive/enqueue transaction. Its outer
-- outbound INSERT is visible here; any capture/check failure aborts enqueue,
-- draft consumption, reservation, content, item, pins and event together.
-- +goose StatementBegin
CREATE FUNCTION capture_sent_recipient_snapshot_v1() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE recipients TEXT[];
BEGIN
 NEW.bcc_addrs := NULL;
 NEW.recipient_completeness := 'legacy_unknown';
 NEW.recipient_snapshot_version := 0;
 SELECT j.bcc_addrs INTO recipients FROM outbound_jobs j
  WHERE j.tenant_id=NEW.tenant_id AND j.id=NEW.id
   AND sent_recipient_source_matches_v1(NEW,j);
 IF FOUND THEN
  NEW.bcc_addrs := recipients;
  NEW.recipient_completeness := 'complete';
  NEW.recipient_snapshot_version := 1;
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER capture_sent_recipient_snapshot_v1 BEFORE INSERT ON sent_mail_assets
 FOR EACH ROW EXECUTE FUNCTION capture_sent_recipient_snapshot_v1();

-- No previously immutable field is writable. The one extension is strictly
-- UNKNOWN -> COMPLETE, with reliable source and exact original-row equality.
-- Comparing the whole composite minus the three new fields also protects any
-- future archive columns by default. COMPLETE -> UNKNOWN/changed BCC is denied.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION protect_sent_asset() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
 IF NEW IS NOT DISTINCT FROM OLD THEN RETURN NEW; END IF;
 IF OLD.recipient_completeness='legacy_unknown' AND OLD.bcc_addrs IS NULL AND OLD.recipient_snapshot_version=0
  AND NEW.recipient_completeness='complete' AND NEW.bcc_addrs IS NOT NULL AND NEW.recipient_snapshot_version=1
  AND (to_jsonb(NEW)-ARRAY['bcc_addrs','recipient_completeness','recipient_snapshot_version'])
    =(to_jsonb(OLD)-ARRAY['bcc_addrs','recipient_completeness','recipient_snapshot_version'])
  AND EXISTS(SELECT 1 FROM outbound_jobs j WHERE j.tenant_id=OLD.tenant_id AND j.id=OLD.id
   AND sent_recipient_source_matches_v1(OLD,j)
   AND NEW.bcc_addrs IS NOT DISTINCT FROM j.bcc_addrs)
 THEN RETURN NEW; END IF;
 RAISE EXCEPTION 'sent mail content is immutable';
END $$;
-- +goose StatementEnd

-- Historical recovery is intentionally NOT an unbounded migration UPDATE.
-- Run the explicit tenant-bounded BackfillSentRecipientSnapshotsV1 command
-- with persisted keyset cursor before job cleanup. Reads never write snapshots.

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'sent recipient snapshots outlive delivery jobs; use a coordinated restore'; END $$;
-- +goose StatementEnd
