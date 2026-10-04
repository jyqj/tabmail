# R5 slice-1 generated-input provenance correction — candidate only

Exact tested source: `8f1e752beadf35587be00fed6beac98cf6859ae6`.
Branch: `candidate/r5-consumer-generated-provenance-20261004`.
Final head adds this report only; source bytes are identical to the tested source.
Its exact SHA is supplied in the handoff. Interface remains **unfrozen** pending
parent-owned independent exact-head review.

Frozen base `412f875985854527f5e7d74040f18954c3a352bf` and approved design
`42f49538f7bceb6eb8e698d716c1418f85343413` are unchanged.
All prior candidates are preserved, including rejected `0c9adb17559e347ed99b3661d789b80ba59762aa`,
`116d44a2a9c0b5d7ac6a09187bf3fae22c1a1e50` and
`3ff6db72b579419a7145d2bc2a3b888c111c9b62`; their reports/branches are untouched.

Read and remote-verified evidence:
[`bcae4d80ccd9d4f928676e63f977c015727a3daf`](https://github.com/jyqj/tabmail/commit/bcae4d80ccd9d4f928676e63f977c015727a3daf),
[exact independent REJECT report](https://github.com/jyqj/tabmail/blob/bcae4d80ccd9d4f928676e63f977c015727a3daf/docs/company-mail/R5-CLASSIFIER-INDEPENDENT-REVIEW-20261004.md).
Earlier review evidence remains immutable at `054d47924e041117afb88f4d8e74217a89bd2464`
and `d0811178bc68760280c44a68cfd7f7bc0790c9b9`.

## Bounded correction

The previous guard mistook any absolute non-Go GoFiles path in a `.test` package
for a generated input, including source-owned `.txt` bytes. It also synthesized
Name=main from the suffix alone, allowing that file to take the existing classifier's
local path branch without its cache checks.

The adapter now proves the **specific input occurrence** before applying the
exception or assigning a generated Name. It must be GoFiles in a `.test` package,
absolute, lexically outside the source root, inside the declared GOCACHE, end in
`-d`, and match a separately represented generated record's package, field and
cache-relative path. These are metadata checks only; no filesystem lookup occurs.
The existing later observed/requested-cache equality, complete generated occurrence
comparison and isolated preserved classifier checks remain active.

All ordinary first-four-Go-field inputs must end in `.go`; source-owned inputs
cannot borrow the exception even when a supplied cache path lies within source,
or when a generated label/suffix is supplied. Ordinary `.go` source files remain
valid for package names ending in `.test`: no package-name deny list is introduced.
Native and declared embed inputs retain their previous semantics and qualification.

No existing classifier/helper/schema code, global state, projection or diagnostic
exclusion changes. Pin-first verification, explicit independently supplied expected
identities, integer-3 opt-in, omitted-v2 default, lazy import, all twelve descriptors,
slots/phase checks and old v1/v2/error-retention contracts remain unchanged. No
producer, runtime, preparation, Go bridge, publisher CLI or closure wiring is added.

## Exact independent controls and baseline red / candidate green

The independent adversarial program is extracted byte-for-byte from `bcae4d80`
to `/tmp/r5-generated-adversarial.py`, SHA256
`165355274becac87f3e741aa1e26e1f61d911c73462a5807643cfc08e4c34db8`.
It executes its own independently completed fixture from the exact preserved
independent_replay.py (SHA256
`d8a2151eba65efe1d6cfcd302ae36c6f7102526338a6893b6103eaa781f7d869`),
under its original `/tmp/r5-third-run/...` layout. Prior independent edge source
remains `9f284ec994c9a9e164aef0103ae546bae79b24a7ab6be70c34dea4b1ae3963d9`.
Temporary scripts symlink selects this checkout; extraction/layout bookkeeping is
outside pure guards.

New `--replay-generated` mode verifies the exact adversarial-source hash and runs
its bytes **unchanged**: all 43 names/order/expectations, positive fixtures, isolation
assertions, forbidden-action mocks, three additional fixed-pin controls and process
guards remain active. The original final assertion fails on the baseline's one gap;
the wrapper records that failure and verifies it is exactly the final assertion at
line 127. It does not rewrite or weaken any independent assertion. The candidate
executes the same program with that assertion passing. Wrapper result envelope
records the actual adapter SHA256; the original program's historical display label
is preserved inside its captured result.

* Rejected `0c9adb`: 43 controls, 42 expected results, one unexpected acceptance:
  `generated-exception-borrowed-local-non-Go`.
* New candidate: **43/43 expected results**, zero unexpected acceptances.
* Preserved independent replay: **114/114 prior + 18/18 delta** expected results;
  cases/expectations/fixtures unchanged.
* Total independent controls preserved: **175/175 expected results**, plus the
  adversarial program's three additional fixed-original-pin rejections.
* All programs: **zero OS-process events**. Independent helper-global and imported-
  module identity isolation, filesystem/env/capture/validation/retention prohibitions
  remain active and pass. No program launches another process.

Author checks: **43 methods pass** (the prior 41 retained plus two new provenance
methods). New cross-field cases cover ordinary/test-suffix packages; all four Go
fields; relative and absolute source-owned `.txt` and `-d` inputs; legitimate
ordinary source `.go` paths; legitimate separately linked cache testmain files;
and a purported generated file in a cache beneath source. Positive controls prove
that package suffixes and generated records are not rejected indiscriminately.

Commands:

```text
PYTHONDONTWRITEBYTECODE=1 python3 scripts/tests/r5_selected_binding_consumer_checks.py
PYTHONDONTWRITEBYTECODE=1 python3 scripts/tests/r5_selected_binding_consumer_checks.py --replay-generated /tmp/r5-generated-adversarial.py --adapter /tmp/r5-third-rejected-consumer.py --expected-gaps 1
PYTHONDONTWRITEBYTECODE=1 python3 scripts/tests/r5_selected_binding_consumer_checks.py --replay-generated /tmp/r5-generated-adversarial.py --expected-gaps 0
PYTHONDONTWRITEBYTECODE=1 python3 scripts/tests/r5_selected_binding_consumer_checks.py --replay-delta /tmp/r5-third-run/docs/company-mail/evidence/R5-ADAPTER-DELTA-INDEPENDENT-20261004 --expected-gaps 0
```

These are author reruns of independently specified controls, not a new independent
approval or runtime qualification. Raw metadata streams and live source/cache/
compiler bytes are not reconstructed from hashes; all prior scope limits remain.

## Exact SHA256 source/results

| File/result | SHA256 |
| --- | --- |
| scripts/r5_selected_binding_consumer.py | 46a43df58a17ea03966b88fe07275e60344a055960aaabfcf405a7d5eb6caa21 |
| scripts/tests/r5_selected_binding_consumer_checks.py | 7b6f66b1234e9be986377a222a49d24b6442927797645840d2aee690853b8f08 |
| /tmp/r5-generated-author.log | b7dcfffbba03d78081f951ca99b9da92dfbdfe329fdcbb1b13a23868c6f4f1a3 |
| /tmp/r5-generated-baseline.json | 049d6e0a11b83eb4acd85968e6c398d30e10751aa67105e13351013bc3b6c323 |
| /tmp/r5-generated-candidate.json | 34e9474d9ba9aed45cb0e05e72d21e5885495718d6368a3324a7b617a2bdfef6 |
| /tmp/r5-generated-prior.json | 493d2bb36821b75e4def0020402e3d09127473494cc4c6dd28219f1c36f6c05c |

## All 114 preserved prior controls

| Control | Expected | Candidate actual |
| --- | --- | --- |
| controlled-two-context-four-slot-positive | accept | accept |
| descriptor-0-role | reject | reject |
| descriptor-0-argv | reject | reject |
| descriptor-0-exit | reject | reject |
| descriptor-0-binding_stdout_domain | reject | reject |
| descriptor-0-stderr_bytes | reject | reject |
| counterfeit-resealed-fixedpin-0 | reject | reject |
| descriptor-1-role | reject | reject |
| descriptor-1-argv | reject | reject |
| descriptor-1-exit | reject | reject |
| descriptor-1-binding_stdout_domain | reject | reject |
| descriptor-1-stderr_bytes | reject | reject |
| counterfeit-resealed-fixedpin-1 | reject | reject |
| descriptor-2-role | reject | reject |
| descriptor-2-argv | reject | reject |
| descriptor-2-exit | reject | reject |
| descriptor-2-binding_stdout_domain | reject | reject |
| descriptor-2-stderr_bytes | reject | reject |
| counterfeit-resealed-fixedpin-2 | reject | reject |
| descriptor-3-role | reject | reject |
| descriptor-3-argv | reject | reject |
| descriptor-3-exit | reject | reject |
| descriptor-3-binding_stdout_domain | reject | reject |
| descriptor-3-stderr_bytes | reject | reject |
| counterfeit-resealed-fixedpin-3 | reject | reject |
| descriptor-4-role | reject | reject |
| descriptor-4-argv | reject | reject |
| descriptor-4-exit | reject | reject |
| descriptor-4-binding_stdout_domain | reject | reject |
| descriptor-4-stderr_bytes | reject | reject |
| counterfeit-resealed-fixedpin-4 | reject | reject |
| descriptor-5-role | reject | reject |
| descriptor-5-argv | reject | reject |
| descriptor-5-exit | reject | reject |
| descriptor-5-binding_stdout_domain | reject | reject |
| descriptor-5-stderr_bytes | reject | reject |
| counterfeit-resealed-fixedpin-5 | reject | reject |
| descriptor-6-role | reject | reject |
| descriptor-6-argv | reject | reject |
| descriptor-6-exit | reject | reject |
| descriptor-6-binding_stdout_domain | reject | reject |
| descriptor-6-stderr_bytes | reject | reject |
| counterfeit-resealed-fixedpin-6 | reject | reject |
| descriptor-7-role | reject | reject |
| descriptor-7-argv | reject | reject |
| descriptor-7-exit | reject | reject |
| descriptor-7-binding_stdout_domain | reject | reject |
| descriptor-7-stderr_bytes | reject | reject |
| counterfeit-resealed-fixedpin-7 | reject | reject |
| descriptor-8-role | reject | reject |
| descriptor-8-argv | reject | reject |
| descriptor-8-exit | reject | reject |
| descriptor-8-binding_stdout_domain | reject | reject |
| descriptor-8-stderr_bytes | reject | reject |
| counterfeit-resealed-fixedpin-8 | reject | reject |
| descriptor-9-role | reject | reject |
| descriptor-9-argv | reject | reject |
| descriptor-9-exit | reject | reject |
| descriptor-9-binding_stdout_domain | reject | reject |
| descriptor-9-stderr_bytes | reject | reject |
| counterfeit-resealed-fixedpin-9 | reject | reject |
| descriptor-10-role | reject | reject |
| descriptor-10-argv | reject | reject |
| descriptor-10-exit | reject | reject |
| descriptor-10-binding_stdout_domain | reject | reject |
| descriptor-10-stderr_bytes | reject | reject |
| counterfeit-resealed-fixedpin-10 | reject | reject |
| descriptor-11-role | reject | reject |
| descriptor-11-argv | reject | reject |
| descriptor-11-exit | reject | reject |
| descriptor-11-binding_stdout_domain | reject | reject |
| descriptor-11-stderr_bytes | reject | reject |
| counterfeit-resealed-fixedpin-11 | reject | reject |
| expected-run_id | reject | reject |
| expected-source_commit | reject | reject |
| expected-source_root | reject | reject |
| expected-producer | reject | reject |
| expected-allowed_slots | reject | reject |
| expected-selected_binding_version | reject | reject |
| expected-selected_binding_version | reject | reject |
| bundle-schema_version | reject | reject |
| bundle-policy | reject | reject |
| bundle-selected_binding_version | reject | reject |
| duplicate-bundle | reject | reject |
| missing-descriptor-0 | reject | reject |
| missing-descriptor-1 | reject | reject |
| missing-descriptor-2 | reject | reject |
| missing-descriptor-3 | reject | reject |
| missing-descriptor-4 | reject | reject |
| missing-descriptor-5 | reject | reject |
| missing-descriptor-6 | reject | reject |
| missing-descriptor-7 | reject | reject |
| missing-descriptor-8 | reject | reject |
| missing-descriptor-9 | reject | reject |
| missing-descriptor-10 | reject | reject |
| missing-descriptor-11 | reject | reject |
| malformed-bundle-b'{' | reject | reject |
| malformed-bundle-b'[]' | reject | reject |
| malformed-bundle-b'\xff' | reject | reject |
| malformed-bundle-b'{"schema_version":1.0}' | reject | reject |
| malformed-bundle-b'{"schema_version":NaN}' | reject | reject |
| receipt-context-slot-duplicate-key | reject | reject |
| receipt-context-slot-float | reject | reject |
| receipt-context-slot-wrong-context | reject | reject |
| receipt-context-slot-wrong-root | reject | reject |
| receipt-context-slot-missing-slot | reject | reject |
| receipt-context-slot-duplicate-path | reject | reject |
| receipt-context-slot-swap-phases | reject | reject |
| schema-edge-archive-policy | reject | reject |
| schema-edge-archive-registry | reject | reject |
| schema-edge-nested-root-mvs-bool | reject | reject |
| schema-edge-selected-local-schema | reject | reject |
| schema-edge-qualification-false-claim | reject | reject |
| schema-edge-production-coverage-types | reject | reject |

## All 18 preserved delta controls

| Control | Expected | Candidate actual |
| --- | --- | --- |
| non-go-local-GoFiles | reject | reject |
| missing-local-native-classification | reject | reject |
| foreign-package-native-classification | reject | reject |
| orphan-generated-testmain | reject | reject |
| non-go-toolchain-GoFiles | reject | reject |
| selected-path-../outside.go | reject | reject |
| selected-path-/outside.go | reject | reject |
| selected-path-internal/../outside.go | reject | reject |
| selected-input-type-sha256 | reject | reject |
| selected-input-type-fields | reject | reject |
| extra-qualification-field | reject | reject |
| missing-qualification-field | reject | reject |
| main-MVS-boolean-type | reject | reject |
| third-MVS-replacement | reject | reject |
| variant-extra-field | reject | reject |
| variant-context-bool-type | reject | reject |
| variant-missing-input | reject | reject |
| resolution-boolean-type | reject | reject |

## All 43 unchanged adversarial controls

| Control | Expected | 0c9adb actual | Candidate actual |
| --- | --- | --- | --- |
| unchanged-positive-isolated | accept | accept | accept |
| native-positive | accept | accept | accept |
| local-native-missing | reject | reject | reject |
| local-native-duplicate | reject | reject | reject |
| local-native-package | reject | reject | reject |
| local-native-field | reject | reject | reject |
| local-native-path | reject | reject | reject |
| local-native-qualification | reject | reject | reject |
| native-two-package-occurrences-positive | accept | accept | accept |
| native-two-package-occurrences-reordered | reject | reject | reject |
| generated-cache-positive | accept | accept | accept |
| generated-exception-borrowed-local-non-Go | reject | accept | reject |
| generated-wrong-package | reject | reject | reject |
| generated-wrong-path | reject | reject | reject |
| generated-missing | reject | reject | reject |
| generated-duplicate | reject | reject | reject |
| generated-cache-env | reject | reject | reject |
| generated-relative-field | reject | reject | reject |
| generated-CgoFiles | reject | reject | reject |
| generated-wrong-suffix | reject | reject | reject |
| generated-count | reject | reject | reject |
| external_modulecache-GoFiles-positive | accept | accept | accept |
| external_modulecache-CgoFiles-positive | accept | accept | accept |
| external_modulecache-TestGoFiles-positive | accept | accept | accept |
| external_modulecache-XTestGoFiles-positive | accept | accept | accept |
| external_modulecache-CFiles-positive | accept | accept | accept |
| external_modulecache-wrong-extension | reject | reject | reject |
| external_modulecache-local-package | reject | reject | reject |
| external_modulecache-count | reject | reject | reject |
| external_modulecache-path-escape | reject | reject | reject |
| external_modulecache-qualification | reject | reject | reject |
| external_modulecache-version-or-goroot | reject | reject | reject |
| toolchain_source-GoFiles-positive | accept | accept | accept |
| toolchain_source-CgoFiles-positive | accept | accept | accept |
| toolchain_source-TestGoFiles-positive | accept | accept | accept |
| toolchain_source-XTestGoFiles-positive | accept | accept | accept |
| toolchain_source-CFiles-positive | accept | accept | accept |
| toolchain_source-wrong-extension | reject | reject | reject |
| toolchain_source-local-package | reject | reject | reject |
| toolchain_source-count | reject | reject | reject |
| toolchain_source-path-escape | reject | reject | reject |
| toolchain_source-qualification | reject | reject | reject |
| toolchain_source-version-or-goroot | reject | reject | reject |

## Exact author result log

```text
test_all_local_native_fields_complete_package_attributed_occurrences (__main__.ClassifierContractChecks.test_all_local_native_fields_complete_package_attributed_occurrences) ... ok
test_classifier_isolation_and_nonlocal_package_count_lower_bound (__main__.ClassifierContractChecks.test_classifier_isolation_and_nonlocal_package_count_lower_bound) ... ok
test_first_four_go_fields_in_every_represented_domain (__main__.ClassifierContractChecks.test_first_four_go_fields_in_every_represented_domain) ... ok
test_generated_cache_exception_complete_linkage_and_wrong_field_rejection (__main__.ClassifierContractChecks.test_generated_cache_exception_complete_linkage_and_wrong_field_rejection) ... ok
test_native_multiplicity_order_and_shared_paths_across_packages (__main__.ClassifierContractChecks.test_native_multiplicity_order_and_shared_paths_across_packages) ... ok
test_non_go_bytes_remain_permitted_for_bound_embeds (__main__.ClassifierContractChecks.test_non_go_bytes_remain_permitted_for_bound_embeds) ... ok
test_source_owned_go_files_cannot_borrow_testmain_exception (__main__.ClassifierContractChecks.test_source_owned_go_files_cannot_borrow_testmain_exception) ... ok
test_test_suffix_ordinary_go_paths_and_generated_cache_are_distinct (__main__.ClassifierContractChecks.test_test_suffix_ordinary_go_paths_and_generated_cache_are_distinct) ... ok
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
Ran 43 tests in 5.078s

OK
OS-process audit events: 0
```

## Preservation and remaining gate

Relative to 0c9adb, only the adapter and its explicitly non-discovered check file
change; this new uniquely scoped report is added. All prior reports/evidence and
all 2,208 frozen-base tracked paths retain their exact bytes. Existing helper/
classifier/schema and discovery remain untouched. Applicable instructions unchanged:
no root/scripts/tests AGENTS.md or `.agents/skills`, empty workspace `.agents`/
`.codex`; previously read web/AGENTS.md has no affected edits.

No producer/source capture, capture/publisher CLI, runtime adoption, preparation/
Go bridge, registry/closure/discovery refresh, existing formal tests/suites, PG/mail/
services, old F52 acceptance, PR, merge, deploy or force push. Central **10/171** is
unchanged. Parent must independently review the final exact head before interface
freeze or assignment of later slices. No independent acceptance is claimed here.
