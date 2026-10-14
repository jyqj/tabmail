# Recipient uncertainty and failure causes — 2026-10-07

## Implementation item and preserved behavior

This is the third independently validated SMTP implementation item in this
local wave, under **R5-P6-050/060**. Tested baseline:
`1386b38c9e156dab4d9c57b4c58c8c9e77c10882`, containing the preceding IPv6 and
greeting fixes. It does not complete those parent tasks or replace their
PostgreSQL, restart, lease-concurrency, or operational acceptance requirements.
Historical central progress remains **10/171 complete, 161 remaining**.

The shipping recipient loop previously discarded information in four paths:

1. A failed job marker for an already uncertain recipient returned only the
   storage error, losing the uncertain classification.
2. A failed job marker after an uncertain SMTP outcome also discarded the
   original network error and its cause chain.
3. A failed recipient completion preserved the checkpoint failure and uncertain
   marker but discarded an accompanying definite SMTP rejection.
4. The final temporary-recipient summary retained only a count, discarding all
   individual failure objects.

The first two paths now wrap both the uncertain outcome and marker error. The
third joins the existing uncertain/checkpoint error with the original delivery
error, if any. The last retains each temporary recipient's address and error
in a joined summary. No historical transport object is invented when only an
existing uncertain ledger state is available.

Successful terminal marking still returns nil. The error change does not
alter recipient state classification, begin/complete/token calls, authorization
ordering, or checkpoint-before-telemetry ordering. The underlying recipient
ledger/in-flight fencing remains responsible for preventing retransmission;
the generic worker does not become a new error-classification mechanism.

## Frozen regression

The new suite uses actual error results parsed by the existing standard-library
SMTP `net.Pipe` exchange, then injects those results and storage faults into the
shipping recipient loop around the existing FakeStore. It covers:

- Fresh and existing uncertainty followed by storage failure or token mismatch;
  uncertain classification and live SMTP EOF remain discoverable through the
  returned chain. A later successful terminal marker does not resend.
- Accepted, 451, and 550 results followed by completion failure; checkpoint
  uncertainty, the storage error, and any original protocol rejection survive.
- A five-target ledger with prior/new acceptance, permanent rejection, and two
  distinct temporary failures; both temporary error objects survive the summary.
  On the subsequent attempt only temporary targets run again. Adapter/begin/
  complete counts are respectively 0 for prior acceptance, 1 for new acceptance
  and permanent rejection, and 2 for each recovered temporary target.

These are real SMTP error-parser and real service-caller tests with a fake
ledger. They do not establish PostgreSQL persistence or process-crash behavior.

| Execution | Result |
| --- | --- |
| Unchanged baseline plus frozen test | 1/8 leaf scenarios PASS; 7 FAIL at the missing error-chain assertions; 0 skips |
| Candidate plus existing recipient checkpoint/recovery/final-reply controls | 18 top-level / 78 test-and-subtest PASS events; 0 failures/skips |
| Complete outbound package with all three wave fixes | 77 top-level / 549 test-and-subtest PASS events, race enabled; 0 failures/skips |
| `go vet ./internal/outbound` | PASS, exit 0 |
| Diff whitespace check | PASS |

The package run is a single combined verification of the three actual product
changes. Counts overlap with the focused selections and must not be added.
All existing local TCP context/TLS, null-MX, DATA-final reply, authorization,
recovery, and recipient-order controls in this package ran normally. No test
budget, exclusion, skip, dependency, collector, generated catalog or database
source was changed.

## Commands and identities

All commands used Go 1.25.7 at
`/workspace/scratch/458de7991ac3/toolchains/go/bin/go`, with
`GOTOOLCHAIN=local`, `GOMODCACHE=/workspace/scratch/458de7991ac3/go-cache/mod`,
`GOCACHE=/workspace/scratch/458de7991ac3/go-cache/build`, and `GOPROXY=off`.

```sh
# Baseline after adding only the frozen new test:
go test -json -race -count=1 ./internal/outbound -run '^TestR5RecipientErrorChain'

# Candidate with related existing recipient controls:
go test -json -race -count=1 ./internal/outbound -run '^(TestR5RecipientErrorChain.*|TestR5RecipientCheckpointOrder.*|TestP0Outbound.*|TestR5SMTPFinalReply.*|TestR5NullMXRecipient.*)$'

# Combined implementation and test bytes:
go test -json -race -count=1 ./internal/outbound
go vet ./internal/outbound
```

| Artifact | SHA-256 |
| --- | --- |
| Frozen `internal/outbound/r5_recipient_error_chain_test.go` | `16b91e8a5159558bfd0b85f63118e93b8b5bb4906a3cbd09896f5bfca31b97c3` |
| Candidate `internal/outbound/recipient_delivery.go` | `41362a0ba1a7b680bd9b4b0fb40559542e1aaffc212b2bc93375f935ce01ac3d` |
| Combined `internal/outbound/delivery.go` | `5091a1f0089e5fbfadb8858f3a382923c9a2595c53173ffebc5214f7f62278b0` |
| `validation/wave-smtp/recipient-baseline.jsonl` | `039402d0add40c319a33bb2c03882262cacb4448f455b673ee073f09b7b9c078` |
| `validation/wave-smtp/recipient-candidate.jsonl` | `0f988321017a7c8be7d24640fa8245c17bf481f4e076032ce702ace0f6ad5a0c` |
| `validation/wave-smtp/combined-outbound.jsonl` | `66bfe56c30ad4fc5252628d768f50b9ea18ece717b0c3a53ad93ead274e89302` |
| Empty successful `validation/wave-smtp/combined-outbound-vet.log` | `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855` |

Log paths are relative to the shared execution workspace. The earlier two
reports pin their unchanged tests. Product/test bytes were finalized before
the combined run; this report was added afterward. Repository-wide, real-PG,
external mail, wire-compatibility, and release gates remain separate.
