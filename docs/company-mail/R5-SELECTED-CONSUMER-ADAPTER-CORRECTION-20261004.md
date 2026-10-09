# R5 selected consumer adapter correction — slice 1 only

Exact tested source commit: `07d09c5ec49305c0ec91a0309a14e6ac793a28cc`.
Branch: `candidate/r5-consumer-adapter-corrected-20261004`.
A subsequent report-only commit carries identical implementation bytes; the final
exact head is provided in the handoff. Interface remains **not frozen** pending
parent-owned independent exact-head review.

Frozen implementation base: `412f875985854527f5e7d74040f18954c3a352bf`.
Rejected candidate preserved: `3ff6db72b579419a7145d2bc2a3b888c111c9b62`,
branch `candidate/r5-consumer-adapter-20261004`; its report is unchanged.
Independent evidence, fetched and verified by exact remote commit:
[`054d47924e041117afb88f4d8e74217a89bd2464`](https://github.com/jyqj/tabmail/commit/054d47924e041117afb88f4d8e74217a89bd2464),
branch `review/r5-independent-20261004`,
[REJECT report](https://github.com/jyqj/tabmail/blob/054d47924e041117afb88f4d8e74217a89bd2464/docs/company-mail/R5-ADAPTER-INDEPENDENT-REVIEW-20261004.md).
The design remains `42f49538f7bceb6eb8e698d716c1418f85343413` and
`docs/company-mail/R5-V3-CONSUMER-MIGRATION-DESIGN-20261004.md`.

## Correction

P1: the pure gate now requires exactly `r5_immutable_archive_boundary_v1` and
the unchanged boundary's registry identity
`8e04081e1f2b9d9b82b854954b56a1ac71047646f8768d1b7a7ff89af223bb15`.
A new independently supplied synthetic byte pin authenticates bytes; it does not
make incompatible policies, malformed fields or false qualification valid.

P2: complete nested output record schemas are enforced before private helper seal
verification. Checks include required/optional keys, precise leaf types, fixed
qualification semantics, local/main MVS identity and exactly two normalized
replacement records, selected input hashes/fields linked to source and package
records, root/explicit/main-selected package coverage, complete variant-directory/
file linkage, and typed/classified generated, external, toolchain and native input
records. Classified inputs retain `unknown`; global qualification stays `blocked`
with all original unknown domains. Valid cache-based generated testmain absolute
inputs remain supported through their separate classification record.

Nested source records are also fully checked: local replacement records; module
resolution/declaration/requirement maps; two local module metadata records and
source-file hashes; upstream filename arrays; embed declaration/input maps;
excluded metadata budgets/semantics; and unchanged historical version-test contract.
These are pure checks of supplied metadata and linkage, not a source traversal or
registry/closure refresh. Historical archive content or external/cache/compiler
bytes are not freshly captured or qualified. Receipt/source boundary statements
must retain the reviewed helper's fixed semantics. Full Go-env output remains the
helper's string map with required ABI/flags; raw stdout is not reconstructed.

The public interface is unchanged. Exact integer 3 opt-in, omitted-v2 lazy helper
selection, strict mixed-version rejection, explicit independent trust arguments,
all twelve command descriptors, exact slot associations, byte/observation/binding
pin distinctions, duplicate/malformed JSON rejection, and helper projection are
preserved. Hydration and retained raw stdout exclusions are unchanged; retained
command stderr hash/length still bind. Neither verification nor projection calls a
producer, capture, validation, retention sink, candidate file reader or environment
lookup. Existing v1/v2 paths and helper error-retention contracts remain untouched.

## Preserved independent controls: baseline red / candidate green

The reviewer program's exact bytes have SHA256
`9c4743698351ea2f6f1f51381a680e49011d750e857dfec3882dfa91d4d86b53`.
They were extracted read-only from the remote-verified evidence commit into
`/tmp/r5-independent-controls.py`; baseline adapter bytes were extracted from
`3ff6` into `/tmp/r5-rejected-consumer.py`. The new non-discovered check program
includes `--replay-review` mode that verifies this evidence-source hash and
preserves **all 114 control names, order, mutation functions, expected outcomes,
selector/projection assertions and forbidden-action mocks**.

Explicit fixture adaptation: the original reviewer positive fixture intentionally
had empty MVS/qualification/module-resolution containers and incomplete coverage.
Those are now invalid complete schemas. Replay therefore replaces only its
`receipt(context)` builder with the documented complete synthetic fixture in the
new author checks. The replacement is used identically on baseline and candidate.
The reviewer still computes and publishes source/binding/observation seals and
pins with its independent publisher. No author test/fixture module is imported;
two fixture functions are extracted as explicit scaffolding. The only additional
review-program adjustments are the obsolete final expected-gap-count assertion
(6 for baseline, 0 for candidate) and display of the actual adapter SHA256 rather
than the rejected head's hardcoded label. Original evidence files are untouched.
This is an author replay of independently specified controls, **not a new
independent approval**.

Baseline: **108/114 expected results, six unexpected acceptances**.
Corrected candidate: **114/114 expected results, zero unexpected acceptances**.
Both programs retain the OS-process guard installed before adapter/helper imports;
**zero OS-process events** occurred. Fixed-original-pin counterfeit rejections,
descriptor/version checks and all retained stderr-binding assertions still pass.

The six exact independently specified gaps now reject after resealing and newly
explicit pinning:

* `schema-edge-archive-policy`
* `schema-edge-archive-registry`
* `schema-edge-nested-root-mvs-bool`
* `schema-edge-selected-local-schema`
* `schema-edge-qualification-false-claim`
* `schema-edge-production-coverage-types`

Commands:

```text
PYTHONDONTWRITEBYTECODE=1 python3 scripts/tests/r5_selected_binding_consumer_checks.py
PYTHONDONTWRITEBYTECODE=1 python3 scripts/tests/r5_selected_binding_consumer_checks.py --replay-review /tmp/r5-independent-controls.py --adapter /tmp/r5-rejected-consumer.py --expected-gaps 6
PYTHONDONTWRITEBYTECODE=1 python3 scripts/tests/r5_selected_binding_consumer_checks.py --replay-review /tmp/r5-independent-controls.py --expected-gaps 0
```

Author checks: **35 test methods passed** with nested-field/type/linkage subcases;
zero OS-process events. The original 31 method names are retained, with complete
positive fixtures and four added nested-schema methods. The generated-record
malformed-path control initially exposed a TypeError in scaffolding linkage;
validation now fails with ValueError before constructing that path. Final source
and all final results below include that correction.

## Exact SHA256 identities

| File or result | SHA256 |
| --- | --- |
| scripts/r5_selected_binding_consumer.py | eb444c6f0a2112ed473c7c89b71739cb0ba5ac62346efda8cbb20323dec49a89 |
| scripts/tests/r5_selected_binding_consumer_checks.py | 82372ed6d398a80dac1864b8af95b5a47586144226bed6ec5e26a462fe3a98c7 |
| /tmp/r5-corrected-author.log | 2547700ba6c62def28b6fcfaac329df27a5fb7e89b3e5350f1576b70c5ea3241 |
| /tmp/r5-replay-baseline.log | 3f28e63a6c6fb1a29104eb922310f01be8f0aa4a8fdc9fb942093ab5e26606db |
| /tmp/r5-replay-corrected.log | 16730dd1f5f325f700ebc14a3ae2789e1792445dbb0ab2e54ef92ba694312477 |

## All 114 preserved control outcomes

Expected outcome is unchanged between baseline and candidate.

| Control | Expected | Rejected baseline actual | Corrected candidate actual |
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
| schema-edge-archive-policy | reject | accept | reject |
| schema-edge-archive-registry | reject | accept | reject |
| schema-edge-nested-root-mvs-bool | reject | accept | reject |
| schema-edge-selected-local-schema | reject | accept | reject |
| schema-edge-qualification-false-claim | reject | accept | reject |
| schema-edge-production-coverage-types | reject | accept | reject |

## Author result log

```text
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
Ran 35 tests in 4.096s

OK
OS-process audit events: 0
```

## Scope and remaining gate

Relative to rejected candidate, the write set is only the adapter, its explicitly
non-discovered checks, and this new report. Rejected report and all original 2,208
tracked base paths remain byte-identical. Independent review evidence remains on
its separate branch and is not modified or merged. Applicable repository/workspace
instructions are unchanged: no root/scripts/test AGENTS.md or `.agents/skills`,
empty workspace `.agents`/`.codex`; `web/AGENTS.md` was previously read and no web
edits apply.

No existing helper/schema/test edits, capture/publisher CLI, runtime/preparation/
Go bridge adoption, discovery/registry/closure refresh, formal suites, PG/mail,
services, old F52 acceptance, PR, merge, deploy or force push. Central **10/171** is
unchanged. Parent must independently review the final exact head and its complete
positive schema fixture before freezing the interface or assigning other slices.
