# Batch supervisor lifecycle correction — 2026-10-04

The later [G1 cleanup correction](R5-EXTERNAL-BATCH-G1-CLEANUP-FIX-20261004.md)
extends lease-held reaping to persistent preflight drain errors and setup failures.
The fixed-source results and exact receipt identities below remain historical
evidence for F1/F2/F3 and are not relabeled by that correction.

This correction addresses F1/F2/F3 from the independent review at tested source
`f80b37fe14d856d2f2a048a06fe848fc4420448d`, retained in review commit
`0a2baf4a6ccd11dca7b9aa3c188e6ae2aab2a338`. The historical review, fault
counterexamples, failed receipts and exact prior receipt identities remain
unchanged. This document supersedes the old lifecycle return-bound claims.

The dedicated Linux CLI is the sole subprocess owner. Registered direct roots
are protected while a serialized subreaper sweep terminates and nonblocking-reaps
adopted orphans on every pending pipe-drain iteration. An orphan that starts a
new session and retains stdout/stderr can therefore be terminated before EOF.
Tail observations persist through the final barrier and reject the entire batch.
The final barrier remains mandatory and reaps all direct/adopted children. This
ownership rule cannot safely be generalized to an arbitrary embedded caller
with unrelated subprocesses.

Full preflight and terminal contract/source/dependency validation run in an owned
pinned-helper/Python worker. They perform the original complete recapture and
content/descriptor inventories; no sampled inventory or metadata cache replaces
those checks. The parent drains the worker with the remaining batch lifetime and
user cancellation, terminates it on expiry/cancellation, and requires physical
join plus successful complete validation. Routine execution shutdown uses a
separate event so it cannot accidentally cancel the mandatory postcheck. A
second final descendant barrier covers the terminal inventory worker as well.

RPC input reads use short time slices against remaining batch lifetime and
cancellation. Shutdown seals acceptance, actively shuts down every tracked
connection, and joins the server and all non-daemon handler/child owners before
postvalidation. A partial request receives no new 80-second lifetime. Every
accepted case cancellation before sealing permanently invalidates the batch,
including a cancellation after successful child results but before exact owner
acknowledgement. A cancellation after sealing is explicitly rejected as already
finalized. Exact physical fixture cleanup acknowledgement is still required;
accepted cancellation never fabricates an acknowledgement.

Go120, batch180, case75 and max4 remain unchanged. Batch180 is an execution
deadline, including pre/post inventories and active RPC reads. Dispatch and
eligibility stop at cancellation/expiry. Physical process/socket/owner cleanup
must still complete, potentially in a cleanup tail. OS uninterruptible waits
cannot be promised a hard physical-return bound. If any required cleanup or
validation remains unknown, no qualified receipt may be issued. The live pinned
capability lease remains held through cleanup and staged all-or-none publication.

Validation passes 10 focused synthetic owned process/socket and controlled
descriptor inventory tests, all 30 existing batch controls, 21 runtime controls
and 70 protocol regressions. It also requires a fresh
fixed-source infrastructure-only probe. Fixed implementation source
`52dd8b9ef538b55812b35308cae8b469400e2705` passes the fresh probe in
21.437 seconds: 10/10 infrastructure terminals, peak4, complete equal pre/post
inventories and exact physical fixture acknowledgement. Zero owned fixture
databases/backend connections remain, and the separate owned PostgreSQL cluster
is shut down. All 22 real-contract semantic assertions pass against that fixed
source, including source/dependency mutation rejection and accepted late-case
cancellation invalidating all eight staged children. See the
[safe evidence](evidence/R5-EXTERNAL-BATCH-LIFECYCLE-FIX-20261004.json) for exact
pins, receipt/log hashes and retained intermediate/rejected attempt identities.
The semantic harness is corrected in this later delivery revision to preserve
the cleaned pinned-tool PATH; its earlier empty-environment invocations rejected
at full preflight and credit no execution. The implementation helper remains
identical to the tested fixed source. This newer evidence/documentation revision
has a separate identity and inherits no receipt qualification. A full-supervisor
late-cancel counterexample is a supervisor-contract test; it does not establish
a real Go business false-pass. The fixed semantic controls also retain real
source/dependency mutation rejection and exact restoration checks.

The authoritative 46-ID catalog, 40 Go paths plus 3 Python assertions, 17 handler
IDs/26 variants, business fixtures/markers/expectations, dependencies, official
lock, Go1.25.7 and both replaces remain unchanged. Formal43/26/46, wholePG180,
audit and actual email do not run. default675/sharedDB89e7 are excluded. Central
10/171 and all earlier failures remain open. No merge/deploy or gate closure is
part of this correction. An infrastructure receipt credits no business case.
