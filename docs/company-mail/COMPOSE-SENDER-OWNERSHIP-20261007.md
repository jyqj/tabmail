# Compose sender and attachment ownership: bounded R5-P9-050 progress

## Reproduced defect

Baseline: [PR #55](https://github.com/jyqj/tabmail/pull/55), commit
`cbc8c17ebd3599d5c92711d28088b0bf4edc3f8f`.
Its `web/components/company/compose.tsx` has SHA256
`bcd5ea166437e4347003aa60afa785a062dfc370fb05c7a820b2de8b949bacdb`.

The editor keeps its chosen mailbox in state but previously filtered every
unsendable mailbox out of the From selector. After that sender lost permission
or disappeared from the current mailbox list, the native select displayed the
first remaining sender while the editor still held the original mailbox ID.
The same mismatch occurred when opening a saved draft whose sender was already
unsendable. With no available sender, the selection became empty. Save/send
remained governed by the original state, making the displayed From misleading.

The attachment input and server-preview button also remained enabled without
current sender permission. In-flight uploads appended their attachment ID after
committed sender revocation/removal; restoring permission before the response
arrived revived that old operation. In-flight previews had the same restoration
problem. Errors from uploads/previews could produce obsolete toasts after the
editor unmounted.

These are mounted-component reproductions with deferred fetch responses and
native select/file controls. Existing input locking is respected: the tests do
not manufacture races by editing disabled inputs during upload or preview.

## Product behavior

Compose now keeps an explicitly unavailable, disabled option for its selected
sender. It explains that edits are preserved and an authorized sender must be
chosen. Permission changes do not select a different sender automatically.
Choosing an alternative explicitly preserves the subject, message and raw
recipient input, then saves against that chosen mailbox. The established
mailbox-switch behavior clears mailbox-specific attachment/template references.

The attachment input requires current `can_send`. Server preview requires
current send/template eligibility. Upload and preview share a small operation
wrapper that checks the editor's original session, committed mount, and
committed sender generation before dispatch and immediately before accepting a
success or error. Preview also checks the template/send eligibility generation.
A committed revocation/removal invalidates the outstanding operation even if
permission is subsequently restored. A fresh explicit choice remains available.
Speculative suspended renders do not mutate ownership, and StrictMode effect
replay preserves legitimate subsequent operations.

An upload already accepted by the server may have created an attachment object.
Discarding its stale UI completion does not cancel or roll back that object;
this change neither deletes it nor claims to verify its retention/GC path.

## Initial ownership regression and verification

The new file `web/components/company/compose-sender-ownership.test.tsx` was
written and run before changing production code. Its bytes were retained
through candidate verification:

- Test SHA256: `07e47551fe05e6feecf4793ac8970644fadf17dd01077e1a27f52e1b6c2c11ea`.
- Test Git blob: `056521a9e9a6e9c0ee99bc93d164a945a73fc1ce`.
- Initial ownership candidate Compose SHA256 (commit `575c2a3`): `c42f9312ecb2e16339be4586337796b9f19163e58de60cabaca92963d924e2c9`.
- Initial ownership candidate Compose Git blob: `accd236be978567201f1289f6e35e5b2f848fdfd`.

| Check | Observed result |
| --- | --- |
| Frozen 21-case suite on baseline Compose | **14 failed / 7 passed** |
| Same frozen suite on candidate Compose | **21 passed** |
| Related editor/writer/navigation/identity selection, 10 files | **178 passed** |
| `npx tsc --noEmit --incremental false` | Exit 0 |
| ESLint on both changed TypeScript files | Exit 0 |
| Full `npm run lint` | Exit 0; four existing warnings in unchanged files |
| `git diff --check` | Exit 0 |

Representative baseline assertions reported `sender-two` where the selected
From should remain `sender-one`, an empty selection when the list became empty,
an enabled server-preview button after revocation, and a visible
`late-attachment.txt` after its sender was removed. The candidate satisfies those
same assertions. The suite also checks explicit identity selection and actual
save request payloads, preservation of existing attachments, a fresh upload
after restoration, current-owner error feedback, stale errors, session changes,
same-identity token rotation, StrictMode, and speculative Suspense denial.

The related selection includes the existing Compose/template, recipient reload,
send lifecycle, DraftWriter save/reload/snapshot, workspace completion/opening,
and compose-identity suites. It does not replace or relax those existing tests.

Dependencies were installed with the existing lockfile using
`npm ci --ignore-scripts --no-audit --no-fund`. The locally installed Next.js
16.3.6 guide `node_modules/next/dist/docs/01-app/01-getting-started/05-server-and-client-components.md`
was read before editing, as required by `web/AGENTS.md`.

Commands, from `web`:

```sh
npx vitest run components/company/compose-sender-ownership.test.tsx --reporter=verbose
npx vitest run components/company/compose.test.tsx components/company/compose-recipient-reload.test.tsx components/company/compose-send-lifecycle.test.tsx components/company/compose-sender-ownership.test.tsx features/mail/draft-writer.test.ts features/mail/draft-writer-reload.test.ts features/mail/draft-writer-snapshot.test.ts features/mail/workspace-compose-navigation.test.tsx features/mail/workspace-editor-opening.test.tsx lib/compose-identity.test.ts --maxWorkers=2
npx tsc --noEmit --incremental false
npx eslint components/company/compose.tsx components/company/compose-sender-ownership.test.tsx
npm run lint
```

## Review follow-up: runtime preview fields

The review identified an adjacent response-integrity gap:
`company<RenderedTemplate>()` only supplies a TypeScript type, while an HTTP 200
response can carry incompatible runtime values. Object-valued `subject` or
`text_body` reached React children and crashed the mounted editor. Null,
missing, or other non-string fields could silently replace a valid preview.
The `CompanyRenderedTemplate` schema in [OpenAPI](../../internal/api/openapi.yaml)
requires `subject`, `text_body`, and `html_body` to be strings.

The preview completion callback now checks exactly those three string fields
after the existing operation wrapper confirms ownership and before changing
preview state. A malformed current-owner response reports
"Invalid template preview response; try again." and preserves the editor's
input and any previous valid preview. A response whose sender/mount/session
ownership has been lost is discarded silently before this validation runs.
Valid empty strings remain accepted, and a valid retry updates the preview.

The separate `web/components/company/compose-preview-response.test.tsx` was
frozen on `575c2a30cdad3be8a11c0c8b2f573b0de9fe0cc9` before the validation edit:

- New frozen suite on that baseline: **14 failed / 5 passed**.
- The same suite on the follow-up candidate: **19 passed**.
- Combined new/ownership/template/send/recipient Compose suites: **84 passed**
  across five files.
- Follow-up nonincremental TypeScript and changed-file ESLint: both exit 0.
- New frozen test SHA256: `78f575566e68cf31e787f7c02382488a7195cf5ff630137e342707c02b3a0519`.
- Follow-up Compose SHA256: `f918ae427ac4ee2460ea42cafbfe0e6af927d2861fe97476420f94bc80865f68`.
- The original 21-case ownership file retains its original SHA256 and meaning.

The new tests use HTTP 200 responses with object/null/missing values in each
required field, numeric subject and null/array/missing data; an error boundary
records actual React render failures. Positive controls cover valid strings,
empty strings, retained valid preview and successful retry, plus silent late
malformed responses after revoke/restore, unmount, and session change.

Follow-up commands, from `web`:

```sh
npx vitest run components/company/compose-preview-response.test.tsx --reporter=verbose
npx vitest run components/company/compose-preview-response.test.tsx components/company/compose-sender-ownership.test.tsx components/company/compose.test.tsx components/company/compose-send-lifecycle.test.tsx components/company/compose-recipient-reload.test.tsx --maxWorkers=2
npx tsc --noEmit --incremental false
npx eslint components/company/compose.tsx components/company/compose-preview-response.test.tsx
```

## Acceptance boundary

This is client-component verification with synthetic fetch, token values and
mail addresses. It does not qualify a real browser, native OS file picker,
production build, Go/HTTP/PostgreSQL integration, real upload retention or actual
mail delivery. The complete web suite was not run for this patch. Revocation
must reach a committed mailbox/eligibility update to invalidate an operation;
unobserved server-side changes still require server authorization. Existing
rendered previews and previously attached files are outside this pending-result
change. Pure URL/query changes that retain the editor are governed by the
existing workspace ownership behavior.

R5-P9-050, R5-P5-120 and their end-to-end dependencies remain open. This bounded
progress does not change the central **10/171** completion count or the historical
F52 failure status. Remote PR checks and independent review are separate from
these local verification results.
