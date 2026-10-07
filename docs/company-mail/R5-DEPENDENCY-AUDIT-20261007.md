# R5 dependency update and complete-lock admission

2026-10-07. This is bounded progress on **R5-P11-070**. The complete dependency
audit still fails; the parent task and the R5 10/171 completion count remain open.

## What changed

The existing locked dependency graph reported 12 affected package entries,
including one critical entry. A normal, targeted npm update kept every direct
requirement and changed only the following transitive packages and sharp's
platform/libvips packages:

| Package | Before | After |
| --- | --- | --- |
| `proxy-addr` | 2.0.7 | 2.0.8 |
| `@modelcontextprotocol/sdk` | 1.27.1 | 1.32.1 |
| `sharp` | 0.35.4 | 0.35.5 |
| `source-map-js` | 1.2.1 | 1.2.2 |

There are exactly 30 changed lock entries and no added or removed entries.
Independent review checked all 44 dependency edges pointing to changed nodes;
the selected versions satisfy their existing ranges. `web/package.json`, the
root lock entry, Next.js/React direct versions, and the complete TypeScript
5.9.3 lock entry are unchanged. Updated tarballs retain registry.npmjs.org
locations and SHA-512 integrity values.

The command was:

```sh
cd web
npm update --package-lock-only --ignore-scripts --no-audit --no-fund \
  proxy-addr @modelcontextprotocol/sdk sharp source-map-js
```

The [before audit](evidence/R5-DEPENDENCY-AUDIT-20261007/audit-before.json)
and [after audit](evidence/R5-DEPENDENCY-AUDIT-20261007/audit-after.json)
record the actual reports from this run:

| Audit | High | Critical | Total |
| --- | ---: | ---: | ---: |
| Before | 11 | 1 | 12 |
| After | 8 | 0 | 8 |

The remaining eight affected entries belong to the braces/micromatch/fast-glob
dependency chain, including eslint-config-next and shadcn tooling. At the time
of this run, the registry's latest braces version was still 3.0.3 and was within
the reported affected range. The report's suggested framework/tooling major
downgrades were not applied. The workflow still requires **total vulnerabilities
equal to zero**, so the audit remains a release blocker. No dev-dependency
filter, severity exemption, forced update or audit-result suppression was added.
The audit JSON includes the individual advisory URLs and affected dependency
paths; package-entry counts are not a count of independent vulnerabilities.

## Keep source preparation explicit

The source runner previously admitted only the original complete lock hash.
That rejected this legitimate security update before TypeScript preparation.
The same new regression on the original helper produced one failing subcase:
the new reviewed lock was refused. The old lock and unreviewed mutations were
already handled as expected.

`scripts/r5_source_runner_prepare.py` now admits a closed set of two reviewed
complete lock SHA-256 values:

- Original: `b839b59e9aa06133819adca60659e0f807ca1e321fbdc35fe55afe1c7b52eba3`
- This update: `b1c85223bc1171b16a5994c2f069ada51a5574719445142c31a213e9fda84e89`

The receipt records the actual lock digest that was read. Unknown lock bytes,
including appended whitespace and JSON reserialization, are still rejected
before archive processing or creation of node_modules. TypeScript's independent
5.9.3 URL, archive SHA-256, SHA-512 integrity, 132-member inventory, path/native
file restrictions and lifecycle checks remain unchanged. The four historical
actual-root tests still run their preserved source at `41b015c`; no historical
helper, catalog or old receipt was rewritten to appear current.

## Verification and review

Local environment: Go 1.25.7, Node 24.19.0 and Python 3.12.14. The CI Node 22
jobs remain separate evidence. [Verification metadata](evidence/R5-DEPENDENCY-AUDIT-20261007/verification.json)
includes the checked source hashes.

| Check | Result |
| --- | --- |
| `npm ci --no-audit --no-fund` from the updated lock | PASS |
| Complete TypeScript check, incremental mode disabled | PASS |
| Next.js production build | PASS |
| New complete-lock regression file | 3/3 PASS |
| Existing archive/lock and runner boundary classes | 10/10 PASS |
| Complete existing source-runner unit file | 14 PASS / 1 FAIL |
| Dependency audit | FAIL: eight high entries remain |
| Independent lock graph and admission review | No blocker in this update |

The source-runner failure is the unchanged executed-FD test: this workspace
reports `Linux pinned executable FD unavailable` before its expected midflight
replacement scenario. The same single test fails with the same cause on the
unmodified PR55 baseline. The failure and both logs are retained; it is not
counted as a pass, skipped, or fixed by loosening the execution boundary.

The new lock tests use the real previous lock bytes from Git history and the
current complete lock. Only archive-member production is mocked to isolate lock
admission; the digest and admission set are not mocked. The unchanged archive
boundary tests exercise the separate archive checks. This does not claim a new
full source-runner, real PostgreSQL, shipping browser or R5 release qualification.

Commands from the repository root:

```sh
python3 -B -m unittest discover -s scripts/tests -p test_r5_reviewed_web_locks.py -v
PYTHONPATH=scripts:scripts/tests python3 -B -m unittest \
  test_r5_source_version_runner.PreparationBoundaryTests \
  test_r5_source_version_runner.SourceVersionRunnerTests -v
npm exec --prefix web -- tsc --project web/tsconfig.json --noEmit --incremental false
npm run build --prefix web
npm audit --json --prefix web
```
