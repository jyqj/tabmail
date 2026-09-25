# R4: company-mail architecture

Base: PR #14 at `5a99281940e54a44ad6a02612c0fd33aaac01d1d`.

## Data ownership

`sent_mail_assets` stores immutable sent content; `sent_mail_items` owns folder, revision and retention; `sent_asset_attachments` pins verified attachments independently of the queue. The existing enqueue transaction archives content and pins together, including draft consumption. IDs retain submission provenance without a queue foreign key. Backfill is performed by migration 00010. Deleting a delivery job never deletes its sent asset. Trash is 30 days; personal sent items have no expiry, shared items inherit their explicitly configured mailbox retention at creation. Assets needed by an existing delivery job remain pinned even after their mailbox item expires.

`mail_documents` and leased `mail_index_jobs` are derived, rebuildable inbound content. Workers parse bounded immutable EML and populate body search and RFC-reference conversation hints. New attachment IDs bind message, raw digest, ordinal and part digest; legacy ordinal URLs remain compatible. Search is literal, permission-scoped, and explicitly reports index progress; substring trigram search is not semantic search or linguistic ranking. Conversations are hints scoped to one mailbox, not proof of common authorship. Parser input/cache are bounded and cached reads never replace current authorization. EML remains the source of truth. Expired workers cannot report either success or failure with a stale lease; five repeatedly expired leases move a job to failed. Company administrators can retry up to 100 failed jobs per action with a documented reason; current authority, tenant scope, retry, audit and outbox are atomic. Recovery never resends mail or grants body access.

## Lifecycle and revisions

Employee offboarding requires a 15-minute, actor-owned plan. Preview lists counts, never private payloads. Execution locks the company, employee and queue evidence and compares IDs/revisions, not only counts. It seals private drafts by default, optionally transfers drafts in owned mailboxes, or explicitly discards active drafts. It cancels unstarted safe queue entries, preserves processing/uncertain evidence, revokes sessions/keys/grants and transfers mailbox ownership atomically. Replaying the same completed plan returns its receipt. A database sender fence rejects late enqueues from inactive employees.

The one-step HTTP offboard command is replaced by preview + `{plan_id}`. The old store method is a compatibility adapter using the same disposition transaction, not a second public workflow. Sealed drafts stay private; this does not add an administrative unseal/export capability.

Drafts have ACL-before-pagination, single-item current-authority reads, and creation receipts. Clients provide a stable UUID for first save; exact retry replays revision 1, while consumed/deleted IDs remain tombstoned. Autosave, explicit save and submit share a serial revision lane. Late save responses do not overwrite typed text. Unknown writes pin their original UUID, revision and payload even when readback is also unavailable; later typing is flushed only after the original command is acknowledged. Readback accepts only the exact identity, input and next revision; it cannot silently adopt someone else's later revision. Conflicts require explicit reload or a new private draft copy; no automatic overwrite or resend.

## Application boundaries

Feature services: drafts, employees, companymail, mailarchive, mailindex, templates, submissions, recovery. Transport composition receives narrow ports and does not pass HTTP handlers into business logic. Existing atomic database commands and recipient ledgers are retained. Company administrative audits and coarse change events use the existing transactional outbox; event payloads contain no private bodies, template variables or activation links. Legacy platform/domain commands not using companyAudit retain their pre-existing event behavior.

## Frontend

Mail workspace components separate incoming mail, mailbox-owned sent assets, personal drafts and cross-mailbox delivery receipts. Company routes separate overview, domains, employees, mailbox administration, access/profile administration, templates and company-scoped audit. Permission explanations reuse the canonical mailbox authority and are display-only. Optional constrained rich text strips active markup and remote media; paste is text-only and HTML drops are blocked. Existing sandboxed received-mail rendering and attachment authorization remain authoritative.

## Upgrade and compatibility

Apply 00009 (PR14) then new migrations 00010–00013. Requires PostgreSQL pg_trgm availability and coordinated database/object backup. Stop old API/SMTP/worker/retention writers; migrate and upgrade all roles and frontend together before restoring traffic. Backfill and index creation can lock large tables: rehearse on a production-sized restored copy and budget the maintenance window. Do not edit historical migrations or run destructive Down.

Draft GET is now paginated (`data`, `meta`); offboard execution requires a plan ID; grant/policy revisions remain mandatory from PR14. Existing submission-content URLs now read sent assets with CURRENT mailbox read authority. Delivery receipts still use jobs and may legitimately disappear after queue-history cleanup. No guarantee of SMTP end-to-end exactly-once is introduced.

New data increases database footprint: monitor document and archive size. Creation tombstones and sealed drafts deliberately have no automatic deletion policy in this release. No already-delivered browser bytes can be revoked; refreshing rights limits further access.

## Dependency verification

Next.js and eslint-config-next are pinned to 16.3.3; Vitest is pinned to 4.1.11.
Compatible transitive security fixes are resolved in the committed lockfile,
including sharp 0.35.4. The dependency repair uses an isolated npm 11.6.4 to
avoid an observed npm 10.9.8 lockfile resolver crash; normal builds still use
`npm ci`, never re-resolve dependencies. The complete npm audit report is saved
in CI and any reported vulnerability or invalid audit response blocks the
frontend job. A zero report is not a proof that all application vulnerabilities
or future advisories are absent. Public advisory sources include:
- https://github.com/vercel/next.js/security/advisories/GHSA-p293-qw3h-jr36
- https://github.com/vercel/next.js/security/advisories/GHSA-2xp9-vwfh-vxw4
- https://github.com/lovell/sharp/security/advisories/GHSA-rgj7-g3m4-5g8c
- https://github.com/vitest-dev/vitest/security/advisories/GHSA-82fw-gwwq-j7x9

## Validation and scope

Exact-commit CI and regression results belong in PR #15 after execution. The browser workflow now builds and runs the shipping standalone Docker image against real Go API/PostgreSQL and loopback SMTP. Tests cover queue-independent assets, explicit purge, privacy, creation replay, ACL pagination, index leases, offboarding fingerprints/disposition, autosave serialization and editor sanitization.

This is not a replacement SMTP transport, external mail gateway, IdP/SSO integration, arbitrary alias/address-migration service, malware scanner, or production rollout. Public deliverability, disaster-recovery objectives, migration load and external gateway configuration still require operator validation. Domain deletion protection remains; domain/address decommissioning is not equivalent to deleting mailboxes.
