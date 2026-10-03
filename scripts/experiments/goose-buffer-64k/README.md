# Isolated Goose parser experiment diagnostics

These files are self-authored. They do not install or qualify a third product
replace. Historical results and the one-run limit are recorded in
`docs/company-mail/GOOSE-BUFFER-64K-TODO-APPENDIX-20261003.md`.

`prepare.py --root NEW_OWNED_PATH --repo PRODUCT_REPO` downloads official fixed
v3.27.3, independently verifies the original go.sum zip Hash1, creates two
owned source trees and temporary modfiles, and emits patch text. Existing paths
are refused; product files are never edited. The experimental temporary third
replace is explicit. The original two fork paths are only made absolute.

For the parser diagnostic, install `diag.go.txt` as
`TREE/cmd/goosebufferdiag/main.go` and `wrapper.go.txt` as
`TREE/internal/sqlparser/experiment_diag.go` in **both** trees. Build both with
Go1.25.7, `-race -mod=readonly -modfile=ARM.mod`, from the product root, selecting
`github.com/pressly/goose/v3/cmd/goosebufferdiag`. Execute each binary with the
same migration directory and a distinct JSONL output filename. The complete
execution environment is `env -i GODEBUG=asynctimerchan=0
GOOSE_EXPERIMENT_VALUE=FROZEN_NON_SECRET`; all other variables are absent. Save
stdout to `ARM-diag-summary.json`. Compare complete raw JSONL **bytewise**, and
stop on any difference. This is a semantic diagnostic, not wholePG performance.

Remove the self-authored diagnostic bridge and command from both temporary
trees before wholePG compilation. Record whole-tree file hashes and full source
and modfile patch texts. Build with `go test -c -race -mod=readonly
-modfile=ARM.mod -o ARM-pg.test ./internal/store/postgres`. Capture `go version
-m` and `go list -deps -test -json` under the corresponding temporary modfile.
Inventory `ARM-pg.test -test.list='^Test'` without selecting a subset, and require
both inventories to match. Frozen historical inventory is 404 top-level IDs.
`source_binding.py` binds the already-collected metadata and selected file
bytes, asserting actual Goose paths in the intended trees. Native/compiler
qualification is outside its claim.

Use only owned PATH/toolchain/module/build caches and TMPDIR. Never source
production secrets. The historical tool root is `/workspace/tabmail-cloud`.
Build preparation and package metadata capture happen outside timed runs. Start
a separate newly initialized owned native PG cluster per arm, with loopback
ports, default durability and no migrated template. Do not use existing app
clusters. Original test SMTP fixtures are loopback and remain unchanged.

`run_wholepg.py ARM --root EXPERIMENT_ROOT --repo PRODUCT_REPO --tool-root
TOOL_ROOT --port OWNED_CLUSTER_PORT` runs a **prepared** entire PostgreSQL race
binary through test2json with count=1, timeout=180s and allocation profiling. It
freezes a complete allowlisted environment and creates a fresh empty arm admin
DB. It has an exclusive once marker, no test selection, and no retry. The final
runner uses the package directory, matching `go test` semantics. The original
baseline cwd mistake and the exact correction diff are retained separately;
the final runner is not claimed to reproduce that erroneous baseline.

`summarize.py --root EXPERIMENT_ROOT --output EVIDENCE_PATH` asserts bytewise
parser equality and preserves test failures, skips, unfinished and unstarted
IDs, along with public URI-redacted synthetic logs. For the historical run only,
`--baseline-cwd-error` explicitly annotates its invalid comparison. Do not pass
that flag for a correctly prepared future experiment. Existing first-run raw
files are never re-executed by this summarizer. Direct test2json panic streams
can lack package terminals; preserve the absence.

Stop owned native clusters after observation. Retain residual-database facts
from timeouts, without claiming cleanup completion. Never commit third-party
source trees or zips, DB data, binaries/profiles, caches, pyc or keys. Publish
only the diagnostics, small patch texts and synthetic redacted/hash evidence.
The historical allocation-profile analysis encountered a readonly-default-cache
failure; it was not retried or bypassed. Profile pins are kept and wholePG
allocation totals are unparsed. Any future run/analysis requires a separately
decided scope, rather than silently exhausting another baseline.

Root subsequently authorized analyzing the existing profiles with owned caches.
`analyze_profiles.py --root EXPERIMENT_ROOT --evidence EVIDENCE_PATH --tool-root
TOOL_ROOT` launches only official installed Go pprof/readelf, never a test or
native service. It checks original profile/binary SHA-256 pins, ELF/profile
mapping build IDs and every selected source hash, and sums raw sample values to
verify official top totals. It retains the original cache-error record. Its
weighted sampled allocation totals are not exact runtime.TotalAlloc, and the
historical wrong-cwd baseline remains an invalid controlled comparison.
