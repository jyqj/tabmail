# Current wire collection: independent 2026-10-03 proposal

Base `ee3308fd3217246c9bdd43b07ae0609ebae6aeb6`, product `aeed47f` unchanged. This directory is an operator-review proposal, not adoption into the compatibility map. Main TODO 10/171 remain open. No production, validator, lock, Go1.25.7, two local replacements or budget changes.

| Formal producer | Terminal | Actual runtime started | Evidence / remaining action |
| --- | --- | --- | --- |
| TestCompanyHTTPContract through check_http_contract.py | PASS HTTP shape | yes, once | 81 responses, 66 successful operations, 67 required variants; race180s/process240s; two live DNS exclusions retained |
| check_r5_protocol.py --run shared-db | FAIL, exit1 | yes, once | race120s/process180s; formal shared_scoped_evidence_failed, zero verified cases; RC02/BC03 below |
| check_r5_protocol.py --run shared-components | NOTRUN_POLICY_BLOCKED | no | archive-v4 refuses standard installed Node dependency topology; requires owner adaptation before real run |

HTTP first invocation failed before Go due to missing date-time validator. It is retained as `http.execution.json`; actualstarted=false because no command.json/log existed. Repository requirements were then installed unchanged into a private venv, and the one actual HTTP runtime passed. It does not prove permissions/CAS or product green.

Own PG17 was initialized at `/workspace/r5-wire-private/pgdata`, bound to 127.0.0.1:55433, database r5_wire_test. Fixtures use synthetic accounts/mail and loopback; no SMTP worker, live DNS, external provider or whole-PG suite was run. PG stopped after collection. Responses, logs with output and private protocol report remain local under mode700 `/workspace/r5-wire-private`, never in Git. Raw hashes are pinned in exported packets; original safe exported packet bytes are also pinned by join-proposal. DB created no observations.json/component packets: the full actual packet set here is the HTTP status packet and DB lifecycle packet, with no invented DB response observations. No historical AB refs, frozen_tree or validation_commit are imported.

Archive-v4 capture succeeded with exact protocol contexts: DB `[['r5protocol']]` and components `[[],['r5protocol']]`. They bind identical file bytes but distinct context identities. DB manifest raw SHA `064ea6672351b7f1536c40fec17c709da7f15ebcfd502f405a7267eb24dc2ce5`; source SHA `a2c7dd0c5728067126b5a5ea05218fda0167cda6`. Independent post-runtime recapture equaled the manifest. HTTP source-sha is the Git commit operator argument, so this equality is an explicitly proposed supplemental join, not a new producer-attested identity claim.

Preparation failures preserved: initial concurrent npm installation caused topology/metadata change during source capture. Dependencies were moved outside the checkout and clean captures generated; no runtime target was retried. Both clean selected-v2 default and race-r5protocol captures failed during fixed `go list` with `error obtaining VCS status: exit status 128`. Their complete formal failure JSON is preserved. No -buildvcs=false, alternate Git executable, credential or checker workaround was used; selected attestation remains unqualified. Preparation timestamps were not instrumented; filesystem times are not asserted as execution times. Actual producer invocation start/end are recorded in execution.json files.

## Assignable remaining work

1. **Runtime/source policy owner:** archive-v4 `r5_archive_boundary.topology` traverses web/node_modules and rejects standard `.bin/semver` symlinks. Standard npm ci installation is incompatible before shared-components runtime. Define/review explicit safe installed dependency treatment or supported external component dependency resolution, without silently skipping source checks. This task deliberately leaves runnerprep and compatibility ownership untouched.
2. **Selected runner owner:** investigate fixed Go metadata invocation VCS error under its intentionally clean environment in this cloud workspace. Preserve original Git identity and exact selected command; do not turn off VCS stamping. No successful selected receipt exists.
3. **Ordinary receipt / protocol owner:** RC02 legacy_outbound_list, legacy_outbound_detail and submit_replay fail `r5_protocol_shared_test.go:1339`, “legitimate safe receipt erased”. These are non-target assertion failures, not accepted reds.
4. **Legacy provenance owner:** BC03 missing_source/same_id_foreign_source capability tests and missing_source/foreign_job shared observations fail for missing explicit legacy_unknown completeness. Preserve unprovable-source boundary and repair semantics, then collect a distinct authorized follow-up run. Current classifier reports target_red={} because RC02 invalidates this run; marker-bearing BC03 failures are not promoted into a passing baseline.

No permission-editor/CAS or component journeys were established by this collection. No additional product-red set is inferred from absent runtime.

## Diagnostic

`python3 -B docs/company-mail/evidence/R5-CURRENT-WIRE-20261003/safejoin.py` verifies actual safe artifact hashes and identity/status joins only, returning current_wire_complete=false. `test_safejoin.py` runs six synthetic safe metadata probes: control plus hash drift, terminal promotion, missing packet, historical identity and rehashed private field rejection. It never manufactures producer runtime JSON. This is a local proposal diagnostic, not replacement for production validate_wire or automatic allpass.
