# R5 目标协议案例草案

2026-09-28，B01-D。依据 [R5-DESIGN](R5-DESIGN.md) 的既定目标，将内容、回执、留存、BCC 与离职语义整理为输入/输出案例。**这是尚未完成可执行核验的草案，不是生产接口行为清单，也不是已冻结的HTTP契约。P0-080保持未完成。** 实际入口和旧行为另见 [R5-API-MATRIX](R5-API-MATRIX.md)。

B01-D 时校验脚本未落地；本轮正常工具流程新增机器案例目录及命名测试参考驱动，新增部分共享输入 Go/组件消费者；HTTP 及其余案例仍缺，不能将结构检查当成协议验收。表中的状态码是目标响应类别；错误响应必需字段是 `error.code` 和非空 `error.message`；`error.reason` 仅是可选子分类，不要求为所有错误创造理由字符串。具体入口仍需与 OpenAPI/Go/TS 核验，不能将目标表当成所有产品入口已经实现。

## 1. 通用规则和判定顺序

先验证当前主体及凭据，再验证具体租户/资源范围，再判断当前权利、生命周期和操作版本；数据库或资格查询失败不得回退为完整内容。管理身份不代替read，历史提交者不代替当前邮箱身份，缓存/任务/对象/索引存在不授予阅读资格。以下案例在未明确改变的轴上均假定其他前置条件成立；匿名与停用凭据优先拒绝。正常正文接口不因为运维人员的角色而跳过普通资格，恢复例外使用单独入口。

时间取服务端当前时间；等于截止即失效。收/发内容使用同一逻辑期限，物理清理进度不影响可见性。历史个人邮箱的异常期限必须按P3-020核查和保守迁移，不能直接套用新过滤进行批量删除。

## 2. 正文、回执与恢复

| 编号 | 输入或改变的条件 | 目标输出/拒绝 | 对应验收 |
|---|---|---|---|
| CT01 | 有效当前reader，原邮箱ID匹配，条目未到期 | 200，允许指定内容/附件；不扩大到别的邮箱 | AC-05、AC-28 |
| CT02 | 匿名或已撤销/冻结凭据 | 401，不打开对象 | AC-31 |
| CT03 | 公司admin或平台super_admin但没有该邮箱read | sent content 404，不凭角色放行；入站入口单独核对 | AC-05、AC-07 |
| CT04 | 历史发送者已撤去read | sent content 404；看得到提交回执也不能展开正文 | AC-05、AC-06 |
| CT05 | 邮箱地址被删除重建，ID不同 | 404，不把旧邮件转向新ID | AC-05 |
| CT06 | 同名资源属于另一租户 | 404，不能跨公司读取或泄露资源存在性 | AC-05 |
| CT07 | 当前域名范围不包含资源 | 403，不因grant或owner而放宽域名范围 | AC-05 |
| CT08 | 硬到期时刻小于或等于当前时刻 | 404，不打开正文/附件对象 | AC-05、AC-10 |
| CT09 | 回收站截止小于或等于当前时刻 | 404，物理记录仍在也不可读 | AC-05、AC-10 |
| CT10 | 权威存储查询失败 | 5xx、无正文，不回退为原始job JSON | AC-05、AC-06 |
| RC01 | 合法回执主体，内容已经失效，任务仍保留 | 回执可依政策200，但只有安全状态投影，view_content=false | AC-05、AC-06 |
| RC02 | 旧/outbound列表、详情、提交重放 | 与安全回执/同一内容资格适配，不再有独立正文旁路 | AC-05 |
| RC03 | 收件人结果包含accepted和permanent，后者恰好为BCC | 显示部分失败，隐藏BCC不能变为全部成功 | AC-06、AC-08 |
| RC04 | 任一收件人uncertain | 显示不确定，普通重试不可用；不能标为取消或成功 | AC-24 |
| RC05 | 全部收件人已被下一跳接受 | 表述为下一跳接受，不宣称最终送达 | AC-23、AC-24 |
| OP01 | 普通reader、公司admin或历史发送者要求例外读取 | 403，无恢复原件 | AC-07 |
| OP02 | 平台运维、选定公司、精确资源、有效理由、必要审计成功 | 允许受控恢复读取；无delivery token等执行凭据 | AC-07 |
| OP03 | 运维理由不足8字节或超过1000字节、仅空白 | 400，不执行例外读取；中文按UTF-8字节核验 | AC-07 |
| OP04 | 必要审计失败 | 5xx、无内容，不能先返回正文再补审计 | AC-07 |

普通回执不得包含正文、BCC、原件/附件对象键、token、原始headers或可回显正文的SMTP诊断。恢复读取也不应返回执行令牌。Key入口维持独立scope及当前属主边界，不因为它看得到某个任务就推导出互动公司管理资格；具体Key兼容映射由P0-110与P1-080逐项落实。

## 3. 留存和完整密送记录

| 编号 | 输入或改变的条件 | 目标输出/拒绝 | 对应验收 |
|---|---|---|---|
| RT01 | 有效共享邮箱条目，有限hard_expiry，trash后restore | 200，hard_expiry完全不变，只清删除/回收站标记 | AC-10 / A05 |
| RT02 | personal正常永久策略，hard_expiry为空 | restore/archive不会创建新的硬到期时间 | AC-11 |
| RT03 | hard_expiry或purge_after已达截止 | 恢复409，状态不变；读取按CT08/09拒绝 | AC-10 |
| RT04 | 组织操作只有read而没有organize；另测organize-only配置拒绝 | 403，组织操作必须同时read和organize；organize-only配置先400拒绝 | AC-10 |
| RT05 | archive/unarchive | 不暂停、清除或后移硬期限 | AC-10、AC-11 |
| RT06 | trash重复执行 | 不通过重复删除向后延长已有回收站截止 | AC-10 |
| RT07 | 草稿/已发送/队列/held原件仍引用对象 | 逻辑正文可失效，但受保护原件不因正文失效立即删掉 | AC-13、AC-14 |
| BC01 | 新提交包含结构化To/CC/BCC | 长期资产保存完整类别和附件；wire没有BCC头 | AC-08、AC-09 / A07 |
| BC02 | 旧job仍存在且tenant/asset精确匹配 | 允许从可靠结构化来源回填；重复回填不覆盖正确值 | AC-09 |
| BC03 | 历史来源已删除或无法确证 | 标明legacy_unknown；不得用空名单声称没有密送 | AC-09 |
| BC04 | 当前共享邮箱reader且条目存活 | 发件内容详情按同一read政策显示BCC，不另造平行权限 | AC-08 |
| BC05 | 普通列表、回执、SSE、webhook、metrics或网络MIME | 不输出BCC地址 | AC-06、AC-09 |
| BC06 | 删除投递job及recipient ledger后条目仍存活 | 完整发件内容、分类收件人和附件独立保留 | AC-08 / A07 |
| GC01 | 前100个候选仍受保护，第101个过期孤儿可删除 | 有界清理最终能够处理孤儿，前100个仍受保护 | AC-12 / A06 |
| GC02 | sealed draft或held原件持有引用 | 普通清理不得删除；创建tombstone也不自动到期 | AC-13、AC-14 |

RT03的操作拒绝与CT08的读取不可见并不矛盾：前者是写命令冲突，后者是不再提供内容。实现时必须一起覆盖实际收件、已发送、附件、原件与兼容回执入口。

## 4. 冻结、离职和权限编辑

| 编号 | 输入或改变的条件 | 目标输出/拒绝 | 对应验收 |
|---|---|---|---|
| LF01 | 目标已冻结、尚未资产处置，当前有权管理员及有效接收人 | 可预览/执行，全过程不重新启用旧账号 | AC-15 / A04 |
| LF02 | 同一有效生命周期的同一已执行plan重放 | 返回原回执，不重复转移/取消/审计 | AC-16 |
| LF03 | 同一次离职已有另一plan完成 | 409，不执行第二次处置 | AC-16 |
| LF04 | 相同数量但资产ID/revision或资格已变 | 409，重新预览，不以数量相同放行旧指纹 | AC-17 |
| LF05 | 接收人已冻结/跨公司，或试图自己交接/越管理层级 | 资格拒绝且无副作用：冻结/跨公司/自己交接400，越目标管理层级403 | AC-04、AC-17 |
| LF06 | 重新启用/再次入职后的旧生命周期plan或旧凭据 | 不复活旧plan、grant或token；旧plan409 | AC-18、AC-31 |
| LF07 | 审计或中间写入失败 | 原子回滚资产、用户及计划状态，不能留下半交接 | AC-18 |
| PE01 | 个人覆盖禁发/限制域名，仅调整quota | 只改额度，其他原始覆盖保持不变 | AC-01 / A01 |
| PE02 | 未提交字段 / 显式null / false / 0 / [] | 分别表示保持/恢复继承/具体输入；原始覆盖不从effective反推 | AC-03 |
| PE03 | 旧profile表单修改描述携带旧安全字段 | revision不匹配409，不恢复刚撤销权限 | AC-02 / A02 |
| PE04 | 删除重建覆盖或重新分配profile后提交旧revision | 409，持久版本不能回到初始值 | AC-03 |
| PE05 | 授权校验后等待草稿行，管理员撤去邮箱发送grant | 二者有明确顺序：保存先完成或新权限先阻止；撤权完成后旧快照不能继续提交 | AC-13、AC-31 |

## 5. 尚缺的交付

本表尚未做到机器可执行、输入模式完整、Go/HTTP/组件适配器逐项对照；部分入口状态码边界仍明确待核对；并非所有错误都必须带 `reason`。因此080未完成，不能据此启动依赖它的110实现。后续须保留这些缺口，并把每项具体案例绑定到相同输入/预期数据与真实测试；不能仅验证Markdown行数就宣布协议验收通过。P1-P4原七项缺陷仍由各阶段修复验收关闭。


## 6. B01-V 机器案例子包（仍未冻结）

- [R5-PROTOCOL-CASES.json](evidence/R5-PROTOCOL-CASES.json) 编码本表全部 **46** 个案例的结构化输入、预期 HTTP 类别、语义拒绝类别、AC/A01–A07、必需证据层和未解决项。`semantic_code` 是目标分类，不是已经落实的 `error.reason`。
- `scripts/check_r5_protocol.py` 核对精确案例集合及真实 Go 测试符号，按 `unit/db/http/components` 执行命名参考测试，要求每个测试实际 run/pass、包成功，拒绝 skip/missing/失败。记录源提交、dirty 路径、案例与适配器源哈希和原始 Go JSONL。
- 19 个案例绑定现有真实测试参考。第二轮 RC03/04/05 与 OP03 已增加共享输入真实 Go 消费者，RC03/04/05 增加实际渲染组件消费者；随后增加 CT01–10、OP01–04、RT01–06、RC01/03/04/05 的实际 PostgreSQL 和 shipping router 消费者，现46例均声明共享消费者，注册数量本身不证明实际执行或全层政策已实现，所有案例的完整必需层尚未全部验收。参考测试 PASS 只能证明该测试的原断言，不能证明此 JSON 的全部目标、精确理由码或各层一致。组件基线观察器 PASS 表示成功复现旧缺陷，不是安全协议已实现。
- CT03/CT04 的 sent-content 错误码从草案 403 明确修正为 404，与 `CONTENT-BOUNDARIES.md` 及 `TestSubmissionAuthorLosesContentButKeepsReceiptAfterReadRevocation` 的当前断言一致。入站或域名拒绝码需单独绑定入口；不通过放宽为任意 403/404 集合隐去矛盾。

```sh
# 仅结构检查：成功不代表协议已冻结
python3 scripts/check_r5_protocol.py
python3 -m unittest discover -s scripts/tests -p 'test_r5_protocol.py'
# 实际 Go 单元参考；fresh output-dir 防止覆盖旧证据
python3 scripts/check_r5_protocol.py --run unit --output-dir /tmp/tabmail-r5-protocol-unit
# 只使用隔离测试 DB，沿用已有 fixture；不读取 .env、不连接生产
TABMAIL_TEST_DB_DSN='<disposable-test-dsn>' python3 scripts/check_r5_protocol.py --run http --output-dir /tmp/tabmail-r5-protocol-http
```

P0-080 仍未完成：要为所有案例接入共同输入、精确入口/status/code、reason presence政策与当前 Go/HTTP/组件断言，且不能用基线缺陷观察器替代安全目标回归。P0-110/G0 等依赖仍按 TODO 原门禁执行。


### 第二轮共享输入消费者

RC03/04/05 的 JSON 输入现在包括真实 job state、分类地址与逐目标状态、in-flight 标记、敏感字段。Go 测试直接调用生产 `delivery.DeriveSubmissionStatus`、`submissions.RedactOutboundJobView`、`FilterRecipientsForJobView` 与 `OutboundCapabilities`，不按案例编号编写第二套政策。能力计算的隔离存储是 FakeStore，仅证明应用策略，**不证明数据库授权或 HTTP 原子边界**。

组件测试读取相同 JSON 及 Go 真实输出（带案例 SHA-256），将其送给真实 `SubmissionPane`；只注入 API 运输结果，不把目标状态/按钮值伪造成服务端输出。验证部分接受、下一跳接受、不确定提醒、重试按钮、BCC与敏感字段不展示。该证据为 jsdom 组件，不是 shipping 浏览器或实际 HTTP。

OP03 新增 11 个输入变体，涵盖空白、trim、7/8 字节、中文 6/9/12 字节及 1000/1001 字节。目标包含无效拒绝与有效边界控制；消费者调用生产 `credentials.AuditReason`，验证真实错误 sentinel 与持久化规范值。恢复 HTTP 使用 `BAD_REQUEST` 与非空 `message`，普通错误不带 `reason`；不要求补造 reason 常量。

```sh
python3 scripts/check_r5_protocol.py --run shared-unit --output-dir /tmp/tabmail-r5-protocol-shared-unit
python3 scripts/check_r5_protocol.py --run shared-components --output-dir /tmp/tabmail-r5-protocol-shared-components
```

共享执行报告逐例列出已验证层和 `missing_required_layers`；报告及返回码成功仅表示所声明的共享纯策略/组件子包通过，`task_complete` 仍为 false。


## 7. B01-X 实际 wire 契约及共享 PostgreSQL/HTTP 子包

### 错误字段冻结

依据 `internal/api/handlers/respond.go`、`app_error.go` 和实际 middleware：

| 响应 | `error.code` | `error.message` | `error.reason` |
|---|---|---|---|
| 普通400/401/403/404/409/500 | `BAD_REQUEST` / `UNAUTHORIZED` / `FORBIDDEN` / `NOT_FOUND` / `CONFLICT` / `INTERNAL` | 必需且非空；文案不是本轮稳定机器枚举 | absent，不写成JSON null |
| outbound retry冲突 | `CONFLICT` | 必需且非空 | 对应分支 exact：`delivery_uncertain` 或 `state_changed` |
| 其他允许子分类的入口 | 由该入口冻结 | 必需且非空 | optional，不能从通用envelope推导为必需 |
| 成功/非HTTP后台操作 | 不适用 | 不适用 | 不适用 |

机器案例的 `expected.wire_error` 记录该政策。旧 `error_reason:null` 保留为兼容数据，但不代表缺口；校验器不再要求额外理由码才能通过。`TestR5ProtocolWireErrorShape` 执行真实输出函数及 application-error 映射，覆盖 absent/exact 的序列化区别。

### 新消费者实际调用与边界

- `TestR5ProtocolContentSharedCases`：输入建模共享grant、旧历史作者、两个管理员角色、租户变更、地址换ID、硬到期/回收截止before/equal与权威表查询错误；调用真实 `GetSubmissionContent` / `GetSubmissionAttachment`，再经过正式 router 的 content/attachments 路由。查询故障通过该测试独有schema内移除权威表触发真实SQL错误，不伪造HTTP响应。CT05保留archive FK禁止物理删除的约束：将旧地址保留到retired namespace，撤旧grant，新建同原地址但不同ID并仅授新ID read；这证明地址重用不扩大旧ID内容资格，不宣称实际DELETE成功。
- `TestR5ProtocolCredentialSharedCases`：匿名、旧session版本、停用用户，经真实JWT与当前主体准入拒绝；不用绕过middleware的注入actor替代HTTP。
- `TestR5ProtocolDomainSharedCases`：真实profile限制与真实grant并存，入站 `GetWorkMessage` 及详情路由403；sent-content隐藏范围继续404，不放宽成403/404集合。
- `TestR5ProtocolRecoverySharedCases`：普通主体真实平台角色门禁；平台操作员、精确job、11个理由变体，逐次核验实际必要audit及规范值；故障trigger仅拒绝本fixture的 `outbound.break_glass`，实际500且无内容。
- `TestR5ProtocolRetentionSharedCases`：真实收件message actions，核查hard expiry不变与重复trash截止；RT03双截止轴×before/equal必须409并无变更。当前已知A05可以精确目标红呈现，不提前修复P3。
- `TestR5ProtocolReceiptSharedCases`：同一JSON的真实job及recipient ledger，通过 `GetSubmission` 与正式receipt路由核对状态、当前正文能力及私密字段；BCC隐藏缺口绑定A03/AC-06/P2-050，不能用纯策略或组件PASS隐去实际路由泄漏。

当前 **46例均有共享消费者声明**，但不是“46例全部产品绿灯”，也不是080自动完成。结构检查不证明执行；所有新声明必须由最终稳定源快照实际跑证据。原4例Go/组件消费者保持原源文件不动。后台GC/引用保护只要求真实DB端口；BC02/03是当前schema/真实响应的精确能力缺口观察，不声称执行尚不存在的R5回填命令。RC01/02、离职和权限编辑的组件层仍未实现，由 `missing_required_layers` 显式暴露。

### 精确目标红与基础设施失败

`baseline_target_failure` 必须绑定 audit、AC、后续任务、精确marker和完整runtime test path。当前准入逐例维护在 `baseline_target_failure.test_paths`，包括已真实复现的内容/BCC/离职/权限/公平GC缺口；每条都只允许特定语义断言，不允许整组泛红；RT03只对真实200“到期恢复被接受”路径写目标marker。其它unexpected status、fixture失败、编译失败、任意parent/cleanup非目标断言、panic/runtime error/race/deadline、未知变体和skip/missing全部拒绝。原Go JSONL及原process exit code保留；精确红报告 `product_green=false`，不会变成产品全绿。P0-120允许精确目标红，但不允许基础设施错误冒充已知缺陷。

```sh
# 主线程在源快照稳定后串行运行，必须显式隔离DSN；不读取.env
TABMAIL_TEST_DB_DSN='<disposable-test-dsn>' python3 scripts/check_r5_protocol.py --run shared-http --output-dir /tmp/tabmail-r5-protocol-shared-http-fresh
```

完整080仍待最终46例实际执行、所有适用层与剩余精确入口边界审查；本轮不修改TODO勾选或制造完成声明。


### 2026-09-30至10-01 后续消费者与适用性审查

- 全部Go共享adapter声明精确 `runtime_test_paths`。driver要求每个实际case/variant有唯一run及pass/精确目标fail，不能仅父测试run/pass而把未执行变体计为验证。parent、helper/cleanup（包括非`_test.go`的源码诊断）、包额外失败/编译、panic/runtime/deadline/race都不能由一个已知target leaf掩盖。原process exit及JSONL不改。
- RT04原organize-only状态被正式grant命令和数据库CHECK禁止。保留原输入于`unrepresentable_target_permissions`、实际PgStore及正式PUT拒绝400；可表示的read-only操作另证403。原状态的下游runtime不可构造，不解除CHECK或冒称已运行该非法状态。完整policy变更记录在机器案例 `policy_change_log`。
- RT07先用SQL确认deleted_at及已到purge_after、同一object key，再调用实际GetWorkMessage。当前`company_message_read.go`没有生命周期谓词，`companymail.message`只复验identity；实际仍返回精确内容provenance才写 `RT07_READ_BOUNDARY`（A05/AC-10/P3-010）。随后继续全部draft/sent_asset/queue/held保护断言。personal permanent的异常hard expiry不是测试前提，不在P3-020之前重新解释历史期限。
- LF01–07：正式preview/execute路由、持久处置回执重放、第二plan、同数不同draft revision、执行前successor冻结、跨公司/自我/目标管理层级、正式复职后旧plan、必要audit失败回滚。LF05拒绝精确到入口阶段；不伪造被preview禁止的非法可执行plan。组件交互仍是另层。
- PE01–04：正式旧权限表单与SQL raw override/实际effective，逐项omitted/null/false/0/[]；旧profile快照或覆盖删除重建后，只有actual200且SQL确认恢复刚撤销权限才为特定目标红。没有将未实现revision字段作为非法请求然后声称CAS已证明。
- PE05：两个并发命令均走正式authenticated HTTP，数据库draft行锁与`pg_blocking_pids`确定顺序，检查持久payload/revision、实际revoke200及后续save403；只对“revoke已完成但旧snapshot仍真正提交”写目标marker。轮询间隔不建立顺序，timeout不能变目标红。
- BC01/04/06：实际draft submit201、独立asset/附件、队列和ledger真实删除、生产MIME；BCC缺snapshot/不向合格reader显示是精确A07后续任务，不掩盖已保留的To/CC/正文/附件。
- BC02/03：实际job/asset provenance和现存sent-content响应。不存在正式R5 backfill命令；repeat只检查当前重复观察不覆盖原值，不冒称执行了幂等回填。无来源或同IDforeign-tenant job必须保守未知，实际没有`legacy_unknown`才观察目标缺口。P2-080仍需真正版本化迁移/回填写测试。
- BC05：正式submission list/receipt/SSE；生产Build；真实DB operational metrics+生产Prometheus renderer（不是shipping /metrics HTTP）；实际message.trash outbox producer，经真实dispatcher POST到loopback接收器（不是尚不存在的outbound submission webhook类型）。这些scope在机器案例 `applicability` 固定，不借一项存在的事件证明所有未来事件。
- GC01：真实100/101候选、实际Sweep三轮、固定ID排序及保护存量，精确A06/P3-070推进缺口。GC02：真实offboard seal、creation receipt；真实ingress hold/finalize、删除邮箱后fixed-destination tombstone；实际原件reaper不调用删除回调。后台DB端口不编造HTTP适配器。

本轮主线程第四快照 `85e67ef4da71218546fb93ccc9ee8f6bdee8c8e8` 的真实报告曾验证35例、17个精确目标红、errors=[]，`product_green=false`/`task_complete=false`。这是中途快照，不替代后续46例和最新源码的最终验收；此前fixture/路径错误均保留原失败日志并独立修复，没有当目标红接纳。

## Y：正式HTTP/PG到组件的独立消费者

16case/23精确variant由Go-owned PostgreSQL、真实shipping HTTP与API client/fetch驱动shipping组件；认证host context由夹具供给但实际JWT/权限仍走正式middleware。它们以Go adapter components层注册，原DB scalar目标/paths不改，UI每adapter exactpath另绑定12个已知安全目标到原P1/P2/P4任务，不覆盖或吞旧DB红。CASE SHA改变后必须同源码fresh运行，注册数不当执行数。

RC02的legacy list/detail/submit-replay仍保留required components欠缺：现有ReceiptFolder/SubmissionPane走company/submissions，Compose走company/drafts/id/submit；不能拿新pane代替独立legacy路径，也不擅免适用层。后续P0-110/P2-040/140与P5-110需正式兼容/调用者裁决。shipping真实浏览器旅程不由jsdom/loopback替代。

## AA：原080目标真值与后续产品写入分层

RC02不免原components要求：当前正式ReceiptFolder/workspace兼容入口连接独立LegacyReceiptFolder，真实GET legacy list/detail，Compose断连后同key重放使用其原公开submission入口；实际三variant消费者注册后须同新CASE SHA／源码fresh运行。旧DB scalar与新UI／能力exact路径marker分开，不覆盖旧失败。

BC02/03原目标（可靠来源回填、重复不覆盖、无可信源legacy_unknown）完整保留；新当前能力consumer只证实际最终schema／精确same-tenant job来源／HTTP缺口及两次当前Migrate no-op。它既不是未来正式write执行，也不是其幂等证明。P0-080/120基线允许绑定AC/后续任务的精确能力红；真正版本化snapshot/unknown迁移、qualified backfill、empty/upgrade及重复同formal writes验收仍必须P2-070/080/110实现，不能以基线层集合或controller0提前标产品完成。

## AB：2026-10-03 receipt / legacy adapter-fixture revision 2

本附录显式修订 RC02/BC03 的执行 oracle / fixture（revision 2），不改变 `R5-PROTOCOL-CASES.json` schema v1、任何原 input、正式 runtime path、target marker 或 required layer。旧版映射是 `ac5db2ee72b97b027a14d6e885d3870276bfaf82`（其产品与授权 base `ee3308fd3217246c9bdd43b07ae0609ebae6aeb6` 相同）。原 `R5-CURRENT-WIRE-20261003` 失败证据与报告保持原样；修正后必须另收 fresh runtime，不提升旧失败。

- RC02 revision 1 将 subject 留在 ordinary receipt 当作“合法回执未丢”的判据，与当前 privacy DTO 相反。revision 2 的三条正式 list/detail/same-key replay 都经过真实 shipping router/JWT/PG，严格关闭顶层与嵌套 DTO 字段，精确核对原 job ID、tenant、pending/submitted、known progress 的 total=2/pending=2、时间、attempt_count=0、delivery_uncertain=false、过期后的 view_content=false/retry=false。list 的 retry_block_reason 为当前保守 `unknown`；detail/replay 为 `state_not_retryable`。DB 原 draft/mailbox/user 身份对应、replay 同 ID、draft 对应 DB job=1 仍必须成立；subject/body/BCC/headers 不得泄露，不以 contains 代替 DTO 断言。
- BC03 revision 1 的 `r5LegacyJob` 实际走当前 `CreateOutboundJob`。migration 17 的 capture trigger 已将其写成 version=1/complete/non-null BCC；source job 消失不应将可信持久 snapshot 降级。revision 2 的原 missing/foreign 负例改用 `r5UnprovableLegacyAssetV2`：直接构造已失去 structured source 的历史 persisted asset/item，所有当前 trigger/constraint 保持开启，初态必须为 version=0/legacy_unknown/NULL BCC 且无 job。它是历史存量形状 fixture，不宣称实际运行了 schema-16 upgrade。真实 schema-16 write → migration17 → bounded backfill 由原 `TestR5SentRecipientSnapshotUpgradeV16BoundedTrustedBackfill` 另行回归。
- 原 BC03 capability/observation paths 与 marker 不删；响应必须同时存在精确 `recipient_completeness="legacy_unknown"` 与 JSON `bcc:null`，不能用字符串 contains 或 known-empty 替代。重复 GET / 两次当前 Migrate no-op 原不变值约束保留；BC02 仍按原 scope 观察当前可靠源，不冒称旧 adapter 执行了正式 backfill。
- 新 `TestR5ProtocolDurableBCCSnapshotV2` 分别证明当前完整 snapshot 在 missing source / same-ID foreign source 后，重复真实 HTTP 与显式 bounded backfill 均保持原 BCC、正文、To/CC 及完整 immutable asset。新 `TestR5ProtocolLegacyBCCIdentityV2` 单独构造所有 immutable payload 相同、只 tenant / zone / mailbox 一轴不符的 same-ID source；真实 selector 必须拒绝，重复真实 HTTP 与 bounded backfill 不改历史 NULL/unknown asset。这些新增反例是独立补充回归，不偷偷扩充原 formal adapter paths 或完成层声明。

这是测试/fixture 最小修复；不改生产 selector/backfill、migration、DTO、数据模型、runnerprep/source policy、兼容映射、锁或预算。原缺层与 P0-080/120 等验收仍按各 owner 的 fresh 证据裁决。
