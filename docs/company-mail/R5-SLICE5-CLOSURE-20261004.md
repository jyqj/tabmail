# Slice5 bounded closure coverage verification

Existing policy covers all 14 changed consumer/control inputs. No inventory, registry, helper, schema, consumer or historical receipt change is needed to close this bounded membership question. This is source coverage evidence; parent independent review and authorization remain required for the next physical gate. Central **10/171** and every historical **F52 failure** remain unchanged.

## Exact source and preservation

Delivery verified: `593bbc6a3ac91b5dbf8917602e45900bff094e27`. Tested implementation retained: `73777de3f51ff185d02ab2e1193421fa91cde9d4`, tree `f27e02ce4987488d783a22f7cc1620dfe3795cd4`. Delivery differs from implementation only by the 31 slice4 report/evidence paths. No claim transfers historical runtime receipts to these sources or to this evidence delivery.

Read VERSIONING, R5-TODO, web/AGENTS.md, actual slice4 integration report and its machine-readable authority inventory, and actual inventory/archive/selected/consumer/batch/runner code. No root or scripts AGENTS.md was found; no web changes require the Next.js guide. The user explicitly selected the stacked frozen source. Work is isolated in `/workspace/tabmail-slice5`; the original `/workspace/tabmail` checkout remains at `d6d512172fb6b874c3283d9df3f4d56758684a13`.

[Results and preservation manifest](evidence/R5-SLICE5-CLOSURE-20261004/results.json) authenticate all **2,331 frozen tracked files** against their actual Git blobs before checks and repeat SHA256 preservation after checks. Every path in the 14-input inventory and every retained authority path matches its pinned SHA256 and Git blob. [Final preservation check](evidence/R5-SLICE5-CLOSURE-20261004/final-preservation.json) also verifies every original regular-file mode. No tracked frozen bytes or modes were edited. All old receipts, author/independent reports, historical registry hashes, protected records, locks, catalogs, forks, discovery, and shared TODO remain frozen. Evidence files added by this worker are outside implementation/control paths.

## Independently verified authority facts

| Finding | Actual code evidence | Verification kind |
|---|---|---|
| All 14 paths already belong to the full source domain | `r5_source_inventory.py:463`: `_current_source_paths` walks `cmd/internal/web/scripts`; `:567` unions `set(base['files'])` into v4. `_walk` at `:211` admits each actual filename. | Executed real-root read-only membership enumeration and hash authentication; executed actual v4 capture and validation on an owned copy of frozen tracked files. |
| `allowed_new_control_files` is parsed, not a membership gate | `r5_archive_boundary.py:155` requires this registry key. AST enumeration of every current `scripts/**/*.py` finds exactly this one occurrence. `check` reads modules, historical files, protected Go and production roots, and never reads the list. | Executed static source scan; code inference about membership semantics. Controls absent from the list successfully enter the actual synthetic-copy v4 files map. No candidate registry or hash refresh was used. |
| `v3_source` intentionally has exactly three keys | `r5_selected_source_binding_v3.py:26` defines `_V3_FILES`: helper, producer schema, explicit checks. Consumer `:560` calls `_shape` with that tuple and pins helper/schema at `:562–563`. Consumer base-source records cover the wider domain. | Static authority reading plus actual `_shape` missing/extra/rename negatives for the three keys. Full consumer dispatch was not executed in these membership-specific supplemental checks. |
| Changed Go files are current production inputs | Archive `check` subtracts protected historical paths from all `.go` files and admits current paths under production roots. Both changed files reside under `internal`. | Executed actual archive/v4 validators and all 14 file hashes on owned source. Historical records are separately authenticated and never refreshed. |
| Versions and pins remain explicit | Inventory `:610` rejects mixed schema/policy and compares full canonical recapture; consumer exact integer selector; batch runtime/contract gates pair selected3 with runtime3/batch2 and require independent manifest byte pins. | Actual pure validator negatives and mocked producer-boundary tests; no runtime launch. |

## Executed bounded checks

Reproduce from a clean frozen delivery checkout, with these two evidence scripts available at the same relative paths:

```sh
python3 -B docs/company-mail/evidence/R5-SLICE5-CLOSURE-20261004/verify_closure.py
python3 -B docs/company-mail/evidence/R5-SLICE5-CLOSURE-20261004/verify_predicates.py
```

The first harness requires HEAD equal to the frozen delivery and authenticates its tree before installing the guard. Its read-only Git processes are outside guarded tests. All imports, fixtures, mutations and pure validators run after an OS audit hook rejecting process launch/fork/exec/spawn. The second installs its guard before authority imports. The only permitted interpreter starts are the two harness invocations; neither executes Go, Node or a child process. There is no discovery or formal runner invocation. An initial harness assembly attempt referenced two tests under the wrong class and stopped before running tests; the corrected explicit class mapping produced the retained successful run.

| Executed evidence | Test methods | Failures/errors/skips | Guarded OS process events |
|---|---:|---:|---:|
| [Closure checks log](evidence/R5-SLICE5-CLOSURE-20261004/checks.log) | 31 | 0 / 0 / 0 | 0 |
| [Supplemental predicate log](evidence/R5-SLICE5-CLOSURE-20261004/predicate-checks.log) and [results](evidence/R5-SLICE5-CLOSURE-20261004/predicate-results.json) | 5 | 0 / 0 / 0 | 0 |

**36 test methods pass.** Subtests are not added to that method count. The first harness records **66 custom negatives**, including **56** actual v4 per-input negatives (14 paths × bytes/missing/extra/rename), eight receipt membership/version changes and two mocked midflight observations. The supplemental harness records **33** predicate negatives. Reused explicit pure methods additionally execute every historical file byte/delete/rename check, registry forgery/duplicate/traversal checks, marker mutations, unknown nested modules, exact two-fork grammar/version/path negatives, root versus explicit/variant/file coverage, archive imports/embed/generate, native/build/ignored Go rejection, workspace/vendor, symlink/FIFO, descriptor replacement, source drift and externally pinned/resealed envelope rejection. These reused subtests remain individually visible in source and named logs; no inflated aggregate mutation count is claimed.

The actual full-copy v4 positive capture/validation covers changed consumer bytes, all current source inputs and immutable archive authority without any producer. `_V3_FILES` and `allowed_new_control_files` findings combine executed static inspection with executed pure predicates; static inspection alone is not represented as a runtime negative. Versioned source-directory capture is enumeration/hash work, not selected Go metadata capture. Producer calls in selected drift/envelope tests are mocked.

Budgets remain **100000 entries / depth64 / 64MiB**, statically read from the unchanged archive guard. Executed tests reduce entry/depth/byte constants against tiny synthetic fixtures to verify fail-closed behavior. Exact maximum/max+1 populations and 64MiB payloads were not allocated. Quiescence is still required; before/after checks are not atomic hostile-write protection. This work proves no Go syntax/type/compile behavior, native/toolchain/external-cache attestation, dynamic loader completeness or physical cleanup/lifecycle behavior. No Go/Node/runtime, real selected capture, DB/mail/service, formal/default/shared/component/lifecycle suite was executed.

## Minimum next physical qualification — proposal only

Parent should independently review this evidence first and authorize a separate, fresh controller run. Freeze the exact next source commit before capturing anything; if this report delivery becomes SOURCE, its full commit rather than `593bbc6` must be supplied. Preserve old receipts; do not relabel, reseal, replace pins or enlarge membership to get a pass.

Minimum useful physical gate is **selected-v3 live admission followed by batch-v2 probe mode**, not formal component qualification. It exercises the two admission slots, fresh source closure, serialized pin chain, real Go probe owners and one Python/Node external-runtime probe. Four preparation slots and all twelve ordered envelope positions remain unchanged; this minimum does not physically qualify the four-slot typed-wire preparation flow. That separate flow uses `r5_source_runner_prepare.prepare(..., selected_binding_version=3)`, compiles/runs only `TestOrdinaryReceiptOpenAPIWireFixtures`, and creates `source/web/node_modules/typescript`. Runtime admission explicitly rejects source node_modules. Therefore it must use a separate owned clone/evidence gate; do not casually combine its receipts or remove its installed outputs to claim runtime continuity.

[Proposed physical controller](evidence/R5-SLICE5-CLOSURE-20261004/proposed_physical_controller.py) is **unexecuted and requires parent review**. It calls existing functions with explicit integer3, captures each admission receipt from owned returns, preserves observation and serialized-byte pins in controller memory, publishes exclusive files, invokes runtime live admission, pins the returned runtime manifest, captures a batch2 contract in **probe** mode, and runs that one batch. It preserves the existing cancellation callback. It never invokes the scoped shared-components script or any formal discovery entry. These proposed commands were not run:

```sh
# Parent supplies approved absolute paths; root, evidence and output names must be fresh.
# Provision prerequisites separately before the timed run; never reuse a mutable old toolroot.
mkdir -m 700 "$R5_TOOLROOT"
git clone --no-hardlinks /workspace/tabmail "$R5_TOOLROOT/source"
git -C "$R5_TOOLROOT/source" checkout --detach "$R5_APPROVED_SOURCE_COMMIT"
git -C "$R5_TOOLROOT/source" status --porcelain --untracked-files=all
# Require empty status, real .git directory, no source node_modules or ancestor shadows.
# Operator provisions R5_TOOLROOT/package{,-lock}.json from exact source/web bytes
# and a separately reviewed lifecycle-enabled installed dependency tree at toolroot/node_modules.
# No npm install is authorized by this proposal's command block.

# Controller is copied from this evidence delivery to a private location outside SOURCE.
# The pinned Python interpreter, Go1.25.7 producer and Node are operator-approved absolute paths.
timeout --signal=TERM --kill-after=30s 1800s "$R5_PYTHON" -B "$R5_CONTROLLER" \
  --root "$R5_TOOLROOT" --source "$R5_TOOLROOT/source" \
  --source-commit "$R5_APPROVED_SOURCE_COMMIT" \
  --go "$R5_GO" --node "$R5_NODE" \
  --cache "$R5_GOCACHE" --modulecache "$R5_MODULECACHE" \
  --evidence "$R5_NEW_EVIDENCE"
```

Prerequisites are blocking inputs, not guessed defaults: exact clean clone available to the physical operator; owned quiescent root; root package/lock bytes equal SOURCE; reviewed dependency installation; absolute metadata-producer bytes equal SHA256 `76ac600b41ad2eceee5d39d02af78009516f8b2a29f30bcc80f765c3e4b4f5a8`; approved Python/Node executable identities; Linux amd64; caches and evidence outside SOURCE/dependencies; environment pollution removed according to existing validators; no DB/mail/service credentials or live product traffic needed. Clone provisioning and dependency installation have separate budgets and review. The timed controller may hydrate module caches through producer network calls.

Budget: proposal outer **1800s +30s kill grace** (supervision limit, not acceptance relaxation). Selected helper preserves **180s per producer command**, twelve ordered descriptors per envelope and exact default/race-r5protocol contexts. Initial two captures and live admission two recaptures may issue many metadata commands; cache hydration can consume this proposed outer budget. Runtime revalidation adds further recaptures. Batch retains **workers4 / Go120s / process180s / case75s / RPC65536**, one remaining deadline and original drain/reap rules. Entry/depth/byte bounds above and dependency inventory **150000 entries / 1GiB** are unchanged. Timeout/failure/tail remains non-green; retain all partial diagnostics and process inventory, and have the operator verify physical descendants/lease cleanup rather than assume outer timeout proves reaping. Do not increase existing internal budgets to rescue failure.

Acceptance requires newly pinned archive/bundle/runtime/batch/result bytes at the approved commit, all two-slot contexts and twelve envelope positions accepted, full pre/post SOURCE and dependencies equal, exact probe terminal set (Go parent plus eight owner realms and one Python assertion), zero skips/errors, joined processes and cleanup acknowledgement, and `BATCH_QUALIFIED`. Product tasks stay incomplete. Even a passing probe does not qualify formal cases, default/shared suites, DB/mail/services, Method19, external native/toolchain inputs, whole CI or lifecycle gates. Generated/dynamic loader boundaries remain as reported by the unchanged validators.

Shared TODO proposed substeps only: parent independent slice5 review; accept unchanged membership sufficiency; freeze next SOURCE; independently review controller/provisioning/budgets; authorize one physical probe; review fresh failures or success without changing old F52 or 10/171. No shared TODO write, PR, merge, deploy or force action is part of this closure delivery.
