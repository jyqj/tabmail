# R5 B01-K：内容返回前重验与已发送期限边界

> 后续状态：本文保留 B01-K 当时未运行 PG 的历史记录。六组数据库测试现已在 B01-O 实际执行，并修复由真实验收发现的审计外键/撤权死锁；完整后端与HTTP结果见 [B01-O验收](R5-B01-O-VALIDATION.md)。不把新结果回写成当时已经完成。

2026-09-29。**应用层修复已完成相同测试的前后对照；PostgreSQL 锁序与期限候选已经实现、编译，但真实数据库并发验收未执行。本批为部分完成，不关闭 P0-070、P0-080 或 G0，统计仍为 6/171。**

## 1. 基线与被测源码

开始时分支 `test/company-http-contract-20260929`、工作区干净，HEAD 为 `1304ee05ecba665df3b09726c0084858ca7098cd`。本批在 `fix/company-content-boundary-20260929` 工作，没有重复上一轮 HTTP 响应绑定。

最终 Go 被测 tree 为 `9e50822571f14dec945f2c4180897e112fb0ef39`，快照含 607 个版本文件。它是临时 Git index 生成的 tree，不是假称 commit。最终报告与证据在测试后追加；具体提交以 Git 历史为准。

较早候选 tree 为 `cde28bb03ad724f1e7307ff413404d604d6f693b`。其后只补两处测试：首次读取失败用例增加零长度 Read 检查；数据库锁探针增加 defer 回滚。生产实现、脚本、依赖及 SQL 在两个候选之间未再修改。最终 Go build/vet、定向与全量无 DSN 测试已重新运行；Python 168 项与静态契约成绩来自前一候选，保留该来源而非改标签。一次补充最终 Python/环境/哈希核对调用在执行前被工具安全检查拦截，没有换入口重试，也没有把它计入执行结果。

## 2. 已实施的应用层修复

### 对象 I/O、解析与缓存不是权限凭据

`companymail.Service` 现在在内容返回前，通过同一个 `GetWorkMessage` 端口重验当前主体/邮箱资格和原件键。正文、附件元数据、序号附件与稳定 ID 附件均覆盖；缓存命中不能跳过权限检查，也不能把不同 message/source/parser 的派生内容拼接到旧元数据上。

开始读取时复制内部 metadata，避免适配器复用指针后把旧来源检查悄悄改成新来源。缓存失败或来源不符安全拒绝，不回退到直接读取原始对象。缓存写入完成后再次检查，数据库错误和 context 取消不返回正文。

上传附件与已发送附件在准确字节、大小及摘要校验后，重新通过各自原有内容端口获取资格，并比对 ID、原件键、摘要、大小和 ready 状态。已发送附件仍走 sent-asset 权限端口，不借上传者或历史投递者身份放行。

### EML 原件首批字节

`Source` 在对象打开后重验；由于后端可能到 `Read` 才真正加载数据，返回一个首批字节守卫。第一次有数据的 Read 后、把字节交给调用者之前，再重验当前资格。失败返回 `n=0`、清空本次写入的调用者缓冲区、立即关闭底层流，后续读取不能恢复。零长度 Read 不消耗检查机会。

正常流首批字节放行后，不再逐块查询数据库；`Close` 幂等。这里定义的是释放前决策点，不宣称能够撤回已发送字节，也不保证 HTTP 头已经写出后还能改成 JSON 错误响应。没有把数据库事务延长到对象 I/O 或整个下载期间。

## 3. 已实施但待 PostgreSQL 验收的候选

### 邮箱授权快照

`GetWorkMessage`、`GetParsedMessage`、`SaveParsedMessage` 在用户 SHARE 锁之后、读取 mailbox/owner/revision/grant 之前，复用 `lockMailboxAuthorization` 获取邮箱 SHARE 锁。该边界与现有推进 mailbox revision 的授权、发送策略和交接写入相对齐，不新增全租户排他锁。

这不等于 permission profile/override 的独立写入已被保护，也不认证其他未改用该边界的列表或操作。`GetParsedMessage` 在事务失败时不再同时返回已填充正文的对象。

### 已发送条目的期限与恢复

共享 `sentContentFrom` 使用实际数据库时间检查 expires_at/purge_after，不再用事务开始时间。这一入口由已发送列表、正文与附件投影复用。

`MutateArchivedMail` 的次序为：当前 user SHARE → mailbox SHARE → 当前 read/organize → 精确 item/revision FOR UPDATE → 新语句读取数据库时钟检查原始 expires/purge/mailbox 期限 → 修改 item → 必要 audit/outbox/事件 → 再次读取实际时钟检查原始期限 → Commit。

第二次检查保留修改前的 purge_after，即使 restore 已经在事务中清空该字段，也不能在后续审计等待跨过原截止后提交。任一检查、审计或事件失败均返回事务错误，预期整体回滚；该回滚与并发次序仍须由下列真实数据库测试确认。最后检查是提交决策点，不承诺 COMMIT 确认返回的时刻仍在期限内。

## 4. 相同测试的红绿结果

在原始 `1304ee0…` 导出副本中只覆盖两份新应用层测试文件，其他源码不改：

- `base-exact`：49 个测试事件，48 fail、1 pass；其中 42 个失败为具名子场景，另有父测试失败与两个独立失败测试。正常原件读取对照通过。不是 48 个独立业务漏洞。
- 最终 `content-boundary` 定向：60 个测试事件全部通过，9 个顶层必跑测试齐全，0 skip。包含应用层内容/附件/缓存/取消测试，以及实际时钟调用与期限纯函数单测。
- 定向适配器使用确定性 callback 让撤权、资源不可用、来源变化发生在对象打开/读取或缓存获取/保存期间，没有任意 sleep。它们证明应用服务边界，不伪装成 PostgreSQL、HTTP 或实际对象后端的验证。

## 5. 本批实际验证

| 验证 | 结果与来源 |
|---|---|
| 最终 `go build -mod=readonly ./...` | 通过；最终 9e5082… tree |
| 最终 `go vet -mod=readonly ./...` | 通过；包含新增 PG 用例的编译/静态检查 |
| 最终定向 Go race | 60 pass、0 fail、0 skip；content-boundary 9 必跑齐全 |
| 最终全量 Go race，无 DSN | 802 started、643 pass、0 fail、159 skip；这不是后端整体验收通过 |
| backend 必跑门禁 | 新增 15 项，55 → 70；对上述无 DSN 日志实际返回 exit 1，正确拒绝缺失/跳过的数据库用例 |
| Python 测试 | 168 项通过；来源 cde28b…，最终仅修改两处 Go 测试后未重复取得该结果 |
| 静态契约 | 来源 cde28b…：16 共享模型、33 投影、67 操作绑定通过；实际 HTTP 未重跑 |
| 运行环境 | 既有 Go 1.25.7 darwin/arm64、Python 3.12.11，隔离 HOME/TMPDIR，GOPROXY=off，使用既有模块/编译缓存 |
| 数据库 | 未配置 TABMAIL_TEST_DB_DSN；本批未创建、启动、连接或清理数据库 |

## 6. 六个待执行数据库测试

`internal/store/postgres/r5_content_wait_test.go` 复用 seedCompany/testpg 与真实 pg_blocking_pids 等待观测，每个用例创建独立测试库：

| 测试 | 目标场景 |
|---|---|
| `TestR5SentMutationRejectsExpiryAfterItemWait` | item 锁等待跨过内容 expires 或垃圾箱 purge；恢复不得清空过期界限 |
| `TestR5SentMutationRollsBackExpiryDuringAuditWait` | 审计表等待跨过内容、purge 或 mailbox 截止，item/audit/outbox/event 不半提交 |
| `TestR5SentMutationOrdersGrantRevocation` | NOWAIT 探针与完整授权命令确认 mailbox fence；已有操作完成后撤权，后续操作拒绝 |
| `TestR5SentMutationCancellationAndAuditFailureRollBack` | 取消、审计失败后无业务/证据残留，下一合法操作可取得锁 |
| `TestR5SentReadUsesTimeAfterIdentityWait` | user 身份锁等待跨期限后正文/附件元数据拒绝、列表不返回过期项 |
| `TestR5ContentReadsFenceMailboxSnapshot` | message/parsed/cache-save 等待 mailbox 后读取新授权，拒绝后缓存内容不被覆盖 |

以上本批均因缺 DSN 未执行，不能引用其名称宣称结果成功。Go 子测试被跳过时父测试仍可能产生 pass 事件；本批这六组出现 14 个跳过事件，不能由父节点 pass 推导数据库案例已运行。完整 backend 门禁实际报告 70 个必跑中 53 个未满足，返回失败。准备好授权的临时数据库后，先在原基线覆盖相同测试取得目标对照，再在候选运行；随后执行完整 backend 与 HTTP 门禁，不只跑纯函数套件。

## 7. 未完成、兼容性与后续

普通收件 messages 的 expires_at/purge_after 与个人/共享永久保留历史规则尚未在本批统一；本批没有把物理 GC 保护条件直接当作新的可读政策。旧 outbound 内容入口、profile/override 同步快照、其余多资源锁图、完整 P0-080、浏览器/HTTP 实测、长流撤权、性能与依赖漏洞审计仍未完成。

新增前后检查增加有界的 repository 调用；正常 EML 流总共最多三次 GetWorkMessage，不按数据块增长。实际查询延迟、锁竞争与对象后端行为没有测量，不能声称性能无回归。

没有迁移、领域摘要、Go module/npm lockfile、OpenAPI 或前端修改；不生成平行权限引擎。保存本地代码和证据，不 push/PR/merge/release/deploy，不操作生产配置、员工邮件或生产数据。

机器摘要见 [R5-B01-K-VALIDATION.json](evidence/R5-B01-K-VALIDATION.json)，[R5-B01-K-LOGS.tar.gz](evidence/R5-B01-K-LOGS.tar.gz) 保存 76 份记录（191178 bytes，SHA-256 `1412484f52289fa96cd3d607830aca59b1f7cde27678ab44f45bdab183d2d959`）。原始失败、最终成功和 backend 预期拒绝分别留存，不重写历史；归档不包含原始 HTTP 响应、运行环境文件、源码 tar 或生产数据。唯一活跃任务清单仍为 R5-TODO.md。
