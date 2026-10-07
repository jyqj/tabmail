# R5-P8-110: SMTP and retention configuration bounds

This increment rejects unsafe SMTP/retention resource values at the existing `Root.Validate` startup boundary. It advances one concrete part of R5-P8-110; it does not claim the entire configuration/schema/deployment TODO complete.

Before this change, an explicit zero/negative retention interval passed `Load` and reached `time.NewTicker` in `internal/retention/scanner.go`, which panics. A nonpositive retention batch also cannot terminate the empty sweep via `n < batch`. Nonpositive SMTP message/recipient limits disable those go-smtp bounds, and nonpositive idle timeout removes its time bound. Negative SMTP connections were accepted as an undocumented unlimited setting.

| Actual environment key | Accepted value | Existing default |
|---|---|---|
| `TABMAIL_SMTP_MAXRECIPIENTS` | > 0 | 200 |
| `TABMAIL_SMTP_MAXMESSAGEBYTES` | > 0 | 26214400 |
| `TABMAIL_SMTP_TIMEOUT` | > 0 | 300s |
| `TABMAIL_SMTP_MAX_CONNECTIONS` | >= 0; 0 explicitly means unlimited | 100 |
| `TABMAIL_STORAGE_RETENTIONSCANINTERVAL` | > 0 | 60s |
| `TABMAIL_STORAGE_RETENTIONBATCHSIZE` | > 0 | 1000 |

The envconfig struct tags remain the sole default source. Validate does not replace an explicitly invalid value with a default. Existing direct-construction unit fixtures now supply required positive resource values. No secret appears in validation errors; each resource error identifies only its environment key and constraint. No PostgreSQL or network service is required by these tests.

## Fixed-baseline reproduction and verification

Baseline: `4065c4909c8f21a401a9a1af6370fa3f72670b99`, with only the new regression test added. Go 1.25.7, existing module/build caches, `GOPROXY=off`, `GOTOOLCHAIN=local`.

```sh
go test -mod=readonly -race -count=1 -timeout=60s -json ./internal/config -run '^TestR5ResourceConfig'
go test -mod=readonly -race -count=1 -timeout=60s -json ./internal/config
```

- Baseline new regressions: 3 top-level tests; 1 PASS, 2 FAIL. Leaf cases: 1 PASS, 17 FAIL, 0 SKIP. The 17 failures are 11 invalid environment overrides and 6 direct Validate calls accepted by the baseline.
- Candidate full config package: 13 top-level tests / 31 leaf cases PASS, 0 FAIL, 0 SKIP, with the same 18 new leaf cases all passing. Existing webhook canonical-CIDR and configuration compatibility tests remain included.
- `git diff --check`: PASS.

The regression file SHA-256 is `b1264289f213271e835b691e4ede38e728d3c2ffbe6216b1395257e355d77a65` on both runs. Local raw evidence: `validation/wave-backend-config-baseline.jsonl` (SHA-256 `51dddae4fac1c9dfab75efc6eadc5b974e50591f718315aa7108b0692b791529`) and `validation/wave-backend-config-fixed.jsonl` (SHA-256 `cbcd7de76a025368bbb3d6dcb94a7345a2b5cd8bc579e662d868bc9c441de69c`) in the session validation directory.

Remaining parent scope includes the other configuration groups, schema/examples/deployment consistency and runtime projection review. This static startup-validation increment does not assert full backend, PostgreSQL, release, or parent-task acceptance.
