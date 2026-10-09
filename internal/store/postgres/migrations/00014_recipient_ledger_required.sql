-- +goose Up
-- Rollout requires all API/worker writers to be drained together before this
-- migration. Historical bytes, domain evidence and job attempt counters stay
-- intact. Never infer non-acceptance merely from an absent legacy checkpoint.
LOCK TABLE outbound_jobs IN ACCESS EXCLUSIVE MODE;
LOCK TABLE outbound_recipients IN ACCESS EXCLUSIVE MODE;

INSERT INTO outbound_recipients(tenant_id,job_id,address,state,diagnostic,attempts,updated_at)
SELECT j.tenant_id,j.id,r.address,
 CASE
  WHEN COALESCE(lower(substring(r.address from '@([^@]+)$'))=ANY(j.delivered_domains),false)
    OR (j.state='sent' AND j.in_flight_domain='') THEN 'accepted'
  WHEN j.state='processing' OR j.in_flight_domain<>'' OR j.attempts>0 THEN 'uncertain'
  ELSE 'pending'
 END,
 CASE
  WHEN COALESCE(lower(substring(r.address from '@([^@]+)$'))=ANY(j.delivered_domains),false)
    OR (j.state='sent' AND j.in_flight_domain='') THEN 'Legacy job/domain acceptance evidence preserved'
  WHEN j.state='processing' OR j.in_flight_domain<>'' OR j.attempts>0 THEN 'Legacy recipient outcome is unproven; operator review required'
  ELSE 'Migrated before any recorded delivery attempt'
 END,
 -- The legacy job counter is not a per-address attempt count. Do not invent
 -- one: retain the original job counter and start the new ledger counter at 0.
 0,clock_timestamp()
FROM outbound_jobs j CROSS JOIN LATERAL unnest(j.rcpt_to) AS r(address)
WHERE NOT j.recipient_ledger
ON CONFLICT(job_id,address) DO NOTHING;

-- Keep explicit terminal/cancellation evidence. Live/queued ambiguous jobs
-- become held failures; the nonempty marker forbids automatic/manual retry
-- until the existing audited operator reconciliation confirms the outcomes.
UPDATE outbound_jobs j SET
 state=CASE WHEN j.state IN ('failed','dead','cancelled') THEN j.state ELSE 'failed'::outbound_state END,
 in_flight_domain=CASE WHEN j.in_flight_domain<>'' THEN j.in_flight_domain ELSE 'legacy:review-required' END,
 last_error=concat_ws(E'\n',NULLIF(j.last_error,''),'Legacy ledger migration: acceptance unproven or incomplete; inspect recipient evidence before retry'),
 claimed_at=NULL,lease_until=NULL,delivery_token=NULL,updated_at=clock_timestamp()
WHERE NOT j.recipient_ledger AND (
 j.state='processing' OR j.in_flight_domain<>'' OR
 EXISTS(SELECT 1 FROM outbound_recipients r WHERE r.job_id=j.id AND r.state='uncertain') OR
 NOT EXISTS(SELECT 1 FROM outbound_recipients r WHERE r.job_id=j.id));

UPDATE outbound_jobs SET recipient_ledger=true,claimed_at=NULL,lease_until=NULL,delivery_token=NULL,updated_at=clock_timestamp()
WHERE NOT recipient_ledger;
ALTER TABLE outbound_jobs ALTER COLUMN recipient_ledger SET DEFAULT true;
-- An older writer cannot silently create a job that needs the retired path.
ALTER TABLE outbound_jobs ADD CONSTRAINT outbound_jobs_recipient_ledger_required CHECK(recipient_ledger);

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
 RAISE EXCEPTION 'recipient outcome evidence cannot be safely downgraded; restore a coordinated backup with all writers stopped';
END $$;
-- +goose StatementEnd
