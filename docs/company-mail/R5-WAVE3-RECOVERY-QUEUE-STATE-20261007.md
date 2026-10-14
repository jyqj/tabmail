# R5 wave 3: authoritative recovery queue load states

Date: 2026-10-07. Parent TODO: `R5-P9-100` (loading, empty, failure, and retry
integrity). This completes one bounded list-state implementation item.

## User-visible defect and result

The recovery queue rendered `No recovery tasks` after an initial failed load,
including `403` and `503`. When a refresh failed after an earlier successful
load, SWR retained the earlier data, and the page continued to render those rows
and their inspection buttons. Pagination also used the old successful total.
There was no loading status, and navigating to page two left the previous-page
button active while that page was still loading or unavailable.

The queue now announces loading and refreshing, disables list inspection and
pagination while a read is pending, and disables refresh during that read. A
failed load displays the existing retryable error instead of old rows or a false
empty result. Pagination stays disabled without a successful current-page list.
An explicit successful retry restores the current page, its rows, and its total.
During a background read that has not failed, previous rows remain readable
under the refreshing status while their inspection actions are disabled.

The operator's reason and outbound job input remain intact. These derived list
states do not change the independently inspected recovery/reconciliation state,
the conflict invalidation from wave 1, request retries, or write semantics.
`LoadError` handles failed reads through the existing SWR revalidation behavior.
The implementation does not add catch-and-ignore transport wrappers or mutations.

## Frozen baseline reproduction

Baseline commit: `d170ef9a37f5d102674b4172018e5dc1f3e05c3d`.

Baseline recovery page SHA-256:
`bbad2f47f4c1f4bf595c7080f52036c728f32aedee948ff4d1c4db752b7fb09f`.

The new test mounts `RecoveryPage` with the actual `AuthProvider`, SWR, and HTTP
client. Only fetch responses and toast delivery are controlled. No hooks, list
data, or state transitions are mocked. The file was frozen before the product
edit and kept unchanged:

`web/features/company/recovery-queue-state.consumer.test.tsx`

SHA-256: `d6a8c81b7686495dd9ed93d1987483ba7d75c582b46f64c2ace1a80a08ef39ab`.

Baseline: **6 failed / 2 passed**, exit 1. The failures reproduce missing initial
and page-two loading feedback, false empty states for initial `503`/`403`, and
stale actionable rows after background `503`/`403`. Successful empty and explicit
retry/input-preservation cases already passed and retain their original meaning.

All **8/8** frozen cases pass on the final code. They also verify that page-two
retry requests page two again, new response totals govern pagination, stale rows
do not return after a successful retry, operator input survives, and list retry
issues only GET requests.

## Validation

Commands run in `web/`:

```sh
npx vitest run features/company/recovery-queue-state.consumer.test.tsx --maxWorkers=2 --reporter=verbose
npx vitest run features/company/recovery-queue-state.consumer.test.tsx features/company/recovery-reinspection.consumer.test.tsx features/company/outbound-inspection.consumer.test.tsx lib/refresh-scope.test.ts lib/session-safety.test.ts contexts/auth-context.test.tsx --maxWorkers=2 --reporter=verbose
npx tsc --noEmit --incremental false
npx eslint 'app/(dashboard)/company/recovery/page.tsx' features/company/recovery-queue-state.consumer.test.tsx
```

- The first command recorded the frozen baseline: 6 failed / 2 passed, exit 1.
- The final combined run is **69/69 passing**, six files, exit 0. It includes
  all three new frontend implementation suites and the existing outbound
  inspection, session, and auth coverage.
- Full nonincremental TypeScript checking: exit 0.
- Changed-source/test ESLint: exit 0, no lint warnings.
- `git diff --check`: pass.
- The actual API collector emits **134** call branches before and after, with
  identical semantic fields and only source line movement. The central generated
  catalog remains unchanged in this worktree for the integration owner to refresh.

Environment: Node 24.19.0, npm 11.9.0, Next 16.3.6, React 19.2.4, Vitest
4.1.11, jsdom, using the existing lock-installed dependency directory. No package,
lockfile, exclusion, CI threshold, or validator was changed.

Final recovery page SHA-256:
`fb7c45ece2271935716c7ab20fce5526d8a74fa3bf7acdb7b879188f42158ba5`.
Final evidence uses `validation/wave3-queue-{baseline,regression,typecheck,lint}.log`
and `validation/wave3-queue-client-calls.json` in the workspace. The intermediate
development log is separate and is not the final-source qualification.

## Boundaries and bookkeeping

This is mounted frontend verification with controlled successful and failed
responses. It is not a live browser, backend/PG, SMTP, or production-build gate.
The broader all-feature loading/error TODO remains open. This work does not alter
server authority or infer that a failed write may be resubmitted automatically.

Suggested central TODO progress entry:

> R5-P9-100: the recovery queue now announces pending reads, keeps unavailable
> data out of the list, distinguishes successful empty responses from errors,
> and gates pagination against a successful current-page response. Explicit retry
> preserves operator input and restores fresh rows and totals. Frozen baseline
> 6 failed / 2 passed becomes 8/8; all three frontend waves plus related tests
> 69/69; full tsc and focused lint pass. One implementation item is complete;
> the parent TODO remains open.

At this frontend round: 3 of 3 implementation items are complete, 0 remain.
Parent TODO count remains 10/171 complete, 161 remaining.
