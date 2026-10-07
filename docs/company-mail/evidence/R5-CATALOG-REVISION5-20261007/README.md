# Reviewed source catalogs, inventory revision 5 — 2026-10-07

Published product source: `79738c17d185f1cd5cd1c8d550a9f15a5501f31c`; source tree:
`1621d5fefb443d9d314abc2f9e0fab94914ce4c4`. This inventory binds the integrated
product after the three follow-up implementation rounds. It changes four
current catalogs and their reconciliation tests. The five original validators
and collectors, historical review packets, source-runner dispatch, timeouts and
skip policies remain unchanged.

This is source-catalog maintenance, not another completed implementation TODO.
Parent-task progress and product execution remain separately recorded in the
central follow-up report. Catalog `task_complete`, `runtime_verified` and
`product_green` remain false; dependency acceptance remains operator review.

## Exact inventory changes

| Catalog | Admitted changes | Preserved facts |
|---|---|---|
| Current clients and `R5-CLIENT-CALLS.json` | 7 records change source locations or the explicitly reviewed owner/expression fields; producer ordering moves 14 records | 134 branches, 7 explicit forwarders, all method/path/branch/callee/forwarder identities and corresponding route mappings |
| Transaction coverage | 8 caller locations across 6 entries; 3 added name-match candidates and no removals | All 62 PostgreSQL files, 395 function syntax records, 498 SQL execution calls, 19 migrations, classifications, precise SQL assertions, per-entry reviews and historical evidence |
| Compatibility rows | Client source facts in 7 template route rows | 132 routes, request/response contracts, middleware, conditions, DTO/test bindings, release batches and unknown legacy-client usage |
| Compatibility closure | 5 hashes: generated clients, company template validation, template page, editor and versions view | The same declared 94-path closure and historical wire reference |

[reconciliation.json](reconciliation.json) records the complete reviewed caller
changes, client correspondence, route changes, source blob/hash provenance and
catalog hashes. Eight changed source files explain these differences.

### Client moves preserve API identity

The original producer sorts calls by source file and position. The final array
corresponds to these revision-4 indices:
`[0..7, 9..20, 23, 21, 22, 8, 24..133]`. Every old record occurs exactly once.
The ordering changes are explicitly bound; they are not counted as new APIs.

| Old → current index | Call | Reviewed source change |
|---|---|---|
| 6 → 6 | List templates | Page line 37 → 43 |
| 7 → 7 | Retire template | Page line 76 → 92 |
| 23 → 20 | Preview template | Editor line 310 → 111; owner `TemplateEditorView` → `value`, inside `renderPreview` |
| 21 → 21 | Update template | Editor line 61 → 169; expression uses `snapshot.id` instead of `edit.id` |
| 22 → 22 | Create template | Editor line 61 → 169 |
| 8 → 23 | Publish template | Page line 98 → editor line 199; owner `onPublished` → `save`, expression uses the saved `res.id` instead of `value.id` |
| 27 → 27 | Read template versions | Versions view line 37 → 48 |

The source collector still emits exactly the same endpoint/method identities
under this correspondence. The original client validator independently checks
the regenerated array against its preserved route mappings and real Go routes.

### Name matches are not resolved dispatch

The three additional candidates arise from the original collector's deliberate
call-name approximation:

- `readQueuedAttachment` adds `input.Close` to the `PgStore.Close` name-match
  inventory. This closes an object reader.
- Recovery `Verify` adds `sha256.New` to the `PgStore.New` name-match inventory.
  This constructs a hash, not a store.
- `readQueuedAttachment` adds `errors.New` to that same `New` inventory. This
  constructs an error, not a store.

The eight location changes also include existing template-builder `New` calls
whose positions moved after scalar validation was factored into a helper. No
PostgreSQL implementation or migration changed. The new queued-reader helper
has no revision-4 Git object, so its `before_blob` and `before_sha256` are null;
the test verifies that absence directly.

## Immutable historical chain

Revision 1 keeps its original files and review. Revisions 2 and 3 retain their
existing fixed objects and tests. Revision 4 is now pinned independently at
`6b6163dc8941aa36bb9fa2b75f40c101b8f662fe`, with original product source
`e32564304c0c84aa80b1184e72129fb0316d989d`:

| Path under `docs/company-mail/evidence/` | Git blob | SHA-256 |
|---|---|---|
| `R5-TRANSACTION-COVERAGE.json` | `bd50c385e9c6c385a650f68ce5c5857ee69986ea` | `5fc5fb70255b5e6986b9aab38157fbb915b6012377d76d5b2a1956fc2907de2c` |
| `R5-COMPATIBILITY-GATES.json` | `23a227e3eae3aacc44d2c2be97c35bfa55bcc8a6` | `56452854110d0c33b5d2ae9eaf1abe43a7f77681a59307fc2470796dbdc18f56` |
| `R5-COMPATIBILITY-CURRENT-20261003/clients.json` | `9989cd7fe881f23eb2fe5b08e1a390e3128033e3` | `83eeb4a43fa3a6c762353c1597c00e998ca64bf68138a7eb165984095b110e3d` |
| `R5-CLIENT-CALLS.json` | `6a9cac1e365ce2664c6cddb170009d52fec2184c` | `0877601e0a825ba28f2e912bc057cf92e7d607ca568f5d1b29621e2798dc5139` |

Resolve each prior snapshot as `previous_snapshot_commit:previous_snapshot`,
then verify both its Git blob and complete SHA-256. A missing object fails
directly. There is no fetch, fallback, skip, or duplicate historical catalog.

All eleven existing reconciliation method IDs remain. The two revision-4
methods now read their catalog/client snapshots and review from the fixed
revision-4 Git objects, and compare historical source to its original product
commit. Their old counts and assertions retain their original meaning. All five
existing mutation-rejection methods have unchanged ASTs. Two added methods
check the exact revision-5 caller transformation, client correspondence,
preserved reviews, source identities and catalog hashes. Existing historical
packet directories are also checked byte for byte against revision 4.

## Verification

The original validators accepted the staged final source facts and rejected
the old catalog facts during preparation. The complete related Python selection
then executed **73 tests**, all passing with no failures, errors or skips. The
original Node client collector selection executed **27 tests**, all passing
with no failures or skips. These counts cover the selections below; they do
not claim execution of the complete versioned source runner.

| Execution | Actual result |
|---|---|
| `test_r5_transactions` | 26 PASS |
| `test_r5_compatibility` | 19 PASS |
| `test_r5_catalog_reconciliation` | 13 PASS: 11 retained IDs and 2 new methods |
| `test_r5_current_source_inventory` | 15 PASS |
| Original `api_calls.test.cjs` | 27 PASS, 0 FAIL/SKIP |
| Original transaction CLI | Exit 0; static inventory PASS, runtime not verified |
| Original compatibility CLI | Exit 0; source/upgrade plan checked, current wire still required |
| Original client `--check` | Exit 0; 134 branches, 7 forwarders |
| `git diff --check` | PASS |

An independent read-only review also checked the fixed snapshot identities,
the five unchanged mutation-method ASTs, all five original collector/validator
files, eight source provenances and all 31 historical packet files. Execution
logs remain private; these source and aggregate checks do not recertify any
historical runtime receipt.

The complete related selections use the original commands:

```sh
python3 -B scripts/check_r5_transactions.py
python3 -B scripts/check_r5_compatibility.py
node scripts/collect_api_calls.cjs --check
PYTHONPATH=scripts:scripts/tests python3 -B -m unittest \
  test_r5_transactions test_r5_compatibility \
  test_r5_catalog_reconciliation test_r5_current_source_inventory -v
node --test scripts/tests/api_calls.test.cjs
```

Local source collection uses existing Go 1.25.7, Node 24.19.0 and TypeScript
5.9.3. It reuses the existing TypeScript bytes; no dependency tree was changed.
These local results do not represent execution under Node 22 or successful
GitHub CI. Raw execution logs stay outside the repository; this directory
contains source facts and aggregate verification statements only.

The declared closure retains its existing scope. This reconciliation does not
certify a complete compiler/import/runtime closure, current wire behavior,
HTTP/DB/SMTP execution, resolved call dispatch, absence of dependency findings,
or release readiness. Historical runtime evidence is preserved without
recertification.
