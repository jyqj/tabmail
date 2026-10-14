# Receipt surface acceptance correction, 2026-10-03

Base: `4b2eb09c18adb50aa128af1907b53820e7305f95`.
Prior formal code freeze: `20b51d48c4a849615817ee461699d31fe3e0cb4c`.
This is a narrow TSX/oracle/unit correction. The [v1 failure](../R5-EXTERNAL-RUNTIME-20261003/README.md)
and [v2 failure](../R5-EXTERNAL-RUNTIME-V2-20261003/README.md), including their
original packets/hashes, remain unchanged. V2 RC01/RC03 completed real
Go/Vitest/HTTP passes; all three RC02 variants failed at the list capabilities
assertion, before detail. The handler then hit timeout120; incomplete RC04 and
later missing runtime are uncredited.

## Source contract and correction

R5-DESIGN sections 5.2/5.3 require the same content authority for compatibility
and replay and safe full-ledger aggregates without private fields.
`internal/app/submissions/receipts.go` separates receipt surfaces:
`ListOutboundReceiptViews` calls `projectReceipt(..., false)` for each row from
one authorized snapshot, with no per-row retry I/O. Capabilities start with the
store's content decision, retry=false and reason=unknown. Detail and replay call
`projectReceipt(..., true)` and retain uncertainty, ledger, job-state, current
sender/retry authority and outbound authorization checks. Read visibility does
not grant current sender authority.

The TSX assertion previously used the same current-retry expectation for every
surface. It now requires an explicit list/detail/replay argument at each call.
The acceptance-only oracle derives expected capabilities from that surface and
the existing seed/original case; it never consumes actual capabilities. List
requires false/unknown exactly, retaining the seed's view_content=false. Detail
and replay keep every original exact capability. Status fallback also uses the
seed state rather than the returned state.

| Case / variant | Surface | Seed state | Status | view_content | retry | retry_block_reason |
| --- | --- | --- | --- | --- | --- | --- |
| RC01/default | detail | sent | needs_attention | false | false | state_not_retryable |
| RC02/legacy_outbound_list and legacy_outbound_detail | list | sent | needs_attention | false | false | unknown |
| RC02/legacy_outbound_list and legacy_outbound_detail | detail | sent | needs_attention | false | false | state_not_retryable |
| RC02/submit_replay | replay | pending | submitted | false | false | state_not_retryable |
| RC02/submit_replay | list | pending | submitted | false | false | unknown |
| RC02/submit_replay | detail | pending | submitted | false | false | state_not_retryable |
| RC03/default | detail | failed | partially_accepted | false | true | empty string |
| RC04/default | detail | failed | needs_attention | false | false | delivery_uncertain |
| RC05/default | detail | sent | accepted | false | false | state_not_retryable |

Shipping closed parsers continue checking the complete raw DTO/envelope before
acceptance: unknown fields/types/categories, list metadata, tenant/job identity,
timestamps/attempt count, completeness, full exact counts, state/status and
delivery uncertainty. HTTP/UI private canary checks and absence of content and
attachment requests/disclosure remain. RC01/RC02 state/counts use the Go-authored
seed; RC03–RC05 use original case ledger/projection. Replay's newly allocated ID
continues to bind subsequent list/detail to the committed command response;
the original Go producer separately checks same key, aborted201 then200, one
job and one consumed draft. No capability/state/count expectation is generated
from a response. Neither the producer nor its SQL checks were changed.

## Scoped validation

- Original three-file 63-unit suite: **63/63 PASS**, separately from new tests.
  `lib/receipt-types.test.ts`, `components/company/r5-protocol.test.tsx`, and
  `components/company/r5-receipt-contract-oracle.test.ts` are unchanged.
  The controlled hook suite uses the actual original Go delivery unit producer;
  it is not formal HTTP/PG evidence. First attempt failed because `/usr/bin/go`
  was not the Go toolchain; rerun with the existing Go1.25.7 binary and existing
  module/build caches passed, without changing launch code or dependencies.
- New `r5-receipt-surface-oracle.test.ts`: **19/19 PASS**. Synthetic seed-only
  positives and negatives reject list/detail and list/replay swaps, list
  unknown→state_not_retryable, retry=true (including empty-reason retry grant),
  wrong exact detail/replay reasons, changed content rights, missing/extra
  fields and absent/unrecognized surface. Exact known/unknown/sender/uncertainty
  cases remain distinct; there is no broad accepted-reason set.
- Additional unchanged receipt consumer suite was included in an initial
  three-file run with parser/oracle: **79/79 PASS** (38 parser +22 oracle +19
  consumer). This count is separate from both the historical63 and new19.
- ESLint for changed TSX/oracle/new tests: **PASS**. `git diff --check`: **PASS**.
- Full `tsc --noEmit --incremental false`: **FAIL**, existing unchanged
  `r5-external-runtime-probe.test.tsx:9` TS7016, missing jsdom declaration.
  A temporary `/tmp` config extending the original project and excluding only
  that unrelated probe passes noEmit for the rest of the project, including all
  changed files. Repository config, probe and dependency declarations are not
  changed; this is scoped TypeScript evidence, not full-project green.

No formal PG/component producer was launched in this task. Parent must freeze
the corrected source and plan any fresh run within the original120 budget.
No product Go/TS, external runtime/cache/helper/launcher, fixed case/markers,
LF/permission, sharedDB89e7 or default675 changes. Central10/171 remains open;
task_complete/product_green remain false. No merge/deploy.

The adjacent summary contains safe source hashes and validation metadata only.
No response, fixture, token or private runtime body is published.
