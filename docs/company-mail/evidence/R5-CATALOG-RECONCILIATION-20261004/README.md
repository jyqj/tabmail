# Reviewed source catalog reconciliation, revision 2 — 2026-10-04

Base/source: `3f34c31ed51a721b318a76512805e4a14dc292c3`. Branch: `codex/r5-approved-catalog-reconciliation-20261004`. This corrects two proven static inventory drifts only. Transaction and compatibility gates retain their original validators and real collectors. It grants no new runtime, external-runtime policy, dependency, product or task qualification.

The independent report at `5b7368286b292ed540293bcb04d1d4c1bdc2e1c4`, `docs/company-mail/evidence/R5-ENV-EVIDENCE-INDEPENDENT-20261004/README.md`, was read from that exact Git object. Its f52/df6 comparison established identical actual AST/source facts and the two inherited errors. This checkout independently reproduces both errors with the old inventories. Historical failures, including the original formal default missing/setup failures, original formal component 17 pass / 9 fail / 17 missing / zero qualified, shared receipt failures and independent environment review, remain unchanged. Correcting inventory freshness cannot retroactively repair or qualify those runs.

## Catalog contracts and preservation

[Versioning](../../../VERSIONING.md), [PR23 inventory review](../../R5-TRANSACTION-INVENTORY-PR23-20261003.md), [compatibility contracts](../../R5-COMPATIBILITY-GATES.md), both validators, their tests and the Go AST/route and Node collectors were read. There is no applicable root/scripts AGENTS.md or repository skill; web/AGENTS.md governs untouched web work. There is no transaction/compatibility auto-refresh generator contract: the existing collectors supply facts and the validators compare them to manually reviewed authoritative data. No validator, producer or automatic acceptance path changes.

The transaction schema remains `r5-transaction-coverage-v1`; compatibility remains schema 2 `source_inventory_and_upgrade_plan`. Those describe data shape. Both current authoritative paths now have explicit **inventory revision 2**, with exact source commit, previous snapshot path/SHA256 and static-only qualification. No schema or unrelated inventory policy is promoted. The old complete files are copied byte for byte as [transaction revision 1](historical-transaction-inventory-revision1.json) and [compatibility revision 1](historical-compatibility-inventory-revision1.json). Their original pins, reviews, acquisition and qualification remain historical truth. The earlier compatibility historical-wire v1 map, pinned SHA, wire summary, observations, source manifests and historical qualification are unchanged.

Current paths remain `docs/company-mail/evidence/R5-TRANSACTION-COVERAGE.json` and `docs/company-mail/evidence/R5-COMPATIBILITY-GATES.json`, so existing consumers retain their authoritative entry points. The transaction baseline/review-base is updated to the explicit inspected source; unchanged per-entry historical review bases stay historical. Compatibility's original acquisition records the acquisition of unchanged route/client facts; revision metadata records this later two-hash reconciliation separately.

## Transaction drift: exact LF policy delta

The old catalog production file hash matches the source before `23228449254bc0312171640a637e359187169192`. That is the only subsequent change to `employee_disposition.go` through the pinned base. Its reviewed contract and red/green evidence are [Offboarding successor authorization](../../OFFBOARDING-SUCCESSOR-AUTHZ-20261004.md) and its linked safe JSON. This task reads that evidence; it does not rerun or inherit its HTTP/PG results.

| Fact | Old reviewed source | Current reviewed source |
| --- | --- | --- |
| Distinctness | target differs from successor/current caller; nil IDs rejected | additionally successor differs from current caller |
| Successor qualification | tenant-scoped `SELECT is_active ... FOR SHARE`, scan activity | same tenant/fence, `SELECT is_active,role ... FOR SHARE`, scan activity and role |
| Policy | target `CanManageTenantMember`, active successor | target policy unchanged; active successor must also satisfy existing `authz.CanManageTenantMember(a, a.TenantID, role)` |
| Failure | existing activity/missing/target denials | caller-self 400; unmanageable successor 403 before effects |
| Lock order | comment omitted current caller and execute plan | comment names tenant -> refreshed caller -> execute plan -> target -> successor -> active jobs -> mailboxes |
| Borrowed transaction | no new commit/transaction/lock | unchanged; role check uses the already held successor SHARE lock |

This implements the accepted LF selected-tenant/current-member policy, rather than defining a new role hierarchy. Ordinary admins cannot give custody to peer admins/super-admins; current super-admin policy continues to use the validated selected tenant. Preview, pending execute, qualified completed replay and legacy one-step command share this validator. Frozen-target planner eligibility and legacy inactive-target restrictions remain distinct; no reactivation, plan/fingerprint/CAS/epoch, lease, queue or replay policy changes are introduced here.

Exact file hashes and every changed entry ID/field are in [reconciliation.json](reconciliation.json). There is **one changed function body** (`offboardingSubjectsTx`), **11 changed syntax entries** (the other ten are location changes), **16 changed caller lists**, **19 entries affected in total**, and **one changed PG file hash**. Every changed caller candidate originates in this same reviewed production file; candidate identities remain unchanged; locations move, and the successor QueryRow Scan expression additionally reflects the reviewed `is_active,role` query. All other candidate expressions are unchanged. The function set, heuristic classification, migration definitions/hashes, reviewed file-type coverage and `historical_review_metadata` are unchanged. Explicit SQL/lock/operation source locations and the changed qualifier's manual role/trace/body hash are reconciled with actual AST facts. Evidence stays source-only; historical linked results elsewhere retain their old scope.

Current scoped counts stay **62 PG files / 395 functions / 498 SQL execution calls / 134 direct-write candidates / 154 call-name write-closure candidates / 19 migrations**. Name matches remain candidates, not resolved dispatch. Dynamic SQL/package constants, runtime waits, audit rollback and universal deadlock safety remain independently unqualified.

## Compatibility drift: full hash provenance, including earlier changes

The old stored hashes match the files at compatibility inventory commit `006ff6939edbe5f9fa5732cd1ab1a2efb8ece4cf`. It would be incorrect to describe the component hash change as only a PE setup edit: all intervening file commits were inspected. [source-change-chain.json](source-change-chain.json) records exact commit/file before/after hashes and evidence-document references.

| Reviewed commit / evidence | Old -> new semantic facts in the component observation file |
| --- | --- |
| `dc350c3e6c66553cf3a16b9771abb091cd6de1a8`, [external candidate v1](../../R5-EXTERNAL-DEPENDENCY-RUNTIME-V1.md) | source-local Node/Vitest -> independently pinned external manifest, regular executable hashes, explicit policy/status/source identity and launcher validation; candidate remains UNADOPTED |
| `7beb93faf99fd50323edb4ddd00ed19a3e6ab405`, [candidate v2](../../R5-EXTERNAL-DEPENDENCY-RUNTIME-V2.md) | manifest policy v1 -> v2; fixed `--cache=false --experimental.fsModuleCache=false`; shared command builder; original configs/fixture75 retained |
| `20b51d48c4a849615817ee461699d31fe3e0cb4c`, [closed RC contract](../../R5-RC-RECEIPT-CONTRACT-20261003.md) | old private canaries/subject-oriented fixture oracle -> subject/all address/body canaries plus independent aggregate counts/state; replay expected pending, list/detail sent; safe closed receipts do not expose content |
| `5b2715332d56c113e69a51bab2728f601ec0a82a`, [PE05 owner join](../../R5-FIXTURE-OWNER-JOIN-20261004.md), [explicit batch](../../R5-EXTERNAL-BATCH-RUNTIME-V1.md) | detached barrier/revoker cleanup -> synchronous owned cancel/release/join with retained errors and only NoRows polling retry; observation body extracted into executor callback usable by original consumer and separate batch bridge; child exit is an interface; assertions, exact identity, markers and original same-key/LF replay checks retained |
| `c8df5721bc3411d23d1e3b5ba4be917893d65fc2`, [PE seed fix](../../R5-PE-SEED-FIX-20261004.md) | legacy PUT `/permissions` expecting 200 (shipping deliberately returns 409) -> real GET/PATCH/GET `/permission-editor`, observed compound revision, explicit false/quota19/list-zone patch and independent raw override/source/revision readback; no changed case/marker or weakened CAS |
| `850f067cf41e082d4134b1b58cccf17a27ce51bd`, [transport diagnosis](../R5-TRANSPORT-DIAGNOSTIC-20261004/report.json) | recorder buffering every response -> real socket writer for ordinary/SSE routes, bounded JSON observation, digest of successfully written bytes, Unwrap/flush/deadline support, owned transport cancellation and request join; only authorized lost-submit-response fault remains buffered |

The other closure file changes at `6138113c1896cb810db567605154b5023f0340de`, integrated through `1150633a4948c873349743658fad2661b08f1240`: [shared receipt/legacy revision 2](../R5-SHARED-RECEIPT-LEGACY-V2-20261003/README.md), R5-PROTOCOL appendix AB. RC02 previously required the expired submission subject; it now forbids subject alongside body/header/BCC, verifies original draft/mailbox/user identity and closed public aggregate receipt metadata, retaining same ID/one job. BC03 previously created a current migration-17 COMPLETE snapshot then removed its source and incorrectly expected legacy_unknown; unprovable-source cases now use an explicitly historical version0/NULL/unknown persisted asset and strict unknown/BCC checks. Reliable-source branch remains. New helper tests live outside this declared closure; changing these two stored hashes does not attest a complete compiler/runtime dependency closure.

All these bytes already exist at the pinned base. The inventory records source-bound test symbol presence and bytes; it executes none of these components or fixture policies. The independent report's exact two drift paths/observed hashes match the fresh result. There are **132 routes / 134 client branches / 94 declared closure files**. Route facts, middleware/conditions, OpenAPI/DTO/client/test bindings, upgrade batches, successor task/rejection plans, legacy usage unknowns, missing OpenAPI operations and historical-wire references are unchanged. Only the two actual test-source hashes and additive revision metadata change. No runtime receipt is re-signed; `task_complete=false`, `product_green=false`, and current wire remains required.

## Verification and negative controls

Pinned tool: existing Go1.25.7 Linux/amd64 at `/workspace/tabmail-cloud/tools/go/bin/go`; existing Node24.19.0 and TypeScript5.9.3. Owned isolated build cache `/tmp/r5-catalog-reconcile-20261004/cache`, existing module cache `/workspace/tabmail-cloud/gomod`, GOPROXY off. No dependency installation/update or service/PG startup. Both forks and root/web locks remain byte-identical.

```sh
export R5_TEST_GO=/workspace/tabmail-cloud/tools/go/bin/go
export R5_TEST_CACHE=/tmp/r5-catalog-reconcile-20261004/cache
export R5_TEST_MODULECACHE=/workspace/tabmail-cloud/gomod GOPROXY=off
export PYTHONDONTWRITEBYTECODE=1 PYTHONPATH=scripts:scripts/tests
python3 -B -m unittest -v test_r5_transactions test_r5_compatibility test_r5_catalog_reconciliation
python3 -B scripts/check_r5_transactions.py
python3 -B scripts/check_r5_compatibility.py
```

Go compilation/execution is limited to `cmd/r5txinventory` and the source-only `./internal/architecture -run '^TestR5RouteInventory$'` producer with its exact readonly/count1/timeout60 subprocess contract. AST extraction retains timeout90. No formal default/sharedDB/HTTP/components/wholePG180 producer, preparation, selected-source capture, service, actual email or product binary runs.

The old unmodified 45-test general run has 43 passes and the two expected inherited errors. The first 50-test revision run has 49 passes and one historical-PR23 assertion failure: that assertion compared the changed LF qualifier to its old PR23 body. Its evidence is retained. The narrow test correction verifies all 78 original PR23 reviews against the byte-pinned historical inventory, then independently asserts the current LF review/hash/base while still requiring every other body review and every classification/evidence level unchanged. No test is removed or skipped. A subsequent 50-test run passes. The final route-inclusive result and CLI outputs are recorded in [verification.json](verification.json).

New controls use original validators and actual source bytes: old snapshots must reject the same current approved facts; current revision must pass; a copied production tree with successor-self rejection removed must fail actual AST comparison; a file-only comment must fail the file hash fence; an extra SQL execution AST fact must fail; altered actual closure-file bytes must fail; fabricated route facts must fail. The original Go route test additionally passes on copied original route inputs and fails when the actual `/docs-assets/*` source route changes to `/unreviewed-docs-assets/*`. These mutations are made in disposable copies, never in candidate sources. All pre-existing transaction/compatibility negative and historical anti-laundering cases run against the corrected current catalog and remain green as rejection controls.

Private logs/fresh syntax JSON remain under the owned mode700 `/tmp/r5-catalog-reconcile-20261004`; only safe static counts, hashes, deltas and this report are published. No fake source hashes are pinned. The baseline/failure snapshots keep their real stale pins.

## Proposed central TODO entry for parent integration

- Reconciled the two independently proven pre-existing transaction/compatibility static inventory drifts at base `3f34c31ed51a721b318a76512805e4a14dc292c3`, with explicit content revision 2 and byte-preserved revision-1 snapshots. LF current-successor policy and the complete two-test-source hash history are traced in this report. Focused results: see verification.json; source-only, no new runtime or parent acceptance.
- Central **10/171 unchanged**. Original default/setup/component/shared-wire/wholePG180/audit failures, missing layers and external/selected-source qualification blockers remain open. Existing historical receipts never transfer to this source. Parent independent review and any later formal-run authorization remain required.

Shared R5-TODO.md is intentionally not edited under the concurrency clarification. No selected binding v2/v3, environment/diagnostic helper, runner, runtime/preparation, default dispatch, product policy, fork, lock, budget, case JSON or marker path changes. Normal branch commit/push is authorized; parent owns PR creation. No PR CLI/API, merge, deployment or denied-action retry is performed here.
