# R5 wave 2: isolate queued refresh coalescing by session scope

Date: 2026-10-07. Parent TODO: `R5-P9-090` (session switches and multiple tabs).
This completes one implementation item, not the whole parent TODO.

## Reproduction and behavior

Refresh coalescing used one process-wide promise without recording the session
scope. If an older scope's refresh was waiting for the shared Web Lock, changing
the account, selected tenant, or session epoch did not remove that queued work.
A new scope's refresh joined the older promise. Once admitted to the lock, the
older operation correctly rejected its stale scope, but its `false` result also
failed the new scope's request. A current-tenant GET receiving `401` could thus
fail without ever attempting its own permitted refresh.

Coalescing now records the scope alongside the promise. Current-scope callers
still share one operation; a different current scope queues its own refresh on
the same shared `tabmail-refresh` Web Lock. Explicit stale-scope calls return
false, and the existing scope check inside the lock still catches a scope that
changes while waiting. An older operation's `finally` clears the coalescing slot
only when it still owns that slot, so it cannot erase a newer pending operation.

Cookie mutation remains serialized. Token rotation does not change scope.
Temporary failure preserves credentials, current-scope `403` refresh rejection
still clears them, and non-idempotent writes are not automatically replayed.
No fallback lock, endpoint, credential policy, or request replay rule was added.

## Frozen baseline evidence

Baseline commit: `8e976b634f36fdd09a8332b01cfe6cd800f15b75`.

Baseline `web/lib/api/base.ts` SHA-256:
`c0260ee0eb281080a419fb3ebc410541b20819a5f3e27d19f2981cb54c48f0ae`.

The new `web/lib/refresh-scope.test.ts` exercises the production `request`,
`tryRefreshToken`, and session implementation. HTTP is controlled, and a
serialized Web Lock queue begins held by a simulated other tab. The test was
frozen before the product edit, with SHA-256:

`5623c542fb8fea02be8f1077b4f73481442aa731216bc1c64c2834a3bd719fdc`.

The frozen file fails **6/6** on the baseline and passes **6/6** after the fix.
Its cases cover account/tenant/epoch changes, actual current-tenant GET recovery
after `401`, an older completion while a new refresh is still pending, and a
current-scope denied refresh without replaying its POST. The pending-refresh
case requires same-scope callers to share the `503` failure instead of scheduling
another refresh after the older operation finishes.

## Validation

Commands run in `web/`:

```sh
npx vitest run lib/refresh-scope.test.ts --maxWorkers=2 --reporter=verbose
npx vitest run lib/refresh-scope.test.ts lib/session-safety.test.ts lib/api/base.test.ts lib/api/base-stream.test.ts contexts/auth-context.test.tsx --maxWorkers=2 --reporter=verbose
npx tsc --noEmit --incremental false
npx eslint lib/api/base.ts lib/refresh-scope.test.ts
```

- The focused command ran before and after the fix: exit 1 (6 failed) then
  exit 0 (6 passed), without changing the frozen file.
- Related API, stream, auth, and session regressions: **35/35**, five files,
  exit 0, with the existing tests unchanged.
- Full nonincremental TypeScript checking: exit 0.
- Changed-file ESLint: exit 0, no lint warnings.
- `git diff --check`: pass.
- The real API collector emits **134** call branches before and after. All
  semantic fields are unchanged; source line offsets move only. The central
  generated catalog remains the integration owner's responsibility.

Environment: Node 24.19.0, npm 11.9.0, Next 16.3.6, React 19.2.4, Vitest
4.1.11, jsdom; dependencies reuse the lock-installed `tabmail-deps/web/node_modules`.
No manifest, lockfile, config exclusion, validator, or CI threshold changed.

Fixed API source SHA-256:
`8a8fe993b3694ab4578cef5f7e63023ac21e7da0239bb6ec653f559be3e487bf`.
Execution logs and collector output use the workspace `validation/` prefix
`wave2-refresh-`.

## Limits and progress bookkeeping

The queue is a controlled serial Web Locks implementation in jsdom, not a real
browser pair or a live authentication server. This result does not qualify
cross-browser HttpOnly cookie behavior, real multi-tab storage timing, or a
deployment. Existing safe-context and credential tests continue to pass.

Suggested central TODO progress entry:

> R5-P9-090: refresh coalescing now belongs to the current session scope and
> retains ownership through older queued completions. A new account/tenant/epoch
> can renew independently under the shared Web Lock; existing non-replay and
> denied/temporary-failure behavior remains. Frozen baseline 6 failed becomes
> 6/6; related regressions 35/35; full tsc and focused lint pass. One implementation
> item is complete; the full parent task remains open.

At this frontend round: 2 of 3 implementation items are complete, 1 remains.
Parent TODO count remains 10/171 complete, 161 remaining.
