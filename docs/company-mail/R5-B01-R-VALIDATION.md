# R5 B01-R：访问说明的目标用户快照

2026-09-29。**B01-R 已完成目标用户访问说明快照修复与真实 PostgreSQL 验收：相同最终测试原版34事件25失败/9通过；候选与O/P/Q一起30组115事件连续三轮通过，完整backend1041通过/0失败/1 browser skip，129必跑齐全，HTTP80响应通过。** 该局部验收不等于整个P0-070、P0-080或G0完成，父任务统计仍6/171。

## 1. 基线与版本

起点为 `862f09b280b7af376b50a523553a822de57c6909`，工作区干净；工作分支 `fix/company-access-explanation-snapshot-20260929`。生产变更位于 `company_access_explanation.go` 和既有 `company_members.go`；`permission_snapshot.go` 仅更新调用前提注释。无迁移、DTO、OpenAPI、依赖或前端变更。

最终程序被测树为 `dbdba853daebd6488f6a869bc045f058c6ec8f40`，643份源文件与工作区逐一校验SHA-256。该值是tree，不是commit；最终提交仅在其上追加本批报告、TODO、事务地图和证据，提交身份以Git回执为准。

## 2. 实际问题与证据分类

原版已在真实 PostgreSQL 复现一个不存在于任何已提交状态的权限说明：目标最初 active=true、profile 禁发；一次原子事务把目标停用并设为允许发送的个人覆盖。原子变更前后都不允许该目标发送，但说明先读旧 active，再读新 override，返回 `Active=true, CanSend=true`。`TestR5AccessExplanationNeverCombinesTargetGenerations` 通过表锁安排两次读取之间的变更，并用真实 NOWAIT 用户锁探针区分是否已保护快照。

这是管理界面的诊断一致性问题，不是证明了发信接口越权或真实邮件泄露。说明接口不是发信凭据，普通操作仍须自行检查当前资格。

其余原版失败分为两类：已有目标写者/繁忙 profile 未与说明建立有序边界；发生拒绝或取消时返回错误的同时附带非空 DTO。前者是快照协议的次序回归，不能把每项旧值读取都称为独立越权漏洞；后者是 store 返回值约束，原 HTTP handler 已检查错误，不能据此声称 DTO 已泄露给客户端。

## 3. 原位修复

保持原有 companyTx 管理授权及租户锁，随后依次取得目标用户 SHARE、目标 profile SHARE NOWAIT（仅有效普通用户）、mailbox SHARE，再进行邮箱判断。目标用户与当前管理员不同，前者保护角色、active、profile 分配和可选 override 的存在/缺失；复用 B01-Q 的稳定用户写锁与 `effectivePermissionSnapshot`，不增加另一套权限解释。

profile 正在修改时返回现有 409，不持有用户后等待可能反向请求用户的 profile 删除。行锁兼容及一致顺序依据 PostgreSQL 16 [显式锁文档](https://www.postgresql.org/docs/16/explicit-locking.html)；具体行为由本批真实数据库回归证明，不能仅凭规则宣称无死锁。

新增私有 `mailboxAccessWithSourceTx` 在原 `mailboxAccessTx` 实现上返回 owner/grant/none 来源。旧调用者仍使用原签名；说明使用同一次读取和 `authz.EvaluateMailboxAccess` 的结果生成来源与能力，删除原先管理员/目标两次 mailboxAccess 和额外 grant EXISTS。它不改变默认权限、管理员分支或空 grant 的来源含义。

任意事务错误返回 nil projection；停用目标保留 `inactive` 解释但四类内容能力均为 false。跨租户目标、非管理员和被冻结操作者仍拒绝。

## 4. 回归与夹具修正

新文件 `internal/store/postgres/r5_access_explanation_snapshot_test.go` 共七组顶层测试，加入现有 content-postgres-boundary 与 backend 两个必跑清单：

| 测试 | 证明范围 |
|---|---|
| WaitsForTargetWrites | 先有目标冻结、角色/profile分配、override创建/清除时，说明等待并读取提交后的结果。 |
| RejectsBusyTargetProfile | 租户/全局 profile 修改中，拒绝而不返回旧成功或部分结果。 |
| OrdersPermissionChanges | 说明已取得快照时，五种生产权限写命令在 mailbox-grant 查询等待期间不能穿越快照；删除与下一次说明正确收敛。 |
| NeverCombinesTargetGenerations | 原子变更前后都禁发，不得拼出从未成立的 Active/CanSend 组合。 |
| FailureReturnsNoProjection | 缺资源、跨公司、非管理员、冻结操作者、取消均为 error+nil。 |
| CancellationReleasesSnapshot | 请求取消后可在原12秒预算内重新取得租户/目标/profile锁，下一请求正常。 |
| PreservesCanonicalDecisions | owner、grant、空grant、无grant、管理员自查、停用、域名限制及模板专用均与既有语义一致。 |

最早夹具有未使用 import，编译失败已保留，不计为业务复现。第一候选在取消后的即时 NOWAIT 探针失败：客户端取消返回与 PostgreSQL 连接清理不同步。最终测试改为原12秒预算内有界重新取锁，不延长预算、不加 sleep，也不改变生产取消实现；验证的是资源最终被回收，而非取消返回瞬间的服务器同步承诺。完全相同的最终测试重新在原版和候选运行。

## 5. 最终结果

全部最终结果对应同一冻结源码树，保留原180秒包级测试预算：

| 验证 | 实际结果 |
|---|---|
| 同测试原版对照 | 34事件：25fail、9pass，0skip；包含19个具名失败子场景、4个父测试失败及2个独立失败，不是25个独立漏洞。 |
| PostgreSQL定向race | 30个顶层必跑、115事件，连续三轮均pass；既有O/P/Q回归一并执行。 |
| Go build/vet | readonly modules，在最终树通过。 |
| 完整backend race | 1042 started、1041 pass、0 fail、1 skip。 |
| backend门禁 | 129个必跑、0缺失；唯一允许skip是独立TestR3BrowserJourney。 |
| 实际HTTP/PG契约 | 80响应、65成功操作、66成功变体通过，两个实时DNS操作明确排除。 |
| Python/静态契约 | 168测试；16共享模型、33公司投影、67公司操作绑定通过。 |
| 缺DSN负例 | Go返回0但目标测试skip；content-postgres-boundary门禁返回1，正确拒绝未执行。 |
| 完整性 | 源文件、必跑清单、Go日志、OpenAPI与原始HTTP捕获的hash逐一核验。 |

HTTP验收使用生产Router、应用服务与PgStore，对象/Redis沿用合成适配器；不启动SMTP投递worker。前端套件、shipping浏览器、远端CI及规模性能未在本批执行。

## 6. 环境、兼容与剩余范围

使用既有 Go 1.25.7、Python 3.12 和原生 PostgreSQL 16 安装；仅新建本批0700临时集群及Unix socket，关闭TCP。Go依赖使用已验证缓存、GOPROXY=off和readonly模块，不读取生产DSN、环境文件或员工邮件。原始HTTP采集和完整服务器日志保持私有，发布归档只含非敏感执行证据。

该管理查询本来就持有租户排他锁，本批保留而非扩大到普通请求。目标用户/邮箱新增保护会使相应并发写等待；没有整站性能或跨进程、全部原生SQL写者安全保证。来源/能力共用一次判定不等于已经统一全库期限时钟；普通收件历史期限、旧 outbound/API Key 内容资格、GC/隐式外键及 P0-080 协议案例仍未验收收口。P1 编辑授权、revision/CAS/ABA 也没有在本批替代实现。

实际工具版本：Go 1.25.7 darwin/arm64、Python 3.12.11、PostgreSQL与psql/pg_dump 16.13。本批集群pid35606已正常停止（exit0），PID文件确认消失；临时源码、完整服务器日志和原始HTTP采集仅私有留存。未读取或变更生产数据，不运行生产迁移，不部署、合并或自动远端发布。

机器结果：[R5-B01-R-VALIDATION.json](evidence/R5-B01-R-VALIDATION.json)。[原始执行归档](evidence/R5-B01-R-LOGS.tar.gz)包含101成员、173865字节，SHA-256 `d3afba82b413fa4862d2e7a65721b53a644452c60d9e08c356855490c724aca4`；成员逐一核验，包含早期编译/候选失败、最终基线/三轮候选/完整backend/HTTP及清理结果，不含原始responses.json或完整服务器日志。
