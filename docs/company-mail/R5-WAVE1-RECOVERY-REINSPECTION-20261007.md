# R5 wave 1: invalidate recovery inspections after conflicts

Date: 2026-10-07. Parent TODO: `R5-P9-080` (recovery center). This is one
bounded implementation TODO; it does not close the broader parent task.

## User-visible defect and change

An operator could inspect an inbound receipt or an outbound delivery, select
recovery targets or evidenced outcomes, and receive `409 CONFLICT` when another
actor changed the inspected version. The page showed the error but kept the old
inspection, old `updated_at`, and selected choices available for another write.

The two conflicting write paths now discard that inspection and its choices.
Each section tells the operator to inspect again. The entered recovery reason is
preserved. A failed reinspection leaves the requirement visible; a successful
inspection clears it and requires a fresh selection against the new version.
The UI does not automatically inspect, retry, or reconcile. Explicit `403` and
network failures keep their existing error feedback and preserve the reason.

Outbound invalidation is tied to the inspected job generation. A late conflict
from a job that the operator has since replaced in the ID field does not label
the new job stale. Existing request/session fencing remains in place.

## Fixed baseline reproduction

Baseline commit: `4065c4909c8f21a401a9a1af6370fa3f72670b99`.

Baseline recovery page SHA-256:
`f7d291f3ee65fb3b106485fc56f7214eb343285d1dc5012449c426890cf39fe9`.

The new test mounts the actual `RecoveryPage` under `AuthProvider`, exercising
the actual SWR hooks, session boundary, `request`, and `company` client. Only
HTTP responses and toast delivery are controlled. Its bytes were frozen before
the product change and retained unchanged:

`web/features/company/recovery-reinspection.consumer.test.tsx`

SHA-256: `591e3ebe431c87536807f794875493f43859b6b2fbcee80ff45183c241498fb3`.

The baseline ran all seven cases: **4 failed, 3 passed**. Both command paths
failed the conflict invalidation and fresh-selection scenarios because the old
action remained mounted. Denial/network non-replay and changing the job while a
write was pending already passed and retain their original assertions.

After the fix the same frozen file is **7/7 passing**. Reinspection tests also
check that a failed explicit inspection cannot revive the retired result, and
that the eventual new write carries the newly inspected `updated_at` and targets.

## Validation

Commands run in `web/`:

```sh
npx vitest run features/company/recovery-reinspection.consumer.test.tsx --reporter=verbose
npx vitest run features/company/recovery-reinspection.consumer.test.tsx --maxWorkers=2 --reporter=verbose
npx vitest run features/company/recovery-reinspection.consumer.test.tsx features/company/outbound-inspection.consumer.test.tsx contexts/auth-context.test.tsx lib/session-safety.test.ts --maxWorkers=2 --reporter=verbose
npx tsc --noEmit --incremental false
npx eslint 'app/(dashboard)/company/recovery/page.tsx' features/company/recovery-reinspection.consumer.test.tsx
```

- The first command is the baseline run: exit 1 with 4 failed / 3 passed.
- The focused fixed run passes all 7 cases, exit 0.
- The combined related regression run passes **55/55**, four files, exit 0.
- Full nonincremental TypeScript checking passes, exit 0.
- Changed-source/test ESLint passes, exit 0, no lint warnings.
- `git diff --check` passes.
- The real `node scripts/collect_api_calls.cjs` still emits 134 call branches.
  Their semantic fields match the baseline catalog; only source line locations
  move. The central generated catalog is left to the integration owner.

Environment: Node 24.19.0, npm 11.9.0, Next 16.3.6, React 19.2.4, Vitest
4.1.11, jsdom. The worktree reuses the lock-installed dependency directory from
`tabmail-deps/web/node_modules`; package manifests, lockfiles, Vitest exclusions,
and CI gates were not changed. The local Next server/client component guide was
read before implementation as required by `web/AGENTS.md`.

The fixed recovery page SHA-256 is
`bbad2f47f4c1f4bf595c7080f52036c728f32aedee948ff4d1c4db752b7fb09f`.
Execution logs and collector output are under the workspace `validation/`
directory with the prefix `wave1-recovery-`.

## Boundaries and TODO bookkeeping

This verifies the mounted frontend against controlled responses. It does not
claim a real browser, live HTTP/PG, SMTP recovery, or release qualification.
Server optimistic concurrency contracts and non-conflict submission semantics
are unchanged. No automatic replay of unknown writes is introduced.

Suggested central TODO progress entry:

> R5-P9-080: recovery UI now invalidates inbound retry/outbound reconciliation
> inspections on conflict, preserves the operator reason, and requires explicit
> successful reinspection plus fresh choices. Frozen mounted-consumer baseline
> 4 failed / 3 passed becomes 7/7; related regressions 55/55; full tsc and focused
> lint pass. This closes one implementation item, not the full parent task.

At this frontend round: 1 of 3 assigned implementation items is complete, 2
remain. Parent TODO count remains 10/171 complete, 161 remaining.
