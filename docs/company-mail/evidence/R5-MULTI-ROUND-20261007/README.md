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
`e32564304c0c84aa80b1184e72129fb0316d989d`. Combined verification and catalog
reconciliation are recorded separately once their actual runs finish.
