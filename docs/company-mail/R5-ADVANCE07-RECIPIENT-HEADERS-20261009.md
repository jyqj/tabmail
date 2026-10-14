# ADVANCE-07 / #203: bounded To and Cc header lines

Parent scope: R5-P6-090. Fifty legal recipients fit the existing submission limit but previously became one overlong To or Cc line. A real SMTP peer enforcing the MIME line limit rejected all six fixed submission/transport examples before this change.

`Build` now folds whitespace between complete mailbox values. It does not split quoted local parts, interpret their internal commas, change recipient order, mutate the envelope, or emit BCC. It targets 78 octets and enforces the 998-octet hard bound. A longer indivisible legacy mailbox value is kept intact when it fits; an oversized value, including its separating comma when present, returns a MIME construction error instead of publishing malformed bytes. Normal sender, custom-header, subject and attachment behavior remains in its existing scope.

Reference: [RFC 5322 section 2.1.1](https://www.rfc-editor.org/rfc/rfc5322.html#section-2.1.1) specifies header line limits; [section 2.2.3](https://www.rfc-editor.org/rfc/rfc5322.html#section-2.2.3) permits folding at legal whitespace positions. This task addresses structured recipient fields and does not claim all-header validation or full SMTP address-size admission.

## Fixed validation

The 40-leaf test file was completed and executed on `6f519fc` before changing `builder.go`; the builder there is byte-identical to the initial public baseline `fdea2178759ce9842844926374ea98517bc4188b`. The preceding SMTP-fragment task remained present in both runs.

| Run | Actual result |
| --- | --- |
| Unmodified builder plus fixed tests | 11 PASS / 29 FAIL / 0 SKIP |
| Modified builder plus the exact same test bytes | 40 PASS / 0 FAIL / 0 SKIP |
| Complete outbound package on clean implementation `d652fd0a32385e674e1d1f39a328e374de49b367`, tree `65aa7a76074f3075e9df2bb788bf53026f7428de` | 790 race leaf PASS / 0 FAIL / 0 SKIP |
| `go vet ./internal/outbound` on that implementation | exit 0 |

The fixed cases cover ordinary, quoted and Unicode local parts; both header roles; 1/2/50 recipients; mixed To/Cc with BCC; multipart body/attachment controls; and indivisible header token boundaries. Six tests execute real submission, the persisted FakeStore queue, queued MIME construction and complete owned TCP relay/direct SMTP conversations with 50 recipients. The peer rejects physical lines exceeding 998 octets. Parsed To/Cc identities and order, exact envelope commands and absence of BCC are checked independently of the folding algorithm.

Commands used Go 1.25.7, `GOTOOLCHAIN=local GOPROXY=off GOFLAGS=-mod=readonly GOMAXPROCS=2`, existing caches, and `go test -race -p 1 ./internal/outbound -count=1 -json`. The focused runs also used `-run '^TestR5AdvanceRecipientHeader'`. Focused runs were the explicit uncommitted overlays on the stated base; the full package and vet ran on the clean source commit before this report. Full-package and focused counts overlap and must not be added.

| Byte identity | SHA-256 |
| --- | --- |
| `internal/outbound/r5_recipient_header_folding_test.go` | `c29f1f5df762918021344ed6f0caf3ef18fa2bc64be167ff08543a8099da1827` |
| `internal/outbound/builder.go` after fix | `b54e7913043b2438fbd942c4cb6c52dd6e269055506c65ae550a9d9bc0636b66` |
| Local raw baseline log, 49,420 bytes | `e5acf851c56f63db4bfec2eca7ddb746af2282fea6d700ca364811068ac26772` |
| Local raw focused candidate log, 34,525 bytes | `c23d9b5a5d7c966cbcd4d56b0120531406af5f474c80d6f82e73b7cde9725fc8` |
| Local raw full-package log, 805,474 bytes | `e8ca94df91201454e072830f8eb9bfbaacaea323503d0751368589ca58ee1c66` |

No external SMTP service or production account was used. Metadata tests use FakeStore, not PostgreSQL. Independent review, integration identity, CI and actual Issue/PR closure remain the batch owner's subsequent steps. Parent checkboxes and full release gates are unchanged.
