# NEXT-1543 execution evidence

Ten bounded implementation TODOs are merged and closed in three rounds: 4, then 3, then 3. Batch remaining counts are 6 → 3 → 0. Original parent acceptance remains 10/171, remaining 161. The full explanation is in [the completion ledger](../../R5-NEXT-1543-20261009.md).

## Contents and integrity

| File | Bytes | SHA-256 |
|---|---:|---|
| [bundle-verification.json](bundle-verification.json) | 10548 | `24d9777ce679d652be24420ee9905cfc71088f19e69a2a5b61796429b0768327` |
| [catalog-ci-cutoff.json](catalog-ci-cutoff.json) | 19945 | `32233502db17cf3303c0170171806076df5d070a97c2daa7a1efee2a8498c5f9` |
| [catalog17-validation-packet-v2.zip](catalog17-validation-packet-v2.zip) | 1801735 | `83142d632ca6616620626a0695ff9b8620941e2140c4bc8a6ddd5a0d06f92b17` |
| [local-evidence-manifest.json](local-evidence-manifest.json) | 79813 | `1f08765f6780877278c01431035962a0cb7f041198f1fe6d06b9462f0d028013` |
| [local-evidence-summary.json](local-evidence-summary.json) | 392592 | `99fe21a5e0b20562089d42c8f8cfdb3b43429dfd51dc901fe879867f4e575ff5` |
| [local-execution.zip](local-execution.zip) | 1393337 | `b7c495a9d7703cd2c8847a539d5f6798ef025bf799cf741b440cd9d8ff19d672` |
| [pg-execution-summary.json](pg-execution-summary.json) | 18000 | `b0a704e34cb7644a66d8341c3644ffbef3bce4e7f010c7476b1c98eb334e2052` |
| [rounds-and-merges.json](rounds-and-merges.json) | 1731 | `16b557c8b02adbe189d0599e9eee921ab5b30f1a72674b886f22801330e6835a` |

The local execution ZIP contains 370 retained files plus its internal manifest. Every member was read back, CRC checked and SHA-256 reconciled with the external manifest. The companion catalog ZIP retains the original generation, failed full source-version execution, clean V2 three-CLI/104-test receipts and independent review records.

Main archive paths are the original workspace-relative paths. For an original absolute `/workspace/scratch/472e5abb7d28/...` reference, remove that workspace prefix to locate the member. Catalog references resolve through the companion package's `packet-files.json`; that package is intentionally separate from the main archive. No repository clones, candidate/baseline worktrees, dependency trees, database data directory, passwords or PostgreSQL binaries are included.

## Actual PostgreSQL qualification

PostgreSQL 16.15 and Go 1.25.7 ran the unchanged reviewed attestor on exact public V2 candidates. Mailbox-grants: 11P/3F → 14P. Derived-text: 2P/6F → 8P. Reconcile-ledger: 12P/7F → 19P. All 41 candidate leaves passed; all-level skip counts are zero. Baseline and candidate share the frozen test bytes and complete leaf sets. All sources remained stable, candidates clean. Both frontend and root reconciled the raw Go event streams.

`pg-execution-summary.json` contains all source/tree/test/comparison/log hashes and normal server shutdown identity. The actual source is distinguished from the earlier local cumulative Go/web source. That earlier combined Go suite reports 1896 PASS and 10 existing PostgreSQL recovery SKIP; skips and compile-only checks are not passing database tests. Frontend related coverage is 371 PASS; intersecting smaller test groups are not added together.

## Retained failure history and limits

The V1 mailbox fixture incorrectly referenced a nonexistent `outbox_events.tenant_id` column. Its raw failures and original test bytes remain. V2 changes only the tenant predicate to the payload JSON field and keeps all 14 original leaves and assertions. The corrected baseline/candidate comparison was executed again on real PostgreSQL.

All failed iterations, source-drift rejections, canceled/queued cloud execution states and independent-review probes retain their original identities. The catalog V1 complete source-version runner remains FAILED with 4 failures and 6 error entries across 873 executed IDs. Catalog V2 full original CI is separately pending in PR #240. This evidence grants the listed bounded implementation acceptance only, not full release or parent completion.

## Independent final archive review and CI cutoff

[bundle-verification.json](bundle-verification.json) independently accepts the final archive snapshot: 57 final incremental checks pass, including the metadata correction, all ZIP members, unchanged PostgreSQL/old failure bytes, issue/merge accounting, and exact original TODO history. It is a separate sidecar so that its archive hash does not create a self-reference.

[catalog-ci-cutoff.json](catalog-ci-cutoff.json) freezes the 2026-10-09 09:08:59 UTC observation: PR #240 is still draft, and the original Company backend and Catalog complete source-version jobs are queued with no executed steps. No queued or optional skipped job is called a successful gate. The maintenance PR remains pending that original CI acceptance and is not included in the ten implementation completions.
