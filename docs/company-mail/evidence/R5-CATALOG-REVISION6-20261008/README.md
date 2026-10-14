# Source catalog revision 6 — final f413 checkpoint

Published source: `f413a9138d305cf154ed2cecaddcf9b9a2397666`. Source tree: `37adfe5efb4ae49274a6d52a5d769bcb5b116cf0`.

This revision reconciles the latest frozen shared integration source. Our ten implementation TODOs retain the original `ff8872b737a947c158035a0b07d78396397ad626` product checkpoint; subsequent STREAM and parallel-929 changes belong to their own batches. This static maintenance completes zero additional implementation TODOs. Runtime, product and parent-task acceptance flags remain false.

## Final facts and precise correspondence

The original producers report 62 PostgreSQL files, 395 functions, 498 SQL execution calls and 19 migrations. PostgreSQL bodies, hashes, classifications, SQL assertions and manual reviews are unchanged. Relative to fixed revision 5, 41 entries have 63 external caller line movements, three added and two removed name-match candidates. The two removed read/close candidates move to `finish`/`closeUnderlying`, while the additional candidate closes the source opened on an error path. These are syntax candidates, not dispatch or runtime proofs. The final webhook change adds ten reviewed caller location movements beyond the e992/ad2 candidate.

There remain 132 routes, now **133 client branches and seven forwarders**. The two template PUT calls were consolidated into the single `changeGrant` helper: old indices 25 and 26 map to new index 25; old 0–24 retain their indices and old 27–133 shift down once. The old PUT rows differ only in line and identify the same route. The final GET is at line 57; the shared PUT is at line 81 with owner `changeGrant`. Route method/path/handler/middleware/contract/release identities are unchanged. Both template-grants route rows update only their client facts. All old client rows are explicitly covered once by the reviewed grouping, so a removed record cannot disappear implicitly.

The existing 94-path compatibility closure changes six digests: the generated current-client list, outbound handler, JSON responder, models, template page and template grants component. Eleven actual source files have explicit before/after Git blob and SHA-256 provenance in the packet. The declared closure is not a complete compiler/import/runtime closure.

`scripts/tests/test_r5_compatibility.py` changes exactly one current-source positive assertion from 134 to 133 branches, matching the original producer. Historical 134 assertions stay fixed to their original snapshots. The five original collector/validator files are byte-identical. All 33 earlier review files remain unchanged. The reconciliation module retains all 13 existing method IDs plus two new revision-6 methods; all five original mutation methods have identical ASTs and still validate current facts. Both revision-5 positive reviews read immutable catalog `697b12d70b84658f5429e9a88ad2efde94e2e443` and product `79738c17d185f1cd5cd1c8d550a9f15a5501f31c` objects.

## Executed evidence, separated by source

| Source checkpoint | Actual execution retained in this packet |
| --- | --- |
| Final f413 | Original transaction, compatibility and client CLI each exit 0; root separately passed 17 Python tests (15 reconciliation plus two current positives), zero skips; diff check exit 0; static byte/AST/history guards retained |
| ad2 intermediate | Three original CLIs and 17 Python tests passed: 15 reconciliation tests plus two current positive assertions |
| e992 intermediate | Three original CLIs, 60 Python tests and 27 Node tests passed |
| ff/d1a intermediate | Three original CLIs, 60 Python tests, 27 Node tests, root's 15-test reconciliation rerun and former independent review retained |

The root integration step separately executed the final-source 17-test selection: 17 passed, zero failures/errors/skips, 27.858 seconds in unittest (28.319 seconds wall time). Its original raw files and tested-review core snapshot are embedded under `root_final_reconciliation`; initial incomplete-copy and import-loader attempts are preserved separately without claiming accepted test results. This packet does not relabel an intermediate test run as a final-source run. No final-source 60/27 rerun is claimed. Original process limits, validators and mutation assertions are preserved.

`reconciliation.json` embeds exact command stdout/stderr with byte counts and SHA-256 hashes, the ff root-15 log and prior independent review, the reconstructable unpublished ff/d1a patch, and explicit ff→e992→ad2→f413 fact transitions. The ff→e992 Git change set includes our already-published round-3 documentation; the separate b812→e992 list identifies the 12 STREAM merge paths. Intermediate candidates are labeled superseded, not current acceptance.

Current collection used Go 1.25.7, Node 24.19.0 and existing TypeScript 5.9.3 bytes through the unchanged producer interfaces, with offline Go dependency lookup. These local static checks do not certify PostgreSQL integration, live wire behavior, dependency acceptance or remote GitHub CI.
