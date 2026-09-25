# Company mail safety and interaction contracts

Baseline: `fc111874efb835bd98215f1841137736c92a9cce`.

## Domain assets

Domain deletion is not mailbox deletion. The store locks the company and domain,
checks primary-domain references, mailboxes, messages, outbound jobs, durable
ingress targets, child zones and active invitations, and rejects used domains.
Tenant scope and the mandatory deletion audit are in the same transaction.
Migration `00009_domain_asset_guard.sql` replaces the mailbox/zone cascading
foreign key with NO ACTION. Concurrent mailbox insertion and direct SQL deletion
cannot cascade-delete company mailboxes. An unused domain can still be deleted.

Upgrade every API writer together; apply the new migration before admitting
traffic. No historical migration is edited. The new migration deliberately
refuses Down: restoring a destructive cascade is not a safe rollback. Back up
PostgreSQL and objects using the existing coordinated procedure. This change
neither deletes nor migrates existing messages. Domain deactivation, aliases and
full address migration are not introduced in this round. Existing post-commit
event publication remains separate; this is not a transactional outbox rewrite.

## Administrative concurrency (HTTP compatibility change)

`GET /company/mailboxes/{id}/grants` returns `{revision, grants}` in `data`, not
an array. The version and grants are read under one company administration lock.
Both grants PUT and send-policy PUT require a positive `revision` in the JSON
body. Missing versions return 400; stale versions return 409. Neither endpoint
accepts an unversioned fallback. They claim the shared mailbox version in the
same transaction as the mutation and audit; failed writes roll back the claim.

Upgrade clients and API together. Grant editors keep the version originally
selected and refuse to silently rebase a stale form. Conflicts require explicit
reload, member selection and review. Policy editors require explicit review of
the latest version. The OpenAPI contract and frontend DTO checks cover this
change. Undefined UUID/string YAML aliases left by the prior spec are replaced
with explicit schemas so the document parses again. Existing owner hierarchy and role restrictions remain intact.

## Templates and composing

The usable-template picker uses the send path's eligibility classifier, including
current usage grants and snapshot integrity, with no admin send-usage bypass.
Template management rights are not template-use rights. Pinned drafts use their
embedded usable version snapshot even after a newer version is published. The
latest-version picker is only for selecting a new pin. Revoked, retired,
unauthorized or corrupt versions stay blocked and can be unpinned. Missing IDs
retain the existing editable draft behavior; actual send remains authoritative.

Compose button gating, opening and reply/forward identity selection share one
selector. A current sendable mailbox wins; otherwise free-writing senders are
preferred, with template-only senders as the final valid fallback. Every server
operation still reauthorizes. The company domain picker and onboarding component
now use one session-scoped query, so verification refreshes both.

## Current content access

Revoked submission capabilities unmount the content disclosure and evict its
session-scoped body and attachment caches. Regranting never reopens the old
expanded view automatically. Denied revalidation hides stale attachment metadata.
Existing server content authorization is unchanged. This cannot revoke bytes
already delivered to the browser; requests already in flight are not physically
cancelled by this UI change. Cache mutation suppresses stale revalidation writes.

## Regression coverage

Added real-PostgreSQL tests for former-primary asset protection, direct SQL and
concurrent deletes, tenant isolation, audit rollback, durable ingress provenance,
stale grant replay, concurrent policy/grant CAS, required HTTP revisions, admin
usage grants and pinned version continuity. Existing tests now observe explicit
versions before administrative writes; production has no test-only bypass.

Frontend regressions cover old pinned template editing/preview/submit, template-only
identity fallback, revocation/cache eviction and collapsed regrant, denied
attachment refresh, shared domain verification, and stale grant/policy review.
The lightweight DTO checker now also checks grant snapshots, pinned template
eligibility and submission capabilities; it is not a complete schema validator.

Exact-commit validation results are recorded in PR #14 and its Actions artifacts.
Local formatting, syntax and field checks are not substitutes for CI.

## Deliberately unchanged

Atomic draft submission, per-recipient delivery ledgers, ingress recovery and
current content authorization remain authoritative. Independent sent-message
assets, retention redesign, full offboarding plans, rich-text/autosave, dependency
upgrades and production/public-SMTP validation are separate work. This change does
not claim to resolve the previously observed npm security findings. No main
merge, production database command, DNS change, deployment or external email test
is part of this PR.
