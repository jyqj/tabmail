# R5 B01-U：手工重试的最终授权与状态写入原子化

2026-09-30。B01-U已完成手工重试的事务内最终授权、状态更新、必要审计与提交后准确回执。原版同测试31事件27fail/4pass；最终55组PG/204事件连续三轮通过，完整backend1151pass/0fail/1 browser skip、160必跑齐全，HTTP80响应通过。P0-070/080、完整P2和G0仍独立跟踪，唯一活跃清单为R5-TODO.md。

## 1. 起点与已复现问题

起点 `f3b64a51f1a3e52e6ca25faf1da1aabf52602e3a`，原工作区clean；分支 `fix/company-atomic-outbound-retry-20260930`。T已保护回执，不重复改回执查询或Key IP投影。

最终被测tree `136de634fa04f78cde67d1d73e72b0162c5703cb`，672份文件哈希核对一致。最终Git提交相对该树只补充文档与非敏感证据；tree不当作commit，交付SHA以Git回执为准。

原RetryJob先调用RetryAuthority、ValidateJobAuthorization，再单独UPDATE outbound_jobs。用生产Router、真实PgStore、合成任务及明确的job行锁屏障复现：用户停用、profile禁发、邮箱禁发、域名失效或Key scope撤销先完成，重试仍成功把dead改为pending。Key在job等待期间过期也仍被重试。任务等待期间被改成无效内容摘要，或recipient进入uncertain，原始裸UPDATE也不重新验证这些事实。

Key删除在原版某些用例本来就因job外键更新等待，不把这种正常排序计为新修复；原版正常状态竞争、合法模板等正例继续保留。上述复现证明不应入队的重试被提交，不等于已经证明SMTP发送越权；worker每次投递仍有独立检查。

## 2. 同一业务校验器，事务绑定读端口

新增 `store.AtomicOutboundRetry`、只读 `OutboundRetryReader` 及同步内部验证回调。应用服务唯一回调调用 `outbound.ValidateRetryRequester` 和提取后的 `outbound.ValidateJobAuthorization`。worker仍委托同一JobAuthorizationReader版本，内容摘要、Key/属主身份、域名、From地址、精确邮箱及模板政策不复制第二份。

这个内部回调是可信应用装配，不是来自HTTP的脚本或用户参数。它只能同步使用所给事务读端口，不能捕获外层Store或进行网络请求；单测以外层GetUser/裸Requeue直接panic验证接线。nil validator明确拒绝。生产唯一HTTP调用已从裸Requeue切换到应用服务；旧裸状态原语仅保留给可信内部回归，不是新的公开权限入口。

Pg读端口全部使用tx：用户、Key、域名、邮箱、grant加SHARE NOWAIT；profile复用Q的effectivePermissionSnapshot；Key/邮箱截止保存为独立值。事务内不做SMTP、对象读写或DNS。模板读取提取loadTemplateForSendTx，普通TemplateForSend和事务读者共用；模板父行/版本行加锁，grant管理由租户锁保护。发送身份精确优先/通配后备查询同样原位抽出可绑定tx的实现。

请求者与任务原始发送者独立校验。当前请求者即使拥有历史回执或读取grant，也必须仍能发件；普通用户CanSend和zone收紧、Key换属主/撤scope/停用属主都不能由陈旧Actor绕过。原始发送Key、原始员工、当前From/模板与摘要仍按既有worker规则验证。

## 3. 事务顺序和失败边界

```text
租户 SHARE（保护默认发件政策和审计父键）
→ 事务读端口保护授权来源，执行原业务规则
→ tenant/job精确FOR UPDATE，重新读取实际任务
→ 再次执行同一规则，拒绝新内容/身份或状态变化
→ recipient SHARE，uncertain绝不自动重试
→ 原requeue状态原语
→ 必要audit_log + outbox（同一事务）
→ 数据库clock_timestamp复查所有Key/邮箱原始截止
→ 同事务读取提交结果 → Commit
```

已accepted/permanent/temporary等收件人结果不被重置，attempt计数也不因人工requeue归零。并发同一任务只允许一个转移成功；不同任务可同时持有共享父键到达各自job锁。更新或审计期间超时/取消/过期，以及audit/outbox写入失败，均回滚状态、诊断清空及审计/事件，不返回半成功对象。

租户用SHARE而非KEY SHARE，是为了保护默认发件政策这一非主键字段；它会等待同公司的管理或配额排他操作，不宣称与所有写路径无竞争。依赖行NOWAIT避免旧删除路径反向锁序死锁，返回明确409，不吞错误自动重试。SQL锁参考PostgreSQL16的LOCK/行锁文档，参考不代替真实并发测试。

特殊的“没有SenderMailboxID、走已验证send identity”的历史集成任务不能锁一个不存在的邮箱。仅该路径使用mailboxes/send_identities的SHARE NOWAIT表级保护，防止空查期间插入邮箱或更高优先级精确身份；有效ownerless集成回归保留。此处有跨租户写入竞争和vacuum影响成本，繁忙时409，未做吞吐评估；普通邮箱任务不取得这两个表级锁。未来可用统一地址生命周期协议替换，但不能先删除保护。

## 4. 已提交成功后的HTTP回执

首轮候选发现，合法重试先提交、Key删除随后继续时，提交后的内容投影会返回409，造成客户端误以为重试失败（实际job已pending）。修复只针对确认commit成功后的展示：再次脱敏查询失败时，复用原RedactOutboundJobView(...,false)返回受限成功回执，隐藏正文/BCC/附件/协议细节。真正的事务失败仍返回错误，不被改成200；不重复入队，不重读原件。

这个处理不声称解决网络断开导致的commit结果未知；提交确认丢失仍需既有回执/对账。没有把已经释放的字节视为可撤回，也没有把整个SMTP过程放进数据库事务。

## 5. 验证方法与原始失败记录

新增八组PG顶层回归：十类授权变化的具体排序；锁等待后摘要/recipient/状态重验；Key等job期间过期；审计等待期间过期或取消；有效模板与版本撤销、合法集成及负查保护冲突；outbox失败及accepted记录不变；重复/独立并发命令；必要审计失败回滚。全部使用独立testpg数据库、生产Router和现有数据库等待观测；不连接生产或公网投递。

新增三个单元门禁覆盖事务读端口使用、nil validator拒绝，以及提交后投影错误不诱导重复操作。Fake使用原mutex和同一应用回调，复用原状态转移；它不伪造PostgreSQL锁、模板数据库或审计原子性，真实证明来自PG。worker、原模板接口和已有S/T测试仍参加完整后端回归。

原版先以相同测试取得红灯，再验修复。候选1的Key删除场景暴露提交后错误回执，已保存诊断中的status=409/state=pending。候选2的同一job双等待测试误以为两者必然直接等待控制器；PostgreSQL也会让第二个排在第一个元组等待者之后。改为有界递归观测真实阻塞链，不放宽结果断言或12秒预算，再将最终同一份测试用于原版与修复版。一次模板结构编辑因匹配到两个入口被整体拒绝，没有部分写入。

## 6. 最终验收

| 验证 | 实际结果 |
|---|---|
| 原版覆盖完全相同最终PG测试 | 31事件，27fail/4pass，0skip；包含父测试、审计新不变量和行为反例，不是27个独立漏洞。 |
| PostgreSQL定向（含K/O/P/Q/R/S/T） | 每轮204pass，连续三轮全部通过；55必跑齐全，0fail/skip。 |
| 完整Go/PostgreSQL race | 1152started、1151pass、0fail、1skip；原180秒包级预算不变。 |
| 完整backend门禁 | 160必跑齐全，0缺失；唯一允许skip为TestR3BrowserJourney。 |
| 实际HTTP/PG契约 | 80响应、65操作、66成功变体通过；两项实时DNS接口显式排除。 |
| Go build/vet | readonly modules，均通过。 |
| Python/静态契约 | 168测试通过；16共享模型、33三方DTO投影、67操作绑定通过。 |
| 缺DSN负例 | 10子测试skip、父节点pass；Go exit0但必跑门禁exit1，未把跳过计通过。 |

最终命令退出码、Go日志hash、必跑清单hash、OpenAPI及HTTP采集hash、源码hash分别核验，原始失败保留。唯一可选进度读取曾在执行前被工具拦截，未重发，也未计为任何测试结果。

## 7. 兼容与剩余范围

未修改公开DTO/OpenAPI、数据库迁移、Go/npm依赖或前端。保留旧入口的初始回执/状态/错误检查，但它们不再承担最终授权；最终写入只走原子端口。成功后额外查询失败时返回受限成功回执，是有意修正不准确错误结果。没有删除员工邮件、修改生产配置或运行生产迁移。

本批只闭合人工重试的数据库决策/状态/必要审计边界。新提交入队的profile/Key/模板全链、全部JWT session版本与旧POST幂等重放、普通收件历史期限兼容、其余GC/多资源锁、P0-080协议真值表、P1编辑CAS及发布验证仍待推进。worker最后校验到远端SMTP接受不是分布式原子事务；对象/附件实际可用性仍由worker和既有恢复协议检查。

环境使用既有Go1.25.7、Python3.12虚拟环境及PG16.13；本批0700临时根/Unix socket、禁TCP、host认证拒绝，child PATH前置Python3.12，依赖readonly且GOPROXY=off。原始HTTP捕获只在本地0600保存，不归档responses.json、运行环境文件或完整数据库服务日志。仅本地提交，不push/PR/merge/release/deploy；父清单6/171不提前更改。

实际版本Go1.25.7 darwin/arm64、Python3.12.11、PostgreSQL/psql/pg_dump16.13。本批PG pid29686已停止exit0且PID文件消失；未操作共享工具或其他数据库。

[机器结果](evidence/R5-B01-U-VALIDATION.json)、[原始执行记录](evidence/R5-B01-U-LOGS.tar.gz)：96成员、191776字节，SHA-256 `9e20e40aa739ca0ca943fd33366c2552f3019cb4b1b11ee399b0bff61f43ce90`。归档成员逐一与MANIFEST核对一致，原始responses.json没有发布。
