# R5 additive selected-binding v3 — limited implementation evidence

Additive v3 helper/schema/explicit checks are implemented. The bounded metadata/compile-only experiment passed for both supported contexts on the frozen code below. **Independent review is required before any consumer migration.** This is not formal product qualification, a stable-cache warmup recipe, or full compilation attestation.

Accepted implementation base: `3f34c31ed51a721b318a76512805e4a14dc292c3`. Read the independent acceptance at `5057e2070aa369e1d7879669bce876e902bb3901` and the failed compile-only report at `f25f7d22badf5240559f0144664e1fa1ef98f331`. Those historical acceptances/rejections retain their exact meanings; this work does not relabel them.

Frozen code tested: `b25e383a5e4fa64f13e9e0a069b75bb263dd7d5b`, independent exact/clean owned clone `/workspace/tabmail-v3-reviewed-experiment`. Base-to-code diff contains exactly these added paths:

- `scripts/r5_selected_source_binding_v3.py`
- `scripts/contracts/r5-selected-binding-v3-producer.json`
- `scripts/tests/r5_selected_source_binding_v3_checks.py`

Report/evidence adds only this document and `docs/company-mail/evidence/R5-SELECTED-BINDING-V3-20261004/`. No existing source, catalog, TODO, runner, preparation, discovery, frozen routing, dependency or lock file changes. The new checks intentionally do not match the existing `test_*.py` discovery pattern. The later evidence-only delivery head is a distinct Git pin; do not inherit a receipt for another clone/head. [scope.json](evidence/R5-SELECTED-BINDING-V3-20261004/scope.json) records the code scope.

## Projection and producer registry

Policy: `r5_root_selected_local_archive_attestation_v3`; receipt schema: integer `3`. Selected v1/v2 receipts reject under v3 before commands; v3 receipts reject under v2. There is no fallback or relabeling. All existing v2 bytes, raw equality checks, rejection behavior and consumers remain unchanged.

`project_package_stdout(raw: bytes) -> bytes` is the single projection API. It parses the complete ordered JSON stream, validates the entire producer schema, then canonically serializes each package after removing **only its top-level `Stale` (boolean) and `StaleReason` (string)**. Object key ordering and JSON whitespace are explicitly nonidentity. All other fields, presence, stream order, arrays and multiplicity remain bound, including Imports, ImportMap, cgo flags, errors, and recursive module metadata. Arbitrary string ImportMap keys named Stale/StaleReason remain bound; same-named unknown keys in nested typed objects reject. Unknown fields/types, duplicate keys at any depth, nonfinite values, malformed/empty streams and incomplete serialized error records reject. Null ImportStack is permitted because the actual error serializer can emit it; pointer/slice fields with omitempty must be omitted rather than emitted with an invalid null type.

The source-bound registry was extracted from and compared in full with the official **Go1.25.7** sources, not accepted by counts alone:

- [load/pkg.go](https://raw.githubusercontent.com/golang/go/go1.25.7/src/cmd/go/internal/load/pkg.go): PackagePublic has 58 declared fields, 56 retained; PackageError.MarshalJSON emits ImportStack/Pos/Err, excluding the internal IsImportCycle flag.
- [modinfo/info.go](https://raw.githubusercontent.com/golang/go/go1.25.7/src/cmd/go/internal/modinfo/info.go): ModulePublic has 19 fields, including recursive Replace/Update, Origin, Time and Reuse; ModuleError has Err.
- [codehost/codehost.go](https://raw.githubusercontent.com/golang/go/go1.25.7/src/cmd/go/internal/modfetch/codehost/codehost.go): Origin has 8 fields.

The registry includes official source URLs and complete-file SHA256 values. The installed source files were byte-equal to the independently downloaded official files. [verify_producer_schema.py](evidence/R5-SELECTED-BINDING-V3-20261004/verify_producer_schema.py) rechecks exact names/types and the error marshaler without executing downloaded code; its [result](evidence/R5-SELECTED-BINDING-V3-20261004/producer-schema-verification.json) is recorded.

The v3 helper reuses the unchanged v2 authority predicates and static inventory v4: exact contexts/commands, root selection authority, archive exclusion, the exact two forks, local source/hash/topology checks, hydration ordering, source-before/source-after checks, production coverage and permitted variant build-constraint errors. Every package command, including hydration and every repeated observation, uses the strict projection. Only post-hydration observations authorize selection. MVS retains byte-for-byte repeated equality and raw binding hashes. Go-env retains only the existing GOGCCFLAGS temporary-prefix normalization.

## API and trusted-pin contract

```python
receipt = capture(root, go, cache=cache, modulecache=modulecache, context=context)
# Trust the direct capture execution/channel; store its complete observation pin
# separately before receiving or validating any later receipt.
trusted_pin = observation_digest(receipt)
observed = validate(receipt, root, go, cache=cache, modulecache=modulecache,
                    trusted_observation_sha256=trusted_pin)
```

`validate` requires the keyword-only external pin; the CLI requires `--trusted-observation-sha256` when `--validate` is used. The pin MUST come from an independently trusted complete capture/envelope channel or trusted external record, never from the untrusted receipt's self-declared hash. The API cannot establish the caller's trust source. CLI receipt parsing is strict; API callers must preserve the complete decoded receipt. Missing/malformed pins and version mismatches reject before any producer command.

`observation_envelope` preserves every command, including unbound hydration, in exact order, binding argv, role, exit/returncode, raw stdout sizes/SHA256 and raw stderr sizes/SHA256. Raw bytes stay private. `observation_digest(receipt)` is SHA256 of the canonical **entire receipt excluding only its own `observation_sha256` member**; it includes the envelope, source records and `binding_sha256`. The self-declared digest is checked against the supplied trusted pin before live capture. Altering or re-signing any complete envelope while holding that external pin fixed rejects.

Each command separately names `binding_stdout_sha256` and `raw_stdout_sha256`. `binding_sha256` hashes the binding payload: all receipt metadata/source records and ordered post-hydration command descriptors using projected stdout hashes, with raw stdout size/hash replaced by that binding hash. Raw stderr hash/size, argv, role and returncode stay bound. The unbound hydration descriptor remains excluded from selection identity as in v2, but remains fully protected by the complete external observation pin. Go-env and MVS use their explicitly named domains. Distinct before/after receipts retain their distinct raw envelopes and observation pins even when their binding digests are equal.

The independent stdlib [observation verification](evidence/R5-SELECTED-BINDING-V3-20261004/verify_trusted_observations.py) rehashed all four trusted local before/after receipts and stored their separate pins; all 28 actual-receipt envelope alterations/re-signings rejected with the original external pin held fixed. Its [safe result](evidence/R5-SELECTED-BINDING-V3-20261004/trusted-observation-checks.json) records each check. These assertions are separate from the 51 outer unit-test count.

On validation the original externally pinned receipt is verified first, then a fresh capture must have exactly the same complete binding payload. The fresh observation is separately sealed and returned; it is not substituted for the old observation. Subsequent validation of that new observation requires its own independently trusted pin.

The executable SHA256 `76ac600b41ad2eceee5d39d02af78009516f8b2a29f30bcc80f765c3e4b4f5a8` is checked before and after capture, as are the helper/schema/check source bytes. This supports an **exact metadata-producer** claim. It does not attest compiler/tools, native toolchain bytes, generated testmain bytes or external module-cache bytes. Those qualification fields stay unknown; overall stays blocked.

## Fresh tests and controlled compilation

On the frozen code, **51 outer Python checks passed**, with zero failures, errors, skips or expected failures: 21 explicit v3 checks; 5 unchanged v2 negative controls; 17 unchanged environment/diagnostic checks; 8 unchanged retention-failure checks. [unit-summary.json](evidence/R5-SELECTED-BINDING-V3-20261004/unit-summary.json) lists every actual ID and the exact invocation. These are new executed counts for this checkout, not the independent review's old 78-check result.

V3 checks exercise diagnostic value/presence changes; every retained producer field and nested leaf; recursive module fields; key ordering, whitespace, arrays, order and multiplicity; unknown/duplicate/nonfinite/malformed/type-invalid metadata; nested same-named keys; all package command roles/repetitions; complete externally pinned envelope tamper/re-signing; version boundaries; source/archive/private/package/coverage/replacement/context rejections; producer failure/timeout preservation; and unchanged v2 authority reuse. Command ordering tests use synthetic executors; authority controls use private fixtures; fresh command execution is separately evidenced below.

Final experiment: new exact clone, newly copied owned prehydrated module cache, initially empty owned build cache, private evidence root `/tmp/r5-selected-v3-reviewed-20261004`. No private service settings were loaded. Both contexts were captured first, their trusted pins stored separately, then these two bounded compile-only commands ran (180-second bound each):

```text
go test -c -mod=readonly -o <private-default-dir>/ ./internal/api/handlers ./internal/architecture
go test -c -mod=readonly -o <private-race-dir>/ -race -tags=r5protocol ./internal/api/handlers ./internal/architecture
```

Both exited 0; zero test binaries were executed. Both original receipts then passed mandatory strict v3 postvalidation. Each capture observes root-plus-both-forks selected metadata, MVS, root/explicit production coverage and all variant directories; all 48 final metadata commands exited 0. All command stdout/stderr facts, argv and durations are in [command-summary.json](evidence/R5-SELECTED-BINDING-V3-20261004/command-summary.json); receipt hashes and results are in [experiment-summary.json](evidence/R5-SELECTED-BINDING-V3-20261004/experiment-summary.json). Compiler artifacts are command/artifact evidence, not syscall tracing or product passes.

| Context | Changed JSON locations | Stale | StaleReason | Outside allowlist |
|---|---:|---:|---:|---:|
| default | 7033 | 2889 | 4144 | 0 |
| race-r5protocol | 7075 | 2910 | 4165 | 0 |

Counts repeat package locations across hydration, selected, coverage and repeated command forms; they are not unique package counts.

Binding digests stayed equal; complete observation digests changed. Full raw JSON before/after differences were retained privately for every package command form, including repeated and hydration observations. MVS raw bytes stayed equal; env differed only within the existing temporary-prefix normalization. No new exception was created. The private full diff files and raw evidence sizes/hashes are recorded in [private-evidence-hashes.json](evidence/R5-SELECTED-BINDING-V3-20261004/private-evidence-hashes.json); [raw-diff-paths.json](evidence/R5-SELECTED-BINDING-V3-20261004/raw-diff-paths.json) publishes every changed JSON path/command index without raw values. The exact executed [experiment recipe](evidence/R5-SELECTED-BINDING-V3-20261004/experiment_recipe.py) is retained.

Two earlier owned checkout iterations also passed their bounded compile-only postchecks at exact code pins `ea06e9fa7ea45727b3d5b3108b4b33dd5732c457` and `3731189993fe02bc8af419b9cac1856b80086cb6`. They are developmental evidence only, not substituted for the final pin; their private roots remain `/tmp/r5-selected-v3-20261004` and `/tmp/r5-selected-v3-final-20261004`. The final iteration was rerun after strict error-schema checks and the explicit-test filename change.

## Proposed central TODO entries for parent reconciliation

The shared R5-TODO remains untouched because parent owns reconciliation with the separate catalog worker.

- [x] Add isolated selected-binding v3 helper, official exhaustive producer registry and explicit opt-in negative checks; preserve v2 and all formal consumers/discovery/routing.
- [x] Record fresh default/race metadata captures, bounded handlers/architecture compile-only interventions, separate trusted observation pins, full raw diffs and strict v3 postvalidation on exact code pin `b25e383a5e4fa64f13e9e0a069b75bb263dd7d5b`.
- [ ] Independently review the full v3 source/schema/tests and external-pin trust contract before deciding any consumer migration. Review the final delivery head separately from the tested code pin.
- [ ] Design any later formal migration/run as a separate reviewed scope; preserve historical v2 failures and outstanding catalog/qualification blockers. No completion count or old passing count is advanced by this helper work.

No formal default/source suite, sharedDB, HTTP, components, wholePG, actual mail, service access, catalog reconciliation, consumer migration, merge, deploy or PR/API action occurred. No repository root/scripts AGENTS.md, relevant.agents or skill files apply; the existing web-only instructions govern untouched paths. `docs/VERSIONING.md` was read. The branch is based on the explicitly authorized accepted tooling pin; normal commit/push alone is authorized. Parent owns any subsequent PR creation using its connected GitHub tool.
