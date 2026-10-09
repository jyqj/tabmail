# Independent review: R5 catalog revision 4

Decision: **ACCEPT**. No blocking issue was found in the fixed 16-file catalog patch.

Reviewer: `smtp_reliability`. Review uses fixed Git objects, Python AST comparison, independently derived JSON deltas, actual source lines, and existing raw execution logs. No Go, frontend, Python suite, scanner, or producer was rerun.

Product parent: `e32564304c0c84aa80b1184e72129fb0316d989d`.
Reviewed commit: `7254f12e833051ce4a6bb6d4cb145db4795f3adc`.
Reviewed tree: `1eb98c6185e7b8b1e0be5967eb3fa741f84f0097`.
Complete binary diff SHA-256: `593de1b227db8b07c0d870fc724a52ca1d27a7a6e3c058b2887403a6679d5dc2`.

## Findings

- The patch is exactly four current catalogs, one version-aware test module, and eleven dedicated evidence files. The original five validators/collectors are byte-identical across fixed revision 2, revision 3, product parent, and reviewed head. No original schema, budget, skip, or validator was edited.
- Independently resolved all eight revision-2/revision-3 commit/path/blob/SHA-256 pins. Revision-3 predecessors still resolve to revision 2; revision-2 predecessors still hash to the original revision-1 files. Required historical commits are ancestors of the reviewed head. Existing relevant CI fetches full history and the source runner uses an independent unshallowed clone. Missing objects remain errors, with no new fetch, fallback, skip, or frozen dispatch.
- All twenty files in the two historical review directories are identical to the fixed revision-3 and product-parent bytes. The original revision-2 static method and five mutation-rejection methods have identical ASTs. All nine old IDs remain, with two new IDs.
- The two revision-3 static review methods retain their complete assertion AST after narrowly substituting fixed revision-3 catalogs/clients and original source/evidence identities. The current-source byte comparison becomes a comparison of the fixed revision-3 commit against its original source commit. No assertion or expected historical delta was removed.
- Independently derived exactly nine client line changes at indices 0–5 and 93–95; all owners, methods, paths, expressions, branch identity, forwarder count, and route bindings are unchanged. Each old/new line points to the actual callee in the fixed old/current source.
- Independently derived fifteen changed transaction entries: twenty-nine line changes, one added `input.Close`, removed `r.Close`, and removed function-local `errors.New`. The derived caller multiset transformations and full-list hashes match the evidence. Every changed line is checked against its actual fixed source. These remain explicitly labeled same-name candidates, not resolved PostgreSQL dispatch. All noncaller entry facts, PostgreSQL source/syntax hashes, SQL/manual-review facts, migrations, and other catalog facts are preserved.
- Compatibility changes only client locations in seven rows and three hashes within the same ninety-four-path declared closure. All ninety-four hashes match actual reviewed Git bytes. Request/response contracts, middleware, conditions, DTO/test bindings, unknowns, and historical wire qualifications are unchanged.
- The seven source-file provenance records match actual before/after blobs and SHA-256 values; the new helper is absent at the predecessor. Every cited implementation commit is an ancestor of the product source, changes its stated path, and has the recorded subject.
- False qualification flags remain false. Catalog revision 4 does not provide new runtime, current-wire, dependency, release, or parent-task acceptance and does not count as an additional implementation item.

## Existing execution evidence reviewed

The embedded raw log hashes match the fixed verification packet. The raw unittest lines independently resolve to 71 distinct executed IDs: 26 transaction, 19 compatibility, 11 reconciliation, and 15 current-source inventory methods, all `ok`. The original scanner log records 27 passed and zero failed/cancelled/skipped/todo. The three baseline CLI results are exit 1; the transaction baseline identifies exactly the fifteen changed entries. The three current CLI results are exit 0. These are the author’s executions, read and verified here without repetition.

## Scope and environment observations

During review, only untracked Python bytecode caches were observed; fixed HEAD/tree and all tracked bytes matched the reviewed commit. The reviewer did not import repository modules or modify the worktree, and does not attribute creation of those caches. Acceptance is bound to the fixed Git objects.

The CI workflow was already changed before the catalog patch during the implementation wave. The catalog parent-to-head diff leaves it unchanged. Both source-runner scripts are unchanged even relative to the fixed prior catalog revision. The review does not incorrectly claim all CI bytes remained unchanged throughout the whole wave.

## Fixed file hashes

| Path | SHA-256 |
| --- | --- |
| `docs/company-mail/evidence/R5-CATALOG-REVISION4-20261007/README.md` | `c1311118acf9031d19bbd26aed741519a2233158da69c0cd8bc2be5ef35d060f` |
| `docs/company-mail/evidence/R5-CATALOG-REVISION4-20261007/clients-baseline.log` | `cb6689d856439375bbae4ab9d8c8715328ba9dd4203489bbde6b8b34d1ea83c0` |
| `docs/company-mail/evidence/R5-CATALOG-REVISION4-20261007/clients-current.log` | `b5e92b0af9e25b23eb3f0ba9fd2ded8ff3a1fca31020a69cb91e0ab94000c5db` |
| `docs/company-mail/evidence/R5-CATALOG-REVISION4-20261007/compatibility-baseline.log` | `a954d20d64d0e992b5e1fb6ce6fc0867a1871be7f54fd7426290df24c454da55` |
| `docs/company-mail/evidence/R5-CATALOG-REVISION4-20261007/compatibility-current.log` | `677f536dea0eccc18aba48fb1134bf11abab8c980e362691fb8a01a9bd29d4db` |
| `docs/company-mail/evidence/R5-CATALOG-REVISION4-20261007/python-tests.log` | `3ada16a17c47094eeed8a46cea098dca81a3c7fe426cd2613469aab05b09cf0f` |
| `docs/company-mail/evidence/R5-CATALOG-REVISION4-20261007/reconciliation.json` | `515da75267d8b900f2e0b8c967ff68a651a510a01b11c2e89bb0519708f20517` |
| `docs/company-mail/evidence/R5-CATALOG-REVISION4-20261007/scanner-current.log` | `de60b47c422ed3ad96abad4280595b749fe5947fe3cdd6640859c2a9af176fa6` |
| `docs/company-mail/evidence/R5-CATALOG-REVISION4-20261007/transactions-baseline.log` | `4a3d33b6adcc3664381889862e42427288c7b6f043e0cd09925bb72e58c05b16` |
| `docs/company-mail/evidence/R5-CATALOG-REVISION4-20261007/transactions-current.log` | `517448bd669e67d3c12da0b4e94ef95e34bf2c94d156a3aadabc4b2a464c80fe` |
| `docs/company-mail/evidence/R5-CATALOG-REVISION4-20261007/verification.json` | `5a3ba198225485c8bc46273a23f8f64ed3e05386e835442572ac09b6ff28aae4` |
| `docs/company-mail/evidence/R5-CLIENT-CALLS.json` | `0877601e0a825ba28f2e912bc057cf92e7d607ca568f5d1b29621e2798dc5139` |
| `docs/company-mail/evidence/R5-COMPATIBILITY-CURRENT-20261003/clients.json` | `83eeb4a43fa3a6c762353c1597c00e998ca64bf68138a7eb165984095b110e3d` |
| `docs/company-mail/evidence/R5-COMPATIBILITY-GATES.json` | `56452854110d0c33b5d2ae9eaf1abe43a7f77681a59307fc2470796dbdc18f56` |
| `docs/company-mail/evidence/R5-TRANSACTION-COVERAGE.json` | `5fc5fb70255b5e6986b9aab38157fbb915b6012377d76d5b2a1956fc2907de2c` |
| `scripts/tests/test_r5_catalog_reconciliation.py` | `d132966eba0f50c7a52eff38afc85884b82c50d902a890194a7ab97470b6eceb` |

## Machine evidence

`catalog-revision4-independent-review.json` SHA-256: `774a60e0c2698c341f15dd5000866a66e29265e14599396fa9b371deda2f2579`.
All 57 final read-only assertions passed. These assertions are reviewer checks, not additional product tests. The JSON retains pin identities, all twenty historical file hashes, six method AST hashes, exact independently derived deltas, source provenance checks, original log results, and the initial environment/scope observations.
