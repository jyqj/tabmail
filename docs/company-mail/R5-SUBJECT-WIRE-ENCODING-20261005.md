# Bounded Subject wire encoding and folding

2026-10-05. R5-P6-090 progress only.

## Source and correction

Baseline: PR52 public head `0e0288fed6da7c8fdfae80c6b4bd4a9958e6b14b`, equivalent local head `b52f0602ddf85474c9d9e57edf803bf44fea8a36`, exact tree `487a8fc9cbb72c6787ebac8fe6f95345fde5cd99`. Reviewed code/test candidate tree: `d4445a97fee8c91118467539532181f34d70f7f6`; delivery adds only this report and one append-only TODO entry.

The API accepts a Subject of up to 998 bytes. The previous builder added the nine-byte `Subject: ` prefix without folding: 990-byte ASCII produced a 999-byte physical header line, and 998-byte ASCII produced 1007 bytes. Unicode Subjects introduced raw UTF-8 into the wire. Literal encoded-word-looking text and leading/trailing whitespace also changed meaning under normal header decoding.

Only Subject rendering changes. After the existing CR/LF stripping, every nonempty Subject is encoded as UTF-8 Base64 encoded-words. Each chunk contains at most 39 input bytes and ends at a complete UTF-8 character. Words are separated by CRLF followed by a space. The maximum word length is 64 characters; the first physical line is at most 73 and continuation lines at most 65. Folding whitespace is ignored between adjacent encoded-words, while all original spaces and tabs remain inside the encoded data.

This follows [RFC 2047 sections 2, 5 and 6.2](https://www.rfc-editor.org/rfc/rfc2047) and stays below the [RFC 5322 section 2.1.1 hard line limit](https://www.rfc-editor.org/rfc/rfc5322). RFC 2047 permits, but discourages, purely ASCII encoded-words. The uniform representation intentionally includes short ASCII Subjects to preserve whitespace and literal encoded-word text without a second formatting path.

The 998-byte input limit, semantic/persisted Subject, API, From/To/Cc/custom headers, BCC envelope handling, body encoding and MIME structure are unchanged. Other headers are not newly encoded or folded; this is not a global SMTPUTF8 or header-safety qualification.

Exact accepted SHA256 values:

- `internal/outbound/builder.go`: `17bf3756c2c0dce1b908abd8f4ad91514a20cd33352333acf1d2fa1add1de000`
- `internal/outbound/builder_subject_test.go`: `48a817ab358bbce5bd099c34b8992555686481caabc3eb519cf9d89d739e21bd`

## Verification

Official checksum-verified Go 1.25.7; existing offline caches; `GOTOOLCHAIN=local`, `GOPROXY=off`, `GOENV=off`, `-mod=readonly`; minimal environment without service credentials. Accepted full source remained unchanged while an owned Go overlay supplied the candidate. No dependency download or duplicate checkout/cache was needed.

- Final author tests ran against the baseline before formatter edits: **30 of 36 leaf cases failed**. The exact same tests pass on the accepted candidate: **36/36 leaves, 37/37 including the group**. An initial chunk-budget prototype failed the first-line limit; the corrected candidate was frozen only after green results.
- Real `internal/outbound` package compilation and focused author/legacy selection: **12 top-level / 54 test-and-subtest passes**, zero failures/skips, with race detection. Includes existing `TestBuild_*` and `TestIsValidHeaderName`.
- Existing company template selection: **8 top-level / 34 test-and-subtest passes**, zero failures/skips, with race detection.
- `go vet` for `internal/outbound` and `internal/company`, Go formatting and patch applicability pass. The outbound test dependency inventory was available offline.
- Independent baseline-first review, separately owned: **9/91 leaf cases pass on baseline; 91/91 pass on the same production bytes**. Original builder controls pass **17/17 total** on both baseline and candidate. Counts overlap and are not additive. The independent oracle remained unchanged.

Coverage includes Chinese, emoji, mixed scripts, combining marks, multibyte boundaries, 989/990/998-byte spaced and unspaced ASCII, repeated/leading/trailing spaces and tabs, punctuation, literal/malformed encoded-word-looking text, CR/LF stripping, forbidden custom Subject overrides, and a 998-byte multibyte Subject produced by `company.Render`. Unrelated header, recipient and MIME controls remain green.

Focused commands, with the accepted candidate overlay active:

```sh
go test -mod=readonly -json -race -count=1 -timeout=60s -p=2 \
  -run '^(TestBuild_|TestIsValidHeaderName)' ./internal/outbound
go test -mod=readonly -json -race -count=1 -timeout=60s -p=2 \
  -run '^Test.*Template' ./internal/company
go vet -mod=readonly -p=2 ./internal/outbound ./internal/company
```

## Acceptance boundary

This is a bounded valid-UTF-8 Subject formatter correction, not a new malformed-UTF-8 validation policy. No broad `./...` suite, PostgreSQL, SMTP/listener, live account/mail/credential, DKIM delivery, formal R5 runtime or performance qualification was run. No parent task is completed: R5-P6-090 and dependencies R5-P6-040/050 remain open; central completion stays **10/171**, historical F52 and other formal gates remain unchanged. No publication, merge or deployment is claimed by this local report.
