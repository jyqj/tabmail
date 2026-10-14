# Sent-folder completion ownership: bounded R5-P9-040 progress

## Change and source

- Base: [draft PR #45](https://github.com/jyqj/tabmail/pull/45), `e290412a0fb0cb1e303557edeff0d313427d2db3`.
- Published implementation: `6a2fed059c61c6c8ccd1786cc2d0d3d74ef13247`, tree `c97eaba4804a2b57287bb74893a849db571fc994`, identical to independently reviewed implementation `db72e3f2e44f63b306ef642d66148b2773d0c971`.
- A pending sent-asset archive/trash/unarchive/restore used a captured selection callback after completion. New selection, search, pagination, mailbox/folder navigation or Compose could be replaced by an older URL. The callback also ran after unmount. SWR's bound mutation followed the latest list key rather than the action's original list.
- SentFolder now records committed view ownership across session, mailbox, folder, query, page and selection. Navigation or unmount retires that ownership, including away-and-back navigation; unrelated rerenders do not. Only the still-owned view can clear selection or report an action failure, using its latest committed callback.
- Successful actions invalidate their captured original session/list key. Current-view actions still clear selection and refresh. Token-only rotation remains valid; replacement identities cannot receive stale effects.

Production/test blobs:

| Path | Git blob |
| --- | --- |
| `web/features/mail/components/sent-folder.tsx` | `2ce8d2e2d90a9e39c884eb758d2787ed006829f5` |
| `web/features/mail/components/sent-folder-actions.test.tsx` | `cd39739c67abead5dee96870b4e0368b86b8c094` |

MessagePane, Compose/recipient reload, draft writer, workspace/SSE, session/transport, backend/API, dependencies, test configuration and CI remain byte-identical to the base. The final delivery adds only this report and one bounded TODO progress line.

## Verification

- Before the production edit, the initial author consumer suite reproduced **9 failures / 9 passes**. The final suite adds two controls and passes **20/20**. It mounts the real workspace, SentFolder, SWR, action helper and authenticated transport; location, fetch, toast delivery and unrelated private-content/editor readers are controlled fixtures.
- Independently frozen expectations and executable tests preceded candidate inspection. The unchanged independent suite reports baseline **21 failures / 12 passes**, candidate **33/33 passed**. It retains real Compose and live-content consumers as well as the workspace, SentFolder, session/transport and SWR. Bounded independent review passed with no blocking finding.
- Independent preservation selection: **15 files / 218 passed**, including its 33 cases and the author's 20; these counts are not additive. Coverage includes writer serialization/reload, recipient reload, message actions, sent-content/receipt readers, workspace refresh/SSE, transport and session safety.
- Ownership controls cover all organizing actions, new selection/search/page/mailbox/folder/source, Compose, route replacement, unmount, away-and-back, late failure/retry, duplicate clicks, identity replacement/logout, same-identity token rotation, original-list invalidation and unrelated-key isolation. An abandoned Suspense transition does not retire the still-committed view.
- Author frontend-only aggregate: **51 files / 693 passed**, explicitly excluding the Go-produced protocol suite and external private PostgreSQL probe. Nonincremental product typecheck, changed-file ESLint and diff checks passed.
- The full default frontend invocation is **not green**: **51 passed / 2 failed files; 693 passed / 1 failed / 3 skipped tests**. The existing protocol producer cannot start (`Go: Unknown option: test`), and the external probe requires an explicit private fixture. No suite source, timeout, assertion or skip configuration was relaxed.
- The independent review-only frozen fixture had typing/lint diagnostics. It was kept unchanged for baseline/candidate behavioral comparison and excluded from the successful exact-product typecheck; final lint covered only the two committed candidate files. The fixture is not claimed to be publication-ready repository test code.

Reproducible product checks, from `web`:

```sh
npm test -- features/mail/components/sent-folder-actions.test.tsx
npm test -- --exclude components/company/r5-protocol.test.tsx --exclude r5-external-batch-probe.test.tsx
npx tsc --noEmit --incremental false
npx eslint features/mail/components/sent-folder.tsx features/mail/components/sent-folder-actions.test.tsx
```

## Acceptance boundary

This verifies mounted client consumers against synthetic transport, not real-browser, server authorization/persistence, mail delivery or Go/PostgreSQL integration. Original-list invalidation checks use a mounted original-list observer; they do not claim an immediate fetch for an unmounted SWR key. No production build or live-service/database test was run for this batch.

R5-P9-040, its dependencies and full lifecycle/browser gates remain open; central completion remains **10/171** and historical F52 stays failed. No real account, credentials, actual mail, merge or deployment was used. Remote-head CI is a separate delivery check.
