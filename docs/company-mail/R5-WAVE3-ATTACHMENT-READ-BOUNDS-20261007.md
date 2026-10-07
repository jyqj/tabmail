# R5-P5-060: bound attachment read progress and cancellation

Attachment upload and verified attachment download used byte-limited `io.ReadAll`, which still continued indefinitely when a reader repeatedly returned `(0, nil)`. Upload also continued into reserve/put/finish after its reader canceled the request. Downloads did not close an owned object stream when cancellation occurred during a blocked Read.

The attachment read boundary now checks context before/after each read, rejects 100 consecutive empty successful reads with `io.ErrNoProgress`, and rejects invalid reader counts without indexing an invalid slice. The counter resets when actual bytes arrive. Existing size limits still bound memory, and ready download size/SHA verification and post-I/O authorization remain unchanged.

The upload reader remains caller-owned: the service neither closes it nor starts a detached worker. Cancellation is observed when its Read returns. An arbitrary `io.Reader` that blocks forever and exposes no cancellation mechanism cannot be forcibly interrupted by this API. Download readers are owned by the service and closed exactly once on cancellation or completion. Their `Close` must unblock concurrent Read, matching the existing object-reader contract used by the MIME parser.

This is one concrete implementation increment under R5-P5-060, not acceptance of every attachment content-security/aggregate-limit concern or a release qualification.

## Verification

Fixed baseline: `b504ba8e2f529b6e52ff332d667bd21390b37d37`. The regression file SHA-256 was unchanged between baseline and candidate: `99ea36bd99441e140130a09cc197a191e204cf6f9de8635508ea0eafda0639da`.

```sh
go test -mod=readonly -race -count=1 -timeout=60s -json ./internal/app/companymail -run '^TestR5Attachment(Upload|Download)'
go test -mod=readonly -race -count=1 -timeout=60s -json ./internal/app/companymail
```

Go 1.25.7, existing caches, `GOPROXY=off`, `GOTOOLCHAIN=local`:

| Execution | Top-level tests | Leaf cases | Skip |
|---|---|---|---|
| Frozen new regression on baseline | 1 PASS / 5 FAIL | 1 PASS / 6 FAIL | 0 |
| Candidate complete companymail package | 28 PASS | 116 PASS | 0 |

The same seven new leaf cases all pass after the fix:

- No-progress upload terminates within the read budget and performs no reservation/storage/finalization.
- Cancellation during upload stops further reads and causes no reserve/put/finish, preserving `errors.Is(context.Canceled)`.
- Ninety-nine empty reads followed by actual progress remain valid, including a second such interval, and the caller-owned upload reader stays open.
- Both draft and sent attachment downloads reject no-progress streams and close them exactly once.
- Canceled downloads stop subsequent reads and preserve cancellation.
- Cancellation of a blocked owned source closes it, releases the synchronous Read, and returns cancellation without a stranded worker. The failing baseline is explicitly released and joined before the test returns.

The complete package also exercises the existing oversize/negative-size/hash-mismatch/short/extra-object cases, upload failure phases, MIME/compose behavior and permission/provenance rechecks. `git diff --check` passes. All tests use memory adapters; no PostgreSQL test was skipped or claimed as executed.

Raw local evidence in the session validation directory:

- `validation/wave-backend-read-bounds-baseline.jsonl`: SHA-256 `492ed324501df9b268dc9219d3cc91f6ab2fba28e37a2ee3910a3fe290718a92`.
- `validation/wave-backend-read-bounds-fixed.jsonl`: SHA-256 `264501fdcd305d2b68b9dd860b05567d315c37238cbf639621966e293b9c3d32`.

No central TODO, catalogs, validators, CI budgets, backend full-suite claims, or skip policies change in this increment. PostgreSQL and release qualification remain separate gates.
