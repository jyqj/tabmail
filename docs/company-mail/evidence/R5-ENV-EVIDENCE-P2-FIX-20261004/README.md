# Bounded P2 retention completion

Implementation tested: `4b75d4067aae2626cbf22cf15fe6e8ac1dc1d67e`, on the existing branch after rejected `df6ffe046611f5b2121f4d48d5114a2ececd2c24`. No general collector, dependency, catalog, selected identity or formal-run repair is included.

The original independent rejection at `5b7368286b292ed540293bcb04d1d4c1bdc2e1c4` is imported byte for byte under [its original evidence prefix](../R5-ENV-EVIDENCE-INDEPENDENT-20261004/README.md), including the red summary and review programs. Its red results remain history. [independentreview_checks.py](independentreview_checks.py) is a byte-identical alias of that review's `review_checks.py`; no requirements were changed. The original program prints the original candidate constant `df6ffe0`; these new executions imported current implementation `4b75d40`. Its static AST comparison still targets original base/df6, while its behavior regressions exercise the fixed imports.

## Exactly two implementation fixes

1. `Retention.failed_command()` attempts diagnostic retention for an already failed producer. On a limit/write exception it attaches the actual secondary exception in the producer's `retention_errors` tuple and adds a safe traceback note containing only type and errno. It returns so compatibility and selected timeouts rethrow the original exception object with unchanged command, returncode/timeout, stdout and stderr. A completed nonzero metadata command constructs its original `MetadataCommandFailure` interface before retention; a secondary failure cannot erase those fields. No raw output or filesystem error message is included in the safe note. This fallback records secondary errors even when disk/limits make further persistence impossible. A successful producer still propagates required-retention errors and remains fail-closed.
2. `write()` retains raw descriptor ownership only until `fdopen` returns. After transfer, context cleanup owns closing and the exception handler never closes the numeric descriptor again. A failure before transfer still closes the raw descriptor. Partial-file removal, unchanged counters after failed writes, and sink closure remain in place. The independent real-descriptor regression proves a reused `/dev/null` descriptor survives cleanup.

## Exact checks and remaining red evidence

| Check | Current result |
|---|---|
| Original focused environment/diagnostic checks | 17 passed |
| Original runner/preparation/binary checks | 15 passed |
| Original selected-v2 negative checks | 5 passed |
| New fix checks | 8 passed |
| Unchanged independent program | 23 methods passed, including real 64 MiB + 1 overflow and descriptor-number reuse |
| Transaction source inventory tests | 25 passed, 1 existing inventory error |
| Compatibility source inventory tests | 18 passed, 1 existing inventory error |

The three final invocations total **113 outer tests: 111 passed, 2 errors**, with no outer skips or failures. The 37 original focused checks, 8 fix checks and 23 independent methods give **68 distinct passing focused checks**. The inherited runner test intentionally asserts a failing, skipped and expected-failing inner child; those child outcomes are not product passes.

The two errors are the exact independently documented inventory test IDs in [fix-summary.json](fix-summary.json): transaction `test_current_inventory_is_only_structure_evidence` rejects employee-disposition/caller/file inventory drift; compatibility `test_current_source_map_is_not_product_or_dependency_completion` rejects `source hash drift`. Both catalogs remain byte-identical to df6. The independent original report retains its base reproduction and source-only equality proof. Those errors are not waived or counted as passes.

The initial inherited invocation ran 67 outer tests with 3 errors: the transaction inventory error plus two compatibility class setup errors caused by absent locked TypeScript bytes. The failed invocation and a direct failed Node diagnostic probe are retained privately and hashed in the summary. Restoring only the verified official 5.9.3 archive from the existing npm cache (132 files, unchanged lock/archive hash) let the final invocation reach all 82 inherited tests and both existing inventory errors. No dependency versions, expectations or catalogs changed; no download occurred.

Raw logs stay in owned mode700 `/tmp/r5-env-p2-fix-20261004`, files mode600. Git contains this report, unchanged review programs/history and safe counts/size/hash summaries only. An isolated owned build cache was used with explicit existing Go1.25.7, existing module cache, Node PATH and `GOPROXY=off`. Live Go compilation/execution was limited to the existing AST producer and exact source-only architecture route inventory test. There was no default formal runner invocation, live selected metadata capture, compile stabilization experiment, sharedDB/HTTP/components/wholePG180 rerun, catalog correction, merge/deployment/email or PR/API action. Existing raw metadata/compile-control rejection and qualification blockers remain unchanged.

Reproduction from the checkout root, with the existing locked TypeScript prepared, uses the controlled environment from the independent README and these three commands:

```sh
python3 -B -m unittest -v test_r5_env_diagnostics test_r5_source_version_runner test_r5_selected_source_binding_v2.SelectedV2NegativeTests test_r5_transactions test_r5_compatibility
# Expected outer result: 82 tests, 2 existing inventory errors; exit 1.
python3 -B -m unittest -v test_r5_retention_failures
# 8 passed; exit 0.
python3 -B docs/company-mail/evidence/R5-ENV-EVIDENCE-P2-FIX-20261004/independentreview_checks.py
# 23 passed; exit 0. Original declared candidate label is preserved.
```

Independent root review is still required. Final branch head is the review freeze; its implementation files are identical to the tested implementation SHA above.
