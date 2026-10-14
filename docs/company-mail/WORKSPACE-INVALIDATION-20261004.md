# Open mail reader invalidation: bounded R5-P7-090 progress

## Scope and observed failure

- Baseline: `650d4807359fae0b625d2a16a5b43466a2466ce0` (draft PR #41).
- Tested local implementation: `67d66c8d9edb8bcfabe992ea04e74bda88d857f4`, tree `2db148dcfb91ac77b7d4838e08c81931609766fc`.
- Published equivalent implementation: `a888b259a7d69b2083033ae088815151a4288ec1`, with the identical whole tree and code/test blobs. Git push lacked interactive credentials; the connected GitHub API delivered the exact reviewed tree. The delivery commit after this implementation changes only this report and the targeted TODO note.
- `MailWorkspace` already revalidates scoped folder lists after a non-ping SSE event, an accepted connection's synthetic resync, or the visible Refresh action. Its allowlist omitted the selected message (`work-message`), attachment metadata (`inbound-attachments`) and open conversation (`mail-conversation`).
- As a result, the list could update while the selected message retained the previous state. Attachment and conversation readers have no periodic refresh, so even normal EOF reconnect plus resync could leave them stale for the mounted view's lifetime.
- The fix adds only those three existing reader keys to the existing session-scoped allowlist. It does not populate caches from event payloads, change reader authorization, replay writes, or touch the reviewed SSE transport/parser.

## Exact changed code

| Path | Git blob |
| --- | --- |
| `web/features/mail/workspace.tsx` | `6fb93714b0b1e59e10bd181258ff9aab16b5c61d` |
| `web/features/mail/workspace-events.test.tsx` | `cd503dc7ad92707ebe65ae994324431a6bc69c58` |

Protected baseline blobs remain unchanged: `web/lib/api/base.ts` is `1e9804d56f9f910f70640d174d56db61bf5d0152`; `web/lib/api/event-stream.ts` is `738d7d92e8dab4699bf383cdffa082b63b912bf7`.

## Reproduction and validation

All commands below ran with Node 24.19.0 and the existing locked web dependencies. The tests substitute fetch and the router location, while exercising the real workspace, message/attachment/conversation components, SWR, request/session owner, and ReadableStream SSE transport. They are deterministic mounted-component evidence, not real browser/backend acceptance.

- Initial reproduction: 3 failure cases (manual Refresh, wire invalidation, actual EOF then one-second reconnect) and 1 passing token-rotation control. Each failure showed that the folder was re-read but all three visible reader GETs were missing and their displays remained old.
- Final seven-case suite, copied unchanged onto baseline `650d480`: **6 failed / 1 passed**. Three additional regression cases cover current-session allowlist isolation and ping handling, old-account delayed responses, and old-tenant delayed responses. Baseline failures in the latter cases occur because the required detail revalidation never starts.
- Final candidate seven-case suite: **7 passed**. The complete web run also ran this final test blob successfully.
- Related mounted consumer/session/stream regression selection: **87 passed across 7 files**.
- `npx tsc --noEmit`: passed.
- `npx eslint features/mail/workspace.tsx features/mail/workspace-events.test.tsx --max-warnings 0`: passed.
- `npm run lint`: exit 0, with four existing warnings in untouched files.
- `npm run build`: passed, including type checking and **29/29 static pages**. An initial attempt with an external node_modules symlink was rejected by Turbopack's filesystem-root check; copying the already-installed dependency tree into this isolated checkout resolved that environment setup issue. No source/config/dependency change was needed.
- Full `npm test`: **627 passed / 1 failed / 3 skipped**, **47 passing files / 2 failed files**. This is **not a full-suite pass**. The two failure files are the existing Go-backed protocol setup (`Go: Unknown option: test`, required Go 1.25.7 unavailable) and the explicit private PostgreSQL fixture test (`explicit private fixture required`). Both failures were independently reproduced on exact baseline `650d480`. Skipped tests are not passes.

Focused command:

```sh
cd web
npm test -- --run features/mail/workspace-events.test.tsx \
  features/company/company-event-consumer.test.tsx \
  features/company/permission-editor-session.consumer.test.tsx \
  lib/session-safety.test.ts lib/api/base-stream.test.ts \
  lib/api/event-stream.test.ts lib/api/company-events.test.ts
```

## Independent review

The reviewer accepted exact implementation `67d66c8` / tree `2db148d` with **no blocking finding** in this bounded slice. The baseline was archived and the independent contract/cases frozen before the candidate source was inspected.

- Frozen independent suite: baseline **6 failures / 1 pass**, candidate **7 passes**. Contract SHA256 `2507075cc9f900ed87c6b1a0653216e01ae804d8877923147b91dfdb7c8e4f66`; test SHA256 `3b5c23551a25e6ec736d096afc85799e5c8250f1338427fc3bdfd42d55d1c913`.
- The independent suite additionally checks wire resync, EOF without manually injecting resync, bounded follow-on request counts, burst/ping behavior, excluded cache keys, message/mailbox navigation, stream cleanup and stable-token/changed-identity behavior.
- Two supplemental, non-frozen cases covering delayed detail/attachment results during message/mailbox switches: **2 passes**.
- Independent combined consumer/transport/session/receipt selection: **78 passes across 7 files**; TypeScript noEmit with incremental disabled and strict candidate lint passed.
- Independent full web suite: **636 passed / 1 failed / 3 skipped**. The extra nine passing cases are the seven frozen and two supplemental reviewer tests, not additional product acceptance. Both known failure files were reproduced with exact baseline source. The full suite remains not green.
- The reviewer verified unchanged transport blobs and bounded TODO wording. It did not independently run production build or a real browser. Existing per-event invalidation remains unchanged; this patch does not add event coalescing or debounce.

## Acceptance boundary

R5-P7-090 and its dependencies remain open. The central accepted count stays **10/171** and historical F52 remains failed. No R5 registry/preparation/receipt work, live accounts/mail, external services, production credentials, database fixture changes, merge or deployment were performed.

Real PostgreSQL commit-order inversion, cursor retention, multi-instance recovery, sustained browser sessions, complete revocation workflows and backend/browser acceptance remain unverified. The bounded fix establishes that existing workspace resync reaches these three mounted authoritative readers; it does not close the wider event-recovery or revocation TODOs.
