# Frozen union default qualification — 2026-10-04

Runtime source: `f52f8cbbf31dcbfb72ba062af6623248334a2e99` (draft PR27). Evidence-only branch: `cloud/r5-union-default-f52-20261004-0441`. This delivery is not a new runtime source qualification.

**FAIL / exit1.** The unchanged `scripts/run_r5_source_version_tests.py` was invoked exactly once after locked dependency and source-selection preparation. It independently cloned the exact source, performed fresh typed preparation, and dispatched its declared current/frozen partitions. No historical receipt was injected and historical 675 was not forced as an expected answer.

| Partition | Source | Discovered/requested | Executed/pass | Class setup errors | Missing | Assertion failures/skips |
| --- | --- | ---: | ---: | ---: | ---: | ---: |
| Current | f52f8cbbf31dcbfb72ba062af6623248334a2e99 | 671 | 630 | 3 | 41 | 0/0 |
| Frozen v1 | 41b015c30c66b3ba58a3c1395e8559ebcd27a65f | 4 | 4 | 0 | 0 | 0/0 |
| Total | Separate declared partitions | 675 | 634 | 3 | 41 | 0/0 |

No overlap; dispatch coverage was complete but execution coverage was not. Frozen 4 is the runner's historical compatibility partition, not new whole-product evidence. Nested fixture-test failure output is not counted as a top-level assertion failure; the authoritative JSON controls the counts.

## Preparation and runtime identity

Python 3.12.14 used a private venv with the original exact `scripts/requirements-contract.txt` closure. Go 1.25.7, two original local module replacements, official npm lock SHA256 `b839b59e9aa06133819adca60659e0f807ca1e321fbdc35fe55afe1c7b52eba3`, and verified TypeScript 5.9.3 archive SHA256 `10e108c9cf7d5f2879053dff18515fb405abf2ccef63eaaf017d9c571687a1d3` were retained. Dependencies were hydrated before binding; module download/verify and default/race hydration passed. Platform TLS verification was preserved. This default run required no PostgreSQL, HTTP or components service; schema19 and all runtime/test/lock/marker/budget source bytes remained unchanged.

The runner's own fresh typed preparation passed one intended Go fixture test through `linux_parent_proc_fd_pinned_inode`, exporting 21 fixtures. Pre/post preparation Go selection matched. Executed binary SHA256: `07f1b5a42402015c8bf9dd7519e20f2a65402649343d43446ce620399618c3f1`; run SHA256: `87f8e06560b97b368cdb148f324b41afebe328b9abdc02b0775d4540b86aa666`. Formal JSON SHA256: `fc75afe564e099dea47acce8b8ac43eecc208fc928b51d8858a1c94b4f8c06ab`. Raw receipts, fixtures, binary and logs remain only in the owned mode700 private root; this branch contains safe statuses and SHA256 references.

## Failed controls and retained history

Three current class setup errors prevented 41 IDs: `CompatibilityGateTests` and `CurrentHistoricalBoundaryTests` (15 missing) plus `TransactionInventoryTests` (26 missing). Compatibility's Go route-inventory subprocess returned nonzero; its captured child diagnostic is not included in the surfaced exception, so no underlying product cause is inferred. Transaction's AST extraction explicitly failed to initialize a default Go build cache on a read-only filesystem outside the owned cache. R5_TEST_* preparation/binding paths alone did not establish writable general collector caches. No configuration change, product edit or formal rerun followed the failure.

Post-run source checkout was clean at the exact frozen SHA. Every one of 2,148 tracked file hashes matched, including product/tests/locks/markers/budgets, migrations, central TODO and all previous evidence. Tracked-file manifest SHA256: `adf17855c875f5828933fe8911c50696f870fa3d04279e06a17202f5b6fcccf0`. Race-context selected binding revalidated. Default-context revalidation failed with metadata identity mismatch; it remains a failed control despite tracked-byte preservation. The unchanged validator does not retain its observed receipt on rejection; its private diagnostic hash is published, and no repeated validation was used to overwrite the failure.

Rejected setup history is retained: the initial environment lacked a pinned Python validator; correction used only original pinned requirements in an owned venv. The first worktree hydration failed Go VCS discovery beneath the workspace placeholder .git; correction used a new owned real clean clone outside that parent without buildvcs/compiler/source overrides. Original logs and hashes were preserved before preparation proceeded. These were pre-formal corrections, not reruns or rejected-action rerouting.

## Cleanup and remaining boundary

The runner completed and removed its own disposable current/frozen clones. The execution-owner real checkout and rejected worktree were removed after source controls. No PG, service or listener was started and no existing resource was cleaned. Private receipts and the evidence worktree remain for review. No automatic approval denial occurred during execution.

The frozen union remains **UNQUALIFIED**. sharedDB/sharedHTTP/components, wholePG180, audit, full Web/build/lint, CI and central closure were not run. No merge, deploy, actual email, dependency upgrade or production edit occurred. Any new formal run needs separate authorization after inventory-cache preparation and default metadata mismatch are resolved. [Safe summary](evidence/R5-UNION-DEFAULT-F52-20261004/summary.json), [pre/post controls](evidence/R5-UNION-DEFAULT-F52-20261004/post-controls.json), [retained preparation history](evidence/R5-UNION-DEFAULT-F52-20261004/preparation-history.json), [private artifact hashes](evidence/R5-UNION-DEFAULT-F52-20261004/private-artifact-sha256.json), and [exact proposed TODO append](evidence/R5-UNION-DEFAULT-F52-20261004/proposed-TODO.md). Central TODO was not edited.

GitHub GraphQL PR27 lookup returned Forbidden during delivery preparation. Draft creation is blocked and was not attempted; the denied action was neither retried nor rerouted. Normal Git branch push is separately authorized; delivery success is reported from its actual result. [Delivery status](evidence/R5-UNION-DEFAULT-F52-20261004/delivery-status.json).
