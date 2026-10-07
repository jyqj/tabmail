# Source catalog revision 7 — final parallel-929 / STREAM source

Public product source: `9b12c93cb03285298267e27893f74aebe742a8a2`.
Source tree: `87c87a0db72ac444050b43fbe67d906f45c4a2ac`.

This is static catalog maintenance. It completes zero additional implementation TODOs and does not accept PostgreSQL runtime, live wire behavior, dependencies, a parent task or the full integration PR. All original `task_complete`, `runtime_verified` and `product_green` declarations remain false.

## Exact predecessor and current facts

The four immutable revision-6 catalog snapshots come from public merge `c3e1419e6245baf0190291ea868787cb2b0177ca`. Their reviewed product source is separately fixed at `f413a9138d305cf154ed2cecaddcf9b9a2397666`. Revision 7 records both identities: snapshot history uses the published catalog commit, and source changes compare the reviewed product `f413` with current product `9b12c93`. This distinction preserves the later STREAM changes that were already present when revision 6 was published.

The original producers retain 62 PostgreSQL files, 395 functions, 498 SQL execution calls and 19 migrations. Function syntax, SQL classifications, file hashes, migrations, manual reviews and historical runtime links are unchanged. Sixteen entries update 21 external caller locations. Two name-match candidates are added and one is removed: original replay content is now read and closed by its dedicated helper. These are syntax candidates, not evidence of runtime dispatch or concurrency behavior.

There remain 132 registered routes. The original client producer now records 134 branches and seven forwarders, compared with revision 6's 133 branches. STREAM's grants editor adds an explicit GET snapshot when a stale permission revision needs review. The existing grants GET supplies the same registered route mapping; the new call does not repeat the PUT mutation. Every old client row is retained exactly once, and the one added row is explicit. Five existing rows change only reviewed locations or ownership, and 116 indices shift after the inserted row. Route method, path, handler, middleware, request/response contract and release identities are unchanged.

The existing 94-path compatibility closure retains its path set. Three hashes change: the generated client list, template page, and mailbox-grants component. Six source files have explicit before/after Git blob and SHA-256 provenance. The declared closure remains a source inventory, not a complete compiler, import or runtime closure.

## Historical controls and current assertions

All 35 earlier catalog-review files retain their exact bytes. The five original producer/validator files retain their exact bytes. All 15 existing reconciliation method IDs remain: revision-6 positive reviews now read its immutable catalog and product objects, and two new methods bind revision 7. All five existing mutation methods retain identical ASTs and continue to test the current catalogs.

The one current-source positive assertion in `test_r5_compatibility.py` changes from 133 to 134 branches, matching the original producer and the explicitly reviewed added GET. Historical branch counts stay bound to their original snapshots. No test is removed, skipped, given a longer timeout, or redirected to invented fixture data.

## Verification

The three original CLI checks each exited 0. The complete original transaction, compatibility and reconciliation modules ran **62 Python tests: 62 passed, no failures, errors or skips**, including all 17 reconciliation methods and the five unchanged mutation controls. The original Node client controls ran **27 tests: 27 passed, no failures or skips**. All six active catalog/test inputs were rechecked against the frozen SHA-256 manifest after execution. Raw command stdout/stderr, exit codes, source identity and hashes are retained in the evidence packet. Independent review **ACCEPTS** this static catalog revision with no blocking findings. It separately reran the three original CLIs and all 17 reconciliation methods, and executed all 15 methods of the original current-source-inventory fixture module: every case passed without skips. That 15-method result covers fixture source-policy and manifest binding, not a full live source-version runner. The independent 17-method rerun is recorded separately from the 17 cases already included in the author's 62-test run.

Earlier source drift remains historical evidence. The separate 42-member handoff at `7f2f477` and 18-member `f9dda710` supplement are immutable; neither is relabeled as a current revision-7 pass. Revision-6 baseline execution at the exact `52c4129` / `9b12c93` tree records all three original CLIs failing before this catalog reconciliation.

[Raw evidence ZIP](raw-evidence.zip) and [member manifest](raw-evidence-manifest.json) retain 71 byte-verified members: the failing revision-6 baseline, fresh producer facts, author and independent commands, frozen review inputs, preparation scripts, and the original f9 supplement ZIP/manifest/report. No original output is converted into a success by omission.

The older 42-member handoff is already published unchanged in [the round-3 evidence](https://github.com/jyqj/tabmail/blob/87c5e6001f8e895d20d493eedd32aa038e232999/docs/company-mail/evidence/R5-PARALLEL-20261008-929/round3/catalog-handoff/r5-catalog-handoff-7f2f477.zip); revision 7 references that exact blob instead of duplicating it. Its public bytes were rechecked against the original archive.
