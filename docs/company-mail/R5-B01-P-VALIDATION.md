# R5 B01-P：收件与模板写入的审计父键顺序

2026-09-29。接续 B01-O，完成收件与模板四个入口、五条写路径的真实死锁修复和收口。最终 PostgreSQL backend 为 980 pass、0 fail、1 个 browser skip，114 必跑齐全；HTTP 80 响应通过。另建隔离数据库独立复现原版五条 40P01，并在相同最终源码上重新取得完整成功结果。P0-070、P0-080 与 G0 不提前关闭，父任务仍为 6/171。

## 1. 范围和版本

本批开始 HEAD 为 `91b4f63ca81dcef8962f5bc52ecb79db7fc7ea22`，工作区干净，工作分支 `fix/company-audited-writes-20260929`。它已包含 B01-O 的数据库验收收口，不能继续把“缺 DSN”当成当前状态。唯一活跃待办仍为 `R5-TODO.md`。

生产代码只修改 `company_mail.go` 与 `company_templates.go` 的四个入口，不增加数据库迁移或权限引擎。`SaveMailTemplate` 的新增/更新两条路径分别验证，共五条写路径。

最终程序被测 Git tree 为 `9fe4aa98398074c3da970e7543b5920a3e49e700`，634 个快照文件在独立验收前后逐一核对哈希；它是源码树，不是 commit。后续提交仅追加本报告、唯一 TODO、事务地图和证据，实际提交号以 Git 历史为准。前序实现会话和本次独立收口使用不同临时集群，记录分开，不合并为一次运行。

## 2. 实际复现与修复

原版在 `MutateWorkMessage`、模板保存、模板退休、模板版本撤销及模板新建，与真实 `UpdateUserGuarded` 停用执行者并发时，五条路径分别产生 SQLSTATE **40P01**。使用每测试独立 PostgreSQL 库、精确资源锁和 `pg_blocking_pids` 安排竞争；不是用 FakeStore 或 SQL 字符串断言代替数据库执行。

旧写操作先持用户 SHARE，再在审计或新模板 INSERT 中取得租户外键的父键锁；停用成员命令先持租户 UPDATE，再锁目标用户。两条完整命令由此互等。数据库可能中止任意一方，因此断言要求双方有序成功，并检查停用后相同旧 actor 的新操作拒绝，不依赖特定一方被选为死锁牺牲者。

四个入口现复用 B01-O 已有的 `companyReferencedTx`：tenant KEY SHARE → 当前 actor SHARE / 重载资格 → 资源锁及 CAS → 必要审计与事件 → 同事务提交。`MutateWorkMessage` 另在读 mailbox/owner/grant 前取得现有 mailbox SHARE，覆盖共享整理与个人 starred 的撤权窗口。纯读取和已有管理事务不改为同一种模式。

不删除外键、不把审计移出事务、不把 40P01 吞成成功，也不引入自动重试。模板版本、退休语义、已撤销版本的幂等行为和正常收件期限政策保持原样。历史收件 restore 的期限问题仍属于独立兼容任务，本批不静默改变。

KEY SHARE 与同类锁兼容、与 UPDATE 行锁冲突，一致获取多资源锁是避免锁环的原则，参见 [PostgreSQL 16 显式锁](https://www.postgresql.org/docs/16/explicit-locking.html)。这只说明机制，仓库是否修复以真实回归为准。普通写操作可能更早等待同租户管理事务；不把安全顺序改动说成零性能代价或全库无死锁证明。

## 3. 新回归范围

`r5_audited_writes_test.go` 包含六组顶层测试：

| 测试 | 断言 |
|---|---|
| TestR5AuditedWritesOrderMemberFreeze | 五条写路径与真实成员停用同时执行，有序完成；停用后的旧身份拒绝且无新增副作用。 |
| TestR5AuditedWritesParentPrecedesActor | 操作者等待时，租户父键已受保护，NOWAIT 探针检查真实锁。 |
| TestR5AuditedWritesReloadActorAfterParentWait | 等待租户时尚未持有子用户锁；管理侧冻结完成后不接受旧 actor。 |
| TestR5AuditedIndependentWritesShareParent | 四类不同资源的写入可并行；第一资源阻塞不使第二资源因租户排他锁串行。 |
| TestR5AuditedWritesAuditFailureRollsBack | 强制审计失败后，消息/个人状态/模板/版本及审计、outbox、mailbox 事件均不变；移除故障可重新执行。 |
| TestR5MessageMutationReloadsGrantAfterMailboxWait | archive/starred 等待邮箱后重新读取撤销的资格，不留下内容/个人状态/事件修改。 |

沿用 `content-postgres-boundary` 和 backend 必跑清单，分别新增六项，不另建测试引擎。原 B01-K/O 九组继续同批执行。

初始独立并发夹具错误复用了模板唯一名称，导致三项在创建测试数据时冲突。已改为不同唯一名称，并对原版与候选使用相同最终测试重跑；原始失败保留，不将夹具失败宣称为生产缺陷。等待父键的测试允许观察早期显式锁或晚期 FK 等待，再用用户 NOWAIT 探针区分，避免把等待超时当作唯一证据。

## 4. 最终验收

| 验证 | 实际结果及来源 |
|---|---|
| 原版覆盖相同最终测试（前序实现运行） | 32 事件：21 fail、11 pass；五条完整写入/停用竞争均输出 SQLSTATE 40P01。正常独立操作与审计回滚对照本来就通过。 |
| 原版五条死锁的独立复核 | 仅覆盖同一新增测试文件；`TestR5AuditedWritesOrderMemberFreeze` 的五个子场景均复现 40P01，共6个失败事件（含父测试），0 skip。 |
| 前序最终 PG 定向 | 原K/O九组加本批六组，共15组、54事件；连续三轮全部通过。 |
| 独立最终 PG 定向 | 同一最终树再跑54 pass、0 fail、0 skip；15个必跑齐全。 |
| 独立 Go build / vet | `-mod=readonly` 通过。 |
| 独立完整 Go / PG race | 981 started、980 pass、0 fail、1 skip；`-timeout=180s` 包级预算未放宽。 |
| 独立 backend 门禁 | 114 必跑，0缺失；唯一允许跳过为单独的 `TestR3BrowserJourney`。 |
| 独立实际 HTTP / PG | 80响应、65成功操作、66必需成功变体通过；两个实时DNS操作显式排除。 |
| 独立 Python / 静态契约 | 168项通过；16共享模型、33三方投影、67操作绑定通过。 |
| 缺DSN反例（前序实现运行） | Go可因子测试skip返回0，但门禁返回1；没有用父测试pass替代真实数据库执行。 |

独立收口运行 `wc_job_T7wMK5o829av-KO7` 为机器记录的 canonical final run；前序 `wc_job_vRIiPfIsXxP0nXcf` 同树结果另列。计数包含顶层和子测试事件，不是独立漏洞数。前端套件、shipping浏览器、真实公网投递和全部权限组合未由这些测试代替。

## 5. 环境、证据与边界

使用本批全新私有原生 PostgreSQL 16.13 集群，Unix socket 权限 0700、禁用 TCP、host 认证拒绝；沿用已验证的安装、现有 Go 1.25.7 和 Python 3.12 工具，不改全局服务。测试源码由 Git tree 导出，依赖只使用已有缓存；不读取生产 DSN、环境文件、员工邮件或真实对象。

一次额外的进行中日志诊断调用在执行前被拦截；没有重试该诊断，既有验证 Job 继续按原句柄观察。失败日志与后续结果分别保存，不覆盖原记录。

剩余：profile/override 一致快照、普通收件历史期限、其他多资源/GC 锁关系、P0-080 协议案例、shipping 浏览器、实时 DNS、SSE 重连/跨进程回放、S/M/L 性能及真实 S3/断电耐久性。本批不把局部锁序验证替代整个 P0-070，不改变 6/171 的父任务统计。

独立收口集群 PID 17716 已经 `pg_ctl -m fast -w stop` 返回0，PID文件消失；前序集群记录的PID文件也已消失，本收口未再次停止或删除该集群。两处临时源码、前序失败和原始 HTTP 采集保留在原私有目录，不读取或清理其他资源。

机器摘要见 [R5-B01-P-VALIDATION.json](evidence/R5-B01-P-VALIDATION.json)，日志包 [R5-B01-P-LOGS.tar.gz](evidence/R5-B01-P-LOGS.tar.gz) 分 primary/independent 保存162个成员，共250287 bytes，SHA-256 `62a541c0346ac7c92d5b89bd892685622b5be95f30c608c6644cc3f914a2f028`。成员逐一验证，原始 responses.json、完整PostgreSQL服务日志、环境变量和数据库文件均不进入归档。

本批只创建本地提交，不 push、PR、merge、release、deploy，不运行生产迁移。没有改历史迁移、公开OpenAPI、领域数据编码、Go module/npm lockfile或前端。
