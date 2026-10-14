# Catalog revision 3 — independent read-only review

Reviewed at 2026-10-07T12:54:59+00:00 by `/root/smtp_reliability`, independently of the catalog author `/root/pr_triage`.

**Conclusion: no blocking finding in the reviewed patch.** This approves the limited catalog reconciliation and its necessary revision-aware tests for integration. It provides no new runtime, wire, PostgreSQL, dependency, release, or parent-task qualification.

## Scope and method

Source parent: `7b7dbfeaad5c87e875bf19e5a9e213867fda2db3`; source tree: `c77ac064af69390ef493f6a7f1599b3ed7173738`. Worktree: `/workspace/scratch/458de7991ac3/tabmail-catalog`. The review covers five existing catalog/test files plus the three new evidence files listed below. Only this review file was written; no worktree source or catalog was modified. The author CLI commands, collectors, unit tests, historical tests, and broad regressions were not rerun.

The reviewer read the complete test diff, catalog field differences, original historical tests, relevant source changes, existing CI/clone configuration, and new README/verification packet. Independent checks used only Git reads, JSON comparison, AST comparison, and complete-file SHA-256. The five-file `git diff --binary 7b7dbfeaad5c87e875bf19e5a9e213867fda2db3 -- <five existing paths below>` contains **47908 bytes**, SHA-256 **`012254f599dfb8f7a3b3d71d95af39d59116ab1593ddf1c8f18762f618ec88fd`**. This digest excludes the new evidence files, which are individually pinned below.

## Historical identity and retained assertions

Revision 2 is read from the fixed ancestor `4540fb91ba742443dcf0a872b80261e58af7b3ab`. The following four identities were independently checked with `git rev-parse commit:path`, `git show commit:path`, and SHA-256; they match hard-coded test constants, not merely current JSON metadata.

| Historical snapshot | Git blob | SHA-256 of complete bytes |
| --- | --- | --- |
| transaction | `3e78588a301512e02d718666c54f9ea4d13640e0` | `9fe6a4d73935b8e51e064c9afe16279187d0516f2959189dabf58bf39e9a6bef` |
| compatibility | `4bddcd23e6d799c05a7fade19a3ceb6456576b3d` | `3c03f670576ecae4d260b6995c7230dde8a598288157940cd08c2921b78a5f06` |
| clients | `2d2990ca3f1672af09fedb50f6f826aa15bcea0b` | `f4a06b6911bfa888d4720235759b0bd59ee1314eddfdc9fe85524eca7e9c2c4f` |
| client_routes | `1dead7eb284dc8b2de7d56b58db40636785b4ddb` | `3fdaa56861ba4e524ddcdbf1d69b2b8513e89f2567500d0410effdf73ab1a617` |

The snapshot paths are the corresponding first four file-table paths below. The helper fails when the fixed Git object is absent or differs; it has no network fetch, skip, or current-file fallback. Current revision-3 metadata is compared to these independent commit/path/blob/hash constants. `previous_snapshot` retains its relative-path type and must be resolved at the fixed historical commit. Current source metadata pins the actual source parent, avoiding a catalog self-reference.

The original `test_only_reviewed_body_and_two_closure_hashes_change` method is AST-identical after normalizing only `self.rev2_tx/compat` back to its original operands and removing the appended historical metadata loop. Thus all original revision-1 to revision-2 static assertions remain: entry identity, the exact one changed PostgreSQL body, unchanged migrations/file-type/manual metadata/classifications/evidence levels, the exact two closure hashes, and all other original compatibility fields. The original revision-2 number and previous-snapshot SHA assertions were retained in that appended loop against fixed revision-2 bytes.

All four pre-existing `test_unapproved_*` method ASTs are exactly unchanged. The old compatibility runtime rejection expectation necessarily changes on current source because changed client rows now fail before the closure-hash check. That current expectation is explicit, while the unchanged original six methods are separately recorded on their exact historical source. Historical results are not inherited as current qualification.

Existing `.github/workflows/company-p0.yml` backend/frontend jobs retain `fetch-depth: 0`; `scripts/run_r5_source_version_tests.py` retains its complete independent local clone without a shallow-depth option. The fixed revision-2 commit is an ancestor of the source parent. The patch changes neither workflow nor source-runner dispatch. A manually supplied shallow repository lacking historical objects intentionally fails rather than silently approving newer bytes.

## Independently measured current delta

| Area | Actual admitted change | Preserved boundary |
| --- | --- | --- |
| Both client catalogs | Each remains 134 rows and 7 forwarders; exactly 19 line values and one owner `a` → `Compose` change | Every other client field and every recorded route binding remains equal |
| Transaction entries | 24 caller line values across exactly 14 of 395 entries; callers originate in `auth.go`, `builder.go`, and `delivery.go` | No other entry field changes: PG syntax/hash, classification, assertions, review data and evidence remain equal; top-level migrations, PG files and historical/manual metadata remain equal |
| Compatibility routes | Client metadata changes in 14 rows; only additional contract difference is `POST /api/v1/auth/refresh` optional request body and removal of the JSON `refresh_token` requirement | Route identities/count 132, route bindings, handlers, middleware, DTO/test references and other contract fields remain equal |
| Compatibility closure | Exactly 10 hashes change: nine source paths plus generated current client facts | Same declared 94-path closure and historical wire reference; no expanded qualification |

The attachment owner difference is supported by the actual Compose source: the previous `const a = await company(...)` expression becomes a returned `company(...)` call inside the owned operation, with completion handled separately. Endpoint and POST method remain equal. The OpenAPI change is already present in source commit `0e0288fed6da7c8fdfae80c6b4bd4a9958e6b14b`; the catalog does not introduce that product contract.

All 11 source records have independently verified before/after Git blobs and SHA-256 at the two fixed commits; each after-byte sequence also equals the current worktree. They cover the nine changed compatibility source paths and three caller sources, with auth shared:

- `internal/api/handlers/auth.go`
- `internal/api/handlers/r5_protocol_component_observations_test.go`
- `internal/api/openapi.yaml`
- `internal/outbound/builder.go`
- `internal/outbound/delivery.go`
- `web/components/company/compose.tsx`
- `web/features/mail/components/draft-folder.tsx`
- `web/features/mail/components/message-pane.tsx`
- `web/features/mail/components/submission-content.tsx`
- `web/features/mail/workspace.tsx`
- `web/lib/api/base.ts`

## Collector and rejection boundary

Setup still obtains actual transaction AST/migration facts and actual route/client facts from the original producers. Revision-2 catalogs are rejected on those current facts and current revision 3 passes the same validators when executed by the author. Added controls reject an unreviewed client line and external caller line. The existing actual-source, AST/SQL, source-hash and route-source mutation controls retain their original bodies. Exact source pinning and immutable snapshots avoid trusting a catalog to approve its own new historical identity.

The reviewer independently confirmed these five original source files are byte-identical to revision 2 and agree with the recorded digests:

| Original validator/collector | SHA-256 |
| --- | --- |
| `scripts/check_r5_transactions.py` | `1f14b5b4e3ef11cd0642e9d7261cde285c17e47384c90e15b56adc68323b9355` |
| `scripts/check_r5_compatibility.py` | `d3d0bd96c84a18c224160faaad96f3a928aab5224d93c31b6ec464489f740209` |
| `scripts/collect_api_calls.cjs` | `06896de9f18dda8c7fd4f1ef2d520e4bd71fa831a99f0d63863a1fa6a06682e8` |
| `cmd/r5txinventory/main.go` | `ad14b919505b23947de335dda6077d4f6745185329d911cc357e60b1001d2361` |
| `internal/architecture/route_inventory_test.go` | `609ee5489c3e021f9b1a0238b47e4a01d09c5e5eef1144df780d6456e2ed4f05` |

The README and verification packet report current Python 54/54, original Node scanner 27/27, all three current CLI commands exit 0, corresponding old-catalog baselines exit 1, and original historical tests 6/6. This review did not independently execute those commands or re-hash their raw log files. It did verify that the recorded current test-module hash, all four tested catalog hashes, historical original test-module hash, and 54 unique recorded current test IDs agree with the reviewed bytes. These execution counts remain author evidence and are separate from this independent static review.

## Final editorial correction and commit

The final catalog commit is `003b7ab7530c4097d98c4a70afc9e46a95474ae4`, tree `a3230b80aba1c63f60be82cb90ee7d341596290b`. After the initial code review, the parent identified an inaccurate README closing sentence describing already accepted historical parent items as open. The author corrected only that prose: historical parent acceptance and central 10/171 progress remain unchanged; the generated catalog flags do not override those separate acceptances or add runtime/release qualification. The reviewer read the corrected closing section and bound the final README bytes below. No catalog/test byte or code conclusion changed, and no test rerun was required for this editorial correction.

## Reviewed file identities

| File | SHA-256 |
| --- | --- |
| `docs/company-mail/evidence/R5-TRANSACTION-COVERAGE.json` | `3a49928f35308951340025741ba0c308e8fecf20fc7792bc5b45115175f3f009` |
| `docs/company-mail/evidence/R5-COMPATIBILITY-GATES.json` | `0817aa5c77142849401dafe8e5acf3194feedc489539442a3cdcf9686cfd7f0d` |
| `docs/company-mail/evidence/R5-COMPATIBILITY-CURRENT-20261003/clients.json` | `842ade963ab45a9215123929ed1db529da5d94c4a8c2462f612df9c615e3399c` |
| `docs/company-mail/evidence/R5-CLIENT-CALLS.json` | `8e00ecf63e11b46062add84f062ad7f7d780949ae842127e5aed055db05fb6de` |
| `scripts/tests/test_r5_catalog_reconciliation.py` | `94b5f572c1d24f40c3c4bfb8f817e075447893214a548dec223f97f48e636ab9` |
| `docs/company-mail/evidence/R5-CATALOG-RECONCILIATION-20261007/reconciliation.json` | `70138d94bc6ac0e9e845ae1bca6d02d3d985282f804065e8a56bea6f30f259c5` |
| `docs/company-mail/evidence/R5-CATALOG-RECONCILIATION-20261007/README.md` | `c85e5d3564a1c1ce4c94e72575c5ffa6b09e633a58d4e3c33aa3b18b378dd5b0` |
| `docs/company-mail/evidence/R5-CATALOG-RECONCILIATION-20261007/verification.json` | `7200eae3eee38eed7afbca1fba09990934628c372b5b78e2b512fa120486ac6f` |

The generated catalogs retain `task_complete=false`, `runtime_verified=false`, and `product_green=false` where applicable. Runtime/wire and dependency requirements remain open. No necessary smaller correction or gate weakening was found in this exact reviewed snapshot. Later changes to any file above require comparison with these identities before carrying this review forward.
