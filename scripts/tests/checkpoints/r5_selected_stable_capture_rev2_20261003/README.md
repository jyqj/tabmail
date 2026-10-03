# Cold hydration successor naming coordination — focused progress append

Base remains PR21 `41b015c30c66b3ba58a3c1395e8559ebcd27a65f`.
Updated code freeze: `96af45104628d11ab9dec462d50194220275576a`.
The original `scripts/r5_selected_source_binding.py` now explicitly identifies
its cold-hydration successor as
`r5_root_selected_local_attestation_v1_stable_capture_rev2`. Receipt
`schema_version=2` continues to mark the incompatible stable observation order;
it is not the independently planned archive policy or inventory v4.
No `*_v2.py` is created or occupied; `r5_source_inventory.py` is not changed.

Only the policy label and module docstring change after the first delivered
implementation. The stable selection→MVS→selection→MVS algorithm, source and
topology protection, raw stdout hash bindings, pinned Go1.25.7/GODEBUG,
two replacements and schema19 remain intact. There are no retries.
Old selected v1 and the intermediate v2 label are rejected by policy equality.
The earlier checkpoint is preserved with its original policy and historical
run meaning. Its reproduction/failure logs are not rewritten; see
`../r5_selected_stable_capture_20261003/README.md`.

Fresh validation after this label change:

- `final-rev2-stable-tests.log`: 8/8 PASS, 40.009 seconds. Truly absent five
  extracted test dependency directories and empty GOCACHE; cold capture then
  immediate validate; warm capture then immediate validate. MVS 138, package
  records 630, selected local 613, generated testmain 60, historical Go inputs 21.
  Real file/module mutation, first-selection source mutation, raw MVS drift,
  re-signed hash forgery and old-v1 dispatch refusal controls all pass.
- `final-rev2-existing-tests.log`: original selected tests 12/12 PASS,
  12.198 seconds, in the independent clean clone with private caches.
- `cold-attestation.json.gz` binds the actual code-freeze snapshot. It does not
  claim to bind this later append. Artifact SHA256 manifest is supplied.

No new failures in this naming pass. Product Go tests, PG/runtime, Method19, SML,
wholeCI, deployment and merge remain not run. No production data or credentials.
Central R5-TODO, inventory, CI, archive boundary and historical evidence unchanged.

Normal Git push is authorized. Earlier native `gh pr create --draft` returned
GitHub GraphQL `Forbidden`; this pass does not retry that rejected action or use
another API route. Updated reviewable draft text is retained as `draft-pr-body.md`.
Remote SHA is supplied in the final handoff.
