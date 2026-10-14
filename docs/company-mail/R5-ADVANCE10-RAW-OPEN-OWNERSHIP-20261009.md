# ADVANCE-10 / #206: own partial raw-object acquisitions

Parent scopes: R5-P6-100 and R5-P8-060; the raw-object lifecycle remains subordinate to its original reference and acceptance gates.

`rawobject.Store.Put` verifies an existing blob before deduplicating or repairing it. Its acquisition boundary previously returned immediately when `BlobStore.Get` returned an error, leaking any reader returned alongside that error. That early exit also omitted cleanup and cancellation causes. Other owned object-reading boundaries already handle this partial-resource contract.

The integrity verifier now registers the returned reader's existing exactly-once close/join path before handling an opening error. It never reads that failed acquisition, never deduplicates it as successful, and never treats uncertain content as permission to repair. Opening, closing and cancellation errors remain identifiable together. A nil reader also preserves the original opening/cancellation causes. Successful verification, corruption repair, bounded reading and metadata reference behavior stay unchanged.

## Fixed validation

Baseline execution used `78385f7` plus the completed new test file before the production edit; `integrity.go` there is identical to initial public baseline `fdea2178759ce9842844926374ea98517bc4188b`.

| Run | Actual result |
| --- | --- |
| Baseline plus fixed tests | 4 PASS / 10 FAIL / 0 SKIP |
| Production fix plus identical test bytes | 14 PASS / 0 FAIL / 0 SKIP |
| Five complete relevant packages at clean implementation `122c8c7cc6244ab6c8bf06a4d3a4399dcfd7c01b`, tree `c1528bedd67913d99048e1250eb94f5bad76be2e` | 642 race leaf PASS / 0 FAIL / 0 SKIP |
| `go vet` for those five packages at the same clean implementation | exit 0 |

The five package totals are rawobject 76, mailcontent 189, companymail 203, fileobj 62 and s3obj 112. The fixed 14 cases are part of that 642 and must not be added again. They cover matching and corrupted stored originals, typed acquisition errors, close failure, cancellation during opening/cleanup, missing-reader controls, successful dedup/repair, and a real blocked Close whose completion must be joined before returning.

Commands used Go 1.25.7, `GOTOOLCHAIN=local GOPROXY=off GOFLAGS=-mod=readonly GOMAXPROCS=2`, existing caches, and `go test -race -p 1 ... -count=1 -json`. Focused runs selected `./internal/rawobject -run '^TestR5AdvanceRawOpen'`; the complete run selected `./internal/rawobject ./internal/mailcontent ./internal/app/companymail ./internal/store/fileobj ./internal/store/s3obj`. Focused tests were explicit uncommitted overlays on the stated base; the complete packages and vet ran on the clean implementation commit before this report.

| Byte identity | SHA-256 |
| --- | --- |
| `internal/rawobject/r5_open_failure_ownership_test.go` | `d58d52a6adbbc6610bdf665ba616914659cff17d5147f91693b41a6cce8bd09d` |
| `internal/rawobject/integrity.go` after fix | `ad8c1268a5871b8e182b7b651522997aaab79c6a639e66f70be9fe737fda0f70` |
| Local raw baseline log, 19,551 bytes | `37628c06ab6da23bbe6afb7960f035d666bc23e6a8374626465cb0f0efb28375` |
| Local raw candidate log, 13,627 bytes | `cca4fae7618a00e870a1707affebba897c6f72b88ae7373a25704ba7f01d673b` |
| Local raw complete-package log, 628,745 bytes | `21e7ec38d7bae3944cbf13b78e19bb631b2f75ab74347f05484aed20b693d815` |

The partial acquisition is injected at the public BlobStore boundary. It does not claim a specific deployed S3 outage or PostgreSQL acceptance transaction. An arbitrary adapter whose Close permanently blocks remains outside a forcibly cancellable protocol; this implementation retains ownership until that Close returns. Independent review, integration/CI, actual PR merge and Issue closure are recorded subsequently by the batch owner. Original parent checkboxes and release gates remain unchanged.
