# External runtime batch design: historical lifecycle review

Implementation after root review is described in
[R5-EXTERNAL-BATCH-RUNTIME-V1](R5-EXTERNAL-BATCH-RUNTIME-V1.md).
The status below records the original design handoff at `6e04545`.

Base: `9fc345308153fb664f115c7ac9ddd5abc3e46929`. Status: design only;
new batch executor NOT IMPLEMENTED, no batch-qualified receipt, no formal run.
Read the v2 runtime document, helper contract and recorded v2 checkpoint before
this review. This checkout has no `.skills` directory or standalone file named
`runtimev2contract`; the actual v2 contract is implemented in
`scripts/preparation/r5_external_runtime.py` and both consumer command builders.

## Catalog and actual isolation

The fixed JSON contains 46 case IDs. The Go-owned component producer selects 17
IDs and expands 26 independent variants: RC02 has 3, LF05 has 4, PE02 has 5,
all other selected IDs have 1. These counts describe this producer, not complete
46-case layer coverage. The direct Python component consumer is a separate
required consumer; the full required terminal set must be derived from all
catalog adapters before execution, not inferred from the 26 handler variants.

`r5UISeed` allocates a UUID-named disposable DATABASE, its own store/observer
pools, memory objects, miniredis, router and HTTP listener for each variant.
Existing isolation uses databases, not merely schemas. Preserve that stronger
boundary. Auth token subject/tenant IDs are fixture-specific, despite the shared
test JWT signing key. Every Vitest child starts independently, so its JS session,
auth and SWR memory are independent. This is static evidence; concurrent real
PG/HTTP/session isolation has not been proved by this change.

## Concrete conflicts requiring root review

1. `r5UIGrantBarrier` in the component observations test registers cleanup that
   cancels and waits on `done` for at most one second. Its nested SetWorkGrant
   goroutine has no separate owner join; a cancellation branch can send `done`
   before that revoker exits. Fixture cleanup completion therefore does not prove
   that every owner is physically joined. Increasing the wait would not prove it.
2. `r5UIExternalVitestCommand` uses Go CommandContext to run the Python helper.
   Python `launch` uses subprocess.run without a process-group supervisor.
   Go cancellation kills the direct Python child, with no explicit Node/Vitest
   descendant termination/join handshake. Python's postcheck/finally cannot be
   relied on after that kill; an owner token may remain. The old entry must not
   acquire new batch qualification based on this lifecycle.
3. `r5UIExternalRuntime` performs a full validation before the per-child helper
   takes its own exclusive token. Reusing it inside concurrent children would
   retain redundant inventories and make children contend for the same token.
   A new owned capability protocol is required; bypassing the old validation
   or exposing a generic unchecked launch flag is insufficient.

The user explicitly requested a conflict/design handoff if isolation could not
be proved. This checkpoint follows that condition and leaves concurrency closed.

## Proposed contract and write set after review

Add `scripts/preparation/r5_external_batch.py` and independent adversarial tests;
add a separately versioned batch contract/schema under `scripts/contracts/`.
Keep the existing v1/v2 manifest and failed receipts untouched. Bind the new
helper bytes, manifest independent pin, source/dependency/node/config hashes,
argv templates, catalog bytes, exact required case/variant/consumer terminal set,
worker limit 4 and fixed 120/180/75 budgets. Distinguish infrastructure-qualified
batch evidence from product-green; every required case must have real terminal
and valid observation/report evidence before any eligibility is published.

A single supervisor takes the same exclusive owned-root token before complete
preflight and anchors descriptors through terminal postcheck. It launches only
bound consumer/case commands using a private owned channel and a nonce scoped
to this batch. A second complete consumer batch must wait for token release or
fail closed, never interleave. No mutable module-global dependency identity may
be shared across batch instances; refactor observation's dependency-root argument
explicitly if required, preserving the v2 return contract and compatibility tests.

Update `scripts/check_r5_protocol.py` through an explicit new entry, not a silent
upgrade of the old one. Add a dedicated Go batch test/command builder next to
`r5_protocol_component_observations_test.go`. Factor reusable case execution and
fixture ownership only after review; leave expected markers/business assertions
and fixed JSON unchanged. All variants enter one checked queue exactly once;
missing, duplicated or unknown keys reject the entire batch. At most four
independent Vitest/fixture owners are live. Each keeps its 75-second context;
Go race120 and outer process180 remain unchanged.

Replace the PE05 barrier's implicit cleanup with an idempotent owner close that
cancels, releases its held transaction and synchronously joins both coordinator
and revoker before pool/server cleanup can complete. Report cleanup failures as
real staged case failures. Never convert a timed-out join into success.

The supervisor owns child process groups and pipe readers. On cancellation it
stops dispatch, terminates and reaps all tracked children and rejects unexpected
living tails. The Go consumer cancels child contexts and physically joins HTTP,
barrier and PG owners before reporting its final acknowledgement. A hard Go test
timeout cannot grant the owner acknowledgement; it rejects the whole batch.
Only after all child/fixture ownership has ended does the supervisor perform the
original complete source/dependency/content/mode/link/manifest terminal check.
Finalize all staged case evidence atomically as one rejected or qualified batch,
then release the exclusive token. Earlier passing children in a rejected batch
remain staged/unqualified. No inode/mtime memoization replaces content hashing.

Both cache-disable flags and strict sourceguard remain. Trusted sole-executor
isolation is required; no OS immutability or hostile transient write detection
is claimed. Mutation followed by restoration during execution remains unknown.

Required independent tests: bounded maximum, exact queue coverage, duplicate,
missing, nonterminal and false-pass rejection; descendant tails and timeout;
postmutation invalidates every staged pass; owner join/postcheck/lock-release
ordering; two consumers bind identical inputs; old-v2 qualification isolation.
Then use a small new TSX loopback HTTP/PG probe with independent fixtures to
exercise parallel auth/session/SWR isolation. Do not run the formal 46-case
producer until root reviews the fixed implementation and authorizes that run.

## Independently completed type repair

Add only `web/r5-external-runtime-probe-jsdom.d.ts`, a minimal legal declaration
of the real JSDOM constructor/document API used by the existing self-probe.
No dependency, package lock, config, marker, fixture or business assertion changes.
Full project TypeScript check first reproduced TS7016 for jsdom, then passed
with `--noEmit --incremental false --pretty false -p web/tsconfig.json`.
Existing `test_r5_external_runtime.py`: 21/21 pass.
Direct local self-probe uses both fixed cache-disable flags and original probe
config: one test passed, zero failed/pending, success=true. This direct local
run used the existing source-local development node_modules; it does NOT prove
external-layout descriptor qualification or either formal consumer. Vitest JSON
omits numRuntimeErrorTestSuites; no value is invented for that field.
Report SHA256: `aa4408df06a1f259282ed7b82ec3c26a16262842a2f24a64d63755c65bf81d3e`.
Only this hash and safe counters are published; the report is private in /tmp.

Central 10/171, wholePG180 and audit8high remain open. default675/sharedDB89e7,
Go1.25.7/two replaces, prior failed receipts and formal run evidence remain
unchanged. No merge/deploy or formal producer rerun occurred.
