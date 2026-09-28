# R5 isolated validation tooling

This Dockerfile is **test tooling**, not the TabMail production Dockerfile. It reuses the Go version declared in `go.mod`, the CI Node 22 major, Python 3.12 with safe tar extraction filters, and PostgreSQL 16 client utilities. It never starts the application, reads a production DSN, or changes a host tool installation.

```sh
GO_VERSION=$(awk '$1=="go" {print $2}' go.mod)
docker build --build-arg GO_VERSION="$GO_VERSION" -t tabmail-r5-test-tools scripts/testenv
docker run --rm --network none tabmail-r5-test-tools sh -c \
  'go version; node --version; npm --version; python3 --version; pg_dump --version'
```

Use `git archive` to make a **tracked-source-only temporary snapshot**, then copy in the exact reviewed changed files. Do not mount the user's working tree root: it can contain `.env`, credentials, or runtime mail. Never mount an existing production volume or the Docker socket. The image build context is only `scripts/testenv`.

`company_snapshot.py` deliberately uses `tarfile.extractall(filter="data")`. The test image verifies that this security interface exists at build time. An older system Python must not be accommodated by dropping the filter; the isolated interpreter is upgraded instead.

Download dependencies in that disposable source snapshot with `go mod download` and `(cd web && npm ci)`; preserve `GOSUMDB` and lockfiles. Run tests on a newly created, session-labelled `--internal` Docker network with a new PostgreSQL container, no host-published ports, and disposable storage. Assign the test DSN only to that container/network. `internal/testpg` creates its own databases, while the migration smoke test uses the supplied database directly: the supplied database must therefore also be disposable. Keep all test resource names and cleanup identities in the run receipt.

The OpenAPI gate now parses YAML with pinned PyYAML. During the disposable
runner's dependency-bootstrap phase, run `python3 -m pip install -r
scripts/requirements-contract.txt` (equivalently `make contract-deps`). Disconnect
external networking before executing tests on the internal PostgreSQL network.
A missing parser is a failed gate; do not bypass the OpenAPI portion. On a host,
activate a project virtualenv first rather than changing the system interpreter.

Commands inside the source snapshot:

```sh
python3 -B -m unittest discover -s scripts/tests -p 'test_*.py' -v
node --test scripts/tests/i18n_sources.test.cjs
python3 -B scripts/check_i18n_keys.py
python3 -B scripts/check_contract_drift.py
python3 -B scripts/check_http_contract.py --output-dir /evidence/new-http-run --source-sha "$SOURCE_SHA"
# Includes company DTO/schema and operation bindings. The Go route-inventory
# test below independently proves the checked-in inventory matches live source.
go build ./...
go test -json -race -count=1 -timeout=180s ./...
go vet ./...
(cd web && npm ci && npx tsc --noEmit && npm test && npm run lint && npm run build)
```

The i18n gate now needs Node and the installed TypeScript compiler from `web/package-lock.json`; run `npm ci` first. It reads real JSON catalogs and production ASTs, including `features` and `hooks`. Both missing dependencies and empty scans fail. Inline `useText(zh,en)` and explicitly typed two-string callbacks are not catalog lookups; mixed files are checked per binding. Computed keys are reported as `dynamic_calls`, not certified as statically validated. No application module is evaluated to extract keys.

`TestR3BrowserJourney` remains a separate, opt-in shipping-image check: the standalone frontend, real Go API, PostgreSQL and loopback SMTP must all be available. Running component tests does not satisfy this browser check. Lack of a browser, client tools or DSN must be recorded, not called a green full regression. Image tags are build inputs, not immutable evidence: record the resolved image IDs/digests and actual tool versions for every run.

## Runtime HTTP contracts

The static gate binds every company route, including binary downloads and SSE.
`check_http_contract.py` additionally starts `TestCompanyHTTPContract` against a
new PostgreSQL fixture and the production Go router over loopback HTTP. It does
not start an SMTP delivery worker. Object bytes and Redis are disposable test
adapters; DNS verify/status are explicitly excluded rather than claimed as run.
The result lists actual success coverage and missing cases. The Go journey is
also mandatory in the full backend suite; the separate runtime gate checks its
captured bytes against the same OpenAPI using pinned JSON Schema 2020-12 tooling.

Always supply a **new** output directory. The gate refuses missing or skipped Go
execution, duplicate captures, stale schema hashes, malformed JSON, missing
success variants, invalid UUID/date-time formats, and unsafe download headers.
All `$ref` resolution is local; validation never fetches remote schemas. Captures
contain synthetic mail and already-consumed/revoked fixture invitation tokens,
never request authorization headers, production settings or employee data.
Keep raw `responses.json` local with mode 0600: CI explicitly excludes it from
uploaded artifacts. Publish the validation report, capture hash and execution
logs, not the raw response payloads.

## Required execution evidence

`scripts/required_go_tests.json` names mandatory **baseline sentinels**, not an exhaustive list of every existing or future test. The backend command still runs `./...`; all reported failures and unexpected skipped tests are rejected. Its sole documented optional skip is `TestR3BrowserJourney`, which must pass separately under suite `browser`. Add new R5 mandatory tests to the manifest when they are implemented; do not use invented future names.

```sh
# Bash: preserve a failed Go process OR failed tee instead of losing it to a pipe.
set -euo pipefail
SOURCE_SHA=$(git rev-parse HEAD)
set +e
go test -json -race -count=1 -timeout=180s ./... | tee go-test.jsonl
test_exit=$?
set -e
python3 -B scripts/check_go_test_evidence.py --suite backend \
  --log go-test.jsonl --exit-code "$test_exit" --source-sha "$SOURCE_SHA" \
  | tee go-test-evidence.json
```

The report records the supplied source SHA, log SHA-256, manifest SHA-256, actual test lifecycle counts and any explicit skip. The caller must freeze and identify the tested source; the checker does not cryptographically authenticate arbitrary uploaded logs. It rejects an empty/broken manifest, invalid events, missing run/pass/package completion, skipped mandatory tests or subtests, repeated/concatenated runs, unknown actions and nonzero process exit. Bare PASS text is never evidence. A missing DSN must produce a failed backend gate even when `go test` itself returns zero. A disabled browser must fail the browser suite, not be silently accepted as optional there.

The real-process counterexamples also run in the backend CI job after the positive run:

```sh
python3 -B scripts/test_go_evidence_integration.py \
  --output-dir /tmp/tabmail-evidence-new-run --source-sha "$SOURCE_SHA"
```

Use a new output directory for each invocation. This driver removes only the test DSN/browser flags from child environments, records actual Go JSONL, requires Go exit 0, and then requires a structured gate rejection specifically due to missing/skipped execution. Compilation errors, malformed logs, timeouts and accidentally matching the zero-test filter cannot satisfy the negative test.

## R5 pending security-behavior reproductions

The default `scripts/run_r5_audit_baseline.py` command runs the seven opt-in DB/HTTP failures; `--layer components` runs the original permission editors against a Go-owned temporary API and PostgreSQL fixture:

```sh
# Supply a disposable DB DSN and a NEW output directory. No production service.
python3 -B scripts/run_r5_audit_baseline.py --layer components \
  --output-dir /tmp/new-component-evidence --source-sha "$SOURCE_SHA"
```

Install frontend dependencies from the lockfile in the isolated source copy first. The component suite uses `web/vitest.r5audit.config.ts` and `*.r5audit.tsx`, not the normal green unit-test glob. It renders the original React/Sidebar/Base UI/SWR components and real API/session clients in jsdom; only the host auth context and absent layout observers are supplied. All fetches must target the Go fixture's loopback origin. Fixtures have short-lived synthetic credentials in temporary 0600 files and must never be published.

The Go parent requires the exact component security assertion, inspects captured HTTP writes and independently reads final PostgreSQL state. Both Go tests are expected to FAIL while the defects exist; driver success means `baseline_reproduced` with `product_fixed=false`, not a release check. Setup errors, missing Node/UI providers, skipped cases or compile failures are not accepted as reproductions. On P1 repair, adapt the new protocol and promote the secure assertions into the ordinary required regression suite; do not invert expectations to preserve vulnerable behavior.

Normal CI also runs the five `TestR5LockMap*` observations and four `TestR5Concurrency*` regressions from B01-D. The old attachment-before-user characterization has been replaced by a parent/user-before-attachment fence check after real deadlock reproduction. Complete enqueue/offboarding commands, draft grant revocation and independent concurrent submissions are tested with controlled PostgreSQL barriers. These passes are not a global deadlock-free certification; remaining scope is recorded in `docs/company-mail/R5-TRANSACTIONS.md`.

## Container-local source and build cache (B01-G)

B01-G completed the previously blocked reservation/reference regression using a different isolated layout: a tracked `git archive` copied into the disposable runner's `/src`, **container-local `GOCACHE=/var/cache/tabmail-go`**, and the already populated module cache mounted read-only at `/go/pkg/mod`. No working checkout, host Go build cache, Docker socket, production config or mail directory is mounted. This is a verified execution alternative, **not a diagnosis of the earlier Docker/linker timeouts**.

Use the existing test-tool image by its recorded immutable ID, a separately created session-labelled PostgreSQL container and an internal network without published host ports. Record every exact resource ID before running tests. B01-G used `GOPROXY=off`, `GOTOOLCHAIN=local`, `GOMAXPROCS=4` and `-mod=readonly`; missing cached dependencies must fail rather than trigger an unrecorded toolchain/download change. Keep checksum verification enabled. This layout does not certify a fresh install or current dependency audit.

Run and preserve evidence in distinct phases: the ten reservation/reference tests first, then build/vet, the complete backend with the **existing** `check_go_test_evidence.py`, and repeated targeted lock/transaction tests. After adding tests, rerun against the new exact source; do not reuse the first phase's source label. The targeted repeat check is supplementary and never replaces the full-suite required-test manifest.

Write each phase's stdout, stderr and actual exit code **inside the runner**, not only into a host process's captured output. Use a new output directory per attempt. A host timeout without the test's exit file is incomplete evidence, not a failed security assertion or a successful test. Retain the container until logs are copied out and verified; do not use auto-removal for evidence that exists only in its filesystem. At completion, hash both the retained raw logs and the source files actually tested; publish only synthetic, credential-free evidence.

For cleanup, independently inspect the stored IDs and `tabmail.validation.session` label. Remove only those owned containers and their empty internal network. After a previous stop timed out, read the actual state again; an exited container can be removed normally. If ownership/state is unknown or the daemon cannot confirm removal, record it and do not escalate into a global restart or system-wide kill.

## Resuming a long-running validation session (B01-J)

Inspect the exact owned runner's command, start time, state and remaining lifetime before resuming tests. In B01-J, a runner started with `sleep 3600` exited normally at its one-hour boundary after a complete backend phase; that exit interrupted the subsequent repeated phase. The backend's persisted process exit and evidence gate remained valid, but the unfinished phase was not counted. Only that disposable runner was restarted, its source reverified, and the repeated phase rerun into a new evidence directory. No database or host restart was used.

Keep failed full-suite attempts and incomplete phases separate from later successful attempts. B01-J retained both 180-second and 600-second package timeouts; after recovery the original 180-second full-suite budget passed. No business lease deadlines, per-case contexts, assertions, required tests or CI budgets were relaxed. This last lifecycle interruption was identified; earlier broad runtime slowdowns remain undiagnosed. A successful retry alone is not proof that the host cause was fixed.

