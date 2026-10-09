# R5 B01-Q：公司事务的有效权限快照保护

2026-09-29。**公司事务的当前普通用户权限来源快照子包已修复并验收：相同27个测试事件原版15失败/12通过；最终23组PG/81事件连续三轮通过，完整backend 1007通过/0失败/1 browser skip、122必跑齐全，HTTP80响应通过。** 基线 `a50571f8d85e20c3359ac53e7ea9eefb7c6492e3`，分支 `fix/company-permission-snapshot-20260929`，开始工作区干净。接续 P0-070，不重复 O/P；不提前关闭完整 P0-070/P0-080/G0，原统计仍6/171。

最终程序被测 Git tree 为 `dbf66c17c2cc55a5a7866428bce06c48265b90d1`，639份文件在验收后逐一哈希核对一致。最后提交只追加文档及证据，不将源码tree冒充commit；提交号以Git回执为准。

## 1. 原版的实际窗口

原 `effectivePermission` 已经通过一条 SQL 合并用户 profile、override 和默认值；问题不是这条 SELECT 内部读出拼接结果，而是结果被放入 Actor 后，事务在 mailbox、draft 或内容查询上等待时，权限来源仍能改变。

在真实临时 PostgreSQL 上，分别对租户 profile、全局 profile、首次 override 插入、已有 override 更新和 override 清除，观察到撤权先提交，已经读取旧 CanSend 的草稿保存后提交。先开始的 override 写入被阻塞时，原版也允许新草稿保存绕过写者、读取旧覆盖值。缓存正文的域名范围撤销单列测试，不能只用发送权测试证明阅读资格。

测试使用 PgStore 的实际方法，控制器只持有精确行锁或测试表锁；顺序由 pg_blocking_pids 确认，10ms ticker 仅用于观测。每项使用 testpg 新建隔离数据库，不依赖任意 sleep、生产测试后门或 SQL 子串断言。

## 2. 原位实现

新增 `internal/store/postgres/permission_snapshot.go`，接入现有 `companyTxScope` 的非管理员有效权限加载位置，不建立第二套权限解释。

### 公司事务读取

保持原来的 tenant 锁模式及 currentMemberActor 用户 SHARE 锁。该用户行已经保护主体状态和 profile 分配；现在其 SHARE 锁还与 override 写入口的用户锁配对。读取者在执行原 COALESCE 查询前，取得被分配 profile 的 `FOR SHARE ... NOWAIT`，持有到事务结束。

这让已开始的、通过公司事务端口读取资格的操作与权限变更有明确先后。不存在 override 行也受到保护，而不是只锁住当前查得到的覆盖记录。未分配 profile 仍按既有默认值和 override 计算。

### Override 写入及清除

`UpsertUserPermissionOverride` 与 `DeleteUserPermissionOverride` 都先在新事务中取得目标用户 `FOR NO KEY UPDATE`，再写覆盖记录并提交。用户行是稳定身份，因此首次插入、更新和删除/重建都不能穿过同一用户的 SHARE 边界；不使用会在删除后消失的 override 行充当唯一锁。

清除不存在用户的 override 保持幂等无操作；为不存在用户创建 override 返回错误。必要写入失败或取消会回滚并释放用户锁。此改动不是权限编辑命令的管理员授权、字段补丁或 revision/CAS 实现。

### 为什么 profile 冲突立即返回 409

不能在持有用户 SHARE 后无限等待 profile SHARE：profile 删除先锁 profile，随后可能因 `users.permission_profile_id ON DELETE SET NULL` 需要用户锁，二者顺序相反。这里用 NOWAIT，把繁忙 profile 转成明确的 app.Conflict，并释放本事务锁；不自动重试用户命令，不删除 FK，不改变事务隔离级别。

正常读取可以共享用户/profile 锁，不把公司读取改为 tenant 排他串行。每个用户的 override 修改也不阻塞另一用户的覆盖修改。锁语义参考 PostgreSQL 16 [显式锁](https://www.postgresql.org/docs/16/explicit-locking.html)，该参考不代替本仓库实测。

## 3. 保持的语义与兼容边界

原 effectivePermission SQL、COALESCE 顺序、显式 false/0、SQL NULL、现有空域名列表含义，以及管理员原有分支均未修改。EvaluateMailboxAccess 仍是邮箱决策点；没有引入持久 revision、public DTO、schema 或迁移。

普通用户请求在 profile 正被编辑/删除锁住时可能收到 409，而不是接受旧权限；这是有意的失败关闭行为。正常 company 非管理员事务增加一次 profile 锁查询，override 写入口增加显式事务及用户锁，不宣称没有延迟或锁竞争代价。

只保护经过公司事务辅助加载的当前普通用户，以及通过两个生产 override 写端口的修改。独立 EffectivePermission 查询仍是一条语句的快照，不能据此认证旧 outbound/API Key 跨端口链；ExplainMailboxAccess 针对另一目标用户的说明快照仍需单独收敛。直接维护 SQL、DDL、所有管理写入权限、跨对象 I/O/已释放字节、全库无死锁与权限编辑 ABA 不在本批保证内。

## 4. 新的可执行回归

`r5_permission_snapshot_test.go` 包含八组顶层测试：权限修改与草稿排序；写者先行后的重载；profile 更新/删除占锁时失败关闭；同用户/同 profile 不同用户的并发；取消与审计失败释放锁；继承/false/0/空列表/默认值语义；缓存正文的域名撤权排序；override 写失败和缺失用户。

所有新测试加入已有 content-postgres-boundary 与 backend 必跑清单，不新建测试引擎。初版测试曾把 Draft.Revision 的 int 与 int64 比较，导致编译失败；已修正测试类型，保留该失败记录，不能把它计作原版业务缺陷。最终前后对照使用同一测试文件。

## 5. 最终验收

| 验证 | 实际结果 |
|---|---|
| 原基线 + 完全相同的新测试 | 27 run、15 fail、12 pass、0 skip；11个具名子场景失败，另含4个父测试失败，并非15个独立漏洞。 |
| 最终 PostgreSQL 定向 race | 原 O/P 与本批 Q 合计23组/81个事件，每轮81 pass，连续三轮，共243个通过事件，0 skip；原180秒包级预算保留。 |
| 定向门禁 | content-postgres-boundary 23个必跑全部满足。 |
| 最终 Go build / vet | `-mod=readonly`，全部通过。 |
| 完整 Go/PG race | 1008 started、1007 passed、0 failed、1 skipped。 |
| backend 门禁 | 122必跑、0缺失；唯一允许skip是独立TestR3BrowserJourney。 |
| 实际 HTTP/PG 契约 | 80响应、65成功操作、66必需成功变体通过，两个实时DNS操作明确排除。 |
| Python / 静态契约 | 168测试通过；16共享模型、33公司三方投影、67操作绑定通过。 |
| 缺DSN负例 | Go exit 0，5子用例skip、父节点pass；content-postgres-boundary门禁exit 1，未把跳过当通过。 |
| 完整性与清理 | 639源文件哈希一致；本批PostgreSQL pid84818停止exit 0且PID文件消失。 |

外层验证驱动在所有上述命令完成后，显示缺DSN门禁的多行JSON时，把其中的JSON字符串当作Go事件对象，发生 `AttributeError: 'str' object has no attribute 'get'`，因此原Job以exit 1结束；finally已成功停止数据库。该异常不是产品测试失败，也没有改成原Job成功。收尾核验改为按完整JSON格式读取报告，逐一确认退出码、Go日志hash、必跑清单hash、OpenAPI/capture hash、全部源文件和清理状态，核验通过；失败记录保留在归档。

## 6. 环境、证据与后续

沿用已经验证的 PostgreSQL 16.13 安装和 Go 1.25.7、Python 3.12 工具，本批创建独立 0700 临时集群和 0700 Unix socket，禁用 TCP，不读取生产 DSN、环境文件或员工邮件。依赖来自既有缓存，GOPROXY=off，Go 使用 readonly modules。额外进行中进度诊断在执行前被工具拦截，未重试；最终状态只以原验证 Job 的观察回执为准。

原版目标失败、首轮候选、最终验收、缺 DSN 负例分别保留；不发布原始 HTTP 响应、完整服务器日志或运行环境文件。实际工具版本为 Go 1.25.7 darwin/arm64、Python 3.12.11、PostgreSQL及客户端/pg_dump 16.13。本批集群已停止，未改全局服务。前端套件、shipping浏览器、远端CI、两个实时DNS、SSE断线/跨进程回放、S/M/L性能与生产迁移未执行。

机器结果：[R5-B01-Q-VALIDATION.json](evidence/R5-B01-Q-VALIDATION.json)。原始记录：[R5-B01-Q-LOGS.tar.gz](evidence/R5-B01-Q-LOGS.tar.gz)，105成员、160148字节，SHA-256 `cce7e6c543f365bab2948731c3238e9f546e9c09e27d4fa426976ae480e2a8d8`。包内MANIFEST.json逐成员记录hash；所有成员已验证，无环境文件、服务器日志或responses.json。

本批只完成权限来源快照子包。普通收件历史期限映射、目标用户说明快照、其余隐式外键/GC锁关系和 P0-080 协议案例仍未全部完成；P1 编辑协议、G0及整体统计不提前关闭。只创建本地提交，不 push/PR/merge/release/deploy，不运行生产迁移。
