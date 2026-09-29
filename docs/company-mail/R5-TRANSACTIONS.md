# R5 事务、锁顺序与授权检查地图

> **B01-T 当前状态：** 旧回执现在由当前主体事务和同条SQL返回可见ID/total/页面/内容决定，Key查询后按实际时钟再验截止；attempts及recipient在读取后重验。47组PG173事件三轮、完整backend1117pass/1 browser skip、149必跑、HTTP80响应通过。另修复Key非空inet使用IP解码，重试最终写入授权仍未合并，见 [B01-T](R5-B01-T-VALIDATION.md)。

> **B01-S 当前状态：** 旧outbound正文/诊断通过新角色端口复用sent存活与内容资格；owned Key当前metadata/owner重验，最终时钟在邮箱等待之后。39组PG142事件三轮、完整backend1084pass/1 browser skip、139必跑、HTTP80响应均通过。仅内容投影范围，回执/重试/批量快照仍待验，见 [B01-S](R5-B01-S-VALIDATION.md)。

> **B01-R 当前状态：** 目标用户访问说明的active/profile/override跨代组合已实测修复；来源和能力共用一次邮箱判断。30组PG/115事件三轮通过，完整backend1041pass/1 browser skip、129必跑、HTTP80响应通过。见 [B01-R](R5-B01-R-VALIDATION.md)；以下Q中目标说明未覆盖的描述保留历史含义。

> **B01-Q 当前状态：** 公司事务当前普通用户的profile/override来源窗口已实测修复；23组PG/81事件三轮通过，完整backend1007pass/1 browser skip、122必跑齐全、HTTP80响应。读取先保护用户及profile，覆盖写入先锁稳定用户；繁忙profile以409拒绝，避免用户/profile反向等待。仅为当前主体的公司事务范围，目标说明和旧接口仍另验。见 [B01-Q](R5-B01-Q-VALIDATION.md)。

> **B01-P 当前状态：** 收件状态修改、模板新建/保存/退休/版本撤销与成员冻结的五条写路径已真实复现并修复40P01。现统一为T KEY SHARE → 当前actor SHARE → 资源/CAS → 必要审计/事件；收件另持M SHARE。15组PG/54事件三轮通过并独立重跑，完整backend980pass/1 browser skip、114必跑齐全、HTTP80响应通过。完整070/080/G0仍未关闭，见 [B01-P记录](R5-B01-P-VALIDATION.md)。

> **B01-O 当前状态：** B01-K 六组PG已实测，发现并修复 sent 操作与撤权的 audit FK 锁环；最终9组/22事件连续三轮通过，完整backend948pass/1 browser skip、108必跑齐全、HTTP80响应通过。以下B01-K/L/M/N缺DSN说明保留历史含义，当前局部运行阻塞已解除。见 [B01-O记录](R5-B01-O-VALIDATION.md)。

> **B01-N：** 真实文件对象后端完成根内路径、长度、取消与原子发布红绿（三轮各67pass），不改数据库锁序或rawobject引用回调。父目录切换失败只清理本次临时对象，不碰根外夹具；Rename后Sync失败仍属可能已发布，不用抢删补偿。B01-K PG未验收，见 [B01-N记录](R5-B01-N-VALIDATION.md)。

> **B01-M：** `mailcontent.Parser` 在数据库事务之外、对象打开前取得实例内四许可；共享30秒截止包含排队，读取关闭与失败释放经虚拟时钟/屏障测试验证。单key等待者仍共享一次解析，缓存命中不排队。它不改变本图SQL锁序，不把每实例解析许可等同全进程内存/请求上限，也不补足B01-K尚缺的PG证据。详见 [B01-M](R5-B01-M-VALIDATION.md)。

> **B01-L 更新：** EML 首读拒绝不再变成空 200，中途失败中断传输；回复/转发在复制和返回前重验源/目标资格。应用层及本机 HTTP/1.1、HTTP/2 相同测试红绿通过，43事件三轮；未执行PG，K候选及下文锁图缺口仍在。见 [B01-L记录](R5-B01-L-VALIDATION.md)。

> **B01-K 更新（部分完成）：** 内容 I/O 后同端口重验、缓存来源与 EML 首批字节保护已取得应用层相同测试红绿；mailbox SHARE 和 sent 锁后/审计后实际时钟候选已实现。新 PG 用例只编译未执行，全量无 DSN 643pass/159skip，backend 门禁拒绝，不覆盖下文历史实测。见 [B01-K记录](R5-B01-K-VALIDATION.md)。

> **B01-J验证更新：** H候选已取得相同测试的基线6通过/4目标失败、候选10通过；最终原180秒预算全量589通过、54必跑齐全，40项各三轮120通过。见 [B01-J记录](R5-B01-J-VALIDATION.md)。070尚有跨语句授权/内容期限及其他多资源边界，不据此宣称全项目无竞争风险。

2026-09-28，B01-D更新。原地图基线为 `97675a743f83287b85297af4e94c339ce5a83bba`；B01-D从 `46711be9d8421db35a687e28e181a67a2e720b2f` 实际复现两类40P01和草稿撤权窗口，并提前修复对应生产路径。本图下列TX08/TX11及锁测试反映本次新顺序；历史证据保留在各批报告。**不是全路径无死锁证明；070仍未关闭**。进度唯一入口是 [R5-TODO](R5-TODO.md)，数据库实体和完整 FK 依据 [R5-MIGRATIONS](R5-MIGRATIONS.md) 及其实际 catalog。

## 1. 阅读方式与证据边界

`T`=tenants；`U`=users；`M`=mailboxes；`D`=mail_drafts；`A`=mail_attachments；`J`=outbound_jobs；`I`=ingest_jobs；`R`=refresh_tokens。`U锁` 表示显式 FOR UPDATE，`S锁` 表示显式 FOR SHARE，`W` 表示 UPDATE/DELETE/INSERT 自身取得的锁；不把所有 W 一律说成同一种行锁。`K` 表示 FK 检查可能要求的父键保护，具体是否发生还取决于插入/更新键及数据库行为。

以下顺序来自函数调用、SQL、触发器与实际 catalog；并发证明单列。普通 SELECT 不等于受锁保护的权限快照，启动事务也不等于整个事务都读取同一时点。这里不把两个函数共用 tenant_id 当成它们必然共享租户行锁。

PostgreSQL 16 的规则依据：[显式锁与行锁冲突](https://www.postgresql.org/docs/16/explicit-locking.html)、[事务隔离](https://www.postgresql.org/docs/16/transaction-iso.html)、[pg_blocking_pids](https://www.postgresql.org/docs/16/functions-info.html)。这些是数据库语义参考，不代替本仓库运行证据。测试只在独立数据库有界观测等待者，不在生产循环扫描会话。

## 2. 事务辅助与当前检查点

| 辅助 | 实际行为 | 不保证的事项 |
|---|---|---|
| `companyTx` / `companyTenantWriteLock` | Begin → T U锁 → 当前操作者 U S锁 → 当前角色/有效权限 → callback → Commit | 只有采用这条路径的管理命令受同一 T 锁约束；旧 permission handler 不自动纳入 |
| `companyReadTx` / `companyNoTenantLock` | Begin → 当前操作者 U S锁 → 有效权限 → callback → Commit；没有显式 T 锁 | 名字带Read不等于只读；隐式FK仍会加锁，尤其不能把晚期审计INSERT当作无父键依赖 |
| `companyReferencedTx` / `companyTenantReferenceLock` | Begin → T KEY SHARE → 当前操作者 U S锁 → 有效权限 → callback → Commit | O已用于MutateArchivedMail；父键先于actor/mailbox，普通独立修改共享父键，不替代管理、邮箱权限或所有其他写路径 |
| `currentMemberActor` | 精确 tenant/user 和 PrincipalUser；数据库重读角色及 is_active，用户行 S锁 | 本函数不锁profile；Q由companyTxScope随后调用effectivePermissionSnapshot，用户S锁与override写者NOKEY锁配对；其他直接调用者不自动获得整个保护 |
| `mailboxAccessTx` / `mailboxAccessWithSourceTx` | 原签名委托私有实现，同一次owner/grant读取生成来源与EvaluateMailboxAccess能力；调用方负责用户/M锁 | R的Explain在目标/profile锁后持M SHARE并复用来源，不再重复管理员/目标查询；其他旧调用者不因helper而自动取得锁 |
| `effectivePermissionSnapshot`（Q/R） | 已持指定用户S锁 → 已分配profile S锁NOWAIT → 原effectivePermission单语句合并；Q用于当前用户，R用于作用域内目标用户 | 繁忙profile立即app.Conflict，避免DELETE profile→user与user→profile互等；不改变管理员分支或独立EffectivePermission端口语义 |
| `permissionOverrideTx`（Q） | user NO KEY UPDATE → override INSERT/UPDATE/DELETE → Commit | 锁稳定user保护覆盖行的有/无状态；不额外锁tenant，仍需P1的写入授权、CAS及字段意图协议；直接维护SQL不自动参加此约定 |
| `claimMailboxRevision` | 条件 UPDATE 消费观察到的 mailbox revision，零行按冲突拒绝 | 不替代外围层级/tenant/所有权校验 |
| `companyAudit` | 同一tx插入audit_log和outbox_events；audit_log.tenant_id通过FK需要T父键保护 | O实证晚期FK可与tenant-first管理成环；不是所有旧操作都调用它，外部SMTP/对象存储也不参加此事务 |

源码：`company_members.go`（companyTxScope、companyReferencedTx、mailboxAccessTx）、`member_guard.go`（lockMemberTenant、currentMemberActor）、`mailbox_revision.go`。`companyTxScope` 当前映射 42501、23505、40001、55P03；没有将 40P01 明确变成可重试业务冲突。后续不得用吞掉数据库错误来制造成功。

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
| TX10 上传 reserve / finish | `ReserveMailAttachment`、`FinishMailAttachment` | reserve：actor U S锁 → M S锁 → 当前mailbox/grant读取 → `attachment-budget:tenant:user` advisory → sum/INSERT；finish：actor U S锁 → 预定位mailbox → M S锁及当前资格 → 精确A U锁 → 锁后clock_timestamp/原state条件W → Commit | B01-F实证reserve窗口并前置邮箱锁；B01-G已补修复后全部10项及全量/重复回归。B01-E的Finish修复保留，不替代完整P5-050 |
| TX11 原子发送入队 | `enqueueOutboundJobTx`，outbound.go | **T KEY SHARE** → submission advisory/已存在回执重放 → **sender U S锁及active检查** → A S锁 → 排序quota advisory及查询 → J INSERT/保留原BEFORE用户触发器 → archive/recipients/pins/audit/D消费 → Commit | 父租户FK锁提前，用户先于附件；KEY SHARE允许不同提交共享父键，不改为租户排他串行。回执重放不新增发送；此处active仍不是完整profile/grant/template授权 |
| TX12 逐收件人 begin / complete | outbound_recipients.go | begin：J U锁/token/lease → recipient uncertain W → J in-flight W → Commit；complete：J 条件 W → recipient结果 W → Commit | SMTP 在这两个事务之间；不确定 fence 不是最终接受证明，不把 token 检查等同全部授权 |
| TX13 手工核对投递 | `ReconcileOutbound`，company_ops.go | 当前 operator U S锁 → J U锁 NOWAIT → state/updated_at/目标检查 → recipient/J W → 必要 audit | NOWAIT 拒绝当前竞争，不代表别的阻塞都已消除；不得覆盖 accepted 事实 |
| TX14 持久入站投递 | `DeliverIngress`，ingress.go:117–209 | I U锁/token/lease → target U锁 → T U锁 → UTC daily usage W → M count W → message INSERT / index/event triggers → target delivered W → audit/outbox → clock_timestamp lease重验 → Commit | job→target→tenant 是真实次序；B01-G实测等待tenant自然越lease后全部回滚，以及audit失败后的消息/配额/进度/派生写入回滚。原件与SMTP在外层，不由这三项DB测试认证 |
| TX15 入站运维 retry | `RetryRecoveryReceipt` | 当前 operator U S锁 → I U锁 NOWAIT →检查状态/时间/hash →精确targets W → audit | 与 worker 锁族对照；只恢复指定未完成目标，原件验证不能被跳过 |
| TX16 刷新令牌轮换 | `RotateRefreshToken`，refresh_rotation.go | family普通查找 → `tabmail:refresh-family:<id>` advisory → U S锁 →旧 R U锁 → 撤销/插入后代 → Commit | 本轮通过真实等待与 NOWAIT/try-advisory 探针验证此顺序；不是先锁旧token再锁user |
| TX17 logout / family GC | `RevokeRefreshTokenByHash`、`deleteExpiredRefreshFamily` | family advisory → family R UPDATE/DELETE → Commit | logout不要求U S锁；GC在整族都到期时删除；不要新引入user→family等待而与TX16成环 |
| TX18 改密及用户会话撤销 | `ChangePasswordAtomic`、`RevokeUserRefreshTokens`，users.go | 用户条件 UPDATE/显式 U锁 → refresh rows W →必要audit（改密）→ Commit | 改密expected password hash和active是条件；与轮换用户锁顺序兼容的假设仍须保留真实旧回归 |
| TX19 已发送状态操作（O已测） | `MutateArchivedMail`，sent_archive.go | T KEY SHARE → actor U S锁 → M S锁 → 当前read/organize → 精确item/revision U锁 → DB实际时间检查原expires/purge/mailbox期限 → item W → audit/outbox+mailbox event → 再验原期限 → Commit | O实测锁后/审计后过期回滚、restore保留原purge截止、撤权两操作有序、父先于actor、独立item并发及等待后冻结拒绝；非全锁图证明 |
| TX20 元数据及附件 GC | `SweepCompanyMetadata` / `sweepCompanyAttachments` | 前面多项DELETE分别自动提交；附件子事务 T U锁 → A按id LIMIT100 U锁 SKIP LOCKED → 引用复检 → DELETE + orphan INSERT一个CTE → Commit | 整个Sweep不是单事务；protected-prefix饥饿A06仍存在；旧“SaveDraft/Finish也首先T锁”注释与实现不符 |
| TX21 原件保护 / 回收 | `CreateMessageWithQuota`、`CreateIngestJob`、`ReleaseRawObjectIfUnreferenced` | 原件key advisory → ensureObject回调/引用检查 → message/job W 或 del回调 → Commit | **这些旧路径的回调在数据库事务内**，对象后端可能阻塞；不能概括成全项目“事务不做对象I/O”。不贸然把回调移出去破坏原件引用保护 |
| TX22 索引结果提交（J已实测） | `CompleteMailIndexJob` / `FailMailIndexJob`，mail_content.go | complete：source message S锁→job U锁→新语句clock_timestamp资格→document写入→最终lease条件UPDATE→Commit；fail：job U锁→实际时间复核→条件UPDATE→Commit | J实证三个等待超lease场景原版仍提交，候选拒绝并回滚；source-before-job由NOWAIT探针验证。7项索引/3项读取及全量通过，非完整死锁或全部claim/retry认证 |
| TX23 域名删除 | `DeleteZone`，domain_delete.go | T U锁 → domain U锁 →资产引用检查 → domain DELETE/FK →必要audit → Commit | 此函数不自行重载 actor；服务/handler身份校验不能在图上被省略；NO ACTION FK仍保护直接SQL竞态 |
| TX24 旧权限 profile / overrides | permission handler → permissions.go | handler 多次读主体/目标/域名，随后独立 profile UPDATE 或 override UPSERT；不走完整 companyTx 管理命令 | 当前无 revision CAS，不能把中间校验当作整个读改写的串行保证；A01/A02本轮组件+HTTP+DB再次复现 |

### B01-K 的内容返回边界（应用层已测，PG接线待测）

`GetWorkMessage` 与 parsed cache 读写：user SHARE → mailbox SHARE → 当前 owner/grant/revision → 内容查询/派生写入 → Commit。对象打开/解析不在这些事务里。`companymail` 在 I/O 或缓存完成后重新调用相同权限端口，比较原始 message/source；上传和已发送附件重新通过各自原端口核对 ID/key/hash/size/state。

EML 在打开完成后和首批字节释放前各有检查。首次 Read 失败立即关闭流并不给出字节；正常首批放行后不逐块查数据库。它不是“已发送字节可撤销”，也不是全下载时段持数据库锁。新 `r5_content_wait_test.go` 用例尚无执行结果，不放进下一节“已实测”表。

### B01-L 的 HTTP 与多步 compose 决策点（非数据库并发证明）

原件链：`Source` 既有前置授权/对象打开后检查 → HTTP 先读最多32KiB并触发首批字节检查 → 成功后写下载响应头/首块 → 继续流式复制。头前失败使用原应用错误映射；头后读取/客户端写入失败中断传输，不拼接JSON。连续空读最多100次，取消在Read前后检查。真实本机HTTP/1.1与HTTP/2验证中断可见，但repo与对象流为合成适配器，没有新PG验收。

Compose 链：目标发送资格快照 → 源邮件及原件快照/解析后源重验 → 每次附件复制前重新核对源和目标 → 既有reserve/put/finish → 最终payload返回前再核对双方。源键或目标地址变化拒绝；首次已完成上传保持原恢复/留存约定，不宣称后续拒绝能回滚已复制附件。各权限调用仍为既有独立事务，未跨对象I/O持锁，也未把多个调用说成一个线性化事务。

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
| RISK02 读grant后等待资源 | B01-D实证：撤去CanSend已完成，旧SaveMailDraft仍提交 | SaveMailDraft/Finish/Reserve共用M SHARE授权边界；B01-G已补Reserve修复后的撤权顺序、额度原子性、不同上传者及取消回归。J三项读取证实新请求撤权拒绝、管理身份不授予正文、身份等待后冻结拒绝；Q已补当前用户profile/override跨语句保护；另一目标说明、旧接口和普通内容期限仍待验证 | P0-070部分修复；P1-120、P3-060、P5-040 |
| RISK03 隐式FK与广域管理锁 | B01-D实证：入队已持user SHARE，后置tenant FK与离职tenant→user形成40P01 | 入队父键保护提前已修复此环；R03覆盖入队先完成导致旧计划冲突。入站、原件、索引/清理其他交叉仍待核对 | P0-070部分修复；P6-100、P10-040 |
| RISK04 对象回调持锁时间 | TX21实际在tx内调用ensureObject/del | B01-F基线实测可控delete取消释放key、ensure期间GC等待且提交后保护引用；实际对象后端、提交未知和其余失败上界仍待验，不直接移出保护区 | P3-090、P6-100、P10-050 |
| RISK05 事务时钟与等待后期限 | 旧TX19期限用now；K改sent读侧实际时钟、写侧锁后和审计后检查原期限 | O已取得K真实PG前后对照并通过；G/J入站/索引历史结果保留，普通收件条目期限和其他时钟边界仍未统一 | P0-070局部已验收；P3-040、P6-030、P7-030 |
| RISK06 多资源批量W及共享锁输入顺序 | 多附件按输入顺序S锁；offboard批量UPDATE未为所有对象固定排序 | S/S本身兼容，不因此认定两次保存死锁；与W/交接/GC一起测试真实冲突 | P3-060、P4-130、P5-130 |

### B01-O：审计FK的实际锁环与后续候选

已复现：sent操作持M SHARE后才在audit_log INSERT取T KEY SHARE，SetWorkGrant持T UPDATE后等M，服务器返回40P01。现在TX19前置T KEY SHARE，原错误映射不改，原始和修正控制器均保留红灯；修复后9组PG连续三轮、完整backend及HTTP通过。

B01-O列出的四个静态候选已经在B01-P逐项复现：`MutateWorkMessage`、`SaveMailTemplate`（新建/更新）、`SetMailTemplateRetired`、`RevokeMailTemplateVersion` 与 `UpdateUserGuarded` 并发均出现40P01。现复用companyReferencedTx前置T KEY SHARE，收件修改在读取owner/grant前持M SHARE；没有将审计移出事务、删除FK或重试吞错。独立不同资源写入不被T排他串行化、等待后冻结/撤权拒绝、审计失败完整回滚均有PG回归。

### B01-Q：有效权限来源保护

原effectivePermission的单条SELECT已经一致，但Actor快照会跨后续等待使用；原版真实复现profile局部/全局更新、override插入/更新/清除先完成，旧草稿后提交，以及缓存正文在域名撤权后仍从等待中返回。现在当前user S锁与override写者NO KEY UPDATE配对，profile单独S锁NOWAIT后再执行原合并SQL。Q八组27事件与O/P一起重复验收；P1持久revision、管理员写授权、独立EffectivePermission调用及ExplainMailboxAccess目标说明并未因此关闭。

### B01-R：目标访问说明与来源单次投影

`ExplainMailboxAccess` 保留 T UPDATE → 当前管理员SHARE/重验 → 同tenant目标用户SHARE → 有效普通目标profile SHARE NOWAIT → mailbox SHARE → 单次owner/grant/能力判断 → Commit。目标用户的覆盖写者使用Q稳定user NO KEY UPDATE；profile写者与SHARE冲突。空grant仍是grant来源，停用目标的能力均为false，任何错误不返回部分DTO。

原版在active/profile禁发与inactive/override允许的原子切换中拼出从未成立的发送说明；本批先复现，再验证顺序、取消后有界锁回收和正常语义。该结果是管理诊断修复，不宣称发送接口越权；保留原租户管理锁，未取得总体性能无回归证明。

### B01-S：旧内容投影与owned Key决策

`CanReadOutboundContent` 不读取正文，复用sentContentFrom与submissionContentScope并校验observed job的tenant/id/zone/mailbox。User走companyReadTx的U SHARE→profile SHARE NOWAIT→M SHARE→存活item/范围EXISTS；Key走owner U SHARE→key SHARE NOWAIT→owner profile SHARE NOWAIT→M SHARE→相同EXISTS及key实际期限→Commit。无owner Key不取得内容权，属主admin角色不扩大Key范围。当前Key SHARE NOWAIT避免旧DeleteUser的key→user反向等待，但可能与last-used写竞争而产生409，未宣称零延迟代价。

旧job保留不能复活过期/清理的内容。正文相关HTTP生产Router实测差异已红绿对照；recipient投影移到ledger读取后决定BCC/诊断可见性。判断点在事务内，不是全HTTP响应或整页结果的线性化授权；原回执metadata、RetryAuthority和完整Key生命周期不由此认证。

### B01-T：回执主体与分页边界

`outboundPrincipalTx` 复用companyReadTx处理用户；Key按owner U SHARE（有owner时）→key SHARE NOWAIT→profile SHARE NOWAIT→读取callback→DB clock检查Key截止→Commit。S的内容Key事务也调用这一共享边界。ownerless Key仍保留自己的回执，但没有内容权。get/read与retry准入/write分别传明确scope，不继承Key属主管理员角色。

详情和列表由同一个readOutboundReceipts构造器生成：独立tenant/当前及原scope限制，owner/admin回执 OR 原live sent内容资格；单条SQL物化可见ID/时间并返回total及分页内容，按created_at/id稳定排序。只有当前页加载完整job，应用层随后纯投影不再逐行请求权限。它是语句数据快照，不是跨分页请求/网络响应/所有clock_timestamp求值同一瞬间；不能冒称整个P2最小回执或零性能成本。

attempts和ledger读取后执行最终回执准入及内容检查；失败无半页结果。GetAPIKey及两个Key列表使用host(last_used_ip)修正真实binary inet→字符串错误，未改数据库存储类型。RetryAuthority/ValidateJobAuthorization检查到RequeueOutboundJob之间仍分开，T只保障其初始write scope准入，不宣称最终重试已原子化。

## 6. P0-070收口前的剩余范围

B01-O已关闭K的局部PG运行缺口，B01-P又完成四个审计FK候选的实际修复和完整验收；两批证据分别保留，不再将它们列为未执行。Q已补当前普通用户的公司事务profile/override保护，R已独立验收目标用户说明，S已统一旧正文/诊断与owned Key内容资格，T已补回执当前主体与语句级整页读取；剩余重试/入队最终写入原子性、其他凭据/旧POST协议、普通收件历史期限、其他隐式FK、GC与多资源交叉。不能从物理GC保护直接推导新可读政策，完整070/080与G0仍未关闭。

截至B01-J，索引候选前后对照、7项索引与3项普通读取、54必跑及原预算全量均已执行通过；该候选运行阻塞解除。跨语句授权/内容到期、其他隐式FK与对象故障仍未齐，因此**070复选框保持未勾选，090仍受依赖限制**。080仍未冻结；G0/P1未开启。

下一轮从未覆盖的跨语句授权/内容截止及其余多资源路径继续，080按既定前置独立推进。索引、Reserve、Finish、入队的已验证局部修复不重复实现；不缩减070原验收标准，提交决策时的有效租约也不宣称数据库确认返回时仍未过期。

B01-C的历史观察见其验证说明；B01-D原始失败、生产修复、普通回归和提交映射见R5-B01-D-VALIDATION.md及其机器证据。原始失败、后续修正和最终通过分别保存，test name 不充当执行结果。
