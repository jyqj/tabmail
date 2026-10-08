# Independent review followup: route lifetime owns the observation

Root review added four actual-route regressions in `02b1156`, with the repeated webhook-label assertion corrected in `c50259e` before fixing product code. The formal fixed 40-case file has SHA-256 `f197d8c6cdb3cc883a635a9ca441b2fb15cc42aedb6da9beab407e75dffbcfb0`. The earlier 36 assertions are unchanged. Root retains the first test-draft output separately; its correction is not presented as a product fix.

The reviewer found that the shared SWR cache can outlive a route. The three pagination generations restart at zero on a new mount, and the domains key was fixed, allowing the prior route's completed rows/counts to appear before the new request's deferred revalidation. This is distinct from selecting A→B→A within one mounted route.

The author copied the exact two test commits and reproduced **0 PASS / 4 FAIL** on the original candidate product bytes, before editing them. The other 36 tests were explicitly filtered in that focused baseline; their pending entries are not executed skips or new passes. All four failures are real prior-visit rows still displayed, including both occurrences of the webhook URL.

Each of the four Session components now obtains a React `useId()` lifecycle identity and includes it in its SWR key. New visits must obtain a new observation. Pagination/filter generations still retire earlier selections; retry/refresh keep their current key; bearer-only rotation leaves the component and its in-flight read intact. API endpoints and query parameters remain unchanged.

| Final verification | Result |
| --- | --- |
| Complete same-byte fixed test file | 40 PASS / 0 FAIL/SKIP |
| Related run including those 40, CompanyAudit 8, session-safety 10 and API-base 3 | 61 PASS / 0 FAIL/SKIP, exit 0 |
| Full TypeScript, `--noEmit --incremental false` | exit 0 |
| Scoped ESLint, all changed pages/test/helper | exit 0 |

This followup adds zero TODOs. It completes the independent-review repair within the single four-list task. The original 10 PASS / 26 FAIL → 36 PASS and first 33 PASS / 3 FAIL diagnostics remain in the original evidence packet; no older run is relabeled as the final 40-case source.

`remount-followup.json` binds the test commits, actual author baseline commit, final source hashes and uncompressed raw-log hashes. The new `readonly-remount-*.gz` files retain the selected baseline and complete final related run plus tsc/lint stdout/stderr. The Vite native-loader advisory is preserved. Reproduction uses the same four-file command documented in README; the focused baseline additionally used `-t 'independent route remount'`.

Only actual UI component behavior with the controlled HTTP boundary is qualified. No shipping browser, real deployment, full frontend/private-fixture suite, PostgreSQL or parent acceptance gate is newly claimed.
