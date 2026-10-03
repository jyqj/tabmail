# Source preparation proposal, 2026-10-03

Base `ee3308fd3217246c9bdd43b07ae0609ebae6aeb6`; historical diagnostic
`ac5db2ee72b97b027a14d6e885d3870276bfaf82`. This independent preparation
does not adopt a new source policy or assert component/product green.
Only new preparation helper/tests and this new evidence directory are changed.

## Git/VCS diagnosis

Read both complete historical selected failure JSONs, initial preparation stderr,
component failure manifest and diagnostic README. The JSONs retain the fixed
default/race hydration argv and error 128, but **do not contain preceding Go env,
Git stderr, Git metadata, or a complete environment receipt**. The recorded cwd
`/workspace/r5-current-wire` does not exist in this selected environment. Therefore
the historical underlying Git error remains unproven; no PATH/environment fix
is justified from that evidence, and no successful observation replaces that red.

A standalone real Git checkout at the exact base, without installed source-local
dependencies, completed both unmodified selected-v2 captures with Go 1.25.7.
With the exact captured clean environment and cwd, Go's actual Git commands
`git status --porcelain` and
`git -c log.showsignature=false log -1 --format=%H:%ct` both exit 0 and report
the exact base commit. Existing Git was used through inherited PATH; no Git
configuration, host permission, credentials, stamping flags or runner code changed.
This proves reproducibility in this checkout, not resolution of the unavailable
historical cwd. Root needs the original cwd/.git metadata and exact prep environment
to assign a specific historical cause. A real permission denial remains a stop.

After external npm preparation the original default receipt revalidated with the
existing validator, including equal source and selected local inputs. A deliberately
wrong source SHA with a recomputed attestation SHA was still rejected as
`selected attestation missing/extra/drifted/context mismatch`.
Both captures retain overall `blocked`; unknown external/generated/native inputs,
Method19, SML and wholeCI remain unknown. Different context source identities are
kept separate; no old protocol source SHA is reused as a selected identity.

## External dependency preparation

Copied only the exact base's `web/package.json` and `web/package-lock.json` into
an independent owned root; used normal lifecycle-enabled
`npm ci --prefix <ownedroot> --cache <private-cache> --registry https://registry.npmjs.org --no-audit --no-fund`.
Exit 0: 760 installed packages. All non-bundled lock records resolve to official
HTTPS registry npm tarballs with SHA512 integrity. Six `inBundle` records have no
independent URL/integrity and are explicitly bound to their enclosing locked
registry package. They are not silently classified as fetched standalone packages.
The first over-strict audit rejected those legitimate bundled records; it was
corrected to distinguish enclosing-tarball provenance. Installation used the
unchanged official lock and completed normally; no lock rewrite occurred.

The candidate observer reads bytes and metadata only. It executes no npm/source
code, never installs, and never launches components. It records 45,584 regular
files, exact targets for 41 declared `.bin` links, modes, byte sizes and hashes;
two equal observations are required. npm lifecycle outputs are observed bytes,
not independently asserted original registry bytes. Twelve synthetic negative
controls pass, including escaping/undeclared links, a source-local root, root
symlinks, registry/lock/version drift, changed installed bytes, and rehashed
terminal promotion. This observer is **not** a production descriptor lease or
resolver authorization; its double walk does not close a concurrent ancestor
replacement race. Production adoption must implement the stronger binding below.

## Actual loader limitations and proposed supported layout

The existing consumers use source-local CLI paths:

* `scripts/check_r5_protocol.py:561`: relative `node_modules/vitest/vitest.mjs`
  with cwd `source/web`.
* `internal/api/handlers/r5_protocol_component_observations_test.go:519`:
  absolute `source/web/node_modules/vitest/vitest.mjs`, same cwd.

Both files belong to other owners and were untouched. Merely exporting NODE_PATH
or changing the top-level Python CLI does not adapt the Go-owned consumer.
Current installed topology still makes archive-v4 reject source-local `.bin`
links. No sourcecheck pruning, symlink substitution or archive allowlist was added.

Proposed owned layout: `<toolroot>/package{,-lock}.json`,
`<toolroot>/node_modules`, `<toolroot>/source/` (clean real Git checkout), and
private outputs/cache outside source. Source has no node_modules directory or link.
Both consumers must launch exactly the validated external absolute
`<toolroot>/node_modules/vitest/vitest.mjs`, preserving cwd `source/web`, original
config/test inputs, and race120/process180. No generic environment-selected binary.

Resolution-only Node probes loaded no source modules: imports from a sibling
dependency root fail with MODULE_NOT_FOUND; metadata for the proposed nested
importer finds the pinned dependencies through normal ancestor lookup. The nested
probe is a prospective importer path, **not a real runtime or relocated capture**.
Local Vite 8.2.2 `dist/node/chunks/node.js` was inspected: `resolvePackageData`
(1892) walks ancestor node_modules; `tryNodeResolve` (33043) selects importer/root
basedir; `bundleConfigFile` (37022) resolves config dependencies from the importer
and emits resolved external paths. Config imports `vitest/config` and
`@vitejs/plugin-react`; setup imports testing-library and vitest. ESM/CJS conditions,
Vite transforms, workers and jsdom must all be exercised by the adopting owner.
These observations support the layout proposal, not complete runtime acceptance.

## Exact adoption request for root review

Add an **independent** `r5_external_dependency_runtime_v1` manifest and status
contract, leaving archive-v4 and selected-v2 semantics unchanged. Candidate schema
and status schema are attached here; they do not authorize execution today.

The runtime manifest must bind source root and archive-v4 manifest SHA, exact
default/race selected receipt SHAs, original package/lock SHAs, canonical external
installed files/dirs/links manifest SHA, source and dependency roots' descriptor
identities, node/npm/CLI/config/loader hashes, exact argv/cwd, resolver conditions,
no shadow node_modules in source or between source/web and owned toolroot, and
all resolved modules to the exact lock package/version and installed file hash.
Any builtin is explicit; every other resolved executable/input must belong to
the source manifest or bound dependency manifest. Pin package exports/subpaths,
platform/optional/bundled records and bin authority to registry tarball metadata
with the original SRI, retaining lifecycle-generated outputs as a separate class.

Hold no-follow descriptor bindings and immutable consumer leases through load and
post-runtime validation. Reject ancestor/root/path swaps, changed files/links,
escaping links, specials, unknown installed package roots, unknown resolver results,
source/ancestor shadow packages, poisoned NODE_PATH/NODE_OPTIONS/npm configuration,
wrong platform/conditions, wrong lock, wrong source, mismatched producer CLI,
schema/manifest/status rehash promotions, missing output and skipped runtime.
Prepared bytes are not an adoption receipt. Status must explicitly distinguish
UNADOPTED, dependency-policy failure, ready-for-scoped-run, actual run failure and
scoped evidence passed; source/runtime qualification stays separate.

Root review and coordinated edits to the two consumer files are required before
the new explicit contract can be supported. No shared-components producer was
started in this task. NOTRUN and all historical reds are retained. No DB/HTTP,
real mail, DNS, provider, deployment or merge was performed. Raw command output,
full manifests and dependency bodies remain in the mode700 private preparation
workspace; public evidence contains safe summaries and raw hashes only.
