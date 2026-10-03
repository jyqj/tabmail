# R5 契约与迁移兼容门禁地图（P0-110 / AB 准备批）

## 范围与依赖

本批将**当前正式源码**的 route → OpenAPI schema → Go/TS DTO → shipped client → test 对照保存为[机器地图](evidence/R5-COMPATIBILITY-GATES.json)。它是源码盘点与协调发布计划，不是接口/旧客户端/数据库升级执行证据。`task_complete=false`、`product_green=false`；P0-020、050、080 已在 AA `364925dcd9ba9a2c1fcddd2593449538ec57081c` 的正式 TODO 中验收；P0-110 的逐变更兼容条件仍须 integration operator 人工审查，不能由源码检查器自动验收。不得把 AA/Y 的旧运行成绩套到 AB 的新源码。

2026-10-01，当前 dirty checkout（基底 `347e341e7f192cf55bda043e14a366250e1c7e43`）执行：

- 正式 `TestR5RouteInventory` 的 Go AST producer：**127** route declarations，exit 0；包含 **67** company 操作。该测试无 DB/HTTP，不按目录文本猜路由。
- 正式 `collect_api_calls.cjs` 的 TypeScript AST producer：**127** 调用分支，exit 0；包含新版 legacy list/detail，动态 transport forwarding 仍显式保留。
- 地图绑定 **52** 份源码 SHA；12 个 route 没有当前 shipped web caller，这不等于无旧 API/Key 使用者。外部使用量未知；本批未读取生产 usage、Key 或 token。
- 新检查器拒绝缺失/重复 route、schema/client/test/source 漂移、升级批次缺失、无旧写拒绝或同批升级条件、伪产品绿/依赖验收/运行成绩。13 项测试方法（内部 mutation 不重复当独立 test）实际通过。

## 现存门禁及不能证明的内容

| 门禁 | 当前能力 | 必須保留的边界 |
|---|---|---|
| Go AST route inventory | 真实 router 与 reviewed route 注册相等 | 不证明 endpoint 实际可达、授权或 effect |
| `check_contract_drift.py` | selected Go/TS/OpenAPI 字段、required、指针/null、形状与枚举；company response bindings | 非 omitempty slice/map 的 nil→null 残余、opaque map/struct、storage shared models不是严格 allow-list wire DTO；不证明 CAS/权限 |
| `collect_api_calls.cjs` | TS AST request path/method、forwarders、source/line | 动态 request body 的 omission/null/false/0/[] 不由路径相等证明 |
| `TestCompanyHTTPContract` | 正式 HTTP fixture 与 response 校验能力 | 2 项 live DNS 操作明示不覆盖；本批没有新跑 HTTP/DB，登记 test 名不计运行 |
| R5 shared protocol | 真输入/状态/HTTP/组件 baseline；精确政策缺陷红 | 接受目标红不等于产品绿；current migration no-op 不等于未来回填写幂等 |

当前 OpenAPI 未登记的5项：`DELETE /api/v1/suppression/{id}`、`GET /api/v1/suppression`、`GET /api/v1/outbound/{id}/attempts`、`POST /api/v1/outbound/{id}/retry`、`GET /docs-assets/*`。地图保留这些实际缺口，不删除 route 取得齐全。最后一项为文档静态资源，不是 JSON API；其余仍需按 P8 契约批次审查。没有显式 DTO binding 的 route 不伪造 Go 类型；由 `go_ts_dto_bindings=[]` 和 `coverage_limits` 保留差异。

## 协调发布与旧写入策略

以下是**后续任务必须实施的策略**，不是当前后端已经拒绝旧写入。每个 route 的 `release_batch` 由路径/真实 handler 范围分类，完整逐条对照以机器地图为准；`stable` 只表示本盘点未声明破坏性修改，不能豁免后来发现的旧写问题。

| 批次 | 破坏性合同/旧写拒绝方案 | 必須同批升级的客户端/数据条件 |
|---|---|---|
| P1 权限 | 当前 revision/CAS；缺失、stale 明确400/409；遗漏保留；false/0/[]/null不混淆；不为旧写保留 last-writer-wins | profile/override/grants/send-policy 表单和外部管理 Key/SDK；security field 编辑与 conflict UI 同批。当前 omission/stale 红项仍属于 P1 |
| P2 内容/BCC | 安全回执去私密字段、旧 `/outbound` 不可绕内容资格；同key replay一次；可信来源回填、不可证来源 legacy_unknown | SubmissionPane/Compatibility receipts/Compose及 send:read Key；allow-list DTO/OpenAPI 同批；版本化回填需真升级/重复写测试，不用当前两次 Migrate no-op替代 |
| P3 留存 | 截止 restore409；旧 action不能重置期限；NULL/0/unlimited/永久历史语义单独迁移 | folder action客户端及 migration 同批；个人永久保护与共享期限独立回归 |
| P4 离职 | 当前 plan/actor/disposition；stale/expired/非法 successor拒绝无effects；completed plan同receipt一次 | UsersPage/OffboardingPanel及admin客户端；preview/execute/receipt与frozen selector合同一起升级 |
| P5 草稿/模板/提交 | stale draft/source token与template/grant revision拒绝；不可变version不能覆盖；提交重放不再入队 | Compose/附件/模板editor/library与CAS、immutable version/grant、same-key retry一起升级；原P5-080/090/100/110 |
| P6 收发/恢复 | stale inspection/reconciliation/uncertain revision、缺recipient ack拒绝；不盲重发；保原目标身份 | RecoveryPage、SubmissionPane retry及operator/Key客户端、recipient outcome、error.code/message及可选 delivery_uncertain/state_changed reason同批；原P6-020/070/130 |
| P7 查询/事件 | search/list/conversation资格先于分页；unsupported index revision、非法或过期cursor按已发布拒绝/resync；不以旧事件继承授权 | 搜索/索引/SSE streamEvents/webhook客户端与稳定分页、cursor/resync及private field allow-list同批；原P7-070/090/100/120 |
| P8 其余DTO | 若未来发生破坏性写，先明确 input DTO/version与旧写拒绝，禁止静默缺值/null coercion | 逐route列出的客户端+OpenAPI+allow-list DTO；私密storage fields不能直接加到公开schema |

外部旧客户端/API Key 使用者没有通过静态源码证明“不存在”。route middleware 的 Key/scope/admin资格是源码事实，不是已经完成 runtime 七类主体验收。调用者在机器地图中逐 source/owner/path/method 列出；无 shipped caller 的 route 保留 `external_usage_unknown_no_shipped_client`。

## 可复现命令与证据等级

```sh
# 在 operator 配置的 Go/Node/Python PATH 和离线 cache 上执行；不启动 DB。
python3 -B scripts/check_r5_compatibility.py
python3 -B -m unittest scripts.tests.test_r5_compatibility
```

检查器默认重新调用正式 Go AST 与 Node AST producer，然后对照 schema/DTO/source hash 和地图；非零或异常拒绝。可提供同一轮实际 producer 的 `--route-inventory` / `--client-inventory` 原始输出做只读复算，二者必须同时提供；不把这些 JSON 声称 HTTP 输出。

后续 integration operator 新快照必须串行执行现有 `make contract-check`、fresh HTTP contract（隔离测试 DB）、协议及旧写消费者，并单独归档 source/exit/raw。这里没有运行这些门禁，也不前置实现 P1–P8。P0-110 最终完成仍需逐破坏性变化人工复审拒旧写方案、客户端同批条件；080正式依赖现已满足，仍不能仅见本检查器 exit0勾选。

## AA 正式提交后的鲜源复核与逐变更审查

AA `364925dc` 已验收080为可执行目标红基线，**不是产品全绿**。本批在该正式 checkout 重新执行 Go AST route producer 与 Node AST client producer，均 exit0；对照原52个源码SHA没有漂移。机器地图保留原采集身份，新增 `post_dependency_source_confirmation` 明确本轮只证明鲜源盘点，没有新HTTP/DB/未来迁移写入成绩。

`request_contract` 现在逐route保留真实OpenAPI requestBody的required及各media type inline schema；`request_schema_refs=[]` 不能被误读为没有请求合同，也不能把 inline/动态 map输入当成显式Go DTO。

| 已知破坏性变更 | 实际现存入口/客户端 | 当前差异与拒旧写/同批条件审查 |
|---|---|---|
| 覆盖遗漏与null语义 | PUT `/api/v1/admin/users/{id}/permissions` → `web/lib/api/permissions.ts::setUserPermissionOverride` | PE01/02原target：当前部分写入可能重置安全字段，null未完整区分；P1保留遗漏/false/0/[]，显式清除策略和revision拒绝必须与该客户端同批；不能仅TS字段存在称已修 |
| stale profile/ABA | PATCH `/api/v1/admin/permissions/{id}` → `web/lib/api/permissions.ts::updatePermissionProfile`；覆盖写同上 | PE03/04原target：当前旧表单可恢复撤权；P1要求missing/stale revision显式400/409，更新/重建后的版本不可复用，客户端刷新conflict并禁盲覆盖 |
| grant及send-policy编辑 | PUT mailbox grants → `web/components/company/grants.tsx::GrantEditor`；send-policy实际caller在矩阵逐条列出 | P1各命令的当前revision/authority与未来编辑合同分别审查；有409基线不等于其它权限命令全有CAS。旧grant写入必须维持read/organize约束、scope和原ID |
| 安全回执/私密字段移除 | company submissions → SubmissionPane；GET `/outbound` → `web/lib/legacy-outbound.ts`；submit → `web/lib/company.ts::submitDraft` | RC01/03仍target；RC02正式兼容入口真网络已安全。P2安全投影不可为旧send:read Key保留原始正文/BCC；P5提交命令身份与P2回执投影须联合发布，不能单批client改名假兼容 |
| BCC完整性/旧数据未知 | 正式sent-content读取+当前资产schema，没有正式R5回填写命令 | BC02/03当前能力红：P2-080/110需真版本化回填、exact provenance/legacy_unknown；重复写保正确值是未来验收，不用已有GET/两次current Migrate no-op替代 |
| restore/expiry动作 | 实际company mailbox action → 矩阵列出的folder caller | RT01/03/07当前红不变；P3拒截止恢复、不可续期限，保守迁移个人永久/NULL/0。UI同批显示冲突而不是恢复失败后重新创建内容 |
| 离职计划/封存语义 | POST employees offboard → `web/features/company/api.ts::executeOffboarding` | LF01/06仍target；P4冻结/复活后的plan边界与completed once receipt不同；可用confirmed plan协议需新旧admin客户端同批，不对旧请求默换successor |
| 模板/草稿/恢复命令 | template editor、Compose与`recovery/page.tsx::RecoveryPage`；SubmissionPane旧outbound retry | P5约束draft/source token及模板immutable version/grant；P6约束inspection/uncertain revision；P7另约束查询/索引/events/cursor；旧outbound retry缺OpenAPI仍保留差异，缺version/revision拒绝策略与明确客户端升级一起发布 |

以上是逐现存入口的兼容裁决与实施约束，不是本批提前实现未来政策。未看到外部调用使用数据时，外部SDK/API Key都保持“未知”；没有OpenAPI或显式DTO/实际测试的格子保留缺口，不编造schema或响应。默认 `stable` route 后来若发生破坏性字段/写入变化必须重新归批，不因当前没有已知变更豁免。

### 人工审查纠错：任务ID存在不等于语义正确

原错误计划把P6写为模板、P7写为恢复；实际TODO是P5包含模板，P6收发/租约/恢复，P7搜索/索引/事件。原source-only门禁绿不作为该错误计划验收，旧raw保留不覆盖。本版已最小纠正各真实route的batch及原任务绑定，并增加semantic-route反例：templates/其grants/submit→P5，legacy outbound retry/inspect/reconcile/recovery→P6，实际company index retry和mailbox events→P7。validator要求这些批次绑定原P5-080/100/110、P6-020/070/130、P7-070/090/100/120任务，拒绝“ID存在但阶段含义错”的旧方案。

### 真实wire登记修正

正式HTTP fixture的两个DNS排除项仅为 **GET `/api/v1/company/domains/{id}/verification`** 与 **POST `/api/v1/company/domains/{id}/verify`**；POST `/api/v1/company/domains` 创建与GET列表均真实覆盖。前版误把create排除且把verification状态列为已fixture覆盖，本版按实际method+canonical path纠正，新增反例。旧source门禁绿不能代替此人工事实审查；原full日志不回写。

## AB fresh实际wire安全摘要与后验范围

产品程序源码快照 **e2d633b13f5e4658f8c25d202be36e8400a363f4**，validation commit **88d1411ee9cc03ed6ddd1e0785b8049f04b7a177** 的原full程序验证保持独立。operator在actual terminal后导出安全summary：SHA **6421a985979e1e61e187d7b51ac30d75c0ac11c1d8082904fa113037bd352e33**。本次四文件DNS标签/摘要join/新反例是**后验工具与文档修正**，不是上述原full已测试的新checker，不把“原20gate通过”改写为新整树全量成绩。

机器地图逐route新增 `wire_observations`：**193** 个actual status/aspect条目对应 **75** 个canonical route；其余 **52** 个标 `explicit_not_observed`。每条保留method+route、status、schema/status或真实component HTTP aspect、response digest、input reference，UI target marker按case/variant精确绑定。只复制安全metadata，**不复制payload/body/token/headers/private fixture**；193不是独立漏洞数、75也不是全部授权/CAS已通过。HTTP schema/status、UI政策target与执行资格必须按aspect区分。

29个输入reference包括fresh HTTP result、DB/component报告和26个安全observation packets；分别绑定实际tree或validation commit，以及OpenAPI/cases SHA。`source_closure_sha256`为原final-source manifest字节SHA，optional artifact模式还要求地图52个source hash与该frozen manifest相同。不存在正式回填命令的BC02/03仍是current capability baseline，不自动补成未来writes。

```sh
# source-only fresh Go/Node + 嵌入摘要/pinned hashes检查，未读取原artifact时明确报metadata-only
python3 -B scripts/check_r5_compatibility.py
# operator只给safe artifact根；不读取responses.json或私有包
python3 -B scripts/check_r5_compatibility.py --wire-evidence-root "$SAFE_AB_EVIDENCE_ROOT"
python3 -B -m unittest scripts.tests.test_r5_compatibility
```

后者实际重哈希原manifest与29个safe inputrefs，读取report/observations metadata核对身份；当前实际运行exit0，报告scope=`actual_safe_artifacts_rehashed`。wrong tree/private commit、spec/case/closure/hash、错误DNS方法、unknown/private fields、错误status/aspect/marker及not-observed冒充observed均拒绝。PE02逻辑variant `[]` 的安全归档目录仅明确别名 `empty_array`，不改变原case或目标断言。

原P0-110要求的是清点差异及兼容/升级方案，不要求本批提前修产品或使127endpoint全通过。现在可逐条追踪源码/已观测wire/未观测/未来方案：5缺OpenAPI、未绑明确DTO与外部Key使用unknown均保留；所有已知breaking变更有原任务、拒旧写和具体同批客户端条件。完成裁决仍由integration operator结合fresh门禁与逐条人工审查，不由checker自动`task_complete=true`。

## 2026-10-03 当前结构与历史 wire 的独立版本边界

本次独立任务基底为 `4aec142bcffd53c6ab91f2263436aaf980833fab`，产品 sourceaeed47f 不变。此前各节的 127 route、193 observation、75 observed route 成绩属于历史 v1，不代表当前结构。当前 [schema v2 地图](evidence/R5-COMPATIBILITY-GATES.json) 用正式 Go AST/TypeScript 5.9.3 AST 鲜源重建：132 route、134 client branches，其中 7 forwarder；route→handler→middleware/conditions→OpenAPI request/response→reviewed DTO→client→test registration→release batch→legacy disposition 全部重新对照。forwarder 保留在原 client producer 输出，不能算具体 route caller。

旧地图原字节保存在 [historical-map-v1.json](evidence/R5-COMPATIBILITY-CURRENT-20261003/historical-map-v1.json)，SHA256 `61b039486bc7804366012298fe87203882b6fb52ba1b160eb8ee77d76989dc22`。旧 wire summary 原 SHA `6421a985979e1e61e187d7b51ac30d75c0ac11c1d8082904fa113037bd352e33`、source identity、spec/cases SHA 和所有 observations 不重签、不回写。历史正例只针对独立 historical-source fixture 的真实 Git 字节，来源提交与 SHA 见 [manifest](evidence/R5-COMPATIBILITY-CURRENT-20261003/historical-source-manifest.json)。这只检查历史嵌入 metadata/pin，不声称重新读取原 safe runtime artifacts。

当前 source validator 独立 PASS，输出 `wire_validation_scope=not_checked_current_wire_required`；当前地图禁止嵌入/重签历史 runtime summary 或 row observations，仅含固定 hash 的历史引用。原 `validate_wire` 当前 spec/cases 校验原样保留；旧 v1 加当前源码必拒绝 `wire spec/case differs from current source`。`--wire-evidence-root` 仍不能让 schema v2 source-only 地图变成 wire 成绩，无当前合格 receipt 时拒绝。两种 PASS 的区别是显式边界，不把 source PASS 改称 wire PASS。`task_complete=false`、`product_green=false`、依赖需 operator review 始终保留。

### 逐 delta 人工复核

完整逐 route/field 的 before/after 保存在 [route-deltas.json](evidence/R5-COMPATIBILITY-CURRENT-20261003/route-deltas.json)，没有删除旧 127 route。下表覆盖非行号/非全局 middleware/非 client 行号变化；所有 caller 的 source/owner/method/path/line 原值在新地图与 producer 输出逐条保留。

| delta | 当前 source 对照与兼容裁决 |
|---|---|
| 新 GET/PATCH permission-editor、POST permission-editor/assignment、GET permission profile deletion-preview | 精确 perm handler、RequireAdmin、named request/response schema、真实 permission-editor TS caller；归 P1。PermissionEditorSnapshot/Command、PermissionAssignmentCommand、PermissionProfileDeletionPreview 命名 Go/TS DTO 实际符号与 source SHA 绑定；response 的 data envelope 显式记录。字段行为仍需独立 consumers，不能从类型名推导 CAS 已通过 |
| 新 GET company/events | adminEvents.Events，cfg.CompanyRepository != nil 与 ok 条件、RequireAuth/RequireAdmin；CompanyAdminInvalidation/StreamScope、company-events caller；归 P7。SSE 手写 projection 不伪造成 company DTO type，本图保留无明确 Go/TS binding 缺口 |
| PATCH profile、DELETE profile、旧 PUT/DELETE user permissions | 更新 versioned profile command/error/preview schema 与真实客户端；旧 PUT requestBody 已移除，不能继续将历史 inline override body 描述为当前支持。拒旧写与 P1 同批升级策略保留，未以结构事实声称全部协议绿 |
| draft submit、company outbound recipients | 当前 OutboundReceipt→OrdinaryReceipt，替代 storage OutboundJob/Recipient wire 假设；P5 submit 与 P2 receipt 合同联合发布约束保留。本任务仅读取已有合同，不修改另一 worker 的 ordinary receipt 产品/工具 |
| legacy outbound list/detail/attempts/retry | 当前 wrapper OutboundReceiptListResponse/OutboundReceiptResponse；attempts/retry 的 OpenAPI 已存在，旧两个缺口不能继续列为当前缺口。wrapper 未有显式 reviewed Go/TS direct binding 的格子仍为空，外部客户端/Key 使用量未知 |
| company outbound inspect | 当前 reason 的 trim、8–1000 UTF-8 byte 说明同步 request schema；字符长度不替代服务器 byte 检查；仍归 P6 |
| 全局 middleware 与所有 client 行号变化 | 实际 Auth(cached,…,authState)、PermissionLoader、原 Key/scope/admin middleware、route conditions 逐条重采；handler/middleware 实现字节纳入 current source closure。语法注册不证明 runtime authority；客户端新 branch/owner 与 source SHA 对照，不因 caller 消失宣布无外部使用者 |

P1–P8 与 stable 的原 release_batches 内容逐字保留；仅把新 permission-editor 入口显式归 P1。外部 legacy disposition 两种 unknown 保留。当前尚无 OpenAPI 的实际 route 为 DELETE suppression/{id}、GET suppression、GET docs-assets/* 共 3 项，未造 schema，也未删除 route。

### 负例和 producer 需求

原 route/schema/client/test/source drift、duplicate/missing、依赖/产品/legacy unknown、P5–P7 task semantics、DNS exact method、safejoin source identity/status/private fields/hash/case/marker 负例全部保留。新增当前 producer 的 future route/wrong handler/middleware/allowed-source drift、权限入口批次、历史 wire/current spec 拒绝、重新嵌入历史 summary/row runtime 与改历史引用 hash 拒绝。历史正例使用原 spec/cases 独立 fixture，未 mock digest 或放松生产校验。历史 v1 safe summary 另绑定原固定 SHA：把旧 expected_spec、HTTP ref spec 和 summary hash 一起改成当前值也拒绝，不能重新签名旧 runtime。client producer 同时对照独立鲜源 clients.json，伪 owner/branch 不能自填地图后获得结构 PASS。

本次不新跑 HTTP/PG wire，已选择用户授权的历史 source fixture 正例方案。若 integration operator 要生成当前 wire，必须先完成同一冻结当前 source 的正式 producer 身份闭包：

1. `check_http_contract.py --output-dir <fresh-private-dir> --source-sha <exact-current-tree>` 使用现有 TestCompanyHTTPContract，隔离自有 PG 的 TABMAIL_TEST_DB_DSN、合成账户/邮件与 loopback，原 `-race -timeout=180s`、240s process ceiling 保持；两项 live DNS 排除不变。responses.json 含合成 secrets 只能私有存放，导出安全 status/hash/schema metadata。
2. `check_r5_protocol.py --run shared-db` 与 `--run shared-components` 都要求正式 supported `--source-policy`、matching versioned `--source-manifest`、其原字节 `--source-manifest-sha256`、新的独立 output-dir；Go 原 race/120s test 与180s process 门禁不增加。PG/loopback/component 合成 fixtures，不接真实邮件或 DNS。
3. 原 AB `validate_wire` 的身份结构是 frozen_tree/validation_commit 40hex 与旧 final-source manifest；当前 protocol versioned producer 的身份语义不同。新完整 packet 与 summary 需要 integration operator 明确新版本 join contract、实际 artifact hashes、精确 source/spec/cases closure、target red 分类和 exclusions 后单独审查；不得仅把 expected_spec_sha256 改成当前值，或混用旧 AB 29 个 input refs。此任务未触碰 versioned runner/prep/coldweb/ordinary receipt，也不替它们造当前 receipt。

仓库和父工作区未提供可读 `.agents/skills` 文件；已检查 `.agents`（为空/仓库中不存在），读取现有 web/AGENTS.md。官方 Go1.25.7、TS5.9.3、锁定 Python requirements 在自有 `/workspace/r5-compatibility` venv/cache 使用，无锁、replace、权限或凭据修改。新结果/raw failure 与固定 remote SHA 见同目录 evidence 的 validation.json；整套范围外 ERROR 原始日志保留，不回写旧运行成绩。

本次最终相关 suite **223 tests PASS**（其中独立 compatibility suite **19 tests PASS**），current source gate / contract checker / client AST **27 tests PASS**。宽范围 tools sweep **652 tests，3 ERROR，1 skipped** 是最终 fixed-summary pin 前的独立探索运行；最终受影响 suite 已重跑，不能把宽范围 sweep 改写为全绿。两个旧 compatibility ERROR 与一次 sibling import invocation ERROR 均保留原始日志。代码与证据 commit `006ff69` 已正常 origin push；唯一一次 draft PR 请求原始返回 `Post "https://api.github.com/graphql": Forbidden`，PR 未创建、不重试、不改凭据。该拒绝与 origin Git 无关，raw 在同目录 draft-pr-once.raw.txt。
