# R5 permission editors — isolated checkpoint (2026-10-03)

Fixed implementation start: `jyqj/tabmail`, `work/company-mail-r5-goal-20260930@93005b644c1f39fa85072fe913156cf3d6c798e1`. Fetched the exact SHA and switched to independent branch `r5-permission-audit-20261003`; no development on the old cloud R4 branch. The initial requested `work/…` local branch name conflicted with an existing local `work` ref; only the new local branch name changed. The remote R5 base was observed at `29a388a3c7d6da03641abe79bb5c728b90b75273`; this work still starts at the authorized fixed SHA.

This is an unaccepted parallel WIP checkpoint. Parent dependencies remain open; `R5-TODO.md` and **10/171** are unchanged. Parent owns integration and central completion decisions.

## Findings and minimal change

The existing implementation already has raw permission snapshots, separate effective reads, explicit override intentions, session leases, decimal revisions and versioned profile commands. No replacement editor was built.

Profile management still sent every form value after manual conflict refresh. A description-only draft could therefore resend an old `can_send=true` after another administrator revoked sending. Refresh failure also left a previously valid revision saveable. Both 401 and 403 could be followed by another manual refresh/submission in the same session. Four added consumer assertions independently failed against the fixed production source (20 original pass, 4 new fail, 0 skip).

Profile PATCH now serializes only differences from the form baseline observed when opening the editor. Reviewing a fresh revision advances the expected revision without making untouched stale fields into edit intentions. Explicit changed false, zero and empty zone list remain present; untouched NULL zone storage is omitted. No-change forms send no command. Full observed-profile validation, failed refresh state and failed list reads prevent saving. Server 401/403 reasons are shown; creation/edit/deletion and revision/preview refreshes remain blocked after denial until a new session. Existing operation and session fencing remains in place.

The standalone A02 TS audit previously waited for stale description persistence, contradicting current CAS. It now records the real profile PATCH response statuses and requires `[200, 409]`, preserved draft and unchanged post-revocation revision/description. This audit edit is type/lint checked only, **NOTRUN** against PostgreSQL/HTTP.

## Parent task mapping

| Parent task | Evidence and boundary | Status |
|---|---|---|
| R5-P1-090 | Existing raw/effective split and failed-read/A→B/session tests executed through real React controls, API serializer and session owner with synthetic fetch. Profile failed-read/malformed snapshot/denied load guards strengthened. | Partial component qualification; parent dependencies and HTTP/DB acceptance open |
| R5-P1-100 | Existing override null/false/0/domain intents exercised; profile description-only body, untouched NULL omission, changed false/0/[] and no-op body protection added. | Partial component qualification; no parent done |
| R5-P1-110 (overlap only) | Conflict review retains dirty draft, requires manual review, never replays automatically; untouched old permissions omitted. Authorization denials fence further commands. | Limited profile consumer evidence, not full deletion/member/CAS acceptance |

## Actual verification

Commands run from `web` unless noted. Exact commands, per-suite assertion names/results, counts, source SHA256 values and unsuccessful intermediate runs are in [structured evidence](evidence/R5-PERMISSION-EDITORS-CHECKPOINT-20261003.json). No raw HTTP traces, credentials, mail or unrelated logs are copied into this checkpoint.

| Run | Total | Pass | Fail | Skip | Interpretation |
|---|---:|---:|---:|---:|---|
| Fixed production / original profile suite | 20 | 20 | 0 | 0 | Existing tests miss the four paths |
| Fixed production / four added reproductions | 24 | 20 | 4 | 0 | Target defects independently reproduced |
| First implementation / old full-form and no-op test assumptions | 24 | 19 | 5 | 0 | Intermediate failure retained; tests changed to stage actual dirty fields |
| Profile-only after field-intent assertions | 25 | 25 | 0 | 0 | Limited component pass |
| First five-suite run | 147 | 146 | 1 | 0 | Profile fixture gave SSE an immediately EOF JSON body, reconnect invalidated draft before submission; retained failure |
| Final five-suite run | 150 | 150 | 0 | 0 | Connected cancelable synthetic SSE fixture; all five suites executed |

Final command:

```sh
npm test -- --reporter=json --outputFile=/tmp/r5-permission-final2.json \
  features/company/profile-management.consumer.test.tsx \
  features/company/permission-editor.consumer.test.tsx \
  features/company/permission-editor-session.consumer.test.tsx \
  features/company/company-event-consumer.test.tsx \
  lib/api/permission-editor.serializer.test.ts
```

Final suite counts: profile 28; member editor 22; session 17; event consumer 14; serializer 69. These are actual jsdom React/Base UI interactions and production consumer/serializer calls with deterministic synthetic fetch, not browser or PostgreSQL acceptance. The existing event suite separately exercises real ReadableStream invalidations, EOF, identity changes and authority revocation; no production SSE behavior was changed to accommodate the profile fixture.

`npx tsc --noEmit --incremental false`: exit 0, including the final opt-in audit instrumentation. Targeted ESLint: exit 0, 0 errors, 1 inherited `operation.current` effect-cleanup warning. Initial lint found an inherited `any` in the touched test; it was replaced with `Record<string, unknown>`. `git diff --check`: exit 0. Initial wrong-cwd insertion attempts and root npm ENOENT ran zero assertions and are recorded as setup errors, never passes.

Existing official dependencies and lockfile were used without mutation. Node `v24.19.0`, npm `11.9.0`. No full web build/suite/CI, real browser, Go server tests or PG/SMTP tests were run. Opt-in audit instrumentation added after the final five-suite run was subsequently type/lint checked, not executed as an audit.

## Integration handoff

Only `profile-management.tsx`, its consumer test, `permission-editors.r5audit.tsx` and this independent checkpoint/evidence are changed. No shared API client, server DTO/OpenAPI, store, migration, dependency, CI or central TODO changes.

The profile wire contract was checked against `internal/app/permissions/service.go` UpdateInput and `internal/api/handlers/permissions.go`: PATCH is flat with `expected_revision`; omitted fields are preserved, explicit false/0/[] replace values. Profile NULL is a no-op in this legacy pointer/slice contract, not override inheritance. Existing permission-editor command tests separately cover explicit NULL inheritance and domain inherit/all/list/none; these semantics were not conflated.

**Remaining parent integration blocker:** `internal/store/postgres/r5_component_baseline_test.go` still demands the historical insecure mutations, exact defect marker and child exit 1. Parent must promote that Go producer to required secure-protocol acceptance before claiming A01/A02 HTTP/PG closure. This block did not change its Go file or run it with misleading expectations. The standalone updated A02 test can be exercised once that producer is adapted. Real browser interaction is NOTRUN.

GitHub `gh auth status` reported the current `GH_TOKEN` invalid. Commit/push/draft PR use the existing configured route/identity only; no token changes, identity fallback, merge, force-push or deployment. Actual delivery results are reported in the handoff response.
