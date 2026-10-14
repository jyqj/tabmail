# Independent R5 environment and private diagnostic review

Decision: **REJECT the complete repair pending root fixes**, while accepting the bounded Go/cache propagation portion within the tested Linux/amd64 context. Reviewed source: `df6ffe046611f5b2121f4d48d5114a2ececd2c24`, against `f52f8cbbf31dcbfb72ba062af6623248334a2e99`. This evidence commit changes no candidate implementation. It does not qualify any formal batch or waive any old failure.

## Blocking findings

1. **P2: diagnostic retention failures replace producer failures and timeouts.** `scripts/check_r5_compatibility.py:45` attempts retention before rethrowing the original `CalledProcessError` or `TimeoutExpired`. A retention limit or filesystem failure escapes instead. `scripts/r5_selected_source_binding_v2.py:294` has the same timeout problem; line 296 attempts retention before constructing `MetadataCommandFailure`, losing its command/returncode/stdout/stderr interface. Fresh checks reproduce both exception types with scaled bounds, `ENOSPC` with an injected write fault, and an actual 64 MiB + 1-byte output against the unchanged production bound. These are failed producer paths, not a request to accept a successful producer whose diagnostic retention failed. Invalid roots and over-limit retention may remain fail-closed; they must not erase the original producer failure. Repros: `RequirementRegressions.test_compatibility_bound_must_preserve_original_exception`, `test_compatibility_write_error_must_preserve_original_timeout`, `test_compatibility_production_bound_must_preserve_original_failure`, and `test_selected_bound_must_preserve_metadata_failure_and_timeout` in [review_checks.py](review_checks.py). Preserve the original exception object/metadata, with any retention failure reported separately. Do not weaken identity validation.

2. **P2: partial-write cleanup can close an unrelated reused descriptor.** At `scripts/r5_private_diagnostics.py:77`, `os.fdopen` takes ownership of `fd`; its context closes it on write/flush failure. The handler at line 81 then closes that numeric descriptor again. If another thread acquires the freed descriptor number between those closes, cleanup closes the other thread's file. `RequirementRegressions.test_write_failure_must_not_close_reused_descriptor` deterministically schedules that interleaving with real descriptors: write a partial file, inject `ENOSPC`, close through the file context, allocate `/dev/null` at the same descriptor, then observe `EBADF` after candidate cleanup. The filesystem write fault and scheduling are injected; the descriptor allocation, close and final `fstat` are real. Distinguish a failure before `fdopen` transfers ownership from a failure after transfer; close only descriptors still owned by the raw-fd path. Keep partial-file removal and sink closure.

## Independent results

| Check | Observed outcome |
|---|---|
| Author's environment/diagnostic controls | 17/17 passed |
| Pre-existing runner, preparation and binary boundary controls | 15/15 passed; intentional failing/skipped/expected-failing inner children stayed non-green |
| Pre-existing selected-v2 negative controls | 5/5 passed |
| General transaction tests | 25 passed, 1 error; identical base error reproduced |
| General compatibility tests | 18 passed, 1 error; identical base error reproduced |
| New independent tests | 23 methods: 18 passed, 5 failed methods containing 7 failing assertions/subtests; no errors, skips or expected failures |
| Syntax-only base/candidate comparison, with incoming PATH containing no Go | Exit 0; exact AST, migrations, compatibility source facts and actual closure equality |
| Whitespace and candidate source preservation | `git diff --check` passed; candidate implementation/settings unchanged |

The final inherited run executed 82 outer tests with 2 errors, no failures or outer skips. Both errors are retained blockers, not counted as passes. All inherited negative controls ran without replacing catalogs or pinning fake hashes. The new requirement tests remain deliberately red on this candidate; their expected semantics have not been changed to fit the implementation.

Two initial harness issues are recorded in [summary.json](summary.json) and private log hashes: the first inherited attempt omitted the existing Node executable's directory from PATH, causing two class setup errors; the first new attempt expected mode755 in its same-uid race fixture despite the process umask077. Subsequent runs supplied the existing Node PATH and explicitly chmodded the synthetic replacement directory. No candidate repair was made.

## Existing source inventory blockers

The transaction gate rejects stale catalog facts for `internal/store/postgres/employee_disposition.go` and affected callers/file hashes. The base's 26 transaction tests reproduce its same single error. Fresh syntax collection at both commits produces exactly the same canonical AST SHA256, `0384d48441d0791e70904af7782cd6dbedd475f46a2964517083afcdcb7ec56b`; migrations and full rejection messages also match. This is pre-existing source inventory drift, not an environment helper regression.

The compatibility gate rejects `source hash drift` at both commits. Fresh Go route/Node syntax collection produced 132 routes and 134 client branches. The actual source-facts and closure maps match the base exactly. The two stored closure hashes differ from actual source for `internal/api/handlers/r5_protocol_component_observations_test.go` and `internal/store/postgres/r5_protocol_shared_test.go`; [summary.json](summary.json) records the exact stored/observed hashes. Historical-safe metadata and anti-laundering checks passed. Neither source gate is product/runtime qualification.

## Environment, retention and unchanged semantics

Independent checks cover primary/legacy tool precedence, empty primary fallback, cache mapping, caller dictionary isolation, standalone legacy flag preservation, the existing Darwin cached transaction fallback, a selected transaction tool without a PATH Go, preparation's passed-tool precedence, and current/frozen runner environment and fresh-fixture separation. Real syntax-only producers succeeded with incoming PATH set only to the existing Node directory; transaction collection still uses `GOPROXY=off`, timeout90, and compatibility retains the exact readonly/count1/route-test/timeout60 command. No external service environment was used in actual collectors; mocked environment checks use synthetic service values only.

Explicit removal of GOOS/GOARCH/GOAMD64/GOEXPERIMENT, CGO and CC/CXX overrides is intentional for the declared selected-tool context. The metadata collector still independently fixes Linux/amd64, CGO1, default/race-r5protocol contexts, `GODEBUG=asynctimerchan=0`, toolchain-local and workspace/env/flags off. Preparation still builds/runs from the attested default environment, and its passed executable controls initial selection. Standalone collectors without an explicit tool retain legacy compiler/flag behavior. No unintended compiler/source-context change was found in the tested scope; arbitrary cross-compilation/custom compiler use is outside this selected Linux/amd64 policy.

AST comparison proves the complete old capture and validate bodies are unchanged after removing only new diagnostic statements/wrappers. Context constants, selection/MVS argv, package/file classification, coverage checks and repeated raw-stdout equality are identical. Existing valid/extra-field/raw-hash tests confirm diagnostics never authorize changed receipts. Frozen v1 helper/test bytes, original source pin, product code, two forks, dependency files, compiler context, formal test selections, limits and expectations remain unchanged. No live metadata capture or compile stabilization experiment was needed for this review.

Retention checks cover opt-in absence, mode700 roots/mode600 exclusive files, ownership/public-root rejection, real Git ancestor/worktree marker rejection, symlinked ancestors/files/markers, parent escapes, duplicate filenames, UUID collisions, accumulated file/count/total bounds, the real 64 MiB and 64-file boundaries, partial writes, constructor/context descriptor closure, and renamed sink paths. Open directory descriptors prevent later pathname replacement from redirecting writes. As a trust boundary observation, an actor with the same uid and write authority can replace the newly created operation directory before it is opened, and the candidate does not recheck that child's mode; the synthetic replacement is accepted at mode755. This actor can already read/chmod/relocate owned private data, so this is not presented as a cross-user confidentiality defect or an additional blocker. The documented bounds cover retained bytes per operation, not subprocess in-memory capture or total lifetime operations; those documented limits are not new defects.

The supplied compile-only control at `053414728b359d66fba0f2a02e1229088e505d1a` remains a reported strict rejection. Its committed summary was read; original private raw evidence is not available in this review workspace, so its Stale/StaleReason-only claim is not independently re-established here. No old receipt was modified, and it grants no qualification or cache-warmup policy exception.

## Reproduction and evidence handling

Use the frozen candidate checkout. Only add/run these review files; do not invoke the default source-version runner. The programs emit synthetic test details or syntax facts, not passing product receipts.

```sh
env -i HOME=/home/agent PATH=/opt/codex/runtimes/codex-primary-runtime/dependencies/node/bin:/usr/bin:/bin \
  PYTHONDONTWRITEBYTECODE=1 PYTHONPATH=scripts:scripts/tests \
  R5_TEST_GO=/workspace/tabmail-cloud/tools/go/bin/go \
  R5_TEST_CACHE=/tmp/R5-ENV-EVIDENCE-INDEPENDENT-20261004/cache \
  R5_TEST_MODULECACHE=/workspace/tabmail-cloud/gomod GOPROXY=off \
  python3 -B -m unittest -v test_r5_env_diagnostics test_r5_source_version_runner \
  test_r5_selected_source_binding_v2.SelectedV2NegativeTests test_r5_transactions test_r5_compatibility

# Same controlled environment; intentionally exits 1 with the findings above:
python3 -B docs/company-mail/evidence/R5-ENV-EVIDENCE-INDEPENDENT-20261004/review_checks.py
```

[collector_comparison.py](collector_comparison.py) additionally requires a clean no-hardlinks base clone at `/tmp/R5-ENV-EVIDENCE-INDEPENDENT-20261004/base`, the same explicit tool as `R5_GO` for the base, and matching `GOCACHE`/`GOMODCACHE` for its legacy environment. Its actual run used PATH containing only the Node executable directory. It writes exclusive private comparison outputs and stops on any unexpected inequality. No dependencies were installed or upgraded; existing Go1.25.7, module cache and TypeScript bytes were used. Compilation was limited to the AST producer and the exact source-only architecture route test, with an isolated build cache. No candidate product test binary was executed.

Raw logs, fresh syntax JSON, the independent base clone and isolated build cache remain in owned mode700 `/tmp/R5-ENV-EVIDENCE-INDEPENDENT-20261004`. Only review programs, this report, safe summary facts and private log size/hash metadata are committed under this unique evidence prefix. Repository versioning instructions and the full changed implementation were read. There is no applicable root/scripts AGENTS.md, .agents or repository skill; the only AGENTS.md governs untouched web work.

No formal default/sharedDB/HTTP/components/wholePG180 batch, real service/PG access, migration, actual email, dependency change, merge, deployment or PR/API action occurred. Root owns repairs and any later formal-run decision. Historical failures and all qualification blockers remain open.
