# Independent lifecycle delta review — 2026-10-04

**F1/F2/F3 are corrected at tested source
`52dd8b9ef538b55812b35308cae8b469400e2705`. Overall verdict: request changes for
one residual medium preflight error-cleanup finding, G1. No false qualification
was observed.** Delivery/evidence head read in full:
`a97e91350b437a18472655ca76177622c1410603` (author's Draft PR26).
The delivery helper is byte-identical to the tested helper, SHA256
`bc49bc0fc39b728edea9799f713c6fb6db95b381141f5068a3120390be9c9f28`.
This delta adds only review-owned tests/report/evidence and preserves the
[original review](R5-EXTERNAL-BATCH-INDEPENDENT-REVIEW-20261004.md), failed controls
and all historical receipt identities.

## Findings before paperwork

### G1 — Medium: repeated preflight drain failure bypasses the outer cleanup barrier

`scripts/preparation/r5_external_batch.py:442–450` starts an owned inventory worker
and retries `finish()` in its cleanup `finally`. If both calls raise, the worker
has received SIGKILL but has not been reaped. Preflight calls this at line 502,
before the execution `try/finally` whose adopted-tail barrier is at line 639.
The exception unwinds the lease context at line 496. `run()` propagates the
exception because publication has not occurred, and clears `leased`.

Two independent controls reproduce this: a small synthetic supervisor and a
real supervisor using the freshly pinned complete contract and actual bound
validation-helper argv. Both inject the same persistent pipe-drain `OSError`
into initial `finish()` and its cleanup retry. On return, the token is absent,
`published` and `leased` are false, `owned.joined` is false, and no receipt exists.
The review then successfully reaps the waitable child itself. This demonstrates
lease release before required physical reaping, not merely a stale boolean.

A single injected drain error is handled: the cleanup retry physically joins
the worker before propagating the original exception. Terminal validation also
has a later descendant barrier in the code. G1 is specifically the preflight
double-failure path. The OS failure is injected; no spontaneous host I/O failure,
live surviving child, product false-pass, or uninterruptible kernel wait is
claimed. All controlled processes were killed/reaped by the review.

Required correction: put the preflight worker inside a lease-held outer resource
cleanup barrier that survives a drain failure, so an independent kill/wait path
reaps it before the lease is released. Continue withholding qualification when
cleanup cannot be established. Reusing the same failing drain routine as the only
cleanup retry does not establish physical join.

## Independent verification of the original findings

| Finding | Result at the fixed source | Evidence boundary |
| --- | --- | --- |
| F1: detached child retains inherited pipes | **PASS:** killed/reaped while draining, tail retained; 0.109s control | Real isolated supervisor/direct child/detached child; final tail barrier returns true then false; registered-root set clears |
| F2: near-deadline idle RPC | **PASS:** shutdown joins in 0.105s, every staged result rejected | Same owned partial-line socket counterexample; no extra handler/server threads or socket remain |
| F2: slow full inventory | **PASS:** preflight/postflight workers terminate and reap on short deadline or user cancel | Four scenarios using real descriptor walks with injected per-file delay; actual inventory-worker dispatch/finish path |
| F3: accepted late case cancellation | **PASS:** real socket accepts cancel before exact ack, rejects all eight staged results | Full real pinned pre/post validation; Go/Python result observations are synthetic and credit no business case |

The controlled durations are observations under short injected deadlines, not
new production budgets or proof of a universal return bound. The fix correctly
distinguishes **execution expiry** from a mandatory **cleanup tail**. Killing an
OS-uninterruptible process can require waiting beyond expiry; no hard physical
return guarantee is approved. The dedicated Linux CLI owns every subprocess
discovered by its sweeper. Registered direct roots are protected from concurrent
sweeps; this ownership assumption does not extend to arbitrary embedded callers
with unrelated subprocesses or hostile concurrent mutation.

## Fresh fixed-source preparation and probe

A new owned toolroot contains a fresh clean real Git checkout at the exact tested
SHA and a fresh normal lifecycle-enabled `npm ci` from the unchanged official
lock. Fresh archive/default/race selected receipts and the original runtime-v2
manifest are captured before the independently pinned batch contract. Full source
and dependency validation remains content/descriptor based; no inventory cache,
sampling, sourceguard relaxation or alternate policy is introduced.

The independent infrastructure-only probe passes **10/10, peak4, 27.518 seconds**,
with exact physical fixture acknowledgement and successful complete equal pre/post
inventories. Source/dependency inventories contain 1,933/45,584 regular files.
Zero owned fixture databases or backend connections remain afterward, and the
separate review-owned loopback PostgreSQL cluster is shut down. The author's
21.437-second probe is separate evidence with its own exact pins and receipt.
Neither timing qualifies or predicts formal component execution.

The author's updated controls independently rerun **40 batch / 21 v2 / 70 protocol**.
The review adds five lifecycle methods (including four slow-inventory subscenarios),
two actual pinned-contract preflight error observations, and **22 real-contract
semantic assertions**. Full project `tsc --noEmit --incremental false` passes.
The Go bridge/consumer, Go version, go.mod/go.sum, two replaces and package-lock
are unchanged from the prior review; the new infrastructure Go probe still uses
race/count1/timeout120. No extra formal Go owner/component suite is credited here.

## Eligibility, scope and delivery boundary

The policy stays `r5_external_batch_validation_v1`; exact semantic recapture,
independent byte pin, helper/Python/tool/config identity, fixture/catalog/input
binding, nonce and live lease are preserved. Accepted case cancellation now
permanently invalidates the whole batch, and cancellation after sealing explicitly
rejects. Exact owner ack and original passing terminal classification remain
mandatory. Source/dependency mutation still rejects all staged children; restored
bytes undergo fresh full validation. Routine execution shutdown uses `cancelled`,
while user abort uses the separate `abort` event, allowing the required terminal
inventory check to run after normal shutdown.

No qualified result was observed before required cleanup/ack. In G1 no receipt
is published, so its qualification failure is fail-closed even though lease-held
reaping is incomplete. This distinction is why G1 does not relabel the normal
probe as failed or claim a product false-pass. The approved PE05 pgx/backend-tail
limitation remains: goroutine completion is not new server-side cancellation proof.

Original bounds remain Go120/batch180/case75/max4. Scope remains 40 Go + 3 Python
required terminals, 17 handler IDs/26 variants, catalog46; this probe is only 10
infrastructure terminals. Formal43/26/46, wholePG180, audit and actual email are
NOTRUN. Central10/171, default675/sharedDB89e7 exclusions, old v1/v2 failures and
all remaining unknown layers remain unchanged. No merge, deploy or gate closure.

See [safe delta evidence](evidence/R5-EXTERNAL-BATCH-INDEPENDENT-DELTA-20261004.json)
and the three new scripts in `scripts/reviews/`. Raw fixtures, manifests, stdout,
response bodies and private logs remain under `/workspace/r5-independent-delta/private`.
Normal commit/push deliver this review on its existing branch. The previously
denied draft API is not retried. This documentation commit has its own identity
and inherits no fixed-source receipt qualification.
