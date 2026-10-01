# R5 共享测试 fixture 与故障端口（AD / P0-090 准备）

**P0-070 尚未验收，P0-090 不完成。** 本批是有限可执行第一段，不把共享helper或本地端口测试等同090完整验收；性能S/M/L属于后续P0-100，未实现。

## 第一段实际入口

| 文件/入口 | 真值来源 | 当前范围/未声称 |
|---|---|---|
| `testpg.NewR5Fixture` | 复用owned DSN的`NewPostgres`隔离DB；实际PgStore公司配置、邀请/激活、邮箱/grant与guarded冻结 | 2tenant，每tenant admin+reader/organizer/sender/frozen员工，各自personal和shared原ID；合法read+organize约束；不证明JWT/Key/HTTP已覆盖 |
| `R5DatabaseNow` / `R5WaitBlockedBy` | 真PG `clock_timestamp()` 与精确backend PID wait edge | 只有数据库条件真才ready；context deadline/cancel为错误；poll ticker仅节流，不以sleep推断就绪；不改生产Clock |
| `testutil.R5ObjectFault` | 包装真实ObjectStore端口，默认fixture用既有memory store | single-shot Put/Get/Delete/Exists失败、实际stream短读、取消；记录operation/keySHA不记录raw key或payload；不声称DB rollback已测 |
| `StartR5SMTP` | 真 `net.Listen("127.0.0.1:0")`、SMTP字节协议、正式`outbound.DeliverRelay` | MAIL/RCPT/DATA/final/QUIT阶段真实回复；DATA回复丢失为uncertain，250后QUIT异常不重发；不以fake DeliveryAdapter返回预期，不声称worker/recipient ledger全链已测 |

所有新R5集成入口显式缺`TABMAIL_TEST_DB_DSN`直接失败；没有修改旧`NewPostgres`本地skip约定。PG测试使用独立 `r5fixtures` tag，默认backend不因准备批新增阻塞或假skip绿；operator必须显式跑tagged suite及收原始JSONL/exit，不以tag未执行声称通过。DB不在helper中启动；测试库由既有隔离工厂创建/清理，只影响本次随机数据库。

SMTP需要hard context deadline，返回Ready channel确认listener实际已绑定；Close取消所有accepted sockets并join接收/连接goroutine，Closed channel确认终态。DATA仅保留SHA与byte count，上限1MiB。没有复制正文/凭据到报告；script配置没有外部host/端口，只有loopback。

## 已实际执行与待执行

- 当前源码tagged PG factory/barrier **编译通过**（未跑DB，不是PG实证）。
- 新对象/SMTP端口测试 `-race -count=1` 实际通过：4个top-level tests及SMTP6个真实协议变体；无需DB。
- PG独立tenant/mailbox角色以及精确阻塞边/DB Clock测试须由operator同owned DSN fresh执行。fixture失败、超时、panic、非法grant输入都不是政策目标红。

```sh
# 无DB的真实ObjectStore/loopback SMTP端口回归
 go test -mod=readonly -json -race -count=1 -timeout=30s ./internal/testutil -run '^TestR5Object|^TestR5SMTP'
# operator只在child env提供私有owned DSN，不输出值、不起第二PG
 go test -mod=readonly -json -race -count=1 -tags=r5fixtures -timeout=180s ./internal/testpg -run '^TestR5Fixture'
```

## 完整090仍需逐段推进

机器范围：[R5-FIXTURE-CONTRACTS.json](evidence/R5-FIXTURE-CONTRACTS.json)。第一段之后不能遗漏：

1. 真正式router/JWT/Key（撤销、过期、缺scope、冻结、foreign tenant）的复用fixture与实际HTTP资格/码；不能仅使用trusted内部Actor就叫认证通过。
2. quota UTC窗口、profile/mailbox权限变化与formal reserve/draft/submit，expiry按合法个人永久/shared有限语义，SQL播种后实际读回；不构造绕CHECK的grant。
3. 封存draft与offboarding receipt、sent asset/job/recipient混合账本的可信来源、index raw/hash/parser version/旧lease；不仅在内存填写预期状态。
4. 对象failure/short read进入真实HTTP/store事务，验证audit/outbox/对象/配额失败回滚；当前端口unit不替代它。
5. 实际worker经本loopbackSMTP推进receipt/recipient账本，覆盖RCPT暂/永拒、DATA之前错误、最终答复丢失、250后QUIT断开与旧token/取消；当前Relay端口测试不是worker证明。
6. shared fault/barrier的失败清理证据及全正常回归，所有资源属于本次fixture；租户、持久对象与后台goroutine不能泄漏。必要driver/mandatory/Make/CI由integration operator串行接线。

P0-100必须之后建立完整seed/规模/消息大小/shared授权分布S/M/L，记录硬件/SQL次数/延迟/内存，S/M实际至少一轮、L明确预算/发布必要性；现parser microbenchmark或数据计数不替代该性能基线。
