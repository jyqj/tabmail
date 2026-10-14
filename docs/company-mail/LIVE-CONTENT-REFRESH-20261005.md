# Workspace live-content refresh (bounded)

## Source and defect

- Baseline: draft PR #50 `7a61bedef174ed82714323c743f33f6438ca3479`, verified local equivalent `e6d74aa5ffb388938d33051ff769e1884ad48afe`, exact tree `09ca872b9238df82f0d809c4092026edd47367b5`.
- Independently accepted implementation and author harness: `818a93354fc98288485f8a88951e12bca20eda4f`, tree `3a3a7ca0d26e517cfda32cb242ccfbb94ed9dda8`.
- PR #50 repaired workspace invalidation of receipt aggregates. Already-disclosed submission body, structured BCC and attachments remain component-local rather than SWR data. Manual Refresh, a meaningful SSE event and reconnect resync could therefore leave those private surfaces stale until their separate 15-second authorization cycle or local Retry loading action.
- The baseline reproduction mounts the real workspace with company receipts, compatibility receipts and selected sent assets. Each trigger revalidates metadata but issues no new live content/attachment read; the prior synthetic private body and files remain visible.

## Narrow correction

Three production modules change: the workspace, its existing live reader and a small new context module. A workspace-local numeric refresh revision reaches only mounted live readers inside that workspace. Manual Refresh and existing non-ping SSE/resync paths advance it. The revision is an effect dependency, never a disclosure/remount key, permission flag, message payload or SWR cache entry.

The existing live reader still authorizes content first and then reads attachments, preserving current-session assertions, selected-submission/session keys, request abortion, disposed/epoch checks and its 15-second timer. The current-session guard prevents a retired callback from directly signalling another session's reader. Refresh callback dependencies remain stable, so revision changes do not restart the event stream.

Closed disclosures stay closed and make no private reads. Open disclosures and expanded HTML retain their nodes/state. Ping and unrelated query changes do not trigger private reads or wipes. Revoked aggregate capabilities unmount disclosure; regrant stays closed. Failed live content or attachment reauthorization clears body, BCC and attachment metadata. Superseded reads cannot overwrite or clear the newer view.

Metadata and live reads may start concurrently. The context grants no access: the live endpoint remains the authorization boundary. As before, currently displayed content may remain during an in-progress read and is cleared when a read failure or capability revocation is observed. This does not claim instantaneous revocation before the client observes it.

PR #50's SWR key fix, editor/send/navigation ownership, backend contracts, session implementation, lazy disclosure and read ordering are unchanged. Refresh does not retry or send an outbound message.

## Reproduction and verification

Only controlled fetch responses and the Next router URL store are substituted. Actual React components, SWR, parsers, session logic and streaming SSE reconnect execute against synthetic identifiers and responses.

- Frozen author harness `workspace-live-content-refresh.test.tsx` SHA256: `f94e5385866bf3b73754f3de280f79d40f7a27dc922c5e8e4ab2ee1895ecfb0f`.
- Before production edits: **40 failed / 2 passed**. After the correction, unchanged harness: **42/42 passed**. Several lifecycle failures are prerequisites missing the refresh read, not separate claims of baseline session-isolation defects.
- An earlier fixture-setup run lacked the receipt DTO's required `retry_block_reason` and was corrected before freezing. That unsuccessful setup is retained separately and is not accepted defect evidence.
- Author frontend-only selection: **56 files / 822 passed**, including prior receipt-key, navigation, editor-opening, send, content, parser and session regressions. Commands explicitly exclude `components/company/r5-protocol.test.tsx` and `r5-external-batch-probe.test.tsx`; the opt-in shared-protocol fixture suite remains excluded by unchanged configuration.
- Author `tsc --noEmit --incremental false`: **exit 0**. Focused lint: **exit 0**, one unchanged epoch cleanup warning verified against baseline. Full lint: **exit 0**, four unchanged warnings. Diff whitespace check: **passed**.
- Independent frozen baseline-first oracle: **41 failed / 15 passed**, then **56/56 passed** unchanged on the exact implementation. Eight separately identified post-candidate supplemental cases also pass. Independent frontend selection: **58 files / 886 passed**, including the 64 independent cases and overlapping product tests. Independent typecheck passes; production lint has zero errors and the same baseline warning. **No blocking finding** in this bounded slice.
- Coverage includes all three entry paths and triggers; ordered body/attachment reads; 403/404/500/network/final-401 and invalid/redacted DTO clearing; lazy/collapsed/unselected views; capability loss/regrant; repeated/late/partial read races; selection/session/logout/unmount; token rotation and renewal; preserved reader/HTML/disclosure nodes; no private canaries in observed SWR cache; outside-workspace isolation; and React commit/suspension semantics.
- Counts overlap and must not be added. Exact remote publication/tree and CI status remain separate verification steps.

## Limits and remaining work

This closes the bounded PR #50 gap for already-mounted authorized live submission readers inside MailWorkspace. It is synthetic React/jsdom verification, not real-browser rendering, live-service integration, backend privacy/capability qualification, exhaustive event-load/starvation testing or production readiness.

No build, actual browser, live mailbox, real message content, backend, Go/PostgreSQL fixture, credential, dependency/lockfile/workflow change, merge or deployment was involved. Standalone readers outside MailWorkspace retain their existing authorization cycle. R5-P7-090, R5-P7-100, R5-P9-040, R5-P9-060 and their dependencies remain open; central completion stays **10/171**. Historical failures and earlier reports retain their original meaning.
