# Preserve direct SMTP greeting failures — 2026-10-07

## Implementation item

This independently verified item advances **R5-P6-040/050**. Tested baseline:
`7a4629c793105cde4038ec05b8ba51e084d0806e`, including the preceding relay IPv6
fix. Parent acceptance remains unchanged: **10/171 complete, 161 remaining**.

The direct session called `client.Extension("STARTTLS")` before explicitly
checking the SMTP greeting. Go 1.25.7 `net/smtp.Extension` calls the lazy hello
exchange internally and converts any hello error to `false, ""`. With required
TLS, the session consequently replaced a real HELO rejection, EOF, or canceled
transport with a generic “STARTTLS required but not supported” message.

The session now checks `client.Hello("localhost")` first and wraps its error
with `%w`. This preserves the standard library's default local name and
EHLO-to-HELO fallback. Only a completed greeting can lead to the subsequent
STARTTLS availability decision. An actual final HELO 451/550 now retains its
typed reply and can reach the existing temporary/permanent classifier; it is
not confused with a server that greeted successfully but omitted STARTTLS.
The preserved protocol rejection is the final HELO reply, not an earlier EHLO
failure that the standard library successfully recovered from.

TLS policy, opportunistic reconnect behavior, DATA-final uncertainty and
acknowledged-delivery cleanup behavior are unchanged. This is not a parent
completion, PostgreSQL proof, public mail test, or new release qualification.

## Frozen oracle and results

The new real TCP fixture reaches the actual EHLO or fallback HELO command
before each fault. It covers 451, 550, EOF, cancellation at EHLO/HELO, and a
HELO deadline, each with required TLS and opportunistic TLS. It requires the
original `*textproto.Error`, EOF or `*net.OpError` plus context cause, no DATA
uncertainty before the envelope, and a bounded connection/fixture close.

The transport assertion deliberately requires `*net.OpError`: checking only
`net.Error` would accept `context.DeadlineExceeded` itself and miss a lost TCP
cause. This was tightened before the final frozen baseline and before any
product edit. The initial oracle's log remains separate from the frozen result.

| Execution | Result |
| --- | --- |
| Frozen tests on unchanged baseline | 6/12 leaf cases PASS; all six required-TLS cases FAIL with lost causes; 0 skips |
| Same frozen tests with fix plus IPv6, context/TLS, final-reply/cause and null-MX controls | 28 top-level / 152 test-and-subtest PASS events; 0 failures/skips |
| Diff whitespace check | PASS |

Both runs used pinned Go 1.25.7, race detection, the existing module graph,
`GOTOOLCHAIN=local`, the shared Go caches, and `GOPROXY=off`. No PG service,
external SMTP account, actual DNS or public network delivery was used.

```sh
# Baseline with only the final frozen new test applied:
go test -json -race -count=1 ./internal/outbound -run '^TestR5DirectGreeting'

# Candidate with unchanged frozen test and relevant shipping controls:
go test -json -race -count=1 ./internal/outbound -run '^(TestR5DirectGreeting.*|TestR5RelayNumericHostConnectAndCancel|TestR5DeliveryContext.*|TestR5SMTPFinalReply.*|TestR5SMTPCause.*|TestR5NullMX.*)$'
```

The actual executable was
`/workspace/scratch/458de7991ac3/toolchains/go/bin/go`; cache paths were
`/workspace/scratch/458de7991ac3/go-cache/{mod,build}`.

| Artifact | SHA-256 |
| --- | --- |
| Frozen `internal/outbound/r5_direct_greeting_test.go` | `83cd188615a1edbd2876308fbdfc9cc70f96a0c003f30aa46ef9dd7fae625646` |
| Candidate `internal/outbound/delivery.go` | `5091a1f0089e5fbfadb8858f3a382923c9a2595c53173ffebc5214f7f62278b0` |
| `validation/wave-smtp/greeting-baseline.jsonl` | `c27cebb06ba46bb99fc641c617feb12ef7ae75b2708bebbec13742f11d1aceb6` |
| `validation/wave-smtp/greeting-candidate.jsonl` | `bf006cdf607dc9d24876af8339ea5d80e18200b5d95a498fe437827b11bf2932` |

Log paths are relative to the shared execution workspace. Existing TLS success,
required-TLS rejection, cancellation, reconnect and DATA boundary controls
were executed as part of the candidate selection, rather than inferred from
the new tests. The complete repository suite was not run in this step.
