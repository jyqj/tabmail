# Independent bounded offboarding pair review

Verdict: no actionable defect observed in the bounded source pair. The frozen target selector and recipient authorization fixes are consistent with existing tenant member policy. This is independent focused evidence, not combined product acceptance or formal qualification.

## Exact reviewed sources

Base: `f875672dea2b68fdec9e0b986f4863724da5d7ba`.

UI: `648938a5baad2f35ba120da2cf5ae1001f8f3b4a`; production file `web/features/company/offboarding-panel.tsx`, Git blob `52554e543a3aef7c9d28cebb149d3411e286a515`.

Backend: `062c5bad63f2d37cedd0bfb1cdccf0fb7a195a8b`; production file `internal/store/postgres/employee_disposition.go`, Git blob `619feb36f23f6f3039cbd8c8230a4bcd7b809fd7`.

The isolated joint-test tree is exactly the base plus those two file substitutions: tree `9fae5d8e219ddfa90e27b1fdb13704b00996554e`. No author tests or reports were imported into the test tree. Both author reports and complete production diffs were read. The [exact union patch](evidence/PAIR-INDEPENDENT-20261004/exact-production-union.patch) records the 23 added / 8 removed production lines. To reproduce, apply this context-free patch with `git apply --unidiff-zero` in another isolated base worktree, then add the review tests and use a fresh owned cluster. Production files were restored to base after testing; this report branch contains independent tests and evidence only. A separate temporary Git index produced the union tree without committing combined production.

## Actual observations

New independently authored tests used an independently initialized PostgreSQL 17 cluster at `127.0.0.1:55447`, disposable per-case databases through unchanged `seedCompany`/`testpg.NewPostgres`, the shipping router with actual loopback HTTP sockets, fixture miniredis, and in-memory objects. There were no actual accounts, outbound email, system setting edits or shared database imports. An owned fixture trigger rejects every inactive-to-active transition.

The [old-source HTTP control](evidence/PAIR-INDEPENDENT-20261004/baseline-http.log) independently reproduced peer-admin successor **200** and caller-self successor **200**, each persisting a preview. The [old UI control](evidence/PAIR-INDEPENDENT-20261004/baseline-ui.log) failed as expected because the frozen target was absent. With the exact pair, peer preview is **403** and self preview **400**, with every public database table unchanged. The new frozen target preview/execute both succeed while the target remains inactive.

Final command (official Go 1.25.7, readonly dependencies):

```sh
TABMAIL_PAIR_REVIEW=1 TABMAIL_TEST_DB_DSN=<owned-cluster-on-port-55447> \
  go test -race ./internal/store/postgres -run '^TestPairIndependent' \
  -count=1 -timeout=180s -v
```

[Final run](evidence/PAIR-INDEPENDENT-20261004/final-fixed-race.log): **9 top-level tests, 58 leaf cases pass; 30.598s package time**. Four leaf cases launch new review-owned Vitest probes against the live shipping router and PostgreSQL. Shipping UI, API functions, local session and fetch are real; only the host auth context and confirmation dialog are supplied. A transparent fetch observer rejects destinations outside the owned origin and records actual statuses; it never synthesizes responses.

| Focused check | Independent result |
| --- | --- |
| Current target/caller/successor tenant, role and activity | Frozen target allowed; admin peer/higher target and successor denied; self/equal subjects and foreign/inactive successors denied; ordinary and frozen caller denied |
| Selected-tenant super-admin | Live preview, execute and replay succeed with user/admin/super-admin successors in selected tenant; ordinary foreign tenant header cannot broaden authority |
| Changes between preview and execute | Successor role 403/freeze 400; target role 403/freeze 409; caller role 403/freeze 401; caller tenant relocation 404, even with old tenant header; allowed role changes under selected super-admin invalidate fingerprint with 409 |
| Tenant relocation of target/successor | Real PostgreSQL 23503 prevents relocation while plans reference them; all tables unchanged, valid plan still executes; no constraint bypass was used |
| Plan safeguards | Asset revision, expiry, different plan creator/target, and synthetic historical self-successor plan all reject without effects |
| Replay/completion | Same receipt is returned with all public tables unchanged; successor/target role, activity or session epoch changes deny; unverified receipt fingerprint denies; second pending plan and new preview cannot repeat completed disposition |
| Legacy one-step command | `OffboardEmployee` rejects peer, self, inactive/foreign successor and frozen target without effects; it shares fixed validation and cannot bypass recipient qualification |
| Real lock waits | `pg_blocking_pids` establishes tenant-lock waits for preview/execute/replay. Successor role change and cross-tenant super-admin caller demotion while waiting both deny after release, with all tables unchanged |
| Locks through commit | Execute waits on `audit_log`; caller, target and successor each reject competing `FOR UPDATE NOWAIT` with 55P03. After commit successor role update succeeds and receipt replay denies |
| Concurrent execute | Four actual HTTP execute calls return identical receipt time; exactly one disposition audit, executed plan and session increment |
| Shipping UI over HTTP/PG | Frozen completion 200 and second preview 409; successor promotion after preview causes execute 403 with no receipt/refresh; target freeze causes 409 and re-preview message; selected-tenant super-admin completion succeeds |

Denial and replay checks independently serialize every public table, ordered by JSON row value, before and after the operation. This includes users, tenant policy, plans, assets, audit/outbox and migration metadata. State mutations that establish each scenario happen before its comparison snapshot.

The initial fixed run had one test expectation error: caller relocation yielded 404 rather than expected 403, because authentication resolves the caller's current tenant and the old plan is absent there. The [initial log](evidence/PAIR-INDEPENDENT-20261004/initial-fixed-race.log) is retained. Only the test expectation was corrected; no implementation was edited. New allowed-role fingerprint and post-qualification lock tests were added before the final run.

Focused `go vet ./internal/store/postgres ./internal/authz`, ESLint for the two new TS files, TypeScript `--noEmit`, and source/test/Markdown diff whitespace checks passed. Raw logs retain the original Vitest output whitespace verbatim. `go.mod`, `go.sum`, both existing replaces and npm lock stayed exactly at base. [Cleanup evidence](evidence/PAIR-INDEPENDENT-20261004/cleanup.log) reports zero owned fixture databases and connections, followed by shutdown of only the newly owned cluster. [Manifest](evidence/PAIR-INDEPENDENT-20261004/manifest.json) records evidence and test hashes.

## Policy review and remaining limits

The UI uses the current selected tenant, excludes caller and equal subjects, admits manageable frozen targets, and offers active manageable successors. Backend authorization remains necessary: `companyTx` refreshes current interactive caller authority after the tenant fence; `offboardingSubjectsTx` locks target UPDATE and successor SHARE, checks current role/activity using existing `CanManageTenantMember`, and is shared by preview, execute, qualified replay and compatibility command. No new lock or policy ordering is introduced. UI retained selections are rechecked before preview; a previously previewed plan still relies on API validation at execute. The four HTTP-backed UI cases do not independently requalify every selector refresh combination from the author's synthetic tests.

No new authorization race was observed within these focused probes. They cover mailbox-only fixture accounts, default seal, role changes on tenant waits and subject fences during an audit wait. Remaining unqualified layers include private draft/attachment disposition, queued/in-flight/uncertain delivery, permission profile ABA and NOWAIT interactions, audit rollback, lease expiry during post-effect waits, last-admin concurrent removals, caller freeze/session/tenant relocation during lock waits, target freeze during lock waits, successor freeze during lock waits, and full browser/session-switch rendering. Cross-tenant super-admin demotion during tenant waits is independently covered here; other cross-tenant caller races remain unqualified. Tenant relocation constraints were observed rather than disabled to fabricate impossible plan states.

Original formal result **17 pass / 9 fail / 17 missing / 0 qualified** remains unchanged, including its rejected qualifications and missing layers. No formal43/26/46, wholePG, audit/default675 or sharedDB suite was run; no central closure, implementation changes, combined accepted product, merge or deployment is claimed.
