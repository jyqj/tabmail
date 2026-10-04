# PE03 shipping profile editor diagnosis — 2026-10-04

The missing second PATCH is correct stale-editor blocking, not a broken Save action, invalid quota/name, or automatic form reset. No production change is warranted by this investigation.

Production source remains `f875672dea2b68fdec9e0b986f4863724da5d7ba`; this diagnostic branch starts from formal evidence commit `46de5010b7dc79b841c52d6ff7245d070a846a76`. The old formal failure remains intact: one 53.781s run, 17 pass / 9 fail / 17 missing, zero qualified. Its report SHA256 remains `7dbcc94223f82606af4a646d79fb8231e677102ff71a33c7b96402c893d3c896`.

## Source chain

- `web/components/company/r5-protocol-shared-components.test.tsx:13` imports the shipping `web/features/company/profile-management` page, not a legacy component. PE03 opens the profile, completes a separate revocation, edits description, clicks Save, then unconditionally expects two PATCH responses at line 268.
- `web/features/company/profile-management.tsx:280` registers the company event consumer. Its invalidation callback sets `editStale`, fences background operations and clears review state; it does not replace the baseline or dirty form.
- `web/features/company/company-event-consumer.tsx:77` calls that invalidation callback and revalidates the list via authoritative overview/profile reads. SWR uses session-scoped keys (`web/hooks/use-api.ts`). Updating the list does not replace the editor's separate snapshot.
- `profile-management.tsx:349` rejects stale saves before constructing a command; line 604 disables Save for `editStale` or `reviewFresh`. Refresh explicitly performs a three-way rebase and requires review before Save. Untouched revoked rights follow the new snapshot; only user-changed fields survive.

## Focused real-chain observations

New dedicated tests use unchanged `r5UISeed`/`r5UISetup` helpers with a fresh separately owned PG17 cluster and per-case disposable databases. They do not call a formal observer, batch, acknowledgement, acceptance runner or qualification producer. API clients, session, SWR, fetch responses, router/handlers and PG are real. Only host identity and browser-environment shims match the original component harness.

Two shipping transports were observed:

| Observation | Original buffered fixture | Live shipping router |
| --- | --- | --- |
| Alert before external revocation | absent | absent |
| Wire SSE ready / profile-update event | absent / absent | present / present |
| SSE response body | empty | real stream frames |
| After old editor description edit | conflict alert, disabled Save | conflict alert, disabled Save |
| Old sending switch / dirty description | retained / retained | retained / retained |
| UI stale Save PATCH count | zero new requests | zero new requests |
| Explicit old-security, old-revision PATCH | 409 CONFLICT | 409 CONFLICT |
| Refresh, explicit review, shipping Save | 200, description-only fresh CAS | 200, description-only fresh CAS |
| Persisted sending after reviewed save | false | false |

For both scenarios, the real responses are revocation200, direct stale CAS409, reviewed UI Save200. The tests assert the old form's sending switch remains true until explicit refresh, all quota inputs remain valid zero, description remains dirty, the stale click emits no business request, and authoritative reads after UI blocking and after CAS409 retain the revocation revision and original persistent description. After refresh, sending becomes false in the form, Save stays disabled until review, and the exact successful request contains only description and fresh expected_revision. The Go observer also queries PG directly for the final false sending / retained description result.

There is an additional harness transport limitation. `r5_protocol_component_observations_test.go:147` runs the real router against `httptest.ResponseRecorder`, then forwards the completed response. `company_admin_stream.go:148` requires `ResponseController.SetWriteDeadline`; the recorder lacks this capability, so the stream returns before writing ready/resync frames. The TSX fetch observer's awaited clone JSON parse then finishes on the empty body. `web/lib/api/base.ts:393` synthesizes an invalidation on every accepted connection before reading frames, and EOF reconnects cause more resyncs. That resync path can mark the open editor stale even without a delivered profile-update event. Thus the original count failure cannot establish that the formal fixture observed a revocation event. The live-router diagnostic does observe that event and the same safe UI guard.

The exact interleaving inside the historical formal run is not recoverable from its one-count assertion. This diagnosis proves the mechanism in new focused observations and source; it does not invent historical event timestamps.

## Validation and proposed review artifact

Final command: `go test -mod=readonly -count=1 -tags=r5protocol -timeout=120s ./internal/api/handlers -run '^TestPE03EditorRealChainProbe$' -v`. Final result: two diagnostic subtests pass, 9.052s. The test gives each component process a 75s bound; formal budgets are unchanged. ESLint passed for the dedicated TSX probe and config. `git apply --check docs/company-mail/PE03-SHARED-TSX-PROPOSAL.patch` passed; the proposal was **not applied**. Safe evidence: [report.json](evidence/PE03-EDITOR-DIAGNOSIS-20261004/report.json).

The [proposed shared TSX patch](PE03-SHARED-TSX-PROPOSAL.patch) replaces the unconditional UI second-request premise with strict alert/disabled/preserved-draft/no-new-PATCH checks and adds a separately labelled real API-client stale command carrying old revision and old security values. It retains the second actual PATCH count, 409 status, CONFLICT code, no-restoration target marker, and strengthens persistent revision/description invariants. That second request is explicitly issued by the HTTP control, never attributed to Save. The complete old-security control is exercised by the dedicated probe; allowed_zone_ids uses the shipping form's `?? []` normalization.

Root must decide whether the catalog admits proactive UI blocking plus a separate HTTP CAS control as component evidence, or requires a separately designed UI journey that reaches server CAS. The proposed patch cannot remove this qualification decision. The original catalog, markers, acceptance, shared TSX, Go seed, offboarding, production component, lock, Go1.25.7 and both replaces are unchanged.

The new probes reused the existing local Node24.19.0/Vitest4.1.11 installed dependency tree; they claim no fresh official-lock bootstrap or formal external-runtime receipt. Early self-authored probe setup exposed a missing live-router rate limiter, then an unnormalized absent zone list; both were corrected in the dedicated harness. These are diagnostic harness iterations, not reruns of the formal 43/26/46 batch. No formal report was replaced.

Cleanup query found zero owned fixture databases and zero owned fixture backend connections; owned PG shutdown succeeded. Private raw probe reports/logs remain under `/tmp/pe03-owned`. No actual email/accounts/data, shared DB import, wholePG180, audit, default675, merge, deploy, or central10/171 closure. Browser-journey qualification, original formal transport repair, and root's UI-versus-HTTP acceptance decision remain open.
