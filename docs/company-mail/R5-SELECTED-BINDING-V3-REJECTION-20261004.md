# R5 selected-binding v3 rejection evidence preservation

## Result and scope

The bounded `validate()` rejection-path defect is fixed: once the fresh binding is known to mismatch, diagnostic-retention failure must not replace that primary rejection. The exact-base regression reproduced **4 passes, 1 failure, and 3 errors out of 8** before the fix. After the fix, **all 8 new checks and 18 existing focused checks pass (26 total)** with zero failures, errors, skips, or expected failures.

This is synthetic Python regression evidence from a partial source extract, not a full checkout or formal qualification. The original failing logs remain retained privately and are not relabeled. [Safe evidence](evidence/R5-SELECTED-BINDING-V3-REJECTION-20261004/summary.json) records exact source/blob hashes, every check ID, commands, outcomes, and hashes/sizes of private logs without their raw contents or paths.

Base commit: `c31076ab0c445f0fdb02da08f0fb3bbf79520271`; base tree: `b460a04c4a0e51bc981f3fef4e5eae4c0bef55c7`. The 12 extracted original source files were verified against their exact base Git blobs, sizes, and SHA256 values before comparison. Python: `3.12.14`.

## Change

`validate()` now constructs the existing binding-mismatch `ValueError` before opening rejection diagnostics. It catches ordinary exceptions from retention construction, JSON retention, or context cleanup; retains the actual secondary exception in `retention_errors`; adds only exception type and errno to the printable note; and raises the original binding mismatch. It does not turn rejection into success. Process-control `BaseException` subclasses are outside this ordinary-error handler.

Eight new explicit checks cover mocked ENOSPC, retention bounds, constructor permission failure, context cleanup failure, successful observation retention followed by rejection, capture-error identity preservation, externally pinned envelope rejection before capture, and unchanged matching-binding success. The private synthetic error message is absent from the printable ENOSPC note. These mocks do not simulate a genuinely full filesystem or establish actual diagnostic-file durability.

The existing 18 focused checks cover `ProjectionTests`, `EnvelopeTests`, and `CaptureOrderingTests`. The existing source/archive `AuthorityTests` were not run in this partial extract. The new filename does not enter the existing `test_*.py` discovery route. Producer schema, binding rules, consumers, preparation, frozen routing, dependencies, catalogs, and product source remain unchanged.

## Reproduction

Run from the source root with diagnostic retention disabled and explicit import paths:

```sh
env -u R5_DIAGNOSTIC_ROOT PYTHONDONTWRITEBYTECODE=1 \
  PYTHONPATH=scripts:scripts/tests python -m unittest -v \
  r5_selected_binding_v3_rejection_checks \
  r5_selected_source_binding_v3_checks.ProjectionTests \
  r5_selected_source_binding_v3_checks.EnvelopeTests \
  r5_selected_source_binding_v3_checks.CaptureOrderingTests
```

For the red control, the same new regression test file was run alone against a separate copy containing the exact original helper blob `3a5c94f4b0c2a22b29636f1ccaa77e6ea597398a`. The red rerun exited 1 (1 failure, 3 errors); the fixed rerun exited 0. All test execution used synthetic receipts/mocks; no Go binary was executed.

Tested implementation SHA256: `c2682ae594c006e85ff860033766a25f920d35ab9a9113d22bf56232132936c2`.

New regression file SHA256: `f19401c2d6c955d0b55660036c72143fd8a45c3d417fe3d96719a6a63074bbc2`.

## Remaining boundaries

No fresh metadata binding, compile-only experiment, Go/product suite, full `make check`, formal default/sharedDB/HTTP/components/wholePG run, database/service access, real mail, or deployment was performed. Prior v3 results apply only to their original tested pins; they are not inherited by this changed helper. Formal consumer migration still requires independent review and fresh exact-source evidence. Method19, SML, wholeCI and all prior qualification blockers remain unchanged.

The current R5-TODO was fetched in full from the exact base and verified as blob `983b50460d2175690fa163a2e68c2fca021acc95` (173,808 characters; 282,463 UTF-8 bytes). This change only appends a bounded completion entry. Central progress remains **10/171**; no parent task is advanced. Commit/push or PR creation does not establish CI success, merge readiness, release, or deployment.
