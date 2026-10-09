# Source catalog revision 8 — final STREAM / QUALITY source

Final public product source: `e0cd175996ca4ee314d7d8b8cf836023b680346c`.
Product source tree: `5307be3cf057104d1bf1529e38235bbaf0c2bcdf`.

This revision completes **zero additional implementation TODOs**. Parent acceptance remains **10 of 171**, with **161 unaccepted**. `task_complete`, `runtime_verified` and `product_green` remain false. No runtime, wire, dependency, release, M/G0 or parent-task qualification is added.

## Predecessor, final source and preserved checkpoint

The four revision-7 catalog snapshots are pinned to public merge `f77c31e2da38bb926dfe6fa134eabad652e94f8c`, with complete Git blob and SHA-256 checks. Their reviewed product source is separately fixed at `9b12c93cb03285298267e27893f74aebe742a8a2`, tree `87c87a0db72ac444050b43fbe67d906f45c4a2ac`. Published catalog bytes define history; the separately named product commits define source changes. The older generated client file in raw `9b12c93` is not treated as an additional revision-8 client change.

Revision 8 first collected and checked STREAM product source `53cd5de5afddfc661ade306c16ca11da573d49de`. That phase is preserved byte-for-byte in [checkpoint53-reconciliation.json](checkpoint53-reconciliation.json), with its SHA-256 fixed in the current packet and tests. Local identifiers `f18d5305be34c1103f74fb335ffd37ebda5fe071` and `f1f82d61f91ec98e415f47619615c889e2253fe9` describe the checkpoint and actual runner candidate; CI does not need those local Git objects. After the final parallel QUALITY integrations completed, this same unpublished revision advances its product pin to `e0cd175`. The 53-phase results are historical checkpoint evidence, and are not relabeled as final e0 execution.

All **40** files in the six earlier catalog-review directories retain their exact published bytes, including revision 7's existing raw ZIP, manifest and integration report. Revision 6's snapshot `c3e1419e6245baf0190291ea868787cb2b0177ca` and reviewed product source `f413a9138d305cf154ed2cecaddcf9b9a2397666` remain unchanged historical pins. New command output stays private; this packet publishes reviewed facts, command outcomes and hashes.

## Reviewed PostgreSQL change

The original transaction producer finds **62 PostgreSQL files, 395 functions, 19 migrations, 134 direct-write functions and 154 write-closure functions**. SQL execution calls change from revision 7's **498 to 499**. Eight syntax records change: `SetTemplateGrant` has a changed function hash, and seven subsequent functions move by exactly 12 source lines with their function hashes unchanged. One PostgreSQL file hash changes: `internal/store/postgres/company_templates.go`. These eight records and their manual review are identical to the preserved 53 checkpoint; the later QUALITY integration does not add another PostgreSQL SQL/body change.

The `SetTemplateGrant` change is reviewed as a real authorization-related SQL branch. When `enabled=true`, `activeCompanyUser` continues to require an active target user in the same tenant. When `enabled=false`, a new `SELECT EXISTS(SELECT 1 FROM users WHERE tenant_id=$1 AND id=$2)` allows an existing frozen employee's grant to be revoked. A missing or foreign-tenant employee returns `BadRequest`.

The surrounding transaction remains explicit: `companyTx` takes the tenant write lock, rereads the current actor, requires current company administration, checks mailbox access, checks the target user, and checks template existence. The DELETE still matches tenant, template, mailbox and user. The grant mutation, mandatory audit record and outbox event remain in the same transaction, and errors return before commit. This is source inspection; this catalog does not certify PostgreSQL behavior, concurrency, or the separate STREAM runtime tests.

Eight current source references are refreshed to the actual function positions and hashes. The grant entry receives the specific revocation review above. All other manual review fields, classifications, evidence levels, migration facts and historical runtime links remain unchanged.

The complete final comparison against revision 7 changes **61 caller entries**, comprising **79 location changes, nine added candidates and six removed candidates**. The records cover current template-preview, integer-setting, raw-object, retention, company-mail and middleware sources. These are name-match syntax candidates; the catalog makes no dispatch or runtime-equivalence claim. Complete before/after syntax records and caller transformations are bound in [reconciliation.json](reconciliation.json).

## Exact client, route and closure changes

The original client producer still reports **134 branches and seven explicit transport forwarders**. Exactly one client row changes: **index 39**, `POST /api/v1/company/mailboxes`, at `web/features/company/mailbox-admin.tsx`, moves from **line 48 to line 65**. Every other field, row and index is unchanged. No client is added, removed or reordered. STREAM-06's grants GET was already revision-7 index 17 and is not added again.

The total remains **132 registered routes**. All registration metadata, methods, handlers, middleware, schema and release identities are unchanged. **131 complete compatibility route records** are identical. At **route index 107**, `POST /api/v1/company/mailboxes`, only the embedded client row's line changes from 48 to 65; its route mapping remains the same.

The declared compatibility closure retains its exact **94-path set**. Exactly **three hashes** change:

- `docs/company-mail/evidence/R5-COMPATIBILITY-CURRENT-20261003/clients.json`, containing the reviewed one-line location change.
- `internal/api/middleware/ratelimit.go`, whose source now delegates the sliding-window operation to the shared helper.
- `web/features/company/mailbox-admin.tsx`, whose current source includes the reviewed retention input validation.

The other **91 path/hash pairs** remain identical. The changed `received-folder.tsx` contributes no direct client row and is outside this closure. It is explicitly recorded alongside the changed account page, send-policy component and new `internal/ratelimit/sliding.go` helper as a selected product path outside the declared closure. These four noted exclusions are not an exhaustive dependency list. The 94-path closure is a finite source inventory; it does not follow all imports or certify a compiler/runtime closure or the behavioral correctness of these product changes.

## Historical assertions and unchanged controls

All five original producer/validator files retain their exact bytes. The original source runner, preparation helpers, frozen baseline, budgets and refusal gates remain unchanged. `test_r5_compatibility.py` remains byte-for-byte unchanged, including its existing 134-client assertion.

All **17** earlier reconciliation method IDs remain, with the two revision-7 positive reviews bound to immutable revision-7 catalog/source objects. Two finite revision-8 methods bind the current transaction and client/closure changes, for **19** methods in total. All five original mutation methods retain identical source segments and ASTs and still exercise the current catalogs.

The original revision-5/6 exact grants-GET rejection assertions retain their original current-producer path; a fresh `gate.collect()` run confirmed both exact rejections on e0. The final e0 source separately proves the exact mailbox-POST rejection of stale revision-7 catalogs. The preserved 53 checkpoint also retains its unchanged-client/closure assertions and original actual command results. No test is removed, skipped, assigned a longer timeout, or redirected to fabricated success evidence.

## Actual verification and limits

At the 53 checkpoint, the three current static CLIs passed, **79 selected Python tests passed** without failure/error/skip, and the **27 original Node client tests passed** without failure/skip. Its complete unchanged current/frozen runner then exited **1** after **109.897 seconds**, at original preparation's `Linux pinned executable FD unavailable` check. The TypeScript preparation, before/after Go selections and fixture compilation completed, but the compiled binary did not execute. The runner discovered **770 methods**, assigned **766 current plus four frozen-v1**, and executed **zero in either group**; `no_missing=false`, `no_overlap=true`. That actual failure is preserved, including the report hash and five retained preparation-file hashes.

After e0 was integrated with the unchanged 53-checkpoint catalogs, all three original CLI commands failed on genuine new source drift. Those final-phase baseline failures are recorded separately from the earlier 53 baseline.

The three final e0 original CLI checks each exited **0**. The four complete selected Python modules first ran **79 tests: 79 passed, no failures, errors or skips** (transaction 26, compatibility 19, reconciliation 19 and current-source-inventory 15). The original Node client module ran **27 tests: 27 passed, no failures or skips**.

After review, the reconciliation test was adjusted to read the published aggregate checkpoint instead of local-only Git objects, and the unnecessary historical source-root adapter was removed. The complete final reconciliation module was then rerun: **19 tests passed**, including all five original mutation controls, with no failures, errors or skips. The four catalogs, other 60 selected methods and CLI/Node inputs were unchanged. These are **98 actual Python method executions covering 79 distinct final methods**, recorded as two separate batches; they are not described as a single run on unchanging test bytes. The final six catalog/test inputs are fixed by the packet's SHA-256 manifest.

The complete unchanged current/frozen runner was actually attempted at final catalog candidate `cd7dfbb503c4fcfb3f3c175367b9f60f5c0ffbed`, tree `7f7301e4fa80857e89a3659f71a0f5312df08e74`. It exited **1** after **96.321 seconds** during original preparation, with the precise error `[Errno 13] Permission denied: '/proc/6/fd/3'`. The helper completed TypeScript preparation, before/after Go selections and compilation of the 30,045,976-byte fixture binary, then was denied access at the mandatory Linux `/proc` descriptor boundary before executing that binary.

This final attempt discovered **770 methods**, assigned **766 current plus four frozen-v1**, and executed **zero in either group**. No typed wire fixture ran. All 770 methods remain missing; `no_missing=false`, `no_overlap=true`. The actual report and five preparation-file hashes are recorded separately from the 53 checkpoint. Neither failure has been reclassified as a pass, and the original FD guard, helpers, budgets and refusal behavior remain unchanged. The complete source gate must be satisfied separately in a supported CI environment; selected module passes do not replace it.

After this final attempt, all six catalog/test manifest inputs and the portable 53 aggregate packet remain byte-identical. Only the README and execution summary were finalized. No additional TODO, runtime, wire, dependency, release or parent-task acceptance is implied.
