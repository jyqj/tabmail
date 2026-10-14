# Frontend evidence after independent gate failures — 2026-10-07

## Implementation item

This is a bounded implementation item under **R5-P11-080**. Its source baseline
is `4065c4909c8f21a401a9a1af6370fa3f72670b99`; it does not accept the parent task.
Parent progress remains **10/171 complete, 161 remaining**.

The latest baseline CI run, [37624990816](https://github.com/jyqj/tabmail/actions/runs/37624990816),
failed the frontend zero-vulnerability audit before TypeScript, Vitest, lint,
or the application build could execute. Those later checks therefore provided
no result for that checkout. This was an evidence gap even though the audit
correctly kept the frontend job red.

## Resulting workflow behavior

After `npm ci` succeeds, each frontend validation step has an explicit
non-cancellation condition tied to that installation outcome. An unrelated
source, audit, type, test, or lint failure no longer prevents the other
executable validations from recording their own result. A failed or skipped
installation prevents dependent commands from running; cancellation still
stops further validation work.

The existing `frontend` job, job timeout, branch events, read-only permissions,
toolchain versions, complete audit predicate, and complete default Vitest test
selection are retained. There is no `continue-on-error`, ignored vulnerability,
test exclusion, empty-suite allowance, or increased backend time budget.
Any unsuccessful validation step continues to fail the job.

The job records the actual checked-out Git SHA and uploads it with the source
version report and full Vitest JSON report. Vitest keeps its normal terminal
reporter as well. The evidence upload runs after failure, and an entirely
missing evidence set fails that upload explicitly. A report for a command
that never started is not fabricated.

## Local verification

The same frozen regression was run against the exact baseline workflow bytes
and the candidate workflow. The baseline reports two failures: sibling
failure prevents independent checks, and there is no retained full frontend
test report bound to the checked-out source. Its existing strict-failure
control passes. The candidate passes all three assertions. The five unchanged
CI event, permission, toolchain, timeout, and archive-boundary tests also pass.

| Check | Result |
| --- | --- |
| Frozen regression against exact baseline workflow | 1 pass / 2 fail / 0 error / 0 skip |
| Same frozen regression against candidate | 3 pass / 0 fail / 0 error / 0 skip |
| Existing `test_r5_ci_wiring.py` | 5 pass / 0 fail / 0 skip |
| `git diff --check` | pass |

Local checks validate the workflow contract; actual GitHub step execution must
be read from the candidate PR run before claiming runtime continuation. Its
terminal result and source identity will be recorded in the batch execution
log. The existing private-fixture Vitest failure and dependency findings remain
real blockers if reproduced; this change does not repair either of them.

Reproduction:

```sh
python3 -B -m unittest discover -s scripts/tests -p test_r5_frontend_evidence_continuation.py -v
python3 -B -m unittest discover -s scripts/tests -p test_r5_ci_wiring.py -v
git diff --check
```

Frozen identities and raw local results are retained in
[`evidence/R5-FRONTEND-EVIDENCE-CONTINUATION-20261007`](evidence/R5-FRONTEND-EVIDENCE-CONTINUATION-20261007).
