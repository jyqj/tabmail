# Root selected local attestation v1 — overall BLOCKED

Based on boundary branch `64cd11816cfd94c9dc80e65578edb06b92725e84`, production checkpoint `93005b644c1f39fa85072fe913156cf3d6c798e1`; historical independent evidence `8de4809516a39ad43b0e17afa288bcedab8b40a7` remains unchanged. This companion is deliberately incompatible with v2/v3 receipts. Their policies and qualification are not upgraded; the new closure binds a fresh v3 source receipt, exact root metadata and selected static local bytes together.

Allowed historical inputs are the individually enumerated `STATIC_EVIDENCE` Go paths in the implementation. This is no permission to read evidence directories, logs, DB/object/cache payloads or private URIs. Every selected repository input must already be in the declared v3 superset or that static allow-list; actual metadata cannot extend the read authority. No Go files were moved/renamed, no product/frontend/fork/mod/sum/TODO changes, no reduction of `./...` or fork patterns.

Parent independent counterexample `30c25c57402a92f7126cb25ad77813fec811e470` reports an excluded-metadata regular-to-symlink race. `_check_excluded_metadata` is owned by a separate narrow repair and is not changed here. Hence the receipt explicitly records overall `blocked` and `excluded_metadata_boundary=blocked_pending_separate_helper_race_fix`. Passing selected-binding tests do not resolve that blocker. A unique integrator must combine the helper repair and independently recheck both domains before any qualification change.

## Actual environment and refusals

Go `/workspace/tabmail-cloud/tools/go/bin/go`, Go1.25.7 linux/amd64, GOAMD64=v1, CGO=1, race=true, tags=r5protocol; GOFLAGS='', GOWORK=off, GOENV=off, GOTOOLCHAIN=local, GODEBUG=asynctimerchan=0. Full effective Go environment including native flags is in `attestation.log`. The ephemeral GOGCCFLAGS temp prefix alone is normalized and its hash domain is named explicitly. Transport proxy settings are inherited only for HTTP transport; no proxy URI is serialized. Public GOPROXY and GOSUMDB are fixed; readonly commands preserved root locks.

- Initial offline metadata attempt refused missing cache metadata; clean network environment first lacked HTTP proxy transport. Neither is claimed successful. Public proxy later supplied metadata with readonly lockfiles.
- Original environment `/workspace/tabmail` selected untracked `web/node_modules/flatted/golang/pkg/flatted/flatted.go`; refused as unclassified, preserving the installed tree in place. No npm/source relocation or broader allow-list was used.
- Git worktree `/workspace/tabmail-selected-binding` reproduced the original VCS exit128 with stamping unchanged. `worktree-vcs-failure.log` is the actual failed test run: 7 negative methods PASS, actual root setup ERROR, no qualification.
- The same Git source commit was checked out in a standalone local Git clone `/workspace/tabmail-selected-checkout`, with an independent `.git` directory and no installed node_modules. Full original command succeeds there, including stamping. This is a different explicitly named checkout environment; it does not erase either refusal. The delivery branch/remote/credentials are unchanged; the evidence checkout is not used to publish.

## Command and binding shape

```sh
python3 scripts/r5_selected_source_binding.py \
  --root /workspace/tabmail-selected-checkout \
  --go /workspace/tabmail-cloud/tools/go/bin/go \
  --cache /workspace/tabmail-cloud/gocache \
  --modulecache /workspace/tabmail-cloud/gomod
```

The only metadata queries are `go env -json`, `go list -mod=readonly -m -json all`, and:

```sh
go list -mod=readonly -deps -test -race -tags=r5protocol -json ./... github.com/jhillyerd/enmime/v2/... github.com/emersion/go-smtp/...
```

No automatic `-buildvcs=false`, tests, PG, SMTP, controllers or scale. Selected and MVS metadata are re-read, every permitted source hash/topology checked before/after, and only fixed local identities plus two exact root replace identities are admitted. Receipt validation independently re-captures rather than trusting metadata/expected filenames from the receipt. Generated testmain is classified without opening cache payloads; external module cache, standard/toolchain source and native/assembly are separate records with unknown byte qualification. No compiled/native/cold-build/runtime qualification is claimed.

`attestation.log` is canonical JSON, despite its `.log` extension. It and test logs are explicit excluded output artifacts under the pre-existing v3 `.log` rule, preventing self-referential receipt capture. The companion code, tests and this README are self-bound through the fresh v3 source receipt. Fresh metadata observed 630 package records, 138 root-MVS modules, 611 unique local paths including 21 static historical evidence Go paths, and 60 generated testmain records. These counts are outputs, never inputs used to manufacture a manifest. Root MVS, two original replacement identities, every local selected path/field/hash and exact argv/command hashes are in the receipt.

## Pure verification and delivery dependencies

`python3 -m unittest discover -s scripts/tests -p 'test_r5_*source*.py' -v`: 60 methods PASS, comprising 48 existing source-policy methods and 12 new methods. New fixtures independently reject forged paths, secret/evidence reads, missing/symlink sources, extra/unknown nested Go inputs including excluded static outputs, wrong fork/third replace, generated/external fake paths, compiled input metadata, duplicate keys and hash drift. Actual root tests use live fixed metadata, roundtrip recapture, missing/extra/hash/flags/ABI/legacy mutation of that actual receipt and a midflight drift fault. The parent helper race cases are a distinct unresolved domain, not included or claimed repaired.

Delivery depends on `fix/r5-source-excluded-metadata-20261003` and the forthcoming excluded-metadata race repair. No Method19/SML/wholeCI/build/runtime green; only the selected-binding implementation and pure scope. GitHub draft PR authentication was unavailable (`GH_TOKEN` invalid); no identity/remote/permission fallback is authorized or attempted.
