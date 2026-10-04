# Retained-evidence diagnosis and root recommendations

Frozen runtime source: `f52f8cbbf31dcbfb72ba062af6623248334a2e99`. Original delivered report: `88ba149cf20e4393e37a465f7b030eb282f8bcb9`. This appendix analyzes retained evidence and source only. No test, compiler, inventory producer, metadata capture or failed validation was rerun. The original FAIL/exit1, three class setup errors, 41 missing IDs and default metadata rejection remain unchanged. No product/test/lock/marker/budget changes or denied API retries occurred.

## Proven collector environment gap

| Frozen call path | Environment/tool behavior | Retained outcome |
| --- | --- | --- |
| `run_r5_source_version_tests.py:119` → `r5_source_runner_prepare.py:168–175` | Maps `R5_TEST_CACHE`/`R5_TEST_MODULECACHE` into a local `env` dict as `GOCACHE`/`GOMODCACHE`. This does not mutate `os.environ`. | Fresh typed preparation passed. |
| `r5_source_runner_prepare.py:214–215` → runner `:135–138` | Returns only four fixture/receipt variables. Runner rebuilds the child env from original `os.environ` and adds those fixture variables; it does not propagate preparation's Go cache environment. | Preparation success does not guarantee general child collector cache setup. |
| Runner child `:82–87,100` | Sets `legacy.GO`, `legacy.CACHE`, `legacy.MODULECACHE` in memory; records those values in `execution_paths`. | These recorded paths describe selected-source test configuration, not an attestation of every collector's Go environment. |
| `test_r5_transactions.py:14–17` → `check_r5_transactions.py:23–33` | `extract()` chooses `R5_GO` or `shutil.which('go')`; inherits `os.environ`, overrides `GOTOOLCHAIN=local` and `GOPROXY=off`, then runs `go run ./cmd/r5txinventory <root>`. It never maps `R5_TEST_*`. | Retained `ValueError` includes child stderr confirming failure to initialize the default Go build cache on a read-only filesystem. This is direct evidence for the transaction setup error and its 26 missing IDs. |
| `test_r5_compatibility.py:14–17` and `:151–152` → `check_r5_compatibility.py:38–45` | `collect()` executes literal `go` via PATH, with `dict(os.environ, TABMAIL_ROUTE_INVENTORY_OUTPUT=...)`. It ignores both `R5_TEST_*` and `R5_GO`; subsequent Node collection is reached only if the Go subprocess succeeds. | Both retained setup tracebacks show the same route-inventory Go command exiting 1. Fifteen IDs were missing. The child stderr/stdout was not retained in the surfaced exception; the cause is UNKNOWN. |

Compatibility's exact failed command was `go test -mod=readonly -count=1 ./internal/architecture -run ^TestR5RouteInventory$`, from its independent current clone, timeout 60. `check=True,capture_output=True` raises `CalledProcessError`; unittest's formatted traceback contains the command and returncode but not its `.stderr`/`.stdout` fields. The child process and exception objects no longer exist, and no separately persisted route-inventory child diagnostic is in this owner's retained output root. There is no evidence that Node collection failed in either setup attempt. The transaction error's cache cause must not be assigned to these two errors merely because they share an environment risk.

The original formal shell invocation explicitly supplied pinned Go in PATH plus `R5_TEST_GO`, `R5_TEST_CACHE`, and `R5_TEST_MODULECACHE`. It did not explicitly supply the general `GOCACHE`/`GOMODCACHE` variables. A complete child environment snapshot was not retained. Transaction stderr proves the default-cache destination for that particular producer; the selected-suite `execution_paths` report cannot fill the missing environment/child-diagnostic evidence for compatibility.

## Minimal owned setup control; no source fix established

For a future separately authorized session, expose the same reviewed, writable owned cache paths to the whole caller environment as well as the R5-specific variables. Keep the exact Go tool first in PATH, and set the transaction collector's supported `R5_GO` to that same executable. A proposed environment block is:

```sh
# Proposal only; not executed in this diagnostic session.
# GO is the verified Go1.25.7 executable. CACHE/MODULECACHE are owned,
# prehydrated exact directories; MODULECACHE_PARENT is its reviewed parent.
export PATH="$(dirname "$GO"):$PATH"
export R5_TEST_GO="$GO" R5_GO="$GO"
export R5_TEST_CACHE="$CACHE" R5_TEST_MODULECACHE="$MODULECACHE"
export GOCACHE="$CACHE" GOMODCACHE="$MODULECACHE" GOPATH="$MODULECACHE_PARENT"
export GOENV=off GOWORK=off GOFLAGS='' GOTOOLCHAIN=local
```

Before binding or any new formal dispatch, inspect ownership and prove create/remove capability only inside those owned cache directories; verify the resolved PATH `go`, Go1.25.7, and general `go env` cache paths under the exact proposed child environment. Retain safe path identities/hashes and private diagnostics. Do not change HOME, TLS/CA settings, compiler flags, dependency versions, source files or readonly module policy. Account for transaction's explicit `GOPROXY=off` when checking hydration. No general Go cache fix was applied after the formal failure.

This environment control addresses the proven propagation gap without a product/source change. It does not prove that compatibility will pass or that the metadata mismatch will disappear. No production or test code fix is established by these receipts. A runner improvement that maps the R5 variables into the dispatched child Go environment is an alternative tooling change, not necessary if the caller supplies these owned variables correctly; it would require separate source authorization and a new frozen SHA.

## Default metadata rejection: exact limits

The retained `pre-default.json` attestation hash recomputes correctly. Retained `post-default.error` shows rejection at `r5_selected_source_binding_v2.py:346–347`, rather than capture's source-byte/topology checks or its within-capture repeated-output check. Thus the candidate receipt hash is valid and the post-capture identity comparison failed. The precise changed identity fields are UNKNOWN: the observed failing receipt was not saved. `validate()` constructs `observed` at :338 and returns it only at :348 after comparison; the private post-control driver writes `post-default.json` only after validation returns. There is no legitimate way to recover those values from the rejection traceback, nor should a new capture replace them.

Race pre/post identity revalidation passed. All 2,148 tracked bytes remained identical and the source was clean at the frozen SHA. These prove tracked preservation, not equality of all default metadata fields. The fresh typed preparation's before/after default and race identities match, but both observations occur before the typed compile at `r5_source_runner_prepare.py:189–190`; they are not post-compilation stabilization evidence.

Validator identity excludes `attestation_sha256`, `hydration_diagnostics`, and per-command stderr (:339–344); it retains per-command `stdout_sha256`. Capture records hashes of raw Go-list outputs, checks repeated raw equality within each capture, and normalizes only the Go-env GOGCCFLAGS temporary prefix (:290–294). A raw-output change can therefore reject an identity even if normalized selected inputs match. This is a property of the code, not proof of this owner's rejected fields or of any `Stale`/`StaleReason` mechanism.

Root supplied the completed DB diagnostic `8cda0a51a5a9dc030a722b34423cef2b5bb1b9a8`, following DB report `c79cd26833008077902dcfec5116c42383bbec22`: changed fields were `commands[i].stdout_sha256` at indices 1, 3, 4, 5, 6, 8, 9, 10; substantive inputs were equal; raw stdout was absent, so a Stale/cache hypothesis remained unproven. These are root-provided comparative facts, not a duplicate inspection or substitute for this owner's absent observation. They support examining raw metadata/cache stability in a future controlled session, not waiving either rejection.

## Tooling changes needed for stronger future evidence

1. **Route-inventory failure visibility:** if the existing pipeline must retain failed child diagnostics, change the tooling around `check_r5_compatibility.collect():43` to preserve captured stdout/stderr and returncode in a new owned mode700 private output root before raising. Emit safe hashes/statuses publicly. Preserve the original command, flags, timeout and test selection. Merely changing caller cache variables cannot recover the lost diagnostics from this run.
2. **Rejected selected-observation visibility:** retain `observed` before the comparison at `r5_selected_source_binding_v2.validate():346`, and retain raw command stdout privately when capture records its hashes at :284–289. This would allow exact rejected-field and raw-output comparison in a new authorized run without relaxing the identity policy. No raw-output normalization or receipt waiver is justified yet.
3. **Compile-cache preparation in the final checkout:** the unchanged runner creates a new clone at :109–110 and calls preparation at :119. Warming another checkout cannot establish cache stability in that final clone. If root requires compile-only warmup before its first binding, a separately authorized preparation-tooling change must place that work inside `prepare()` after exact-source checks and before `before = go_selection(...)` at :179, in the same final checkout and original context. Keep warmup binaries private and separate from the freshly compiled, pinned binary later used for typed evidence. Repeated Go-list hydration alone is not proof of compilation-cache stabilization. The root-proposed future compile-only/raw-stdout control remains a proposal, not proof of the failure mechanism.

First apply the owned general-cache setup control in a separately scheduled preparation phase. Any observability or final-clone warmup source changes must be reviewed and frozen before a new formal authorization. Retain all original FAIL and rejected receipts, then collect fresh controls at the newly authorized source. Do not reuse old receipts, drop stdout hashes, infer all errors have one cause, or rerun the frozen union under this diagnosis authorization. The union remains UNQUALIFIED; sharedDB/HTTP/components, wholePG180 and central closure remain outside this task.
