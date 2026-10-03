# Excluded descriptor metadata checkpoint

Base: `64cd11816cfd94c9dc80e65578edb06b92725e84`.
Independent counterexample: `30c25c57402a92f7126cb25ad77813fec811e470`.

`oracle.py` is byte-for-byte the independent oracle at that counterexample.
`baseline-fail.raw.log` and `historical-fail.raw.log` preserve its original FAIL
logs unchanged; no oracle expectation was weakened. The new run passes 9 tests,
including all 8 original regular-to-symlink API/CLI capture/validate subcases.

`descriptor.raw.log`: 12 tests pass, including the original 9, 96 additional
bounded API/CLI capture/validate cases (two forks, regular -> symlink/directory/
FIFO/different regular inode, first and second stat; unknown/unreadable metadata;
unsupported platform and missing O_PATH), metadata-handle/no-read guards, and
real CLI legal-artifact and entry/depth overflow checks.
`source-suites.raw.log`: 38 existing source inventory tests pass.
`legacy-compatible.raw.log`: 4 existing excluded metadata tests pass. The old
positive test invoking chmod(0) was intentionally not run because this task
forbids permission changes; its name-only os.open guard also predates the
explicitly authorized metadata-only handles. It remains unmodified. New legal
artifact tests cover the no-body-read invariant without permission changes.

## Observations and limits

Linux O_PATH|O_NOFOLLOW acquisition binds type/device/inode without regular
file, FIFO or device I/O. An initial handle stays alive until inspection and the
closing stat plus closing metadata-only acquisition finish, preventing inode
reuse of the held object. Directory enumeration uses a separately identity-
matched O_RDONLY|O_DIRECTORY|O_NOFOLLOW FD. Every FD closes on error.

The closing acquisition rejects the original os.stat hook that replaces the
entry after returning its second regular result. This is a bounded identity
observation contract, not an atomic snapshot: replacement after the final
acquisition, transient changes undone between observations, and concurrent
namespace changes elsewhere are not proven absent. Ancestors are traversed
through no-follow directory FDs, as before. Unsupported platforms/primitives
fail closed; no ordinary-file-open fallback exists. Artifact names and bodies
remain absent from SOURCE. The 12 exclusions and per-fork/per-pass 4096 entry /
32 depth budgets are unchanged. No selected-binding/capture policy edits, Go,
PG, mailbox, SMTP, external cache access or permission changes are performed.

Reproduce from repo root:

```sh
python3 -B scripts/tests/test_r5_excluded_descriptor_metadata.py -v
R5_REVIEW_INVENTORY="$PWD/scripts/r5_source_inventory.py" python3 -B scripts/tests/checkpoints/r5_excluded_descriptor_20261003/oracle.py -v
python3 -B -m unittest discover -s scripts/tests -p 'test_r5_*source_inventory.py' -v
```
