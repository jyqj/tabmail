# Shared PE adapter migration — combined focused review

Base `d9a56effa08489f65ebc169ed161d18f8326ee25`; migration `78cd8f9ad1bb41879f6d2a300fb6cc310ae7d353`; PE03 diagnosis/proposal `4b0bc7da980663069eb653e88f29e66dcb4e456a`. Imported the exact transport worker delta `f027fcd9f2720407d06a2459406b8af24ab38b90` as `833cbf1`. The shared TSX integrates its real fetch observer; the historical proposed integration patch remains an unapplied reference because the migrated PE branches required different wiring.

Root decision: genuine invalidation → dirty draft preserved / alert / disabled Save / no UI PATCH, plus separately labelled actual old-revision + old-security HTTP PATCH409 CONFLICT without restoring revoked permissions. The direct control is never attributed to the UI. Production stale guards are unchanged.

## Actual focused results and qualification matrix

Dedicated `TestPESharedAdapterFocused` reuses unchanged catalog/setup helpers with a separately owned PG17 cluster and fresh per-variant disposable databases. It invokes the real shipping shared TSX through the local installed Node/Vitest tree, never the formal all-case producer. API clients, session, SWR, fetch responses, shipping router/handlers and PG are real. Only host identity and browser-environment shims use the established component harness.

| Case / acceptance | Actual result | Genuine wire observations | Persisted assertions |
| --- | --- | --- | --- |
| PE01 / AC-01 | PASS | ready 1, override patch 1 | exact quota-only body.patch; compound revision; omitted raw restrictions preserved |
| PE02 omitted / AC-03 | PASS | ready 1, override patch 1 | exact quota-only patch; omitted raw scalars/domain preserved |
| PE02 null / AC-03 | PASS | ready 1, override patch 1 | actual inheritance reset; explicit patch.can_send:null; raw null/source validated |
| PE02 false / AC-03 | PASS | ready 1, override patch 2 | current successful true preparation before editor; actual switch to changed false; exact false patch/raw readback |
| PE02 0 / AC-03 | PASS | ready 1, override patch 1 | actual quota input; exact numeric zero patch/raw readback |
| PE02 [] / AC-03 | PASS | ready 1, override patch 1 | actual All control; exact patch.domain_access:{mode:all,zone_ids:[]}; raw all/empty slice; no legacy zone body |
| PE03 / AC-02 | PASS | ready 1, targeted profile update 1 | successful revocation200; UI blocked/no new PATCH; separate old-security/revision HTTP409; can_send false and old persistent description/revision retained |
| PE04 / AC-03 | PASS | ready 1, override patch 3 | successful current reset200/restoration200 with strictly advancing revisions; UI blocked/no new PATCH; separate old-compound-revision HTTP409; restored raw state/revision retained |

Frame counts include observed replayed seed/preparation events where applicable; they are not all attributed to later mutations. PE03 captures a stream cursor before revocation and awaits a subsequent tenant/resource/action-bound profile-update frame. PE04 captures distinct cursors before reset and restoration and awaits a subsequent targeted override-patch frame for each. Connection-only/synthetic resync cannot satisfy these assertions. Both establish enabled Save/no alert with dirty input before mutation; both preserve the dirty form after invalidation. PE03's dirty description and old true sending switch remain; PE04's dirty true sending intent remains. Disabled clicks emit zero new UI business requests. Direct CAS controls are independently issued after those UI assertions.

PE02 false preparation is necessary because the unchanged Go setup seeds false for every variant. Returning a switch to baseline false correctly omits it and produces no changed command. Preparation uses a successful current versioned command before opening the editor, preserves other restrictions, and is never represented as the UI mutation qualified. The oracle reads raw values independently and never infers overrides from effective permissions.

## Checks and cleanup evidence

Command: `go test -mod=readonly -count=1 -tags=r5protocol -timeout=180s ./internal/api/handlers -run '^TestPESharedAdapterFocused$' -v`. Result: 8/8 focused real-chain variants PASS, 38.988s. The diagnostic runner independently queries actual PG raw scalar/domain columns and the PE03 profile after each passing component chain. Each cleanup asserts zero owned fixture databases/backends and a closed owned HTTP listener. Every observed stream ends or aborts, no stream observation errors; final owned-cluster query is `0|0`, and `pg_ctl fast stop` succeeds.

Safe aggregate source hashes, per-variant results and cleanup metadata: [report.json](evidence/PE-SHARED-ADAPTER-FOCUSED-20261004/report.json). Private fixtures, Vitest JSON and logs remain outside the repository under `/tmp/pe-adapter-owned`; only sanitized metadata is committed.

Eight self-authored pure oracle regressions also PASS (1.25s). These synthetic checks are not mocked network responses or HTTP/PG qualification. They reject raw restriction loss despite equal effective permissions, null omission, redundant security writes, legacy zone spelling, revision reuse and wrong compound observations; BigInt revisions exceed JS's safe integer range. TypeScript noEmit, focused ESLint and diff check PASS.

Owned changes are shared TSX, new oracle/regressions, dedicated diagnostic runner and review evidence/docs. Transport worker's Go fixture/helper/tests/docs delta is imported unchanged. No production/offboarding/profile or LF changes. Catalog inputs, target markers, acceptance, Go1.25.7/replaces, lock and formal component budgets remain unchanged. Diagnostic timeout is confined to the new focused runner. Local Node24.19.0/Vitest4.1.11 dependency tree is reused; no fresh official-lock bootstrap or formal external-runtime receipt is claimed.

No formal all-case rerun, historical report replacement, response-mock qualification, actual email/user data, wholePG/audit, central10/171 closure, merge or deploy. The original failure remains intact. Focused passing component chains are ready for independent combined review; formal/catalog qualification and browser journey closure are not claimed. The denied PR/API action is not retried; normal origin push only.
