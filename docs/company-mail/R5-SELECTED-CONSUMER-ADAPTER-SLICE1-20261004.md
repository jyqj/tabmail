# R5 selected-v3 consumer adapter candidate — bounded slice 1

Base: `412f875985854527f5e7d74040f18954c3a352bf`.
Approved design: `42f49538f7bceb6eb8e698d716c1418f85343413`,
`docs/company-mail/R5-V3-CONSUMER-MIGRATION-DESIGN-20261004.md`.
Exact tested implementation commit: `125b002e87a02529a1520d3e3afccff89805929a`.
Candidate branch: `candidate/r5-consumer-adapter-20261004`.
The subsequent report-only commit carries these identical implementation bytes;
its exact head is supplied in the handoff. This is a candidate, not an interface
freeze or an independent implementation approval.

## Scope and instructions

Only the two new Python paths below and this uniquely scoped report are added.
All 2,208 pre-existing tracked paths are unchanged from the base (clean tracked
`git diff --exit-code BASE --` before adding these files; final committed delta
must contain only these three additions). No root, scripts, or scripts/tests
AGENTS.md or repository `.agents/skills` exists. Workspace `.agents`/`.codex`
are empty. `web/AGENTS.md` was read; no web files are edited.

No helper/schema, existing tests, discovery, registry, closure, capture/publisher
CLI, preparation, runtime, Go bridge, service, database, mail, or formal suite
changes/runs. No old F52 acceptance is claimed; central **10/171** is unchanged.
No PR, merge, deployment or force push. Parent must obtain independent review of
the final exact candidate head before freezing the interface or assigning slices.

## Interface and trust contract

* `selected_version(selected_binding_version=2)` accepts exact integer 2/3;
  omitted selection stays v2, explicit None/bool/float/string/unknown rejects.
  Historical v1 callers are untouched, and integer 1 is not a current selector.
* `selected_helper(selected_binding_version=2)` lazily returns the existing helper.
  It invokes no producer and adds no error/retention wrapper. Default loads v2;
  v3 is imported only after explicit integer selection.
* `verify_bundle(bundle_bytes, receipt_bytes, *, trusted_bundle_byte_sha256,
  run_id, source_commit, source_root, producer, allowed_slots,
  selected_binding_version=2)` is pure and v3-only: omission or v2 rejects.
  All trust inputs are separately supplied, required keyword arguments; none
  comes from candidate paths, environment, a colocated sidecar or receipt seals.
  Returns decoded, verified receipts by slot after every check passes.
* `binding_payloads(receipts, *, selected_binding_version=2)` is explicitly a
  projection-only convenience for already verified results, not an authentication
  gate. It delegates directly to the unchanged helper's `binding_payload`.
  Cross-slot full-payload equality is a future caller's responsibility: default
  and race contexts are intentionally distinct.

Pin bundle fields are exactly the design's schema_version=1,
policy=`r5_selected_observation_pins_v1`, run_id, source_commit, source_root,
selected_binding_version=3, producer={path,sha256,version}, observations.
Each observation has exactly receipt_path, receipt_byte_sha256,
observation_sha256, context. Receipt paths are canonical absolute **labels**;
they are never opened. The caller supplies receipt bytes separately by slot.
Distinct paths are required; identical complete observations may share digests.

Approved slot groups are exactly `default`, `race-r5protocol` for admission, or
`before-default`, `before-race-r5protocol`, `after-default`,
`after-race-r5protocol` for preparation. The caller chooses the entire group;
missing/extra/duplicate/renamed slots reject. Phase is encoded by the slot name
and authenticated byte association. Default/race contexts are checked against
helper constants using type-sensitive JSON equality, including int versus bool.

The controller's independent expected hash authenticates **serialized bundle
bytes** first. Bundle identity is then checked against every expected caller
input. Each bundle slot pins exact receipt bytes and the complete observation
digest. Replacement/resealed candidate bundles fail against the original pin.
The receipt's original schema has no run_id or Git source_commit field: these
are checked in the authenticated bundle, not fabricated inside the receipt.
Source root/policy/version/context and source closure seals are additionally
checked inside the receipt. The adapter validates shape and linkage, not live
source inventory or proof that an executor actually captured those bytes.
Controller provenance and quiescence remain prerequisites, as in the design.

Before private helper verification, the adapter checks exact receipt/base-source/
archive-boundary/envelope fields, producer identity, normalized absolute producer
path, fixed Go1.25.7 executable hash, helper/schema identities, source/context
linkage, environment ABI/flags, file digest shapes and variant-directory linkage.
It requires all twelve ordered descriptors: env; hydration; selected; MVS;
root/explicit/variant; repeated selected/MVS/root/explicit/variant. Every role,
exact absolute argv, integer zero exit, stdout domain, hash and integer length is
validated, as are repeated projection/raw-MVS equalities. Duplicate JSON keys
at every nesting level, nonfinite/floating values, malformed JSON, mixed policies
and versions reject. Candidate raw stdout bytes are not supplied by this contract,
so no claim is made to reconstruct metadata streams from their hashes.

Exact private dependency is `r5_selected_source_binding_v3._verify_receipt` from
the base: it checks policy/version and the observation/binding seals, **not** the
adapter's descriptor/slot/shape contract. The adapter performs those checks first.
It uses the same helper projection without reproducing or relaxing it: hydration
is excluded only from binding, and raw stdout hash/length are excluded from the
retained commands' binding exactly as before; retained stderr hash/length bind.
Complete observations bind every descriptor field, including hydration diagnostics.
No capture, validation producer action, retention sink or wrapper is called.
Existing primary/secondary error-retention behavior is therefore unchanged.

## Exact source and preservation hashes (SHA256)

| Path | SHA256 |
| --- | --- |
| scripts/r5_selected_binding_consumer.py | 4326d0e211cfddd1f29f2813c0924f79d40e2a9b463672f08d2e9bb9343b904d |
| scripts/tests/r5_selected_binding_consumer_checks.py | 27fd6df5d4bdb9162d78a60f325087793be002fea3dd89054df9ad84c0942573 |
| unchanged scripts/r5_selected_source_binding.py | 15f5cdf4b13ee09558cc368e367091d4802804a7c9724065faf5c50605b8a873 |
| unchanged scripts/r5_selected_source_binding_v2.py | 4bafe6a0b146c69aaf76dfaa206a95ac581739e3c0efa4a52bf36613ae817d67 |
| unchanged scripts/r5_selected_source_binding_v3.py | 12799a213c93510bdb449b5f610a493f517395878dd83c4b48a06e574bbf6acb |
| unchanged scripts/contracts/r5-selected-binding-v3-producer.json | cc80c215f1bf51f2a014ff5e6a37084f29f54a904c69ee58ba52febbd125df66 |

## Pure synthetic results

Command: `PYTHONDONTWRITEBYTECODE=1 python3 scripts/tests/r5_selected_binding_consumer_checks.py`

Exit 0; **31 checks passed**, with many per-field/position/version subcases;
**zero OS-process audit events**. The audit guard is installed before helper
imports and rejects subprocess, fork, spawn and exec events. Checks mock capture,
validate and Retention as forbidden; another check forbids candidate file reads
and getenv. Synthetic fixtures are constructed directly in memory without
importing or running existing test fixtures or producer captures. These are not
runtime receipts or fresh qualification. Initial development exposed a fixture
resealing error for a deliberately missing role; the fixture publisher was fixed
to pin malformed bytes so they reach the adapter shape gate. Final results follow.

Complete combined stdout/stderr log SHA256:
`d4f9e6ebeba8cc702ffdabbd65f673d74741d924611a40f949a04114ad7e7f21`.

```text
test_adapter_does_not_read_environment_or_candidate_paths (__main__.ProjectionAndIsolationChecks.test_adapter_does_not_read_environment_or_candidate_paths) ... ok
test_only_existing_hydration_and_raw_stdout_exclusions (__main__.ProjectionAndIsolationChecks.test_only_existing_hydration_and_raw_stdout_exclusions) ... ok
test_os_process_guard_has_observed_zero_events (__main__.ProjectionAndIsolationChecks.test_os_process_guard_has_observed_zero_events) ... ok
test_retained_command_stderr_hash_and_length_remain_bound (__main__.ProjectionAndIsolationChecks.test_retained_command_stderr_hash_and_length_remain_bound) ... ok
test_verification_never_calls_capture_validate_or_retention (__main__.ProjectionAndIsolationChecks.test_verification_never_calls_capture_validate_or_retention) ... ok
test_bool_context_cannot_equal_integer (__main__.ShapeChecks.test_bool_context_cannot_equal_integer) ... ok
test_bundle_and_pin_missing_extra_fields (__main__.ShapeChecks.test_bundle_and_pin_missing_extra_fields) ... ok
test_command_shape_and_field_types (__main__.ShapeChecks.test_command_shape_and_field_types) ... ok
test_duplicate_malformed_json_at_nested_boundaries (__main__.ShapeChecks.test_duplicate_malformed_json_at_nested_boundaries) ... ok
test_environment_and_source_nested_type_mismatches (__main__.ShapeChecks.test_environment_and_source_nested_type_mismatches) ... ok
test_missing_extra_descriptor_at_every_position (__main__.ShapeChecks.test_missing_extra_descriptor_at_every_position) ... ok
test_order_role_argv_exit_and_domain_at_every_position (__main__.ShapeChecks.test_order_role_argv_exit_and_domain_at_every_position) ... ok
test_receipt_top_level_shape (__main__.ShapeChecks.test_receipt_top_level_shape) ... ok
test_swapped_context_root_source_and_producer (__main__.ShapeChecks.test_swapped_context_root_source_and_producer) ... ok
test_variant_directory_inconsistency_and_repeat_mismatch (__main__.ShapeChecks.test_variant_directory_inconsistency_and_repeat_mismatch) ... ok
test_duplicate_receipt_path_rejected (__main__.TrustChecks.test_duplicate_receipt_path_rejected) ... ok
test_forged_resealed_replacement_against_original_controller_pin (__main__.TrustChecks.test_forged_resealed_replacement_against_original_controller_pin) ... ok
test_identical_complete_observations_may_share_digests (__main__.TrustChecks.test_identical_complete_observations_may_share_digests) ... ok
test_missing_and_malformed_trust_inputs (__main__.TrustChecks.test_missing_and_malformed_trust_inputs) ... ok
test_missing_extra_duplicate_slots_and_wrong_phase (__main__.TrustChecks.test_missing_extra_duplicate_slots_and_wrong_phase) ... ok
test_receipt_bytes_not_canonical_digest (__main__.TrustChecks.test_receipt_bytes_not_canonical_digest) ... ok
test_swapped_phases_with_distinct_pinned_observations (__main__.TrustChecks.test_swapped_phases_with_distinct_pinned_observations) ... ok
test_valid_two_and_four_slot_bundles (__main__.TrustChecks.test_valid_two_and_four_slot_bundles) ... ok
test_wrong_expected_identifiers (__main__.TrustChecks.test_wrong_expected_identifiers) ... ok
test_wrong_inner_observation_pin_or_binding_seal (__main__.TrustChecks.test_wrong_inner_observation_pin_or_binding_seal) ... ok
test_all_selector_bundle_receipt_version_pairings (__main__.VersionChecks.test_all_selector_bundle_receipt_version_pairings) ... ok
test_bundle_policy_schema_strict (__main__.VersionChecks.test_bundle_policy_schema_strict) ... ok
test_explicit_three_imports_only_v3 (__main__.VersionChecks.test_explicit_three_imports_only_v3) ... ok
test_invalid_versions_never_import (__main__.VersionChecks.test_invalid_versions_never_import) ... ok
test_omission_is_v2_without_v3_import (__main__.VersionChecks.test_omission_is_v2_without_v3_import) ... ok
test_v3_pure_operation_requires_explicit_opt_in (__main__.VersionChecks.test_v3_pure_operation_requires_explicit_opt_in) ... ok

----------------------------------------------------------------------
Ran 31 tests in 1.169s

OK
OS-process audit events: 0
```
