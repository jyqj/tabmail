# Offboarding successor authorization — 2026-10-04

Fixed source base: `f875672dea2b68fdec9e0b986f4863724da5d7ba`. UI repair and the full LF01 report were inspected at `648938a5baad2f35ba120da2cf5ae1001f8f3b4a`. This is a separate backend fix and dedicated regression run. Aggregate evidence and exact source/test SHA256 values: [OFFBOARDING-SUCCESSOR-AUTHZ-20261004.json](evidence/OFFBOARDING-SUCCESSOR-AUTHZ-20261004.json).

## Policy and transaction contract

`internal/store/postgres/employee_disposition.go`, `offboardingSubjectsTx`, now requires the successor to be distinct from both target and caller, active in the authorized selected tenant, and manageable through existing `authz.CanManageTenantMember`. It reads role and activity together under the existing successor `FOR SHARE` fence. There is no new role ordering or cached-role authorization.

An ordinary administrator may select a user-role successor in their company. Peer administrators and super-admin successors are forbidden (403). Caller-self, target-self, missing, inactive and foreign successors are bad requests (400). Target management still uses the same helper. A current super-admin may act in the middleware-validated selected tenant and select its user/admin/super-admin members subject to existing target, activity, distinctness and last-administrator rules. The ordinary caller's `X-Tenant-ID` cannot broaden selection.

The validator is shared by preview, pending execute, qualified executed-receipt replay, and the internal one-step compatibility command. Existing plans are rechecked before fingerprint comparison or effects. A historical receipt cannot bypass current recipient qualification.

Lock order was inspected before editing: company tenant -> refreshed current caller -> persisted plan for execute -> target UPDATE -> successor SHARE -> authority/profile fences and active jobs -> owned mailbox locks. `companyTx` retains its tenant lock and interactive caller refresh. The added policy uses the already locked successor row; it introduces no lock. Source comments now include the caller and execute-plan fences.

Frozen targets remain eligible for planner disposition and are never reactivated. The legacy one-step inactive-target restriction remains. Completion epochs, permission/profile revisions, lease checks, asset fingerprint, plan CAS/locking, last-admin guard, queue disposition and replay semantics are unchanged. A manageable successor role change still invalidates an old fingerprint (409); stricter current qualification can reject earlier (403/400). Completed same-epoch replay returns the same receipt without repeating mutations; post-completion asset changes retain existing receipt behavior.

## Independent reproduction and validation

A new owned PostgreSQL 17 cluster at loopback port 55439, independently initialized under `/tmp/offboarding-authz-pg`, hosted disposable databases through existing unmodified seed/router helpers. Dedicated HTTP probes use an actual loopback `httptest.Server` and shipping authentication/router, miniredis and in-memory objects. No mocked HTTP responses, actual users, mail, or security configuration were used. A fixture-only database trigger rejects every false-to-true activity transition.

Before the fix, newly authored direct HTTP regression cases independently reproduced both gaps: admin -> peer-admin successor preview **200** and admin -> caller-self successor preview **200**, each persisting a preview plan. Both denial assertions failed as expected. Thus caller-self is now actual HTTP evidence, beyond LF01 source inspection. After the fix these requests return **403** and **400**, with full owned database state unchanged.

Final command, Go1.25.7 with readonly dependencies, both existing replaces and locks intact:

```sh
TABMAIL_SUCCESSOR_AUTHZ_OWNED=1 TABMAIL_TEST_DB_DSN=<new-owned-cluster-dsn> \
  go test -race ./internal/store/postgres -run '^TestSuccessorAuthzOwned' -count=1 -timeout=120s -v
```

**Pass: 8 top-level tests / 44 leaf cases, 20.249s package time.** Opt-in without the owned DSN fails; normal invocation skips these dedicated probes. Focused `go vet ./internal/store/postgres ./internal/authz`, existing `TestCanManageTenantMember`, and `git diff --check` pass.

Coverage includes active/frozen target execution and completion, successor deny/allow matrix, selected-tenant super-admin HTTP preview/execute/replay, stale successor role/activity, refreshed caller demotion/activity/tenant, target freeze after preview, allowed successor role change and asset fingerprint conflicts, replay current-state checks, synthetic pre-fix caller-self plan rejection, and legacy command denial. Every denial compares a full owned database snapshot including plans, identities, mailboxes, drafts, attachments, keys, tokens, grants, jobs, recipients, audit and outbox.

Successor tenant relocation between preview and execute is rejected by the persisted plan's composite same-tenant FK; after completion mailbox custody also rejects relocation. A no-custody synthetic successor isolates the plan FK before execute. Constraints were not disabled. After the failed relocation the valid unchanged plan still executes/replays. Foreign selection and current caller tenant changes are separately exercised through HTTP.

Real PostgreSQL wait edges establish qualification after tenant-lock waits for preview, execute and replay. An audit-table wait after qualification/effects proves the successor stays locked through commit: a competing `FOR UPDATE NOWAIT` gets 55P03, while a role update succeeds after commit. Four concurrent execute calls return the identical receipt timestamp and produce exactly one completion, one disposition audit, one session increment and one mailbox transfer.

## Scope and remaining limits

Only the subject-validation source, this dedicated Go test and these dedicated docs/evidence changed. No frontend, shared TSX, seed/router/fixture helper, go.mod/go.sum, lock, formal catalog, marker or acceptance file changed. No formal43/26/46, wholePG180, audit/default675/sharedDB import, central closure, merge or deployment occurred. Original formal failures and missing qualifications remain open.

No new authorization race was observed: current recipient policy is evaluated under the lock held until commit. These probes cover mailbox-only synthetic accounts and default seal disposition; they do not requalify queue uncertainty, private draft transfer, profile ABA, audit rollback, lease expiry, cross-tenant super-admin demotion races or all administrative concurrency. Existing profile NOWAIT and transactional protections remain untouched. Raw logs are local under `/tmp/offboarding-authz-{baseline,final,vet,cleanup}.log`, with hashes in the evidence. Cleanup independently reported zero owned fixture databases and connections, then stopped only this newly owned cluster.
