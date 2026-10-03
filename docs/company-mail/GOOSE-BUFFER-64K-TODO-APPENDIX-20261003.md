# Goose 64 KiB initial-buffer isolated experiment

Fixed product source: PR23 `aeed47f0e1998d9925acb680992b051463821d69`.
This appendix does not amend the central R5 TODO, original receipts or dependency qualification. PR23's subsequently observed `e5e23e2` tip adds documentation/evidence only.

Official Goose v3.27.3 registry zip matches original go.sum and an independently recomputed Go zip Hash1. Temporary baseline and candidate trees differ only in `bufferPool.New`: initial allocation 4 MiB → 64 KiB. Both `ParseSQLMigration` and `endsWithSemicolon` still pass the original 4 MiB scanner maximum. The actual selected 3,995 source files differ only in this parser line after temporary tree path normalization. The diagnostic bridge was removed before wholePG compilation. The product go.mod/go.sum, two original replaces/forks and all 19 migration files remain byte-identical (270 protected pins).

Parser differential **PASS**: 600 complete JSONL rows (173,812,438 bytes) compare byte-for-byte, including 38 Up/Down migration rows and 167 error rows. Ordered statement bytes, nil/empty distinctions, useTx and full errors are included. Cases cover StatementBegin/End, comment handling, NO TRANSACTION, ENVSUB ON/OFF and missing variables, 64 KiB ±2 and 4 MiB ±2 lengths, newline/no newline, ScanWords, explicit statement blocks, EOF-with-data and 997-byte chunked EOF readers. Only `GOOSE_EXPERIMENT_VALUE=FROZEN_NON_SECRET` and GODEBUG are present; other environment variables are absent. The committed case table uses lengths and hashes after the full raw equality check; full large outputs remain local.

| Whole PostgreSQL package | Baseline | Candidate |
|---|---:|---:|
| Invocations | 1 | 1 |
| Original timeout | 180 s | 180 s |
| Wall, excluding preparation/compilation | 180.506 s | 180.108 s |
| Top-level IDs started / inventory | 226 / 404 | 252 / 404 |
| All tests/subtests passed | 448 | 681 |
| Failure events, including timeout | 6 | 1 |
| Top-level IDs never started | 178 | 152 |
| Started IDs without terminal event | 20 | 20 |
| Browser skip, as default backend scope | 1 | 1 |
| Result | TIMEOUT / FAIL | TIMEOUT / FAIL |

**Comparison invalid:** the self-authored runner executed the baseline precompiled binary from the repository root, causing relative migration/snapshot path failures. Candidate cwd was corrected to `internal/store/postgres`. Baseline was not repeated. Raw failures and full unstarted/unfinished ID lists are retained. Candidate had no assertion failure before timing out in `TestR5PermissionAuthorityMatrixLockWaitRecheck/freeze/assignment`. Precompiled direct test2json streams lack a final package terminal on panic; that is retained, never filled in or treated as success. This is the complete unfiltered PostgreSQL package, not the root `./...` backend gate.

Both binaries were built with Go1.25.7 and race; actual build info confirms linux/amd64, CGO=1 and GOAMD64=v1. Both runs freeze GOMAXPROCS=2, GODEBUG=asynctimerchan=0, default build tags, count=1 and the same 180s timeout. They use separate freshly initialized owned PostgreSQL 17.11 clusters, fresh empty admin DBs and original fixture-owned databases, original migrations and original synchronous cleanup. fsync/full_page_writes/synchronous_commit remain on and max_connections is 100. SMTP fixtures are the unchanged tests' owned loopback servers. No migrated DB templates, race disabling, gate slicing or timeout increase was used. Both clusters are stopped. Baseline timeout left one owned fixture database; candidate left no fixture database. Admin DBs and stopped local data trees are not published.

Serial diagnostic allocation for parsing the 19 migrations once in both directions under race: baseline 961,345,000 bytes / 11,746 mallocs; candidate 14,829,352 bytes / 11,529 mallocs. These are a small diagnostic, not wholePG speed/allocation qualification. WholePG default-rate allocation profiles were collected, including timeout profiles. Their analysis invocation omitted owned GOCACHE and failed attempting `/home/agent/.cache/go-build` on the read-only filesystem. No retry, escalation, permission change or bypass was attempted. The command failure and profile SHA-256 pins are recorded; wholePG allocation totals remain **unparsed**. This was a process filesystem error, not an automatic approval-review rejection.

Open follow-up for root: decide whether any official upstream patch/fork strategy is acceptable. A new baseline run is outside this experiment's exhausted one-baseline limit; do not silently repeat it. Neither wholePG failure closes a parent TODO or qualifies a third product replacement. Whole root/CI, Method19/Catalog/SML, million M, runtime and shipping remain unqualified.

Evidence: [experiment receipt](evidence/GOOSE-BUFFER-64K-EXPERIMENT-20261003/experiment-receipt.json), [parser comparison](evidence/GOOSE-BUFFER-64K-EXPERIMENT-20261003/parser-summary.json), [baseline outcome](evidence/GOOSE-BUFFER-64K-EXPERIMENT-20261003/baseline-result.json), [candidate outcome](evidence/GOOSE-BUFFER-64K-EXPERIMENT-20261003/candidate-result.json). Self-authored diagnostics live in `scripts/experiments/goose-buffer-64k/`. Publication is restricted to these diagnostics, patch text and synthetic redacted/hash evidence, without third-party source trees, zips, binaries, database files or keys. No merge/deploy.

## Authorized existing-profile follow-up, no wholePG rerun

After initial delivery, root explicitly authorized reading the existing profiles with the normal owned GOCACHE/GOMODCACHE. The earlier default-cache OSError is a process filesystem error, not a tool approval-review rejection. Official existing Go1.25.7 `go tool pprof` now exits 0 with empty stderr under the frozen owned-cache allowlist. No access to the default readonly cache, permission change, native restart or test execution was needed. The initial blocker/receipt remain historical records; the latest analysis is [allocation-analysis-followup.json](evidence/GOOSE-BUFFER-64K-EXPERIMENT-20261003/allocation-analysis-followup.json).

| Existing profile observation | Baseline | Candidate |
|---|---:|---:|
| Weighted sampled cumulative allocation bytes | 340,803,930,185 | 7,050,034,796 |
| Weighted sampled cumulative allocation objects | 9,642,438 | 11,712,243 |
| Goose `sqlparser.init.func1` allocation bytes | 340,110,667,457 | 6,214,712,788 |
| Goose pool initializer share of alloc_space | 99.796580% | 88.151519% |
| Pool buffer allocation-size bucket | 4,194,304 bytes | 65,536 bytes |
| Weighted buffer objects in that bucket | 81,086 | 94,787 |

These are the actual integer totals in the existing profiles, independently summed from official pprof `-raw` sample values and checked against `-top`. The sampling period is 524,288 bytes; totals and object counts are weighted sampled **estimates**, not exact runtime `TotalAlloc`/`Mallocs`. Exact wholePG MemStats were not collected and cannot be recovered from these sampled profiles. The 24-byte escaped slice header is accounted for separately in the bucket ledger. The reduced buffer bucket is direct evidence that the intended candidate allocator compiled and executed. The allocations cover different executed work and failed/time-limited suites; no performance ratio or controlled wholePG improvement is claimed. Candidate allocation remains dominated by the same pool initializer.

Profile SHA-256, binary SHA-256, actual source-binding digest and ELF GNU build ID are linked per arm in the follow-up JSON. Both binary hashes match their original compiled-source manifests; all 3,995 selected source hashes were revalidated. The profile's executable mapping path and GNU build ID match `readelf -n` on the corresponding existing binary. Source frames identify the intended arm's `internal/sqlparser/parser.go:88`. Official textual `-top`, object counts, raw symbol/sample records and empty stderr outputs are published, without profiling binaries or third-party source bodies.

Baseline actual cwd was `/workspace/tabmail`; candidate actual cwd was `/workspace/tabmail/internal/store/postgres`. Both streams contain exactly one standalone test package, `tabmail/internal/store/postgres`. Dependency packages were compiled and exercised through these tests, but standalone root/API/other package tests did not run. Complete runtime environment allowlists differ only in the owned admin DB DSN (separate clusters on loopback ports 55437/55438). Both retain the same GOMAXPROCS=2, race binary flags, GODEBUG, PATH/native tool paths, locale and count/timeout. Temporary Goose source, binary, arm profile/admin DB and working directory differ. Baseline compile scheduling did not explicitly pass `-p=1`; candidate did. That scheduling distinction does not repair the behavioral cwd difference.

The cwd error caused these early exits, retained in the original logs; all corresponding candidate scenarios passed:

- `TestR3CoordinatedSnapshotRestoresIntoEmptyTargets`: `../../../scripts/company_snapshot.py` resolved to `/scripts/company_snapshot.py`, rather than `/workspace/tabmail/scripts/company_snapshot.py`. Backup/restore work never ran in the baseline.
- `TestP0GooseRestartAndHistoricalGrantSafety/upgrade_and_restart` and `/preserve_incompatible_grants`: `migrations/00001_baseline.sql` resolved under `/workspace/tabmail/migrations/`, which is absent. Fresh fixture migration and DROP SCHEMA had happened; historical baseline-provider/upgrade/conflict assertions had not.
- `TestArchitectureUpgradeBackfillsExistingEmployeeAssets`: root-relative `migrations/0000*.sql` returned zero files rather than nine; old-schema provider and backfill/upgrade assertions did not run.

Candidate's cumulative 180s alarm reports `TestR5PermissionAuthorityMatrixLockWaitRecheck` running about 2s and `/freeze/assignment` about 0s. The active child stack is `uuid.NewString → testpg.NewPostgres` at `fixture.go:44`, before `CREATE DATABASE` (line 45), reached from the subtest's initial seed at `r5_permission_authority_matrix_test.go:295`. The assignment operation and lock-wait body had not started. Baseline's alarm was in `/demote` initial fixture migration, `Migrate → Goose Provider.runSQL → pgx/database/sql I/O`, not in its after-wait assertion body. The default timeout handler writes the allocation profile and performs a GC before printing panic; other test goroutines may advance while it writes. Thus the profile snapshot and final stack are distinct observations, and the final test2json fail event's package-length Elapsed must not be read as the child test's runtime. Both arms have 19 parallel PAUSE events and zero CONT events; the package never reached that parallel phase.

Candidate slowest completed top-level tests: `TestR5OutboundReceiptKeyExpiryAfterQueryWait` 6.79s, `TestR5AttachmentGCTenantInterruptedSessionResumes` 6.58s, `TestR5KeyUsageAuthorityAdmissionIsolation` 4.78s, `TestR5OffboardingLifecycleHTTPRechecksAfterTenantWait` 4.61s, `TestR5OutboundReceiptRejectsStalePrincipal` 4.34s. The follow-up JSON preserves the slowest 15 for each arm, final event times and exact timeout stack blocks. No CPU profile was recorded, so allocation hotspots do not identify a proven wall-time bottleneck.

The 600-row parser equivalence evidence remains valid. The wrong-cwd baseline remains invalid as a controlled comparison, both wholePG gates remain failed, and a third product replace remains unapproved. Formal go.mod/go.sum, original forks/migrations/receipts and central TODO remain unchanged.
