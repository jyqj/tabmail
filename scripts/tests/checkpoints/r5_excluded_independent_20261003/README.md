# Independent excluded-fork metadata review: bounded REJECT

Target: `64cd11816cfd94c9dc80e65578edb06b92725e84`.
Old baseline: `8de4809516a39ad43b0e17afa288bcedab8b40a7`.
This evidence branch changes only this checkpoint directory. Production scripts,
central R5 TODO, dependencies and fork sources stay byte-for-byte at the target.
The independent fixture builder does not import or run the author's 48 tests.
The same rejection assertions run against both exact helpers, without converting
failures to expected failures. The intentionally red oracle is isolated here,
outside top-level unittest discovery.

## Finding requiring rejection under the requested race criterion

`scripts/r5_source_inventory.py:136-142` returns immediately after observing a
regular file's no-follow stat. Directory entries receive an O_NOFOLLOW open;
regular entries receive no subsequent type or identity check. The second full
inventory pass repeats that same regular-file branch, and excluded entries do
not enter `names`, `files`, or the closure payload.

The synthetic schedule replaces `.cache/switch` with a symlink to a synthetic
`cmd/main.go` after the second no-follow stat obtains its regular-file result,
before that result is returned to the helper. Both exact forks are tested.
Public capture and validate return successfully with the symlink still present.
A separate Python subprocess loads unchanged helper bytes and invokes its real
argparse `main`: both CLI capture and byte-pinned CLI validate exit **0** and
emit exactly the pristine receipt. Four public API and four real-main subcases
fail the independent rejection oracle. No symlink target is read by this test.

This is a deterministic scheduling injection into `os.stat`, not an observed
production exploit or a claim that a live filesystem can be atomically attested
by a finite number of path checks. The author's checkpoint already disclaims an
atomic hostile-filesystem snapshot. Nonetheless, the delegated criterion
explicitly requires regular-file-to-link race fail-closed; this source does not
establish that criterion. A repair or an explicitly approved weaker concurrent
filesystem contract requires a separate task. No repair is made here.

## Same-oracle results

| Source | Methods | Passed | Failed methods | Failing subcases | Skipped |
| --- | ---: | ---: | ---: | ---: | ---: |
| old `8de4809` | 9 | 1 | 6 | 48 | 2 (no CLI existed) |
| fixed `64cd118` | 9 | 7 | 2 | 8 | 0 |

`old.raw.log` and `fixed.raw.log` retain actual unittest failure traces. Both
oracle processes exit 1. The old race hook observes zero excluded no-follow
stats because old code never inspects those descendants; it therefore does not
demonstrate the same injected race schedule on old code. Its persistent nested
modules, links, special entries and over-budget trees also escape the identical
public-entry rejection assertions. New code closes those static omissions.

The seven passing fixed-source methods establish these bounded results:

- All 12 explicitly excluded directory names in both forks admit ordinary
  multilayer synthetic artifacts. Path read and open guards see no artifact
  body reads. Changing bytes, renaming artifacts and removing excluded trees
  preserves the entire receipt, original file list and closure hash. Changing
  an included non-Go source file changes closure and invalidates the receipt.
- Multilayer `go.mod`, file/directory/dangling symlinks, FIFO and Unix socket
  entries reject through public capture and validate for both exact forks.
- Each fork independently admits exactly 4096 entries across two excluded
  roots; one extra fails capture and validate. Exactly 32 directory levels,
  counting the excluded root, pass; level 33 fails both entries.
- Injected unavailable metadata and unknown stat type fail both public entries.
  Directory replacement with symlink before descriptor-relative open fails
  both entries; that open is verified to use O_NOFOLLOW.
- Unmodified real CLI capture/validate reject nested modules, dangling links,
  and FIFOs in both forks. Pristine receipt validation succeeds; wrong byte pin
  fails. Race failures are separately preserved, not hidden by the CLI passes.

Static inspection additionally confirms `os.walk(..., followlinks=False)`,
descriptor-relative ancestor descent using O_DIRECTORY/O_NOFOLLOW, streaming
scandir rather than unbounded materialization, and budget checks before entry
stat/descent. Unknown types fail rather than being included as source. The
metadata budget resets per exact fork per pass; excluded names/bytes are absent
from SOURCE while the policy's stated limits are included. The helper's actual
bytes remain self-bound; old and fixed receipt identities are not interchangeable.

## Reproduction

From the evidence worktree, with the project's existing Python 3.12.14 and stdlib:

```sh
git show 8de4809516a39ad43b0e17afa288bcedab8b40a7:scripts/r5_source_inventory.py > /tmp/r5-inventory-old.py
git show 64cd11816cfd94c9dc80e65578edb06b92725e84:scripts/r5_source_inventory.py > /tmp/r5-inventory-fixed.py
R5_REVIEW_INVENTORY=/tmp/r5-inventory-old.py python3 -B scripts/tests/checkpoints/r5_excluded_independent_20261003/oracle.py -v
R5_REVIEW_INVENTORY=/tmp/r5-inventory-fixed.py python3 -B scripts/tests/checkpoints/r5_excluded_independent_20261003/oracle.py -v
```

Only static source and self-created temporary synthetic fixtures are used.
No Go invocation, real PG, mailbox, SMTP, watchdog, external secret, runtime,
artifact body collection, cache admission, dependency or version-policy change.
The historical 21 Go-selected omissions belong to the parent's separate task;
this review neither fixes them nor asserts they are resolved. Method19/SML,
whole backend, product/runtime/performance and shipping remain unqualified.
No merge or deployment. Review stops at this bounded rejection.
