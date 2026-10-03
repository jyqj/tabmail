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
