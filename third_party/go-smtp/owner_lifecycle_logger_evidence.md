# Owner tail logger regression evidence

Source: `66eb5e819170d3508edb07db8ab1e0014555ff8c`.

The DATA panic recovery and Serve error reporting can both call the same test
logger. The original fixture closed `entered` on every call, so the second
call panicked at `Server.Serve.func1` (`server.go:168`). The fixture now closes
only its notification through `sync.Once`; every call still waits on `release`.
Production SMTP code and the original panic-tail Shutdown/Logout assertions
are unchanged.

The new fixture regression checks the first blocked call, eight further
concurrent calls before release, completion of all nine after release, and
repeated calls after release. Scheduling uses `synctest.Wait`, without sleeps.

## Validation

Go 1.25.7, `GODEBUG=asynctimerchan=0`, `GOTOOLCHAIN=local`.
All commands run from `third_party/go-smtp`, using workspace-owned caches:

```sh
export GODEBUG=asynctimerchan=0 GOTOOLCHAIN=local
export GOMODCACHE=/workspace/tabmail-cloud/gomod
export GOCACHE=/workspace/tabmail-cloud/gocache
GO=/workspace/tabmail-cloud/tools/go/bin/go
# Before changing the fixture: exit 1, original output preserved below.
"$GO" test -race -count=1 -timeout=180s -v -run '^TestOwner' .
# After the fix: PASS; 10 repetitions registered before execution.
"$GO" test -race -count=10 -timeout=180s -v -run '^(TestOwnerTailLoggerRepeatedConcurrentCallsWaitForRelease|TestOwnerBDATPanicTailIncludedInShutdown)$' .
# After the fix: PASS, all 16 top-level owner tests and their subtests.
"$GO" test -race -count=1 -timeout=180s -v -run '^TestOwner' .
```

Only in-memory fixture transports were used. No database or existing mail/service
connection was used. The 180-second gate, two fork replaces, schema 19, lockfiles,
central TODO/CI, and existing test targets were not changed. Whole-PG, application
combined targets, and other known failure targets were not run; their prior
failures remain unresolved by this fixture-only change.

## Original unedited red-run output

```text
=== RUN   TestOwnerBDATDelayedEntryShutdown
--- PASS: TestOwnerBDATDelayedEntryShutdown (0.00s)
=== RUN   TestOwnerBDATResetJoinsBeforeSessionReuse
--- PASS: TestOwnerBDATResetJoinsBeforeSessionReuse (0.00s)
=== RUN   TestOwnerBDATCapturesPrivateResultEvenWhenNextChannelFull
--- PASS: TestOwnerBDATCapturesPrivateResultEvenWhenNextChannelFull (0.00s)
=== RUN   TestOwnerBDATPanicTailIncludedInShutdown
panic: close of closed channel

goroutine 23 [running (durable), synctest bubble 3]:
github.com/emersion/go-smtp.(*ownerTailLogger).Printf(0xc0000901b0, {0xc0000c8000?, 0x4bddb2?}, {0xc00018f7a8?, 0x449d6f?, 0xc00018f790?})
	/workspace/tabmail/third_party/go-smtp/owner_lifecycle_test.go:284 +0x36
github.com/emersion/go-smtp.(*Server).Serve.func1()
	/workspace/tabmail/third_party/go-smtp/server.go:168 +0x24d
created by github.com/emersion/go-smtp.(*Server).Serve in goroutine 22
	/workspace/tabmail/third_party/go-smtp/server.go:163 +0x65d
FAIL	github.com/emersion/go-smtp	0.014s
FAIL
```
