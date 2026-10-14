# R5-P8-060: distinguish unavailable attachment sources from absent part IDs

`InboundAttachmentByID` previously turned every parser error into `NotFound`, including unavailable object storage, caller cancellation/deadline, malformed MIME, and MIME depth-budget exhaustion. The real authenticated parts endpoint consequently returned 404 for a storage outage or malformed source.

The parser now returns a stable `ErrAttachmentNotFound` only after a source has parsed successfully and no immutable part ID matches. The service maps that sentinel to the existing 404; other failures are wrapped by the existing application Internal constructor. `errors.Is` retains their causes, and the existing HTTP renderer returns a generic 500 without disclosing storage details. Authorization and post-read rechecks remain unchanged.

This is one bounded implementation increment under R5-P8-060. It does not accept the entire errors/status/clock TODO or claim that every backend path has been normalized.

## Verification

Fixed baseline: `042ad48357671f6181a1ef0ecc0d968988756e37` (the preceding configuration increment). Both new regression files were frozen before baseline execution and unchanged for candidate execution:

- `internal/app/companymail/r5_attachment_error_test.go`: SHA-256 `4c90a33f3e72235124384d31f071bbd1e8667fa5385e9665e7e4d2ce8ca4ae03`.
- `internal/api/handlers/r5_attachment_error_test.go`: SHA-256 `31a70d65debb30c862c38a12f71e933ae1d2d39007d5e8534e2454980bfb602c`.

```sh
go test -mod=readonly -race -count=1 -timeout=60s -json ./internal/app/companymail ./internal/api/handlers -run '^TestR5InboundAttachment'
go test -mod=readonly -race -count=1 -timeout=60s -json ./internal/app/companymail ./internal/mailcontent
go test -mod=readonly -race -count=1 -timeout=60s -json ./internal/api/handlers -run '^(TestR5InboundAttachment|TestRespondAppErrorContract)'
```

Go 1.25.7 used the existing caches with `GOPROXY=off`, `GOTOOLCHAIN=local`. The tests use memory repository/object adapters with the real parser, content service, Auth middleware, and attachment HTTP handler; no PostgreSQL requirement is skipped.

| Execution | Top-level | Leaf cases | Skip |
|---|---|---|---|
| Frozen regressions on baseline | 4 FAIL | 2 PASS / 8 FAIL | 0 |
| Candidate complete companymail + mailcontent packages | 44 PASS | 263 PASS | 0 |
| Candidate HTTP regressions + existing error-rendering contract | 2 PASS | 12 PASS | 0 |

All 10 new leaf cases pass in the candidate. Negative controls retain a genuine missing part as 404, while storage read failure, malformed source and actual MIME depth-limit exhaustion produce HTTP 500. Wrapped I/O/cancellation/deadline causes and an active caller cancellation survive the application boundary. `git diff --check` passes.

Raw local evidence in the session validation directory:

- `validation/wave-backend-errors-baseline.jsonl`: SHA-256 `db34efe7b6d571c5530b986d358da06bd41753024009d0d0d5a23fb6080068fd`.
- `validation/wave-backend-errors-fixed-unit.jsonl`: SHA-256 `e8c652504bdc335666f019688d0c0b4481f6008d58fa3582ec10f452f1a159c6`.
- `validation/wave-backend-errors-fixed-http.jsonl`: SHA-256 `71fc8acd1484ecef053f5338acee1b72ded035b57d645b47ea01c3c50806875f`.

The full handlers/PostgreSQL/release suites are outside this increment's local acceptance. No validator, catalog, central TODO, CI budget, or skip policy changes are included.
