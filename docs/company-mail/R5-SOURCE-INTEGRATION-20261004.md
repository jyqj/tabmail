# Reviewed source integration and qualification handoff — 2026-10-04

Branch: `cloud/r5-source-integration-20261004`, based on accepted union `5d3a8cfc81a77581919ea0b2a93f9c352fae5208` (product checkpoint `925264f1293120b70bfb1c6e8a865b65b1c5cad0`). Two ordinary no-ff merges preserve incoming ancestry: default `d97d45c0ea92d3733870ea8082a09f13160abe10` → `d9fb84c`, sharedDB `89e7af39aa53408225b284548bffe39ce467aa2d` → `1150633a4948c873349743658fad2661b08f1240`. This report/evidence/TODO commit freezes the final handoff HEAD; use the full SHA returned with delivery, not either historical test SHA.

## Exact scope and conflict decision

[Inventory](evidence/R5-SOURCE-INTEGRATION-20261004/inventory.json) includes every tracked before/after path and Git blob, plus SHA256 for all 153 imported changed paths. Before: 1,990 tracked files; after integration: 2,134. Default contributes 113 paths and sharedDB 40. The accepted union's delta from `ee3308fd` is actually 113 paths, not the preliminary 108; exact intersections remain default TODO only and sharedDB none. This handoff additionally creates this report and its new evidence directory and appends the reconciliation below to TODO.

The only conflict was the final append in `R5-TODO.md`. Both complete append histories were retained in order (accepted union first, incoming source history second). No conflict in source files or protocol appendix AB. All default imports except TODO exactly equal incoming d97; all sharedDB imports exactly equal incoming 89e7; every other accepted-union file is byte-identical. Older compatibility files are already ancestral and were not replaced. Both incoming heads and accepted union are ancestors.

Complete R5-SOURCE-COMBINED-20261003, R5-SOURCE-RUNNER-PREP-20261003, R5-CURRENT-WIRE-20261003 and R5-SHARED-RECEIPT-LEGACY-V2-20261003 directories are imported byte-for-byte, including original failures and Forbidden PR evidence. All previously accepted evidence is unchanged. PE/LF CAS/event behavior and successor403, case JSON/markers/layers, Go1.25.7, exactly two local replacements, schema19, official npm lock, and Go120/process180/case75/max4 are unchanged.

## Actual checks and qualification boundary

- Scoped runner-preparation/cold-web synthetic unit tests plus seven ordinary-receipt static tests: **62 PASS**. The fresh typed shipping envelope test was deliberately excluded because it requires formal source-runner preparation; it is NOTRUN, not passed/skipped.
- Protocol and component-evidence pure unit controls: **63 PASS**.
- Original protocol static check without `--run`: exit0. Structure only; its unresolved gaps remain.
- Go1.25.7 `go test -c -tags=r5protocol -o /tmp/r5-integration-postgres.test ./internal/store/postgres`, readonly/offline with existing module cache: exit0. Compiled only; zero Go tests executed and no DB started.
- Exact-path/ancestry/byte-preservation guards: PASS; [safe result](evidence/R5-SOURCE-INTEGRATION-20261004/guards.json).
- Source/document scoped whitespace check: PASS. Full imported-tree whitespace check: exit2 on eight pre-existing raw-log trailing-whitespace lines in imported diff-check.log/push.stderr; original bytes deliberately retained, not cleaned or called green.

No default, sharedDB, sharedHTTP, components formal producer, wholePG180, full Web/build/lint, audit, CI or actual mail/service runtime was started. No existing service/database/process was cleaned up. The historical 675 at a901211, sharedDB46 at 6138113 and components43 at 925264f are three separate historical qualifications. Their union is **UNQUALIFIED**. Missing89 slots means 46 DB + 43 HTTP; those adapters already exist. wholePG180, audit and central10/171 remain open.

## Next same-source qualification (separately authorized session)

Use a new real clean checkout of the full frozen handoff SHA for each execution owner. Pin every receipt to that same SHA, recapture independently after runtime, and reject any source drift. Keep private fixtures/binaries/raw runtime logs in an owned mode700 output root; publish safe statuses and hashes only. Requirements: Linux pinned-executable-FD support, Python3.12+ with original pinned requirements, Go1.25.7, original two forks, verified/prehydrated Go module/cache inputs, original official npm lock and validated Node/Vitest/TypeScript, fresh disposable owned PG with schema19 plus required pgcrypto/pg_trgm. No existing DSN, cloud service or existing database directory. Preserve platform CA verification. The default runner supplies fresh typed fixture preparation itself; do not inject old fixture receipts. Preparation can legitimately fail if first hydration changes observed command output: preserve that failure, prepare dependencies before separately scheduling runtime.

Commands below are handoff instructions only, **not run here**. Define `SOURCE` as the clean checkout of frozen SHA, `PRIVATE` as a new private root, and `GO`, `CACHE`, `MODULECACHE`, `NODE`, `DEPS` as reviewed owned exact tool/dependency paths. Work from `SOURCE`, put the pinned Go in PATH, and use each producer's unchanged bound environment (no inherited GOFLAGS/compiler overrides):

```sh
# Default source-version runner; R5_TEST_* configure exact execution paths.
R5_TEST_GO="$GO" R5_TEST_CACHE="$CACHE" R5_TEST_MODULECACHE="$MODULECACHE" \
  python3 -B scripts/run_r5_source_version_tests.py --root "$SOURCE" --output "$PRIVATE/default.json"

# Capture protocol archive manifest; keep the exact context object, not an array.
python3 -B scripts/r5_source_inventory.py capture --root "$SOURCE" --purpose protocol \
  --policy r5_current_local_inputs_archive_boundary_v4 \
  --build-context '{"goos":"linux","goarch":"amd64","cgo_enabled":1,"build_tag_sets":[["r5protocol"]],"race":true,"go_work":"off","go_flags":"","selection":"all_local_variants_superset"}' \
  > "$PRIVATE/archive.json"
ARCHIVE_PIN=$(sha256sum "$PRIVATE/archive.json" | cut -d ' ' -f 1)
# Run separately, on owned disposable DBs, preserving original 120/180 budgets.
TABMAIL_TEST_DB_DSN="$OWNED_TEST_DSN" python3 -B scripts/check_r5_protocol.py \
  --run shared-db --source-policy r5_current_local_inputs_archive_boundary_v4 \
  --source-manifest "$PRIVATE/archive.json" --source-manifest-sha256 "$ARCHIVE_PIN" \
  --output-dir "$PRIVATE/shared-db"
TABMAIL_TEST_DB_DSN="$OWNED_TEST_DSN" python3 -B scripts/check_r5_protocol.py \
  --run shared-http --source-policy r5_current_local_inputs_archive_boundary_v4 \
  --source-manifest "$PRIVATE/archive.json" --source-manifest-sha256 "$ARCHIVE_PIN" \
  --output-dir "$PRIVATE/shared-http"

# New default/race selections, runtime-v2 manifest and components batch contract.
python3 -B scripts/r5_selected_source_binding_v2.py --root "$SOURCE" --go "$GO" \
  --cache "$CACHE" --modulecache "$MODULECACHE" --context default > "$PRIVATE/selected-default.json"
python3 -B scripts/r5_selected_source_binding_v2.py --root "$SOURCE" --go "$GO" \
  --cache "$CACHE" --modulecache "$MODULECACHE" --context race-r5protocol > "$PRIVATE/selected-race.json"
python3 -B scripts/preparation/r5_external_runtime.py capture --source "$SOURCE" --root "$DEPS" \
  --archive "$PRIVATE/archive.json" --default "$PRIVATE/selected-default.json" \
  --race "$PRIVATE/selected-race.json" --node "$NODE" --go "$GO" \
  --cache "$CACHE" --modulecache "$MODULECACHE" > "$PRIVATE/runtime.json"
export TABMAIL_R5_EXTERNAL_MANIFEST="$PRIVATE/runtime.json"
export TABMAIL_R5_EXTERNAL_MANIFEST_SHA256=$(sha256sum "$PRIVATE/runtime.json" | cut -d ' ' -f 1)
python3 -B scripts/preparation/r5_external_batch.py capture --source "$SOURCE" --go "$GO" \
  --mode components > "$PRIVATE/components-contract.json"
CONTRACT_PIN=$(sha256sum "$PRIVATE/components-contract.json" | cut -d ' ' -f 1)
python3 -B scripts/preparation/r5_external_batch.py validate \
  --contract "$PRIVATE/components-contract.json" --pin "$CONTRACT_PIN"
# Only after root reviews fresh prestart controls and authorizes one formal batch:
python3 -B scripts/preparation/r5_external_batch.py run \
  --contract "$PRIVATE/components-contract.json" --pin "$CONTRACT_PIN" --output "$PRIVATE/components"
```

The unchanged external-batch owner/fixture capability and cleanup acknowledgements must govern the components run; no generic shared-components substitution. Validate matching archive and selection receipts again after each runtime. Do not join old 675/46/43 with new receipts, or infer whole-product/central closure from scoped success.
