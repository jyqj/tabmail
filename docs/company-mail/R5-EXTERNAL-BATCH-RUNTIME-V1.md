# Explicit external batch runtime v1

Implementation frozen for the final infrastructure probe:
`f80b37fe14d856d2f2a048a06fe848fc4420448d`, based on
`9fc345308153fb664f115c7ac9ddd5abc3e46929` plus the independently approved
PE05 owner delta from `8a5200bfd9496b02c2182cc0a04151103973a6e7`.
The earlier design handoff is historical; this implementation is a distinct
`r5_external_batch_validation_v1` contract. Its source/contract and infrastructure
probe do not authorize the formal producer run. Root must review this fixed
implementation before authorizing that run.

## Owned lifecycle and publication

`r5_external_batch.py capture` produces an independently byte-pinned UNADOPTED
contract containing the original runtime-v2 observation, complete catalog hash,
exact derived required terminals, pinned tools/helper/bridge/config hashes,
exact Go/child/Python argv templates and fixed build context. `run` loads that
pin; semantic recapture and the original complete descriptor-bound source and
dependency validation are mandatory. Old v1/v2 receipts and failed runs keep
their original identities. Old-v2 policies cannot authorize this new entry.

The supervisor serializes cooperating batches on the same owned-root token;
waiting never steals a stale token. It anchors source/cwd/dependency descriptors
and performs one full preflight for the batch. The private Unix channel carries
the actual owner nonce, independent contract hash, allowlisted case key and
private fixture path. It accepts case/cancel/ack operations. It accepts no
executable, argv or generic unchecked-launch flag. A capability works only after
preflight inside the live lease. Children share the same fixed source and
installed dependencies; observers receive their dependency root explicitly.

The Go bridge starts no Python subprocess. The supervisor starts each independent
Vitest process in its own owned process group, with the fixed cache-disable
options. The Go runner admits at most four fixture owners, including cleanup;
each retains a separate disposable DATABASE, store and observer pools, Redis,
HTTP listener, fixture and Vitest JS realm. Fixture files must be owned/private;
full fixture bytes, descriptor identity and fixed catalog input are checked.
There is no shared database or migrated template. The sampled JS realms use the
same SWR key while retaining independent auth/session storage and database reads.

| Bound | Value |
| --- | --- |
| Go toolchain | Go 1.25.7 linux/amd64 |
| Go test | race, count1, timeout120 |
| Batch/process | 180 seconds |
| Case context | 75 seconds |
| Internal workers | at most 4 |

All results remain staged. Go `t.Run` waits for parallel leaves and their LIFO
cleanup, then sends the exact completed-owner set. The approved barrier owner
synchronously joins coordinator and nested revoker. Normal fixture cleanup also
closes HTTP/store/observer resources and completes the owned database drop.
Go goroutine join alone is not proof of PostgreSQL backend cancellation; a
cleanup failure or missing ack rejects the group. A hard Go timeout stops further
dispatch, kills/reaps owned children and withholds the terminal check without
fixture-owner acknowledgement. No prior passing child inherits qualification.

The supervisor joins pipe readers/direct children/process groups and, as an
isolated Linux subreaper, terminates and reaps detached adopted tails. An observed
tail rejects the group. RPC owner threads join before final validation. Only the
full terminal source/dependency/content/mode/link/manifest check plus every exact
required passing terminal permits atomic batch publication. Token contents and
descriptor identity are rechecked; observed lease-finalization failure changes
that attempt's receipt and all its child qualifications to rejected. A Batch
instance is single-use and cannot rewrite an earlier receipt. Token release
follows finalization. Receipts contain hashes, counters and status, with
`product_green=false` and `task_complete=false`.

## Exact scope

The unchanged catalog has 46 case IDs. The component scope derives 40 Go runtime
paths from three actual consumer groups and 3 direct Python Vitest assertions:
43 required terminals. The handler contribution is 17 IDs / 26 variants. The
new handler parent and its auxiliary `/owners` group are explicitly mapped to
the original declared case paths for classification; original raw JSONL remains
private and independently hashed. Original fixtures, business expectations and
markers remain unchanged. The complete derived set is checked; missing,
duplicate, skipped/nonterminal or failed terminals reject the whole batch.
This scope leaves other catalog layers and cases unknown/unqualified.

Probe mode is separately explicit. Its 8 synthetic Go-owned PG/HTTP/TSX realms,
their Go parent and the Python builder's original TSX/CJS/ESM/jsdom self-probe
form 10 required terminals. Its eligibility scope is `infrastructure_probe_only`;
these terminals credit no catalog business case. Both paths use the same pinned
CLI, normal bundle config loading, `--cache=false` and
`--experimental.fsModuleCache=false`. Sourceguard and the original complete
inventories remain mandatory. No inode/mtime cache replaces content reads.

This is a sole-executor isolated-workspace contract. It is
`not_qualified_for_hostile_concurrent_mutation`; no OS immutability is asserted.
Mutation followed by restoration during execution remains unknown. Exhaustive
dynamic/native/generated/module-loader coverage remains unknown. Old v2's direct
Go-to-Python cancellation limitation remains separately documented; its receipts
are not relabeled as batch-qualified.

## Independent validation

The safe evidence file beside this document records exact source and report
hashes. Private manifests, packets, stdout/stderr and fixtures stay outside
source/dependencies; response bodies, auth tokens and fixture values are not
published.

- Batch controls: 30/30, including real owned process/descendant deadline and
  cancellation, detached tails, actual maximum-four dispatch, missing/duplicate/
  nonterminal/false-pass rejection, real terminal source mutation invalidating
  every staged pass, lease ordering/serialization/finalization failure, live
  capability gating, late cancellation, two consumers' identity agreement and old-v2 isolation.
- Original v2 controls: 21/21; existing protocol regressions: 70/70.
- Full project TypeScript check passes with no skip. The legal minimal jsdom
  declaration repairs only the original self-probe; dependencies/lock are unchanged.
- Go bridge exit/physical-response control passes with race; the approved four
  focused owner tests also pass with race and two independent loopback PG/HTTP
  fixtures. Their scope does not qualify formal component cases.
- The first real batch probe at `1337ae8` was rejected because the new probe
  config omitted tsconfig paths, causing setup import failure. Its independent
  Python probe passed; the group and all staged child eligibility stayed rejected.
  The failed receipt is retained. The config fix affects only the new probe.
- The corrected intermediate probe at `c77df12` completed 10/10 terminals,
  eight Go realms, peak4 and full equal pre/post inventories. It took 23.478s
  wall time; the Go top test reported 7.780s. Those timings are scoped observations,
  not a prediction for formal 26/46-case execution. Exact stage attribution and
  physical I/O remain unknown.
- The final fixed-code probe also passes 10/10, peak4, complete physical joins/
  fixture cleanup ack and equal full pre/post inventories. Its wall time is
  24.601s; the Go owner reports 7.650s. See
  [safe evidence](evidence/R5-EXTERNAL-BATCH-V1-20261004.json) for exact hashes.
  A cancellation/deadline observed before eligibility returns rejects all staged
  passes, including cancellation during terminal postcheck.

The official-lock toolroot was installed using normal lifecycle-enabled npm ci.
The earlier copied development tree failed lock-version validation and was not
qualified. Default NODE_PATH/npm environment pollution was rejected and the
prepared/run environment explicitly cleans those unbound overrides.

The complete formal 26-handler/46-catalog producer has not run. default675 and
sharedDB89e7 are excluded; package/lock, Go version, both replaces and audit inputs
remain unchanged. Central 10/171, wholePG180 and audit8high remain open. No merge,
deploy or PR API retry occurred; the previously attempted draft returned Forbidden.

## Entry points for reviewed execution

Prepare a fresh clean owned toolroot/source checkout and original runtime-v2
manifest with independent pin first. From that exact source, capture a new batch
contract using the manifest environment and pinned Go path:

```sh
python3 scripts/preparation/r5_external_batch.py capture \
  --source /owned/toolroot/source --go /pinned/go --mode probe
```

Save its exact bytes outside source/dependencies and independently pin their
SHA256. The explicit run entry consumes that file and a fresh absolute private
output directory:

```sh
python3 scripts/preparation/r5_external_batch.py run \
  --contract /private/batch.json --pin SHA256 --output /private/new-output
```

Use `--mode components` only after root authorizes the formal execution. Capture
and run must use the bound source helper and pinned Python interpreter. Any
later source/evidence publication changes the source identity and requires fresh
preparation; the recorded frozen probe receipt does not qualify a newer tree.
