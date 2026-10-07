# Three native-agent rounds — frozen implementation evidence

This directory retains the ten-item execution ledger and exact original logs
for the bounded implementation checks completed on 2026-10-07. The active task
state lives only in `../../R5-TODO.md`.

- `wave-batch-summary.json` maps all ten items to local commits, identical
  published trees, PRs and merge commits. The nine product changes contain 80
  fixed regression cases: baseline 18 pass / 62 fail, candidate 80 pass. The CI
  change adds three contract assertions plus an actual GitHub execution check.
- `wave-backend-summary.json` and `wave-compose-summary.json` record the
  authors' command scopes and source identities. SMTP commands and hashes are
  recorded in the three accompanying reports in `docs/company-mail`.
- `wave-ci/remote-37628534160/acceptance.json` records actual failed gates,
  subsequent executed checks, tested checkout, and verified artifact identity.
- `evidence-files.json` identifies every copied original evidence file by
  byte length and SHA-256. Logs are copied byte-for-byte; terminal ANSI codes,
  trailing spaces and final blank lines remain intact. These are evidence
  bytes, so whitespace diagnostics in the raw logs are not rewritten to make
  them look like hand-authored source files.

Package and focused test counts overlap and must not be added. The original
171 parent-task checkboxes are unchanged: 10 complete, 161 remaining. This
batch records ten completed implementation items and zero remaining in that
bounded plan; it does not assert PostgreSQL, browser, performance or release
qualification beyond each independently named actual execution.

The common product integration is
`e32564304c0c84aa80b1184e72129fb0316d989d`. The additional combined evidence is
now retained here:

- `wave-batch-independent-review.json` and `.md` record ACCEPT for the original
  fixed checkpoint `9232dee…`, its ten mappings, 31 original files, 80 product
  scenarios and unchanged parent checklist. That historical 31-file audit is
  not represented as an audit of later appended files.
- `wave-final-go/` retains the fresh four-package and eight-handler results,
  source manifest, vet, and original worktree VCS build failure. The separate
  `wave-final-go-build-clone/` supplement records the one successful ordinary
  clone build on the same commit, with default VCS stamping and no test rerun.
- `wave-combined-web-*` retains the complete default 886-pass/1-fail result,
  original JSON and logs, lint/build, exact web subtree, existing private
  fixture preflight failure, and comparison with the previous full execution.
- `wave-ci/remote-37631117018/` records the actual product CI checkout, PG
  timeout and mandatory-execution failure, verified artifact identities, and
  unchanged eight-high dependency audit. Its failure-event JSONL is explicitly
  a derived subset, not the complete original 7,256,944-byte JSONL.

The actual GitHub product checkout `8a6fe55…` has exactly the same full tree as
`e325643…`. Catalog revision4 has its own adjacent evidence directory and
strict historical provenance. See the [complete execution report](../../R5-MULTI-ROUND-20261007.md)
for scope, results, remaining gates and source relationships.

The final [revision4 remote packet](remote-ci-revision4-37633385810/README.md)
retains 19 files, including four original digest-verified ZIPs, complete source
runner and Vitest JSON, strict audit/checker results and exact tested checkout.
Its current 713 tests and historical four tests passed on their separately
recorded sources; the complete workflow still failed at its original gates.
`source-bindings.json` records root's actual Git tree and web-subtree comparisons.
