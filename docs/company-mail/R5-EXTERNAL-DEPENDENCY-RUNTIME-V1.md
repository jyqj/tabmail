# Independent external dependency runtime candidate v1

Policy `r5_external_dependency_runtime_v1` is an independent, UNADOPTED
candidate. It does not change archive-v4, selected-v2, their unknown external,
native or generated inputs, or the central 10/171 gates. Historical VCS error
128 remains unexplained; successful new captures do not establish its cause.

The supported layout is an executor-owned physically isolated temporary
`toolroot/package{,-lock}.json`, `toolroot/node_modules`, and a real clean Git
checkout at `toolroot/source`. Source contains no node_modules or symlinks.
No chmod/chown or security-sensitive host permission changes provide isolation.
The executor uses the original official lock with normal lifecycle-enabled npm
ci and original SRI. Bundled children retain enclosing registry-parent provenance;
absent optional packages retain their exact lock platform declarations. Installed
and lifecycle bytes are observations, not independent tarball-member provenance.

The capture command validates both fresh default and race-r5protocol receipts
using the unchanged selected-v2 validator, then binds exact receipt bytes,
archive policy/receipt, source tree, roots' descriptor identities, package/lock,
complete dependency entry types/modes/links/content root, node, Python, Git,
Vitest CLI, configs/setup, platform, argv templates and cwd. The caller supplies
an independent manifest byte SHA256 through `TABMAIL_R5_EXTERNAL_MANIFEST_SHA256`.
Manifest status is always UNADOPTED; rewriting it into a run status is rejected.
Both consumer entries require this manifest and use the same absolute verified
external CLI with cwd source/web and their original config/test inputs.
Go race120/process180 remain unchanged, as does its existing 75-second per-fixture
Node context. No source-local fallback or NODE_PATH workaround exists.

Every consumer takes an exclusive owner token, holds root/ancestor descriptors,
and performs full no-follow, directory-relative source/dependency checks before
launch and after terminal completion, including failures/timeouts. Standard
.bin links are admitted only for metadata-declared targets that are pinned regular
files in the same dependency root; undeclared and escaping links fail. Source,
lock, config, executable, platform, dependency or descriptor drift fails closed.
NODE_OPTIONS, NODE_PATH and inherited npm_config overrides are rejected.

This is **not_qualified_for_hostile_concurrent_mutation**. Tokens serialize
cooperating executors; descriptor observations do not prevent a malicious writer
from temporarily changing and restoring bytes during runtime. No OS-enforced
immutable lease is claimed. The candidate records normal ancestor resolution
and leaves complete dynamic ESM/CJS/Vite/worker/jsdom resolution coverage unknown.
Actual real-component execution is required for any scoped runtime observation;
no metadata probe is treated as a loader-completeness proof. Runtime-generated
writes to the pinned dependency tree also fail validation.

`run_r5_external_scoped.py` records UNADOPTED, preparation_failed,
ready_for_scoped_run, run_failed or scoped_pass separately from source
qualification. It invokes one formal shared-components producer with an explicit
own disposable PostgreSQL DSN and synthetic loopback fixtures. Missing output,
skips, process failures, target reds and final missing layers remain evidence;
preparation success never implies product green. Private logs/manifests are not
publication artifacts. Only safe status, summaries and hashes are publishable.

Example capture (all receipt paths/output/cache outside source):

```
python3 source/scripts/preparation/r5_external_runtime.py capture \
  --source /absolute/toolroot/source --root /absolute/toolroot \
  --archive /private/archive.json --default /private/default.json \
  --race /private/race.json --node /absolute/node \
  --go /absolute/go --cache /private/gocache --modulecache /private/gomod
```

Execution requires an independent externally retained SHA256 of these exact
manifest bytes. The run-status file cannot authorize or promote another run.
