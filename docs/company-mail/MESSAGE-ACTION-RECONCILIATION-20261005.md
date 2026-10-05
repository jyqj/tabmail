# Received-message action reconciliation: bounded R5-P9-040 progress

## Change and source

- Baseline: [draft PR #42](https://github.com/jyqj/tabmail/pull/42), `b90b91176ed0a85b14d1de63e869120fee89ee16`.
- Published implementation: `22974d6fd7a6a895daddccd96e981b5c256ae163`, whole tree `3a876c0346bbacc7e4d7e1bd9f2a38906f3b77d1`. This tree is identical to the independently reviewed implementation `42a01f3af537acac964292e9ac3e641e95272847`.
- `MessageAction` returns an acknowledgement (`updated: true`), not a replacement message snapshot. Previously, `MessagePane` refreshed only the folder list after a successful action. The open detail and its next-action buttons could stay stale until a later poll or SSE invalidation.
- `MessagePane` now awaits its authoritative detail GET after the existing folder-list invalidation. The existing action guard remains busy until this read settles. No optimistic state is inferred from the POST acknowledgement, and no business POST is replayed.
- A failed POST leaves the previous state intact. If the confirmed POST succeeds but the following GET fails, the existing detail error view hides stale action controls and provides a read-only retry. Folder-list invalidation still occurs independently of that GET's outcome.

Only these production/test files change:

| Path | Git blob |
| --- | --- |
| `web/features/mail/components/message-pane.tsx` | `c62898347604e2bc6f9f2f4dc33c6ba1c8115147` |
| `web/features/mail/components/message-pane-actions.test.tsx` | `8a76bd0354e496f0b78b0fb221f75430e5031929` |

The reviewed SSE transport/parser and workspace remain unchanged: `base.ts` blob `1e9804d56f9f910f70640d174d56db61bf5d0152`, `event-stream.ts` blob `738d7d92e8dab4699bf383cdffa082b63b912bf7`, and `workspace.tsx` blob `6fb93714b0b1e59e10bd181258ff9aab16b5c61d`.

## Verification

The tests use controlled fetch responses with the real mounted component, SWR cache, action guard and authenticated request/session layer. They do not advance polling timers or inject SSE events to satisfy the action's required refresh.

- Final author suite copied unchanged onto the exact baseline: **11 failed / 3 passed**; candidate: **14 passed**. It covers seen/unseen, starred/unstarred, archive/unarchive, trash/restore, a deferred authoritative read, repeated clicks, rejected POST, failed GET/read-only retry, account/tenant changes, and token-only rotation.
- Related eight-file mounted consumer/session/stream selection: **101 passed**.
- Independent contract and tests were frozen before candidate inspection. The unchanged independent suite produced baseline **21 failed / 7 passed**, candidate **28 passed**. It additionally checks arbitrary authoritative flags, 403/404/503 GET failures, logout and late POST/GET results, keyed message/mailbox navigation, unmount, and capability controls. No blocking finding was reported for this bounded change. Frozen test SHA256: `bdc53b036ea60fff7bc0d91a8dc591ce64897e890018bd6b8a560e2ed51136dc`.
- Separate independent seven-file regression selection: **73 passed**. Independent TypeScript check of the candidate product and author tests, plus strict changed-file ESLint, passed. The reviewer did not run a full-web suite, production build or real browser.
- Author `tsc --noEmit --incremental false` and strict changed-file ESLint: passed.
- Full `npm run lint`: exit 0, with four existing warnings in untouched files.
- Production `npm run build`: passed, including **29/29 static pages**.
- First full-web attempt: **640 passed / 2 failed / 3 skipped**, with three failed files. Alongside the two known environment-blocked files, the unchanged permission-editor session suite had one 5-second timeout while build/lint were also running. This attempt is not discarded or counted as a pass.
- An unchanged full-web rerun without concurrent build/lint produced **641 passed / 1 failed / 3 skipped**, with **48 passed / 2 failed files**. The remaining failures are the Go-backed protocol suite setup (`Go: Unknown option: test`; required Go 1.25.7 unavailable) and the explicitly required private PostgreSQL fixture. Both blockers were separately reproduced with exact baseline files. No timeout, assertion or suite configuration was relaxed.

Focused command:

```sh
cd web
npm test -- --run features/mail/components/message-pane-actions.test.tsx \
  features/mail/workspace-events.test.tsx \
  features/company/company-event-consumer.test.tsx \
  features/company/permission-editor-session.consumer.test.tsx \
  lib/session-safety.test.ts lib/api/base-stream.test.ts \
  lib/api/event-stream.test.ts lib/api/company-events.test.ts
```

## Acceptance boundary

The full suite is **not green**, and skipped tests are not passes. This is bounded client action reconciliation, not complete folder/content lifecycle acceptance. R5-P9-040, its dependencies, P7 recovery/revocation work and G0/G7/G9 remain open; the accepted count stays **10/171** and historical F52 remains failed.

No backend or API contract, dependency, session owner, reviewed SSE/workspace logic, R5 registry/preparation/receipt, or CI configuration changes. Real Go/PostgreSQL/browser lifecycle validation remains unverified. Validation used no live accounts/mail, external mail services or production databases/credentials. Nothing was merged or deployed.
