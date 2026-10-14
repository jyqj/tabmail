# SMTP DATA final reply independent review — 2026-10-07

## Reviewed scope

Independent read-only review of the delivery fix and its new regression suite in `tabmail-smtp`. No production or test file was modified by the reviewer.

- `internal/outbound/delivery.go` SHA-256: `9f6d583deb4e8573787c375cab6f1a138172c5abe028f0aca090d49141580e88`
- `internal/outbound/r5_smtp_reply_classification_test.go` SHA-256: `f1d362a4d3f1c3cb3255d9e234d027cfc65b92a589afb2f01e50d56f084a8c66`

## Conclusion

No blocking issue found in this bounded change. The new 400–599 condition correctly separates explicit negative DATA completion from unexpected positive/intermediate/invalid replies. The existing original error remains discoverable, and the uncertain sentinel reaches both direct MX fallback suppression and recipient terminal hold. Observed DATA 250 remains successful despite QUIT cleanup failure.

Reviewed the real `sendSMTP`, `deliverDirectWith`, `deliverRecipients`, and the regression assertions covering authorization, manual requeue, and worker non-resend. The new tests use the actual standard-library SMTP parser and the existing bounded net.Pipe fixture, which observes the complete DATA terminator before its final response. They do not synthesize the production `textproto.Error`.

## Verification boundary

This is independent code and test review. The author is running the bounded and full outbound package tests; the reviewer did not repeat those executions. The in-memory store tests establish their explicitly stated scope and do not claim PostgreSQL, durable restart, real mail delivery, or whole-project CI qualification.

