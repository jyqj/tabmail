# Independent external batch review — 2026-10-04

**Verdict: request changes on the lifecycle contract at fixed source
`f80b37fe14d856d2f2a048a06fe848fc4420448d`. The normal infrastructure probe passes;
the claimed bounded physical cleanup does not pass independent fault controls.**
Documentation/evidence read in full at
`f5e54f101964822da6cea6f9e958fe89d1688a39`. These are separate identities.
This review adds only its own tests, report and safe evidence. It does not change
implementation, authorize a formal component run, or close product gates.

## Findings before delivery paperwork

### F1 — High: detached descendants retaining pipes defeat the deadline

`scripts/preparation/r5_external_batch.py:151` repeatedly calls
`communicate(timeout=0.05)` until EOF. After cancellation/deadline it kills only
the initial process group. An owned descendant can start a new session while
keeping stdout/stderr open; killing the original group cannot close those pipes.
`join_adopted_tails()` at line 201 is reached only after `finish()` and the RPC
owners return. Subreaper adoption therefore does not resolve this deadlock.

The independent control launches a real owned supervisor, direct child and
detached child with inherited pipes. With a 0.2-second `finish` deadline, the
supervisor is still blocked after another 0.6 seconds. Only the review watchdog's
explicit kill of its recorded owned descendant releases it. All test processes
are then reaped. This is a direct test of the same deadline loop, with a short
control deadline; it is not an altered production budget or a timed 180-second
component run. An indefinitely retained pipe has no eventual return bound here.
Batch180 can neither physically finish nor release its lease through this path.

Required correction: descendant termination/reaping must participate while pipe
draining is pending, and cleanup failure must reject without claiming completion.
The current closed-pipe detached-tail control does not cover inherited pipes.

### F2 — Medium: rejection after expiry is not a bounded physical return

At lines 423 and 524, `validate_contract()` performs synchronous full inventories
without cancellation/deadline participation. At lines 430 and 508–510, accepted
RPC handlers have a fresh 80-second socket timeout and `server_close()` joins
them without reference to the batch's remaining lifetime. A partial request
accepted near expiry can add almost 80 seconds beyond the batch deadline.

The real-socket control leaves an owned incomplete request open and injects a
near-expired batch deadline. The handler join takes **79.988 seconds** after that
deadline; the receipt eventually rejects. A separate deterministic slow-inventory
control confirms preflight can complete after expiry. No real filesystem stall
lasting 180 seconds was induced. These controls and the code establish missing
deadline participation, not a measured formal-run overrun.

Required correction: account for remaining time during inventories and actively
close/join pending RPC reads during cancellation/shutdown. Do not describe an
eventual rejected receipt as proof of a bounded 180-second physical return.

### F3 — Medium: accepted per-case late cancellation does not invalidate results

At lines 287–295, `cancel` stores `cancelled_keys` and kills an active child.
It does not invalidate an already stored successful result or set batch failure.
The acknowledgement and final eligibility checks do not consult this set.

An independent control uses the actual bound contract, real private RPC server,
full pre/post validation, and synthetic successful Go/Python observations. After
all eight child results exist but before owner acknowledgement, the private socket
accepts a per-case cancel and then accepts the exact owner ack. The resulting
receipt remains `BATCH_QUALIFIED`, with that child's qualification true.
This is a supervisor-contract counterexample, not a passing business case.

The current Go bridge separately joins its cancel goroutine and checks `ctx.Err()`
before returning success. Its ordinary context-cancellation path therefore has
an additional failure check; this review has **not** reproduced a false passing
business case through that bridge. Nevertheless, an acknowledged cancellation
must either invalidate eligibility or explicitly reject an already-finalized
request. Silently accepting it while retaining qualification is inconsistent
with the proposed cancellation guarantee.

## Checks that pass and important limits

* Fixed-source author's controls reproduced: batch **30/30**, runtime-v2 **21/21**,
  protocol regressions **70/70**. These remain author's controls independently
  rerun; they are not counted as newly authored review controls.
* Six review lifecycle controls reproduce the three findings and distinguish
  local helper exception safety from the outer batch's actual cleanup. A simulated
  `finish()` I/O error leaves `run_process()` locally unjoined, but the real outer
  batch's final barrier kills/reaps the direct child, rejects, and releases the
  token. Thus this is **not** an additional demonstrated end-to-end leak.
* Twenty-two assertions against a real captured contract cover byte pin, semantic
  workers/budgets/required-set recapture, out-of-lease capability, nonce, contract
  identity and argv rejection, actual source/dependency mutation rejection of all
  eight staged children, restoration validation, and F3 before owner ack.
  Mutation groups use synthetic execution and credit no catalog case.
* Full project `tsc --noEmit --incremental false` passes without skips, against the
  unchanged official-lock external toolroot installed by normal lifecycle-enabled
  `npm ci`. No audit command ran. Preparation rejects inherited npm overrides and
  a user-supplied `GOTOOLCHAIN` override; clean runtime sets its own pinned value.
* Official installed Go reports exactly `go version go1.25.7 linux/amd64`.
  Focused race/count1/timeout120 bridge and four owner tests pass, including two
  independent loopback PG/HTTP fixtures. Both existing replaces and package-lock
  bytes are unchanged. No wholePG180 or formal component suite ran.
* The normal fresh probe uses only owned synthetic fixtures and a separate
  trust-authenticated loopback PostgreSQL cluster, without host secrets or email.
  Safe evidence records the final exact contract/receipt/log hashes, timing,
  10/10 infrastructure terminals, peak4, full equal inventories and cleanup ack.
  The final pristine-source probe takes **22.173 seconds**; its source/dependency
  inventories contain 1,924/45,584 regular files. Zero owned fixture databases or
  backend connections remain afterward, and the review-owned PG cluster shuts down.
  A normal pass provides no qualification for the counterexample paths above.

## Contract, scope and cleanup analysis

The schema alone does not authorize launch. `validate_contract` recaptures the
catalog-derived terminal sets, fixed commands/config/tools/helpers, original
runtime manifest and complete content/descriptor inventories. Independently
pinned contract bytes, fixed-source helper/Python identity, private fixture
content/catalog/input/descriptor binding, nonce and live lease remain enforced.
No executable/argv enters through RPC. Cooperating tokens serialize and do not
steal stale ownership. Lease-finalization failure and late **batch-level**
cancellation reject every staged qualification; instance reuse cannot rewrite
historical receipts. Source/dependency drift fails closed rather than retaining
a prior child's eligibility.

The component set remains **40 Go paths + 3 Python assertions = 43** required
terminals, with **17 handler IDs / 26 variants**. The catalog still has 46 IDs.
The new handler parent and auxiliary owners group map to the original catalog
paths before classification; raw JSONL remains private and hashed separately.
Missing/duplicate/skipped/failing terminals and infrastructure errors reject.
The independently run probe is **10 infrastructure terminals only**; it credits
none of those business cases. Formal26/46 execution is still NOTRUN.

Normal Go fixture cleanup is synchronous and LIFO: HTTP/server and Redis cleanup,
store/observer pool closure, owned database DROP, then completed-owner ack and
slot release. Cleanup failures mark Go tests failed and cannot qualify merely
because an ack arrived. Hard Go timeout with no physical fixture ack rejects and
withholds terminal qualification; it does not prove PostgreSQL cleanup happened.
The PE05 coordinator/revoker joins and NOWAIT checks remain useful finite probes.
The approved pgx cancellation/backend-tail limitation is retained: goroutine Done
is not a new guarantee of server-side query cancellation or released backend locks.

On Linux/POSIX, `communicate` drains pipes through selectors; the relevant joined
threads here are the non-daemon RPC handlers and server thread. The final subreaper
barrier treats **every direct/adopted child of this supervisor** as owned. It has
no independent process provenance beyond PPID and relies on the dedicated,
sole-executor CLI process. It must not be generalized to an embedded caller that
also owns unrelated subprocesses. `/proc` discovery/kill/wait failures propagate
before qualification; uninterruptible process waits still lack a return bound.
No hostile concurrent mutation, OS immutability, exhaustive dynamic/native/loader
coverage, or stronger cleanup guarantee is inferred.

## Evidence and delivery boundary

See [safe evidence](evidence/R5-EXTERNAL-BATCH-INDEPENDENT-20261004.json) and the
review-owned scripts in `scripts/reviews/`. Private contracts/manifests, fixture
values, response bodies and full logs remain under `/workspace/r5-independent/private`.
Preparatory rejections and intermediate probe identities are retained separately.
The final probe is freshly captured after restoration and frozen source binding;
this review's documentation commit has a newer identity and inherits no receipt.

Legacy v1/v2 failures retain their identities. `default675`, `sharedDB89e7`,
central10/171, wholePG180, audit8high and remaining unknown layers stay open.
No merge or deploy occurred. Normal commit/push and the single authorized draft
attempt are delivery actions only; their outcome is reported separately.
