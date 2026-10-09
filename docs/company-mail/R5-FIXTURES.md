# R5 共享测试 fixture 与故障端口（AD / P0-090 准备）

**P0-070 尚未验收，P0-090 不完成。** 当前设施已有AG统一真实验证，不把通过设施直接当依赖未验的090任务完成；性能S/M/L属于后续P0-100，未实现。

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

- 历史第一段仅编译；后续 AG **9516c2a5a152d66a1bf057bf1e64934588419fae / 774文件** fresh统一tagged PG实际18pass/0fail/0skip，12必跑top全部成立。
- 新对象/SMTP端口测试 `-race -count=1` 实际通过：4个top-level tests及SMTP6个真实协议变体；无需DB。
- PG/HTTP/Key/配额/封存/index/真实worker/object/failed-child cleanup现已同AG source实际执行；缺DSN实际exit1/0skip，虚无测试Go0但证据controller1拒绝12缺失+no tests。fixture失败、超时、panic、非法grant输入仍不当政策目标红。

```sh
# 无DB的真实ObjectStore/loopback SMTP端口回归
 go test -mod=readonly -json -race -count=1 -timeout=30s ./internal/testutil -run '^TestR5Object|^TestR5SMTP'
# operator只在child env提供私有owned DSN，不输出值、不起第二PG
 go test -mod=readonly -json -race -count=1 -tags=r5fixtures -timeout=180s ./internal/testpg -run '^TestR5Fixture'
```

## 原090条款与已实际统一验证的范围

机器范围：[R5-FIXTURE-CONTRACTS.json](evidence/R5-FIXTURE-CONTRACTS.json)。下列六类要求均由新共享fixture与AG原日志支持；这不是提前验收后续产品政策：

1. 真正式router/JWT/Key（撤销、过期、缺scope、冻结、foreign tenant）的复用fixture与实际HTTP资格/码；不能仅使用trusted内部Actor就叫认证通过。
2. quota UTC窗口、profile/mailbox权限变化与formal reserve/draft/submit，expiry按合法个人永久/shared有限语义，SQL播种后实际读回；不构造绕CHECK的grant。
3. 封存draft与offboarding receipt、sent asset/job/recipient混合账本的可信来源、index raw/hash/parser version/旧lease；不仅在内存填写预期状态。
4. 对象failure/short read进入真实HTTP/store事务，验证audit/outbox/对象/配额失败回滚；现新HTTPObject两测试已补真实Put/finish/short-read验证，端口unit不单独替代它。
5. 实际worker经本loopbackSMTP推进receipt/recipient账本，覆盖RCPT暂/永拒、DATA之前错误、最终答复丢失、250后QUIT断开与旧token/取消；另有Worker6 SMTP场景与MixedLedger实证，Relay端口unit不是单独worker证明。
6. shared fault/barrier的失败清理证据及全正常回归，所有资源属于本次fixture；租户、持久对象与后台goroutine不能泄漏。必要driver/mandatory/Make/CI由integration operator串行接线。

P0-100必须之后建立完整seed/规模/消息大小/shared授权分布S/M/L，记录硬件/SQL次数/延迟/内存，S/M实际至少一轮、L明确预算/发布必要性；现parser microbenchmark或数据计数不替代该性能基线。

## AG统一运行与依赖裁决（2026-10-02）

所有14个Go设施/测试源码SHA均与AG原`final-source.json`相同，覆盖本文件描述的原090设施。原full **23步骤控制器exit0**、default **1451pass/0fail/1原shipping-browser skip，244必跑**；独立`r5fixtures` **18pass events/0fail/0skip，12必跑top**。事件数量含6个worker子用例，不当独立漏洞数。12个top名字、safe原始artifact SHA与14源码SHA见机器JSON。

| 原要求 | 真入口与实际证明 |
|---|---|
| 多租户/角色/personal与shared/失效Key | 实际公司配置/邀请激活/guarded冻结/grants；JWT current-session+foreign/frozen；M15实际catalog，正式Key create/delete/expiry合法非空scope，X-API-Key不假Bearer |
| 封存草稿/混合投递状态 | 实际preview/execute seal，payload与custody/receipt once；mixed accepted/pending由真实SMTP与durable checkpoint建立，worker只补pending |
| 审计/对象/提交故障 | owned审计触发器真实PG error，五effects/draft不变，normal201→200一次及quota429；真正multipart Put/finish失败按既有语义保reservation/对象，短Get500无下载头/内容并metadata不变后正常200 |
| 可复用fixture/同步屏障 | NewR5Fixture/HTTPFixture、真clock_timestamp、确切pg_blocking_pids与hard deadline；不以sleep推测ready、没有生产Clock或测试后门 |
| loopback SMTP行为脚本 | actual net.Listen/Ready，正式DeliverRelay/worker经历accepted、RCPT暂/永拒、DATA拒、final reply丢失、250后QUIT丢；stale recipient/index lease不写 |
| 失败清理仅owned资源 | 故意失败child在cleanup callbacks之后、进程尚存活检查Closed/socket，parent再验exactDB已drop和另sentinel tenant未变；子exit1只为cleanup输入，不是产品红 |

缺DSN新入口actual fail且0skip；不匹配regex的Go进程虽exit0，manifest evidence gateactual exit1：no runtime tests+12mandatory missing，不能伪绿。默认backend及真实tag均已正常回归，不再仅援引初始本地端口10事件。

历史AD首次fixture错列/错认证header、审计rawPgError→400产品分类缺陷以及failed-copy未运行cleanup都保留原日志；不是把旧scope累计叫AG同树full。审计分类最小生产修为业务输入typed safe400、未知infra generic500，原typed403/404/409和quota分支保持，same31cb严格500与五effect断言没有弱化。

**设施交付范围已完整实证，但P0-070当前slot46 fence尚未正式验收，P0-090依赖未满足，task_complete/product_green仍false，TODO不能勾。** 这不要求先完成未来P1/P7全部政策；仅保既定依赖。本文与机器JSON是在AG运行后做的两文件元数据更新，不声称原程序full已经跑过这些新文档字节。
