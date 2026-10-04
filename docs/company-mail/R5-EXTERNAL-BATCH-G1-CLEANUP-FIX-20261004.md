# G1 lease-held cleanup correction — 2026-10-04

The independent delta review at `c2a5e599eae0ae7b2599c0a8bd0ea8150ca15f2b`
confirms F1/F2/F3 at tested source `52dd8b9ef538b55812b35308cae8b469400e2705`
and identifies G1: persistent preflight pipe-drain errors bypassed physical
reaping while the lease token was released. No receipt or false qualification
was observed. Both independent G1 controls have been reproduced at that exact
previous source; their synthetic/actual-contract logs remain separately retained.
The previous probes, failed attempts, source identities and receipt hashes are
unchanged. This correction does not relabel their qualification.

A resource scope now begins immediately inside the acquired lease, before
anchors, preflight or server setup. Every OwnedProcess is explicitly registered.
Its independent cleanup path kills the owned group, directly waits/reaps the
registered root and group descendants, and closes its pipes. It never invokes
`finish()` or `communicate()` and remains usable under persistent drain failures.
RPC acceptance is sealed, tracked connections shut down, and non-daemon server/
handler threads joined before independent process reaping. Setup failures before
thread start close the already-created channel safely. The same outer scope
covers initial preflight, execution, terminal validation and their exceptions.
The original exception propagates after physical cleanup, with private cleanup
facts recording primary error type, direct reaping, group join, pipe closure,
channel join, attempts and any cleanup errors. Exception text/traceback is not
replaced by a failing drain retry.

The barrier completes before receipt publication; an exceptional scope exit also
requires it to complete if cleanup has not already been established.
A completed barrier is recorded as `resources_joined=true`, separately from the
unchanged exact Go physical-fixture `owners_joined` acknowledgement. Successful
process cleanup cannot fabricate fixture acknowledgement. Accepted case cancel,
missing acknowledgement, failed validation/terminals and recovered cleanup errors
continue to reject all staged child eligibility. Tail observations persist even
across retrying a failed cleanup operation.

If independent kill/wait/channel cleanup cannot be established, execution remains
in a lease-held blocking cleanup state. It publishes no receipt, retains the
owned token and process registry, and records pending/blocked facts privately.
User cancellation and deadline expiry do not authorize unwinding that ownership.
After the controlled fault is removed, cleanup completes before release and the
original preflight error still propagates. There is no promised hard physical
return bound: an OS-uninterruptible wait can require an indefinite cleanup tail.
An externally terminated supervisor may leave stale ownership; cooperating
consumers must never steal it. The dedicated Linux CLI is the sole subprocess
owner; this is not a safe embedded-caller policy for unrelated subprocesses.

Focused controls cover single and persistent preflight drain failures, persistent
execution/postflight drain failures, channel-start and registered-root setup
failures, normal qualification only after joined resources, and persistent
independent wait failure retaining the token without a receipt through expiry/
cancel until an explicit controlled recovery. The actual pinned-helper checker
runs full initial/restoration validation and repeats both G1 phases plus the
blocked ownership control. The existing F1/F2/F3 controls remain required.
All OS failures are injected; these controls do not claim a spontaneous kernel
failure or measured uninterruptible wait.

Full checks, one pinned capability lease, staged all-or-none receipt, catalog/
markers/business expectations, dependencies/official lock, Go1.25.7 and both
replaces remain. Limits remain Go120/batch180/case75/max4. Formal43/26/46,
wholePG180, audit and actual email do not run. No merge/deploy, central10/171
closure, default675/sharedDB89e7 import or other gate closure is authorized.
The source/test/documentation delta is delivered by normal push to existing PR26;
no additional draft/API attempt is needed. Exact fixed-source validation evidence
is recorded separately from the later evidence-documentation revision.

## Fixed-source validation

Fixed source `f875672dea2b68fdec9e0b986f4863724da5d7ba` passes all 48 batch
controls (8 new G1 plus the 40 existing controls), 21 runtime and 70 protocol
checks. The delta reviewer's four F1/F2/F3 regression methods also pass, including
four controlled inventory deadline/cancellation scenarios. The actual pinned
helper checker passes single/persistent drain failures and persistent wait failure
with retained ownership through cancellation/expiry, then controlled recovery.
All 22 real-contract semantic assertions pass with complete initial/restoration
checks and source/dependency mutation rejection.

A fresh infrastructure-only probe at that exact source passes 10/10 terminals in
23.077 seconds, peak4, full equal pre/post inventories and exact owner ack. The
cleanup record confirms all 12 registered roots directly reaped, groups joined
and pipes closed before release, with no cleanup failures. Source/dependency
inventories contain 1,937/45,584 regular files. Zero owned fixture databases or
backend connections remain, and the separate owned PostgreSQL cluster is shut
down. See [safe evidence](evidence/R5-EXTERNAL-BATCH-G1-CLEANUP-FIX-20261004.json)
for exact pins, receipt/log hashes and reproduced historical failure observations.
This later evidence/documentation revision has a separate source identity and
inherits no fixed-source receipt qualification; its implementation helper is
byte-identical to the tested fixed source.
