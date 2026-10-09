# R5 slice-1 classifier contract correction — candidate only

Exact tested implementation head: `7377bdd0ab1ec80c013cd5f8a67d56169faab398`.
Branch: `candidate/r5-consumer-classifier-contract-20261004`.
The final head adds only this report and retains these exact tested source bytes;
its SHA is supplied in the handoff. Interface remains **not frozen**.

Frozen base: `412f875985854527f5e7d74040f18954c3a352bf`.
Design: `42f49538f7bceb6eb8e698d716c1418f85343413`,
`docs/company-mail/R5-V3-CONSUMER-MIGRATION-DESIGN-20261004.md`.
Preserved rejected candidates: `3ff6db72b579419a7145d2bc2a3b888c111c9b62`
and `116d44a2a9c0b5d7ac6a09187bf3fae22c1a1e50`; their branches and reports
are untouched.

Second review evidence fetched and verified at exact remote commit
[`d0811178bc68760280c44a68cfd7f7bc0790c9b9`](https://github.com/jyqj/tabmail/commit/d0811178bc68760280c44a68cfd7f7bc0790c9b9):
[delta REJECT report](https://github.com/jyqj/tabmail/blob/d0811178bc68760280c44a68cfd7f7bc0790c9b9/docs/company-mail/R5-ADAPTER-DELTA-INDEPENDENT-REVIEW-20261004.md).
Original review/evidence remains immutable at `054d47924e041117afb88f4d8e74217a89bd2464`.

## Preserved classifier contract

The new gate reuses the **unchanged** `v3.classify` (the preserved v2 classifier
function) rather than another local/native classifier implementation. A private
`types.FunctionType` uses its exact code object and a copy of its globals. Only
`base_markers` is replaced in that private dictionary with an in-memory lookup of
the already byte-authenticated boundary's marker labels. No helper global, helper
source, schema or public API is patched. The inspected classifier uses lexical
Path operations; base_markers was its sole filesystem-reading dependency. Isolation
controls prove the original global remains unchanged and its reader is never called
by adapter verification. No producer, runtime import/action, traversal or candidate
file read is added. Existing lazy v3 import still occurs only after explicit selection.

Normalized local package records provide the ordered package/directory/file fields.
Minimal metadata rows reconstruct only the fields the classifier actually uses;
main/fork module identities come from the already checked source contract. Its
returned local field map, complete ordered **local native occurrences**, and
complete ordered generated testmain occurrences must exactly match the receipt.
Missing, extra, duplicate, reordered or wrongly package-attributed native records
reject, including multiple packages selecting the same path and field. This retains
occurrence multiplicity rather than reducing it to a path/field set.

Every first-four-Go-field input (`GoFiles`, `CgoFiles`, `TestGoFiles`, `XTestGoFiles`)
must end in `.go` in the represented ordinary local, external and toolchain domains.
The reviewed exception remains absolute cache-based `.test` **GoFiles** with a
separately linked generated record; the preserved classifier checks cache containment
and the `-d` suffix. Arbitrary local non-Go files or other first-four fields cannot
borrow that generated exception. Non-Go bytes remain valid as declared, bound embeds
and as native input fields; no filename deny list is introduced.

Typed nonlocal records are replayed through the same classifier using their normalized
path/domain/module fields. Native/non-native routing and returned classification/
qualification must match exactly. External/toolchain/native records cannot claim a
represented local package. Observed GOCACHE/GOMODCACHE values must match the clean
requested environment whenever present, and are required for their represented cache
domains. Partial synthetic fixtures without cache-domain records need no cache
observation; they cannot gain authority for that absent domain. GOROOT is required
when checking toolchain records. All these paths are lexical metadata, not trusted
executable/file-reading capabilities or new independent pins.

The receipt's package_records count must cover every represented local row plus the
minimum distinct nonlocal package identities. Generated records must be represented
by local testmain rows. This is a **lower bound**, not an invented exact total: raw
metadata rows without selected file fields are not represented by this normalized
receipt and cannot be recovered from hashes. Similarly, original external Module.Dir,
Name/Standard/full PackagePublic stdout, external bytes and compiler bodies are not
re-attested by synthetic reconstructed rows. Those remain producer-bound metadata
and unknown qualification domains. No runtime/source-capture claim is made.

The independent pin-first gate, caller-supplied expected identities, strict integer
version selection, default v2, all twelve descriptors and exact slots remain unchanged.
Projection is still solely the original helper projection; only its original hydration
and retained raw-stdout exclusions apply. Retained stderr hash/length remain bound.
Existing v1/v2 paths and primary/secondary error-retention contracts remain untouched.

## Complete independent control results

The two delta-review source files are extracted read-only from `d0811178` into
`/tmp/r5-delta-run/docs/company-mail/evidence/R5-ADAPTER-DELTA-INDEPENDENT-20261004`.
Their exact hashes are checked before execution:

* independent_replay.py: `d8a2151eba65efe1d6cfcd302ae36c6f7102526338a6893b6103eaa781f7d869`
* new_edge_checks.py: `9f284ec994c9a9e164aef0103ae546bae79b24a7ab6be70c34dea4b1ae3963d9`

The `--replay-delta` wrapper executes **both independent programs byte-for-byte**,
with their independently completed fixtures, publisher, all assertions, forbidden-
action mocks, classifier parity checks and guards. It adjusts neither fixtures nor
case definitions nor expectations. Suppressed historical source labels are replaced
only in the wrapper's result envelope by the SHA256 of the adapter actually loaded.
A temporary scripts symlink provides the original repository-relative import layout.
No evidence file or existing review is changed. This is an author rerun of independent
controls, not independent approval of this new candidate.

Against `116d44a2`: all 114 prior controls pass; delta controls have 13 expected
rejections and **five unexpected acceptances** (127/132 total expected results).
Against this candidate: **114/114 prior + 18/18 delta = 132/132 expected results**,
zero unexpected acceptances. Both runs have **zero OS-process events**. The independent
classifier parity assertions for both false-Go failures and exact native attribution
remain active and pass. A separate replay against `3ff6` still reproduces the original
six schema gaps across all 114 original controls, preserving prior baseline-red evidence.

The five exact newly fixed gaps:

* `non-go-local-GoFiles`
* `missing-local-native-classification`
* `foreign-package-native-classification`
* `orphan-generated-testmain`
* `non-go-toolchain-GoFiles`

Commands (Git extraction and filesystem bookkeeping occur outside the pure guards):

```text
PYTHONDONTWRITEBYTECODE=1 python3 scripts/tests/r5_selected_binding_consumer_checks.py
PYTHONDONTWRITEBYTECODE=1 python3 scripts/tests/r5_selected_binding_consumer_checks.py --replay-delta /tmp/r5-delta-run/docs/company-mail/evidence/R5-ADAPTER-DELTA-INDEPENDENT-20261004 --adapter /tmp/r5-second-rejected-consumer.py --expected-gaps 5
PYTHONDONTWRITEBYTECODE=1 python3 scripts/tests/r5_selected_binding_consumer_checks.py --replay-delta /tmp/r5-delta-run/docs/company-mail/evidence/R5-ADAPTER-DELTA-INDEPENDENT-20261004 --expected-gaps 0
PYTHONDONTWRITEBYTECODE=1 python3 scripts/tests/r5_selected_binding_consumer_checks.py --replay-review /tmp/r5-independent-controls.py --adapter /tmp/r5-rejected-consumer.py --expected-gaps 6
```

Author checks: **41 methods passed**, zero OS-process events. The original 35 method
names are retained; six new contract methods exercise all four Go fields in three
domains, every native field's complete attributed records, shared-path occurrence
order/multiplicity, valid bound non-Go embeds, generated cache success plus malformed
linkages, classifier isolation, cache observation linkage and represented package
count bounds. Positive classified fixtures now include observed cache/GOROOT metadata
and an adequate total package count, consistent with their additional records.
Every guarded program installs its OS-process guard before helper imports; no program
launches another OS process. Capture/validate/Retention and candidate readers remain
forbidden by controls. Import-time preserved helper/schema reads precede read mocks.

## Exact SHA256 source and result identities

| File/result | SHA256 |
| --- | --- |
| scripts/r5_selected_binding_consumer.py | 6d87b0b172a25871fdc1c766cf4e135c1ebf075d7886514faf2259f9cde4c6e3 |
| scripts/tests/r5_selected_binding_consumer_checks.py | 71db298f70aded89e13eae597ce5dc9315f49bebc8b6978fa8228f9db515e6a6 |
| /tmp/r5-classifier-author.log | 498ecb3c9aa160998c8696ce70ed98acc82a1714d3052250035a0bc7ef523382 |
| /tmp/r5-classifier-baseline.json | 8134388a1d263ffae2388424bfcba97c9ae0067e66ddc7b376d3b9150b13ac6e |
| /tmp/r5-classifier-candidate.json | 3b33de32f7f7720e24c98b394c46e10dc401be79c95248367ef357d6dd4fd7fd |
| /tmp/r5-classifier-first-baseline.log | 3f28e63a6c6fb1a29104eb922310f01be8f0aa4a8fdc9fb942093ab5e26606db |

## All 114 preserved prior controls

| Control | Expected | 116d44a2 actual | New candidate actual |
| --- | --- | --- | --- |
| controlled-two-context-four-slot-positive | accept | accept | accept |
| descriptor-0-role | reject | reject | reject |
| descriptor-0-argv | reject | reject | reject |
| descriptor-0-exit | reject | reject | reject |
| descriptor-0-binding_stdout_domain | reject | reject | reject |
| descriptor-0-stderr_bytes | reject | reject | reject |
| counterfeit-resealed-fixedpin-0 | reject | reject | reject |
| descriptor-1-role | reject | reject | reject |
| descriptor-1-argv | reject | reject | reject |
| descriptor-1-exit | reject | reject | reject |
| descriptor-1-binding_stdout_domain | reject | reject | reject |
| descriptor-1-stderr_bytes | reject | reject | reject |
| counterfeit-resealed-fixedpin-1 | reject | reject | reject |
| descriptor-2-role | reject | reject | reject |
| descriptor-2-argv | reject | reject | reject |
| descriptor-2-exit | reject | reject | reject |
| descriptor-2-binding_stdout_domain | reject | reject | reject |
| descriptor-2-stderr_bytes | reject | reject | reject |
| counterfeit-resealed-fixedpin-2 | reject | reject | reject |
| descriptor-3-role | reject | reject | reject |
| descriptor-3-argv | reject | reject | reject |
| descriptor-3-exit | reject | reject | reject |
| descriptor-3-binding_stdout_domain | reject | reject | reject |
| descriptor-3-stderr_bytes | reject | reject | reject |
| counterfeit-resealed-fixedpin-3 | reject | reject | reject |
| descriptor-4-role | reject | reject | reject |
| descriptor-4-argv | reject | reject | reject |
| descriptor-4-exit | reject | reject | reject |
| descriptor-4-binding_stdout_domain | reject | reject | reject |
| descriptor-4-stderr_bytes | reject | reject | reject |
| counterfeit-resealed-fixedpin-4 | reject | reject | reject |
| descriptor-5-role | reject | reject | reject |
| descriptor-5-argv | reject | reject | reject |
| descriptor-5-exit | reject | reject | reject |
| descriptor-5-binding_stdout_domain | reject | reject | reject |
| descriptor-5-stderr_bytes | reject | reject | reject |
| counterfeit-resealed-fixedpin-5 | reject | reject | reject |
| descriptor-6-role | reject | reject | reject |
| descriptor-6-argv | reject | reject | reject |
| descriptor-6-exit | reject | reject | reject |
| descriptor-6-binding_stdout_domain | reject | reject | reject |
| descriptor-6-stderr_bytes | reject | reject | reject |
| counterfeit-resealed-fixedpin-6 | reject | reject | reject |
| descriptor-7-role | reject | reject | reject |
| descriptor-7-argv | reject | reject | reject |
| descriptor-7-exit | reject | reject | reject |
| descriptor-7-binding_stdout_domain | reject | reject | reject |
| descriptor-7-stderr_bytes | reject | reject | reject |
| counterfeit-resealed-fixedpin-7 | reject | reject | reject |
| descriptor-8-role | reject | reject | reject |
| descriptor-8-argv | reject | reject | reject |
| descriptor-8-exit | reject | reject | reject |
| descriptor-8-binding_stdout_domain | reject | reject | reject |
| descriptor-8-stderr_bytes | reject | reject | reject |
| counterfeit-resealed-fixedpin-8 | reject | reject | reject |
| descriptor-9-role | reject | reject | reject |
| descriptor-9-argv | reject | reject | reject |
| descriptor-9-exit | reject | reject | reject |
| descriptor-9-binding_stdout_domain | reject | reject | reject |
| descriptor-9-stderr_bytes | reject | reject | reject |
| counterfeit-resealed-fixedpin-9 | reject | reject | reject |
| descriptor-10-role | reject | reject | reject |
| descriptor-10-argv | reject | reject | reject |
| descriptor-10-exit | reject | reject | reject |
| descriptor-10-binding_stdout_domain | reject | reject | reject |
| descriptor-10-stderr_bytes | reject | reject | reject |
| counterfeit-resealed-fixedpin-10 | reject | reject | reject |
| descriptor-11-role | reject | reject | reject |
| descriptor-11-argv | reject | reject | reject |
| descriptor-11-exit | reject | reject | reject |
| descriptor-11-binding_stdout_domain | reject | reject | reject |
| descriptor-11-stderr_bytes | reject | reject | reject |
| counterfeit-resealed-fixedpin-11 | reject | reject | reject |
| expected-run_id | reject | reject | reject |
| expected-source_commit | reject | reject | reject |
| expected-source_root | reject | reject | reject |
| expected-producer | reject | reject | reject |
| expected-allowed_slots | reject | reject | reject |
| expected-selected_binding_version | reject | reject | reject |
| expected-selected_binding_version | reject | reject | reject |
| bundle-schema_version | reject | reject | reject |
| bundle-policy | reject | reject | reject |
| bundle-selected_binding_version | reject | reject | reject |
| duplicate-bundle | reject | reject | reject |
| missing-descriptor-0 | reject | reject | reject |
| missing-descriptor-1 | reject | reject | reject |
| missing-descriptor-2 | reject | reject | reject |
| missing-descriptor-3 | reject | reject | reject |
| missing-descriptor-4 | reject | reject | reject |
| missing-descriptor-5 | reject | reject | reject |
| missing-descriptor-6 | reject | reject | reject |
| missing-descriptor-7 | reject | reject | reject |
| missing-descriptor-8 | reject | reject | reject |
| missing-descriptor-9 | reject | reject | reject |
| missing-descriptor-10 | reject | reject | reject |
| missing-descriptor-11 | reject | reject | reject |
| malformed-bundle-b'{' | reject | reject | reject |
| malformed-bundle-b'[]' | reject | reject | reject |
| malformed-bundle-b'\xff' | reject | reject | reject |
| malformed-bundle-b'{"schema_version":1.0}' | reject | reject | reject |
| malformed-bundle-b'{"schema_version":NaN}' | reject | reject | reject |
| receipt-context-slot-duplicate-key | reject | reject | reject |
| receipt-context-slot-float | reject | reject | reject |
| receipt-context-slot-wrong-context | reject | reject | reject |
| receipt-context-slot-wrong-root | reject | reject | reject |
| receipt-context-slot-missing-slot | reject | reject | reject |
| receipt-context-slot-duplicate-path | reject | reject | reject |
| receipt-context-slot-swap-phases | reject | reject | reject |
| schema-edge-archive-policy | reject | reject | reject |
| schema-edge-archive-registry | reject | reject | reject |
| schema-edge-nested-root-mvs-bool | reject | reject | reject |
| schema-edge-selected-local-schema | reject | reject | reject |
| schema-edge-qualification-false-claim | reject | reject | reject |
| schema-edge-production-coverage-types | reject | reject | reject |

## All 18 unchanged delta controls

| Control | Expected | 116d44a2 actual | New candidate actual |
| --- | --- | --- | --- |
| non-go-local-GoFiles | reject | accept | reject |
| missing-local-native-classification | reject | accept | reject |
| foreign-package-native-classification | reject | accept | reject |
| orphan-generated-testmain | reject | accept | reject |
| non-go-toolchain-GoFiles | reject | accept | reject |
| selected-path-../outside.go | reject | reject | reject |
| selected-path-/outside.go | reject | reject | reject |
| selected-path-internal/../outside.go | reject | reject | reject |
| selected-input-type-sha256 | reject | reject | reject |
| selected-input-type-fields | reject | reject | reject |
| extra-qualification-field | reject | reject | reject |
| missing-qualification-field | reject | reject | reject |
| main-MVS-boolean-type | reject | reject | reject |
| third-MVS-replacement | reject | reject | reject |
| variant-extra-field | reject | reject | reject |
| variant-context-bool-type | reject | reject | reject |
| variant-missing-input | reject | reject | reject |
| resolution-boolean-type | reject | reject | reject |

## Exact author log

```text
test_all_local_native_fields_complete_package_attributed_occurrences (__main__.ClassifierContractChecks.test_all_local_native_fields_complete_package_attributed_occurrences) ... ok
test_classifier_isolation_and_nonlocal_package_count_lower_bound (__main__.ClassifierContractChecks.test_classifier_isolation_and_nonlocal_package_count_lower_bound) ... ok
test_first_four_go_fields_in_every_represented_domain (__main__.ClassifierContractChecks.test_first_four_go_fields_in_every_represented_domain) ... ok
test_generated_cache_exception_complete_linkage_and_wrong_field_rejection (__main__.ClassifierContractChecks.test_generated_cache_exception_complete_linkage_and_wrong_field_rejection) ... ok
test_native_multiplicity_order_and_shared_paths_across_packages (__main__.ClassifierContractChecks.test_native_multiplicity_order_and_shared_paths_across_packages) ... ok
test_non_go_bytes_remain_permitted_for_bound_embeds (__main__.ClassifierContractChecks.test_non_go_bytes_remain_permitted_for_bound_embeds) ... ok
test_all_nested_leaf_type_errors (__main__.CompleteNestedSchemaChecks.test_all_nested_leaf_type_errors) ... ok
test_classified_records_positive_and_every_field_type (__main__.CompleteNestedSchemaChecks.test_classified_records_positive_and_every_field_type) ... ok
test_each_required_nested_field_and_extra_key (__main__.CompleteNestedSchemaChecks.test_each_required_nested_field_and_extra_key) ... ok
test_nested_linkage_missing_or_extra_records (__main__.CompleteNestedSchemaChecks.test_nested_linkage_missing_or_extra_records) ... ok
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
Ran 41 tests in 4.710s

OK
OS-process audit events: 0
```

## Preservation and remaining gate

Only adapter/checks change relative to `116d44a2`, with this one newly scoped report
added. Both old reports, all 2,208 pre-existing frozen-base paths and all existing
helpers/schemas/classifier bytes remain unchanged. All review evidence is preserved
on its existing independent branches/commits. Applicable workspace/repository
instructions are unchanged: no root/scripts/tests AGENTS.md or `.agents/skills`,
empty workspace `.agents`/`.codex`; previously read web/AGENTS.md has no affected edits.

No capture/publisher CLI, producer execution, preparation/runtime adoption, Go bridge,
registry/closure/discovery refresh, existing test edits, formal suite, PG/mail/service,
old F52 acceptance, PR, merge, deployment or force push. Central **10/171** remains
unchanged. Parent must independently review the final exact candidate head and this
classifier isolation/reconstruction boundary before freezing the interface or assigning
later slices. No independent acceptance is claimed here.
