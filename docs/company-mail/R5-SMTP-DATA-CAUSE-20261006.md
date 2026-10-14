# DATA 最终回复错误链限定修复

2026-10-06，仅记录 R5-P6-050 的限定进展。

## 固定源码与行为

基线为 PR54 head `2987f509131b0a569094959a2b35a5b8883e9c68`、tree `22268d3af375bb31c7bcbf7886774cc2120d9f31`。被测代码／测试候选 tree 为 `0017ce652b58b9dadf4fe25c35e85c66deadcca8`；交付仅追加本报告及 TODO 记录。

[sendSMTP](../../internal/outbound/delivery.go) 原来用 `%v` 格式化非 SMTP 协议拒绝的 DATA.Close 错误，保留了 `ErrOutboundUncertain`，但丢失底层 `errors.Is`／`errors.As` 链。本次唯一生产改动将第二个格式动词改为 `%w`，同时保留不确定标记及原 EOF、timeout、ProtocolError 或取消时的 transport cause。现有 `smtpContextError` 因而能在 deadline 已到、`ctx.Err()` 尚未发布时识别原 `net.Error` 并保留 `context.DeadlineExceeded`。

MAIL／RCPT／DATA／最终回复的明确 4xx／5xx 继续保留 `textproto.Error` 且不标记 uncertain；DATA 前及写入失败分类不变。收到最终 250 后，QUIT 断开、拒绝或取消均继续返回 nil，不产生重发。adapter、API、recipient ledger、重试、TLS／DNS 策略和生产测试接口均未改。

验收字节 SHA256：

- `internal/outbound/delivery.go`：`9366e81b06bea9de561eba37ec33ed9fd442e9368f3684172599a14cd89d631f`
- `internal/outbound/r5_smtp_cause_test.go`：`d05a995718d44dc03ad2fa486af9c24f7ae5950464a41123c60ba4de04c122c2`

## 实际验证

使用官方 Go1.25.7、原锁定依赖及两个本地 replace；runtime 为 `GOTOOLCHAIN=local`、`GOPROXY=off`、`-mod=readonly`，测试启用 `-race`。作者和独立测试均使用真正的标准库 `net/smtp` 客户端，通过 **net.Pipe 内存 SMTP 协议交换** 实测；不是 fake adapter，也不是真实 TCP 拨号／listener。最终回复故障均在 peer 读到完整 DATA 点终止符后注入，fixture 有界关闭并等待 peer 结束。

- 同一最终[作者测试](../../internal/outbound/r5_smtp_cause_test.go)：基线 3 top-level PASS／3 FAIL，18 PASS／3 FAIL events；候选 6 top-level／21 events PASS，0 fail／skip。基线仅 EOF、deadline-before-Err 和取消时底层错误链断言失败。
- 相同 53 个具名已有离线控制：基线及候选各 417 events PASS，0 fail／skip；候选作者矩阵重复 50 次，300 top-level／1050 events PASS。
- 独立协议 oracle 在候选检查前冻结：基线 27 leaf PASS／6 cause-chain FAIL；候选 33／33 leaf PASS，覆盖自定义 sentinel、EOF、typed timeout、畸形 ProtocolError、deadline race 和取消原因，并保留全部前置阶段／明确拒绝／250 后 cleanup 控制。最终独审重复 10 次通过 330 leaf executions／390 events；合并已有控制、最终作者测试和 oracle 复验 66 top-level／444 leaves／477 events PASS，0 fail／skip，focused vet PASS，无阻塞发现。
- outbound package vet、whitespace 和固定基线 patch applicability PASS。没有运行完整 `./...`；计数包含重叠，不相加为全套成绩。

新增作者矩阵可复验：

```sh
go test -mod=readonly -race -count=1 -run '^TestR5SMTPCause' ./internal/outbound
go vet -mod=readonly ./internal/outbound
```

## 验收边界

9 个既有真实 listener／dial delivery-context 用例未跑，不将其写成 skip／PASS；只选择无需联网的公共 pre-canceled adapter 控制。本次没有真实 DNS、TCP dial／listener／socket、邮件／SMTP 账号／凭据、PostgreSQL 或 restart／fullcaller／CI／部署实跑。

仅限定错误链修复与内存协议验证通过；actual PG fencing／persistence、loopback SMTP／TLS／DNS 和 restart 等完整验收需另行限定实施。R5-P6-030／040／050／090 及父门禁仍 open，中央 **10/171**、历史 F52／G0 和其他 formal blockers 不变，无父项新增勾选；不宣称 merge、release 或 deployment。
