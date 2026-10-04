# Bounded R5 validation environment and diagnostic repair

Review base: `f52f8cbbf31dcbfb72ba062af6623248334a2e99`. Isolated branch: `codex/r5-env-evidence-20261004`. Initial tooling/control source: `053414728b359d66fba0f2a02e1229088e505d1a`. Final source is the pushed branch head; the later transaction selector guard also handles an explicit tool when PATH contains no Go. This document and its evidence are diagnostic only. Independent root review must precede any new formal run.

## Repair and identity boundary

`r5_go_environment.selected()` resolves the explicit `R5_TEST_GO` (then legacy `R5_GO`, then existing fallback), maps `R5_TEST_CACHE`/`R5_TEST_MODULECACHE` to `GOCACHE`/`GOMODCACHE`, and maps the module-cache parent to `GOPATH`. With an explicit tool it pins both selectors and the leading PATH directory, disables Go environment/workspace/flags/toolchain auto-selection, and removes inherited compiler/ABI switches, all CGO overrides, alternate GOROOT/temp/cache-program selectors, FIPS and coverage overrides. It copies the environment without changing the caller or creating caches, installing dependencies, changing ownership, or broadening permissions. Existing service and transport variables remain available to runner children. Standalone collectors without an explicit tool preserve their existing environment behavior.

Preparation, runner dispatch (both exact current and frozen groups), transaction AST collection and compatibility route collection use this helper. The transaction producer retains `GOPROXY=off` and timeout 90. Compatibility retains the exact route test, readonly module flag, count, pattern and timeout 60. Frozen helper/test bytes and their historical source pin remain unchanged. Public `prepare`, `collect`, `extract`, `capture` and `validate` signatures remain unchanged.

Optional `R5_DIAGNOSTIC_ROOT` names an existing owned mode700 directory outside a Git checkout. Directory traversal uses directory descriptors and `O_NOFOLLOW`; operation directories are unique mode700, files are exclusive mode600, and descriptors close on success or failure. Traversal, symlinks, caller replacement filenames, public/unowned roots and oversized retention fail closed. An empty protected sandbox `.git` mount is distinguished from a repository by HEAD; a worktree `.git` file is rejected. Limits are 64 MiB/file, 512 MiB/operation and 64 files/operation. The limits bound retained bytes; subprocess capture still uses its existing in-memory capture behavior. Operators control the number of operations and private evidence lifecycle.

Each selected metadata command retains stdout, stderr and an internally computed size/hash/returncode summary, including command failures and timeouts. Validation saves the rejected observed receipt privately before raising. Compatibility preserves captured child stdout/stderr/returncode before rethrowing the original `CalledProcessError` or timeout; it also records completed producers when opted in. Diagnostics are outside the receipt identity and cannot substitute receipt fields or authorize qualification. Raw stdout hashes, within-capture raw equality, strict validation, source-bound receipts, dependencies, two forks, official locks, formal budgets, test selections and expectations are unchanged.

## Focused checks

37 distinct Python checks passed: 17 new environment/diagnostic controls, 15 existing runner/preparation/binary-boundary checks and 5 existing selected-v2 negative checks. They cover dispatch to both groups, a selected transaction tool with no PATH Go, cache propagation and flag isolation, the exact compatibility command, failed/timeout output, invalid roots, symlinks/escape/overwrite, per-file/total/count bounds, partial-write cleanup, descriptor closure, valid receipt behavior and signed extra-field/raw-hash receipt rejection. The runner test intentionally creates a failing, skipped and expected-failing child and asserts each stays non-green; its outer test passes.

Two early focused attempts each had 3 failures and 6 errors because an empty protected sandbox `.git` mount was treated as a repository. Both attempts remain recorded in the test summary and tool history; neither is described as passing. After the directory check was corrected, all final focused checks passed. `git diff --check` passed. No full default, sharedDB, HTTP, component, wholePG180, dependency or production test batch ran.

## New exact-source metadata/compile-only control

The new independent no-hardlinks Git clone was clean at `053414728b359d66fba0f2a02e1229088e505d1a`. It used the selected Go1.25.7 executable (SHA256 in evidence), a new owned empty build cache, and the existing owned prehydrated module cache. Create/remove probes inside those two caches passed; no cache permissions changed. No TypeScript preparation or test execution occurred.

| Observation | Result |
|---|---|
| Default selected capture before compile | Passed, 9.611 s |
| `go test -c -o <private-output>/handlers.test ./internal/api/handlers` | Compile only, exit 0, 23.234 s; zero test binaries executed |
| Strict default receipt validation after compile | Rejected, 7.514 s; observed receipt and raw output preserved |
| Tracked source afterward | Clean and unchanged |

Receipt identity differences were exactly `commands[i].stdout_sha256` for indices **1, 3, 4, 5, 6, 8, 9, 10**. The attestation and unbound hydration diagnostics also changed as consequences/outside the identity domain. All other receipt fields matched, including selected input hashes, package records, MVS and coverage. Raw comparison of every command proved that Go-list differences were exclusively **`Stale` and `StaleReason`**. The selected union had 995 changed field locations; root and explicit coverage each had 953, and variant-directory coverage had 104, with matching repeated-command changes. Raw Go-env also changed its GOGCCFLAGS temporary prefix, which remains within the pre-existing normalization domain and did not change its attested stdout hash.

This establishes that this compile-only intervention changed Go-list cache diagnostic fields and triggered the retained strict hash policy in this new control. It does not identify the missing raw fields in old receipts, prove compatibility's old child failure cause, prove all future compiles stabilize, or qualify any batch. Metadata controls stopped after rejection. No compile warmup was added to preparation.

## Evidence, history and next decision

Only [control facts](evidence/R5-VALIDATION-ENV-EVIDENCE-20261004/control-summary.json), [private file sizes/hashes](evidence/R5-VALIDATION-ENV-EVIDENCE-20261004/private-evidence-hashes.json) and [test attempt counts](evidence/R5-VALIDATION-ENV-EVIDENCE-20261004/test-summary.json) are committed. Raw evidence, candidate/observed receipts, compile output, test logs, the control script and independent checkout remain in the owned private root `/tmp/r5-env-evidence-20261004`; raw retention is in its `raw/` subtree. Copy hashes cannot make these diagnostics passing receipts.

Read repository versioning, runner/preparation/selection/collector code and relevant tests, plus diagnostic commit `6dd507f565f2073ed1582d63cbc03ed10530014e` recommendations. This source tree has no applicable root/scripts `.agents` or skill files; its only AGENTS.md governs untouched web work. No earlier failure receipt was edited. Default `88ba149cf20e4393e37a465f7b030eb282f8bcb9` remains 634/675 observed with 41 missing and 3 class setup errors; compatibility's original cause is unknown. Root-provided DB and component reports retain their rejected post-binding/zero-qualified outcomes.

Proposed next preparation, subject to root decision: independently review the selected environment and private retention changes, then separately design a compile-only stabilization experiment in the final owned clone before its first binding, covering exactly the intended default/race/tag/fork compilation contexts. A handlers-only compile demonstrably changes metadata after an existing binding; it does not prove complete cache stabilization. Preserve raw comparison and every failure, bind only afterward, and retain strict mandatory postchecks. This proposal was not implemented or run. No metadata policy weakening, result waiver, formal rerun, merge, deploy or actual email is authorized by this handoff.

Normal branch push is authorized. Draft PR text is retained privately for review; no PR/API creation action is retried because the delegation explicitly prohibits repeating previously denied PR/API actions or rerouting them.

## Independent P2 rejection and bounded completion

Independent `5b7368286b292ed540293bcb04d1d4c1bdc2e1c4` rejected df6 for secondary retention failures replacing producer exceptions and a double close after fdopen ownership transfer. That original red report and programs are preserved unchanged. Implementation `4b75d4067aae2626cbf22cf15fe6e8ac1dc1d67e` fixes only those two issues; [completion evidence](evidence/R5-ENV-EVIDENCE-P2-FIX-20261004/README.md) records original37 + own8 + unchanged independent23 passing. The inherited 82-test invocation still has exactly the two independently documented source-inventory errors (25/26 transaction and 18/19 compatibility); neither catalog is repaired or waived. No new formal/metadata control ran. Freeze remains subject to root review.
