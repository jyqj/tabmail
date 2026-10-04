# LF01 frozen target selector repair — 2026-10-04

Base: `f875672dea2b68fdec9e0b986f4863724da5d7ba`. This is a focused production UI repair with new, self-authored regression probes, not a formal catalog run or business qualification. Safe aggregate evidence: [LF01-SELECTOR-REGRESSION-20261004.json](evidence/LF01-SELECTOR-REGRESSION-20261004.json).

The original failed formal record remains at commit `46de5010b7dc79b841c52d6ff7245d070a846a76`, `docs/company-mail/R5-FORMAL-COMPONENTS-F875-20261004.md` and its original report. Preserve `R5_PROTOCOL_UI_TARGET_LF01_FROZEN`: the single formal run was 53.781s, 17 pass / 9 fail / 17 missing / 0 qualified. No original shared TSX, Go fixture, catalog, marker, acceptance, lock, Go1.25.7, two replaces or budget was edited. No formal 43/26/46 rerun, central10/171 closure, wholePG180, audit, default675/sharedDB import, merge or deployment.

## Production change

`web/features/company/offboarding-panel.tsx` now derives manageable targets without an active-only restriction. Frozen and active targets use the same current tenant and caller hierarchy checks. Only active, manageable, distinct successors are offered; foreign, frozen, caller-self, target-self and roles outside caller authority are excluded. Super-admin choices use the selected company; ordinary administrators cannot use a different company to gain choices. Retained selections are checked against current employees and caller authority before enabling preview.

No account is reactivated. Completed disposition is a durable server state, not something inferred from `is_active`; completed targets can remain in the selector and receive the existing 409 on a new preview. The receipt and execute conflict flows are unchanged.

At this fixed base, the real planner already calls `offboardingPlanTarget(..., true)`, admitting frozen targets while checking completion epochs. The legacy one-step internal `offboardingTarget(..., false)` guard remains separate. `companyTx` refreshes caller authority, `CanManageTenantMember` enforces target hierarchy, and execute checks current subject qualification and the persisted asset/authority fingerprint. HTTP routes use the real planner. No backend authority was widened.

## Focused observations

An independently initialized PostgreSQL 17 cluster on loopback hosted disposable fixture databases. A new Go test starts the shipping HTTP router on a real loopback `httptest.Server` and launches a dedicated Vitest configuration against it. The shipping panel, API functions, session and fetch use real responses. Only host authentication context and the confirmation dialog are supplied by the test. A transparent fetch observer records real execute statuses and rejects non-owned network destinations; it never supplies a response.

| Owned real HTTP/PG + shipping UI scenario | Observation |
| --- | --- |
| Active target | preview 200; execute 200; completed receipt; new preview 409 |
| Frozen target, undisposed | old JWT 401; preview 200; stays inactive after preview; execute 200; new preview 409 |
| Successor frozen after preview | execute 400; no receipt/refresh or completed plan; target and mailbox unchanged |
| Target frozen after preview | execute 409; stale plan removed and shipping re-preview message shown; no completed plan or mailbox transfer |

DB assertions check target activity, exact completed-plan count and mailbox owner. A DB trigger rejects every false-to-true user activity transition throughout these probes. Dedicated HTTP negatives also check target-self and identical successor 400, actual foreign successor 400, actual foreign target 404, peer-admin target 403, and inactive successor 400.

Final owned probe: `go test ./internal/store/postgres -run '^TestLF01Owned' -count=1 -timeout=120s -v`, with `TABMAIL_LF01_OWNED_PROBE=1` and a DSN pointing only to the newly owned cluster: **pass, 7.968s**. The opt-in Go tests fail if the owned DSN is missing once enabled; normal test invocation does not launch them. `npm exec -- vitest run --config vitest.lf01.config.ts` is launched only by this producer with its private, short-lived fixture packet.

The same newly authored frozen probe against the original production selector at `f875672` failed at option membership (**1.688s**). The production file was restored before the fixed run. This is a narrow regression control, not a rerun of the old formal test.

Four synthetic selector tests pass under `npm test -- --run features/company/offboarding-panel.test.tsx`. They use invented employee rows to cover hierarchy, tenant scoping, inactive successors, self exclusions and refreshed/stale selections; they make no API calls or response mocks and are not real HTTP evidence. Focused ESLint, TypeScript `--noEmit` and `git diff --check` pass. Independent owned-cluster queries showed zero fixture databases and zero fixture backend connections before successful shutdown. Raw logs remain in `/tmp/lf01-{baseline,go,unit,cleanup}.log`; hashes are in the aggregate JSON.

## Remaining gaps

The real API accepts an admin peer as successor: dedicated HTTP observation is **200**, because `offboardingSubjectsTx` checks successor tenant/activity but not successor role hierarchy. It also does not exclude the caller as successor in that subject function (source inspection only). These are separate backend follow-ons; the panel cannot secure direct HTTP callers. The 200 is recorded as an existing gap, not accepted as the intended business contract.

These probes cover mailbox-only synthetic fixture accounts and the default seal option. Existing seed/router helpers are reused without modification; miniredis and an in-memory object store support the router. No real Redis/S3/SMTP service, browser automation, private draft disposition, queue uncertainty, audit rollback, last-admin concurrency, super-admin HTTP impersonation or full catalog qualification is claimed. No actual accounts, user data or email were used. Original rejected formal qualifications and missing layers remain open.
