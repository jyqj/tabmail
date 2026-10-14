# Catalog revision 15 independent frontend review

**ACCEPT** for the catalog reconciliation and preservation of historical checks. Final independently reviewed source: `0e329e872685209486e966fe7d5cf031e3020034`, tree `0135d90c89bedef6bf9a44c4738944a485feea3f`. The independent ordinary clone was clean before and after every recorded test execution. This review completes **0 implementation TODOs**; the original parent count remains **10/171 accepted, 161 outstanding**.

## Exact execution chain

| Stage | Actual source | Result | Original receipt |
| --- | --- | --- | --- |
| Original transaction / compatibility / client CLIs | `26a30e3` / tree `437de071` | exits **1 / 1 / 1** | `baseline-receipt.json` |
| Independent reconciled data CLIs | `53d0d925` / tree `f4030eeb` | exits **0 / 0 / 0** | `candidate-data-cli-receipt.json` |
| First independent full five modules | `d79c5ba` / tree `01bd88c2` | **99 PASS, 1 FAIL**, 100 methods | `candidate-final-modules-receipt.json` |
| Corrected source first independent targeted run | `0e329e8` / tree `0135d90c` | **28 PASS, 2 ERROR** from ENOSPC, 30 methods | `candidate-pr23-followup-receipt.json` |
| Corrected source independent serial targeted run | same `0e329e8` / tree `0135d90c` and exact command | **30 PASS, 0 FAIL / ERROR / SKIP** | `candidate-pr23-serial-receipt.json` |
| Author full five modules, independently opened and verified | same `0e329e8` / tree `0135d90c` | **100 PASS, 0 FAIL / ERROR / SKIP** | `author-final-full-suite-verified.json` |

The independent final 30 methods are the complete 26-method transactions module and both revision 14 historical positives plus both revision 15 current positives. The author’s final full-suite receipt and all 100 distinct PASS lines were independently read, with log and module hashes checked against the frontend candidate. That author run is attributed separately; the earlier independent 100-method failure is not relabeled as a full independent pass on a later commit.

## Review findings and resolution

The submitted data correctly records a changed existing `CreateAPIKey` body and two new functions, `insertAPIKeyRows` and `CreateAPIKeyAuthorized`. The trusted seed transaction, caller-owned row helper, and authorized transaction have distinct responsibilities. The authorization trace accurately records tenant-before-user ownership, current JWT/home identity and permissions, profile/zone NOWAIT behavior, key → usage → required audit writes, and output update only after Commit. The manual review limits the error-cause guarantee to raw PgErrors observed by the outer defer; the existing profile helper’s typed Conflict does not preserve that cause. It also retains uncertain-commit and future-writer limits.

The initial full run exposed a historical PR23 assertion that still required the current function to retain its old source review, hash, and classification. The correction preserves the entire original method body and AST, and runs it against the exact clean public fdea revision 14 ordinary clone. Both before and finally identity checks include its independent Git directory. Current revision 15 checks still validate the new function body, classification, manual review and complete old review archive. The preservation test reconstructs the transaction test module from the original source with only the permitted imports, context wrapper and count change; all other original assertions remain unchanged.

The first corrected targeted execution encountered actual disk exhaustion while checking out the complete historical revision 14 clone. The transactions module and both current revision 15 positives passed; two historical positives errored before a valid checkout. The original errors remain in this bundle. Completed, committed inactive worktrees were reclaimed. During the author's final 100-test run, root also removed 65 large, rebuildable compiler `.a` files from the established `GOCACHE`, after verifying that they belonged to Tabmail; these files totaled **1,172,124,994 bytes**. Source files, `go-mod` module packages, the compiler, standard-library and AST-tool caches, and original evidence were preserved. The independent retry used the same source and exact command in serial execution, with fixtures, collectors, assertions, and identity requirements unchanged.

## Preservation and boundaries

- 120 historical files and 174 protected source files match the public Git objects byte-for-byte, including blob, SHA256 and length checks.
- All 72 prior `REVISION*` constants and five catalog negative controls retain their original source. The catalog retains its original 31 methods and adds two current positives; all 26 transaction methods remain.
- The 401 unchanged functions retain all manual and unknown fields; all 64 original file reviews are preserved. `CreateAPIKey` archives its four old manual fields and complete original `source_review` before rebinding the current review.
- Static facts are 64 PostgreSQL files, 404 functions, 505 lexical SQL calls, 19 migrations, 133 routes, 136 client branches, and 95 compatibility closure files.
- Current wire execution, whole-product CI, dependencies, performance and parent acceptance remain separate requirements. `runtime_verified=false`, `product_green=false`, and `task_complete=false`; this review does not change #56’s draft boundary.

## Key raw log hashes

| Log | Bytes | SHA256 |
| --- | ---: | --- |
| First independent full-suite failure | 19714 | `9dd414041041d7be8f4b9d230856ef4c65d7f86e270617b562cacd5706f4af88` |
| First corrected targeted ENOSPC run | 9503 | `a225f949992d87fde204305261532b758c9fbb5d6546194b9db1133b2a86d5ed` |
| Final independent targeted PASS | 4898 | `92fb6df6b5f140ca8be0326c77852f3a0a06580e654595b13d2ef8557442b626` |
| Final author full-suite PASS | 18540 | `774ad64339dc13b41ae6287718604e92a4bcd284fcbbbd8fd349e33bf7614f7e` |

See `review-summary.json`, the original per-stage receipts, the independent static-review receipts and the archived original outputs for the complete identity and execution chain.
