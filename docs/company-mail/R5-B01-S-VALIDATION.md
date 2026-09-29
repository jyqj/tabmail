# R5 B01-S：旧 outbound 内容资格与当前已发送资产同源

2026-09-29。B01-S已完成旧任务详情/列表/尝试诊断、收件人投影及owned API Key的内容放行修复。原版同测试27事件21fail/6pass；39组PG/142事件连续三轮通过；最终完整backend1084pass/0fail/1 browser skip、139必跑齐全，HTTP80响应通过。P0-070、P0-080、P2整体与G0不提前关闭，唯一活跃清单仍为R5-TODO.md。

## 1. 基线与实际问题

起点 `8bd5dae8413f2390a318b1d4aebf5d0f39baaf34`，原工作区clean；工作分支 `fix/company-legacy-content-authority-20260929`。不重做Q/R权限快照，也不修改普通收件的历史期限政策。

最终被测源码tree为 `c33fda56bac580421ea600072b7cc023a1ad86ad`，651份版本文件逐一核验哈希。最终提交仅在其上补文档与证据，不把tree当作commit；精确commit由Git交付回执提供。

原 `submissions.ContentAllowed` 分别读取用户、邮箱和grant，却没有查询sent asset/item。真实PostgreSQL及生产Router夹具中，同一已发送条目硬到期后，新 `/company/submissions/{id}/content` 返回404，旧 `/outbound/{id}`、`/outbound` 和 `/outbound/{id}/attempts` 仍返回正文或协议诊断。已清理item/asset但保留job的状态也不能靠邮箱可读权复活正文。

对持有旧Actor的服务调用，原逻辑还不复核Key是否已删除、过期、换属主、撤去发送scope或缩小Key/属主域名范围。这是认证之后的内容决策缺口；不把它描述为新请求能绕过原有X-API-Key认证。原HTTP认证对已删除Key本就拒绝。

收件人接口先决定BCC可见性，再读取可能阻塞的ledger。独立屏障用例让条目在ledger等待期间到期，确认原决定不能一直沿用到返回时。

## 2. 原位实现

新增单方法 `store.OutboundContentAuthority` 并嵌入现有聚合端口；`submissions.ContentAllowed` 删除分散查询，直接调用该端口。PgStore使用原 `sentContentFrom` 和 `submissionContentScope` 的EXISTS查询，不加载正文，也不复制一套到期或可读邮箱SQL。精确核对tenant、job/asset ID、原sender mailbox与zone，缺asset/item、已到期、无当前阅读权均为false；存储故障不回退到job正文。

交互用户复用现有companyReadTx：当前user SHARE / 身份重载 → 普通用户profile SHARE NOWAIT及原effective合并 → mailbox SHARE → 存活item和内容范围检查 → Commit。无全租户排他锁，管理员仍须拥有普通邮箱read。

owned Key保持独立凭据语义：当前同tenant属主user SHARE → 当前key SHARE NOWAIT → 核对Key ID、属主、tenant、scope和Key域名范围 → 属主当前profile/override快照 → mailbox SHARE → 同一sent内容谓词及实际数据库时钟检查Key截止 → Commit。私有user形状的范围选择器仅用于复用内容SQL，不授予交互管理权，也不继承属主管理员特权。未知主体和ownerless Key不取得正文资格。

Key的SHARE使用NOWAIT，因为旧DeleteUser可能先删Key再锁user；繁忙Key返回409，避免用户/Key反向等待。该策略也可能与last-used等Key元数据更新冲突，不能宣称正常请求绝无重试成本。profile同样沿用Q的NOWAIT语义，不吞错自动重试。锁覆盖的是事务决策点，不延长到整个HTTP响应。

纯函数 `authz.OutboundContentKeyMatches` 只集中静态身份/scope/zone元数据判断，Pg/Fake共用；到期使用各适配器的决策时钟。允许send:read或send:write产生各自合法操作的内容投影，但实际GET仍由既有RequireScopes(send:read)控制，HTTP反例证明send:write不能读取GET。

## 3. 投影、错误和测试接线

现有RedactOutboundJob/RedactOutboundAttempts和capabilities自动使用同一端口。收件人handler把最终RedactOutboundJob移到ledger读取之后，再调用原BCC/诊断过滤函数。旧handler复用respondAppError保留409，未知数据库错误仍为脱敏500，不另造错误映射。

原回执的Subject、To/CC、状态及既有所有权维度保持；本批不是P2最小安全回执DTO，不宣称已关闭全部元数据、重放、重试授权或API Key生命周期问题。有效owner回执在内容过期后仍可读取。job中保留的内容字节没有被物理删除。

FakeStore新增独立的合成archive事实表，由CreateOutboundJob建立；任意未保存的job字面量不再当作正文资格。它不模拟真实PG期限、锁或GC。两个旧单测补齐保存夹具后，原有允许/拒绝、脱敏及对象不变断言不放宽；真正生命周期、Key和跨语句等待由PgStore测试证明。

## 4. 验证方法与原始失败

新PG测试使用每用例独立数据库、生产Store/Router和现有pg_blocking_pids屏障，覆盖live/trash-live/硬到期/purge/缺item/缺asset、当前Key/属主、身份等待后权限收紧、Key等邮箱锁跨过截止、Key繁忙后恢复、ledger等待后的BCC过滤，以及数据库失败/取消。HTTP使用合成对象/Redis适配器，不启动公网投递worker。

初始夹具有两处错误：直接删除仍被item引用的asset触发FK；撤scope误用不存在的mailbox:read而非mailboxes:read。修正后对原版与候选使用同一份测试重新取得行为对照，初始失败不计作产品漏洞。另一HTTP Key夹具最初用Bearer而非产品规定的X-API-Key，401是正确拒绝；只修夹具，不改认证规则。一次结构编辑因锚点多匹配被拒绝，未产生部分写入。

首轮修复版PG23事件通过；两个旧服务单测因缺合成archive而失败，补齐保存前提后93事件通过。后续新增Key实际HTTP、Key截止等待和ledger等待用例，最终结果以以下冻结源码的完整验收为准，不挪用前轮成绩。

## 5. 最终验收

| 验证 | 结果 |
|---|---|
| 原版覆盖相同最终九组PG测试 | 27事件：21fail、6pass，0skip；包含父节点，不是21个独立漏洞。 |
| 最终PG定向（含O/P/Q/R） | 每轮142pass，三轮共426；39必跑齐全，0fail/skip。 |
| Go build/vet | 最终源码树通过，readonly modules。 |
| Python/静态契约 | 168测试通过；16共享模型、33三方投影、67操作绑定通过。 |
| 首次完整backend | 1083pass、1fail、1skip；备份测试子进程错误选到系统Python3.9.6，不支持安全tar filter。原失败保留。 |
| 修正环境后单独备份回归 | 1pass；仅PATH前置既有Python3.12虚拟环境，不修改脚本、断言或超时预算。 |
| 最终完整backend race | 1085started、1084pass、0fail、1skip；139必跑齐全，原180秒包级预算。 |
| 实际HTTP/PG契约 | 80响应、65操作、66成功变体通过，2个实时DNS操作显式排除。 |
| 缺DSN负例 | Go exit0但6子测试skip/父节点pass；门禁exit1，38个必跑未满足，拒绝未执行。 |

唯一backend允许skip为独立TestR3BrowserJourney。本批未重跑前端套件或shipping浏览器。外层首次验证Job因真实备份失败返回1；修正子进程解释器路径后在同一被测源码上重跑完整backend/HTTP，不将首次失败改写为成功。原解析器、原件下载、文件后端及前序PG回归均保留在全量运行中。

## 6. 兼容、运行与剩余范围

不修改历史migration、公开DTO/OpenAPI、Go module/npm lockfile或前端。存量没有对应可读存活sent item的旧任务，现在只能显示既有受限回执；不自动回填、删除或迁移生产数据。返回前重验不是跨网络的线性化撤权，已经释放的字节不能追回；列表的逐条决策也不构成整页单一快照。新增SQL/锁会有成本，未测吞吐、P95或S/M/L规模。

本批使用既有Go1.25.7、Python3.12及PG16.13，在全新0700临时根和Unix socket上运行，禁TCP、host认证拒绝、依赖只读缓存/GOPROXY=off。不读取生产DSN、环境文件、邮件或真实对象。原始responses.json仅本地0600保留，不纳入发布证据；完整服务日志及运行环境文件也不归档。

实际版本：Go1.25.7 darwin/arm64、Python3.12.11、PostgreSQL/psql/pg_dump16.13。第一次实例pid58360停止exit0；环境修正后同一私有集群重新启动pid62525，最终停止exit0且PID文件消失。只保留本批私有目录，不操作其他集群或共享工具。

[机器结果](evidence/R5-B01-S-VALIDATION.json)及[原始执行记录](evidence/R5-B01-S-LOGS.tar.gz)包含113成员、225791字节，SHA-256 `abb00e775d49aeeef72e526f7a77092357773e33283dd42f9182ec9497826739`。所有成员与MANIFEST逐一核验；Go日志、必跑清单、OpenAPI和HTTP采集hash分别比对通过，原始responses.json未发布。

剩余包括旧回执自身的身份/scope与整页一致性、RetryAuthority/入队最终资格、普通收件期限兼容映射、P1编辑CAS、其余FK/GC、P0-080协议、shipping浏览器、真实DNS/SSE重连、规模性能及真实S3/断电耐久性。仅创建本地提交，不push/PR/merge/release/deploy，6/171父任务统计不变。
