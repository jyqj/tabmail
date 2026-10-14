# ADVANCE-06: template revocation preserves unsaved draft edits

Issue #202. Parent scopes: R5-P5-080, R5-P9-030, R5-P9-050.

The page previously refreshed a revoked template by replacing the entire selected editor with server data. This discarded unsaved name, subject, plain text, HTML and variables. An unsuccessful SWR revalidation could also return retained cached data, discard edits and leave an obsolete draft revision available for another save. Changing tabs while the revoke was pending retired the history view before its acknowledgement reached the editor.

Revocation is now owned by the page's selected template, with the original persisted baseline and a revision-review state committed alongside its current draft. Save/publish are blocked during the mutation and readback; users may keep typing. Only a successful explicit GET releases the revision fence. A three-way field merge adopts untouched server changes and preserves local edits. If both sides changed the same field, the complete local draft remains visible and a named snapshot comparison requires explicit approval to keep those edits at the reviewed revision; that choice does not itself save or repeat a revoke. Failed or unknown outcomes offer a GET-only revision refresh. Session/page/selection ownership guards obsolete continuations, and token-only rotation remains valid.

## Fixed verification

The actual template page, tabs, editor, version history, SWR, session layer and HTTP adapter were mounted against a controlled remote CAS peer. Only fetch, confirmation, ResizeObserver and notifications are controlled. The 18-case fixed file SHA256 is `89368cf0bfd56526f8f6be9fd315688db4604140320bef249bbeb88902993837`.

Before production edits, these same bytes produced **3 PASS / 15 FAIL / 0 SKIP**. The baseline HEAD contained only the preceding offboarding change; all three targeted template production files were byte-identical to original batch `fdea2178759ce9842844926374ea98517bc4188b`. The final candidate produced **18 PASS / 0 FAIL / 0 SKIP**. The six-file related selection, including these 18 cases, produced **95 PASS / 0 FAIL / 0 SKIP**. Do not add the overlapping counts. Complete nonincremental TypeScript and scoped ESLint exited 0 on final source; summary.json retains its file hashes.

An initial 21-case test draft contained three control scenarios that selected another editor only after the readback had already resolved. They were removed before production editing because they did not establish their stated late-response condition. The first raw exploratory output remains separately named `initial-draft-baseline`; only the subsequently frozen 18-case red/green pair is counted. The initial candidate's 95-case pass is also kept separately from the final candidate after the named comparison UI and revision-downgrade guard were added.

Commands, from web:

- `node node_modules/vitest/vitest.mjs run components/company/templates/version-revoke-editor.test.tsx --reporter=json --outputFile=.../version-revoke-baseline.json`
- `node node_modules/vitest/vitest.mjs run components/company/templates/version-revoke-editor.test.tsx components/company/templates/editor-save-ownership.test.tsx components/company/templates/versions-load-state.test.tsx components/company/templates/versions.test.tsx components/company/templates/library-load-state.test.tsx components/company/templates/library-retry-ownership.test.tsx --reporter=json --outputFile=.../version-revoke-final.json`
- `node node_modules/typescript/bin/tsc --noEmit --incremental false`
- `node node_modules/eslint/bin/eslint.js 'app/(dashboard)/company/templates/page.tsx' components/company/templates/editor.tsx components/company/templates/versions.tsx components/company/templates/version-revoke-editor.test.tsx`

This does not claim real PostgreSQL/server revocation, shipping-browser, private-fixture, complete frontend or whole-product qualification. All parent checkboxes and pre-existing gates remain open at their existing status.
