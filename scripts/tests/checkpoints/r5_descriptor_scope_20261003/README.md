# Descriptor scope evidence — 2026-10-03

Source base: `aeed47f0e1998d9925acb680992b051463821d69` (PR23).
Only the two assigned test files change, with this separate evidence directory.
`.agents/skills` is absent in this checkout; `/workspace/.agents` is empty.

## Original failures and causal limits

The supplied batch4 docs-only head `e5e23e21abfe4c7e9f082405e56a95a7f346fa9a`
provides the prepared 640-test report and raw log. `original-context.json` records
its source SHA, exact nine scoped failure IDs and hashes; `prepared-original-scoped.log`
preserves their tracebacks. These are prepared local integration observations,
not a newly downloaded CI job log. CI run37151630259/frontend111286597343 is the
user-reported failing context. Other integration failures stay outside this task.

The eight regular/phase1 failure events report exit 0 instead of 1 and
`METADATA_OBSERVATION regular 1 4`. The original hook did execute unlink/touch,
but it did not record old/new identities in those invocations. The separate
batch4 eight-observation inode reuse probe cannot establish each failure's cause.

Here the unchanged baseline ran 17 tests with only the open-guard failure
(`baseline-local.log`). `unanchored_probe.py` runs the original phase1 hook with
metadata observations added, across both forks, API/CLI and capture/validate.
`unanchored-local.jsonl` records eight distinct old/new dev/inode identities,
and eight exit-1 rejections. Thus the attack happened locally, with no observed
inode recycling, and the original eight failures were not reproduced here.

The corrected hook holds an O_PATH reference to the old object before unlink,
verifies the anchor's identity, creates the replacement, and requires different
dev/inode. It closes this fixture-owned reference immediately after mutation,
before returning the original stat to the implementation. It does not extend the
implementation's descriptor lifetime or suppress the scheduled race. The final
log records all eight phase1 regular replacements and rejections. All 96 bounded
fault/phase/fork/API/CLI/capture/validate cases retain rejection assertions;
inherited symlink/final-stat/FIFO/socket/module/availability/budget negatives remain.

The second original failure rejects `.DS_Store` at `os.open` even though line143
uses `O_PATH|O_NOFOLLOW`, which acquires metadata without a body-readable handle.
The new guard permits only those exact flags for artifact files. It checks full
paths, dir_fd-relative paths, inode identities and live FDs; it intercepts Path,
builtins/io, raw read/pread/readv/preadv/readinto, fdopen, mmap, copy_file_range,
splice and sendfile where available, including `/proc/self/fd` readable reopening.
Counterexamples exercise readable flags (including O_RDONLY=0), missing NOFOLLOW,
extra access/write/create flags, readable and O_PATH FDs, and each available body
API. Directory aliases still fail both public capture and metadata traversal.

## Validation and independent negatives

`related-final.log`: 81 tests, zero failures/errors, across the complete eight
modules below, including all 19 tests in the two changed modules. The runner unit
test intentionally produces an inner expectedFailure/skip/failure report to check
that those are rejected; the outer 81-test result is OK with no expected failures.

```sh
PYTHONPATH=scripts/tests:scripts python -B -m unittest -v \
  test_r5_excluded_descriptor_metadata test_r5_source_excluded_metadata \
  test_r5_smtp_owner_source_inventory test_r5_current_source_inventory \
  test_r5_protocol_source_inventory test_r5_source_v3_boundary_negatives \
  test_r5_archive_boundary test_r5_source_version_runner
PYTHONPATH=scripts/tests:scripts python -B \
  scripts/tests/checkpoints/r5_descriptor_scope_20261003/unanchored_probe.py
```

Two disposable helper mutants were tested via `R5_REVIEW_INVENTORY`, never written
into the repository implementation:

- Change `return (stat.S_IFMT(info.st_mode), info.st_dev, info.st_ino)` to
  `return (stat.S_IFMT(info.st_mode), 0, 0)` in a temporary helper. Running the
  full bounded-replacements method rejects this mutant with 16 regular failures
  (both phases, both forks, both actions, both entries): `identity-mutant.log`.
- Change `metadata_flags = os.O_PATH | os.O_NOFOLLOW` to
  `metadata_flags = os.O_RDONLY | os.O_NOFOLLOW`. Running
  `test_only_metadata_handles_for_artifacts_and_no_body_reads` rejects it with
  one flags assertion: `readable-mutant.log`.

## Compatibility and limits

No production implementation change was warranted by these observations. v2's
historical single-fork/pruned inventory domain remains historical; the v3
metadata guard is tested under its existing exact dual-fork policy, without
rewriting historical receipts, source identities, or broadening its qualification.
v4 retains its existing descriptor-bound archive boundary; its full adversarial
boundary suite passes. No new policy, policy auto-approval, or atomic snapshot
claim is introduced. Type/dev/inode observations cannot prove absence of changes
between observations, including a recycled inode before any handle is held.
Artifact contents remain outside SOURCE. Existing byte/directory budgets, default
4MiB controls, benchmark/transaction code, central TODOs and lock files are unchanged.
This scoped result does not make the original full integration or CI green.

## Remote blocker

`gh auth status` reported the active GitHub token invalid. Remote writes stopped;
no alternate connector/identity/route was attempted. Local commit is authorized;
push and draft PR require restored authorized credentials. No merge or deploy.
