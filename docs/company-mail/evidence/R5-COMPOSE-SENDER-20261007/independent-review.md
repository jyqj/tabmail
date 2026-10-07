# Compose sender operation independent review — 2026-10-07

## Scope and identity

Independent read-only review of the sender selection and pending upload/preview changes at author commit `575c2a30cdad3be8a11c0c8b2f573b0de9fe0cc9`.

- `web/components/company/compose.tsx` SHA-256: `c42f9312ecb2e16339be4586337796b9f19163e58de60cabaca92963d924e2c9`
- `web/components/company/compose-sender-ownership.test.tsx` SHA-256: `07e47551fe05e6feecf4793ac8970644fadf17dd01077e1a27f52e1b6c2c11ea`

## Conclusion

No blocking regression found in the ownership change.

- Sender refs change in layout effects after commit. An abandoned suspended render cannot revoke a still-current upload.
- StrictMode effect replay is separated by the existing mount epoch. Permission restoration permits a fresh explicit action without reviving the old generation.
- Generation changes on selected identity change or committed allowed-to-denied transition. Equivalent mailbox object updates and list ordering do not unnecessarily revoke work.
- Upload ownership tracks sender authorization; preview also tracks current send/template eligibility. Stale success cannot attach content, and stale failures become the existing quiet AbortError.
- The existing synchronous `useAction.running` ref enforces one action per hook instance. Its old finally cannot clear a newer action's busy state in the same instance; a newly mounted editor has separate hook state.
- The unavailable option accurately preserves the controlled select's current identity, and switching sender retains the existing explicit attachment/template reset behavior.

Reviewed the author tests' actual mounted Compose, SWR, session and request path. The author reports 21 new cases and 178 related cases passing; those executions were not repeated by this reviewer.

## Existing follow-up outside the delivered diff

The preview transport still uses a TypeScript-only `company<RenderedTemplate>` assertion. `company` returns `res.data` without validating the three required strings. An authorized success response with object-valued `subject` or `text_body` would reach React children and fail rendering. This predates the ownership change. The OpenAPI `CompanyRenderedTemplate` schema requires `subject`, `text_body`, and `html_body` to be strings.

A small endpoint-local runtime shape check before publishing preview state would close this gap. Keep the ownership assertion before consuming or reporting the response; add malformed-success coverage without broad parser or catalog rewrites. No claim that this follow-up is fixed is made here.

## Limits

No independent full test rerun, real browser, native file chooser, backend, PostgreSQL, actual upload cleanup, or live mail validation. Previously displayed preview content and already attached files are outside the pending-result scope.

