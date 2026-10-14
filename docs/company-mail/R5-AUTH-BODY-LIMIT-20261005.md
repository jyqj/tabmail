# Bounded authentication request decoding

2026-10-05. Baseline: PR51 head `71b99514f4aa4457462387abe2f68ca351185bff`, tree `a8ab3734314a3002c046e276ba8714504a715863`.

## Finding and correction

Login decoded an unbounded JSON body before looking up credentials. A finite synthetic 65,537-byte body reached the fake credential store once and returned 401 on the baseline, for both known length and unknown/chunked request metadata. The rejecting regression expected 400 with no lookup. This demonstrates a missing parser budget, not measured production denial of service.

An auth-local wrapper now applies `http.MaxBytesReader` at **65,536 bytes** to Login, Register, Refresh, Logout, and ChangePassword. It uses the unchanged strict single-document decoder and existing 400 `BAD_REQUEST` / `invalid request body` response. Shared decoding and mail/upload limits are unchanged.

The budget derives from the request fields: stored email/display names each allow 255 characters, password creation allows at most 72 bytes, and generated refresh tokens contain 43 ASCII bytes. Two maximally surrogate-escaped profile strings plus a fully escaped password and JSON envelope consume 6,596 bytes. The 64 KiB transport envelope therefore leaves substantial encoding/formatting headroom without adding per-field credential restrictions.

**Intentional compatibility change:** Refresh and Logout previously ignored all decoder errors. They now accept empty or whitespace-only bodies within budget, but reject supplied malformed/unknown-field/multiple-document/oversized JSON even with a valid cookie. Rejection precedes token rotation/revocation and cookie mutation. Empty-object/null behavior, cookie preference, body-token fallback, and empty authenticated logout remain supported. Registration gates and ChangePassword authentication retain their order; middleware may perform its required identity lookup before a protected handler. OpenAPI describes the limit, optional refresh-cookie body, and 400 behavior.

## Verification

Official checksum-verified Go 1.25.7, owned existing caches, `GOTOOLCHAIN=local`, `GOPROXY=off`, `-mod=readonly`, minimal environment without service credentials. Test imports/initializers were inspected; the pre-existing metrics ticker is in-memory only.

```sh
go test -buildvcs=false -mod=readonly -json -race -count=1 -timeout=60s -p=2 \
  -run '^Test(Auth(LoginBodyLimit|BodyLimit|OptionalBody)|RegistrationPasswordBytePolicy|R5APIBackgroundShutdown(Login|PendingLogin|NewLogin))' \
  ./internal/api/handlers
```

- Author: 13 top-level tests / **97 test-and-subtest events pass**, zero failures/skips. Includes unchanged registration policy and Login lifecycle regressions.
- Independent baseline-first review: **402 subcases / 411 total events pass** on the candidate; all 144 expected baseline failing subcases are fixed. Independent rerun of the same author/legacy selection also passes 97/97. Counts overlap and are not additive. No blocking findings.
- Coverage: all five handlers; 65,535/65,536/65,537 bytes; UTF-8/JSON escaping; inaccurate/unknown/chunked-marked lengths; small incremental readers; bounded 131,073-byte whitespace tails; malformed bodies and read errors; optional cookie/body compatibility; one close; and successful fake authentication flows for all five handlers. OpenAPI YAML parses and its five contracts were checked.

The **65,537-byte assertion covers decoder Read consumption**, including the overflow-detection byte. It does not bound total socket traffic or real server-side Close/draining. Request presentations are in-process fixtures, not socket-level HTTP tests.

Tests used a verified partial backend export. Review is bound to these exact SHA256 values, not a claim that the entire publication tree ran:

- `internal/api/handlers/auth.go`: `204248871797cbca6638f2d9da4faa46d07505e0d65629ea8e20022e20c7d806`
- `internal/api/handlers/auth_body_limit_test.go`: `bcf3bafa75b693fb9d265f5beff0bd70f54c1a189adb93d197a961660496e677`
- `internal/api/openapi.yaml`: `2cd1caeb0bbff4fa11d028860b6f514611e18f57de1f28a9a392525f9a8a4dda`
- Independent tests, identical in baseline/candidate runs: `995e10e8951ce24866e714b54ea06273f17005824494e5d09330827ee2a2892e`

Publication applies only these three files, this report, and one additive TODO line to the verified full baseline tree. Other files/modes, module locks, both local forks, and existing TODO checkboxes are preserved. No listener, remote attack, live credentials, database/mail service, load/OOM test, full suite, formal R5 qualification, merge, or deployment was run. Central completion stays **10/171**; parent items and prior qualification blockers remain open.
