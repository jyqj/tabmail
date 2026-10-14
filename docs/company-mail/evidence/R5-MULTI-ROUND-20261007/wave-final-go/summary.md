# Final combined Go verification — 2026-10-07

Result: **tests and related vet PASS; repository-wide build blocked by local VCS discovery**. The complete requested qualification remains false because the build did not pass.

Source commit: `e32564304c0c84aa80b1184e72129fb0316d989d`; tree: `bfa06a61ce4bc8411243d3b57de26db5e161dc8b`.

Toolchain: `go version go1.25.7 linux/amd64`. All tests used `-mod=readonly -race -count=1 -timeout=180s`.

| Package / scope | Top-level P/F/S | Leaf P/F/S | All terminal test events P/F/S | Package seconds |
| --- | ---: | ---: | ---: | ---: |
| `tabmail/internal/app/companymail (complete)` | 28/0/0 | 116/0/0 | 130/0/0 | 2.402 |
| `tabmail/internal/config (complete)` | 13/0/0 | 31/0/0 | 34/0/0 | 6.049 |
| `tabmail/internal/mailcontent (complete)` | 22/0/0 | 154/0/0 | 163/0/0 | 37.822 |
| `tabmail/internal/outbound (complete)` | 77/0/0 | 510/0/0 | 549/0/0 | 2.967 |
| `tabmail/internal/api/handlers (8 selected controls)` | 8/0/0 | 18/0/0 | 20/0/0 | 1.165 |

## Commands

- `/workspace/scratch/458de7991ac3/toolchains/go/bin/go version` — exit 0; log `go-version.log`; elapsed 0.026 s.
- `/workspace/scratch/458de7991ac3/toolchains/go/bin/go list -mod=readonly -json ./internal/config ./internal/outbound ./internal/app/companymail ./internal/mailcontent ./internal/api/handlers` — exit 0; log `package-list.log`; elapsed 0.858 s.
- `/workspace/scratch/458de7991ac3/toolchains/go/bin/go test -mod=readonly -race -count=1 -timeout=180s -list '^(TestOrdinaryReceiptOpenAPIWireFixtures|TestOutboundJobAccessCheckCoversGetRetryAndAttempts|TestOutboundListJobsScopesRegularPrincipalsAndTenantContext|TestSubmissionReceiptCapabilities|TestAtomicRetryCommittedResponseStaysSuccessfulAndRestricted|TestRetryJobConflictReasonMapping|TestR5InboundAttachmentHTTPSourceFailureIsNotMissing|TestRespondAppErrorContract)$' ./internal/api/handlers` — exit 0; log `handler-test-list.log`; elapsed 21.224 s.
- `/workspace/scratch/458de7991ac3/toolchains/go/bin/go test -mod=readonly -race -count=1 -timeout=180s -json ./internal/config ./internal/outbound ./internal/app/companymail ./internal/mailcontent` — exit 0; log `four-full-packages.jsonl`; elapsed 44.223 s.
- `/workspace/scratch/458de7991ac3/toolchains/go/bin/go test -mod=readonly -race -count=1 -timeout=180s -json -run '^(TestOrdinaryReceiptOpenAPIWireFixtures|TestOutboundJobAccessCheckCoversGetRetryAndAttempts|TestOutboundListJobsScopesRegularPrincipalsAndTenantContext|TestSubmissionReceiptCapabilities|TestAtomicRetryCommittedResponseStaysSuccessfulAndRestricted|TestRetryJobConflictReasonMapping|TestR5InboundAttachmentHTTPSourceFailureIsNotMissing|TestRespondAppErrorContract)$' ./internal/api/handlers` — exit 0; log `handler-controls.jsonl`; elapsed 6.261 s.
- `/workspace/scratch/458de7991ac3/toolchains/go/bin/go build -mod=readonly ./...` — exit 1; log `build-all.log`; elapsed 1.426 s.
- `/workspace/scratch/458de7991ac3/toolchains/go/bin/go vet -mod=readonly ./internal/config ./internal/outbound ./internal/app/companymail ./internal/mailcontent ./internal/api/handlers` — exit 0; log `vet-related.log`; elapsed 11.42 s.

The handler command additionally exported `ORDINARY_RECEIPT_WIRE_FIXTURE` to the new evidence file shown in `summary.json`.

## Boundaries and source identity

- Fresh execution on the fixed combined source; no earlier worker result is inherited.
- All four named packages run in full; only the eight explicitly listed handler tests run.
- Selected handler controls use real handlers/services and synthetic repository/object seams, not PostgreSQL.
- The ordinary wire fixture is typed projection/envelope evidence, not live router or PostgreSQL admission.
- Any test skip is counted separately; unselected PostgreSQL or external probes have no qualification here.
- No repository source, test, budget, validator, exclusion, or module file is changed by this runner.

The complete selected Go source/test/embed file hashes and tracked module-file hashes are in `source-manifest.json`. The runner compared all of those bytes after execution and checked the repository remained on the same clean source commit. The machine summary records exact listed/executed handler names, commands, exit codes, timestamps, raw-log hashes, per-package pass/fail/skip counts, and any missing terminal events. Counts describe this combined tree only.

## Observed build blocker

The single `go build -mod=readonly ./...` attempt exited 1, emitting two VCS-status acquisition errors. There were no compiler diagnostics in that log. The build is not qualified as passing.

Read-only diagnosis followed the exact pinned Go 1.25.7 implementation: `vcsGit.RootNames`, `FromDir`/`isVCSRoot`, and `gitStatus`. Go's directory-only repository marker check skips this worktree's file marker and selects the ancestor `/workspace`. Running its first Git status command in that ancestor reproduces exit 128; running both stamping Git commands in the actual worktree succeeds and returns the expected source commit. This root selection is inferred from the pinned implementation and observed filesystem types; an instrumented trace of the failed Go child was not captured. `vcs-diagnosis.json` records the exact commands, exit codes, output, and toolchain source hashes.

No source or environment rule was changed to work around the failure, and neither the build nor tests were repeated. If root needs a complete local build, a normal independent Git clone of the identical source commit is the narrow follow-up; it does not require disabling VCS stamping or rerunning successful tests.

## Combined execution counts

The four complete packages produced **140 top-level / 811 leaf / 876 terminal test events**, all passing. Adding the eight selected handler controls produces **148 top-level / 829 leaf / 896 terminal test events**, all passing, with **0 failures and 0 skips**. Parent and leaf counts are separate and must not be added together. The handler wire fixture generated 21 encoded responses; it remains typed-projection/envelope evidence only.
