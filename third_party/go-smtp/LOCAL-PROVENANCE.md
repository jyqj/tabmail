# TabMail go-smtp v0.24.0 生命周期补丁

**状态：SOURCE-only，尚未编译、执行测试或通过独立审查。** 本目录是完整、普通、可编辑的上游 Go module 源码树，不是下载脚本、归档占位或版本升级。只增加 BDAT/DATA owner 生命周期约束；未禁用 CHUNKING、BINARYMIME、BDAT、LMTP，未调整 ACK/status 分类。

## 来源与范围

- 上游：[`emersion/go-smtp v0.24.0`](https://github.com/emersion/go-smtp/tree/v0.24.0)。版本时间来自本地 module cache `.info`：`2025-07-08T08:43:11Z`。
- 本地只读源：`/Users/jin/go/pkg/mod/github.com/emersion/go-smtp@v0.24.0`；全部 23 个文件与既存 module zip 解压内容逐字一致后复制。未修改缓存、未下载依赖。
- 原版文件 SHA-256、module zip SHA-256、Go module h1：`UPSTREAM-MANIFEST.json`。
- 原版 `LICENSE` 全文保留：MIT，版权主体为 The Go Authors、Gleez Technologies、emersion、Proton Technologies AG；原 README、全部 upstream tests、示例、命令、module pins 原样保留。
- 仅改两个原版文件：`conn.go`、`server.go`；新增 `owner_lifecycle_test.go`。可重放 diff：`LOCAL-PATCH.diff`。
- 根 `go.mod` 仅追加 `replace github.com/emersion/go-smtp v0.24.0 => ./third_party/go-smtp`；已有 enmime 精确版本 replace 和其他字节保留。根/本 fork/enmime 的 mod/sum 闭包见 `LOCAL-SOURCE-MANIFEST.json`。
- 2026-10-03 查看官方 [`master/conn.go`](https://raw.githubusercontent.com/emersion/go-smtp/master/conn.go) 与 [`master/server.go`](https://raw.githubusercontent.com/emersion/go-smtp/master/server.go)：所显示的 BDAT 动态字段访问仍与此问题一致，未据此升级或宣称上游全历史没有修复；本补丁仅以本地精确 v0.24.0 为基线。

## 最小集成契约

`Server.Shutdown(ctx)` 首次成功返回（无 context 错误）前等待所有 Serve owner、连接 handler、其完整 DATA/BDAT goroutine tail、Session.Logout。`Close` 仅中断 socket/pipe，不等同 join；保持原版 closed 单次语义，Close 后或第二次 Shutdown 返回 `ErrServerClosed`，**不是完成证明**。

TabMail 现有唯一 drain owner 可保持以下顺序；外围超时只取消合作 work context，不能把超时标作 drain 完成：

1. seal/关闭原 listener 与原始连接；
2. 等 `serveDone`；
3. 唯一 retained drain goroutine 调 `inner.Shutdown(context.Background())`；
4. 如保留 backend WG，它只是附加检查，不能替代依赖内 owner join；
5. 之后才允许 caller 的 ownerDone/资源关闭。

未新增生产 hook API；也不需要 caller 使用反射或访问依赖私有 WG。普通 SMTP DATA 在 connection owner 内；LMTP DATA 与 BDAT 的额外 goroutine 都登记至 connection `dataWG`。

## Evidence → Finding → Path

| Evidence | Finding（SOURCE 推论，非运行验收） | Path/修复 |
|---|---|---|
| E1：原版 conn.go 1039–1073；原版 hash 清单 | F1：仅在 Session.Data 内 Add 不能覆盖尚未进入 Data 的已创建 goroutine；可变 `c.dataResult` 使旧 tail 写到新事务 channel | P1：connection loop 创建 worker 前 `dataWG.Add(1)`；捕获本事务 session/result/pipe/status/recipients；唯一 terminal send、reader close 完成后 Done |
| E2：原版 reset/Close 未 join worker | F2：RSET/Logout 能与延迟 Data 或恢复 tail 并发，session 重用和资源销毁过早 | P2：Reset 先关闭 pipe，再无锁 join，再 Session.Reset；连接 loop 终止后 Close→join→Logout |
| E3：原版 Serve 在 Accept 后无 seal 锁 Add | F3：Shutdown Wait 可与未登记的已接受连接交错 | P3：Serve/连接 admission 与 done seal 共锁；Serve WG 纳入 late Accept socket disposal；Shutdown 先等 Serve，再等 connection WG |

RSET 可能等待不合作的 backend 真正返回。这是诚实保留 owner，不是死锁规避用的假取消/假 ACK。连接 `Close` 从 backend Data 内调用不会等待自身；join 时不持有 `Conn.locker`。关闭后 handler 不继续执行已缓冲的下一条命令。STARTTLS 在切换 transport/session 前 reset/join。

## 待唯一验收方执行

新测试只使用内存 pipe/自定义 `net.Conn`、channel barriers；5 秒 timer 仅作失败 watchdog，无 Sleep、真实监听端口、PG、外部 API。14 个顶层选择器、18 个展开叶用例（静态计数，不是通过数）。无 `t.Parallel`；重复事务固定 12 次，LMTP native/fallback 各 4 次。建议 `GOMAXPROCS=2`、`-p=1` 串行验收，不与其他 Go/PG 作业并行。

```sh
cd /Users/jin/Desktop/tabmail/third_party/go-smtp
GOTOOLCHAIN=go1.25.7 GOWORK=off GOMAXPROCS=2 GOPROXY=off GOSUMDB=off go test -mod=readonly -p=1 -count=1 -timeout=60s -run '^TestOwner' .
GOTOOLCHAIN=go1.25.7 GOWORK=off GOMAXPROCS=2 GOPROXY=off GOSUMDB=off go test -mod=readonly -p=1 -race -count=1 -timeout=90s -run '^TestOwner' .
```

两个命令尚未执行。新测试使用 Go 1.25+ 标准库 testing/synctest，故 nested module 需要显式指定项目工具链；本目录原版 go.mod/go.sum 字节不变。上述精确工具链必须已由唯一 runner 准备好，GOPROXY=off 禁止临时下载，缺失时应报告工具链 infra，不升级依赖或改 module pins。也可在项目根使用已批准的项目工具链直接测试 github.com/emersion/go-smtp 包。完整上游回归与 root SMTP integration 另由唯一验收方审批/执行；根 `go test ./...` 不递归 nested module，不能替代以上 fork tests。运行前独立 SOURCE reader 应重点检查 Add/Wait 时序、reset/Logout 串行性、zero/non-LAST/LAST/error/panic/LMTP、mod closure；需要真正预调用调度冻结的证据时，不把测试 backend entry gate冒称 runtime 调度器级 pre-call barrier。

新 selectors（详情见测试源码）：

- `TestOwnerBDATDelayedEntryShutdown`
- `TestOwnerBDATResetJoinsBeforeSessionReuse`
- `TestOwnerBDATCapturesPrivateResultEvenWhenNextChannelFull`（白盒隔离旧闭包 bug，不冒充 wire 输入）
- `TestOwnerBDATPanicTailIncludedInShutdown`
- `TestOwnerBDATRepeatedTransactions`
- `TestOwnerBDATBackendCloseDoesNotSelfJoin`
- `TestOwnerBDATLastErrorAndPanicStatuses`（3 subtests）
- `TestOwnerBDATEarlyErrorResetsAndPreservesStatus`
- `TestOwnerShutdownWaitsForLateAcceptDisposal`
- `TestOwnerShutdownRejectsLaterServe`
- `TestOwnerBDATTailClosesReaderBeforeJoin`
- `TestOwnerBDATLMTPRepeatedResetAndLast`（2 subtests）
- `TestOwnerBDATCloseResetRace`


## 独审 F1/F2 修订（仍未运行）

- F1：`Conn.isClosed` 由同一 locker 读取；handler 在 loop-entry 和 read 后 dispatch 前统一检查。parse-error 的 continue 不再绕过关闭门。新 `TestOwnerClosedParseErrorsDoNotDispatchBufferedCommands` 含 new/existing session 两支，一次 Read 预缓冲四个短命令和后续 EHLO/MAIL/RCPT/RSET/BDAT，检查 NewSession/Reset/业务调用未发生及原 501×4→500 响应未变。
- F2：五个并发负断言用例放入 `synctest.Test`；负断言前 `synctest.Wait` 推进同 bubble 的所有 owner 至完成或 durable-blocked。不是多次轮询、Sleep 或单纯 Data entered。channel/WG/内存 pipe 都在 bubble 内创建，无真实网络 I/O。官方依据：[Go 1.25.7 testing/synctest](https://pkg.go.dev/testing/synctest@go1.25.7)。
- DelayedEntry 的 gate 仍位于 Data 方法内部；Add-before-go 是源码证明，不将此 gate 标为 scheduler pre-call barrier。panic logger gate 在 Data panic 后，验证 recovery tail；完成后还检查结果/reader-close。
- `LOCAL-REVIEW-FIX.diff` 是上一签准版本到本次的精确增量；`LOCAL-PATCH.diff` 继续是完整上游 v0.24.0→当前 SOURCE diff。
- F3 根 SMTP 测试修订由其独占 owner 执行；本作者未改 root SMTP/main，也不修改独立 reader 报告结论。

### 未执行的 mutation negative-control 设计

只允许唯一 runner 在隔离临时副本逐项变异；不得覆盖工作区 fork，不能并发跑。每项只施加一处 mutation，原版应 PASS、mutant 应 FAIL，最后分别保存实际日志；目前均为 NOT_RUN。manifest 的 `negative_controls_source_design` 提供精确 from/to 和 selector。

| Mutation | 必须检出的目标 | 确定性依据 |
|---|---|---|
| 删除 handler cleanup 的 `c.dataWG.Wait()` | DelayedEntry/PanicTail 的 Logout 过早 | `synctest.Wait` 后 runnable cleanup 已完成，Logout channel 已关闭，不能靠未调度假绿 |
| 删除 reset 的 `c.dataWG.Wait()` | RSET 过早 Session.Reset | wait 后 Reset 的 buffered event 必须已送达；backend 仍阻塞在 release gate |
| 删除 Shutdown 的 `s.wg.Wait()` | DelayedEntry/PanicTail 的 Shutdown 过早 | wait 后 stopped 已送达，即使 handler 仍正确等待 DATA |
| 删除 Shutdown 的 `s.serveWG.Wait()` | late Accept disposal 漏 join | wait 后 Shutdown 已完成但 Accept 仍在 release gate |
| 将 `isClosed` 返回值变为 false | parse error 后 buffered command 仍分发 | 同步一次 Read 回归直接检查 NewSession/Reset 调用数，不依赖调度 |
