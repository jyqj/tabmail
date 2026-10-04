# Independent slice-1 review: REJECT

Reviewed candidate `3ff6db72b579419a7145d2bc2a3b888c111c9b62` against base
`412f875985854527f5e7d74040f18954c3a352bf`; design read directly from
`42f49538f7bceb6eb8e698d716c1418f85343413` (that design file is not present in
candidate ancestry). Candidate report read:
`docs/company-mail/R5-SELECTED-CONSUMER-ADAPTER-SLICE1-20261004.md`.
No interface freeze or independent acceptance is authorized by this review.

## Blocking findings

1. **P1: incompatible nested archive policy/registry accepted** —
   `scripts/r5_selected_binding_consumer.py:196–200` checks the archive-boundary
   policy only as nonempty text and registry SHA256 only as digest syntax.
   Changing the expected `r5_immutable_archive_boundary_v1` to
   `incompatible_archive_v999`, or changing its pinned registry hash to 64 `e`
   characters, resealing source/binding/observation and independently publishing
   the synthetic bundle still returns verified receipts. The unchanged boundary
   declares registry hash
   `8e04081e1f2b9d9b82b854954b56a1ac71047646f8768d1b7a7ff89af223bb15`.
   The outer selected/source policies remain correct. This is a compatibility
   validation gap, not a bypass of the independently supplied bundle byte pin.
   A genuinely substituted bundle with the original fixed pin is rejected.
   The complete envelope must be compatible before future caller producer action;
   a valid byte pin alone cannot substitute for that contract check.

2. **P2: nested schema-invalid and false qualification records accepted** —
   `scripts/r5_selected_binding_consumer.py:135–142` validates several containers
   only as dicts or lists of dicts. Independently pinned, resealed receipts pass
   with `root_mvs=[{"Path":true,"Main":"wrong"}]`,
   `selected_local={"internal/a.go":{"sha256":false,"fields":7}}`,
   `qualification={"overall":"passed","compiler_native":"attested"}`, or
   coverage values `no_missing="yes", no_overlap=false, root_packages=true`.
   These cannot be valid outputs of the reviewed helper contract. Its private
   `_verify_receipt` authenticates seals, not schema correctness, so the later
   call does not repair the omission. Validate the nested receipt record types,
   required fields, linkage and fixed qualification semantics in the pure gate;
   do not expand diagnostic exclusions or execute a producer to fix this.

Reproduction identities are `schema-edge-archive-policy`,
`schema-edge-archive-registry`, `schema-edge-nested-root-mvs-bool`,
`schema-edge-selected-local-schema`, `schema-edge-qualification-false-claim`,
`schema-edge-production-coverage-types` in the machine-readable results.
These six controls intentionally publish NEW synthetic expected pins to reach
schema validation. They do not claim that a malicious candidate can replace a
controller-held original pin. Synthetic baseline receipts are adapter-level
objects, not live source captures, closure attestations or runtime qualification.

## Independent evidence and passing boundaries

[Independent source](evidence/R5-ADAPTER-INDEPENDENT-20261004/independent_checks.py)
imports neither author checks nor author fixtures. It builds its own in-memory
receipt publisher and independently computes source, binding and observation
seals, using the preserved helper for command construction/projection only.
[Results](evidence/R5-ADAPTER-INDEPENDENT-20261004/results.json) contain **114
explicit controls: 108 expected results and six unexpected schema acceptances**.
Separate assertions pass for omitted-v2 lazy dispatch, invalid selectors without
imports, projection equality, and retained stderr binding at all eleven retained
command positions. No OS-process audit events occurred. Audit hooks are installed
before adapter/helper imports and block subprocess, fork, spawn and exec.
Producer capture/validate/Retention, candidate reads and getenv are forbidden
during the main pure controls. Helper import-time reads of its preserved source
and schema occur before the read mocks.

Coverage includes resealed mutations at all twelve descriptor positions against
an original fixed bundle byte pin; role/argv/exit/domain/bool-length failures;
missing descriptors at all twelve positions; mismatched expected run/source/root/
producer/slot inputs; selector and outer schema/policy failures; malformed JSON,
duplicate bundle/receipt fields, floats/nonfinite numbers, swapped phases,
wrong contexts/roots, missing slots and duplicate receipt paths.
`verify_bundle` requires all expected identities/pin as explicit keyword inputs;
inspection found no environment/path trust derivation. Supplying an arbitrary
candidate-derived expected pin would be caller misuse; future wiring must preserve
controller provenance, which this pure slice cannot establish.

Author checks were rerun unchanged: **31 pass, zero OS-process audit events**;
[full combined log](evidence/R5-ADAPTER-INDEPENDENT-20261004/author-checks.log).
Their success does not cover the six failing independent expectations.

Commands (from the review worktree):

```text
PYTHONDONTWRITEBYTECODE=1 python3 docs/company-mail/evidence/R5-ADAPTER-INDEPENDENT-20261004/independent_checks.py
PYTHONDONTWRITEBYTECODE=1 python3 scripts/tests/r5_selected_binding_consumer_checks.py
```

The independent program exits zero only when all other expectations and all six
reported rejection gaps reproduce. Zero exit therefore means successful
reproduction of this REJECT review, not candidate acceptance.

[Exact SHA256 identities and preservation evidence](evidence/R5-ADAPTER-INDEPENDENT-20261004/identities.json)
record candidate adapter `4326d0e211cfddd1f29f2813c0924f79d40e2a9b463672f08d2e9bb9343b904d`
and author checks `27fd6df5d4bdb9162d78a60f325087793be002fea3dd89054df9ad84c0942573`.
All **2,208 pre-existing tracked paths** have identical Git blob identities between
base and candidate. Only the new adapter, non-discovered author checks and report
are added. Existing v1/v2 helper/caller paths, v3 helper/schema and discovery are
unchanged. Projection calls the exact existing `binding_payload`: hydration alone
is excluded as before, retained raw stdout hash/length alone are excluded as
before, retained stderr hash/length remain bound. Complete observations retain all
descriptor diagnostics. Explicit integer 3 is required; bool and unsupported
selectors reject; omitted helper selection remains lazy v2.

## Scope and remaining gate

Repository/workspace `.agents` and applicable skill directories were checked;
none exist in this tree, and workspace `.agents`/`.codex` are empty. The only
repository AGENTS.md is `web/AGENTS.md`, read; no web edits apply. No external
architecture/artifact skill is needed for this repository code review.
Review ran only the two explicit pure programs. Git/Python bookkeeping commands
were used outside the guarded programs for source inspection and preservation
hashes; the zero-event claim applies to the pure programs, not the review shell.

No product candidate edits, live producer, source capture, preparation/runtime
wiring, Go bridge, formal suite, PG/mail/service/registry action, discovery change,
main update, PR, merge, deployment or force operation. This independent evidence
commit adds only this report and its four evidence files on a separate review
branch. Correct the pure compatibility/schema gate and request independent review
of a new exact candidate before interface freeze. Central **10/171** and old F52
remain unchanged; no broader acceptance is claimed.
