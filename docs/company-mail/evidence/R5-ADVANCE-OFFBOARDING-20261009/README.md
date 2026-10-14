# ADVANCE-02: authoritative member reads for handover

Issue: #198. Parent scopes: R5-P4-120, R5-P9-070, R5-P9-100.

The real employee page previously passed retained SWR member rows to the handover panel without read state. A failed or pending refresh therefore left an old preview executable and accepted late preview/execution continuations after the latest directory observation was lost.

The page now passes explicit directory readiness, removes non-current member rows from the table and selection controls, and shows loading/empty states. The panel retires preview ownership on each committed read-state transition while preserving typed inputs. A successful retry requires a new explicit preview. Acknowledged execution receipts belong to the selected operation independently and survive their own post-execution directory refresh, without recreating an executable plan. Token-only rotation retains the original scope.

## Frozen regression

`web/features/company/offboarding-read-state.test.tsx` was written and executed before production edits at baseline `fdea2178759ce9842844926374ea98517bc4188b`. Its SHA256 is `52f4c04cd8692af8d72bb38642fa60bdf952efed989b25078304d7f2ec91616b`; exactly the same bytes produced **3 PASS / 12 FAIL / 0 SKIP** on the baseline and **15 PASS / 0 FAIL / 0 SKIP** on the candidate.

The regression mounts the actual EmployeesPage, Employees, OffboardingPanel, SWR, session layer and HTTP adapter; only fetch, confirmation and notifications are controlled. It covers 403/503 with retained cache, pending reads, identical-row recovery, late successful and failed continuations, acknowledged execution readback, account replacement and token-only rotation.

The candidate also ran the existing offboarding-panel, offboarding-ownership and employee-invitation-lifecycle files, for **64 distinct cases PASS / 0 FAIL / 0 SKIP**, including the 15 new cases. Do not add these overlapping totals. Complete nonincremental TypeScript and scoped ESLint exited 0. Production and test hashes are in summary.json. The original baseline failures are retained in the compressed raw Vitest output.

Commands, from web:

- `node node_modules/vitest/vitest.mjs run features/company/offboarding-read-state.test.tsx --reporter=json --outputFile=.../offboarding-baseline.json`
- `node node_modules/vitest/vitest.mjs run features/company/offboarding-read-state.test.tsx features/company/offboarding-panel.test.tsx features/company/offboarding-ownership.test.tsx features/company/employee-invitation-lifecycle.test.tsx --reporter=json --outputFile=.../offboarding-candidate.json`
- `node node_modules/typescript/bin/tsc --noEmit --incremental false`
- `node node_modules/eslint/bin/eslint.js features/company/offboarding-panel.tsx features/company/employees.tsx features/company/offboarding-read-state.test.tsx`

The existing dependency tree was reused read-only after verifying an identical package-lock SHA256. This component/HTTP-peer result does not constitute a real server, PostgreSQL, shipping-browser or whole-product qualification. Existing parent checkboxes, private-fixture requirements and other gates are unchanged.
