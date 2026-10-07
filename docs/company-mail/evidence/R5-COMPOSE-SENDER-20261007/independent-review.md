# Compose sender operation independent review — 2026-10-07

## Scope and identity

Independent read-only review of the sender selection and pending upload/preview changes at author commit `575c2a30cdad3be8a11c0c8b2f573b0de9fe0cc9`, followed by the preview response validation at `292eb4b108a1b6fb6ab27e0856afcb33780bc294`.

- `web/components/company/compose.tsx` final SHA-256: `f918ae427ac4ee2460ea42cafbfe0e6af927d2861fe97476420f94bc80865f68`
- `web/components/company/compose-sender-ownership.test.tsx` SHA-256: `07e47551fe05e6feecf4793ac8970644fadf17dd01077e1a27f52e1b6c2c11ea`
- `web/components/company/compose-preview-response.test.tsx` SHA-256: `78f575566e68cf31e787f7c02382488a7195cf5ff630137e342707c02b3a0519`

## Conclusion

No blocking regression found in the ownership change or the preview validation follow-up.

- Sender refs change in layout effects after commit. An abandoned suspended render cannot revoke a still-current upload.
- StrictMode effect replay is separated by the existing mount epoch. Permission restoration permits a fresh explicit action without reviving the old generation.
- Generation changes on selected identity change or committed allowed-to-denied transition. Equivalent mailbox object updates and list ordering do not unnecessarily revoke work.
- Upload ownership tracks sender authorization; preview also tracks current send/template eligibility. Stale success cannot attach content, and stale failures become the existing quiet AbortError.
- The existing synchronous `useAction.running` ref enforces one action per hook instance. Its old finally cannot clear a newer action's busy state in the same instance; a newly mounted editor has separate hook state.
- The unavailable option accurately preserves the controlled select's current identity, and switching sender retains the existing explicit attachment/template reset behavior.

Reviewed the author tests' actual mounted Compose, SWR, session and request path. The author reports 21 new cases and 178 related cases passing; those executions were not repeated by this reviewer.

## Preview response follow-up independently reviewed

The initial review found that the TypeScript-only `company<RenderedTemplate>` assertion allowed incompatible HTTP 200 values to reach React children. The OpenAPI `CompanyRenderedTemplate` schema requires `subject`, `text_body`, and `html_body` to be strings. The follow-up now checks all three fields in the endpoint-local completion callback before publishing preview state.

The wrapper checks ownership before entering that callback. A malformed response from a revoked, unmounted, or superseded session is therefore discarded as the existing quiet AbortError, without a validation toast over the current interaction. A malformed response for the current owner throws a regular error through the existing action handler; it preserves user input and any previous valid preview. Valid empty strings remain accepted. There is no await between the ownership check, runtime field check, and state publication, and the existing single-flight action ref continues to prevent an old finally from clearing a newer action in the same instance.

The new 19-case suite exercises the actual mounted editor and request parser with synthetic fetch. Its malformed HTTP 200 cases include object/null/missing values for all three required fields, numeric subject, and null/array/missing data. An error boundary records actual render failures. Positive controls cover valid and empty strings, retaining the prior preview on a malformed refresh, a successful retry, and silent stale malformed responses. The author records the same frozen test bytes at baseline 14 failed / 5 passed, candidate 19 passed, and 84 related Compose cases passing, with TypeScript and changed-file ESLint exiting 0. This reviewer inspected the code, tests, and hashes without repeating those executions. The original 21-case ownership test hash remains unchanged.

## Limits

No independent full test rerun, real browser, native file chooser, backend, PostgreSQL, actual upload cleanup, or live mail validation. Previously displayed preview content and already attached files are outside the pending-result scope.
