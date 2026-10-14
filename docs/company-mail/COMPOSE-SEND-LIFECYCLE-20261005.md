# Compose send ownership: bounded R5-P5-120 progress

## Source and behavior

- Baseline: [draft PR #46](https://github.com/jyqj/tabmail/pull/46), upstream `00461ab2f5b8343b844c0cea1d014b20fe7c2ffb`. Local mirror `53cf607b12c4b0fe4bd67233da5605ee753a9375` has the exact same tree, `42d767eb661473b58e5deffb2a1cd7cc5e0e6cda`.
- Immutable implementation commit: `5348f95ae879e291416ec7b87e607cecaa898b71`, tree `ff468500cf6d2c46d8a02dde08acfe03347ed514`.
- Independently accepted production file: `web/components/company/compose.tsx`, Git blob `846a0633addf4727576b247e36e66827e0b6c98e`, SHA256 `bcd5ea166437e4347003aa60afa785a062dfc370fb05c7a820b2de8b949bacdb`. Later additions retain this production file byte-for-byte.

Previously, Compose awaited a draft-save acknowledgement and then called submit even if navigation had unmounted that editor. A late response to an already-dispatched submit could also call the old `onSent` callback, close a replacement editor, or display an obsolete toast. A retained uncertain submission bypassed the writer on retry and could acquire the new session's transport identity.

The component now binds sends to the editor's initial identity scope and committed mount generation. The pre-submit boundary also checks committed send eligibility and its revocation generation. A committed permission denial cancels the old intent even if permission is restored before save finishes; a fresh explicit click is allowed. Layout effects update those guards only at commit, so speculative suspended renders cannot revoke the still-visible authorized editor. Cleanup does not permanently close the writer, preserving StrictMode setup/cleanup replay.

After submit has already dispatched, the server may have queued mail. The fix does not claim to cancel or roll back that request. Late UI delivery is suppressed when the originating editor has unmounted, been replaced, or lost its session. A still-mounted same-session owner can report its queued result even if sending permission was withdrawn after dispatch. Uncertain-result retries retain the exact original draft, revision and idempotency key; they cannot borrow a changed session.

Only Compose production code changes. DraftWriter, recipient reload/reset behavior, workspace routing, transport/session implementation, server/API, dependencies, test configuration and CI remain unchanged.

## Verification

All lifecycle checks mount real Compose, DraftWriter, request/company transport, session guards and SWR. Only fetch and toast delivery are substituted, using synthetic addresses, tokens and deferred responses. No live mail, account, credentials, backend or PostgreSQL fixture is used.

- The original author suite was frozen before editing production: **14 failed / 8 passed** on baseline; **22 passed** on candidate. Frozen test SHA256 `c4b380c3b6585c5f08f966726b1ced886f2f666e5cf80cf36ff38981cb1c1dcc`. A type-only fixture annotation was then added for nonincremental TypeScript checking.
- Two controls from the independently frozen review were subsequently retained in the repository suite: committed permission denial/restoration and a speculative denied Suspense transition. Final repository lifecycle suite: **24 passed**. Final test Git blob `09ac1279a87e82cee860c2caa199219565b6058e`, SHA256 `0e987362184060b79c6e3c3a5bcf48e7b75cca4491d060b9db30572c846d31ce`.
- Author related regression selection: **129 passed** across nine files before the two additional controls. Includes existing Compose, recipient reload, writer/save/reload/snapshot, session safety, compose identity and workspace events.
- Independent cases were frozen before candidate inspection: **19 failed / 18 passed** on baseline; **37 passed** on the exact accepted production bytes. Primary suite SHA256 `433d92a1d0cdeb6b7ed069d1783eed65367e7eefd8620a77c8dfa5ff2825aa0e`; supplemental pending-session-retry suite SHA256 `748da442518c62a7f5f2b171307bde027d261d914a1797698cc7f847f19b9d2a`.
- Independent final combined run: **216 passed**, consisting of the 37 frozen lifecycle cases and 179 preservation cases. Product TypeScript and Compose ESLint passed. Review found no production blocker within this scope.
- Coverage includes existing PUT/new POST/clean-flush unmount, keyed replacement including reopening the same draft, stale save and dispatched-submit errors, explicit Close/cancel, repeated clicks, permission loss and restoration, missing mailbox/template-only restrictions, speculative renders, StrictMode, account/tenant/logout/epoch changes including logout-return, normal sends, token rotation and exact-key unknown-result retry.
- Author final `npx tsc --noEmit --incremental false` passed. Changed-file ESLint passed; full `npm run lint` exited 0 with four pre-existing warnings in unchanged files.
- Final full web `npm test -- --maxWorkers=2`: **717 passed / 1 failed / 3 skipped**, **52 passed / 2 failed files**. The existing Go-backed protocol setup fails with `Go: Unknown option: test`; the other test requires an unavailable explicit private PostgreSQL fixture. These failure sources and configuration are unchanged. The full suite is **not green**; no assertion, skip, timeout or environment gate was relaxed.
- A production build was deliberately not run under the shared workspace's disk constraint. No dependency installation or full clone was performed.

Commands:

```sh
cd web
npm test -- components/company/compose-send-lifecycle.test.tsx
npx tsc --noEmit --incremental false
npx eslint components/company/compose.tsx components/company/compose-send-lifecycle.test.tsx
npm run lint
npm test -- --maxWorkers=2
```

## Acceptance boundary

The navigation guarantee is limited to editor unmount or keyed replacement. URL/query-only navigation that leaves the same Compose mounted is not observed by these guards and is not claimed as fixed. This is mounted client-component verification with controlled fetch, not a real-browser or Go/PostgreSQL/send qualification.

R5-P5-120 and its dependencies remain open. Central completion stays **10/171**; historical F52 remains failed. No merge, deployment, migration, real mail or production database action occurred. Publication and remote-head CI are separate from this local acceptance.
