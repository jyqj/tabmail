# Recipient display after draft reload: bounded R5-P5-030 progress

## Change and source

- Baseline: [draft PR #44](https://github.com/jyqj/tabmail/pull/44), `1b85ff3e8a700fc179b64fdb93f951dbfb67b30f`.
- Published implementation: `02d9ceada80f6b224eae2735d16e0bd208493d84`, tree `d81d545aa225179f38263f588082bc11feccd8c4`, identical to the independently tested implementation `c983208d352f4c6e6f861c1eb8df688a35486e13`.
- A successful explicit server-draft reload replaced the Compose payload but did not replace the recipient fields' mount-local raw text. To/Cc/Bcc could still display discarded local or old addresses while submission used the newly loaded server recipients.
- Compose now advances a recipient-input reset generation only after a confirmed reload succeeds. The recipient inputs remount using the authoritative payload, including empty or omitted Cc/Bcc. This also resets discarded separators when parsed recipients happen to be unchanged.
- Ordinary typing, unrelated rerenders, explicit-save/autosave acknowledgements and keep-as-new recovery do not reset recipient inputs. Failed, canceled, invalid-identity/revision or stale-session reloads do not advance the generation. Parsing and server validation behavior remain unchanged.

Production/test changes:

| Path | Git blob |
| --- | --- |
| `web/components/company/compose.tsx` | `7e0ea4229a63a5e29678ff20d8b57df22c2e8fea` |
| `web/components/company/compose-recipient-reload.test.tsx` | `a1d39fa3ff86c7f6a1a8f67932ce349ff088256d` |

The PR44 pure writer, previously reviewed workspace and message pane, request transport, event parser and session owner are byte-identical to baseline. No backend/API, dependency/lockfile, build/test configuration, R5 infrastructure or CI change.

## Verification

The author tests were frozen and run against the exact baseline before the production edit. They mount real Compose, recipient fields, DraftWriter, company/request transport, session guards and SWR. Only fetch and toast delivery are substituted. Synthetic HTTP responses and captured submission requests never contact a real account or mail service.

- Frozen author suite: baseline **9 failed / 8 passed**; candidate **17 passed**. Test SHA256: `ce7ed60e1bdcfba65f3a8818a7d40df13b907651e5f84cc40d11a04c44af2fd3`.
- Baseline failures include an observed mismatch between displayed parsed To/Cc/Bcc and the synthetic submission's persisted recipients. Candidate tests verify display, loaded revision, no stale rewrite, and the exact submit idempotency key.
- Coverage includes changed and empty/omitted recipients, same parsed recipients with discarded raw separators, repeated reloads, cancel, failed reads, wrong draft identity, invalid revision, invalid-recipient save rejection and recovery, partial typing through an older autosave acknowledgement, keep-as-new, pending-read locks/repeated clicks, session change, and replacement keyed draft isolation.
- Seven-file related author selection: **99 passed**, including existing Compose, send-policy, writer/reload/snapshot and workspace tests.
- Independent tests were frozen before candidate inspection: baseline **8 failed / 11 passed**; exact candidate **19 passed**. Frozen test SHA256: `62b7ddd037bc0b778909d4a7ea3dd2b6c2d31bd1a6e96d232b21d118acd3c605`. The independent controlled save/send test compares displayed parsed recipients against the outgoing write and verifies loaded revision/idempotency. Independent source/test lint, nonincremental typecheck, 93 related tests and 39 SSE/transport tests also passed; scoped review approved with no blocking finding. Its broader synthetic selection passed 692 tests across 51 files, including the 19 independent cases and explicitly excluding the two environment-blocked suites described below; this is not a full integration pass.
- Nonincremental `tsc --noEmit --incremental false` and strict changed-file ESLint: passed.
- Full `npm run lint`: exit 0 with four existing warnings in unchanged files.
- Production `npm run build`: passed, including **29/29 static pages**. An initial environment-only attempt failed because Turbopack rejects a dependency symlink outside its root; reusing the same installed dependency files through local hardlinks resolved that setup issue without an installation, dependency update or configuration change.
- Full web `npm test -- --maxWorkers=2`: **673 passed / 1 failed / 3 skipped**, **50 passed / 2 failed files**. The failures are the existing Go-backed protocol suite setup (`Go: Unknown option: test`) and the explicitly required private PostgreSQL fixture. The failed suite sources/catalog are unchanged; the byte-identical PR44 baseline's prior full run has the same two failures and 656 passes. No assertion, timeout or skip configuration was relaxed.

Focused command:

```sh
cd web
npm test -- --maxWorkers=2 components/company/compose-recipient-reload.test.tsx \
  components/company/compose.test.tsx components/company/send-policy.test.tsx \
  features/mail/draft-writer.test.ts features/mail/draft-writer-snapshot.test.ts \
  features/mail/draft-writer-reload.test.ts features/mail/workspace-events.test.tsx
```

## Acceptance boundary

The full suite is **not green**, and skipped tests are not passes. This is a mounted client-component correction with controlled fetch, not a real-browser or Go/PostgreSQL draft-lifecycle qualification. The keyed-draft replacement test uses the existing workspace key contract; workspace code was not changed.

R5-P5-030 and dependencies remain open; central completion stays **10/171** and historical F52 remains failed. No real credentials/accounts, actual mail, live service or production database was used. No merge or deployment occurred. Publication and exact remote-head CI status are separate from this local verification.
