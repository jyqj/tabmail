# Bounded independent delta acceptance: absolute-only v3 producer

## Decision

**Accept the bounded producer-path correction for publication at the frozen bytes below.** No new blocker was found in this delta. The deterministic caller-cwd/source-cwd/PATH executable mismatch is closed by rejecting nonabsolute inputs before source capture or subprocess execution. This acceptance is not consumer-migration approval, merge/deploy approval, actual-Go evidence, or formal product qualification.

- Base: `52c2d4a996ff6e6bc7f7f8f36a3e161c5f6f3e73`.
- Frozen archive SHA256: `bd876e37adea85ecb33ceab47a3b159dc6a6dc7c6fe65cceffc6e437cf232158`.
- Fixed helper SHA256: `12799a213c93510bdb449b5f610a493f517395878dd83c4b48a06e574bbf6acb`.
- Fixed helper Git blob: `390f72d638a8cb3398492bab26c810802017115d`.
- New explicit test blob: `46e701f8d3d5e56090640add3139c35cd5539ebc`.
- Author report blob: `c215356256d29096b21853b595b8bc010077ac3d`.

The reviewer extracted the archive into a new owned directory, verified its full SHA256 before extraction, rejected absolute/traversal/symlink/special archive entries, and checked every delivered file's bytes, Git blob and SHA256 against the frozen manifest. All 56 previously verified exact-base project files were compared: only the v3 helper changed. The publication payload consists of that helper plus one explicitly named test and one report. V2 and the schema remain byte-identical. No catalog, consumer, dependency or discovery-path change appears in the delivery payload. This is a partial extract; it does not reconstruct a complete repository tree.

## Independent guarded execution

**82 outer tests passed, zero failures, errors, skips, expected failures or unexpected successes. OS subprocess attempts: zero.**

- 56 author/new and unchanged pure checks: 7 new producer-path checks, 21 existing v3 checks including AuthorityTests, 8 rejection-retention checks, 5 v2 negatives and 15 archive checks.
- 16 original independent non-path methods, unchanged.
- 3 original independent absolute-only contract methods, unchanged.
- 7 fresh independent delta methods.

Both independent sources retain their frozen hashes:

- Frozen original source: `f885d97b6bda46c957b31f42421123b81fe587823a2d97b41c7fc7d99e479a52`.
- Absolute-only contract: `635d3a2ae433acde5831d2e90a6044ce33da43b0499133010191da33c218ccdb`.

The independent runner sets explicit synthetic producer/cache environment values before any project/test import and places every such import and test inside an OS subprocess-creation guard. It does not rely on the author's execution claims. It runs no actual Go binary or other child process. Per-test results and guard counts are in `R5-V3-INDEPENDENT-ROOT-20261004-delta-test-summary.json`; the log and exact runner are alongside it.

The seven fresh methods cover:

1. Empty, dot, parent-relative, shell-like, URL-like, Windows-looking and whitespace-prefixed forms fail early without expansion or PATH search.
2. A relative custom `os.PathLike` fails before source acquisition or commands.
3. An absolute custom PathLike is converted once to a stable Path, then all 12 commands and envelope entries retain the same hashed target.
4. An absolute path containing a lexical parent component still hashes and addresses the same target for all 12 calls; no unsupported canonical-path restriction is invented.
5. Wrong absolute-producer bytes reject before any command.
6. Wrong external observation pins still reject before capture/path validation, preserving the previous trust order.
7. Explicit absolute selection never falls back to PATH or silently resolves a substitute executable.

## Findings and accepted limits

The new `_absolute_producer_path` creates one Path and requires it to be absolute at the first step of `_capture`. That same value is used for the existing before/after file hashes, subprocess executable and envelope argv. API documentation and CLI help correctly explain the new compatibility restriction. Absolute Path and string positive controls preserve all 12 commands, metadata producer SHA256, complete observation pin logic, source/schema checks and blocked/unknown qualifications.

The original red18 tests remain historical rejection evidence. Their two old path tests assumed an accepted capture and intentionally failed its identity invariant; they are not relabeled as green or used as acceptance criteria for the newly unsupported inputs. The separate early-rejection contract passes unchanged.

Absolute paths do not guarantee atomic execution against a concurrent hostile executable/ancestor/symlink writer. Before/after checks can miss change-and-restore behavior. The documentation correctly retains the trusted/quiescent producer assumption and does not claim immutable descriptor execution, syscall tracing or compiler-input attestation. External complete-observation pin provenance remains a caller responsibility; optional raw diagnostics remain non-authoritative. Compiler/native/generated/external-module bytes remain unattested; overall remains blocked and Method19/SML/wholeCI remain unknown.

The author's initial unguarded v2 module import attempted `/usr/bin/go env GOCACHE`, exited 1 with `Go: Unknown option: env`, and ran no tests. This was an execution-boundary mistake, was disclosed, and is not accepted evidence. The separate expanded partial-extract run has 71 passes and one missing-fixture assertion failure; it remains nonpassing. Both logs are preserved in the frozen archive. The later independent 82-pass run does not erase or reinterpret either attempt.

No production code was edited by this reviewer. No GitHub write, actual Go metadata capture, compilation, product suite, formal batch, default/sharedDB/HTTP/components/wholePG run, database, service, mail, merge, deployment or release occurred in this review. The shared TODO remains untouched. Exact remote delivery verification is still the parent's publication step; any changed bytes require a new review boundary.

## Parent-owned TODO proposal

- [x] Independently accept the bounded absolute-only v3 producer correction at helper SHA256 `12799a213c93510bdb449b5f610a493f517395878dd83c4b48a06e574bbf6acb`: 82 guarded pure controls pass, including 16 unchanged independent methods, 3 unchanged policy methods and 7 fresh boundaries; zero OS subprocess attempts. Preserve original red evidence and the disclosed author import/partial-extract failures. No formal migration or central completion-count increase.
