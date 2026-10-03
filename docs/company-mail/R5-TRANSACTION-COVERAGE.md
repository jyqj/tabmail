# R5 全入口事务／FK／trigger／等待覆盖目录

> **当前19不继承18（2026-10-03）：** 新physical usageSOURCE合格不完整runtime，A失败/D1P/D2F/D3BSC未run；catalog15 170pureP无owned/fullref，旧18 partialcatalog/完整源码checkpoint不当19catalog或全caller事务覆盖。Stopdeadline/asyncTouchjoin/原retry拒409边界与新族freshAST/coverage具名待验。[当前层级](R5-CURRENT19-USAGE-DIAGNOSTIC-C18-STATUS-20261003.md)，未知不统一归零。

> **2026-10-03 边界补充：** 新旧actualcatalog结构观察不等完整reference或函数coverage。现schema18七集合缺typmod/triggerenabled/indexvalid-ready/seqcacheownership/普通functions，11是trigger_functions；C18-07 fullreference尚待actual采集签准，原02a585 bytes不改。[分类](evidence/R5-CATALOG18-HISTORICAL-PARTIAL-C18-BOUNDARY-20261003.json)另文件，不静态填默认或统一清unknown。

> **最新追加：** cb03/2c3ea72f实际schema18 catalog独立实采51tables/75FK/11triggers，GC/head限定15top21leaf race0；新生产family AST/分类/全coverage仍pending，不清全unknown。见[新18报告](R5-GC18-RECEIPT-OAS-REGISTRY-INTEGRATION-20261002.md)。

> **此前source实际catalog17（历史独立层）：** source1a15/closure77714、officialNew/Migrate/O_EXCL原194779字节、50tables/75FK/11triggers及3upgrade/catalog目标race通过，见[独立新17报告](R5-SCHEMA17-ACTUAL-CATALOG-20261002.md)与[原catalog](evidence/R5-TRANSACTION-DB-CATALOG-SCHEMA17-ACTUAL-20261002.json)。原15/16历史JSON和下方旧批source边界保，不currentAST/每函数全runtime，不17回贴18。


> 结构目录补充 [R5-TRANSACTIONS](R5-TRANSACTIONS.md)，不替代其事务图和运行验收。开始基线 `3893945`；当前目录包含 B01-X 主线程的 source-message NOWAIT 、ChangePassword tenant-first 及 queue nullable读取改动。未勾选 P0-070。

## 1. 结论与复验

- 精确 Go AST：49 个非 test postgres 文件、350 个函数、451 个 SQL 执行调用；126 个字面 SQL 写候选、141 个名称闭包写候选。此前 grep 的669含测试，不是实际生产函数数。
- 所有350函数均有 owner、入口/调用候选、源码哈希、函数专用操作序列或手工边界、锁/隐式FK范围、证据级别、具体风险及后续任务。49源码类型已人工复核；不将它等同350条运行验收。
- 14个迁移的Up源定义：77处REFERENCES声明、7个trigger；包含FK替换/DROP及第14迁移停写/表锁。声明计数不是最终生效FK数。
- 本工具仅验证目录新鲜和结构覆盖；`task_complete=false` / `runtime_verified=false` 是本结构产物级别，不裁决整个070。人工验收可引用本目录及其它动态证据独立作决定。

```sh
python3 scripts/check_r5_transactions.py
python3 -m unittest discover -s scripts/tests -p test_r5_transactions.py -v
```

Go抽取器使用标准库，无数据库连接；可用 `R5_GO=/absolute/path/to/go` 指定工具链。仅扫描 `internal/`、`cmd/` 的非test Go，不读取.env、配置凭据或生产数据。不修改产品源码。

机器目录：[R5-TRANSACTION-COVERAGE.json](evidence/R5-TRANSACTION-COVERAGE.json)。函数键为`file:receiver:name`；同名adapter/helper不会合并。`callers`明确是名称匹配候选，并非已解析接口dispatch或每条路由都可达的证明。

## 2. Evidence → Finding → Path

| Evidence | Finding | Path / 仍需审查 |
|---|---|---|
| E-TX-SOURCE：Go AST逐函数SQL表达式、锁片段、callback签名、函数SHA和49个文件SHA | 所有当前postgres函数及包级SQL常量所在文件纳入漂移门禁；纯值函数不假称有写事务 | 后续改写函数/新文件/新caller/SQL常量时目录拒绝；更新必须重新人工review，不提供盲目accept命令 |
| E-TX-FK：14迁移Up的FK/trigger/LOCK/DROP源定义及SHA | DB副作用不能只看Go SQL；源定义包含NO ACTION、SET NULL、CASCADE和替换历史 | 需按已执行迁移/catalog确认最终约束；source目录不能充当当前DB catalog |
| E-TX-CACHE：主线程cache-baseline-final和三个candidate JSONL，目录记录每份SHA | baseline17事件9pass8fail；候选17/17三轮。cache与worker/ingress无祖父T FK；三个物理DELETE命令双向确切40P01、victim rollback，修复message SHARE NOWAIT | 仅覆盖具体cache关系；其余message→M与M→message维护组合、event晚期T FK等仍source-only。本线程未执行DB |

E-TX-PASSWORD：`password-baseline.jsonl`为3事件1pass2fail；三个candidate均3pass0fail。完整Pwd/freeze原版真实40P01且victim回滚，父先user探针原版红，audit失败回滚原版正例绿。新定位tenant不授信→T KEY SHARE→重新核对tenant/expectedHash/active条件U更新→refresh/audit同事务。机器目录记录四份日志SHA；仅此关系为本批实证，不将其它user写涂绿。

E-TX-QUEUE-NULL：`queue-baseline.jsonl`四leaf+parent共5fail；三个candidate均5pass0fail。合法持久NULL未改为非NULL，queue六处读取只用COALESCE诊断字符串，不改writer、迁移、state或lease。直接运行范围为outbox/webhook claim、dead webhook与ingest列表；legacy ingest claim和webhook列表是同型源码复核，不把四leaf日志冒称六函数逐项运行证据。机器目录各条保留`nullableScan`变更来源/字段/fixture不变/证据范围；原本queue token/fanout/过期ack风险保持。

E-TX-CACHE原日志当前由主线程持有 `/tmp/tabmail-r5-b01x.8ht5RWf6/evidence`；最终交接须归档至项目证据并更新映射。它不是永久证据链接。

## 3. 多资源类型规则与例外

| 类型 | 规则 | 不能省略的例外 |
|---|---|---|
| 管理域 companyTx | T UPDATE→当前U SHARE→profile SHARE NOWAIT→资源/CAS→audit/outbox | ActivateEmployee自有Begin，普通定位tenant→T锁→invitation锁；旧CRUD不自动继承 |
| 引用域 companyReferencedTx | T KEY SHARE→当前U/profile→M/resource→audit/outbox | KEY SHARE不是tenant policy非键更新fence；正式enqueue/retry用T SHARE |
| companyReadTx | 当前U SHARE→profile SHARE NOWAIT→callback | 名称Read仍可写draft/attachment/document；没有统一T锁；FK按实际child，不递归猜祖父 |
| Enqueue | T→幂等→正式reader NOWAIT→sender U→A→排序quota→J/triggers/pins→D→期限 | trusted低层不validate；幂等已存在回放早提交，不写新job；缺M表级SHARE NOWAIT |
| 手工retry | T SHARE→依赖NOWAIT→J→实际generation重验→recipient NOWAIT→requeue/audit→期限 | 外层观察job不能代替实际锁定job；low-level Requeue是另一路径 |
| 普通消息／缓存／索引 | cache M SHARE→精确message SHARE NOWAIT→doc；worker message SHARE→job→doc→lease | physical DELETE先message及派生cascade/event再M count；不能把doc FK误说为T FK |
| 持久Ingress | I→target→T UPDATE→zone/M NOWAIT→usage/M/message/targets/audit→同clock终点fence | Fail/Hold/Finish是独立事务，仅初始lockIngress资格，不共享Deliver终点检查 |
| refresh | rotation family advisory→U SHARE→R；logout/GC family→R | user撤销U→R不加family；ChangePassword已先T KEY SHARE→U，Pwd/freeze本批实测通过；其余user竞争另验 |
| GC | 多句metadata自动提交；附件子事务T→A按id SKIP LOCKED→引用复查→delete/orphan同CTE | 保护prefix饥饿，跨tenant公平性、raw引用锁参与者不完整不能靠SKIP LOCKED认证 |
| 对象回调 | rawkey advisory→引用/ensureObject或del→Commit | 三条实际对象回调在SQL事务内；对象操作成功而COMMIT失败不能假设已撤销 |
| 队列/启动/系统旧写 | 精确记录单SQL或本函数事务；迁移由goose外部执行器承担 | outbox/webhook done/retry仅ID无token fence；旧DeleteUser Key→U反向；配置缓存不原子 |

### 3.1 外部callback真实caller

- `rawobject.Store.StoreMessage` → `CreateMessageWithQuota` → `ensure` → `Put`，事务内对象写。
- `rawobject.Store.StoreIngestJob` → `CreateIngestJob` → `ensure` → `Put`，事务内对象写。
- `rawobject.Release` → `ReleaseRawObjectIfUnreferenced` → `blob.Delete`，事务内对象删。
- `outbound.Service.createOutboundJob` → `CreateOutboundJobAuthorized` validator → requester/job authorization/current quota；当前只通过事务reader/policy，不是SMTP callback。
- `app/submissions.Service.RetryOutboundJob` → `RequeueOutboundJobAuthorized` validator → requester/job authorization；当前只通过事务reader/policy。
- `companyTxScope`、`permissionOverrideTx`、`outboundPrincipalTx`、`legacyKeyContentTx`的callback由当前调用者承担；工具不证明以后callback永远无I/O。

### 3.2 SQL组合而非字面量执行

`messageMutation`的六个action SQL、`applyOffboarding` stmt循环、`SweepCompanyMetadata` q循环、`enqueue` parentLock与各query predicate、template资格格式化均保留完整SQL表达式和函数body字串。包级constants由文件SHA保护。动态SQL不执行、不猜展开值；`dynamic-sql-review`明确纳入复核。

## 4. 全49源码类型人工目录

| 源文件 / state owner | 组 | 规则 / 例外 | 具体剩余风险 |
|---|---|---|
| `apikeys.go` / PgStore | CTX25 | 单语句Key增删/最后使用写；FK tenant/owner；scope与归属校验在外层，Touch不形成授权事务 | 旧Key增删与member冻结/owned-Key读取交叉，ID-only删除需外层tenant限制；期限/更新窗口未在本轮执行 |
| `audit.go` / PgStore | CTX26 | audit INSERT依赖T FK；monitor无tenant FK；动态monitor筛选仅读，不扩大事务 | 独立InsertAudit不自动与业务变更同提交；晚期父键锁与tenant-first写者需等待验证 |
| `company_access_explanation.go` / PgStore | TX24 | T UPDATE→当前actor SHARE/profile NOWAIT→目标user SHARE/profile NOWAIT→M SHARE→来源与资格同读 | 目标/当前profile交叉并发只保留已有R报告，非当前全图运行证明 |
| `company_bootstrap.go` / PgStore | CTX27 | Begin→tenant INSERT/plan FK→admin INSERT→audit INSERT→Commit | 启动/重复bootstrap唯一约束等待、Commit不明和迁移并发未本轮执行 |
| `company_console.go` / PgStore | CTX28 | companyReadTx→actor SHARE/profile SHARE NOWAIT→overview/audit读取；没有强制T锁 | 多语句overview跨资源快照不是REPEATABLE READ；读取helper不能推定没有隐式锁 |
| `company_mail.go` / PgStore | TX08 | 按分支TX08草稿、TX09删草稿、TX10附件、CTX29普通收件状态；actor SHARE→M SHARE→A SHARE/CAS；审计写使用T KEY SHARE包装 | 草稿附件顺序依输入；普通restore清除expires/purge与sent不同，属于070/080剩余政策；trusted TrashCompanyMessages无当前actor重载 |
| `company_members.go` / PgStore | TX01 | companyTxScope三种T模式：UPDATE/KEY SHARE/无T→actor SHARE→profile SHARE NOWAIT→callback→Commit；grant/移交先mailbox revision；Activate独立事务 | Activate普通定位tenant→T→invitation锁，profile/users/M新增含隐式FK；邀请expires使用now事务时间；验证末次期限及所有交叉等待未全执行 |
| `company_message_read.go` / PgStore | CTX28 | companyReadTx→actor/profile→M SHARE→message读取；对象解析由应用层在外部 | 内容来源期限与多语句资格依调用方重验；读取入口不自动覆盖内容释放全过程 |
| `company_metrics.go` / PgStore | CTX28 | 只读metrics动态聚合，不开启新的持久写事务 | metrics访问授权依上层；聚合多资源一致性不是并发验收 |
| `company_ops.go` / PgStore | TX20 | recoveryReferencedActor T KEY SHARE→actor SHARE→I/J NOWAIT；Sweep前4个DELETE各自提交，附件T UPDATE→A按id SKIP LOCKED→引用复检→delete/orphan CTE | InspectRecoveryReceipt虽读名称仍插audit；GC保护前缀饥饿及单次/跨轮扫描公平性未闭合；运维与worker等待需全族验证 |
| `company_templates.go` / PgStore | CTX30 | Save/retire/revoke T KEY SHARE→actor/profile→template CAS或version UPDATE；publish/grant T UPDATE；FK版本→模板、grant→模板/M/U；immutable版本trigger保留 | 版本revoke先version→template，publish先template→version INSERT；不同版本/反向等待及旧模板资格跨语句需专项验证 |
| `domain_delete.go` / PgStore | TX23 | T UPDATE→zone UPDATE→资产引用检查→DELETE/FK NO ACTION→audit；不自己重读actor | 外层当前actor与域名删除写的授权窗口及zone/ingress/mailbox交叉等待待全图验证 |
| `draft_query.go` / PgStore | TX08 | companyReadTx分页/计数、mailboxAccessBatch；纯读取但参加actor/profile锁 | 分页资格/模板版本跨语句一致性需批量快照验证，不以helper名称替代 |
| `employee_disposition.go` / PgStore | TX07 | T UPDATE→actor SHARE/profile→plan UPDATE(执行)→target UPDATE→successor SHARE→J按id UPDATE→A/D/refresh/Key/grants/U/M→audit | 计划deadline是Go时间早期判定；多用户successor与最后管理员、历史FK及其它maintenance交叉未全验 |
| `ingress.go` / PgStore | TX14 | claim单SQL SKIP LOCKED/token；delivery I UPDATE→target UPDATE→T UPDATE→zone/M SHARE NOWAIT→usage W→M count W→message triggers→audit→最终lease；fail/hold/finish独立I→target事务 | CreateIngress原件在应用外层，固定目标并无事务内对象回调；fail/hold/finish锁后lease复核与等待的细分覆盖待验 |
| `ingress_destination.go` / PgStore | TX14 | zone SHARE NOWAIT→M SHARE NOWAIT→精确tenant/zone/address/expires fence | 目的地缺行不存在可锁行；expires最终跨语句clock复验与direct维护SQL交叉需验 |
| `mail_content.go` / PgStore | TX22 | complete message SHARE→index job UPDATE→clock/token检查→document UPSERT→最终lease；fail job→clock；claim两条自动提交；retry T→actor→job SKIP LOCKED→audit | SaveParsedMessage actor/profile→M SHARE→精确source message SHARE NOWAIT→document UPSERT，不新增T锁；cache/物理删除已由主线程17事件三轮验证，其余派生写不等于index completion；retry与complete/source修改触发器交叉、claim清理与领取非整体原子需验证 |
| `mail_conversation.go` / PgStore | CTX28 | companyReadTx→actor/profile→mailboxAccess读取→conversation动态筛选 | conversation单源资格与分页/结果deadline可能跨语句；未当独立写路径 |
| `mailbox_grants.go` / PgStore | TX24 | SetMailboxGrant旧单SQLUPSERT，FK T/M/U/granted_by；没有companyTx或mailbox revision CAS | 旧直接grant写与公司M SHARE授权fence不自动统一；需证明实际caller授权及撤权竞争 |
| `mailbox_predicate.go` / PgStore | CTX28 | 产生静态SQL条件片段/参数，非执行器、无自主持久写 | 动态条件的正确拼接由调用函数负责；语法清点不证明ACL语义 |
| `mailbox_revision.go` / PgStore | TX03 | claimMailboxRevision条件W；lockMailboxAuthorization M SHARE；transferOwnedMailboxes M按id UPDATE→CAS→owner FK | helper依外围tenant/actor锁；接收user FK与多邮箱更新交叉需要caller级验证 |
| `mailboxes.go` / PgStore | CTX31 | Create/DeleteMailbox单SQL；create FK tenant/zone/route/owner；delete触发cascade messages→events/index及grant/state，可能晚期T FK | 旧mailbox delete与T-first管理/消息插入/GC可能隐式锁环；当前catalog与DDL NO ACTION删除拒绝需运行确认 |
| `member_guard.go` / PgStore | TX04 | T UPDATE→current actor SHARE→target UPDATE→guard/profile读取→U/refresh/Key W→audit；delete含FK cascades | target-profile读取与profile删除SET NULL、self target锁提升、所有历史资产FK拒绝未由此静态目录关闭 |
| `messages.go` / PgStore | TX21 | Create quota rawkey advisory→ensureObject回调→M count W→messages INSERT/triggers；delete/purge/expiry messages DELETE/triggers→M count W；release rawkey advisory→引用SELECT→del回调→Commit | 消息插入M→message与删除message→M相反；message event晚期T FK；不同消息同行M交叉等待待验证。rawkey锁协议不能证明附件原件写者都参加；对象回调可能阻塞/提交不明 |
| `migrate.go` / PgStore | CTX32 | New→Migrate；database/sql→goose UpContext执行全部Up，migration元数据/DDL锁；版本清单与goose baseline转换 | Go AST不能解析动态goose执行等价性；FK目录为源定义非当前runtime catalog，在线DDL需要停写/超时/备份协议 |
| `outbound.go` / PgStore | TX11 | 正式enqueue T SHARE(trusted KEY SHARE)→幂等advisory→validate reader NOWAIT/缺行table SHARE NOWAIT→sender SHARE→A SHARE→quota keys排序→J INSERT→BEFORE sender+AFTER archive→recipient/pins→audit→D消费→deadline→Commit | 幂等回放早返回保留协议；低层trusted变体不走validate；其它outbound claim/attempt/suppression单写并非enqueue；quota等待日界策略需独立覆盖 |
| `outbound_content.go` / PgStore | CTX28 | 旧content委托outboundPrincipalTx及sent item/M资格；legacyKeyContentTx callback在SQL事务内 | read名字不自动纯函数；跨对象I/O重验需外层companymail。callback不具备静态副作用隔离证明 |
| `outbound_principal.go` / PgStore | CTX28 | user companyReadTx；Key分支owner U SHARE→Key SHARE NOWAIT→profile NOWAIT→read callback→最终Key deadline→Commit | 没有统一T父锁；仅已有当前授权读取报告，callback扩展写入将新增FK风险，校验器只能捕捉源码变化 |
| `outbound_receipts.go` / PgStore | CTX28 | outboundPrincipalTx→可见ID/total/page或attempts/recipient读取→终点资格/clock；receipt动态SQL | 凭据/owner与页内容不混代依现有实现；读取多语句不会自动变repeatable snapshot |
| `outbound_recipients.go` / PgStore | TX12 | begin J UPDATE锁/token/clock→recipient uncertain W→J inflight；complete J条件W→recipient终态W；SMTP在二者之间 | 等待后lease及收件集合完整性与legacy migration结果须独立运行；accepted不可被重试覆盖 |
| `outbound_retry.go` / PgStore | CTX33 | T SHARE→validate NOWAIT依赖→J UPDATE→实际generation再次validate→recipient SHARE NOWAIT→requeue/audit→最终reader deadlines→Commit | validator是app回调非DB外调用；未静态证明所有未来validator无网络I/O；可信low-level Requeue不参加当前授权 |
| `outbound_retry_reader.go` / PgStore | CTX33 | 事务内reader U/Key/profile/zone/M/grant/template SHARE NOWAIT；缺精确M/identity另走table SHARE NOWAIT；累计Key/M deadline | NOWAIT限制等待非吞错误；表级保护影响其它写者；不存在行的身份fence必须和reader分支合验 |
| `outbound_transition.go` / PgStore | TX12 | finalize单UPDATE含token/state/clock lease；in_flight决定uncertain与终态；require RowsAffected | SMTP在前后DB事务之间，无分布式原子性；token-fence不证明下游接受/未接受 |
| `permission_snapshot.go` / PgStore | TX24 | snapshot持user后profile SHARE NOWAIT；override事务先user NO KEY UPDATE→callback UPSERT/DELETE→Commit | NOWAIT fail-closed保护profile反向等待，但旧profile更新/删除与current用户保护覆盖要全族测试 |
| `permissions.go` / PgStore | TX24 | profile旧单SQL CRUD，delete可SET NULL user FK；override走permissionOverrideTx；effectivePermission查询为独立端口 | 旧handler多次验证随后写无revision CAS；profile租户/system与字段意图未在070子包解决 |
| `plans.go` / PgStore | CTX34 | plan CRUD单SQL；delete受tenants.plan FK NO ACTION | 计划修改与有效quota读取之间未同事务固定；待产品policy/CAS验证 |
| `postgres.go` / PgStore | CTX32 | pool.New/Ping→Migrate，连接池close；hash helpers无自主写 | pool MaxConns/等待/ctx边界及迁移外部调用属于启动责任，不当本轮DB运行证据 |
| `queue.go` / PgStore | CTX35 | 旧outbox/webhook/ingest claim单SQL SKIP LOCKED；createWebhook多row事务/event FK；createIngest rawkey advisory→ensureObject→I INSERT；done/retry按ID单UPDATE | outbox/webhook旧done/retry无claim token/lease fence；过期worker可能写当前claim，待P6验证。网络webhook/对象I/O均在worker或显式回调，不推定全项目无事务I/O |
| `refresh_rotation.go` / PgStore | TX16 | family探测→advisory→user SHARE→R UPDATE→撤销/后代；logout/GC family→R，无user-first family等待 | family gc/用户删除/改密交叉依历史回归，Commit不明与客户端重放不能靠inventory证明 |
| `send_identities.go` / PgStore | CTX36 | identity CRUD单SQL，FK tenant/zone/mailbox；zone bulk verified UPDATE | 发送exact identity NOWAIT/table SHARE与维护写者关系需实测；业务校验在上层 |
| `sent_archive.go` / PgStore | TX19 | T KEY SHARE→actor/profile→M SHARE→sent item revision UPDATE→实际deadline→W→audit/event→原deadline重验 | 保留原purge与普通收件restore不同；sent asset GC/cascade与item mutation交叉需要全图运行 |
| `sent_archive_deadline.go` / PgStore | TX19 | checkSentItemDeadline使用DB clock_timestamp；读取mailbox/原expires/purge并比较 | 函数仅检查时点不是永久授权；调用者负责锁与原始期限保留 |
| `settings.go` / PgStore | CTX37 | system setting UPSERT单SQL，无tenant FK | settings service跨key多次UPSERT非整体事务；SMTP/config缓存刷新边界待验证 |
| `smtp_policy.go` / PgStore | CTX37 | 唯一smtp_policy行UPSERT，nonNil辅助 | DB策略与SMTP in-memory reload不是同提交；上层管理授权与cache时效待验证 |
| `submissions.go` / PgStore | CTX28 | companyReadTx→当前主体→动态submissionScope/contentScope→回执/正文/附件/收件读取 | 列表total/page和资格多语句、Key使用其它legacy端口不可混为统一snapshot；仅静态入口覆盖 |
| `tenants.go` / PgStore | CTX38 | tenant create/delete单SQL、override UPSERT；plan FK与tenant cascade扇出 | DeleteTenant带cascade user/mailbox/message/job/Key等，不等同只锁T；系统管理授权和撤权并发待P1/070 |
| `users.go` / PgStore | TX18 | ChangePassword定位tenant→T KEY SHARE→U条件W→refresh W→audit；RevokeUser U UPDATE→R；旧DeleteUser Key DELETE→U DELETE；旧CRUD/refresh创建单SQL | ChangePassword的Pwd/freeze父键关系已由本批3事件三轮验证；其它tenant/user删除及旧DeleteUser Key→U与冻结U→Key相反仍需独立运行；不覆盖提交不明。 |
| `webhook_endpoints.go` / PgStore | CTX35 | endpoint CRUD单SQL，FK tenant/created_by；delivery来自outbox批事务 | endpoint配置与已建立delivery快照外层边界，租户/用户外层授权和删除竞争待验证 |
| `zones.go` / PgStore | TX23 | zone Create/Update单SQL，tenant/owner/parent FK；DeleteZone在domain_delete.go | 旧zone UPDATE无company当前actor fence；parent-chain与身份verified/cache更新需要实际caller验证 |

## 5. 当前函数专用多资源边界

以下是独立人工断言，不套整文件顺序。其余函数精确分支SQL/锁/helper顺序在机器entry的`assertions`和`lock_fk_wait_fence.local_operations`；后者按词法位置，**不是把所有分支压成一个必经运行序列**。

| 函数 / 组 | 函数专用锁、FK、等待、fence范围 | 证据层 |
|---|---|---|
| `BootstrapCompanyAdmin` / CTX27 | Begin→super tenant INSERT/plan FK→super_admin user INSERT→audit INSERT→Commit；空用户表检测并发需独立启动测试。 | source-only |
| `DeleteMailDraft` / TX09 | companyReadTx actor/profile→精确tenant/user/id/revision且未sealed D DELETE→Commit；不删creation receipt，也不显式锁M。 | source-only |
| `FinishMailAttachment` / TX10 | companyReadTx actor/profile→预定位M→M SHARE/current send→精确A UPDATE锁→state/expires clock条件A ready W→Commit；不是对象Put。 | source-only |
| `MutateWorkMessage` / CTX29 | companyReferencedTx T KEY SHARE→current actor/profile→M SHARE/current rights→organize/owner seen source NOKEY UPDATE NOWAIT，或nonowner sparse source KEY SHARE NOWAIT→原W/state→audit/outbox→Commit；latebusy409同tx rollback，保留不同state owners。 | actual-batch-linked-relationship（Y精确子集；统一full待freeze） |
| `ReserveMailAttachment` / TX10 | companyReadTx actor/profile→M SHARE/send资格→attachment-budget tenant:user advisory→sum→A上传占位INSERT/FK M/U→Commit；真正对象上传在外层。 | source-only |
| `SaveMailDraft` / TX08 | companyReadTx actor/profile→M SHARE/当前send资格→逐A SHARE→新建receipt INSERT或现D revision CAS→模板版本填充→Commit；receipt FK到user，不直接到T。 | source-only |
| `TrashCompanyMessages` / CTX29 | 独立Begin→T UPDATE→work M UPDATE→single id的messageMutation NOKEY NOWAIT，或bulk MATERIALIZED精确tenant/mailbox/deleted_atNULL按id NOKEY NOWAIT，仅locked ids+复核原predicate更新30day→audit/outbox→Commit；trusted actor标签不自动重读actor，bulk旧trash不改。 | actual-batch-linked-relationship（Y精确子集；统一full待freeze） |
| `ActivateEmployee` / TX01 | 独立Begin→普通invitation定位tenant（不授信）→T UPDATE→hash+tenant绑定invitation UPDATE锁/原expires_at→锁后DB clock期限→当前sponsor/domain/profile资格→U/M/consumption→audit/outbox→原primary/zone/domain verified+mx SHARE NOWAIT→最终原expires DBclock→Commit；late无资格400/busy409均整体rollback，不新增T锁。 | actual-batch-linked-relationship（Y最终同14事件三轮） |
| `ConfigureCompany` / TX01 | companyTx T UPDATE→actor/profile→primary zone verified/mx资格读→settings revision比较及INSERT/UPDATE CAS→tenant mail_send_policy W→audit/outbox→Commit；提交后另读返回设置。 | source-only |
| `ConvertSharedMailbox` / TX03 | companyTx T UPDATE→actor/profile→M UPDATE锁/current owner/revision→mailbox CAS→M类型/期限/password清除W→活message expires清除W/event trigger→audit/outbox→Commit。 | source-only |
| `CreateWorkMailbox` / TX01 | companyTx T UPDATE→actor/profile→公司domain及owner active资格→M INSERT/FK zone/user→audit/outbox→重读新M→Commit。 | source-only |
| `InviteEmployee` / TX01 | T UPDATE→currentactor U SHARE→domain/address检查→可选exactP id+tenant/global KEY SHARE NOWAIT→绑定lockedP INSERT→required audit/outbox→Commit；missing/foreign400、busy409，P持锁到commit。 | AF最终ccf同test原two40P01/0660三轮5pass＋AI冷回归 |
| `OffboardEmployee` / TX01 | companyTx T UPDATE→actor/profile→offboardingTarget U/继任U/J锁→applyOffboarding→Commit；没有preview plan，也不同于新ExecuteOffboarding的fingerprint协议。 | source-only |
| `RevokeEmployeeInvitation` / TX01 | companyTx T UPDATE→actor/profile→未consumed/revoked invitation条件UPDATE→RowsAffected冲突判定→audit/outbox→Commit。 | source-only |
| `SetWorkGrant` / TX02 | companyTx T UPDATE→actor/profile→M访问/owner层级资格→目标activeCompanyUser→mailbox revision CAS W→grant DELETE或UPSERT/FK M/U/granted_by→audit/outbox→Commit。 | source-only |
| `SetWorkMailboxSendPolicy` / TX02 | companyTx T UPDATE→actor/profile→M当前访问/owner管理资格→mailbox revision CAS→send_policy W→audit/outbox→Commit。 | source-only |
| `TransferWorkMailbox` / TX03 | companyTx T UPDATE→actor/profile→M UPDATE锁→owner管理资格/新owner active检查→mailbox revision CAS→owner W/user FK→新owner grant DELETE→audit/outbox→Commit。 | source-only |
| `companyTxScope` / TX01 | Begin→按lock枚举T UPDATE/KEY SHARE/无T→currentMemberActor U SHARE→非admin有效profile SHARE NOWAIT→f callback→Commit；error mapping不把40P01变成功。 | source-only |
| `InspectRecoveryReceipt` / TX20 | Begin→recoveryReferencedActor T KEY SHARE/当前actor SHARE→receipt+targets读→companyAudit/outbox写→Commit；读名称仍是多资源审计事务。 | source-only |
| `ReconcileOutbound` / TX13 | Begin→recoveryReferencedActor T KEY SHARE/当前actor SHARE→J UPDATE NOWAIT→state/updated_at/ledger检查→recipient及J W→必要audit/outbox→Commit。 | source-only |
| `RetryRecoveryReceipt` / TX15 | Begin→recoveryReferencedActor T KEY SHARE/当前actor SHARE→I UPDATE NOWAIT→版本/state/hash核对→目标SHARE NOWAIT与destination fence→精确target reset/I pending W→audit→Commit。 | source-only |
| `SweepCompanyMetadata` / TX20 | sent item DELETE→sent asset DELETE→runtime DELETE→event DELETE四句pool.Exec分别提交；随后按tenant调用sweepCompanyAttachments；失败不回滚已提交前缀。 | source-only |
| `sweepCompanyAttachments` / TX20 | Begin→T UPDATE→按id LIMIT100 A UPDATE SKIP LOCKED→draft JSON/outbound pin/sent pin引用复检→A DELETE和orphan INSERT同CTE→Commit；保护prefix可能反复占据候选窗口。 | source-only |
| `PublishMailTemplate` / CTX30 | companyTx T UPDATE→actor/profile→template UPDATE锁→当前revision/draft validated→MAX(version)+1 version INSERT/FK template→template revision W→audit/outbox→Commit。 | source-only |
| `RevokeMailTemplateVersion` / CTX30 | companyReferencedTx T KEY SHARE→actor/profile→指定version UPDATE锁→version revoked_at W/immutable trigger→template revision CAS W→audit/outbox→Commit；是version→template，不压成publish顺序。 | source-only |
| `SaveMailTemplate` / CTX30 | companyReferencedTx T KEY SHARE→actor/profile→new template INSERT/FK T或观察revision条件template UPDATE→audit/outbox→Commit；new唯一name可能等待。 | source-only |
| `SetMailTemplateRetired` / CTX30 | companyReferencedTx T KEY SHARE→actor/profile→观察revision条件template retired/revision UPDATE→audit/outbox→Commit。 | source-only |
| `SetTemplateGrant` / CTX30 | companyTx T UPDATE→actor/profile→template版本/目标user/M资格检查→grant INSERT或DELETE/FK template/M/U→audit/outbox→Commit。 | source-only |
| `DeleteZone` / TX23 | Begin→T UPDATE→zone UPDATE→资产引用检查→zone DELETE/NO ACTION FK→optional audit→Commit；身份校验在调用层，不自己重载actor。 | source-only |
| `ExecuteOffboarding` / TX07 | companyTx T UPDATE→actor/profile→plan UPDATE→早期Go时间expires校验→target U UPDATE/successor SHARE/J按id UPDATE→重新snapshot与fingerprint比较→applyOffboarding→plan executed W→Commit。 | source-only |
| `PreviewOffboarding` / TX06 | companyTx T UPDATE→actor/profile→target U UPDATE→successor U SHARE→J按id UPDATE→资产snapshot/fingerprint→plan INSERT/target+successor FK→audit/outbox→Commit。 | source-only |
| `applyOffboarding` / TX07 | 调用者tx：transfer_owned先A owner W→D owner/revision W，seal/discard另分支；cancel已知非在途J→R撤销→owned Key/grants/template grants DELETE→target U inactive/session W→按id M转交/CAS→audit/outbox；依赖调用方预持T/target/successor/J。 | source-only |
| `CreateIngress` / TX14 | Begin→rawkey advisory→recovery_managed ingest_jobs INSERT→固定ingest_recipient_outcomes逐目标INSERT→Commit；对象已由外层持久化，没有ensureObject参数。 | source-only |
| `DeliverIngress` / TX14 | Begin→I UPDATE/claim fence→target UPDATE→T UPDATE→zone SHARE NOWAIT→M SHARE NOWAIT→usage seed/increment→M count W→message INSERT/triggers→target delivered W→audit/outbox→DB同一clock最终lease与destination deadline→Commit。 | source-only |
| `FailIngressTarget` / TX14 | Begin→lockIngress(I UPDATE/初始token/lease)→pending target attempts/error W→Commit；此函数没有末次lease重验。 | source-only |
| `FinishIngress` / TX14 | Begin→lockIngress(I UPDATE/初始token/lease)→target状态汇总→必要pending→held W→I状态/token清除W→Commit；不是投递message/quota事务。 | source-only |
| `HoldIngressTarget` / TX14 | Begin→lockIngress(I UPDATE/初始token/lease)→pending target held/error W→Commit；此函数没有末次lease重验。 | source-only |
| `ClaimMailIndexJobs` / TX22 | pool.Exec耗尽租约job标failed先独立提交；随后pool.Query SKIP LOCKED领取job/token/90s lease另一statement提交；不是两句整体事务。 | source-only |
| `CompleteMailIndexJob` / TX22 | Begin→精确source message SHARE→index job UPDATE→锁后clock/lease/source重验→saveParsed→最终token/state/lease条件job UPDATE→Commit；document FK仅message。 | actual-batch-linked-relationship |
| `FailMailIndexJob` / TX22 | Begin→index job UPDATE→锁后clock/lease/token检查→条件job失败/待重试UPDATE→Commit；没有mailbox/T授权事务。 | source-only |
| `RetryFailedMailIndex` / TX22 | companyTx(T UPDATE→actor/profile)→job按message id SKIP LOCKED→源message rawkey JOIN→reset attempts/token/lease→audit/outbox→Commit。 | source-only |
| `SaveParsedMessage` / TX22 | companyReadTx→当前actor U SHARE/profile SHARE NOWAIT→M SHARE→当前CanRead→精确tenant/mailbox/id/rawkey message SHARE NOWAIT→saveParsed document UPSERT/FK(message)→Commit；不存在祖父T FK，不新增T锁；55P03由companyTxScope映射Conflict。 | actual-batch-linked-relationship |
| `DeleteUserGuarded` / TX05 | Begin→T UPDATE→current actor SHARE→target UPDATE→成员/owner移交拒绝规则→Key DELETE→U DELETE/FK→member audit→Commit。 | source-only |
| `UpdateUserGuarded` / TX04 | Begin→T UPDATE→current actor SHARE→target UPDATE→角色/最后管理员/profile核对→refresh/Key撤销→target U W→member audit→Commit。 | source-only |
| `CreateMessage` / TX21 | Begin→M count W→message INSERT（tenant/mailbox/zone FK，event/index triggers）→Commit；与quota/rawobject回调变体不同。 | source-only |
| `CreateMessageWithQuota` / TX21 | Begin→rawkey advisory→ensureObject对象回调→M有条件count W→message INSERT/FK/event/index triggers→Commit；quota拒绝回滚，无新的tenant先锁。 | source-only |
| `DeleteExpiredMessagesReturningKeys` / TX21 | Begin→有界expiry/trash候选CTE→messages DELETE/cascades/event trigger→按分组M count扣减W→Commit→返回rawkeys供外层回收；不在本函数删除对象；group结果未显式排序。 | actual-batch-linked-relationship |
| `DeleteMessage` / TX21 | Begin→messages DELETE RETURNING mailbox_id（message row、cascade documents/index/userstate、event trigger含T FK）→M message_count W→Commit；raw对象删除不在本函数。 | actual-batch-linked-relationship |
| `PurgeMailbox` / TX21 | Begin→指定M下messages DELETE（cascade documents/index/userstate/event trigger）→M message_count=0 W→Commit；M不是先锁，反向cache候选现source NOWAIT拒绝。 | actual-batch-linked-relationship |
| `ReleaseRawObjectIfUnreferenced` / TX21 | Begin→rawkey advisory→message+attachment+ingest引用普通SELECT→零引用时del对象回调→Commit；没有metadata DELETE；del后commit失败可能对象已删。 | source-only |
| `DeleteSuppressionAudited` / TX11 | Begin→suppression UPDATE锁→DELETE→独立audit INSERT/T FK→Commit；没有companyTx/currentMemberActor。 | source-only |
| `enqueueOutboundJobValidated` / TX11 | Begin→T SHARE正式/KEY SHARE trusted→idempotency advisory与早回放→正式validator/缺行table SHARE NOWAIT→sender U SHARE→A SHARE→排序quota advisories+counts→J INSERT/BEFORE user/AFTER archive→recipient/pins/sent-pin trigger→audit→精确D消费→final deadlines→Commit。 | source-only |
| `outboundPrincipalTx` / CTX28 | user分支companyReadTx→read callback；Key分支Begin→owner U SHARE→Key SHARE NOWAIT+identity/scope→profile SHARE NOWAIT→read callback→最终Key clock→Commit；无T祖父锁假设。 | source-only |
| `BeginOutboundRecipient` / TX12 | Begin→J UPDATE锁/token/state/lease/inflight fence→指定recipient uncertain W/attempt++→J inflight条件W→Commit；SMTP在函数返回后。 | source-only |
| `CompleteOutboundRecipient` / TX12 | Begin→J token/state/lease/domain条件UPDATE→指定recipient结果W→Commit；是SMTP之后checkpoint，不证明SMTP下游原子性。 | source-only |
| `RequeueOutboundJobAuthorized` / CTX33 | Begin→T SHARE→缺M分支table SHARE NOWAIT→validate observed依赖NOWAIT→J UPDATE→validate locked generation NOWAIT→recipient SHARE NOWAIT/拒uncertain→J requeue+audit/outbox→final reader deadline clock→Commit。 | source-only |
| `permissionOverrideTx` / TX24 | Begin→指定user NO KEY UPDATE（missingOK分支）→write callback→Commit；callback当前仅override UPSERT/DELETE。 | source-only |
| `CreateIngestJob` / TX21 | Begin→rawkey advisory→ensureObject对象回调→legacy ingest_jobs INSERT→Commit；不是CreateIngress固定目标事务。 | source-only |
| `CreateWebhookDeliveries` / CTX35 | 复制caller URL slice并sort.Strings→Begin→按稳定exactURL序获取unique(event_id,url) INSERT/FK、ON CONFLICT DO NOTHING→wholeTx Commit；不改input/ACK/外部发送。 | AE346535同test原two40P01/3102三轮5pass＋AI冷回归 |
| `RevokeRefreshTokenByHash` / TX17 | Begin→lockRefreshFamily→族R未revoked UPDATE→Commit；不锁U。 | source-only |
| `RotateRefreshToken` / TX16 | Begin→普通family查找→family advisory→U SHARE/active→旧R UPDATE→重放族撤销或单token撤销+后代INSERT→Commit。 | source-only |
| `deleteExpiredRefreshFamily` / TX17 | Begin→族advisory→全族无未到期者条件DELETE→Commit；不锁U。 | source-only |
| `MutateArchivedMail` / TX19 | companyReferencedTx T KEY SHARE→actor/profile→M SHARE/current access→sent item精确revision UPDATE锁→原expires/purge+M期限 DB clock→action W→audit/outbox/event→原deadline再次检查→Commit。 | source-only |
| `ChangePasswordAtomic` / TX18 | Begin→只定位user.tenant_id（不授信）→T KEY SHARE→user条件UPDATE重新核对tenant+expectedHash+active/session版本→R撤销W→audit INSERT→Commit；父键先于user，不使用T排他锁；保持审计失败全回滚。 | actual-batch-linked-relationship |
| `DeleteUser` / TX18 | 旧Begin→owned Key DELETE→user DELETE及FK/cascades→Commit；没有T/actor授权锁，不等同DeleteUserGuarded。 | source-only |
| `RevokeUserRefreshTokens` / TX18 | Begin→U UPDATE锁→该user未revoked R UPDATE→Commit；没有family advisory。 | source-only |

## 6. 全写候选入口索引

126个直接字面写候选加15个调用闭包候选。closure按name保守扩张，可能包含adapter同名误关联；本表不替代源码/caller人工确认。Goose动态DDL执行另在CTX32，不被伪装成普通字面写函数。

| 函数 | 文件 / 组 | 入口候选或调用者责任 |
|---|---|---|
| `CreateAPIKey` | `apikeys.go` / CTX25 | internal/api/handlers/admin.go:CreateAPIKey；internal/api/handlers/admin.go:UserCreateAPIKey；internal/app/admin/service.go:CreateAPIKey |
| `DeleteAPIKey` | `apikeys.go` / CTX25 | internal/api/handlers/admin.go:DeleteAPIKey；internal/app/admin/service.go:DeleteAPIKey；internal/app/admin/service.go:DeleteAPIKeyForTenant |
| `TouchAPIKey` | `apikeys.go` / CTX25 | internal/api/middleware/apikey_touch.go:touchAPIKeyAsync；internal/api/middleware/auth_cache.go:TouchAPIKey |
| `CreateMonitorEvent` | `audit.go` / CTX26 | internal/realtime/hub.go:Publish |
| `InsertAudit` | `audit.go` / CTX26 | internal/api/handlers/audit.go:insertAudit；internal/app/admin/service.go:CreateAPIKey；internal/app/admin/service.go:CreatePlan |
| `BootstrapCompanyAdmin` | `company_bootstrap.go` / CTX27 | cmd/tabmail/main.go:bootstrapAdmin |
| `DeleteMailDraft` | `company_mail.go` / TX09 | internal/app/drafts/service.go:Delete |
| `FinishMailAttachment` | `company_mail.go` / TX10 | internal/app/companymail/service.go:UploadAttachment |
| `MutateWorkMessage` | `company_mail.go` / CTX29 | internal/api/handlers/company_mail.go:MessageAction |
| `ReserveMailAttachment` | `company_mail.go` / TX10 | internal/app/companymail/service.go:UploadAttachment |
| `SaveMailDraft` | `company_mail.go` / TX08 | internal/app/drafts/service.go:Save |
| `TrashCompanyMessages` | `company_mail.go` / CTX29 | internal/app/messages/service.go:DeleteMessage；internal/app/messages/service.go:PurgeMailbox |
| `writePersonalMessageState` | `company_mail.go` / CTX29 | 内部helper/无外部名称候选；核查当前caller，不假称公开HTTP入口 |
| `messageMutation` | `company_mail.go` / CTX29 | 内部helper/无外部名称候选；核查当前caller，不假称公开HTTP入口 |
| `messageOutbox` | `company_mail.go` / TX08 | 内部helper/无外部名称候选；核查当前caller，不假称公开HTTP入口 |
| `ActivateEmployee` | `company_members.go` / TX01 | internal/api/handlers/company_setup.go:Activate |
| `ConfigureCompany` | `company_members.go` / TX01 | internal/api/handlers/company_setup.go:Configure |
| `ConvertSharedMailbox` | `company_members.go` / TX03 | internal/api/handlers/company_mailboxes.go:ConvertShared |
| `CreateWorkMailbox` | `company_members.go` / TX01 | internal/api/handlers/company_mailboxes.go:CreateMailbox |
| `InviteEmployee` | `company_members.go` / TX01 | internal/api/handlers/company_setup.go:Invite |
| `OffboardEmployee` | `company_members.go` / TX01 | 内部helper/无外部名称候选；核查当前caller，不假称公开HTTP入口 |
| `RevokeEmployeeInvitation` | `company_members.go` / TX01 | internal/api/handlers/company_setup.go:RevokeInvite |
| `SetWorkGrant` | `company_members.go` / TX02 | internal/api/handlers/company_mailboxes.go:Grant |
| `SetWorkMailboxSendPolicy` | `company_members.go` / TX02 | internal/api/handlers/company_mailboxes.go:MailboxSendPolicy |
| `TransferWorkMailbox` | `company_members.go` / TX03 | internal/api/handlers/company_mailboxes.go:Handover |
| `companyAudit` | `company_members.go` / TX01 | 内部helper/无外部名称候选；核查当前caller，不假称公开HTTP入口 |
| `Heartbeat` | `company_ops.go` / TX20 | cmd/tabmail/main.go:main |
| `InspectRecoveryReceipt` | `company_ops.go` / TX20 | internal/api/handlers/company_recovery.go:InspectReceipt；internal/api/handlers/company_recovery.go:RetryReceipt |
| `ReconcileOutbound` | `company_ops.go` / TX13 | internal/api/handlers/company_recovery.go:Reconcile |
| `RetryRecoveryReceipt` | `company_ops.go` / TX15 | internal/api/handlers/company_recovery.go:RetryReceipt |
| `SweepCompanyMetadata` | `company_ops.go` / TX20 | internal/retention/scanner.go:sweep |
| `sweepCompanyAttachments` | `company_ops.go` / TX20 | 内部helper/无外部名称候选；核查当前caller，不假称公开HTTP入口 |
| `PublishMailTemplate` | `company_templates.go` / CTX30 | internal/api/handlers/company_templates.go:Publish |
| `RevokeMailTemplateVersion` | `company_templates.go` / CTX30 | internal/api/handlers/company_templates.go:RevokeTemplateVersion |
| `SaveMailTemplate` | `company_templates.go` / CTX30 | internal/api/handlers/company_templates.go:SaveTemplate |
| `SetMailTemplateRetired` | `company_templates.go` / CTX30 | internal/api/handlers/company_templates.go:Retire |
| `SetTemplateGrant` | `company_templates.go` / CTX30 | internal/api/handlers/company_templates.go:TemplateGrant |
| `DeleteZone` | `domain_delete.go` / TX23 | internal/api/handlers/company_domains.go:Delete；internal/app/domains/service.go:DeleteZone |
| `ExecuteOffboarding` | `employee_disposition.go` / TX07 | internal/app/employees/service.go:Execute |
| `PreviewOffboarding` | `employee_disposition.go` / TX06 | internal/app/employees/service.go:Preview |
| `applyOffboarding` | `employee_disposition.go` / TX07 | 内部helper/无外部名称候选；核查当前caller，不假称公开HTTP入口 |
| `ClaimIngress` | `ingress.go` / TX14 | internal/ingest/workqueue.go:Claim |
| `CreateIngress` | `ingress.go` / TX14 | internal/ingest/recovery.go:acceptDurable |
| `DeliverIngress` | `ingress.go` / TX14 | internal/ingest/recovery.go:deliverTarget |
| `FailIngressTarget` | `ingress.go` / TX14 | internal/ingest/recovery.go:processReceipt |
| `FinishIngress` | `ingress.go` / TX14 | internal/ingest/workqueue.go:MarkDead；internal/ingest/workqueue.go:MarkDone；internal/ingest/workqueue.go:MarkRetry |
| `HoldIngressTarget` | `ingress.go` / TX14 | internal/ingest/recovery.go:processReceipt |
| `ClaimMailIndexJobs` | `mail_content.go` / TX22 | internal/app/mailindex/service.go:Batch |
| `CompleteMailIndexJob` | `mail_content.go` / TX22 | internal/app/mailindex/service.go:Batch |
| `FailMailIndexJob` | `mail_content.go` / TX22 | internal/app/mailindex/service.go:Batch |
| `RetryFailedMailIndex` | `mail_content.go` / TX22 | internal/api/handlers/company_console.go:RetryIndex |
| `SaveParsedMessage` | `mail_content.go` / TX22 | internal/app/companymail/service.go:document |
| `saveParsed` | `mail_content.go` / TX22 | 内部helper/无外部名称候选；核查当前caller，不假称公开HTTP入口 |
| `SetMailboxGrant` | `mailbox_grants.go` / TX24 | 内部helper/无外部名称候选；核查当前caller，不假称公开HTTP入口 |
| `claimMailboxRevision` | `mailbox_revision.go` / TX03 | 内部helper/无外部名称候选；核查当前caller，不假称公开HTTP入口 |
| `transferOwnedMailboxes` | `mailbox_revision.go` / TX03 | 内部helper/无外部名称候选；核查当前caller，不假称公开HTTP入口 |
| `CreateMailbox` | `mailboxes.go` / CTX31 | internal/app/mailboxes/service.go:Create；internal/resolver/resolver.go:resolve；internal/testutil/fake_store.go:SeedMailbox |
| `DeleteMailbox` | `mailboxes.go` / CTX31 | internal/app/mailboxes/service.go:Delete |
| `DeleteUserGuarded` | `member_guard.go` / TX05 | internal/api/handlers/users_admin.go:DeleteUserByAdmin |
| `UpdateUserGuarded` | `member_guard.go` / TX04 | internal/api/handlers/users_admin.go:UpdateUserByAdmin |
| `insertMemberAudit` | `member_guard.go` / TX04 | 内部helper/无外部名称候选；核查当前caller，不假称公开HTTP入口 |
| `ClearOrphanRetry` | `messages.go` / TX21 | internal/retention/scanner.go:clearRetry |
| `CreateMessage` | `messages.go` / TX21 | internal/testutil/fake_store.go:SeedMessage |
| `CreateMessageWithQuota` | `messages.go` / TX21 | internal/rawobject/store.go:StoreMessage |
| `DeleteExpiredMessagesReturningKeys` | `messages.go` / TX21 | internal/retention/scanner.go:sweep |
| `DeleteMessage` | `messages.go` / TX21 | internal/app/messages/service.go:DeleteMessage |
| `EnqueueOrphanRetry` | `messages.go` / TX21 | internal/retention/scanner.go:enqueueRetry |
| `MarkSeen` | `messages.go` / TX21 | internal/app/messages/service.go:MarkSeen |
| `PurgeMailbox` | `messages.go` / TX21 | internal/app/messages/service.go:PurgeMailbox |
| `ReapExhaustedOrphanRetries` | `messages.go` / TX21 | internal/retention/scanner.go:reapExhausted |
| `AddSuppression` | `outbound.go` / TX11 | 内部helper/无外部名称候选；核查当前caller，不假称公开HTTP入口 |
| `ClaimOutboundJobs` | `outbound.go` / TX11 | internal/outbound/workqueue.go:Claim |
| `CreateOutboundAttempt` | `outbound.go` / TX11 | internal/outbound/recipient_delivery.go:deliverRecipients |
| `CreateOutboundJob` | `outbound.go` / TX11 | 内部helper/无外部名称候选；核查当前caller，不假称公开HTTP入口 |
| `CreateOutboundJobAuthorized` | `outbound.go` / TX11 | internal/outbound/service.go:createOutboundJob |
| `CreateOutboundJobConsumeDraft` | `outbound.go` / TX11 | 内部helper/无外部名称候选；核查当前caller，不假称公开HTTP入口 |
| `CreateOutboundJobWithQuota` | `outbound.go` / TX11 | 内部helper/无外部名称候选；核查当前caller，不假称公开HTTP入口 |
| `DeleteSuppression` | `outbound.go` / TX11 | 内部helper/无外部名称候选；核查当前caller，不假称公开HTTP入口 |
| `DeleteSuppressionAudited` | `outbound.go` / TX11 | internal/api/handlers/outbound.go:DeleteSuppression |
| `MarkOutboundJobFailed` | `outbound.go` / TX11 | internal/outbound/recipient_delivery.go:deliverRecipients |
| `MarkOutboundJobRetry` | `outbound.go` / TX11 | internal/outbound/workqueue.go:MarkRetry |
| `MarkOutboundJobSent` | `outbound.go` / TX11 | internal/outbound/recipient_delivery.go:deliverRecipients |
| `RequeueOutboundJob` | `outbound.go` / TX11 | 内部helper/无外部名称候选；核查当前caller，不假称公开HTTP入口 |
| `enqueueOutboundJobTx` | `outbound.go` / TX11 | 内部helper/无外部名称候选；核查当前caller，不假称公开HTTP入口 |
| `enqueueOutboundJobValidated` | `outbound.go` / TX11 | 内部helper/无外部名称候选；核查当前caller，不假称公开HTTP入口 |
| `insertOutboundJob` | `outbound.go` / TX11 | 内部helper/无外部名称候选；核查当前caller，不假称公开HTTP入口 |
| `requeueOutboundJob` | `outbound.go` / TX11 | 内部helper/无外部名称候选；核查当前caller，不假称公开HTTP入口 |
| `BeginOutboundRecipient` | `outbound_recipients.go` / TX12 | internal/outbound/recipient_delivery.go:deliverRecipients |
| `CompleteOutboundRecipient` | `outbound_recipients.go` / TX12 | internal/outbound/recipient_delivery.go:deliverRecipients |
| `RequeueOutboundJobAuthorized` | `outbound_retry.go` / CTX33 | internal/app/submissions/service.go:RetryOutboundJob |
| `finalizeOutbound` | `outbound_transition.go` / TX12 | 内部helper/无外部名称候选；核查当前caller，不假称公开HTTP入口 |
| `CreatePermissionProfile` | `permissions.go` / TX24 | internal/app/permissions/service.go:CreateProfile |
| `DeletePermissionProfile` | `permissions.go` / TX24 | internal/app/permissions/service.go:DeleteProfile |
| `DeleteUserPermissionOverride` | `permissions.go` / TX24 | internal/api/handlers/permissions.go:DeleteUserPermissionOverride；internal/app/permissions/service.go:DeleteUserPermissionOverride |
| `UpdatePermissionProfile` | `permissions.go` / TX24 | internal/app/permissions/service.go:UpdateProfile |
| `UpsertUserPermissionOverride` | `permissions.go` / TX24 | internal/app/permissions/service.go:SetUserPermissionOverride |
| `CreatePlan` | `plans.go` / CTX34 | internal/api/handlers/admin.go:CreatePlan；internal/app/admin/service.go:CreatePlan；internal/testutil/fake_store.go:SeedPlan |
| `DeletePlan` | `plans.go` / CTX34 | internal/api/handlers/admin.go:DeletePlan；internal/app/admin/service.go:DeletePlan |
| `UpdatePlan` | `plans.go` / CTX34 | internal/api/handlers/admin.go:UpdatePlan；internal/app/admin/service.go:UpdatePlan |
| `ClaimIngestJobs` | `queue.go` / CTX35 | 内部helper/无外部名称候选；核查当前caller，不假称公开HTTP入口 |
| `ClaimOutboxEvents` | `queue.go` / CTX35 | internal/hooks/workqueue.go:Claim |
| `ClaimWebhookDeliveries` | `queue.go` / CTX35 | internal/hooks/workqueue.go:Claim |
| `CreateIngestJob` | `queue.go` / TX21 | internal/rawobject/store.go:StoreIngestJob |
| `CreateOutboxEvent` | `queue.go` / CTX35 | internal/hooks/dispatcher.go:Publish |
| `CreateWebhookDeliveries` | `queue.go` / CTX35 | internal/hooks/dispatcher.go:processOutbox |
| `MarkIngestJobDone` | `queue.go` / CTX35 | 内部helper/无外部名称候选；核查当前caller，不假称公开HTTP入口 |
| `MarkIngestJobRetry` | `queue.go` / CTX35 | 内部helper/无外部名称候选；核查当前caller，不假称公开HTTP入口 |
| `MarkOutboxEventDone` | `queue.go` / CTX35 | internal/hooks/workqueue.go:MarkDone |
| `MarkOutboxEventRetry` | `queue.go` / CTX35 | internal/hooks/workqueue.go:MarkDead；internal/hooks/workqueue.go:MarkRetry |
| `MarkWebhookDeliveryDone` | `queue.go` / CTX35 | internal/hooks/workqueue.go:MarkDone |
| `MarkWebhookDeliveryRetry` | `queue.go` / CTX35 | internal/hooks/workqueue.go:MarkDead；internal/hooks/workqueue.go:MarkRetry |
| `PurgeOldIngestJobs` | `queue.go` / CTX35 | internal/retention/scanner.go:purgeIngestJobs |
| `RevokeRefreshTokenByHash` | `refresh_rotation.go` / TX17 | internal/api/handlers/auth.go:Logout；internal/api/handlers/auth.go:Refresh |
| `RotateRefreshToken` | `refresh_rotation.go` / TX16 | internal/api/handlers/auth.go:Refresh |
| `deleteExpiredRefreshFamily` | `refresh_rotation.go` / TX17 | 内部helper/无外部名称候选；核查当前caller，不假称公开HTTP入口 |
| `CreateSendIdentity` | `send_identities.go` / CTX36 | internal/app/domains/service.go:CreateZone |
| `DeleteSendIdentity` | `send_identities.go` / CTX36 | 内部helper/无外部名称候选；核查当前caller，不假称公开HTTP入口 |
| `UpdateSendIdentitiesVerifiedByZone` | `send_identities.go` / CTX36 | internal/app/domains/service.go:TriggerVerify |
| `MutateArchivedMail` | `sent_archive.go` / TX19 | internal/app/mailarchive/service.go:Change |
| `UpsertSetting` | `settings.go` / CTX37 | internal/settings/settings.go:Seed；internal/settings/settings.go:Set |
| `UpsertSMTPPolicy` | `smtp_policy.go` / CTX37 | cmd/tabmail/main.go:main；internal/app/admin/service.go:UpdateSMTPPolicy |
| `CreateTenant` | `tenants.go` / CTX38 | internal/api/handlers/admin.go:CreateTenant；internal/api/handlers/auth.go:Register；internal/app/admin/service.go:CreateTenant |
| `DeleteTenant` | `tenants.go` / CTX38 | internal/api/handlers/admin.go:DeleteTenant；internal/app/admin/service.go:DeleteTenant |
| `UpsertOverride` | `tenants.go` / CTX38 | internal/app/admin/service.go:UpdateTenantOverride |
| `ChangePasswordAtomic` | `users.go` / TX18 | internal/api/handlers/auth.go:ChangePassword |
| `CreateAdminInvitation` | `users.go` / TX18 | internal/api/handlers/users_admin.go:InviteAdmin |
| `CreateRefreshToken` | `users.go` / TX18 | internal/api/handlers/auth.go:issueTokenPair |
| `CreateUser` | `users.go` / TX18 | internal/api/handlers/auth.go:Register |
| `DeleteExpiredRefreshTokens` | `users.go` / TX18 | 内部helper/无外部名称候选；核查当前caller，不假称公开HTTP入口 |
| `DeleteUser` | `users.go` / TX18 | 内部helper/无外部名称候选；核查当前caller，不假称公开HTTP入口 |
| `MarkInvitationAccepted` | `users.go` / TX18 | 内部helper/无外部名称候选；核查当前caller，不假称公开HTTP入口 |
| `RevokeRefreshToken` | `users.go` / TX18 | 内部helper/无外部名称候选；核查当前caller，不假称公开HTTP入口 |
| `RevokeUserRefreshTokens` | `users.go` / TX18 | internal/api/handlers/auth.go:ChangePassword；internal/api/handlers/auth.go:Logout |
| `TouchUserLogin` | `users.go` / TX18 | internal/api/handlers/auth.go:Login |
| `UpdateUser` | `users.go` / TX18 | 内部helper/无外部名称候选；核查当前caller，不假称公开HTTP入口 |
| `UpdateUserPassword` | `users.go` / TX18 | internal/api/handlers/auth.go:updatePasswordHash |
| `CreateWebhookEndpoint` | `webhook_endpoints.go` / CTX35 | 内部helper/无外部名称候选；核查当前caller，不假称公开HTTP入口 |
| `DeleteWebhookEndpoint` | `webhook_endpoints.go` / CTX35 | 内部helper/无外部名称候选；核查当前caller，不假称公开HTTP入口 |
| `UpdateWebhookEndpoint` | `webhook_endpoints.go` / CTX35 | 内部helper/无外部名称候选；核查当前caller，不假称公开HTTP入口 |
| `CreateZone` | `zones.go` / TX23 | internal/api/handlers/company_domains.go:Create；internal/app/domains/service.go:CreateZone；internal/testutil/fake_store.go:SeedZone |
| `UpdateZone` | `zones.go` / TX23 | internal/app/domains/service.go:TriggerVerify |

## 7. FK 与 trigger展开核查

| trigger | 入口 | SQL副作用 / 等待关系 |
|---|---|---|
| fence_employee_enqueue BEFORE | outbound_jobs INSERT | sender user SHARE/active检查；与离职user锁配对，不替代其它发送资格 |
| archive_outbound_mail AFTER | outbound_jobs INSERT | sent asset→item→mailbox event；FK mailbox/zone/asset，事件FK tenant |
| pin_sent_attachment AFTER | outbound_attachments INSERT | sent attachment pin，FK asset/attachment；不是raw对象复制 |
| protect_sent_asset BEFORE | sent asset UPDATE | 不可变字段拒绝，无额外表写；保留trigger错误回滚 |
| immutable_mail_template_version BEFORE | template version UPDATE | 发布内容不可变检查，允许revoked变化 |
| mailbox_change AFTER | messages INSERT/UPDATE/DELETE | mailbox_event_log INSERT，晚期T FK；DELETE也执行，不能漏当删除路径 |
| enqueue_mail_index AFTER | messages INSERT/rawkey UPDATE | document DELETE、index job DELETE/UPSERT；child FK仅message，父消息行→派生job/doc |

FK目录保留迁移中的DROP/重建；例如domain mailbox-zone原CASCADE已在00009改NO ACTION。draft/doc/pins有复合tenant约束但并不因此直接引用tenant。CASCADE/SET NULL会在删除父表时写子行；状态停用不能豁免历史FK。77是源声明数而非当前catalog基数。

## 8. 未验证竞争候选和后续阶段（历史候选，当前裁决见AI矩阵）

> 本节为早期清点快照，不当current pending清单。AC/AE/AF/AH原同源码红绿已归证；domain/plan按当前准入时点，P1/P4/P7与callback物理effect仅列未来边界。当前352/70/15实际证据及原四要求见 [AI最终审材料](evidence/R5-P0-070-FINAL-REVIEW.json)。

| 关系 / 已知边界 | 尚需证据 | 对应任务 |
|---|---|---|
| ChangePassword旧U→R→audit T FK vs 管理T→U | 主线程完整命令原版真实40P01/victim回滚；新T KEY SHARE→条件U写候选3/3三轮，父键顺序和audit回滚已验。不扩展其它user维护 | 本批关系已验证；其它P0-070 / P1-120 |
| 旧DeleteUser Key→U vs member freeze U→Key；tenant/mailbox删除cascade及event T FK | 所有caller权限边界、删除拒绝/cascade、取消与victim回滚 | P0-070 / P1-030 / P3-040 |
| 消息维护M→message与message→M；cache关系已修，不代表其它维护组合安全 | 除已验证cache/三物理delete外的实际并发、multi-mailbox顺序和event隐式父键 | P0-070 / P3-040 |
| publish T UPDATE vs revoke T KEY SHARE | 同tenant在父T先互斥，不把template/version相反词法顺序当40P01证据；其它caller、取消/CAS单列 | P0-070 / P5-010 |
| Activate普通定位tenant→T→invitation/profile/new U/M | expire等待和重复token、FK/unique等待以及邀请实际期限 | P0-070 / P4阶段 |
| profile DELETE→U SET NULL vs reader U→profile NOWAIT | NOWAIT受限拒绝已设计；旧管理writer的授权/CAS/字段意图仍需整链 | P1-030/060/070/120 |
| rawobject Ensure/Delete成功→Commit错误，附件写者rawkey参与性 | callback取消、提交不明不抢删、完整引用协议与重试 | P3-050/060/130 |
| metadata先提交前缀、附件GC保护prefix及tenant LIMIT20 | 公平性、无错误清理活引用、delete/orphan故障原子性 | P3-070/080/090 |
| outbox/webhook ID-only ack/retry与lease重领 | 过期worker不得覆盖新claim、多实例幂等与下游投递不确定 | P6-030 / P7-080/110 |
| 14迁移goose/online startup | 当前catalog一致、停写协调、DDL表锁等待及不能downgrade的数据证据 | P0-060 / 发布门禁 |

## 9. 反例门禁与边界

19项结构测试通过：缺/重复函数、新写函数、新无函数文件、既有函数新SQL、包级常量漂移、callback签名、外部caller、FK/trigger、新migration、缺类型review、缺函数断言、删函数专用SQL effect、缺callback effect、伪runtime/parent completion、UPDATE识别（FOR UPDATE不是写语句）、动态拼接和精确cache message fence。没有运行DB。

校验器不证明锁模式兼容、SQL行为等价、死锁不存在、接口dispatch已全解析、FK最终catalog或任意对象回调无I/O。后续编辑必须重新运行目录门禁并按改变的entry审核；大型修改后执行 Code Index `refresh_index`。

### Queue nullable adapter前置缺口（B01-X）

合法`last_error=NULL`会使Go string Scan失败；claim SQL已提交processing而调用者拿到error，可能未创建fanout。六处COALESCE仅纠正读取适配，不认证outbox+fanout+ack原子性或过期worker安全。四直接leaf已三轮动态通过，其余两同型projection保持source-only。

### 集成事实校正

当前 `ActivateEmployee` 是普通SELECT定位tenant后先T UPDATE，再invitation FOR UPDATE。先前人工表错误把locator读写成邀请锁；结构校验未捕获该人工语义错误。本次只校正文档，不制造invitation→T死锁。期限使用事务now()、重复token及当前sponsor/domain/profile窗口仍需真实命令验收。

## 10. AI当前P0-070原四要求矩阵

当前49 postgres源码／352函数／70函数专用手工轨迹（原68slot家族+新Actor/helper）／456SQL执行调用／15 migrations。逐要求证据与明确不能证明的边界以[最终审材料](evidence/R5-P0-070-FINAL-REVIEW.json)为准；typed寄存仍不填closed_entire_slot=true，不借247测试数量代表每branchruntime。

- 原multi-resource规则/actor等待窗口/异常atomic均按当前真实入口和owner作用域审核，14/46/profile晚FK、53Actor、61URL批序都有原完整命令真红→同最终test候选→当前冷回归。
- freshcatalog75FK／7enabledusertrigger／34CHECK、goose0..15；source声明14Up/77REFERENCES只是历史。tableclosure不是triggerfire，PublishINSERT与Convertexpires分别不触发immutableUPDATE/rawkeyUPDATEOF。
- companyReadTx/Tx/Referenced及enqueue/GC/callback作用明确；没有SQL不等没有external effect，del成功/commit不明不能物理回滚，metadataGC pool前缀非整TX。
- 7internal未装配、1legacycfg与正式caller分清。普通domain verified及plan期限按已有准入时点，不自创最终commit合同。未以未来P1/P4/P6/P7商用验收豁免或无限扩P0。


## 11. AI当前最终catalog与保守entry链接

实际fresh官方New/Migrate15 catalog SHA `c51f6d858c9d12ba037c769e8e83a03ab8a251b8b5f980272d31700571e86736`：75FK／7enabled usertrigger／34CHECK，Goose0..15；[机器catalog](evidence/R5-TRANSACTION-DB-CATALOG.json)352 entry的literal/name closure保守关联不冒dispatch/triggerfire。Publish version INSERT不触发immutable BEFORE UPDATE；Convertexpires UPDATE不触发rawkey UPDATE OF index。旧Z的0f807/14/350仅历史批次，非当前最终数据。


## 12. AI当前70轨迹／68原slot家族寄存

[逐slot寄存](evidence/R5-TRANSACTION-PROOF-REGISTER.json)保留68原编号，新增Actor/helper两个函数是70手工轨迹，352总inventory。7internal未装配、1Durable=false legacycfg与正式caller分清。14/46精确P NOWAIT、53Actor同Tx、61稳定URL批序实际归证，domain/plan按原准入policy checkpoint。当前原070任务需验证的有限条件在独立最终审中无额外缺项，closed_entire_slot=false不是任务否决票亦不代表everybranch runtime；外部callback物理rollback／GC前缀／P1P4P6P7产品政策依然明确未认证。slot1全局bootstrap advisory只收敛同端口同email，不假全库唯一政策。当前最终裁决见[四条审材料](evidence/R5-P0-070-FINAL-REVIEW.json)。


## 13. AB slot53实际有限归证（历史批次）

当前49 postgres非test文件／350函数／452 SQL执行调用结构清单更新；exact T KEY SHARE→S UPDATE/delete→required audit FK→Commit是DeleteSuppressionAudited自己的函数序列，不套enqueue家族风险。same final560c原生产双向40P01和victim全回滚、c874/ff50候选三轮8/8明确归证；正式HTTP父消失404／未知500／原无S204分别保留。

授权仍由请求前middleware决定；tenantID/AuditEntry没有等待后currentactor重载。slot53 parent/S/audit关系安全不等whole-slot070或权限窗口PASS。继承family_context非精确函数phase；未来AC新测试排AB被测744文件。

## AC snapshot闭包（历史批次，当前优先AI）

49 postgres文件／352函数／456 SQL执行调用／15 migrations，caller清单来自独立AC source闭包而非AD未提交helper；实际catalog15是75FK／7trigger／34CHECK。slot53当前正式Authorized Actor路径与trusted Audited/helper分清，九leaf仅bounded准入／线性化proof，不涂全slot或全070。见AC报告。

## AG同源实物证据（历史批次，46随后AH归证）

9516／774 source含AD16／AEAFfinal，ASTcaller从snapshot内生成49files／352fn／456SQL／15migration，不混未提交未来helper。真实23gate、default244／tag12 required及emptyGo0拒门禁不等各writer全证明；14/61 bounded动态与当前domain/planpolicycheckpoint已收敛，46 Guarded requestedP晚FK当时待AH；现AH65e51/8fac三轮5pass及AI冷回归已归证。精确外部callback/error/operationtrigger boundary保持，见AG报告。
## 实际18结构证据不等current函数覆盖（2026-10-02）

[新actual18catalog](evidence/R5-TRANSACTION-DB-CATALOG-SCHEMA18-ACTUAL-20261002.json)是sourcecb03官方实采51tables/75FK/11triggers/18migration hashes，不旧catalog升version。新source1a15实际17、旧15/16与M852900保历史。GC18/head15top21leaf racePASS有限运行不清全library unknown；fresh AST、新production owner family/caller/锁/FK-trigger/callback分类与合法coverage派生仍pending，旧静态统计按原source看。原171/fullCI/M有效baseline未闭。
