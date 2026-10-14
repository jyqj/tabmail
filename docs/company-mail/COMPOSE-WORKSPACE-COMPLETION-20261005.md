# Compose completion and newer workspace navigation

## Source and reproduced behavior

- Baseline: [draft PR #47](https://github.com/jyqj/tabmail/pull/47), `4c8c6880e4936acfc933fde2102b88db21e8c754`. The local tested baseline `5053ff89af5bb4f64979f706f1a596aa3da9d9b9` has the same tree, `ada145c49513a97d09a4625475155bdcf4b8eaf5`.
- Immutable implementation: `ffd9dba6633dceeae1f95dca2dcc8c2a51def7df`, tree `07a9e4fae3c7e9a61e7a147f05043889cdbc5a1d`.
- Only production change: `web/features/mail/workspace.tsx`, Git blob `1f674bdf3235a46b30a40eab338ff42d79a54f27`, SHA256 `8c205f2d1746785a72c97e0760e9d746f5dc55b27937531568838a10d0da50b8`.
- New regression suite: `web/features/mail/workspace-compose-navigation.test.tsx`, Git blob `afaffdeaeec2b6d8a40a2438263c411ad7048dcd`, SHA256 `b81921d5ef9fefac6b07b64537440fad77404e2711e813cf6f5e0e86e8b2a683`.

The workspace retains Compose across query-only navigation. Its draft, sender identity and unsaved input are editor state, distinct from the mailbox/folder/query underneath. Folder controls are hidden while composing, but the dashboard's My mail link and browser navigation can change the same route's query. The installed Next.js routing documentation describes search-parameter navigation without replacing the route segment.

A successful submit previously called the `onSent` closure captured when Send started. That closure replaced the URL using its old query snapshot. A mounted-component reproduction changed the location from `mailbox-one/drafts/old-search/page-2` to `mailbox-two/sent/message-two/new-search/page-3` while submit was already dispatched. Completion incorrectly restored mailbox-one and old-search while opening receipts. A separate case lost unrelated query data added during the wait.

## Deliberately narrow semantics

The current TODO requires navigation not to initiate implicit sending, and also requires preserving unsaved input. It does not unambiguously state that every query-only change abandons an earlier explicit Send while the same editor stays visible. This change does not invent that cancellation rule.

- Query-only navigation keeps the same editor, sender, draft and unsaved content. It does not initiate a send or retry. An already-explicit send may finish its save and submit while that same eligible editor remains.
- A send's authority to redirect belongs to its originating committed workspace view: session, path, effective mailbox, folder, page, search, selection and received/sent source view. A later committed change, including away-and-back, retires that redirect authority.
- Unrelated query data and parameter order do not retire a legitimate completion. Its redirect uses the latest committed query callback, preserving newer unrelated parameters.
- A successful submit reconciles only its own consumed editor, identified by its editor key. It cannot clear a replacement editor. A consumed same editor is closed even after query navigation; suppressing all completion handling would strand it as a locked, already-submitted draft.
- Already-dispatched mail is never described as canceled. The existing Compose mount/session/permission guards, exact draft/revision/key retry behavior and save writer remain unchanged.

A semantic-view owner is retained in state and replaced when that view changes. The current owner, editor key and query callback are published only in a layout effect. Speculative renders cannot acquire or revoke committed redirect authority. No query-driven Compose key or unmount was added.

## Verification

All new tests mount the real MailWorkspace, Compose, DraftWriter, readers, SWR, session guards and authenticated request layer. Only Next's location, fetch and toast delivery are controlled. Fixtures use synthetic data; no actual account, backend, PostgreSQL or mail transport is contacted.

- The original four-case suite was frozen before production changes: **2 failed / 2 passed** on baseline, **4 passed** on candidate. Frozen SHA256 `28df61773831f1e4fb79d3c71ed91682da5ee6ba79ecaf1f6567fc94494aacb9`.
- Final author suite: **15 passed**, covering preserved unsaved editor/input, ordinary success, each meaningful query dimension, received/sent archive source, away-and-back, query ordering, unrelated query retention, refresh, Send after earlier navigation, and navigation during a deferred save without canceling the explicit send.
- Related regression selection: **11 files / 166 passed**, including existing Compose lifecycle, recipient reload, draft save/reload/snapshot serialization, session safety, compose identity, workspace events and sent-folder completion ownership.
- Independent pre-candidate frozen suite: **10 failed / 13 passed** on baseline, **23 passed** on the exact candidate. SHA256 `44ba1629635214493402b8f9fb5560585dcce02aca037a25e2218f0364e4d5a6`. A separately identified post-inspection supplement recorded **6 failed / 1 passed** on baseline and **7 passed** on candidate; SHA256 `847bfd05a3182f75f9a95b0953df0683b846a19dd3b553422e958dad5aa2910a`. It is not presented as pre-candidate frozen coverage.
- Independent final related sweep: **14 files / 213 passed**, including both prior PR47 independent lifecycle suites. These overlapping counts are not additive. Review found no blocking production finding; controls include suspended speculative navigation, StrictMode, normalized no-ops, duplicate unrelated query keys, fresh-click navigation ownership and unknown-result retry.
- Independent exact-product TypeScript and focused source lint passed. Including injected review-only harnesses in a broad TypeScript scan produced test-fixture diagnostics; those harnesses were excluded from the exact-product static check, and are not claimed to pass that broader scan.
- Frontend-only aggregate: **53 files / 732 passed**. This explicitly excludes the Go-backed protocol entry suite and the external private-PostgreSQL probe. Their historical failure/fixture requirements remain; this is not a full-default web pass.
- Nonincremental product TypeScript and changed-file ESLint passed. Full ESLint exited 0 with four pre-existing warnings in unchanged files.
- An initial memo-based development attempt failed React compiler lint. It was replaced with explicit owner state; no lint rule or compiler check was disabled.
- No production build was run under the shared disk constraint. No dependency installation, full clone, backend/PG run or real-browser qualification was performed.

Commands from `web`:

```sh
npm test -- features/mail/workspace-compose-navigation.test.tsx --maxWorkers=1
npm test -- features/mail/workspace-compose-navigation.test.tsx features/mail/workspace-events.test.tsx features/mail/components/sent-folder-actions.test.tsx components/company/compose.test.tsx components/company/compose-send-lifecycle.test.tsx components/company/compose-recipient-reload.test.tsx features/mail/draft-writer.test.ts features/mail/draft-writer-reload.test.ts features/mail/draft-writer-snapshot.test.ts lib/session-safety.test.ts lib/compose-identity.test.ts --maxWorkers=2
npm test -- --maxWorkers=2 --exclude components/company/r5-protocol.test.tsx --exclude r5-external-batch-probe.test.tsx
npx tsc --noEmit --incremental false
npx eslint features/mail/workspace.tsx features/mail/workspace-compose-navigation.test.tsx
npm run lint
```

## Acceptance boundary

This is client-component navigation/completion qualification with controlled transport. It is not browser history, real mail delivery, backend authorization/persistence, or PostgreSQL integration qualification. It does not close the broader pre-dispatch navigation cancellation policy, P5/P9 lifecycle acceptance or their dependencies.

R5-P5-120 and R5-P9-040 remain open; central completion remains **10/171** and historical F52 remains failed. Compose, DraftWriter, transport/session, backend/API, dependencies, test configuration and CI are byte-identical to PR47. No merge, deployment, migration, real mail or production database action occurred. Remote publication and head-specific CI remain separate delivery checks.
