# Independent bounded P2 delta review

**ACCEPT the two P2 fixes within this bounded review.** Exact tested source: `3f34c31ed51a721b318a76512805e4a14dc292c3`. Repair base: `df6ffe046611f5b2121f4d48d5114a2ececd2c24`. No new defect was found in the reviewed delta. This closes the two implementation findings from the [original independent rejection](../R5-ENV-EVIDENCE-INDEPENDENT-20261004/README.md) for the fixed source only; that original red evidence remains unchanged. It does not approve a formal batch, resolve inventory drift, or qualify the product.

## Findings resolved

- **Original producer failures survive secondary retention failures.** Compatibility now calls `Retention.failed_command()` before rethrowing the same `CalledProcessError` or `TimeoutExpired`. Selected capture retains the same timeout object, and constructs `MetadataCommandFailure` before attempting retention for a completed nonzero producer. Its `command` object still includes exact role, argv, returncode, raw-stdout hash and decoded stderr; original stdout/stderr bytes remain accessible. Limit, write and encoding failures are attached as actual secondary exception objects in `retention_errors`, with traceback notes limited to exception type and errno. Successful producer retention failure remains fail-closed.
- **Transferred file descriptors have one cleanup owner.** `write()` clears raw ownership after `fdopen` returns. Write or file-context exit failure closes through the file object and removes the partial file without a second close of the numeric descriptor. A failure before `fdopen` returns still closes the raw descriptor. Both the original real-descriptor reuse repro and a fresh file-context exit/close fault leave the newly allocated `/dev/null` descriptor alive. Failed-write counters stay unchanged; retry succeeds; sink descriptors close.

Sources: `scripts/check_r5_compatibility.py:45`, `scripts/r5_selected_source_binding_v2.py:294`, `scripts/r5_selected_source_binding_v2.py:298`, and `scripts/r5_private_diagnostics.py:77`–`84`, `:103`–`111`. Review was of the complete source delta, not just the new tests.

## Independently executed checks

| Suite | Result |
|---|---|
| Original independent review program, byte unchanged | 23/23 pass |
| Original environment/diagnostic checks | 17/17 pass |
| Original runner/preparation/binary checks | 15/15 pass |
| Original selected-v2 negative controls | 5/5 pass |
| Author's P2 completion checks | 8/8 pass |
| Fresh delta checks | 10/10 pass |

Total: **78 outer tests passed**, zero outer failures, errors, skips or expected failures. The existing runner test still deliberately runs failing, skipped and expected-failing inner children and verifies that each stays non-green; these are controls, not product passes.

The original 23-check program is byte-identical to the previously rejected program (`SHA256 be06635e2583374e4415b2059071fed97feeaab026b75c7d7d53e716de870ffd`). Its printed `source_sha` remains the original df6 constant, and its static AST proof still compares f52/df6. This execution imported the implementation files from checked-out **3f34c31**; [summary.json](summary.json) separately identifies the actual tested source and hashes all imported implementation files. We did not edit the 23 requirements or relabel its raw output.

Fresh [delta_checks.py](delta_checks.py) covers real Python child nonzero exit and a real 0.3-second timeout, preserving the exact producer exception object and raw binary/partial outputs under injected retention failures. Other checks inject faults at stderr and summary retention after stdout is already saved, exhaust file count midway through a command, exercise a Unicode encoding failure, preserve pre-existing notes and None outputs, fail the selected hydration command after a successful Go-env observation, and verify its complete `MetadataCommandFailure` interface. Filesystem failure and descriptor scheduling are controlled injections; descriptor creation, close and `fstat`, as well as the two Python child processes, are real. The original program also rechecks the real 64 MiB + 1-byte retention bound and descriptor-number reuse after partial-write failure.

## Identity policy and baseline blockers

The new AST proof compares **df6 directly with 3f34c31**: all selected-binding module statements outside `_capture` match, including `validate`, constants and identity policy. The complete `_capture` body outside its nested command executor also matches. Manual inspection of the command executor confirms the only change is failed-producer diagnostic sequencing; command records, normalization, repeated raw-stdout equality and receipt assembly are preserved. The compatibility change is confined to its failed-command retention call. Directory traversal, root/mode/owner rules, exclusive files and bounds remain unchanged.

Go environment selection, preparation, both runner groups, transaction collection, both source-inventory catalogs, dependency files and locks are byte-identical to df6. This delta adds no fallback behavior, compiler context, receipt exception, catalog correction or cache-warmup policy. Diagnostics cannot authorize changed receipts; the original strict negative controls still pass. Caller isolation, mode700/mode600 handling and the trusted-same-uid/per-operation limits remain the previously reviewed boundaries.

The **two existing source-inventory errors remain explicit and unwaived**:

1. `test_r5_transactions.TransactionInventoryTests.test_current_inventory_is_only_structure_evidence` rejects employee-disposition/caller/file catalog drift.
2. `test_r5_compatibility.CompatibilityGateTests.test_current_source_map_is_not_product_or_dependency_completion` rejects stored source-closure hash drift.

The previous independent review proved both at the base and candidate; the author's fixed-source general run also retained both. This bounded delta review did not rerun or repair those general collectors. Their relevant source and catalogs were checked unchanged. The prior compile-only metadata control rejection and all earlier qualification blockers remain in force. No new live selected capture or stabilization experiment occurred.

## Reproduction and evidence

Run from a checkout of the fixed source using the existing Go1.25.7 and module cache, an isolated owned build cache, and no diagnostic-root opt-in for the outer environment. Each diagnostic test creates its own private synthetic root.

```sh
env -i HOME=/home/agent PATH=/usr/bin:/bin PYTHONDONTWRITEBYTECODE=1 \
  PYTHONPATH=scripts:scripts/tests R5_TEST_GO=/workspace/tabmail-cloud/tools/go/bin/go \
  R5_TEST_CACHE=/tmp/R5-ENV-EVIDENCE-P2-DELTA-INDEPENDENT-20261004/cache \
  R5_TEST_MODULECACHE=/workspace/tabmail-cloud/gomod GOPROXY=off \
  python3 -B docs/company-mail/evidence/R5-ENV-EVIDENCE-INDEPENDENT-20261004/review_checks.py

# Use the same controlled environment for these two commands:
python3 -B -m unittest -v test_r5_env_diagnostics test_r5_source_version_runner \
  test_r5_selected_source_binding_v2.SelectedV2NegativeTests test_r5_retention_failures
python3 -B docs/company-mail/evidence/R5-ENV-EVIDENCE-P2-DELTA-INDEPENDENT-20261004/delta_checks.py
```

All three invocations exited 0. Raw stdout/stderr remain mode600 in owned mode700 `/tmp/R5-ENV-EVIDENCE-P2-DELTA-INDEPENDENT-20261004`; only the review program, report, safe facts and size/hash metadata are committed under this new unique prefix. Candidate source and the old evidence are unchanged. `git diff --check` passed. Existing repository versioning/instruction boundaries remain applicable; no new root/scripts AGENTS.md or repository skill was introduced.

No formal default/sharedDB/HTTP/components/wholePG180 run, real service/PG access, dependency installation/upgrade, actual email, catalog repair, merge, deploy, or PR/API action occurred. Root retains the decision about subsequent work and formal runs.
