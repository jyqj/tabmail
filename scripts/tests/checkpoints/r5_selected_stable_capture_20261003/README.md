# Selected attestation stable capture — focused progress appendix

Base: PR21 `41b015c30c66b3ba58a3c1395e8559ebcd27a65f` (normal Git fetch).
Independent branch: `fix/selected-attestation-stable-capture`.
Code freeze: `70134d4e6b999df5a08464046436e474b7713c04`.
PR20 reproduction: `722fcf2f5f9d28c8f6372494b49cccc7b8816467`.
No `.agents/skills` exists in the selected workspace or repository. This change
owns only the selected helper/tests and this new appendix. Central R5-TODO,
inventory, archive boundary, CI and previous evidence are untouched.

## Stable identity and version

Selected query runs first, inside the existing before/after source, topology,
module and hash protections. Its successful completion hydrates selected test
modules before the formal MVS observation. The command order is now
`env → selected → MVS → selected → MVS`. Both selected and MVS repeated stdout
must match byte for byte, and all four original raw stdout SHA256 values remain
bound to the receipt. No hashes are dropped or normalized to conceal hydration.
No retry or hidden prewarming command is introduced. Source changes during the
first selection are refused, as are changes during the final observations.

This formal command-order/identity change is explicit companion attestation v2:
`r5_root_selected_local_attestation_v2`, `schema_version=2`. Old selected v1 and
historical source v2/v3 receipts are incompatible, refused before live dispatch.
Product database schema19, Go1.25.7, `GODEBUG=asynctimerchan=0`, both third_party
replacements, race/tags, readonly mod and original VCS stamping are preserved.
No generated, external-cache, toolchain or native bytes gain qualification.
Existing qualification blockers are not upgraded by this focused change.

## Actual reproduction and final checks

`pr20-final-repro.log`: first PR20 cold capture succeeds; immediate validate
rejects; warm capture/validate passes. Root MVS 138, selected records 630, local
611. Five actual Dir additions are recorded (go-spew, deep, go-difflib, testify,
yaml.v3); normalized MVS is equal while the raw hashes differ. Complete raw MVS
observations and the actual first receipt are retained as gzip artifacts.

`final-v2-stable-tests.log`: **8/8 PASS**, 38.816 seconds. The module cache is a
private copy of the shared seed; the five extracted modules are actually removed
and asserted absent (download cache remains authenticated). GOCACHE starts empty.
Cold capture immediately validates, then a warm capture immediately validates.
Actual PR21 shape: MVS **138**, records **630**, selected local **613**, generated
**60**, historical Go inputs **21**. PR21 includes additional existing Go tests;
its 613 local count is retained rather than replacing it with PR20's 611.
Both bound selected hashes match and both bound MVS hashes match. Additional
controls reject raw MVS whitespace drift despite normalized equality, a re-signed
raw-hash forgery, old selected v1 before dispatch, a real parser.go byte change,
a real go.mod x/text version change (readonly metadata command refuses), and an
actual parser.go change during the initial selected query.

`v2-existing-tests.log`: **12/12 PASS**, 12.322 seconds, including live full-scope
hashes/roundtrip, midflight drift, omission/hash/context negatives, filesystem,
nested-module, fork and metadata boundaries. Run in a normal independent Git
clone with private caches using the existing test module, unchanged assertions
except the explicit selected-command index required by the new order.

```sh
python3 scripts/tests/test_r5_selected_stable_capture.py
# Existing suite: independent clone without web/node_modules; bind its CACHE and
# MODULECACHE constants to private caches before loading it with unittest.
python3 scripts/tests/test_r5_selected_source_binding.py
```

The new suite creates clean independent Git clones itself; it never changes the
source checkout or the shared cache. Go uses the pinned official registry and
checksum service. No offline-cache assumption is treated as a permission denial.
The cold receipt binds its actual disposable source snapshot at code freeze;
the later evidence append is not claimed to be the captured source tree.
`artifact-sha256.json` hashes the raw supporting artifacts.

## Earlier failures, preserved rather than erased

- `pr20-repro.log`: Git worktree selected query failed with native VCS exit128;
  stamping was retained. Normal standalone clone resolves that environment issue.
- `fixed-tests.log`: initial existing checkout run: 6 tests, 2 PASS / 3 ERROR /
  1 FAIL. web/node_modules/flatted contains Go inputs rejected by the existing
  boundary. Clean independent clones resolve this without widening the boundary.
- `pr20-repro-standalone.log`: initial removal used ignored filesystem errors;
  read-only extracted cache directories remained, so the supposed cold baseline
  unexpectedly validated. This was a test-setup error. The final harness chmods
  only copied private module directories before removal and asserts absence.
- `fixed-standalone-tests.log`: same incomplete removal: 6 tests, 5 PASS / 1 FAIL.
- `fixed-forced-cold-tests.log`: actual cold roundtrip passes, but 6 tests have
  5 PASS / 1 FAIL because the assertion incorrectly copied PR20's local 611 count;
  PR21's actual count is 613. No selection was omitted to force the old count.
- `final-stable-tests.log`: corrected count, pre-version-update suite 6/6 PASS.
- `v2-stable-tests.log`: intermediate v2 suite 7/7 PASS; final 8/8 adds actual
  source mutation during the first selected observation.

## Limits and delivery

Not run: product Go tests, PG/database/runtime/watchdog, scale, Method19, SML,
wholeCI, deployment or merge. Metadata/static binding only; no production mail,
databases or credentials accessed. Shared preinstalled environment service files
were not sourced. GitHub CLI PR metadata read returned `Forbidden`; no alternate
API route was used to bypass it. Draft-PR delivery status is reported with the
remote SHA in the final handoff; prepared PR text is retained here.
