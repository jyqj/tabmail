# Source runner preparation and executed binary evidence

Base source: `4aec142bcffd53c6ab91f2263436aaf980833fab`.
Product source remains `aeed47f0e1998d9925acb680992b051463821d69`.
The compatibility map, wire registry, source guards, locks, two replacements,
historical artifacts and PostgreSQL 180-second CI gate are not changed.
Whole PG180 was not run locally. Typed wire evidence proves projections and
shipping envelopes only; it does not prove HTTP admission or PG acceptance.

## Initial run, retained without replacement

`initial/` contains the original real results from source
`753e8f94897b92008274d42861f26bcebe3c98b8`: 664/664 tests actually started,
no missing/overlap, 0 failures/skips, two existing compatibility/wire ERRORs;
frozen v1 at `41b015c30c66b3ba58a3c1395e8559ebcd27a65f` passed 4/4.
Scoped tests passed 58/58; the Go fixture test exported 21 complete synthetic
route fixtures. The formal runner returned exit 1 / FAIL.

This initial receipt's `build_sha256` pins a separately compiled binary while
its `run_argv` ran another `go test` command. It does **not** identify that pinned
binary as actually executed. Retaining the initial receipt does not promote
this limited evidence to the corrected execution claim.

## Execution identity correction

The revised preparer compiles once, hashes and holds an open FD on that binary,
then uses official `go tool test2json` to execute `/proc/<parent-pid>/fd/<fd>`.
It uses the handler package cwd and fixed `-test.v=test2json`, exact test regexp,
and `-test.count=1`. The open parent FD names the original inode even if its
pathname is replaced. Source, binary bytes and pathname inode are checked before
and after execution; stale output paths are rejected before dispatch.
The receipt records actual executed argv/cwd and `executed_binary_sha256`, equal
to `build_sha256`. Independent regressions reject binary replacement, wrong
source, old fixture, midflight replacement and post-run binary tampering.

Each run's source identity is recorded in its original report. Evidence-only
commits after a run are delivery commits, not claims that those commits were
the runtime source. Final results and preservation comparison are appended in
`final/` after the corrected source run finishes.

## Safe review payload

Each directory's `payload-sha256.json` hashes only its delivered files.
`initial/original-local-payload-sha256.json` retains the original local manifest;
paths for intentionally omitted binaries are hash references only.
The four full selected-source metadata observations are stored as deterministic
gzip files with decompressed byte hashes in `compressed-raw-sha256.json`.
All copied results, logs, receipts and synthetic wire bytes are preserved
verbatim. No binary, npm package tree, source clone, fixture secret or private
credential is included. Absolute paths and proc FD names in raw receipts are
historical execution references, not live capabilities or shipped files.
