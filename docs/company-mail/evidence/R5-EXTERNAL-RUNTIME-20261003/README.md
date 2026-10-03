# External runtime candidate checkpoint, 2026-10-03

Implementation/proposal base: `cf2c2cdd783d1484c5a964404bf90c4bc4c80d3f`.
Product base: `ee3308fd3217246c9bdd43b07ae0609ebae6aeb6`.
Actual source freeze: `9c04feda833d1b3195f96ff2b10285fa1907fe7a`.
The later evidence commit adds safe documentation only, without changing tested
consumer/helper/config/test bytes. Candidate policy remains **UNADOPTED**.

## Independent acceptance

30 preparation/runtime controls and 70 existing protocol regression tests pass.
Four additional real-inventory controls reject independently rehashed content
roots, CLI hashes and source descriptors, and a real archive receipt with the
wrong producer tag context. The original selected-v2 validators revalidated exact
fresh default and race-r5protocol receipts independently. The protocol archive
receipt uses the required union tag context `[[], ["r5protocol"]]`; selected
contexts retain distinct source identities. Go remains 1.25.7, both local replaces
and the original package/lock bytes remain unchanged.

Normal lifecycle-enabled npm ci installed 760 packages from the fixed official
lock in the owned external root. Acceptance pins 45,584 regular files, 4,614
directories, 41 declared bin links and 96 explicitly absent optional lock records.
Bundled records retain enclosing locked registry provenance; lifecycle outputs
are observed installed bytes, without independent tarball-member attribution.
Exact manifest/receipt/content roots and private artifact hashes are in summary.

Development-time refusals are retained privately: inherited NODE_PATH/npm
configuration, an over-strict system-tool ownership check, selected metadata
changing during compile-only preparation, a mismatched Python discovery path,
and an insufficient protocol tag context. The latter stopped before any Go/Node
consumer execution. These refusals do not qualify runtime or explain historical
VCS error 128. The finalized acceptance and actual run use the exact same final
manifest pin `0f9028da18593f5001f4886e71266451d04fa59c53ee17b192eedd01b0dbdb36`.

## Actual formal shared-components producer

One producer launched real consumer tests from the final fixed identity against
its own PostgreSQL 17 instance on loopback. Existing synthetic `.test` fixtures
and loopback HTTP were used; no real mail, DNS/provider, merge or deployment.
Go commands retain race, count1, timeout120s and per-process180s; the existing
per-fixture Node context remains75s. Credential and delivery Go producers pass;
the handlers producer fails. The direct Python Vitest consumer does not run
because the Go component producer fails; it is not credited as runtime proof.

The bound external Vitest CLI actually loads the original config and executes
`R5 protocol component RC01 default secure behavior`. The original assertion
at `web/components/company/r5-protocol-shared-components.test.tsx:126:18` fails
while waiting for the Attachments label. Actual normalized HTTP metadata records
GET `/api/v1/company/submissions/<fixture-id>` returning200. No declared target
marker appears, so this is an unclassified original-case failure, not accepted
target red. The report/failure/observation hashes are preserved; the test and
shipping business code were not edited to turn it green.

Vitest also creates `source/web/node_modules/.vite/vitest/.../results.json`.
The terminal no-follow inventory rejects this as **shadow source node_modules**;
subsequent component launches fail closed. The pinned external dependency tree
has no added, removed or changed entries. This result exposes a remaining runtime
integration blocker: source-local tool cache creation must be prevented by a
separately validated supported invocation before this candidate can qualify.
No archive exception, dependency/source link, NODE_PATH workaround or permission
change admits that cache. No retry silently cleans/promotes the failed receipt.

Final status is **run_failed**, task_complete=false, product_green=false. All46
cases' final missing required layers remain explicitly recorded. Passing two
unit producers is not a product-green or whole-protocol result. Complete dynamic
ESM/CJS/Vite/worker/jsdom loader resolution remains unknown; the actual TSX/jsdom
assertion demonstrates scoped execution only. Central10/171 remain open and old
VCS error128 remains unexplained history.

Trust boundary remains one executor's owned physically isolated workspace:
**not_qualified_for_hostile_concurrent_mutation**. Descriptor validation detects
observed changes, and exclusive owner tokens serialize cooperating consumers;
no OS-enforced immutable lease is claimed.

## Safe deliverables

Only this README and a metadata/hash summary are published. No response payload,
fixture token, dependency body or binary is included. Full manifests, original
Go/Vitest reports, observations and logs remain private at
`/workspace/r5-runtime/`. Final fixed source remains there for inspecting the
observed runtime artifact; it is no longer ready for another qualified run.
