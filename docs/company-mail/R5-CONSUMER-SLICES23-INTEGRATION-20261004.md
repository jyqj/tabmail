# Accepted consumer slices 2/3 — exact combined source freeze

Combined implementation source: `8182e40d3b1f135415e644e122791539ab75fc1b`; tree: `4856a756af28912eb90eaa24e9ce4a5625a0747a`. The subsequent integration delivery adds this report and fresh evidence only. It is a distinct commit, not the tested implementation identity. Central **10/171** and historical **F52 failures** remain unchanged. This accepts no physical runtime or formal qualification.

| Input | Tested source | Delivery | Independent acceptance |
| --- | --- | --- | --- |
| Frozen adapter | `632d55de566254c618bca9a54c5dbae7b355fcf9` | same frozen base | prior acceptance retained through runtime ancestry |
| Preparation slice 2 | `cad567ff2bb224b94fbf20c359383edcd0fce59b` | `7262e78e0120c5a203bb588807ca876b82c4d5bc` | `f15468a0c06160dc3155e386c5f0dc8d7a862f54` |
| Runtime slice 3 | `dcaf2bf91355d4c0524f34f0c767b7997a994833` | `97c54dc8dad6ab2da6d4dd29e43c20b7091db73f` | `d6cda3a4b458aecf44b01467970796a143e89cb4` |

Read the actual preparation author JSON and independent acceptance, runtime delivery and independent acceptance reports, design `42f49538f7bceb6eb8e698d716c1418f85343413`, repository VERSIONING and TODO, and `web/AGENTS.md`. No root/scripts AGENTS.md or workspace agent instructions exist. No web edits were made, so the Next.js guide requirement does not apply. User authorization explicitly selects the frozen stacked base instead of the ordinary main branch convention.

## Source, paths and ancestry

The isolated worktree is `/workspace/tabmail-slices23`; branch `integration/r5-consumer-slices23-20261004`. Local integration merges retain both independent-review heads and every accepted source/delivery commit as ancestors. No branch was merged into main. Both merges were conflict-free; no semantic resolution or source adaptation was performed.

[Fresh static verification](evidence/R5-CONSUMER-SLICES23-INTEGRATION-20261004/static-review.json) proves the frozen combined tree equals the exact union of both reviewed heads over the adapter base, including Git blob IDs and modes. Ownership intersection is empty. All seven implementation/control files equal their separately tested source and delivery bytes. Thirty-five paths differ from the adapter base: five existing consumers changed, two explicit author controls added, and twenty-eight report/evidence paths added. **2,209** adapter-base paths remain identical; their full mode/blob manifest is [preserved-tree.json](evidence/R5-CONSUMER-SLICES23-INTEGRATION-20261004/preserved-tree.json).

Existing path locations are preserved: runtime runners/probes remain under `scripts/preparation/`; no top-level wrappers were invented. Historical author and independent evidence is retained byte-for-byte. Historical static checkers were read but not rerun because they write to their historical evidence and the runtime checker assumes preparation is still unchanged. The new checker verifies the combined tree without rewriting that evidence.

The adapter, selected-v1/v2/v3 helpers, producer schemas, registry/closure/inventory, batch helper/schema, all Go sources/bridges, `check_r5_protocol.py`, discovery/cases/markers/budgets, locks and TODO remain byte-identical to the frozen base. Thirty-one definition/constant AST preservation checks and the v2 runtime manifest-constructor equivalence pass. Fresh controls preserve omitted/default v2 behavior, integer-strict explicit v3, current-only preparation opt-in, exact four frozen-v1 IDs and sanitation of new selectors/pins out of frozen children.

## Fresh combined pure controls

Command: `python3 -B docs/company-mail/evidence/R5-CONSUMER-SLICES23-INTEGRATION-20261004/verify_combined.py`. The orchestration source is retained alongside results and can be rerun without changing historical evidence. Each suite installs its OS-process audit guard before consumer imports; producer/build/runtime process boundaries are mocked. Orchestrator Python launches and read-only Git metadata processes are outside the zero-process claims.

| Fresh suite against combined source | Passed | Guarded OS-process events |
| --- | ---: | ---: |
| Preparation author | 64 controls | 0 |
| Preparation independent | 90 controls | 0 |
| Preserved preparation/runner/held-FD boundaries | 15 tests | 0 |
| Runtime author | 30 test methods | 0 |
| Runtime independent | 13 test methods | 0 |

Total: **212 named controls/test methods**, zero failures/errors. Runtime envelope-mutation subtests are not added to this total. [fresh-results.json](evidence/R5-CONSUMER-SLICES23-INTEGRATION-20261004/fresh-results.json) records exact commands, source/log SHA256s, full preparation control lists and runtime results. Separate stdout/stderr files preserve all fresh logs. `git diff --check` passes. No actual producer, capture, runtime launch, Go, Node/Vitest, typed-wire, PostgreSQL, mail, service, CI or formal suite was run. Synthetic checks do not establish physical capture provenance, hostile concurrent mutation protection or lifecycle qualification.

## Concrete slice 4 interface inventory — proposed work only

| Exact frozen interface | Current behavior/gap | Bounded proposed slice 4 change |
| --- | --- | --- |
| `scripts/preparation/r5_external_batch.py:27,73–116` (`POLICY`, `capture`, `validate_contract`, `load`) | Batch schema/policy 1; nested runtime hash is canonical JSON, while outer contract pin hashes serialized bytes. Capture can construct a v1-shaped contract from a supplied v3 object; validation calls `runtime.validate` with default v2 and rejects it. Schema checks currently do not establish an explicit runtime/version pairing. | Preserve v1/v2 route; add explicit batch-v2/runtime-v3 pairing and strict integer selector/schema checks before capture/process boundaries. Propagate the independently authenticated runtime/admitted bundle transitively through the pinned contract. Never originate observation authority from recapture or candidate self-hashes. |
| `scripts/contracts/r5-external-batch-v1.schema.json` | Schema version 1 and policy `r5_external_batch_validation_v1`, broad nested runtime object, four workers, fixed fields. | Leave v1 schema intact; introduce narrowly scoped new v2 schema and explicit non-discovery pure checks. Decide exact new fields before implementation; no new schema is claimed here. |
| Batch `Batch.__init__:339`, `validate_inventory:487`, `_run:644`, `main:846` | Environment derives from pinned context; worker validates an independently pinned contract with `--contract/--pin`; CLI capture uses environment runtime selection, but contract validation defaults to v2. Receipt publication remains schema/policy 1 at line 829. | Carry selected version consistently through contract capture/load, killable validation worker, owned child environment and result policy. Preserve worker4/Go120/process180/case75, unchanged catalog derivation, remaining-time accounting and no deadline reset. |
| `internal/api/handlers/r5_protocol_component_observations_test.go:590` (`r5UIExternalRuntime`) | Independently pins manifest bytes and absolute regular executable bytes; policy hard-coded runtime-v2. Parsed manifest currently lacks schema/version fields. Python `validate` argv omits selector, and CLI defaults v2 even when environment says 3. | Parse explicit integer schema/version and require exact pairing; preserve manifest/tool/source pin checks. Pass the negotiated selector on the Python validation argv. Omission must stay v2, with no policy-based upgrade. |
| Same Go file `r5UIExternalVitestCommand:641`; callers `:653,:680` and `internal/api/handlers/r5_external_runtime_probe_test.go:90,93` | Launch argv omits selector; helper CLI defaults v2. Component and probe reuse same bridge. | Thread the selected version through existing helper interface or one narrowly scoped companion value, and add explicit launch argv selection. Preserve original test/case IDs, assertion paths and timeouts; no Go execution in this inventory. |
| `internal/api/handlers/r5_external_batch_test.go:31,70,204,268` | RPC bridge carries socket/nonce/contract hash, strict named operations, 65536-byte response bound and case75/join180. It does not parse runtime policy/schema or launch a helper. | No contract-parser change currently required by this interface. Reassess only if the approved batch-v2 RPC response actually introduces a new field requiring parsing. Keep capability/known-case/exactly-once/ack/cleanup semantics intact. |
| `scripts/check_r5_protocol.py:549–585` | `from_environment` may select v3, but subsequent `validate`/`launch` calls omit the selector and default v2. `external_component_command:526` is the unchanged fixed Vitest argv template used by batch. | Inventory blocker only in this task; no edits authorized. If future slice 4 needs this caller, obtain that explicit bounded scope and propagate the selector through the three calls without changing cases/markers/commands. |
| `scripts/preparation/r5_external_runtime.py:429,440,471,517`; scoped runner/probe | Accepted APIs already provide explicit `selected_binding_version`; load authenticates manifest bytes and delegates immutable admitted bundle pins. Scoped runner/probe carry opt-in. | Consume these frozen interfaces; do not alter slice 2/3 or adapter/helper/schema bytes. Batch v1 and current Go bridge remain closed to v3 until the new gate is accepted. |

Required future pure gate: every omitted/v1/v2/v3 selector/schema/policy pairing; bool/string/float/unknown rejection; forged receipt/bundle/manifest/contract pins; rejected admission dispatches no producer/child; complete envelope and binding checks; transitive pin integrity and strict child propagation; unchanged original catalogs/argv/budgets; lifecycle failure precedence and staged-result invalidation. Physical lease/kill/reap/RPC/drain/socket/owner cleanup qualification remains separately authorized and cannot be replaced by pure results.

## Plan ownership and remaining gates

`R5-TODO.md` is unchanged. Its console reserves shared TODO ownership to the designated integration operator; this handoff does not establish that ownership. Proposed bounded substeps for the parent: (1) approve exact batch-v2/version-plumbing write set above; (2) implement/review guarded pure contract and bridge controls; (3) freeze slice 4; (4) separately integrate closure/authority bytes as design slice 5; (5) authorize physical qualification only after exact-source independent acceptance. None closes a 171-parent task or changes historical F52 evidence.

The parent creates the draft PR. This task pushes only the isolated integration branch; no PR creation, main merge, deploy, force push, registry refresh, batch/bridge changes or subsequent qualification is authorized here.
