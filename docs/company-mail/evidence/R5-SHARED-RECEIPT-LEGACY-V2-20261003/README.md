# R5 shared receipt / legacy adapter-fixture revision 2

Authorized base `ee3308fd3217246c9bdd43b07ae0609ebae6aeb6`; checkout `ac5db2ee72b97b027a14d6e885d3870276bfaf82` adds only the retained prior current-wire evidence. Tested source commit `6138113c1896cb810db567605154b5023f0340de`. Revision 2 and historical mapping are explicit in R5-PROTOCOL.md appendix AB. Original case JSON, markers, adapter paths, failed current-wire run, locks, Go1.25.7 and two local replacements are unchanged. No production files changed.

The independent unmodified baseline reproduced RC02's three non-target subject-oracle failures and four BC03 capability/observation fixture failures. Revision 1's current CreateOutboundJob already captured a migration-17 COMPLETE snapshot; source loss did not make that asset legacy-unknown. The new original-path BC03 fixture models a historical persisted asset with no trustworthy source and asserts version0/NULL/unknown before HTTP. Actual schema16-to17 upgrade is separately rerun. RC02 now closes the DTO, checks precise public metadata and original draft/mailbox/user identity, and forbids subject/body/BCC/headers; same-key replay remains same ID and one DB job. New durable snapshot and single-axis tenant/zone/mailbox identity tests call real HTTP and bounded backfill, retaining exact immutable asset bytes.

| Fresh runtime | Result |
| --- | --- |
| RC02 list/detail/replay targeted | PASS |
| BC02/BC03 observation targeted | PASS |
| BC02/BC03 current capability targeted | PASS |
| New durable snapshot (2), identity (3), original genuine schema16 upgrade | PASS |
| Original shared-db producer, one fresh runtime | shared_scoped_evidence_passed, exit0; 46 verified cases; errors=[], target_red={} |

All Go runs use -race, -count=1, -timeout=120s; targeted process ceilings 180s. The formal producer keeps its own original process180/race120 command. No whole-PG, component, selected runner, live DNS, real mail, SMTP worker, merge or deployment was run. Formal product_green=true is scoped to this shared-db run; task_complete=false and all missing component layers remain in shared-db-status.json. This does not certify components or a complete cross-producer wire join. BC02's original capability scope is preserved and is not relabeled a formal historical backfill run; the added actual write tests remain supplementary.

One private PG17, loopback 127.0.0.1:55434, owned disposable databases and synthetic fixtures only. Private raw Go JSONL, actual producer report/output, and intermediate failures remain mode700 under /workspace/r5-receipt-private, with hashes pinned in exported status metadata. No response bodies, BCC values, header values, tokens or raw private logs are published. Original failed-run artifacts are never rewritten or promoted.

Source capture uses unchanged archive-v4 and exact protocol context [['r5protocol']], in a clean independent worktree /workspace/r5-receipt-source. Fresh manifest SHA256 829fbc76f259c7fbbbab63964e047d14854dd620a4a6d835ac7e1f0543fdae45, source SHA 28919f2a356f752e0ec78cca16aac7f3463ca49b. Independent post-runtime capture is byte-identical. Initial preparation passed an invalid context array instead of the required object; its formal rejection is retained as preparation-invalid-context.json, before any runtime. No capture policy/runner bypass or source-policy edits were used.

The two intermediate targeted failures are retained by hash and terminal paths: first had a fixture SQL parameter-type error and an incorrect expected list retry hint; second fixed SQL but still expected the detail retry hint on list. Final expectations follow the actual declared receipt contract: conservative list unknown, detail/replay state_not_retryable. They are not counted as passes. Final targeted runs and the formal producer started only after those corrections.

Delivery: evidence commit a71d51cb4b96d12fc7f1d2c33e3e10fe741bbcec pushed and its exact remote SHA confirmed. The single authorized draft PR attempt against codex/r5-current-wire-20261003 returned `Post "https://api.github.com/graphql": Forbidden`; exit1, no PR created, no alternate API or permission bypass attempted. Exact safe denial output and delivery.json retained. Owned PostgreSQL stopped after collection.
