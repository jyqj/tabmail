# Draft reload serialization: bounded R5-P5-030 progress

## Change and source

- Baseline: [draft PR #43](https://github.com/jyqj/tabmail/pull/43), `e80a057adefb1dfe7ec0389a6b03430c01222b2d`.
- Published implementation: `0648ddbf60522f14506a69162e140e457215299a`, whole tree `b35c3182aa41a77bdc2bfe115716dfa89c930a28`, identical to the independently reviewed implementation `738c34d7b047c222742a81a3a9cac8edae324cc0`.
- `DraftWriter.reload()` previously waited for the existing promise tail without becoming part of that tail. A later save or reload could start while the earlier read was pending. The earlier read could then replace a newer acknowledged snapshot and make a following save send an obsolete CAS revision.
- Reload now reserves the same promise lane used by saves before returning. Saves and reloads execute in invocation order, including when a preceding operation rejects. A later save uses the successfully reloaded revision or, after a rejected reload, the unchanged acknowledged snapshot and existing recovery state.
- Identity/revision validation, detached response snapshots, exact uncertain-write replay, conflict latching, and checks before/after transport reads remain unchanged. Only a valid explicit reload clears pending/conflict state.

Only these production/test files change:

| Path | Git blob |
| --- | --- |
| `web/features/mail/draft-writer.ts` | `aebfd5a175822bace116c44f56f1bfdaa53e08e6` |
| `web/features/mail/draft-writer-reload.test.ts` | `ab412626cc641ea97f484e52b8460484ebcf4b23` |

Previously reviewed product files remain unchanged: message pane `c62898347604e2bc6f9f2f4dc33c6ba1c8115147`, workspace `6fb93714b0b1e59e10bd181258ff9aab16b5c61d`, SSE request transport `1e9804d56f9f910f70640d174d56db61bf5d0152`, and event parser `738d7d92e8dab4699bf383cdffa082b63b912bf7`.

## Verification

The author suite was written and run against the exact baseline before the implementation edit. Controlled read/write promises reproduce a delayed revision-3 read replacing an acknowledged revision-4 save, followed by a rejected stale revision-3 CAS. The same test passes after serialization, with consecutive writes based on revisions 3 and 4.

- Frozen author suite: exact baseline **10 failed / 5 passed**; candidate **15 passed**. It covers same-turn and in-flight reload/save calls, save/reload/save ordering, two reads with reversed response readiness, rejected/invalid reads, queue recovery, conflict latching, exact uncertain replay, valid-reload recovery, and close/session boundaries with a separate replacement writer. Test SHA256: `1824bd9eb6c0a05e3da0e3052c95f164aee88d96f7471e4f5603e16c03a56555`.
- Related five-file writer/snapshot/compose/send-policy selection: **75 passed**.
- Independent tests were frozen before candidate or author-test inspection: exact baseline **17 failed / 5 passed**; candidate **22 passed**. They additionally exercise attachment-payload ownership, reloaded-content no-op detection, repeated-save coalescing, invalid revision classes, and borrowed/returned response mutation. Frozen test SHA256: `113ecc9d6d48e9bea102ed666e71b18fb7619e32ce81b2424a626725c042b3ff`.
- Independent plus author and existing writer/snapshot suites: **88 passed** across four files. Independent nonincremental typecheck, strict writer/test ESLint, and diff checks passed. No blocking finding was reported for the exact implementation. The reviewer did not duplicate the full-web suite, production build, or browser testing.
- `tsc --noEmit --incremental false` and strict changed-file ESLint: passed.
- Full `npm run lint`: exit 0, with four existing warnings in untouched files.
- Production `npm run build`: passed, including **29/29 static pages**.
- Full web: **656 passed / 1 failed / 3 skipped**, with **49 passed / 2 failed files**. The remaining failures are the Go-backed protocol suite setup (`Go: Unknown option: test`) and the explicitly required private PostgreSQL fixture. Both failures were reproduced separately against the exact baseline. No timeout, assertion, or suite configuration was relaxed.

Focused command:

```sh
cd web
npm test -- features/mail/draft-writer-reload.test.ts \
  features/mail/draft-writer.test.ts features/mail/draft-writer-snapshot.test.ts \
  components/company/compose.test.tsx components/company/send-policy.test.tsx
```

## Acceptance boundary

The full suite is **not green**, and skipped tests are not passes. This is a reusable client revision-lane correction. The controlled unit reproduction does not establish the same race through the mounted Compose UI, which has its own busy/lock controls. Real browser/Go/PostgreSQL draft lifecycle validation remains unverified.

R5-P5-030 and its dependencies remain open; accepted progress stays **10/171** and historical F52 remains failed. No backend/API contract, dependency, session owner, previously reviewed SSE/workspace/message-action code, R5 infrastructure/registry/preparation/receipt, or CI configuration changes. Validation used no live accounts/mail, external mail services, or production databases/credentials. Nothing was merged or deployed.
