# Independent final G1 review — 2026-10-04

**PASS at fixed source `f875672dea2b68fdec9e0b986f4863724da5d7ba`: G1 is repaired;
F1/F2/F3 remain corrected. No remaining lifecycle blocker was observed in the
reviewed scope. It is reasonable to proceed to a separately authorized formal
component run under the same bounded contract. This review does not run or
authorize that producer, nor credit business gates.**

The G1 document and safe evidence were read in full at delivery head
`68ae9c566af48c378c9f1958ae3f2caeeb66936e`, on the existing author PR26. Its helper
is identical to the tested fixed source. The prior f80b37f failures and 52dd8b9
G1 failure remain documented with their exact identities in the original and
delta review; neither receipt is relabeled. This final delta adds only its own
review tests/report/evidence.

## Findings before delivery paperwork

The independent kill/wait/pipe-close path removes the failing drain routine from
cleanup. Every direct OwnedProcess is registered immediately after spawn. The
resource scope starts inside the lease before directory anchors, preflight and
channel setup, and covers execution and terminal validation as well. Its barrier
seals/joins RPC owners before independently reaping roots and descendants.
Successful physical cleanup precedes publication and lease release.

The same persistent drain failure that reproduced G1 was injected against the
**actual pinned validation helper and interpreter**. Only one drain invocation
occurred; cleanup did not retry `finish()` or `communicate()`. The independent
wait ran while the token remained present; the root was reaped and both actual
pipe objects closed while the lease still existed. After release, the original
exception object propagated, the process was not waitable, the registry was
empty, and no receipt existed. Initial and restoration full validation passed.

A persistent independent cleanup failure was then held behind a controlled
recovery gate. The supervisor recorded BLOCKED, retained the token and registered
root, and published no receipt. User cancellation and an injected expired
execution deadline did not unblock it or release ownership. After the controlled
fault was removed, physical reaping/pipe closure completed before release and the
original drain exception still propagated. This is an injected failure/recovery
control, not a spontaneous OS failure or an actual uninterruptible kernel wait.

Two additional actual-contract controls confirm source-preflight rejection and
server-thread-start failure clean resources inside the lease and preserve the
original error without a receipt. Two independently authored synthetic-observation
controls cover persistent execution and postcheck drain errors: roots are physically
reaped before the rejected receipt, resource acknowledgement is true, and every
staged child remains unqualified. Fixture acknowledgement is separate and is
never fabricated by process cleanup.

## Regression and normal-path evidence

* The original independent F1/F2/F3 regression methods pass: detached open-pipe
  tail termination/reaping (0.111s), near-deadline idle RPC join (0.159s), accepted
  late cancellation rejection, and four slowed inventory deadline/cancel scenarios.
  These are short controlled deadlines, not new production limits.
* Author controls independently rerun: **48 batch, 21 v2, 70 protocol**. Full
  project `tsc --noEmit --incremental false` passes with the fresh locked toolroot.
* The **22 actual-contract semantic assertions** retain byte pin, semantic
  workers/budgets/required-set recapture, nonce/live-lease/contract/argv rejection,
  full source/dependency drift rejection and restored validation. Accepted late
  cancellation before exact owner ack rejects all eight synthetic staged results
  after the full terminal validation. These controls credit no catalog case.
* A fresh fixed-source infrastructure probe passes **10/10, peak4, 27.506s**.
  Complete pre/post inventories match; exact fixture owner acknowledgement and
  `resources_joined` are true. The private cleanup record shows all **12 roots**
  directly reaped, groups joined and pipes closed, no pending roots or cleanup
  failures, and lease release after the barrier. Zero owned fixture databases or
  backend connections remain; the separate owned loopback PG cluster shuts down.

The preparation uses a fresh clean Git checkout at the exact fixed SHA, normal
lifecycle-enabled npm ci from the unchanged official lock, fresh archive/default/
race selected receipts, original runtime-v2 observation and independently pinned
batch bytes. Source/dependency inventories contain 1,937/45,584 regular files.
The author's 23.077s probe is separate evidence; neither timing predicts the
formal producer. Raw fixtures, manifests, response bodies and logs stay private.

## Formal-run readiness and limits

From this lifecycle review, a separately authorized formal run can proceed with
the **fixed implementation**, a freshly captured/validated **components-mode**
contract and fresh private outputs, within a dedicated owned sole-executor Linux
workspace using synthetic fixtures and loopback services. Retain Go120/batch180/
case75/max4 and exact full pre/post checks, nonce/lease/input bindings, owner ack
and terminal classification. Expiry, cleanup error, missing ack, drift or failed
terminal rejects the entire batch; do not extend budgets or inherit qualification
from this infrastructure receipt. A newer docs/evidence head needs its own
source binding and cannot borrow the fixed-source receipt.

Batch180 remains an **execution deadline**. Unknown mandatory cleanup retains
blocking lease ownership and withholds qualification, potentially indefinitely.
OS-uninterruptible waits have no approved hard physical-return bound. A forcibly
terminated supervisor can leave a stale token, which cooperating consumers must
not steal. The subreaper registry assumes the dedicated CLI owns all subprocesses;
it is not qualified for embedded callers owning unrelated children or hostile
concurrent source/dependency mutation. Dynamic/native/generated/loader coverage
remains unknown. The approved PE05 pgx/backend-tail limit is unchanged: goroutine
completion does not newly prove backend cancellation or released query locks.

Catalog46, handler17 IDs/26 variants and 40 Go + 3 Python required component
terminals remain unchanged. The authoritative catalog/markers/expectations,
Go1.25.7, go.mod/go.sum, both replaces, bridge/consumer and package-lock are
unchanged. Formal43/26/46, wholePG180, audit and actual email are NOTRUN. Old
failures, default675/sharedDB89e7 exclusions, central10/171 and all business gate
closure remain separate and open. No merge or deploy.

See [safe final evidence](evidence/R5-EXTERNAL-BATCH-INDEPENDENT-G1-FINAL-20261004.json).
The two new review scripts reside in `scripts/reviews/`; earlier scripts and
reports are unchanged. Private evidence remains in `/workspace/r5-independent-g1/private`.
Delivery is a normal commit/push on the existing review branch. The denied PR API
is not retried. This review commit has a new identity and inherits no runtime
qualification of its own.
