# ADVANCE-04 / #200: preserve incomplete SMTP reply uncertainty

Parent scope: R5-P6-050 and R5-P6-060. This is a new ordinary transport-fragment defect after the earlier response-byte budget work, not a second completion of that work. Parent acceptance and release gates remain open.

## Behavior

The standard library's buffered line parser can return an unterminated fragment as a line when the underlying connection reaches EOF, times out, or returns another error. The previous plaintext reply guard only recognized exhaustion of the response-byte budget. A short `250` fragment could therefore acknowledge a message, while short `451` and `550` fragments could become definitive rejections and authorize another MX or an incorrect recipient state.

The same fixed-size plaintext observer now retains every actual read error. A consumed reply without its terminating LF returns that underlying transport cause and does not retain a `textproto.Error` pretending to be a definitive rejection. DATA completion then uses the existing uncertainty classification and recovery hold. MAIL, RCPT, and preliminary DATA stop before a subsequent command or message body. A complete response remains authoritative even if a buffered read has already encountered an error in subsequent data. Existing byte allowances, TLS policy, state transitions, and exact 250 acceptance rules are unchanged.

Protocol reference: [RFC 5321 section 4.2](https://www.rfc-editor.org/rfc/rfc5321.html#section-4.2) defines reply framing; [section 4.2.5](https://www.rfc-editor.org/rfc/rfc5321.html#section-4.2.5) defines the DATA acceptance boundary. This change preserves the existing LF-tolerant compatibility behavior; it does not add a new strict CRLF policy.

## Fixed baseline and validation

Baseline production: `fdea2178759ce9842844926374ea98517bc4188b`. Both new regression files were completed and run before production changes; their exact bytes were then retained.

| Actual run | Result | Scope |
| --- | --- | --- |
| Baseline plus fixed tests | 9 PASS / 32 FAIL / 0 SKIP | 41 leaf tests; real net/smtp over net.Pipe, actual timeout, owned TCP relay/direct half-closes, buffered data-plus-error controls, MX fallback and recipient/retry state |
| Same fixed tests plus production patch | 41 PASS / 0 FAIL / 0 SKIP | Same 41 leaves and byte-identical regression files |
| Clean implementation `fcad85135e86f59d5ad59391a6a6f7991310faf8`, tree `e3a773f43325980e6c9743c6a33777202538d322` | 750 PASS / 0 FAIL / 0 SKIP | Entire outbound package under race, including the 41 new tests and existing byte-budget, TLS read-ahead, cancellation, quoted-recipient and DATA controls |
| Same clean implementation | exit 0 | `go vet ./internal/outbound` |

The first diagnostic run contained 17 of these leaves and observed 3 PASS / 14 FAIL. It remains a separate original log; the table uses the final frozen 41-leaf baseline, not a rewritten diagnostic result. Counts from the full package and targeted tests overlap and must not be added.

Commands used Go 1.25.7 with `GOTOOLCHAIN=local GOPROXY=off GOFLAGS=-mod=readonly GOMAXPROCS=2`, the existing module cache, and `go test -race -p 1 ./internal/outbound -count=1 -json`; the focused invocation also used `-run '^TestR5AdvanceSMTP'`. The baseline and focused candidate were uncommitted worktree runs on the stated baseline with the explicit test/production overlays. The complete package and vet were run on the clean committed implementation before this report was added.

| Byte identity | SHA-256 |
| --- | --- |
| `internal/outbound/r5_smtp_short_reply_test.go` | `98bdd034727a5fc5236d7b862c0896714955006c22b23fdb074869511d6075f4` |
| `internal/outbound/r5_smtp_short_reply_transport_test.go` | `4d272fe95128b8a637449cbcefdcbdeabe02d16288aa7a75928b76dac711a840` |
| `internal/outbound/delivery_context.go` after fix | `73561d6c26e4effcfe573035edfd261d67419d1bd743a00002e92ed1f6b6b03a` |
| Local raw baseline log, 53,616 bytes | `2a3e8f65e188beeb4380f3cd3b664b44574696dff11f8643f4965196db7f9e34` |
| Local raw focused candidate log, 36,811 bytes | `875719b0631b9012d66f686e26ecaa24de68c28a9473963874e240924c2ca7e2` |
| Local raw full-package log, 771,503 bytes | `90cc54be9198433957ba514a7bec8c52d328aeb82985f094cf230dffca3a4241` |

All peers are synthetic and owned by the test. Recipient persistence/retry uses the existing FakeStore; no PostgreSQL, external SMTP destination, real mailbox or deployment is claimed. Independent review, integration identity, CI, PR merge and Issue closure are recorded by the batch owner after this implementation handoff.
