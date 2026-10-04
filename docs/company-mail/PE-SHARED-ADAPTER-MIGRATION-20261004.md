# Shared PE component adapter migration — partial review

Base `d9a56effa08489f65ebc169ed161d18f8326ee25`; PE03 diagnosis/proposal `4b0bc7da980663069eb653e88f29e66dcb4e456a`. Root decision: qualify genuine invalidation → preserved dirty draft / alert / disabled Save / no UI PATCH, plus a separately labelled actual old-revision + old-security HTTP PATCH returning 409 CONFLICT without restoring revoked permissions. Never attribute the direct control to the UI.

Only shared TSX, new pure oracle/regressions and this document change. Go fixtures, production/offboarding/profile code, catalog inputs, target markers, acceptance, Go1.25.7/replaces, lock and formal budgets remain unchanged.

## Qualification matrix

| Case / acceptance | Migrated assertions | Status |
| --- | --- | --- |
| PE01 / AC-01 | Raw GET; actual quota-only shipping PATCH; exact compound revision/body.patch; omitted raw scalars/domain preserved; monotonic revision | Migrated; no real-chain rerun |
| PE02 omitted / AC-03 | Exact quota-only patch; omitted raw restrictions preserved | Migrated; no real-chain rerun |
| PE02 null / AC-03 | Actual inheritance reset; explicit patch.can_send:null; raw null/source validated | Migrated; no real-chain rerun |
| PE02 false / AC-03 | Current successful true preparation before editor opens; actual switch to false; exact changed false patch/raw readback | Migrated; no real-chain rerun |
| PE02 0 / AC-03 | Actual quota input; exact numeric zero patch/raw readback | Migrated; no real-chain rerun |
| PE02 [] / AC-03 | Actual All control; exact patch.domain_access:{mode:all,zone_ids:[]}; raw all and empty/null Go slices; no legacy top-level zone body | Migrated; no real-chain rerun |
| PE03 UI / AC-02 | Dirty description before revocation; enabled Save/no alert initially; monotonic successful revocation; dirty description/old sending retained; alert/disabled Save/no new UI PATCH; persisted revision/description unchanged | Genuine stream observation pending |
| PE03 direct HTTP / AC-02 | Separate old revision plus complete old security; actual second PATCH 409 CONFLICT; revoked sending/revision/persistent description retained | Implemented separately; not UI; no rerun |
| PE04 / AC-03 | Dirty grant; successful current reset/restoration with monotonic revisions; stale UI blocked/preserved; separate old compound-revision direct CAS409; full restored raw state/revision retained | Genuine stream observation pending |

PE02 false preparation is necessary because unchanged Go setup seeds false for every variant. Returning a switch to its baseline false correctly produces omission/no changed command. The current successful preparation preserves domain/quota restrictions and is never represented as the UI mutation being qualified. The oracle reads raw values independently; it never infers overrides from effective permissions.

## Proposed transport hook contract

Transport owner thread `01a104e3-568d-7062-a66f-1c0fd4624001`. Reserve fetch-observer imports/top-level hooks for its helper API and exact source. Historical empty SSE is not event evidence; JSON-clone observation must not await an unbounded SSE body.

Combined patch: install helper at real-fetch observation boundary, return original streaming Response promptly, record bounded safe frame metadata on a separately drained branch, cancel observer/release readers at cleanup. Preserve ordinary real JSON/204 observations and loopback enforcement.

Before opening PE03/PE04 dialogs, await genuine tenant-bound wire ready and completion of initial connection resync/read work. Establish enabled Save/no alert after dirty input. Capture stream cursor before external mutation. PE03 must await a subsequent validated company.admin.changed frame with permission.profile.update / permission_profile / fixture.profile_id metadata. PE04 must observe permission.override.patch / user / fixture.employee_id frames for reset and restoration. Assert dirty preservation/blocking after genuine frames; synthetic reconnect resync alone is insufficient. Missing stream fails qualification. Never response-mock it or weaken/force the disabled shipping stale guard.

This partial patch does not claim genuine event delivery from its historical observer. Full real-chain qualification depends on combined transport review.

## Self-authored focused checks

- `cd web && ./node_modules/.bin/vitest run components/company/r5-permission-contract-oracle.test.ts`: 8/8 pass, 1.07s. Synthetic pure-oracle checks, not mocked network responses or HTTP/PG qualification. Reject raw-loss despite unchanged effective values, null omission, redundant security writes, legacy zone spelling, revision reuse and wrong compound observation. BigInt revisions exceed JS safe integer range.
- `cd web && ./node_modules/.bin/tsc --noEmit --pretty false`: pass after direct shipping helper imports.
- ESLint shared TSX/new oracle/test and `git diff --check`: pass.

No formal all-case rerun before combined review, no historical report replacement, response-mock qualification, actual email/user data, wholePG/audit, central10/171 closure, merge or deploy. Partial migration only.
