# R5 bounded compile-only stabilization experiment

Result: **stopped, not stabilized**. The five compile-only interventions passed, but the race `r5protocol` selected-source capture rejected its own repeated raw metadata. No proposed warmup is ready for source-runner preparation. Root decides any further experiment or preparation change after separate review.

Frozen source: `df6ffe046611f5b2121f4d48d5114a2ececd2c24`; tree `90956a05c3276210d51a9b935f43ae92064b78ed`. A new HTTPS Git clone at `/workspace/r5-compile-stability-20261004` was detached at that pin and clean before execution. It is independent of the code-review worker. Raw/private diagnostic root: `/tmp/r5-compile-stability-20261004`, mode700; diagnostic operation directories are mode700 and evidence files mode600. The empty build cache and copied owned prehydrated module cache are private to this attempt; no service environment file was sourced, no service was started or used, and no cache permissions were broadened.

Pinned tool: Go1.25.7 Linux/amd64, executable SHA256 `76ac600b41ad2eceee5d39d02af78009516f8b2a29f30bcc80f765c3e4b4f5a8`, matching the earlier control. The existing binding code verified the toolchain/ABI context. Exactly the two existing `enmime/v2 v2.3.0` and `go-smtp v0.24.0` local replacements were retained, with root/fork module lock hashes and the unchanged original web lock recorded in [control-summary.json](evidence/R5-COMPILE-STABILITY-20261004/control-summary.json). No product, tool, test, preparation, lock or central TODO bytes changed.

## Coverage and observed interventions

Read `docs/VERSIONING.md`, `R5-VALIDATION-ENV-EVIDENCE-20261004.md`, archive/source inventory, selected binding v2, environment/diagnostic retention, runner preparation, source-version tests, CI backend compiler selectors, protocol/compatibility/transaction collectors, protocol case adapters and audit/benchmark selectors. No root/scripts applicable AGENTS.md, relevant.agents or repository skill file exists; the only AGENTS.md governs untouched web work. No external skill workflow or subagent was needed.

The selected binding supports only default and race `r5protocol`, each observing root plus both fork package trees, root/explicit production coverage, variant directories and MVS. Actual formal compiler selectors also include untagged race root tests and race `r5audit` postgres. These received raw Go-list observations, not invented formal binding contexts. The proposal covered:

| Compile selection | Compiler exit | Seconds |
|---|---:|---:|
| `root-build` | 0 | 46.984 |
| `default-selected` | 0 | 6.854 |
| `race-root` | 0 | 73.642 |
| `race-protocol` | 0 | 12.236 |
| `race-audit` | 0 | 9.083 |

The exact selections were `go build -mod=readonly ./...`; `go test -c -o <private-dir>/ ./internal/api/handlers ./internal/architecture`; `go test -c -o <private-dir>/ -race ./...`; `go test -c -o <private-dir>/ -race -tags=r5protocol ./internal/api/handlers ./internal/store/postgres`; and `go test -c -o <private-dir>/ -race -tags=r5audit ./internal/store/postgres`. Default handlers matches typed-wire compilation; architecture matches compatibility compilation; root build covers the transaction collector program without running it. Untagged protocol adapter compilation is contained in the race root graph. All compiler flags preserve the declared semantic contexts; runtime-only test selectors/output/count/timeouts are omitted by the compile-only transformation.

Before any binding, hydrate the four raw contexts, retain a baseline, then retain a fresh raw snapshot after each compile. Every snapshot contains selected root-plus-forks, root, explicit cmd/internal, variant-directory and MVS JSON. 140 raw metadata commands and 5 compiler commands exited 0; their stdout/stderr hashes, durations and argv are recorded in [command-summary.json](evidence/R5-COMPILE-STABILITY-20261004/command-summary.json). The exact recipe and its unreached repeat/strict-validation block are in [recipe.md](evidence/R5-COMPILE-STABILITY-20261004/recipe.md).

All prewarm raw JSON field differences were `Stale` or `StaleReason`; MVS stayed unchanged. Counts per context/intervention are in the control summary. The completed default handlers/architecture comparison did have additional changes (24 `Stale`, 158 `StaleReason` locations over three repeated coverage forms); an early progress read occurred before that comparison had been written. These counts repeat package locations across command forms and are not unique package counts.

## Preserved rejection and stop boundary

Default capture completed with within-capture raw equality in all five comparison forms. Its attestation SHA256 is `ca507621d5a16356ca8556719c0b324ff864876b423f33fdf81ad30b56b9d80d`. This candidate is private and **was not strictly postvalidated**.

Race `r5protocol` capture then failed with `selected metadata changed during capture`. The two selected-union observations differed at exactly one JSON location: `/462/StaleReason`, package **`tabmail/cmd/tabmail.test`**. The value changed from **`build ID mismatch`** to **`stale dependency: tabmail/cmd/tabmail`**. `Stale` and every other selected-union field were unchanged. Repeated MVS raw bytes were equal. The source helper short-circuited at that comparison, so repeated root/explicit/variant commands, a completed race receipt, the five repeated compiler interventions and both strict post-binding validators were **not reached**. The nine actual metadata command stdout/stderr pairs and sizes/hashes in the rejected capture remain private alongside all prior observations. There was one binding rejection; no retry or added warmup followed it.

This identifies the exact failing field/context in this checkout; it does not establish why that build-ID/dependency diagnostic changed or prove a successful alternative recipe. Nothing normalizes/drops Go-list stdout hashes, changes receipt identity, loosens within-capture raw equality or waives old receipts. Existing Go-env temporary-prefix normalization is unchanged. Earlier default/DB/component/compatibility failures retain their prior rejected/unqualified status.

## No execution, final-clone caveat and remaining work

Tracked bytes were clean through the stop. The five Go compiler command receipts are exclusively `build` or `test -c`; there is no artifact invocation, `test2json`, `go run`, formal producer or service command. 46 compiler artifacts were retained and set to mode600, with sizes/hashes recorded in [private-evidence-hashes.json](evidence/R5-COMPILE-STABILITY-20261004/private-evidence-hashes.json). Zero test binaries were executed. This is command/artifact evidence, not a syscall-tracing claim. Raw metadata, candidate receipt, rejected observations, stderr and private scripts are not committed.

The experiment only concerns the bounded selections in that final owned checkout/cache. Both forks were included in source/metadata closure and their production dependencies compiled through the root graph; their own test binaries were not precompiled. Nonrace root test compilation beyond handlers/architecture, `r5benchmark`, Method19, SML, wholeCI and fresh mutation-created clones are outside coverage. No Python formal default/source suite, sharedDB, HTTP, component, PG, mail/business service, source-runner preparation or typed-wire fixture execution ran. Neither a future formal pass nor complete compile-only stability was proved.

**Final-clone caveat:** an evidence-only commit and any later final-source clone have a different source/checkout identity. Source tests also create/mutate independent clones. This attempt cannot authorize those receipts or warm their cache diagnostics by inheritance. Any future proposal must be designed and prewarmed before first binding in the actual final owned clone, preserve every rejection, repeat the same actual compile-only selections and pass both existing strict mandatory postchecks there. No source-runner preparation change is proposed for acceptance on this failed result.

Normal Git branch push of only this document and its evidence prefix is authorized. No PR/API retry or alternate route, merge, release or deployment was attempted.
