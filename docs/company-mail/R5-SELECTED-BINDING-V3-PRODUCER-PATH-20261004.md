# R5 selected-binding v3: absolute metadata-producer path

This bounded correction is based on exact PR #30 head `52c2d4a996ff6e6bc7f7f8f36a3e161c5f6f3e73`. The original v3 helper Git blob is `cd5aeca02d10c81802daec655e6ca913786d63c5`; its SHA256 is `c2682ae594c006e85ff860033766a25f920d35ab9a9113d22bf56232132936c2`. The local review input is a partial source extract: all 56 copied project files were checked against their exact-base Git blob IDs and SHA256 values before editing. It is not a full checkout or a fresh runtime qualification.

## Failure and chosen contract

The old code hashes `Path(go)` in the caller's working directory, runs the same argument with `cwd=root`, and rehashes the caller-side path afterward. For a relative path containing `/`, subprocess execution therefore resolves under the source root; for a bare name, execution searches `PATH`. Either may execute different bytes from the hashed file.

The correction deliberately requires an **absolute metadata-producer path**. `_capture` checks that requirement before source capture or any subprocess. Bare names and relative paths raise `ValueError`; the helper does not make them absolute, consult `PATH`, or select a substitute executable. API documentation and `--go` help state this contract. The selected absolute `Path` is then used by the existing before/after hash reads and every subprocess invocation.

Examples for this API and CLI must supply an independently selected absolute executable path, such as `/trusted/toolchain/bin/go`, instead of `go`, `bin/go`, or `./bin/go`. The example is a shape, not a trusted installation recommendation. `validate` still verifies the separately supplied complete observation pin before fresh capture; with a valid pin, its `go` argument must satisfy the same absolute-only contract. Existing receipts remain evidence for their original exact helper/source bytes; this change does not relabel them or authorize reuse against changed source.

The approved SHA256 remains `76ac600b41ad2eceee5d39d02af78009516f8b2a29f30bcc80f765c3e4b4f5a8`. Before/after producer hashes, source/schema checks, projection rules, observation pin verification, command sequence, rejection diagnostics, receipt schema, and v2 authority predicates remain in place. No formal consumer or discovery migration is included.

## Verification

The explicit new file `scripts/tests/r5_selected_binding_v3_producer_path_checks.py` contains seven outer checks:

- Bare names, including a `Path` argument, reject before `capture_current_source` and `subprocess.run`.
- Relative, parent-relative, empty, and current-directory paths reject before those calls.
- Validation with a valid complete observation pin rejects bare and relative paths before those calls.
- CLI bare and relative inputs reject before those calls.
- CLI help explains the absolute-path requirement.
- An existing synthetic absolute-path capture still produces the complete twelve-command sequence, with the same absolute executable in every invocation and observation, the expected producer hash, and blocked/unknown qualifications.
- The production executable SHA256 constant is unchanged.

The new tests were first run on the untouched exact-base helper: seven outer checks ran and the suite failed, with two passing methods and the remaining methods reporting failures/errors. That red log is retained. The original independent review's frozen eighteen-check source and log are also preserved: sixteen passed and two intentionally failed because relative and bare arguments returned captures executing different synthetic bytes. Those two old tests assume capture returns, so they are historical reproduction evidence rather than acceptance tests for the new rejection policy.

On the fixed helper, the guarded allowlisted run passes **56 checks**: 7 new path checks, 21 unchanged v3 checks, 8 unchanged rejection-diagnostic checks, 5 unchanged v2 negative controls, and 15 unchanged archive-boundary controls. A separate run of the original independent review's **16 unchanged non-path checks** passes, including exact official schema-source comparisons, absolute twelve-command behavior, source drift, producer-byte drift, external observation pins, and rejection retention. The independent fixture root alone is redirected into this implementation's owned directory; its source and assertions are unchanged. A third run passes all **3 unchanged independent absolute-only acceptance methods**: early rejection for five capture path variants, early rejection with a valid validation pin, and both absolute string/Path twelve-command target/hash identities. Its source SHA256 is `635d3a2ae433acde5831d2e90a6044ce33da43b0499133010191da33c218ccdb`; its exact-base red log has one passing method, two failing methods, and seven subtest failure events. All three accepted runners prohibit OS child-process creation and report zero calls to `subprocess.Popen`. Thus the accepted bounded runs total **75 passing outer checks (56 + 16 + 3)**, with no failures, errors, or skips in those runs.

Two unsuccessful verification attempts are retained rather than hidden:

1. The first combined-suite import omitted the v2 module's fixture cache environment variables. Its module-level setup attempted `/usr/bin/go env GOCACHE`; that process exited 1 with `Go: Unknown option: env`, and no tests in that invocation ran. This was outside the intended Python-only process boundary. Subsequent runners explicitly set synthetic producer/cache paths and reject every OS subprocess creation. No valid Go metadata capture, compilation, product test, service, database, or formal runtime qualification resulted.
2. An optional broader pure inventory run executed 72 checks with 71 passing and one failure caused by the partial extract lacking `docs/company-mail/evidence/R5-BENCHMARK-DATASETS.json`. It is not claimed as a passing suite. The final 56-check allowlist does not claim that separate benchmark-related inventory check.

See the accompanying review artifact manifest and logs for exact commands, hashes, and frozen files. Only the listed accepted runs count as successful evidence.

## Limits and scope

Absolute-path selection closes the deterministic caller-cwd/source-cwd/PATH mismatch. It is **not atomic execution attestation**. An executable, parent directory, or symlink target can be changed concurrently between checks and execution, including change-and-restore behavior that before/after hashes cannot detect. This API requires a trusted, quiescent producer path and environment; no file-descriptor execution or immutable toolchain guarantee is introduced.

The claim remains an exact metadata-producer byte check under that assumption. Compiler/toolchain, native, generated testmain, and external module-cache bytes remain unattested. Overall qualification stays blocked; compiler/native, Method19, SML, and whole-CI qualifications remain unknown. Python synthetic fixtures are not real metadata or product-runtime passes.

The change is limited to the v3 helper, one explicitly invoked test file, this report, and bounded evidence. It changes no v2 helper, schema/contract, catalog, formal runner, existing test-discovery routing, dependency, historical capsule, or existing report. No central TODO file is edited. Publication, merging, deployment, and any later real producer experiment are separate decisions.

## Proposed TODO entry for parent reconciliation

- [x] Reject nonabsolute v3 metadata-producer paths before source capture or execution; preserve the approved executable SHA256 and before/after checks; record guarded synthetic red/green evidence and the concurrent-writer limit. Exact implementation and delivery hashes must be attached when accepted.
- [ ] Independently accept the new absolute-only path contract on the frozen artifact before any publication or consumer-migration decision. Historical observations retain their original pins and qualifications.
