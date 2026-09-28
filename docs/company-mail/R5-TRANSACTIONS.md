# R5 事务、锁顺序与授权检查地图

> **B01-D 验证限制：** 三项生产修复及原五项锁测试曾在中间测试版本完成8/8定向通过；生产二文件此后未改。随后加强错误类型断言、增加并行提交用例，最终全量及重复验证因Docker运行超时未完成。下文的“回归覆盖”表示用例已落地，不等于最终九项或全量已通过。详见本批验证记录，070仍不关闭。

2026-09-28，B01-D更新。原地图基线为 `97675a743f83287b85297af4e94c339ce5a83bba`；B01-D从 `46711be9d8421db35a687e28e181a67a2e720b2f` 实际复现两类40P01和草稿撤权窗口，并提前修复对应生产路径。本图下列TX08/TX11及锁测试反映本次新顺序；历史证据保留在各批报告。**不是全路径无死锁证明；070仍未关闭**。进度唯一入口是 [R5-TODO](R5-TODO.md)，数据库实体和完整 FK 依据 [R5-MIGRATIONS](R5-MIGRATIONS.md) 及其实际 catalog。

## 1. 阅读方式与证据边界

`T`=tenants；`U`=users；`M`=mailboxes；`D`=mail_drafts；`A`=mail_attachments；`J`=outbound_jobs；`I`=ingest_jobs；`R`=refresh_tokens。`U锁` 表示显式 FOR UPDATE，`S锁` 表示显式 FOR SHARE，`W` 表示 UPDATE/DELETE/INSERT 自身取得的锁；不把所有 W 一律说成同一种行锁。`K` 表示 FK 检查可能要求的父键保护，具体是否发生还取决于插入/更新键及数据库行为。

以下顺序来自函数调用、SQL、触发器与实际 catalog；并发证明单列。普通 SELECT 不等于受锁保护的权限快照，启动事务也不等于整个事务都读取同一时点。这里不把两个函数共用 tenant_id 当成它们必然共享租户行锁。

PostgreSQL 16 的规则依据：[显式锁与行锁冲突](https://www.postgresql.org/docs/16/explicit-locking.html)、[事务隔离](https://www.postgresql.org/docs/16/transaction-iso.html)、[pg_blocking_pids](https://www.postgresql.org/docs/16/functions-info.html)。这些是数据库语义参考，不代替本仓库运行证据。测试只在独立数据库有界观测等待者，不在生产循环扫描会话。

## 2. 事务辅助与当前检查点

| 辅助 | 实际行为 | 不保证的事项 |
|---|---|---|
| `companyTx` / `companyTxScope(lock=true)` | Begin → T U锁 → 当前操作者 U S锁 → 当前角色/有效权限 → callback → Commit | 只有采用这条路径的管理命令受同一 T 锁约束；旧 permission handler 不自动纳入 |
| `companyReadTx` / `companyTxScope(lock=false)` | Begin → 当前操作者 U S锁 → 有效权限 → callback → Commit；没有显式 T 锁 | 名字带 Read 不等于只读，它也用于草稿、附件和单行 CAS；FK 与业务 UPDATE 仍会等待 |
| `currentMemberActor` | 精确 tenant/user 和 PrincipalUser；数据库重读角色及 is_active，用户行 S锁 | 不自动锁 permission profile、override、mailbox 或 grant |
| `mailboxAccessTx` | 本函数仍用普通SELECT读取邮箱、revision和grant；统一调用EvaluateMailboxAccess。SaveMailDraft在调用前新增同邮箱FOR SHARE并保持到提交 | 草稿与公司grant/policy/handover写入共享邮箱行边界；不扩大为profile/override/全部读取都已受锁保护 |
| `claimMailboxRevision` | 条件 UPDATE 消费观察到的 mailbox revision，零行按冲突拒绝 | 不替代外围层级/tenant/所有权校验 |
| `companyAudit` | 同一 tx 插入 audit_log 和 outbox_events | 不是所有旧操作都调用它；外部 SMTP 和对象存储也不会自动参加此事务 |

源码：`company_members.go:21–91,325–353`、`member_guard.go:26–70`、`mailbox_revision.go`。`companyTxScope` 当前映射 42501、23505、40001、55P03；没有将 40P01 明确变成可重试业务冲突。后续不得用吞掉数据库错误来制造成功。

## 3. 已清点的关键命令链

每行表示一个真实原子范围或明确的非原子边界；多个条件分支不会被强行压成唯一顺序。

| 链 | 入口与源码 | 实际顺序 / 原子范围 | 授权、版本及后续核查 |
|---|---|---|---|
| TX01 公司配置 | `ConfigureCompany`，company_members.go | T U锁 → actor U S锁 → 已验证域名读取 → settings revision 条件比较 → settings/tenant policy W → audit/outbox | 观察 revision 与改动同事务；配置改变的 FK/域名生命周期仍须按入口地图检查 |
| TX02 邮箱 grant / policy | `SetWorkGrant`、`SetWorkMailboxSendPolicy` | T U锁 → actor U S锁 → mailbox/grant/owner 读取 → mailbox revision W → grant/policy W → audit/outbox | organize/read 与 owner/成员层级校验保留；列表 snapshot 也走 T 锁 |
| TX03 邮箱移交 / 共享转换 | `TransferWorkMailbox`、`ConvertSharedMailbox` | T U锁 → actor U S锁 → mailbox U锁 → 当前 owner/revision 检查 → 所有权/类型 W → audit/outbox | 接收人资格与管理者资格不同；不得仅凭新 owner_id 写入 |
| TX04 成员冻结 / 管理更新 | `UpdateUserGuarded`，member_guard.go | T U锁 → actor U S锁 → target U U锁 → 层级/最后管理员/profile 核对 → user/session W → refresh W、owned Key 删除 → member audit | target U 锁使已开始的持有 U S锁写入先完成；新请求必须按冻结状态拒绝 |
| TX05 成员删除 | `DeleteUserGuarded` | T U锁 → actor U S锁 → target U U锁 → owner/角色保护 → key/user 删除及 FK → member audit | 所有历史资产 FK 不能被“用户已停用”自动豁免；有损删除不在本轮执行 |
| TX06 离职预览 | `PreviewOffboarding` → `offboardingTarget` | T U锁 → actor U S锁 → target U U锁 → successor U S锁 → 相关 J 按 id U锁 → 资产指纹 → plan INSERT/audit | inactive target 当前拒绝是 A04；不是目标政策 |
| TX07 离职执行 | `ExecuteOffboarding` → `applyOffboarding` | T U锁 → actor U S锁 → plan U锁 → target U U锁 → successor U S锁 → J 按 id U锁；transfer_owned 分支先 A W 后 D W；再封存/取消已知未在途 J、refresh、keys、grants、user、mailbox、audit/outbox | B01-D已将TX11改为父租户KEY SHARE→用户SHARE→附件；两种完整命令竞争由普通回归覆盖，其他交叉关系仍不宣称全部安全 |
| TX08 草稿保存 | `SaveMailDraft`，company_mail.go:373–430 | actor U S锁 → M S锁 → mailbox/grant读取 → A 按输入顺序 S锁 → 创建receipt/草稿或D revision CAS → 模板资格读取 → Commit | receipt FK 指向 `(tenant_id,user_id)`；新 draft FK 指向用户和邮箱，**不是直接指向 tenant**；现有 D 更新与首次 INSERT 等待行为不同 |
| TX09 草稿删除 | `DeleteMailDraft` | actor U S锁 → 精确 tenant/user/id/revision、未封存 D DELETE → Commit | 保留 creation tombstone；不能误称 DELETE 释放了全部历史来源 |
| TX10 上传 reserve / finish | `ReserveMailAttachment`、`FinishMailAttachment` | reserve：actor U S锁 → mailbox 普通读取 → `attachment-budget:tenant:user` advisory → sum/INSERT；finish：actor U S锁 → uploading/归属/权利读取 → A UPDATE | reserve 并发预算有同一上传者 advisory；finish 目前 UPDATE 未再次限定 uploading、未检查零行，需 P5-050 验证；不要借本图提前宣称修复 |
| TX11 原子发送入队 | `enqueueOutboundJobTx`，outbound.go | **T KEY SHARE** → submission advisory/已存在回执重放 → **sender U S锁及active检查** → A S锁 → 排序quota advisory及查询 → J INSERT/保留原BEFORE用户触发器 → archive/recipients/pins/audit/D消费 → Commit | 父租户FK锁提前，用户先于附件；KEY SHARE允许不同提交共享父键，不改为租户排他串行。回执重放不新增发送；此处active仍不是完整profile/grant/template授权 |
| TX12 逐收件人 begin / complete | outbound_recipients.go | begin：J U锁/token/lease → recipient uncertain W → J in-flight W → Commit；complete：J 条件 W → recipient结果 W → Commit | SMTP 在这两个事务之间；不确定 fence 不是最终接受证明，不把 token 检查等同全部授权 |
| TX13 手工核对投递 | `ReconcileOutbound`，company_ops.go | 当前 operator U S锁 → J U锁 NOWAIT → state/updated_at/目标检查 → recipient/J W → 必要 audit | NOWAIT 拒绝当前竞争，不代表别的阻塞都已消除；不得覆盖 accepted 事实 |
| TX14 持久入站投递 | `DeliverIngress`，ingress.go:117–209 | I U锁/token/lease → target U锁 → T U锁 → UTC daily usage W → M count W → message INSERT / index/event triggers → target delivered W → audit/outbox → clock_timestamp lease重验 → Commit | job→target→tenant 是真实次序，不能改写成“所有业务都tenant-first”；原件取得与解析在外层，提交前租约重验保留 |
| TX15 入站运维 retry | `RetryRecoveryReceipt` | 当前 operator U S锁 → I U锁 NOWAIT →检查状态/时间/hash →精确targets W → audit | 与 worker 锁族对照；只恢复指定未完成目标，原件验证不能被跳过 |
| TX16 刷新令牌轮换 | `RotateRefreshToken`，refresh_rotation.go | family普通查找 → `tabmail:refresh-family:<id>` advisory → U S锁 →旧 R U锁 → 撤销/插入后代 → Commit | 本轮通过真实等待与 NOWAIT/try-advisory 探针验证此顺序；不是先锁旧token再锁user |
| TX17 logout / family GC | `RevokeRefreshTokenByHash`、`deleteExpiredRefreshFamily` | family advisory → family R UPDATE/DELETE → Commit | logout不要求U S锁；GC在整族都到期时删除；不要新引入user→family等待而与TX16成环 |
| TX18 改密及用户会话撤销 | `ChangePasswordAtomic`、`RevokeUserRefreshTokens`，users.go | 用户条件 UPDATE/显式 U锁 → refresh rows W →必要audit（改密）→ Commit | 改密expected password hash和active是条件；与轮换用户锁顺序兼容的假设仍须保留真实旧回归 |
| TX19 已发送状态操作 | `MutateArchivedMail`，sent_archive.go | actor U S锁 → mailbox read/organize 读取 → item revision+期限条件 UPDATE → audit/outbox+mailbox event → Commit | now() 是事务时间；跨期限等待的政策与测试属于 P3-040，不能只凭条件存在就认证 |
| TX20 元数据及附件 GC | `SweepCompanyMetadata` / `sweepCompanyAttachments` | 前面多项DELETE分别自动提交；附件子事务 T U锁 → A按id LIMIT100 U锁 SKIP LOCKED → 引用复检 → DELETE + orphan INSERT一个CTE → Commit | 整个Sweep不是单事务；protected-prefix饥饿A06仍存在；旧“SaveDraft/Finish也首先T锁”注释与实现不符 |
| TX21 原件保护 / 回收 | `CreateMessageWithQuota`、`CreateIngestJob`、`ReleaseRawObjectIfUnreferenced` | 原件key advisory → ensureObject回调/引用检查 → message/job W 或 del回调 → Commit | **这些旧路径的回调在数据库事务内**，对象后端可能阻塞；不能概括成全项目“事务不做对象I/O”。不贸然把回调移出去破坏原件引用保护 |
| TX22 索引结果提交 | `CompleteMailIndexJob`，mail_content.go | 来源校验 → index job lease条件 UPDATE → parsed document写入 → Commit | 状态与派生正文同事务；claim为SKIP LOCKED，是否等待后越lease由P7独立验证 |
| TX23 域名删除 | `DeleteZone`，domain_delete.go | T U锁 → domain U锁 →资产引用检查 → domain DELETE/FK →必要audit → Commit | 此函数不自行重载 actor；服务/handler身份校验不能在图上被省略；NO ACTION FK仍保护直接SQL竞态 |
| TX24 旧权限 profile / overrides | permission handler → permissions.go | handler 多次读主体/目标/域名，随后独立 profile UPDATE 或 override UPSERT；不走完整 companyTx 管理命令 | 当前无 revision CAS，不能把中间校验当作整个读改写的串行保证；A01/A02本轮组件+HTTP+DB再次复现 |

## 4. 已实测的五项竞争观察

测试文件：[r5_lock_order_test.go](../../internal/store/postgres/r5_lock_order_test.go)。每项由 `seedCompany` 创建新数据库，使用 `pg_stat_activity` 与 `pg_blocking_pids` 确认指定 blocker 的实际等待者；只在已观察到等待后推进。10ms轮询是观测节奏，不是通过 sleep 猜测顺序；每个场景有 context 截止，探针事务回滚释放锁。

| 测试 | 控制方式 | 证明及限制 |
|---|---|---|
| `TestR5LockMapExistingDraftCASAvoidsTenantLock` | 保持 T U锁，执行不改变父键的现有 D 更新 | 更新提交且revision增加；只证明这条热路径不等显式T锁 |
| `TestR5LockMapDraftAuthorizationWaitsForMailboxFence` | 保持M U锁，观察保存先等待M SHARE再读资格 | 替代旧FK等待观测：保存现在显式保护邮箱授权读取；原FK事实仍见B01-C证据 |
| `TestR5LockMapDraftUserFenceOrdersSuspension` | 阻塞 D更新，确认writer已持 U S锁；启动真实 UpdateUserGuarded冻结，观察它等待writer | 先开始的草稿保存结束后冻结完成；冻结之后新old-actor写入拒绝 |
| `TestR5LockMapRefreshFamilyThenUserThenToken` | 保持U U锁使轮换等待；尝试family advisory与旧token NOWAIT | family已被轮换持有，旧token此时仍可锁；释放用户后轮换成功 |
| `TestR5LockMapEnqueueParentAndUserBeforeAttachments` | 保持sender U U锁，启动入队；观察U SHARE等待，探测A与T | A仍可NOWAIT锁定，T已受KEY SHARE保护；释放U后入队成功。不再把旧反向次序固定成必需行为 |

B01-C第五项原先只证明单侧次序。B01-D已补两条完整生产命令竞争：原源码在附件/用户环和用户/租户FK环都实际返回40P01；前置修复后返回有序成功或明确的禁用主体/旧计划冲突。原始失败与修复后普通回归分别保留，不据此关闭A01–A07。

## 5. 仍需处理的顺序与窗口

| 风险 | 实际支持 | 尚缺验证 / 下一操作 | 对应现有任务 |
|---|---|---|---|
| RISK01 入队与transfer_owned相反顺序 | B01-D完整ExecuteOffboarding/CreateOutboundJob实测40P01，失败事务资产无半提交 | 已改父租户KEY SHARE→用户SHARE→附件；R01回归覆盖离职先持锁时入队拒绝，无重复资产 | P0-070本批前置修复；后续P3/P4/P5/P6仍需各自全链验收 |
| RISK02 读grant后等待资源 | B01-D实证：撤去CanSend已完成，旧SaveMailDraft仍提交 | SaveMailDraft现先锁M SHARE再读取grant；R02证明撤权等待先开始的保存，完成后新保存拒绝。Finish、正文读取、profile/override变更仍待对应验证 | P0-070部分修复；P1-120、P3-060、P5-040 |
| RISK03 隐式FK与广域管理锁 | B01-D实证：入队已持user SHARE，后置tenant FK与离职tenant→user形成40P01 | 入队父键保护提前已修复此环；R03覆盖入队先完成导致旧计划冲突。入站、原件、索引/清理其他交叉仍待核对 | P0-070部分修复；P6-100、P10-040 |
| RISK04 对象回调持锁时间 | TX21实际在tx内调用ensureObject/del | 慢/失败对象适配器、取消、提交未知的故障测试与上界，不直接移出保护区 | P3-090、P6-100、P10-050 |
| RISK05 事务时钟与等待后期限 | TX19期限用now；ingress末尾用clock_timestamp再次检验lease | 精确截止跨越/阻塞后重验，不把无等待的单次通过扩大为边界成立 | P3-040、P6-030、P7-030 |
| RISK06 多资源批量W及共享锁输入顺序 | 多附件按输入顺序S锁；offboard批量UPDATE未为所有对象固定排序 | S/S本身兼容，不因此认定两次保存死锁；与W/交接/GC一起测试真实冲突 | P3-060、P4-130、P5-130 |

## 6. P0-070收口前的剩余范围

本轮已完成上述两类死锁及SaveDraft撤权窗口的前后对照和生产修复，并加入并行提交共享父键回归；仍未完成Finish/内容操作、GC引用、入站/对象回调等交叉评审，因此**070复选框保持未勾选，090仍受依赖限制**。080的目标文本见R5-PROTOCOL.md，尚缺可执行核验；G0与P1未开启。

下一轮先补最终源码的完整回归，不重复编写已修复的三个场景，先验证其余明确窗口并收口070；必要的范围变更仍须记录。原070“每个多资源写入有一致锁顺序”的要求不因本批局部回归通过而删减。

B01-C的历史观察见其验证说明；B01-D原始失败、生产修复、普通回归和提交映射见R5-B01-D-VALIDATION.md及其机器证据。原始失败、后续修正和最终通过分别保存，test name 不充当执行结果。
