# Revision 4 remote CI evidence — run 37633385810

This packet records the completed **PR #73** run:
https://github.com/jyqj/tabmail/actions/runs/37633385810

The run ended **failure** at `2026-10-07T14:10:32Z`. It is not represented as a
green release or as a completed run of the later integration PR.

## Tested identity

- PR head: `6b6163dc8941aa36bb9fa2b75f40c101b8f662fe`.
- Actual GitHub PR merge checkout, independently recorded in frontend,
  backend and browser artifacts: `57ae4946ea60f0021c531aefc0c091dd984ca784`.
- Actual tree: `1eb98c6185e7b8b1e0be5967eb3fa741f84f0097`.
- Git API verified parents: `e32564304c0c84aa80b1184e72129fb0316d989d` and
  `6b6163dc8941aa36bb9fa2b75f40c101b8f662fe`.
- Integration checkpoint `9bcfa4f77f27ad4f118f32e92efef740ebac2c1e` has that
  same complete tree. Local catalog commit `7254f12e833051ce4a6bb6d4cb145db4795f3adc`
  also has that tree. Later documentation changes require their own explicit
  scope comparison; no future checkout is silently substituted here.

## Actual terminal results

| Job/check | Result | Evidence |
|---|---|---|
| Frontend job `112833169325` | FAILURE | Strict audit and complete Vitest fail; independent later steps still execute |
| Current source group | 713 requested / 713 executed PASS | Zero failures, errors, skips, expected failures or missing IDs |
| Frozen v1 source group | 4 requested / 4 executed PASS | Original `41b015c30c66b3ba58a3c1395e8559ebcd27a65f` checkout; zero failures/errors/skips |
| Source dispatch | 717 total, no missing/overlap | Requested IDs equal actual IDs; union equals discovered IDs |
| Catalog and TypeScript source validation step | SUCCESS | Actual job step metadata; includes original client `--check` |
| Strict dependency audit | FAILURE | 8 high, total 8; no lower-level or critical findings |
| TypeScript | SUCCESS | `npx tsc --noEmit` step completed |
| Complete Vitest | FAILURE | 887 assertions: 886 passed, 1 failed, 0 pending/todo |
| Lint | SUCCESS | Runs after audit/Vitest failures |
| Application build | SUCCESS | Runs after audit/Vitest failures |
| Backend job `112833169912` | FAILURE | PostgreSQL package timeout under original 180-second budget |
| Production-web job `112833170157` | SUCCESS | Production image/build step completed |
| Browser-journey job `112833169730` | SUCCESS | Mandatory real browser test: 1 passed, 0 failed/skipped |

The single Vitest failure is `r5-external-batch-probe.test.tsx`, assertion
`R5 batch independent realm loopback PostgreSQL`, with the exact failure
`explicit private fixture required`. This is retained as a failure; no exclusion
or empty-suite allowance was added.

The backend raw log contains the fresh line `panic: test timed out after 3m0s`
at `2026-10-07T14:08:09Z`, while
`TestR5EnqueueProtectsAuthorityThroughInsert/override-insert` was running.
The PostgreSQL package terminates at 180.061 seconds. The strict evidence
checker reports 7,975 passed / 105 skipped / 8,093 started events and 180 of 247
mandatory test IDs missing or not passed. Its zero individually reported
failed tests does not override the process/package timeout or checker failure.
The complete checker errors, including unexpected skips, remain in the raw
extracted checker JSON.

After that backend failure, its later explicit compatibility CLI, transaction
CLI, DTO, protocol, HTTP-contract and vet steps are **skipped**. They are not
reported as remote successes. The frontend source runner and catalog step did
complete, and revision 4's separate local three-CLI results remain documented
in its own reconciliation packet.

## Artifacts and extraction scope

`artifacts.json` records original artifact IDs, API URLs, archive sizes/digests,
and every extracted file's size and SHA-256. The four downloaded ZIP digests
were recomputed and matched GitHub metadata. Backend and browser checker
`log_sha256` values were also verified against the original JSONL bytes inside
their ZIPs. All three checkout text files agree with the actual merge SHA.

- Frontend artifact **11487712667**: raw source-runner JSON, raw complete Vitest
  JSON, checkout text, plus small independently derived summaries.
- Audit artifact **11486589368**: raw npm audit JSON.
- Backend artifact **11486559494**: raw checker JSON, checkout text and four
  timeout/failure excerpts. The 7,230,203-byte original backend JSONL remains
  inside the retained ZIP and is not separately extracted into this packet.
- Browser artifact **11486538884**: raw checker JSON and checkout text; original
  browser JSONL and Next log remain inside its retained ZIP.

The successful source-archive run **37633385737** and its 41,509,332-byte
`review-source` artifact **11487810721** are recorded by metadata/digest in
`summary.json`. That large source archive was not downloaded or added here.

`summary.json` also keeps the distinct integration checkpoint run
**37634509934**: it was observed in progress, with production-web successful
and the other three jobs still running. This is a status observation only;
PR #73's completed results are not relabeled as that run's terminal results.

No source, workflow, validator, timeout, skip policy or PR comment was changed
while collecting this packet. The failed job-log API returned `Transport
closed`; the backend findings above come from the successfully downloaded and
hash-verified artifact instead.
