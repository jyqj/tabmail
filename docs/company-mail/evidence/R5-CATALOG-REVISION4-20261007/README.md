# Reviewed source catalogs, inventory revision 4 — 2026-10-07

Product source: `e32564304c0c84aa80b1184e72129fb0316d989d`; source tree:
`bfa06a61ce4bc8411243d3b57de26db5e161dc8b`. This is the integrated source after
the ten implementation increments in this wave. It reconciles the four current
client/transaction/compatibility catalogs without changing any validator,
collector, historical review, source-runner dispatch, budget, or skip policy.

All three original CLI checks failed on this source with the revision-3 catalogs.
After the exact changes below, all three pass. The related Python selection
passes **71 tests** and the original client scanner passes **27 tests**, with no
failures, errors, or skips. This catalog maintenance is not an eleventh TODO
implementation. Parent progress remains **10/171 accepted, 161 remaining**.
Catalog `task_complete`, `runtime_verified`, and `product_green` remain false;
these flags do not override independently recorded parent-task acceptance.

## Exact inventory changes

| Catalog | Admitted changes | Preserved facts |
|---|---|---|
| Current client facts and `R5-CLIENT-CALLS.json` | 9 line locations: recovery page 6, API base 3 | 134 branches, 7 explicit forwarders, every owner, method, endpoint, expression, branch identity and route binding |
| Transaction coverage | 29 caller line locations in 15 entries; 1 added and 2 removed name-match candidates | All 62 PostgreSQL files, 395 function syntax records, 498 SQL execution calls, 19 migrations, classifications, precise SQL assertions, per-entry reviews and historical evidence |
| Compatibility rows | Client location metadata in 7 route rows | 132 routes, request/response contracts, middleware, conditions, DTO/test bindings, upgrade batches and unknown legacy-client usage |
| Compatibility closure | 3 hashes: recovery page, API base, generated current clients | The same declared 94-path closure and historical wire reference |

[reconciliation.json](reconciliation.json) records every client location, every
transaction caller location/addition/removal, full before/after caller-list
hashes, source blob/hash provenance, and the integrated commits responsible for
each source path. Seven reviewed source files account for all admitted changes.

The source producer deliberately records same-name call candidates, not resolved
dispatch. Three candidate-set changes therefore need explicit explanation:

- The owned attachment-reader helper adds `input.Close` under
  `internal/app/companymail/attachment_read.go::readOwnedAttachment` to the
  `PgStore.Close` name-match inventory.
- The previous `r.Close` under `Service.verifiedFile` is removed from that same
  inventory because the helper now owns closing. These are object-reader calls;
  neither is a claim of a PostgreSQL close operation.
- `Parser.Attachment` no longer constructs `errors.New` in the method; it returns
  the reviewed package-level missing-part sentinel. The original collector
  records function-body calls, so that `PgStore.New` name-match candidate is
  removed. No collector behavior was changed to hide it.

The new helper has no object at the revision-3 source: its `before_blob` and
`before_sha256` are explicitly null, and tests prove absence using the fixed
Git tree. All other before bytes come from the fixed revision-3 commit. All
after bytes are checked both at the fixed current source commit and in the
actual checkout. PostgreSQL behavior is not inferred from these call candidates.

## Immutable historical chain

Revision 1 keeps its original files, hashes and historical review. Revision 2
keeps the independently pinned objects at
`4540fb91ba742443dcf0a872b80261e58af7b3ab`. Revision 3 is now retained through
these independently hard-coded objects at
`4065c4909c8f21a401a9a1af6370fa3f72670b99`:

| Path under `docs/company-mail/evidence/` | Git blob | SHA-256 |
|---|---|---|
| `R5-TRANSACTION-COVERAGE.json` | `2dbd71b6b9f2ccd6eb1bf141f439b6ff83a1f26e` | `3a49928f35308951340025741ba0c308e8fecf20fc7792bc5b45115175f3f009` |
| `R5-COMPATIBILITY-GATES.json` | `6b14b9ec4e8aac5caac5d74f47847b16e25e9e5e` | `0817aa5c77142849401dafe8e5acf3194feedc489539442a3cdcf9686cfd7f0d` |
| `R5-COMPATIBILITY-CURRENT-20261003/clients.json` | `f9f82391f73b3e8a5680c015a860bf880a429d2d` | `842ade963ab45a9215123929ed1db529da5d94c4a8c2462f612df9c615e3399c` |
| `R5-CLIENT-CALLS.json` | `1d375226040c4edb3b4571eaddf1396f875643b1` | `8e00ecf63e11b46062add84f062ad7f7d780949ae842127e5aed055db05fb6de` |

`previous_snapshot` remains a repository-relative path. Resolve it as
`previous_snapshot_commit:previous_snapshot`, then verify both blob and complete
SHA-256; the current same-named file contains revision 4. Missing Git objects
fail directly. There is no fetch, fallback, skip, or new copy of the large
historical catalogs. Existing CI and source-runner clones provide full history.

The revision-1 to revision-2 static assertions still use the exact revision-1
files and revision-2 blobs. The two revision-3 review methods now use pinned
revision-3 catalog bytes and their original `7b7dbfe…` source, instead of binding
their old delta assertions to whichever revision happens to be current. Their
19 client locations, old single owner change, 14 transaction entries and refresh
contract change retain the original meaning. Revision-3 metadata is also checked
to preserve its revision-2 predecessor tuple.

All nine existing reconciliation test IDs remain. The historical revision-2
static-delta method and all five existing mutation-rejection methods have
identical ASTs to the revision-3 test module. Two additional methods validate the
revision-4 caller transformation and source/client provenance. Original strict
validators continue to reject old revision-1/2/3 facts on current source and
reject unreviewed source, route, AST, caller, and client mutations.

The complete prior 2026-10-04 and revision-3 review directories are byte-for-byte
unchanged. Historical runtime, current-wire, and original six-test execution
receipts are neither re-executed nor transferred to this source. No per-CI
historical checkout or new global frozen dispatch is introduced.

## Actual verification

[verification.json](verification.json) contains the exact test IDs, source pins,
module/catalog hashes, original test-method AST hashes, unchanged historical
file hashes, tools, command outcomes, and raw-log hashes. Logs are retained in
this directory.

| Execution | Result |
|---|---|
| Exact current source with old transaction catalog | Exit 1; 15 entries report caller drift |
| Exact current source with old compatibility map | Exit 1; current client producer differs |
| Exact current source with old client-route catalog | Exit 1; first recovery-page client line differs |
| `test_r5_transactions` | 26 PASS |
| `test_r5_compatibility` | 19 PASS |
| `test_r5_catalog_reconciliation` | 11 PASS |
| `test_r5_current_source_inventory` | 15 PASS |
| Original `api_calls.test.cjs` | 27 PASS, 0 FAIL/SKIP |
| Original transaction CLI | Exit 0; static inventory PASS, runtime not verified |
| Original compatibility CLI | Exit 0; source/upgrade plan checked, current wire still required |
| Original client `--check` | Exit 0; 134 branches, 7 forwarders |
| `git diff --check` | PASS |

The Python run uses the standard unittest loader and records all 71 executed
IDs without removing tests. Source-runner discovery separately confirms that
both new reconciliation IDs belong to the existing current group, while the
same four v1 IDs retain their original frozen baseline. That discovery result
is not represented as execution of the complete source runner.

Commands, using existing Go 1.25.7, Node 24.19.0 and TypeScript 5.9.3:

```sh
python -B scripts/check_r5_transactions.py
python -B scripts/check_r5_compatibility.py
node scripts/collect_api_calls.cjs --check
PYTHONPATH=scripts:scripts/tests python -B -m unittest \
  test_r5_transactions test_r5_compatibility \
  test_r5_catalog_reconciliation test_r5_current_source_inventory -v
node --test scripts/tests/api_calls.test.cjs
```

The original five validator/collector files remain identical to revisions 2
and 3. Their original Go and Node producer time limits and source-runner policy
are unchanged. No dependency tree was modified. This review retains the declared
closure's existing scope; it does not claim a complete compiler/import/runtime
closure, new HTTP/DB/SMTP execution, resolved call dispatch, zero dependency
findings, or release readiness.
