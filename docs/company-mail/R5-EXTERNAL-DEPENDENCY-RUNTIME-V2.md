# External dependency runtime candidate v2: cache-disabled invocation

V1's actual source freeze and failed receipt remain unchanged in the
R5-EXTERNAL-RUNTIME-20261003 checkpoint. V2 is a distinct UNADOPTED policy and
schema; v1 manifest/status rehashing cannot authorize v2. Archive-v4 and selected-v2
are unchanged. Original product component/tests, package/lock, Go 1.25.7, both
replaces and audit inputs remain unchanged.

Both consumer command builders add the fixed official Vitest 4.1.11 CLI options
`--cache=false --experimental.fsModuleCache=false`. They retain original formal
configs/test inputs, cwd source/web and race120/process180/fixture75 budgets.
Manifest v2 binds both options, exact argv templates, original configs, the new
separate probe config, and cache policy. `on_disk_runtime_cache_root=null` means
filesystem caches are disabled, not redirected into source/dependencies. Unknown
flags, enabled cache options, missing disable options and source-local cache
artifacts fail closed. Child env pollution is checked explicitly too.

The fixed installed official implementation was inspected: Vitest CAC declares
both options; ResultsCache.setConfig leaves cachePath unset for cache=false and
writeToCache returns without a path. Filesystem module caching is explicitly
false. Vitest's optimizer is disabled unless test options enabled=true; both
original configs omit optimizer overrides and remain pinned. Vite's normal bundle
config loader is retained: these original .ts configs and package.json without
an ESM type use its CJS in-memory config path. No sourceguard pruning, shadow
exception, NODE_PATH, config replacement, chmod/chown or OS immutability claim is
introduced. Real source/dependency before/after inventories, rather than these
static observations alone, decide runtime acceptance.

A separate small TSX probe exercises React render/jsdom, normal createRequire CJS
React loading, dynamic ESM jsdom import and the actual Vitest worker ID. Python
uses check_r5_protocol.external_component_command; Go uses the same command
builder and verified helper as the formal Go consumer. Both use one pinned CLI,
the new dedicated probe config and full no-follow pre/post validation. Probe
success is infrastructure evidence only; it never credits business case layers
or complete dynamic-loader coverage. Probe and formal outputs are fresh and
outside source/dependencies.

The supported layout remains an owned toolroot with a real clean source Git
checkout and normal ancestor dependency lookup. Exclusive cooperating owner
tokens and anchored descriptors are retained, including the cwd. The trust
boundary remains not_qualified_for_hostile_concurrent_mutation; temporary hostile
writes cannot be ruled out by terminal equality. The formal v2 run requires a
freshly validated prepared identity after the two-entry probe, keeps previous
failure evidence separate and retains all actual reds, missing layers and unknown
coverage. No PR API rejection path is retried.
