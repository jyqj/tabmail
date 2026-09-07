# Durable ingress recovery — independently reviewable increment

Base: `d14f9fd3b7a9368a71c2bd97baf5ab80c72c9d97`, the current head of PR #5.
Branch: `feat/durable-ingress-recovery`; follow-up to Issues #4 and #6.
This change intentionally does **not** republish the earlier, safety-blocked
frontend-alignment/outbound patches, or change their workflow files. Those local
bundles remain separate work. No deployment, DNS edit, main merge or external mail.

## What changes

Durable SMTP acceptance requires an `IngressLedger`. Adapters without it reject
DATA rather than silently using the old nontransactional fake-store path.
Before returning success, the service resolves/provisions each canonical mailbox,
freezes mailbox/tenant/zone IDs, writes a unique receipt object, and commits the
receipt plus its target rows. Repeated envelope addresses targeting one mailbox
produce one local delivery; separate DATA submissions remain separate receipts.

Workers claim one receipt at a time using database time and a fresh fencing token.
A one-minute processing timeout is shorter than the five-minute claim lease.
Mailbox insertion, both quota updates, target completion, audit and outbox insertion
share a PostgreSQL transaction. A committed mailbox is not repeated after another
mailbox fails, a worker crashes or the commit acknowledgement is lost. Progress
survives deletion of the user's message until the completed receipt is retired.

Raw bytes are not content-deduplicated across durable receipts. Each receipt has
`ingress-<uuid>.eml`, its expected length and SHA-256. Workers bound reads by the
accepted length and reject checksum mismatches. This deliberately trades storage
for eliminating the new-ingress shared-object/reclamation race. The old synchronous
content-addressed path and its broader cleanup debt have **not** been rewritten.
The filesystem adapter now writes to a private temporary file, fsyncs it, atomically
renames it, and syncs the affected directories. A failed write cannot truncate the
previous object. Actual durability still depends on the filesystem/storage contract.

## State and retry semantics

| Target state | Meaning |
| --- | --- |
| pending | Not committed; automatic retry is still allowed |
| delivered | Message/progress/audit/outbox committed atomically; never replay |
| held | Retry budget exhausted; original bytes retained for operator review |

The existing aggregate job states remain compatible with the current UI:
`retry` means some destinations remain; `dead` means review is needed; `done`
requires all target rows to be delivered. Failure reasons are stored per target.
Deleted/recreated addresses never redirect old mail: a changed destination is held.
Post-accept policy, size, quota, configuration and storage errors retain the receipt;
none are interpreted as permission to delete already acknowledged DATA.

Quotas use UTC delivery days and a transactional persistent tenant counter, seeded
from existing messages on first use. Retries of committed targets consume no quota;
failed transactions consume none. Deleting mail does not refund the new counter.
The legacy Redis counter is not used for durable receipts. Do not mix old writers
with the new workers when relying on this quota boundary.

Held and pending receipts pin their raw objects. Automatic seven-day job cleanup
only removes completed receipts, not dead letters. Completed recovery receipts also
pin their objects until job cleanup; surviving messages continue to pin them after
that. Lost/ambiguous acceptance commits leave an orphan rather than risking deletion
of committed mail. **Capacity monitoring and reviewed orphan/dead-letter disposal
are operational requirements; no automatic orphan sweep is provided here.**

## Operator endpoints

The served `/openapi.yaml` includes the new paths (one complete spec assembled from
an independently reviewed path fragment). Both endpoints require the same platform
super-admin interactive session as the existing ingest-job list. A receipt may span
multiple tenants; company admins, ordinary employees and API keys are denied.

`GET /api/v1/admin/ingest/jobs/{id}/recipients` returns `data: IngressTarget[]`, with
no-store caching. The IDs are frozen destination snapshots, not permission grants.
A missing or legacy receipt without a ledger returns 404.

`POST /api/v1/admin/ingest/jobs/{id}/retry` takes `{"reason":"storage restored"}`.
The reason is required and at most 2000 characters (request body at most 16 KiB).
Only held ledger receipts can be requeued. The mutation and audit commit together;
audit failure rolls everything back. No content, recipient or mailbox reassignment
is accepted. Successful targets are untouched. The follow-up operator console is documented in `INGRESS-OPERATIONS.md`; employee UI surfaces and their privileges are unchanged.

## Migration and deployment boundary

Versions 1 and 2 remain byte-for-byte immutable. `0004_ingress_recovery.sql` reserves
version 3 for the separately reviewed but unpublished outbound increment. The
registered set in **this** binary is exactly `{1,2,4}`; unknown versions (including
an independently installed version 3) and checksum mismatches reject startup. A
combined binary must explicitly register and review both 0003 and 0004; do not run
this standalone binary against the cumulative unpublished outbound database.

Retained legacy pending/retry/processing jobs have no reliable per-mailbox progress
and are moved to `dead` for review, not silently replayed. Legacy dead-letter bytes
are now pinned too. The retry API refuses to invent a ledger for historical jobs.
Already purged historical bytes cannot be recovered by this migration.

Before upgrading, back up and test restoration; quiesce SMTP acceptance, old workers,
retention, and writes. Deploy consistent binaries to all process roles, migrate,
verify the receipt count and raw objects, then resume. Keep `TABMAIL_INGEST_DURABLE=true`.
The legacy synchronous mode is still available for compatibility but is not covered
by the recovery guarantee. No rolling mixed-version or automatic downgrade claim.

## Verification

Tests use disposable PostgreSQL 16 databases and loopback SMTP only. Coverage:
partial recipient insert failure and retry; audit rollback; persistent quota under
parallel workers; claim replacement; deleted-message tombstones; destination address
reuse; object loss/corruption; missing-ledger adapter rejection; legacy finalizer
isolation; held-byte retention and audited retry; duplicate addresses vs separate
submissions; migration adoption/restart/unknown version/checksum rejection; real
SMTP 250 after spool+receipt commit and 451 on database failure; interrupted/cancelled
filesystem writes; full Router authorization and the combined OpenAPI document.

Final run evidence and exact delivered SHA belong in the PR, not inferred from an
older green check. The earlier tests that expected zero-delivery and oversized
accepted mail to be deleted were replaced by recovery/retention tests, not retained
as product requirements. Real-browser, public deliverability and backup drills are
not claimed by local unit/integration tests.

## Remaining release gates

Issue #6 stays open: the separate outbound increment is not on this remote base;
SMTP final-ack uncertainty across distinct submissions is not exactly-once delivery.
Automated DSN/bounce generation, bounded quarantine/orphan disposal,
legacy object-reference coordination and global audit/outbox coverage remain.
Department managers, approvals, drafts/replies/attachments, SSO/MFA and cookie
sessions remain tracked in Issue #4. This is not a production-readiness declaration.

## Design references

RFC 5321, section 6.1: a positive DATA response transfers responsibility for delivery
or relay; ordinary post-accept storage errors must not silently discard the message.
https://www.rfc-editor.org/rfc/rfc5321#section-6.1

PostgreSQL 16 explicit locking: transaction-scoped locks and row locks protect the
short local commit boundary; leases and fencing additionally reject stale workers.
https://www.postgresql.org/docs/16/explicit-locking.html
