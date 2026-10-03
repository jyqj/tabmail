# go-smtp BDAT 生命周期 fork 独立 SOURCE 审查

- 日期：2026-10-03；审查角色：独立于实现作者。
- 范围：`third_party/go-smtp`、根 module replace，以及必要的 SMTP wrapper/test 集成契约。
- 结论：**SOURCE_REVIEW_CHANGES_REQUIRED**。BDAT 完整 owner 的核心修复成立；下列源码关闭分支、回归证据和根集成测试问题解决前，不批准完整 SOURCE 验收。
- 本轮未执行 Go/build/test/list、SMTP socket、PG、TLS 或其他运行验收；没有修改产品源码，也没有做全树 hash。工作区本来就包含大量其他 dirty 变更，HEAD 为 `587ba75`。

## 1. 来源与基线

独立读取本地 module cache 和既存 zip，做字节比较而非重算全树 hash：zip 的 23 个文件名与 UPSTREAM-MANIFEST 相同，缓存 23 文件与 zip 全部逐字一致；fork 仅两个原文件不同（conn.go/server.go），其余 21 文件相同。普通可编辑文件树、MIT LICENSE 全文均存在。LOCAL-PATCH.diff 与两处修改和新增 owner_lifecycle_test.go 的精确 unified diff 一致。

仅定点核对四个派发标识，均匹配：

| 文件 | SHA-256 |
|---|---|
| LOCAL-SOURCE-MANIFEST.json | `5f6bfbe8cb9ff6a6807d6c39588bd7be991aad7b3a68ab1cf7beea8b7e81988b` |
| conn.go | `8ad62e3b8e253ccac422cbfbcb48fca978d234629a7cfae5af35997c3e4e767c` |
| server.go | `bf29aa2178e756fb39d892f1c90a3007ac4bba4484721353cb43ba785d4a01c0` |
| owner_lifecycle_test.go | `e35e6ac2a451b4e493fa3971f9231d972a07b936c4a5c3b8c4b286998b82fe56` |

根 go.mod 去掉唯一追加的精确 `go-smtp v0.24.0` local replace 后，恰好恢复作者记录的 before hash `a2e87e7f71509440e83df143c1875bb778ac1d2aabaa3fc66f109dcb2f32acc4`；旧 enmime replace 保留。根 go.sum 与 HEAD 字节一致。没有把清单的其他 hash 宣称为本轮重新验证结果。

## 2. 已成立的 SOURCE owner 证明

- `conn.go:1048–1090`：在创建 goroutine 之前 Add；固定捕获 session、result、reader、status、recipients。单一 terminal send 写入该事务私有容量 1 channel，reader CloseWithError 之后才由外层 defer Done；不再把旧 tail 写到后来 c.dataResult。
- `conn.go:1353–1377`：reset 先关 writer、释放 locker、join，再清事务和 Session.Reset。连接命令 loop 串行，因此不会由同一 owner 在 Wait 期间启动下一事务；公共 Close 不再清 session/pipe 字段，避免旧并发字段访问。
- `server.go:174–185`：handler 结束后 Close → dataWG.Wait → Logout → 从连接表删除，外层 s.wg.Done 更晚。普通 SMTP DATA 在 handler owner 中；LMTP DATA 的额外 goroutine 在 conn.go:1242–1262 登记，reset 包含其完整尾部。
- `server.go:108–170,326–349`：Serve/连接 Add 和 shutdown seal 同锁；先 serveWG.Wait 再连接 WG，包含 seal 后 late Accept socket disposal。未发现本路径的零计数 Add/Wait 竞态。
- Backend 从 Data 内调用 Conn.Close 不 self-join。等待 backend 真正退出不会持有 Conn.locker。不合作 backend 会保留 owner；这不是“取消已完成”。
- CHUNKING/BDAT/BINARYMIME/LMTP 未被禁用。dataErrorToStatus 和 LAST/非 LAST 的正常分类未改写。STARTTLS 在替换 session/transport 前执行 reset/join；实际 TLS 回归尚未执行。

以上不代表运行 PASS，也不代表 main 全 owner 已被证明。

## 3. Evidence → Finding → Path

### F1：关闭后的 parse-error 分支仍可执行已缓冲的下一命令（源码 blocker）

**Evidence：** `server.go:203–217` 的 closed 检查只在 `c.handle` 后；`parseCmd` 错误分支调用 protocolError 后直接 continue。`conn.go:223–226` 的第四个协议错误会 Close；`parse.go:27–28` 将短命令归入该分支。

**SOURCE 反例：** 一次 transport Read 预缓冲以下字节：

```text
x\r\nx\r\nx\r\nx\r\nEHLO example.org\r\n
```

第四个 x 关闭连接，但 continue 可从 bufio 中继续取得 EHLO；下一轮在 closed 检查之前调用 NewSession。已有 session 时也可能执行 Reset。已有上游 `TestServer_tooManyInvalidCommands` 使用 XXXX（进入 c.handle 分支），且逐条写入，不覆盖此路径。

**Path：** 在每轮 dispatch 前/循环入口统一检查 closed，并覆盖 parse-error 的 continue 分支；加一次性预缓冲输入回归，确认关闭后不调用 NewSession/Reset/业务命令。不能只验证 transport.Close 已调用。

### F2：若干 owner 负断言有调度型 false-green（证据 blocker）

**Evidence：** `owner_lifecycle_test.go:107–113,183–192,222–224,302–310,456–461` 使用立即 default/ownerAbsent 检查；serveDone 仅证明 accept loop 返回，不证明 connection cleanup 已到 join，listener.closed 也不证明 Shutdown waiter 已执行等待。

**可行假绿顺序：** 假设删掉 dataWG.Wait，handler cleanup 尚未被调度；测试先收到 serveDone，立即检查 Logout/stopped 尚未发生，然后释放 Data/tail barrier；之后 handler 才运行，也会得到绿色结果。RSET case 的 readReset 只证明 pipe close 已发生，不能证明 Reset/join 路径已充分推进。late Accept case 同样可能仅因 Shutdown goroutine 尚未继续而通过 default。

**Path：** 增加目标 owner 的确定性阻塞/完成判据，以及独立负控（临时移除对应 join/admission 时必须失败）；可以通过可审查的测试专用 instrumentation 或受控调度设施建立目标 waiter 已运行的证明。禁止用 sleep、更多重复次数、仅 Data entered 替代。现有 panic logger gate 确实位于 Data 返回后的 recovery tail，是有价值的目标 barrier，但其 shutdown 负断言仍需补强。

`TestOwnerBDATCapturesPrivateResultEvenWhenNextChannelFull` 和 `TestOwnerBDATTailClosesReaderBeforeJoin` 对 fixed capture、终态结果和 reader close 有直接正断言；它们不能替代所有 server join 的阻塞证明。DelayedEntry 的 gate 已在 Session.Data 内，不是真正的 scheduler pre-call barrier；LOCAL-PROVENANCE 已诚实说明此边界。

### F3：根 SMTP 旧回归等待顺序与新 fork 冲突（集成 blocker）

**Evidence：** `internal/smtp/r5_shutdown_lifecycle_test.go:276–307` 中 `BDAT_data_outliving_connection_is_joined` 于 :296 等待 sess.logout，直到 :301 才关闭 sess.release。新 fork 必须先等待 Data/tail 退出，才允许 Logout。

**Finding：** 该测试在新依赖下存在静态确定的循环等待，预期卡 watchdog；此为 SOURCE 推论，不是实际运行失败记录。

**Path：** 由根 SMTP 写锁 owner 改成“释放前 Logout/drained 均不得完成；释放后完整 Shutdown 和 Logout 必须完成”，并补 F2 所述确定性 owner 证据。更新 wrapper :306–310、:377–378 的旧上游描述，明确 backend WG 只覆盖业务 work，fork 才覆盖依赖 goroutine tail。

## 4. 集成契约与不能扩大之结论

外层仍应唯一 retained drain owner：seal 原 listener → 关闭原 connections → serveDone → **首次且唯一** inner.Shutdown(context.Background()) → 业务 WG 附加检查 → drained/资源释放。外层 ctx deadline 仅返回 incomplete 并取消合作 work，重复 Shutdown 必须等待同一个 owner。不得先调用 inner.Close，也不得用第二次 inner.Shutdown 的 ErrServerClosed 当成 join 证明。

本轮看到 `internal/smtp/server.go:274–361` 已有上述 retained-owner 结构；只确认依赖契约的相容性，不重审其其他锁/资源，也不授予 wrapper/main 整体 PASS。fork Shutdown 的 ctx 超时返回会保留内部 waiter，但不能靠再次调用该 API 获得成功 join；这正是外层必须用 Background 的原因。

## 5. 唯一验收方候选命令和资源

以下全部未执行。先解决 F1/F2/F3、更新 patch/provenance/source manifest，再由唯一验收方串行执行；其他人不得并发启动 Go/PG 作业。

### 5.1 fork 纯内存 owner tests

```sh
cd /Users/jin/Desktop/tabmail/third_party/go-smtp
GOWORK=off GOMAXPROCS=2 GOPROXY=off GOSUMDB=off go test -mod=readonly -p=1 -count=1 -timeout=60s -run '^TestOwner' .
GOWORK=off GOMAXPROCS=2 GOPROXY=off GOSUMDB=off go test -mod=readonly -p=1 -race -count=1 -timeout=90s -run '^TestOwner' .
```

现状 13 top-level/16 展开叶是静态 inventory，不是通过数。仅内存 pipe、自定义 Conn/Listener，无真实端口/PG，watchdog 5 秒；不并行测试，重复事务 12 次、LMTP 每支 4 次。修改后重新报告静态数量，不沿用旧数字。

### 5.2 root MVS 和真实依赖路径

```sh
cd /Users/jin/Desktop/tabmail
GOWORK=off GOPROXY=off GOSUMDB=off go list -mod=readonly -m -json github.com/emersion/go-smtp
GOWORK=off GOMAXPROCS=2 GOPROXY=off GOSUMDB=off go test -mod=readonly -p=1 -count=1 -timeout=60s -run '^TestOwner' github.com/emersion/go-smtp
GOWORK=off GOMAXPROCS=2 GOPROXY=off GOSUMDB=off go test -mod=readonly -p=1 -race -count=1 -timeout=90s -run '^TestR5SMTPShutdownLifecycle$' ./internal/smtp
```

要求 module version 为 v0.24.0、Replace.Dir 指向本项目 third_party/go-smtp。根 `./...` 不包含 nested module 自身测试，不能替代直接 module/package 选择。

### 5.3 上游兼容回归（另需批准真实 loopback socket）

```sh
cd /Users/jin/Desktop/tabmail/third_party/go-smtp
GOWORK=off GOMAXPROCS=2 GOPROXY=off GOSUMDB=off go test -mod=readonly -p=1 -race -count=1 -timeout=120s -run '^Test(Server|Client|TLS|LMTP|Basic|NewClient|Hello|Auth)' .
```

该选择覆盖原 server/DATA/CHUNKING/LMTP/client/TLS 测试；原 server helper 使用 `127.0.0.1:0`，client TLS helper 可 fallback `[::1]:0`。需要本地临时端口，仍不需要 PG。完整 module 回归可由唯一验收方另跑 `go test ... ./...`；不得把纯内存 owner 选择器说成上游全量已覆盖。

正式 loopback TLS+BDAT→PG、ACK-lost、main signal→最后资源关闭等仍未证明。源码变化后刷新 Code Index；本报告不得替代这些验收。
