# Supplementary build in an independent Git clone — 2026-10-07

Result: **PASS**.

This supplements the earlier worktree build failure without replacing that failure or rerunning any successful test. The earlier combined test/vet evidence remains in `../wave-final-go/` and retains its original result and bytes.

Commit: `e32564304c0c84aa80b1184e72129fb0316d989d`. Tree: `bfa06a61ce4bc8411243d3b57de26db5e161dc8b`. Clone: `/workspace/scratch/458de7991ac3/tabmail-wave-build-independent`.

Toolchain: `go version go1.25.7 linux/amd64`. The original build command `go build -mod=readonly ./...` ran once with default VCS stamping and the existing offline module/build caches. Exit code: **0**.

## Commands

- `git clone --local --no-hardlinks --no-checkout /workspace/scratch/458de7991ac3/tabmail-wave-verify /workspace/scratch/458de7991ac3/tabmail-wave-build-independent` — exit 0; 0.307 s; raw log `clone.log`.
- `git checkout --detach e32564304c0c84aa80b1184e72129fb0316d989d` — exit 0; 1.445 s; raw log `checkout.log`.
- `/workspace/scratch/458de7991ac3/toolchains/go/bin/go version` — exit 0; 0.009 s; raw log `go-version.log`.
- `/workspace/scratch/458de7991ac3/toolchains/go/bin/go env -json GOFLAGS GOWORK GOOS GOARCH CGO_ENABLED GOVCS` — exit 0; 0.006 s; raw log `go-environment.log`.
- `/workspace/scratch/458de7991ac3/toolchains/go/bin/go build -mod=readonly ./...` — exit 0; 9.5 s; raw log `build-all.log`.

## Identity and scope

- Normal independent repository metadata was verified; this is not a linked worktree.
- The checkout HEAD and tree equal the previously tested combined source before and after the build.
- All source/test/embed and tracked module hashes from the earlier package manifest match before and after.
- The clone remains clean. No product source, tests, module graph, budgets, exclusions, or validators changed.
- VCS stamping was not disabled. No tests were rerun.
- This is a local build qualification, not PostgreSQL, external runtime, production browser, or release acceptance.
