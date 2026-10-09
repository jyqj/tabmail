# Receipt aggregate refresh (bounded)

## Source and observed defect

- Baseline: published draft PR #49 `24f5bd4edcd50dbb4702b5fa109672697a8f0342`; its verified local equivalent is `b83e24c370a5fce06256d32a1885a0aa7ca05344`, exact tree `f64c0715b190c58c734a7f83eb4ea77e0a2bdd36`.
- Frozen implementation and author harness: `60e064fccba7b8b7ff09bb10c05380c57834a813`, tree `b8d9fa636f37ef952eb273917605eb1878481431`.
- The workspace invalidator already covered the company submission list but omitted the `submission` detail key, the `legacy-outbound-receipt` detail key and the scalar `legacy-outbound-receipts` list key.
- In the mounted workspace, manual Refresh, an SSE invalidation and normal EOF reconnect all left the selected company aggregate at its old count while its list updated. Compatibility list and detail both stayed stale. Explicit refresh did not retry an errored aggregate or promptly observe revoked content capabilities and denied detail access. Company polling could eventually mask the defect; compatibility readers have no polling interval.

## Narrow correction

Only `web/features/mail/workspace.tsx` changes in production. The two missing array-key families and the exact scalar compatibility-list key are added to the existing invalidation predicate. The namespace, current-session check, remaining key families and event transport are unchanged.

Refresh now revalidates these mounted aggregate readers through their original authenticated fetchers and strict receipt parsers. A denied detail suppresses cached aggregate display and disclosure through existing consumer behavior. A returned capability remains the prerequisite for displaying the content-disclosure control; revalidation never opens it. Account or tenant changes still abort old reads, and late responses cannot enter the replacement session. Token-only rotation keeps the current scope and uses the new credential on refresh.

## Reproduction and checks

The new `workspace-receipt-refresh.test.tsx` mounts the actual workspace, receipt consumers, SWR, session logic, DTO parsers and SSE transport. Only Next router location and fetch are substituted. All identifiers, sessions and responses are synthetic. No content or attachment endpoint is requested by this harness.

- Frozen 20-case harness SHA256: `e6e05ee43ec4c295c18e49fd723d3774b56a32bb5ec5409a28bcb94d38a97355`.
- Before production edits: **18 failed / 2 passed**. The in-flight session tests fail at the prerequisite missing refresh request, not evidence of a baseline session-isolation vulnerability. Session/key-family negative controls and mailbox stream-revocation controls already pass.
- Unchanged harness on candidate: **20/20 passed**. The three refresh paths update both entry points; error recovery, capability revocation/re-grant, denied detail, old account/tenant response fencing and token rotation pass. Refresh remains metadata-only; ping frames do not invalidate unrelated keys.
- Related regression selection: **13 files / 219 passed**, including receipt/live-content lifecycle, existing workspace invalidation, compose navigation, editor-opening ownership, received/sent actions, transport, session and receipt parsers.
- Frontend-only selection: **55 files / 780 passed**. `components/company/r5-protocol.test.tsx` and `r5-external-batch-probe.test.tsx` are explicitly excluded; the shared-protocol fixture suite stays excluded by unchanged configuration. This is not the full default suite or backend/PostgreSQL qualification.
- `tsc --noEmit --incremental false`: **exit 0**. Focused production/harness lint: **exit 0**. Full lint: **exit 0**, with four existing warnings in unchanged files. `git diff --check`: **passed**.
- Independent baseline-first frozen harness: **13 failed / 3 passed**, then **16/16 passed** on the identical candidate. Its related selection passes **19 files / 361 tests**; candidate typecheck and changed-file lint pass. The reviewer found no blocking issue in this bounded slice.
- Independent controls additionally verify that revoking the aggregate capability unmounts an already-open synthetic private disclosure, that re-grant stays closed, and that private body/BCC/attachment/token canaries never enter SWR. Its frozen test-only cache-capture instrumentation produced a separate exploratory lint warning/error; passing changed-file lint does not claim that reviewer fixture is lint-clean.
- Counts overlap and must not be added. Publication and exact-head CI checks remain separate gates.

## Explicit remaining gap and limits

Already-disclosed live content and its attachments use a separate, non-SWR reader with its own 15-second authorization cycle and local Retry loading action. Workspace Refresh/SSE reconnect still does not force that reader to reload while its aggregate capability remains true. This gap is **open**, and this change does not claim to refresh every receipt reader or alter live-content retention/security semantics.

No build, actual browser, live mailbox, backend, Go/PostgreSQL fixture, real message content or deployment was exercised. Dependencies, lockfiles, workflows, backend contracts, session fences and historical evidence are unchanged. R5-P7-090, R5-P7-100, R5-P9-040, R5-P9-060 and their dependencies remain open; central completion stays **10/171**. Historical whole-PG/audit/CI failures are not promoted by these frontend results. No merge or deployment is authorized.
