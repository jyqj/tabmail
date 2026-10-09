# Accepted PE/LF union and stopped combined gate — 2026-10-04

Fixed product/test source: `925264f1293120b70bfb1c6e8a865b65b1c5cad0`. Base: `f875672dea2b68fdec9e0b986f4863724da5d7ba`. **Combined gate FAILED; formal NOTRUN (zero attempts).**

Normal origin fetch obtained the exact supplied commits. Complete production diffs and both independent reports were read. Exact deltas were cherry-picked without whole-file stale-source overlays. PE carries seed d9a56 and transport f027 (imported by 833cbf1); LF production blobs remain exactly 52554e543a3aef7c9d28cebb149d3411e286a515 and 619feb36f23f6f3039cbd8c8230a4bcd7b809fd7. Historical evidence46de and all old rejected receipts are retained. Unsafe/baseline findings remain historical, not cleared by this integration. No applicable skill required an additional workflow; web/AGENTS.md and installed Next documentation were read.

The only mechanical test correction changes the old LF UI-only peer-successor HTTP200 gap expectation to HTTP403 required by the accepted backend fix. Original author evidence is unchanged. No production policy, formal checker/catalog/marker, budgets, dependencies, module replacements or assertions were weakened. PE03/04 retain genuine targeted invalidation, dirty intent, disabled Save, zero UI PATCH, and separately labelled direct stale-CAS409/no restoration.

## Combined focused outcomes

| Check | Outcome |
| --- | --- |
| Exact imported source union and all other baseline tracked bytes | PASS; 53 paths, one documented test correction |
| PE real owned PG/router/UI + seed/CAS + transport + unmount | PASS; 6 top-level tests, 69.630s |
| LF author selector/negative + backend tests with race | PASS; 10 top-level tests |
| Independent pair with race | FAIL; 9 top-level tests rejected before product assertions |
| TypeScript noEmit / focused ESLint | PASS / PASS |
| UI/oracle unit tests | 30 PASS |
| Existing protocol evidence guards | 70 PASS |

The executor initialized a new owned PG17 cluster at loopback 55459, but the unchanged independent pair guard explicitly requires 55447. Every independent pair failure is `review-owned cluster required` at its `pairSeed` entrance. This is an executor setup mismatch, not an observed policy regression. The combined gate is nevertheless failed. Under the explicit no-ad-hoc-retry instruction, no replacement cluster, retry, budget change or assertion/marker change was attempted.

Formal preparation did not begin: no fresh external official-lock root, runtime-v2 capture, components contract/pin, lease, formal parent/child or fixture-owner acknowledgement exists. The authorization prerequisite was not met. Every new formal terminal/layer below is NOTRUN and unqualified; this does not replace the old rejected 17 pass / 9 fail / 17 missing / zero-qualified receipt or count as the full 46 catalog.

## Full production/test/fixture/evidence union

| Path | Accepted source | Exact |
| --- | --- | --- |
| `docs/company-mail/LF01-SELECTOR-REGRESSION-20261004.md` | `648938a` | yes |
| `docs/company-mail/OFFBOARDING-SUCCESSOR-AUTHZ-20261004.md` | `062c5ba` | yes |
| `docs/company-mail/PAIR-INDEPENDENT-REVIEW-20261004.md` | `7b30bbe` | yes |
| `docs/company-mail/PE-INDEPENDENT-REVIEW-20261004.md` | `4502cf3` | yes |
| `docs/company-mail/PE-SHARED-ADAPTER-MIGRATION-20261004.md` | `4502cf3` | yes |
| `docs/company-mail/R5-EXTERNAL-BATCH-G1-CLEANUP-FIX-20261004.md` | `4502cf3` | yes |
| `docs/company-mail/R5-FORMAL-COMPONENTS-F875-20261004.md` | `4502cf3` | yes |
| `docs/company-mail/R5-PE-SEED-FIX-20261004.md` | `4502cf3` | yes |
| `docs/company-mail/R5-STREAMING-OBSERVER-INTEGRATION.patch` | `4502cf3` | yes |
| `docs/company-mail/evidence/LF01-SELECTOR-REGRESSION-20261004.json` | `648938a` | yes |
| `docs/company-mail/evidence/OFFBOARDING-SUCCESSOR-AUTHZ-20261004.json` | `062c5ba` | yes |
| `docs/company-mail/evidence/PAIR-INDEPENDENT-20261004/baseline-http.log` | `7b30bbe` | yes |
| `docs/company-mail/evidence/PAIR-INDEPENDENT-20261004/baseline-ui.log` | `7b30bbe` | yes |
| `docs/company-mail/evidence/PAIR-INDEPENDENT-20261004/cleanup.log` | `7b30bbe` | yes |
| `docs/company-mail/evidence/PAIR-INDEPENDENT-20261004/eslint.log` | `7b30bbe` | yes |
| `docs/company-mail/evidence/PAIR-INDEPENDENT-20261004/exact-production-union.patch` | `7b30bbe` | yes |
| `docs/company-mail/evidence/PAIR-INDEPENDENT-20261004/final-fixed-race.log` | `7b30bbe` | yes |
| `docs/company-mail/evidence/PAIR-INDEPENDENT-20261004/initial-fixed-race.log` | `7b30bbe` | yes |
| `docs/company-mail/evidence/PAIR-INDEPENDENT-20261004/manifest.json` | `7b30bbe` | yes |
| `docs/company-mail/evidence/PAIR-INDEPENDENT-20261004/typescript.log` | `7b30bbe` | yes |
| `docs/company-mail/evidence/PAIR-INDEPENDENT-20261004/vet.log` | `7b30bbe` | yes |
| `docs/company-mail/evidence/PE-INDEPENDENT-REVIEW-20261004/report.json` | `4502cf3` | yes |
| `docs/company-mail/evidence/PE-SHARED-ADAPTER-FOCUSED-20261004/report.json` | `4502cf3` | yes |
| `docs/company-mail/evidence/R5-EXTERNAL-BATCH-G1-CLEANUP-FIX-20261004.json` | `4502cf3` | yes |
| `docs/company-mail/evidence/R5-FORMAL-COMPONENTS-F875-20261004/report.json` | `4502cf3` | yes |
| `docs/company-mail/evidence/R5-PE-SEED-PROBE-20261004/cleanup.raw.log` | `4502cf3` | yes |
| `docs/company-mail/evidence/R5-PE-SEED-PROBE-20261004/setup.raw.log` | `4502cf3` | yes |
| `docs/company-mail/evidence/R5-TRANSPORT-DIAGNOSTIC-20261004/report.json` | `4502cf3` | yes |
| `internal/api/handlers/pe_independent_review_test.go` | `4502cf3` | yes |
| `internal/api/handlers/pe_shared_adapter_focused_test.go` | `4502cf3` | yes |
| `internal/api/handlers/r5_protocol_component_observations_test.go` | `4502cf3` | yes |
| `internal/api/handlers/r5_protocol_permission_seed_test.go` | `4502cf3` | yes |
| `internal/api/handlers/r5_protocol_transport_test.go` | `4502cf3` | yes |
| `internal/store/postgres/employee_disposition.go` | `062c5ba` | yes |
| `internal/store/postgres/lf01_selector_regression_test.go` | `648938a` | documented 200→403 test correction |
| `internal/store/postgres/offboarding_successor_authz_test.go` | `062c5ba` | yes |
| `internal/store/postgres/pair_independent_review_test.go` | `7b30bbe` | yes |
| `scripts/reviews/pe_independent_mutations_20261004.py` | `4502cf3` | yes |
| `web/components/company/pe-independent-review.test.ts` | `4502cf3` | yes |
| `web/components/company/pe-independent-unmount.probe.tsx` | `4502cf3` | yes |
| `web/components/company/r5-permission-contract-oracle.test.ts` | `4502cf3` | yes |
| `web/components/company/r5-permission-contract-oracle.ts` | `4502cf3` | yes |
| `web/components/company/r5-protocol-shared-components.test.tsx` | `4502cf3` | yes |
| `web/components/company/r5-streaming-fetch-observer.probe.ts` | `4502cf3` | yes |
| `web/components/company/r5-streaming-fetch-observer.ts` | `4502cf3` | yes |
| `web/features/company/offboarding-panel.test.tsx` | `648938a` | yes |
| `web/features/company/offboarding-panel.tsx` | `648938a` | yes |
| `web/lf01-owned.probe.tsx` | `648938a` | yes |
| `web/pair-review.probe.tsx` | `7b30bbe` | yes |
| `web/vitest.lf01.config.ts` | `648938a` | yes |
| `web/vitest.pair-review.config.ts` | `7b30bbe` | yes |
| `web/vitest.pe-independent.config.ts` | `4502cf3` | yes |
| `web/vitest.r5transport.config.ts` | `4502cf3` | yes |

Exact per-path Git blobs/SHA256s are in [source-guards.json](evidence/PE-LF-COMBINED-GATE-20261004/source-guards.json). Aggregate results, raw evidence hashes and full matrix are in [report.json](evidence/PE-LF-COMBINED-GATE-20261004/report.json). Raw test logs/fixtures remain private outside source under `/tmp/tabmail-pe-lf-integration`; no credential/response packet is published.

## New formal per-terminal matrix

| Terminal | Outcome | Qualified |
| --- | --- | --- |
| `TestR5ProtocolAuditReasonSharedCases/OP03/eight` | NOTRUN | false |
| `TestR5ProtocolAuditReasonSharedCases/OP03/empty` | NOTRUN | false |
| `TestR5ProtocolAuditReasonSharedCases/OP03/maximum` | NOTRUN | false |
| `TestR5ProtocolAuditReasonSharedCases/OP03/over_maximum` | NOTRUN | false |
| `TestR5ProtocolAuditReasonSharedCases/OP03/seven` | NOTRUN | false |
| `TestR5ProtocolAuditReasonSharedCases/OP03/trim_maximum` | NOTRUN | false |
| `TestR5ProtocolAuditReasonSharedCases/OP03/trim_seven` | NOTRUN | false |
| `TestR5ProtocolAuditReasonSharedCases/OP03/utf8_nine` | NOTRUN | false |
| `TestR5ProtocolAuditReasonSharedCases/OP03/utf8_six` | NOTRUN | false |
| `TestR5ProtocolAuditReasonSharedCases/OP03/utf8_twelve` | NOTRUN | false |
| `TestR5ProtocolAuditReasonSharedCases/OP03/whitespace` | NOTRUN | false |
| `TestR5ProtocolComponentObservations/LF01/default` | NOTRUN | false |
| `TestR5ProtocolComponentObservations/LF02/default` | NOTRUN | false |
| `TestR5ProtocolComponentObservations/LF03/default` | NOTRUN | false |
| `TestR5ProtocolComponentObservations/LF04/default` | NOTRUN | false |
| `TestR5ProtocolComponentObservations/LF05/foreign_tenant` | NOTRUN | false |
| `TestR5ProtocolComponentObservations/LF05/frozen` | NOTRUN | false |
| `TestR5ProtocolComponentObservations/LF05/higher_role` | NOTRUN | false |
| `TestR5ProtocolComponentObservations/LF05/self` | NOTRUN | false |
| `TestR5ProtocolComponentObservations/LF06/default` | NOTRUN | false |
| `TestR5ProtocolComponentObservations/LF07/default` | NOTRUN | false |
| `TestR5ProtocolComponentObservations/PE01/default` | NOTRUN | false |
| `TestR5ProtocolComponentObservations/PE02/0` | NOTRUN | false |
| `TestR5ProtocolComponentObservations/PE02/[]` | NOTRUN | false |
| `TestR5ProtocolComponentObservations/PE02/false` | NOTRUN | false |
| `TestR5ProtocolComponentObservations/PE02/null` | NOTRUN | false |
| `TestR5ProtocolComponentObservations/PE02/omitted` | NOTRUN | false |
| `TestR5ProtocolComponentObservations/PE03/default` | NOTRUN | false |
| `TestR5ProtocolComponentObservations/PE04/default` | NOTRUN | false |
| `TestR5ProtocolComponentObservations/PE05/default` | NOTRUN | false |
| `TestR5ProtocolComponentObservations/RC01/default` | NOTRUN | false |
| `TestR5ProtocolComponentObservations/RC02/legacy_outbound_detail` | NOTRUN | false |
| `TestR5ProtocolComponentObservations/RC02/legacy_outbound_list` | NOTRUN | false |
| `TestR5ProtocolComponentObservations/RC02/submit_replay` | NOTRUN | false |
| `TestR5ProtocolComponentObservations/RC03/default` | NOTRUN | false |
| `TestR5ProtocolComponentObservations/RC04/default` | NOTRUN | false |
| `TestR5ProtocolComponentObservations/RC05/default` | NOTRUN | false |
| `TestR5ProtocolDeliverySharedCases/RC03` | NOTRUN | false |
| `TestR5ProtocolDeliverySharedCases/RC04` | NOTRUN | false |
| `TestR5ProtocolDeliverySharedCases/RC05` | NOTRUN | false |
| `R5 shared receipt RC03` | NOTRUN | false |
| `R5 shared receipt RC04` | NOTRUN | false |
| `R5 shared receipt RC05` | NOTRUN | false |

## New formal per-case layer matrix

| Case | Required layers still missing | Formal |
| --- | --- | --- |
| CT01 | db, http | NOTRUN |
| CT02 | db, http | NOTRUN |
| CT03 | db, http | NOTRUN |
| CT04 | db, http | NOTRUN |
| CT05 | db, http | NOTRUN |
| CT06 | db, http | NOTRUN |
| CT07 | db, http | NOTRUN |
| CT08 | db, http | NOTRUN |
| CT09 | db, http | NOTRUN |
| CT10 | db, http | NOTRUN |
| RC01 | db, http, components | NOTRUN |
| RC02 | db, http, components | NOTRUN |
| RC03 | db, http, components | NOTRUN |
| RC04 | db, http, components | NOTRUN |
| RC05 | db, http, components | NOTRUN |
| OP01 | db, http | NOTRUN |
| OP02 | db, http | NOTRUN |
| OP03 | db, http | NOTRUN |
| OP04 | db, http | NOTRUN |
| RT01 | db, http | NOTRUN |
| RT02 | db, http | NOTRUN |
| RT03 | db, http | NOTRUN |
| RT04 | db, http | NOTRUN |
| RT05 | db, http | NOTRUN |
| RT06 | db, http | NOTRUN |
| RT07 | db | NOTRUN |
| BC01 | db, http | NOTRUN |
| BC02 | db, http | NOTRUN |
| BC03 | db, http | NOTRUN |
| BC04 | db, http | NOTRUN |
| BC05 | db, http | NOTRUN |
| BC06 | db, http | NOTRUN |
| GC01 | db | NOTRUN |
| GC02 | db | NOTRUN |
| LF01 | db, http, components | NOTRUN |
| LF02 | db, http, components | NOTRUN |
| LF03 | db, http, components | NOTRUN |
| LF04 | db, http, components | NOTRUN |
| LF05 | db, http, components | NOTRUN |
| LF06 | db, http, components | NOTRUN |
| LF07 | db, http, components | NOTRUN |
| PE01 | db, http, components | NOTRUN |
| PE02 | db, http, components | NOTRUN |
| PE03 | db, http, components | NOTRUN |
| PE04 | db, http, components | NOTRUN |
| PE05 | db, http, components | NOTRUN |

## Cleanup and remaining work

The independently queried owned cluster returned `0|0` fixture databases/backends before successful fast shutdown. PE fixtures additionally acknowledged closed HTTP listeners, zero databases/backends, and released streams. Formal process/owner/lease cleanup is NOTRUN, not fabricated. Existing infrastructure history still distinguishes physical process cleanup from fixture-owner acknowledgement.

Combined acceptance remains blocked by the pair setup guard; a future gate/formal attempt needs renewed root authorization. default675/sharedDB89e7, wholePG180/audit, central10/171, broader races and required catalog layers remain excluded/open. No actual email, merge or deployment occurred.
