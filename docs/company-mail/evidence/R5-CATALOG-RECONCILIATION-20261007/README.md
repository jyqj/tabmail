# Reviewed source catalogs, inventory revision 3 — 2026-10-07

Source commit: `7b7dbfeaad5c87e875bf19e5a9e213867fda2db3`; source tree:
`c77ac064af69390ef493f6a7f1599b3ed7173738`. This reconciles the current
R5-P0-020/070/110 source inventories with already integrated source changes.
The unchanged transaction, compatibility, and client CLI checks all failed on
this exact source before reconciliation and pass with the reviewed catalogs.
The three related Python modules pass **54 tests**, and the original client
scanner passes **27 tests**, with no failures or skips.

This is static inventory maintenance. `task_complete=false`,
`runtime_verified=false`, `product_green=false`, current-wire requirements,
historical qualification, and central **10/171** progress retain their meaning.
These catalog-level flags do not override independently recorded parent-task
acceptance.
No result here qualifies PostgreSQL behavior, HTTP compatibility, SMTP delivery,
external runtimes, dependencies, or release readiness.

## Fixed historical snapshots

The original revision-1 files and the complete 2026-10-04 reconciliation and
independent review remain unchanged. Revision 2 is retained through its exact
Git objects at `4540fb91ba742443dcf0a872b80261e58af7b3ab`, an ancestor of the
current source. The complete original JSON is available without adding another
copy of the two large catalogs:

| Revision-2 path under `docs/company-mail/evidence/` | Git blob | SHA-256 |
| --- | --- | --- |
| `R5-TRANSACTION-COVERAGE.json` | `3e78588a301512e02d718666c54f9ea4d13640e0` | `9fe6a4d73935b8e51e064c9afe16279187d0516f2959189dabf58bf39e9a6bef` |
| `R5-COMPATIBILITY-GATES.json` | `4bddcd23e6d799c05a7fade19a3ceb6456576b3d` | `3c03f670576ecae4d260b6995c7230dde8a598288157940cd08c2921b78a5f06` |

Revision 3 keeps `previous_snapshot` as a repository-relative path and adds
`previous_snapshot_commit` and `previous_snapshot_blob`. **Resolve that path at
the declared commit**, using `commit:path`; the current file with the same name
contains revision 3. `previous_sha256` continues to identify the complete previous
bytes. The tests independently hard-code and compare commit, path, blob, and
SHA-256. A missing Git object fails immediately; no download, skip, or fallback
to current bytes is provided. Existing CI uses complete Git history, and the
source runner creates independent local clones from that history.

The revision-1 to revision-2 assertions still compare the original byte-pinned
snapshots: the same single PostgreSQL function body changed, the same two
compatibility closure hashes changed, and all original preserved metadata stays
equal. The old revision-2 metadata still validates its original revision-1 file
paths and hashes.

The **original six reconciliation methods**, with original module SHA-256
`e76e9feec0f7963506a1fbb43e696ac4cd22921fd354be8859a1dc4f62987900`, were also
executed unchanged in a separate clean checkout of `4540fb91…`: **6 passed,
zero errors/failures/skips, tracked diff empty**. That preserves the original
hash-only rejection proof on the source for which it was written. These six
historical results do not become current-source qualification. No historical
checkout is added to each CI run, and the source runner's four frozen v1 test IDs
and dispatch policy are unchanged.

## Precisely admitted current changes

Full before/after file hashes, Git blobs, source-history commits, and every
affected location are in [reconciliation.json](reconciliation.json).

| Inventory | Admitted delta | Preserved facts |
| --- | --- | --- |
| Current client AST facts | 19 locations across five files; one attachment-call owner changes from `a` to `Compose` | 134 branches, methods, paths, expressions, branch identities, and 7 explicit transport forwarders |
| `R5-CLIENT-CALLS.json` | The same 19 locations and one owner change | Every recorded route binding; original `--check` remains strict |
| Transaction coverage | 24 caller locations in 14 entries, originating in three non-PG files | All 62 PG file hashes, 395 function syntax records, 498 SQL execution calls, classifications, assertions, per-entry manual reviews, historical evidence, and 19 migrations |
| Compatibility route rows | Client metadata in 14 rows; the existing refresh request-body contract change in one of those rows | 132 routes, middleware/conditions, DTO/test bindings, upgrade batches, successor tasks, and unknown legacy usage |
| Declared compatibility closure | 10 hashes: nine source files and the current generated client facts | The same 94-path declared closure and unchanged historical wire reference |

### Client ownership metadata and the scanner preflight

The already integrated Compose change replaces the attachment callback's local
`const a = await company(...)` with an ownership-checked completion callback.
The original scanner therefore reports the enclosing owner as `Compose`
instead of `a`. This is an explicit reviewed metadata change; it is not described
as a line-only delta. The actual endpoint and POST method are unchanged.

Preflight on earlier integration source `97d6b71…` additionally found that the
new generic callback parameter `request()` looked like an API call to the
unchanged scanner, producing a 135th row with an empty path. The parent renamed
that local callback to `performRequest` before the source above was fixed.
The scanner itself was not changed, and no unresolved call was added to either
catalog. This reconciliation starts from the corrected **134-branch** source.

### Existing refresh request-body contract

Commit `0e0288fed6da7c8fdfae80c6b4bd4a9958e6b14b` already changed the auth
handler and OpenAPI, with the review recorded in
[R5-AUTH-BODY-LIMIT-20261005.md](../../R5-AUTH-BODY-LIMIT-20261005.md).
The handler bounds authentication JSON to 65,536 bytes and rejects a supplied
malformed or oversized body before token mutation. Empty in-budget bodies
remain usable for cookie clients. The refresh OpenAPI request body is optional,
and `refresh_token` is no longer required inside the optional JSON object.

Revision 3 records exactly that existing `request_contract` difference for
`POST /api/v1/auth/refresh`. The entire auth/OpenAPI file hashes remain bound as
well. This report reads the existing review and actual diff; it does not rerun
or inherit the reported auth runtime tests.

### Source hash provenance

The 11 source paths in the review packet cover all nine changed compatibility
source files and the three external caller sources, with auth shared by both:

| Source | Reason for the admitted source/location changes |
| --- | --- |
| `internal/api/handlers/auth.go`, `internal/api/openapi.yaml` | Existing bounded auth decoding and optional refresh-cookie body contract |
| `internal/api/handlers/r5_protocol_component_observations_test.go` | Existing explicit selected runtime version 2/3 passed through the bridge and validation/launch caller |
| `internal/outbound/builder.go` | Existing subject encoding/folding change moves call-name candidate locations |
| `internal/outbound/delivery.go` | Existing direct-delivery/null-MX work and reviewed DATA-final reply classification move call-name candidate locations |
| `web/components/company/compose.tsx` | Recipient reload, committed send/sender ownership, preview shape checks, and the local callback rename |
| `web/features/mail/components/draft-folder.tsx`, `message-pane.tsx` | Existing asynchronous editor-opening ownership and authoritative message refresh |
| `web/features/mail/components/submission-content.tsx`, `workspace.tsx` | Existing scoped workspace/reader refresh and editor/navigation ownership |
| `web/lib/api/base.ts` | Existing event-stream parser integration and chunk handling |

Every before hash is re-read from the fixed revision-2 Git object. Every after
hash and blob is re-read at the fixed current source commit and compared with
the actual checkout. The catalog revision identifies that current source
commit; it does not confuse it with the commit retaining revision 2. The
current transaction base/review-base and compatibility acquisition now name
the current source, while old per-entry review bases and complete previous
acquisition records remain available unchanged in revision 2.

## Verification and rejection controls

[verification.json](verification.json) records the actual test IDs, scoped
counts, original and current module hashes, generated catalog hashes, command
results, and log SHA-256 values.

| Execution | Result |
| --- | --- |
| Exact current source with old transaction catalog | Exit 1; 14 entries report caller drift |
| Exact current source with old compatibility map | Exit 1; current client producer differs |
| Exact current source with old client route catalog | Exit 1; first Compose source line differs |
| Current three Python modules | 54 passed, 0 failures/errors/skips |
| Original client scanner tests | 27 passed, 0 failures/skips |
| Current transaction CLI | Exit 0; source inventory PASS, runtime not verified |
| Current compatibility CLI | Exit 0; source inventory/upgrade plan checked, current wire still required |
| Current client `--check` | Exit 0; 134 branches and 7 explicit transport forwarders |
| Original six revision-2 tests on exact historical source | 6 passed, 0 failures/errors/skips |

The Python selection is `test_r5_transactions`, `test_r5_compatibility`, and
`test_r5_catalog_reconciliation`, loaded through the standard unittest loader
and runner with actual test-ID recording. No tests were removed or skipped.
The reconciliation module retains its six existing test IDs and adds three
current revision controls. The original source-runner discovery includes them
in its current group without changing global dispatch.

Existing source/AST/SQL/file-hash/route mutations continue to reject, including
the copied actual source mutation removing successor-self rejection and the
actual changed route tested by the original Go producer. Added controls reject
an unreviewed client line and an external caller line. Revision-2 catalogs are
explicitly rejected against current actual facts, while revision 3 passes the
same strict validators. The earlier compatibility row/client check now rejects
old rows before the closure hash check; that is the expected current failure
stage, separate from the preserved historical hash-only proof.

Tools were existing Go 1.25.7 Linux/amd64, Node 24.19.0 and TypeScript 5.9.3.
The isolated checkout received only the 132 files from an independently
archive-verified TypeScript preparation, checked against its complete file
hash receipt. No shared dependency tree was modified. The source Go producers
retain their original readonly/count and timeout contracts. All five original
validator/collector source files are byte-identical to revision 2 and are
checked as such by the new reconciliation tests.

## Remaining boundary

The 94-path closure is the existing declared compatibility source inventory;
it is not a complete imported/compiler/runtime dependency closure. Call-name
candidates are not resolved dispatch or concurrency proof. Historical runtime
and wire artifacts are not re-signed, re-executed, or transferred to this source.
The existing missing OpenAPI operations, unobserved wire/runtime work,
PostgreSQL gate failures, dependency audit findings and release prerequisites
remain with their original owners. Historical parent-task acceptance remains
unchanged; this static revision does not grant new runtime or release
qualification. This patch
contains current catalog data, the necessary revision-aware tests, and this
dedicated report; the parent owns central TODO and PR management.
