# ADVANCE-09 — administrative read-only lists

Issue [#205](https://github.com/jyqj/tabmail/issues/205), parents R5-P9-100 / R5-P9-090. This closes **one** bounded product task across the ingest queue, webhook deliveries, administrative audit and domain inventory. The four consumers, test cases and evidence files are not separate TODOs. Original parent checkboxes, dependencies and gates are unchanged.

Independent review subsequently found a route-remount cache gap in candidate `36455c9`. The [followup](remount-followup.md) records the four additional frozen failures, lifecycle-identity fix and final **40 fixed / 61 related PASS**. The original 36-case baseline/candidate history below is retained under its original source hashes.

## Product behavior

These four actual route pages previously discarded SWR errors and background-validation state. A failed initial request appeared as an empty result, while a failed refresh left old rows, delivery diagnostics and counts visible. Ingest and webhooks also reported a total of zero before any successful observation. Audit had no explicit refresh/retry control, and all three paginated pages could strand an operator on a confirmed empty later page.

The pages now share a small read-state feedback component. A pending request announces loading/refreshing, a failed request retains an alert and an explicit retry, and retry is disabled until the current read settles. Cached rows, diagnostics and totals are hidden during pending/error states. Unknown numerical observations display a dash; zero is reserved for an actual successful empty response. Audit has the same explicit refresh affordance as the other lists.

Retry and refresh perform the existing GET for the current query; they do not introduce writes or replay unrelated commands. Filters stay editable and survive failure/retry. A confirmed empty later page still exposes Previous. Pagination and filter changes begin a new read generation so returning A→B→A cannot restore the old A rows/counts before SWR's deferred revalidation starts. Session scope changes reset page/filter state, while a bearer-only rotation preserves an in-flight read and uses the renewed token for the next request.

## Fixed baseline and candidate

The frozen test file is `web/app/(dashboard)/admin/readonly-list-state.test.tsx`, SHA-256 `ee7ad5d11b1e7b306684915f4f66ac20c1ad3017b823cdec90c009621db81174`. Its bytes were unchanged between all three executions below.

| Execution | Passed | Failed | Skipped | Exit |
| --- | ---: | ---: | ---: | ---: |
| Baseline `647688b` (four target pages identical to original batch `fdea217`) | 10 | 26 | 0 | 1 |
| First candidate diagnostic | 33 | 3 | 0 | 1 |
| Final fixed 36 in the related run | 36 | 0 | 0 | 0 |
| Final four-file related run, including the same 36 | 57 | 0 | 0 | 0 |

The first candidate's three remaining failures were not removed or relaxed. Returning from page 2 reused SWR's cached page 1 before the deferred revalidation began, despite the more recent observation that page 2 was empty. The product change added a generation for each pagination/filter selection. The final same-byte tests then pass and verify the actual GET sequence `1, 2, 2, 1` through failure, retry, confirmed empty page and Previous.

The suite mounts the actual four route components, actual dashboard header with SidebarProvider, tables, locale provider, session-scoped SWR and HTTP adapter. Only HTTP responses, viewport APIs and notification display are controlled. It covers initial pending and confirmed empty reads; 403/503 errors and real same-read retries; hidden cached rows, diagnostics and totals during/after refresh failure; current-query pagination/filter preservation; session changes with late former-owner responses; and legitimate bearer-only rotation.

The related run contains the new 36 plus existing CompanyAudit 8, session-safety 10 and API-base 3. These 57 include the new tests and must not be added to the 36 again. Final full nonincremental TypeScript and scoped ESLint both exited 0. The Vite native-loader advisory in the raw stderr is retained.

## Reproduction and evidence

Run from `web/`, using the existing lock-matching dependency tree:

```sh
/opt/codex/runtimes/codex-primary-runtime/dependencies/node/bin/node node_modules/vitest/vitest.mjs run 'app/(dashboard)/admin/readonly-list-state.test.tsx' features/company/audit-state.consumer.test.tsx lib/session-safety.test.ts lib/api/base.test.ts --maxWorkers=1 --no-file-parallelism --reporter=json
/opt/codex/runtimes/codex-primary-runtime/dependencies/node/bin/node node_modules/typescript/bin/tsc --noEmit --incremental false
/opt/codex/runtimes/codex-primary-runtime/dependencies/node/bin/node node_modules/eslint/bin/eslint.js 'app/(dashboard)/admin/readonly-list-state.test.tsx' 'app/(dashboard)/admin/ingest/page.tsx' 'app/(dashboard)/admin/webhooks/page.tsx' 'app/(dashboard)/admin/audit/page.tsx' 'app/(dashboard)/admin/domains/page.tsx' components/crud/list-read-state.tsx
```

`summary.json` records full baseline/original commits, final source and fixed-test hashes, raw uncompressed log hashes and terminal exits. The baseline, first-candidate and final-related JSON/stdout/stderr logs are preserved as gzip files, together with first/final tsc and ESLint output. The first candidate is a retained diagnostic execution, not a substitute for the final source-qualified run.

These are production component + controlled-network tests. No real API/PG deployment, browser shipping journey, private protocol fixture, cold build or full-web gate acquired new qualification from these results. Existing test exclusion/fixture policy and dependency files were not changed; no parent TODO is marked complete by this bounded improvement.
