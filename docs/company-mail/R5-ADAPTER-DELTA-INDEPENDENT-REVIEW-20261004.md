# Independent corrected-head delta review: REJECT

Exact candidate: `116d44a2a9c0b5d7ac6a09187bf3fae22c1a1e50`.
Frozen base: `412f875985854527f5e7d74040f18954c3a352bf`.
Prior candidate: `3ff6db72b579419a7145d2bc2a3b888c111c9b62`.
Design: `42f49538f7bceb6eb8e698d716c1418f85343413`.
Read the correction report and applicable repository/workspace instructions.
No root/scripts/test AGENTS.md or repository skills; workspace .agents/.codex empty.
Read web/AGENTS.md; no web edits. No applicable external skill is required.

The six original gaps are fixed, and the fixture replay adjustment is legitimate:
all 114 control names, order and expectations remain unchanged. Nonetheless,
new independently authored controls show incomplete filename/classification
contract validation. The interface must remain unfrozen.

## Remaining blocking findings

1. **P2: false Go filenames pass the verified receipt gate.**
   `scripts/r5_selected_binding_consumer.py:267` delegates file fields to a generic
   string/path-map validator. The selected-input and classified-input checks
   never enforce the helper's Go filename semantics. A complete fixture with
   `GoFiles=["not-go.txt"]`, matching `selected_local`/source hashes and both
   coverage lists returns verified receipts. A toolchain record with
   `field="GoFiles", path="src/runtime/not-go.txt"` also passes. Both are
   independently resealed and explicitly pinned so they reach the contract gate.
   The unchanged pure `v3.classify` rejects both metadata examples with
   `false Go source path`, under mocked archive-marker lookup and no filesystem
   traversal. Preserve the reviewed exception for generated `.test` GoFiles;
   enforce the original first-four-Go-fields rule for other inputs. The adapter
   currently accepts supplied metadata that could not pass its preserved producer
   contract, despite claiming complete nested validation.

2. **P2: local native classification linkage is incomplete.**
   `scripts/r5_selected_binding_consumer.py:385–412` checks rows only when present
   and matches local native records to selected path/field without the package.
   Add selected `internal/native.c` in `CFiles`, link its hash to source, and update
   selected/coverage package fields: an empty `native_inputs` is accepted.
   Supplying that native record with `package="foreign/unselected"` also passes.
   Independent pure classification of the corresponding metadata deterministically
   emits exactly `package="tabmail/internal", field="CFiles",
   path="internal/native.c", qualification="unknown"`.
   Require complete local native records for the package/path/field occurrences
   in selected metadata, and reject missing or wrongly attributed records.
   Keep classification and qualification unchanged; do not broaden exclusions.

New case IDs (all expect **reject**, currently **accept**):

* `non-go-local-GoFiles`
* `non-go-toolchain-GoFiles`
* `missing-local-native-classification`
* `foreign-package-native-classification`
* `orphan-generated-testmain`

The last probe also accepts a generated testmain record for an unrepresented
package with package_records still 1. It is recorded as a further linkage edge;
the two blockers above have direct preserved-classifier parity evidence and are
sufficient for rejection. No claim is made to reconstruct raw metadata from hashes
or perform live inventory/source closure validation.

## Replay scrutiny and independent results

The author replay implementation was inspected: it verifies the original reviewer
source SHA256, extracts only its two fixture functions, replaces the fixture hook,
updates the output source label and expected-gap-count assertion. No mutation,
control expectation, forbidden-action mock or fixed-pin check is removed.
Reran author replay against old and corrected adapter: old has exactly the original
six unexpected acceptances; corrected has zero. Reran all 35 author tests: pass,
zero OS-process events. These are author-fixture replays, not independent approval.

Independently completed the original partial fixture without importing author
fixtures or checks. New fixture scaffolding is explicit in
[independent_replay.py](evidence/R5-ADAPTER-DELTA-INDEPENDENT-20261004/independent_replay.py).
It extends the original independently constructed source records using preserved
helper contract constants and literal schemas; independent publisher seals and
pin computations are retained. The exact same fixture runs against both adapters:

* Old adapter: 114 controls, 108 expected results, exactly the prior six gaps.
* Corrected adapter: 114 controls, all expected results, zero prior gaps.
* [New independent edges](evidence/R5-ADAPTER-DELTA-INDEPENDENT-20261004/new_edge_checks.py):
  18 controls, 13 expected rejections and five unexpected acceptances.
* Separate classifier parity assertions establish both non-Go rejections and the
  required exact native record. They call pure classification with archive-marker
  lookup mocked; no producer, source capture or boundary traversal occurs.

Compared all four replay output sequences to the immutable original review:
all 114 names/order/expectations match. All twelve original fixed-pin counterfeit
controls remain and reject. Original selector/projection/stderr assertions remain.
The original review/evidence stays immutable at
[054d479](https://github.com/jyqj/tabmail/blob/054d47924e041117afb88f4d8e74217a89bd2464/docs/company-mail/R5-ADAPTER-INDEPENDENT-REVIEW-20261004.md).
The adapted positive fixture is not a runtime receipt or complete historical
archive/source closure attestation.

Audit guards precede adapter/helper imports; every pure program reports zero
OS-process events. New edges forbid capture/validate/Retention, getenv and candidate
file reads; import-time preserved helper/schema reads happen before those mocks.
No test process launches another process. Git and evidence bookkeeping run outside
these guards; the zero-event claim is scoped to the pure programs.

Commands from the review worktree:

```text
PYTHONDONTWRITEBYTECODE=1 python3 docs/company-mail/evidence/R5-ADAPTER-DELTA-INDEPENDENT-20261004/independent_replay.py --baseline
PYTHONDONTWRITEBYTECODE=1 python3 docs/company-mail/evidence/R5-ADAPTER-DELTA-INDEPENDENT-20261004/independent_replay.py
PYTHONDONTWRITEBYTECODE=1 python3 docs/company-mail/evidence/R5-ADAPTER-DELTA-INDEPENDENT-20261004/new_edge_checks.py
PYTHONDONTWRITEBYTECODE=1 python3 scripts/tests/r5_selected_binding_consumer_checks.py
PYTHONDONTWRITEBYTECODE=1 python3 scripts/tests/r5_selected_binding_consumer_checks.py --replay-review /tmp/r5-independent-controls.py --adapter /tmp/r5-rejected-consumer.py --expected-gaps 6
PYTHONDONTWRITEBYTECODE=1 python3 scripts/tests/r5_selected_binding_consumer_checks.py --replay-review /tmp/r5-independent-controls.py --expected-gaps 0
```

Baseline replay needs the exact prior adapter extracted with git show to
`/tmp/r5-rejected-consumer.py`; author replay needs original reviewer source at
`/tmp/r5-independent-controls.py`. Those sources retain the previously recorded
identities. Program success means successful evidence reproduction, not acceptance.

## Identities, preservation and scope

[Identities](evidence/R5-ADAPTER-DELTA-INDEPENDENT-20261004/identities.json) record
all evidence/source SHA256s, preservation checks and exact heads. Candidate adapter
SHA256: `eb444c6f0a2112ed473c7c89b71739cb0ba5ac62346efda8cbb20323dec49a89`.
Candidate author checks:
`82372ed6d398a80dac1864b8af95b5a47586144226bed6ec5e26a462fe3a98c7`.
Independent replay:
`d8a2151eba65efe1d6cfcd302ae36c6f7102526338a6893b6103eaa781f7d869`.
New edges:
`9f284ec994c9a9e164aef0103ae546bae79b24a7ab6be70c34dea4b1ae3963d9`.

All 2,208 frozen-base tracked paths retain their exact Git blobs. Correction touches
only the new adapter/checks and adds its report; rejected report is unchanged.
Trust arguments remain explicit caller-supplied inputs; no candidate/environment
trust derivation or producer action appears. Omission remains v2; explicit exact
integer 3, bool rejection and lazy v3 import remain. The exact twelve descriptors
and pin-first gate are preserved. Projection still directly delegates to the
unchanged helper; retained stderr binds without broader diagnostic exclusion.

This review branch adds only its independent report/evidence. No product edits,
producer/capture, runtime/preparation wiring, Go bridge, registry/discovery change,
formal suite, PG/mail/service, PR/merge/deploy/force action. Central **10/171** and
old F52 remain unchanged. Parent should request correction and a fresh exact-head
independent review; no interface acceptance is granted.
