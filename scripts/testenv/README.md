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

Commands inside the source snapshot:

```sh
python3 -B -m unittest discover -s scripts/tests -p 'test_*.py' -v
node --test scripts/tests/i18n_sources.test.cjs
python3 -B scripts/check_i18n_keys.py
python3 -B scripts/check_contract_drift.py
go build ./...
go test -json -race -count=1 -timeout=180s ./...
go vet ./...
(cd web && npm ci && npx tsc --noEmit && npm test && npm run lint && npm run build)
```

The i18n gate now needs Node and the installed TypeScript compiler from `web/package-lock.json`; run `npm ci` first. It reads real JSON catalogs and production ASTs, including `features` and `hooks`. Both missing dependencies and empty scans fail. Inline `useText(zh,en)` and explicitly typed two-string callbacks are not catalog lookups; mixed files are checked per binding. Computed keys are reported as `dynamic_calls`, not certified as statically validated. No application module is evaluated to extract keys.

`TestR3BrowserJourney` remains a separate, opt-in shipping-image check: the standalone frontend, real Go API, PostgreSQL and loopback SMTP must all be available. Running component tests does not satisfy this browser check. Lack of a browser, client tools or DSN must be recorded, not called a green full regression. Image tags are build inputs, not immutable evidence: record the resolved image IDs/digests and actual tool versions for every run.

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

Normal CI also runs five `TestR5LockMap*` PostgreSQL observations and the required-test manifest includes them. The enqueue attachment-before-user test explicitly records a current cross-path inversion: its pass is NOT a global deadlock-free certification. Read `docs/company-mail/R5-TRANSACTIONS.md` for pending complete-path concurrency experiments.


