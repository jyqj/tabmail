# DATA 最终回复分类：只有 4xx／5xx 能证明拒绝

2026-10-07。基线为 PR #55 head `cbc8c17ebd3599d5c92711d28088b0bf4edc3f8f`。本次完成 R5-P6-050 的限定产品修复，并实跑 R5-P6-040 的既有本地网络控制；两个父任务仍 open。

## 问题与修复

[sendSMTP](../../internal/outbound/delivery.go) 原来把 DATA writer 的 `Close()` 返回的任意 `*textproto.Error` 当成明确拒绝。但 Go 1.25.7 标准库的 `net/smtp.dataCloser.Close` 固定调用 `ReadResponse(250)`；`net/textproto` 的这种错误类型表示回复码不匹配，范围也包括 `251`、`354` 和 `999`，并不只表示负回复。官方源码见 [net/smtp](https://go.dev/src/net/smtp/smtp.go) 和 [net/textproto](https://go.dev/src/net/textproto/reader.go)；本轮另核对了实际安装的 Go 1.25.7 源码。

基线的实际内存 SMTP 协议交换证明：这些异常终态被传给 recipient loop 后归为 `temporary`；直接投递也会继续第二台 MX，甚至把第二台的成功作为整次成功返回。第一台已经读完 DATA 点终止符，不能由这种错误类型推导出第一台未接收邮件。

生产改动将明确拒绝条件收紧为 `errors.As(err, &reply) && reply.Code >= 400 && reply.Code < 600`。其余 DATA 最终错误仍用两个 `%w` 保留 `ErrOutboundUncertain` 和原始 cause。没有新增全局测试钩子或改写 SMTP 客户端。

| DATA 最终结果 | 修复后结果 |
| --- | --- |
| 正常 `250` | 成功；后续 QUIT 断开、拒绝或取消不触发重发 |
| `4xx` | 保留原始 SMTP reply；进入既有临时拒绝处理 |
| `5xx` | 保留原始 SMTP reply；进入既有永久拒绝处理 |
| 其他由标准库报告为不匹配的数字回复 | 标为 `uncertain`，保留实际 reply code／message；停止 MX 回退，recipient 保持人工核对状态 |
| 畸形回复、EOF、timeout、取消 | 保持原有 `uncertain` 与完整 transport／context 错误链 |

[RFC 5321 §4.2.1](https://www.rfc-editor.org/rfc/rfc5321.html#section-4.2.1) 将 4yz／5yz 定义为负完成回复；[§4.2.5](https://www.rfc-editor.org/rfc/rfc5321.html#section-4.2.5) 说明 DATA 终态 2yz 意味着服务器接管投递责任，[§4.3.2](https://www.rfc-editor.org/rfc/rfc5321.html#section-4.3.2) 要求处理陌生码时解释首位数字。因此，非 250 的 2xx 绝不能据此重发。本切片保留标准库只认可 250 的成功边界，对这些回复采用保守的人工核对策略；不将它们描述为 RFC 意义上的拒绝，也不扩张成功判定。

## 可复验回归

新增 [r5_smtp_reply_classification_test.go](../../internal/outbound/r5_smtp_reply_classification_test.go) 复用已有有界 `net.Pipe` SMTP fixture，经过真实 `net/smtp` 解析后再调用生产 MX 编排与 recipient loop。最终回复均在 peer 读完 DATA 点终止符后注入；fixture 关闭并等待协议 goroutine 退出。

- **38 个新增 leaf cases**：25 个终态回复分类（含边界码、正常 250 和畸形回复）、5 个 MX 停止回退控制、8 个实际协议结果到 recipient 分类的控制。未知结果还检查 job hold、人工 requeue 拒绝和下一轮 worker 不重发。recipient 存储为已有 FakeStore，不能作为 PostgreSQL 持久化证据。
- **冻结的相同测试在未改生产源码基线上**：18 leaf PASS、20 leaf FAIL；3 个父测试均 FAIL，合计 18 PASS／23 FAIL events。失败精确对应 14 个异常数字终态、3 个第二 MX 回退和 3 个错误进入 temporary 的 recipient 结果。EOF／畸形回复、250、4xx／5xx 控制已在基线通过。测试开发阶段先纠正了一处用已清除的 delivery token 直接再次 finalization 的 fixture 假设；上述数字来自纠正后、生产源码仍为基线的完整重跑。
- **修复后新矩阵＋既有 SMTP cause＋delivery-context**：19 top-level／104 events PASS，0 fail／skip。其中新矩阵 3／41、cause 6／21、context 10／42。已有 9 个真实本地 TCP listener／dial 测试全部运行，覆盖 implicit TLS、STARTTLS、可信 TLS 成功／取消、各 SMTP 阶段取消、deadline、socket 所有权与释放，以及确认 250 后 QUIT 取消；另有一个 pre-canceled adapter 控制。
- **完整 outbound 包**：71 top-level／520 events PASS，0 fail／skip，启用 race detector；`go vet` PASS。此项包含上面的选定测试，计数不相加。

执行使用官方 Go 1.25.7、原 `go.mod`／`go.sum`、原两个本地 replace；测试时 `GOTOOLCHAIN=local`、`GOPROXY=off`。复验命令：

```sh
go test -mod=readonly -race -count=1 -timeout=90s -json \
  -run '^TestR5SMTPFinalReply' ./internal/outbound
go test -mod=readonly -race -count=1 -timeout=90s -json \
  -run '^(TestR5SMTPFinalReply|TestR5SMTPCause|TestR5DeliveryContext)' ./internal/outbound
go test -mod=readonly -race -count=1 -timeout=90s -json ./internal/outbound
go vet -mod=readonly ./internal/outbound
```

复现基线失败时，将同一新增测试文件放到上述固定基线，保持 `delivery.go` 不变后执行第一条命令。

验收文件 SHA-256：

- `internal/outbound/delivery.go`：`9f6d583deb4e8573787c375cab6f1a138172c5abe028f0aca090d49141580e88`
- `internal/outbound/r5_smtp_reply_classification_test.go`：`f1d362a4d3f1c3cb3255d9e234d027cfc65b92a589afb2f01e50d56f084a8c66`

## 边界

本轮没有真实外部邮件、SMTP 账号、外部 DNS、PostgreSQL、restart 或完整 `./...` 实跑。SMTP 的新错误分类与已有 loopback 连接行为验证通过，不能替代 actual PG fencing／persistence 和完整发布门禁。中央 TODO 由整合者追加；R5-P6-030／040／050／090 及父门禁保持 open，不新增父项勾选。本报告不宣称 CI、merge、release 或 deployment 已完成。
