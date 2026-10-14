# R5 本轮有限交付与未完成边界

**原规模新 S baseline 已完整采集；M 未完整，P0-100／120／G0 不验收，父任务仍 10/171。** 新 S、4k 诊断、恢复检查、收件资格各有独立不可变运行源。权限 WIP、旧失败与原依赖保留，局部通过不等于最新 checkout 全门禁。本文只集成作者交付并检查必要 raw 终态、scope 与脱敏；没有重跑 PG、Go、浏览器或基准，没有提交、push、PR 或部署。

> 后续实际新增 B4k 与 global-profile fanout 见[有界后续交付](R5-BOUNDED-FOLLOWUP-20261002.md)。本报告原 A／初次交付按当时source保留，现方法仍未准入、M未完整；fanout生产者窄PG已过，但可见list／平台通知／SSE消费各未闭。

> 后续已新增显式opt-in新S4074、profilevisibility与nativecold实际分层；最新事实及M条件准入见[最新有限集成](R5-OPTIN-S-COLD-INTEGRATION-20261002.md)。本文件旧source/原阶段时点保留，不能作为最新WIP状态。

## 范围与状态归属

- 主 checkout：`/Users/jin/Desktop/tabmail`；本轮集成开始只读观察 HEAD `587ba75`，分支 `work/company-mail-r5-goal-20260930`，产品／UI／schema／脚本／文档均有未提交 WIP。HEAD 不是运行源身份，dirty checkout 不是任一旧快照。
- 唯一活动计划为 [R5-TODO](R5-TODO.md)。只统计原父复选框，实际 171 项中 10 项 `[x]`：P0-010–090 及 P0-110；P0-100、120 和 P1–P11 全部未勾。下文残余工单是父任务具名拆分，不另增完成率分母或改变父依赖。
- 新合同 `9e81020680dbd6e990cbb35ab1aac16413885e65306d949a5889c4bafda00b36` 的源／方法原冻结，本轮未修改 `R5-BENCHMARK-DATASETS.json`。新 S 另存原合同字节，避免现 metadata 漂移后把旧结果贴到新源。
- 本报告 flavor=null；scope 仅为项目本轮证据与活动缺口，非生产部署、容量认证或安全全审。历史暂停、原 RED、S2、M/M2 失败及 [权限 WIP 原记录](R5-B02-WIP-RUNTIME-20261002.md)不覆写。

## Evidence：运行源与原终态

| ID | 源身份／实际 closure SHA256 | 观察与安全交付 | 证据层与范围 |
|---|---|---|---|
| E-S | canonical closure SHA1 `3d07b72b3732cf289410aefaaba49f8d8423f746`；`3e3e5d35af81f1f000eaa998f2b7232a8af2388ab78dcbbc36d27df7785ed173` | [原 review](evidence/R5-B01-AJ-S-PIPELINE-REVIEW.json)、[27 原 payload 安全档](evidence/R5-B01-AJ-S-PIPELINE-LOGS.tar.gz)、[当时冻结合同](evidence/R5-B01-AJ-S-PIPELINE-FROZEN-CONTRACT.json) | P/D/H；schema16，原 S=100/500/100000，14×200，Go/runner/strict CLI 均 0 |
| E-DIAG | canonical closure SHA1 `b45b043d822ddabb536f0a2321a1ce0913ae7137`；`e2f7f44e26cb8f357cf34ac51c7a3727e76d7b08ed1bea8bb6c82cc10223c4e2` | [原 review](evidence/R5-B01-AJ-STORE-DIAGNOSTIC-4K-REVIEW.json)、[39 原 payload 安全档](evidence/R5-B01-AJ-STORE-DIAGNOSTIC-4K-LOGS.tar.gz) | D/诊断；20/100/4000，Go0，非 S/M、非 tool admission |
| E-INSPECT | canonical closure SHA1 `2514362546277c3003e97e122ed39eb9f3e9df76`；`b341f50b3fc31434cf20211d8a152af05e0de87c041e98b5e6d01994faf2ac81` | [原 review](evidence/R5-P2-060-INSPECTION-REVIEW.json)、[14 原 payload 安全档](evidence/R5-P2-060-INSPECTION-LOGS.tar.gz) | D/H/F；10 top、25 leaf，race／Go0，仅 `^TestR5OutboundInspection` |
| E-RECEIVED-A | canonical closure SHA1 `dea20bfbaf757029243c4a26d4499b0536375dc4`；`42d17929ba846b28b5d1e9ba2fcf6db00a10c5bae1e4999ec0d6b629bd59265c` | [原 review](evidence/R5-P2-RECEIVED-ELIGIBILITY-REVIEW.json)、[64 原 payload 安全档](evidence/R5-P2-RECEIVED-ELIGIBILITY-LOGS.tar.gz) | D/H/F；完整 `^TestR5Received` 实际 Go1，HTTP 4 top 全过；PG 两个等待观测夹具失败，不包装成完整 Go0 |
| E-RECEIVED-B | canonical closure SHA1 `206d862c56884ca827d14a52366e091968c805f9`；`9e8793988d6b8db828a62b61196874557613f7fe602a95442e86bd78413ab08a` | 同一安全档 `necessary2-source-receipt.json`／`necessary2-green/` | 仅两 child 修观测屏障后 race Go0；唯一变更文件为 `r5_received_content_eligibility_test.go`，生产与预期断言未改 |
| E-RECEIVED-RED | 原与 late canonical overlay 各自身份记录 | 同档 `original-red-canonical/`、`late-red-canonical/`、`late-red-canonical-remainder2/` | 15 个真实受控 leaf RED；旧 `/tmp` 对 `/private/tmp` alias 未应用 attempt 留档但不当产品 RED |

`source_tree` 在新 S 的字段名兼容 registry，**其值是 canonical closure SHA1，绝非 Git tree／commit**；S 档有原 canonical bytes 与 identity proof。其他新源也不伪称 Git。各 review 中继承的 raw/hash 字段只属于作者原件；新安全档包装 hash 与原 tarhash 分列在 [交付清单](evidence/R5-BOUNDED-DELIVERY-20261002.json)，没有每文件重新算 hash，也没有重贴旧运行标签。

### 新 S 实际值与候选范围

- 原执行墙钟 `1518.898014 / 1800s`；包含外层 process 的原 guard 观测 `1530.726965 / 1800s`，未超时；raw package pass `1529.978s`。这些时钟起点不同，不互相冒充。
- 实际 100 employees、500 mailboxes、100000 messages／index-ready；四 lifecycle 精确 80000/10000/5000/5000，4 legal grants/shared，tenant 80000/20000；六 actual SQL streams 指纹 `4bd741d42fda6d537c563d4698b3429bc0192aef139f2635c36af8451797877c`。
- 全 14 行各 200 样本，SQL20 校准、安全拒绝／正控、captured-query 同参数 plan、resource 与 terminal joined 完整。窗口／active≤20，合法 actual Claim20，Parser4／原 30s 与 31s wall reserve 不变，最终 parser quiescent／inflight0。
- RSS 本进程高水位 `328728576 bytes`，owned objects+DB 最大 `1892809751 bytes`；不是整机／PG／Redis 内存上界。准备 `1380.357542s`，Store 阶段 `1203.257564s`，claim 合计 `8.837165s`。
- ready heap 99980 时，**实际** Claim20 `1.085ms`；完整 ready100000 时末空 claim `0.563ms`。同参数 EXPLAIN 是观测，不是产品 Claim 改造或 1M 容量承诺。
- 列表 p95 app cold/hot=`9.118/4.145ms`，通过 **S list≤300ms** 候选；index-ready search=`5932.551/6092.991ms`。原 S 没有 search cutoff，不冒 S 阈值失败，也绝不借 S 值认证 **M search≤1s** 或性能全绿。
- [registry](evidence/R5-BENCHMARK-RUNS.json)保原 S2 条目，新 S 独立成功 baseline；原 M/M2 失败不变，M2 Go exit 仍缺观察。未发生新 M/L，诊断不入 successful-scale runs。

### 4k 诊断能与不能说明什么

- 原审批范围仅 diagnostic 4k；Go0，ready4000，execution `55.760752/180s`；Go-owned DB／object／observer cleanup `63.164988/300s`；**native init 到 stop** 原 origin `63.462329/300s`。owned disk `97820075 bytes`，RSS `238829568 bytes`。
- sampled quota-row Lock=`1652/7563=21.843%`，目标 100ms 采样但最大实际 interval `1.033992s`。百分比是 sample rows，不是占墙钟 21.843%。call spans 并发／嵌套重叠，总和不能当串行 critical path。
- File.Put／File.Sync／Fsync 栈存在，CPU／sched／syscall 聚合只支持定位；sched 可运行延迟不是 fsync I/O 时长，不能据此断言 durable commit 的下界。
- S Store×10=`12032.576s` 大于原 M `10800s` 只是线性规划风险，不是已测 M 失败下界或准入结论。[S 档内 M resource plan](evidence/R5-B01-AJ-S-PIPELINE-LOGS.tar.gz)原结论保留。
- A交付时lookahead100尚未实运行；后续B4k已有独立actual runtime，见[后续交付](R5-BOUNDED-FOLLOWUP-20261002.md)。B仍不能替benchmark方法准入、改DATASETS原方法、盲开下一3h M，或推成产品Store／Claim／durability改造许可。

### 恢复检查与收件资格：限定落地

- E-INSPECT：新受控检查在真实 PG/HTTP 验证 selected company 下当前 platform super、busy snapshot fencing、等锁后降级／冻结／epoch、同 Tx 必要 audit/outbox fault 无 payload／effects；允许字段 projection 不带 secret／headers／BCC／object key，状态来自完整内部 ledger。只覆盖新窄 inspection，不扩大普通 DTO／普通正文阅读。
- P2 OpenAPI 专用 schema、10 targeted static；API-MATRIX 127 operations 中 index109 单条五字段同期更新；UI 4 文件专用 types／121 pure consumer/types 与 scoped tsc/lint由作者报告通过。该层不等 PG→UI 或 shipping browser；OpenAPI／API-MATRIX作者明确为 **transcript-only**：无持久 raw/report/log，静态 GREEN chunk `c4dc15`、矩阵单条断言 chunk `723cae`；矩阵127operations、changed_rows1/index109，仅authority/version/audit/content/evidence五字段变化。没有伪造机器raw，也未重复已绿命令。UI作者已确认无持久raw/report/log，仅transcript：`8f74a3`RED／`99bc57`tracer green／`4fc235`121+tsc+lint0／`685f6f`scopedtsc／`a3bacf`diff；不包装成本agent重验、机器raw或PG→UI/browser。
- E-RECEIVED-A/B：实际 expiry/purge/个人历史非空 hard-expiry 豁免/shared零或NULL历史期限/实际 nonNULL期限、IOReceivedAtZoneID 与空 EOF 权威重核已具名落地。原 10 top／53 leaf 均有真实 green provenance，但来自 A 加必要 B 两 child，**不是 newest source 一次 full Go0**。
- 两失败夹具因 native `track_activity_query_size=1kB` 截断，test marker 在第 1033 字节后不可见；B 改成实际可见 `FROM messages m`，仍保 exact blocking PID 与 deadline 断言，不放宽预期404／count0。
- `MATERIALIZED` 只固定本语句相关时钟／输入，不证明所有写者全面线性一致性。当前 legacy unpin 与同字段完全重建 incarnation 无 schema fence 仍未闭，必须保独立残余。
- 权限、ownerless、P6 前批局部证据依 [原 WIP](R5-B02-WIP-RUNTIME-20261002.md)及其 guard 记录，不重复运行或把 source087849、b927／081a／0ecb 贴到本轮源。tenant global-profile fanout生产者现有后续6top27leaf PG/race实证，但global可见list／平台通知／SSE消费者、全角色与全父条款仍待。

## Finding → Path

| Finding | Evidence | 结论／置信度 | 后续 Path |
|---|---|---|---|
| F-S（validated/high） | E-S | 原 S baseline 完整；仅 S list 候选通过，不证明 M／性能全绿 | P-S：冻结合同→真实 Store/Claim/Parse→ready100k→shipping 14×200→strict CLI→只登记 S；下一走 RES-M-01 |
| F-DIAG（validated/high） | E-DIAG | quota row waiting 与 durability stacks 可观察；非 critical-path 百分比 | P-D：诊断 4k→hashed state samples／聚合 profile→join／清理→单独审 lookahead 实验；下一走 RES-M-01，不跳 M 等价与原规模 |
| F-INSPECT（validated/high，限定） | E-INSPECT | 受控检查窄服务 D/H/F 通过；UI／schema 是另一证据层 | P-I：fresh actor + selected company→job/ledger fence→safe projection + required audit/outbox→响应；下一走 RES-UI-01、RES-GATES-01 |
| F-RECEIVED（validated/high，composed） | E-RECEIVED-A/B/RED | 指定期限／读取边界有对拍 provenance；观测修正没有新源完整 Go0 | P-R：current source eligibility→authorization/object/EOF postwait recheck→拒绝 stale content；下一走 RES-RCV-01/02/03 |

## 具名活动残余与验收

所有下列残余都保持 pending/in_progress，不得据现 test 数量勾父项。原父依赖照 [R5-TODO](R5-TODO.md)；并行 WIP 仅沿用户明确授权的例外，不等依赖已完成。

| 残余 ID | 原父项／依赖 | 明确未完成与验收 |
|---|---|---|
| RES-M-01 | P0-100；040/090 已验；新方法须独审准入 | A/B4k实际诊断已交（B非race），六streams/aggregate/parameters等价与时序/UID未证边界保。下一只读benchmark方法方案／独审→新freeze和必要race/tool/actualS，才裁决M；不改现9e810原合同 |
| RES-M-02 | P0-100；依 RES-M-01 准入或接受原方法后唯一 operator 窗口 | 原 M=1000/5000/1M、10800s／80GiB／C20／14×200，实际 ready1M、raw/Go/runner/CLI/clock/resource/cleanup 全终态，保候选原值；timeout/缺观察留 failed，不借 S 外推 |
| RES-PERF-01 | P7-010/040、P10-020/030；原依赖不改 | shipping indexed_search 当前 S p95 5.93/6.09s，需定位真实 query/同参数 plan/权限前分页及冷热点；产品改动另 source 与等价安全回归，M 1s 只能由真实 M 观测判断 |
| RES-G0-01 | P0-120；依 P0-100 及原其余父依赖 | 完整 S/M 初始证据后逐项审 G0，目标红／fixture失败／unknown 分开；原 B01 PR/交付条件仍保，未获本轮发布授权不自动开 PR |
| RES-RCV-01 | P2-020/030、P7-020；依原 P2-010 等未验父项 | 当前 legacy unpin：需列 exact caller/资源身份保存与失去条件，正式兼容读取/对象/空EOF负控和允许历史正控；不随便禁全 legacy 或改永久政策讨绿 |
| RES-RCV-02 | P2-020/030、P7-020；依 RES-RCV-01 身份设计及原 P0-060 | 同 ID/同全部字段完全重建 incarnation 仍无 schema fence；独立 owner 定持久不可回退版本与升级策略，ABA真PG/HTTP/object/stream负控与合法重建正控，审迁移/历史兼容后再落地 |
| RES-RCV-03 | P2-030/140；依当前 test-only delta 集成与原父依赖 | 冻结集成 source 后 **唯一 operator** 执行受影响完整 received selectors；须保原 A Go1与2-child B，fresh all-prefix Go0、exact waits 与失败来源，不篡改 composed 为一轮绿 |
| RES-INSPECT-01 | P2-060/140；依 P2-010/P1-030 等原未验父项 | 当前10top/25leaf仅窄检查；需按父项所有入口／body/headers／必要审计逐条审、兼容与reason/noeffects全要求，不靠 operation index109 或 static10 称父项完成 |
| RES-UI-01 | P1-100/110/140、P2-130/140、P9-130；保原依赖 | 121 pure tests／scoped tsc/lint不是正式服务浏览器；shipping Go+PG→recovery UI／权限编辑、多窗口stale409保draft、company切换／撤权清private结果、empty/error/no payload真实旅程 |
| RES-P1-EVENT-01 | P1-130、P7-070/080；依 P1-100/110/120 等原未验父项 | 生产者多tenant受众/required audit+outbox rollback已有后续窄PG实证，不勾父项。RES-P1-EVENT-02global可见list／-03空受众平台通知／-04真实SSE消费者分别待验，精确依赖见后续报告 |
| RES-GATES-01 | P1-140、P2-150、P6-160、P11-130；保全部原依赖 | 新集成树 full Go/race/PG/HTTP/协议/契约/迁移/必跑、clean Next cold build、shipping browser 与独立最终审尚未跑；所有P3–P11其他原条目照旧，不以本表聚合吸收或取消 |

## 安全包装与离线复核

四安全档只保原选中 payload 字节，剔除 AppleDouble。DIAG 额外排除 runtime trace／CPU 与 trace 衍生 `.pprof` 二进制，保原 diagnostic JSON（仅 query hash／hashed mailbox，`sql_args_recorded=false`）、聚合 profile 文本与解析 exit；原 profiling 二进制仍私有 `/tmp` 不随仓库交付。无私有 DSN/JWT/Bearer、邮件原件、objects、数据库目录、完整 CPU/trace 参数或生产配置。新包装 hash 仅在 [delivery manifest](evidence/R5-BOUNDED-DELIVERY-20261002.json)计算一次；原 tarhash沿作者 delivery receipt，不重复全文件 hash。

只读离线验证（不启动数据库、不重跑测试）：

```sh
# 本报告同目录为 cwd
python3 -m json.tool evidence/R5-BENCHMARK-RUNS.json >/dev/null
python3 -m json.tool evidence/R5-BOUNDED-DELIVERY-20261002.json >/dev/null
tar -xOf evidence/R5-B01-AJ-S-PIPELINE-LOGS.tar.gz S/go.exit
tar -xOf evidence/R5-P2-RECEIVED-ELIGIBILITY-LOGS.tar.gz green/go.exit
tar -xOf evidence/R5-P2-RECEIVED-ELIGIBILITY-LOGS.tar.gz necessary2-green/go.exit
```

预期依次 `0`、`1`、`0`。actual runtime命令、redacted env、冻结 source closure 和清理回执均在各安全档。完整 runtime复现仍需 owner 原只读源码快照／native PG 工具及私有 owned DSN，不声称仅 safe tar 自带可运行生产 tree。清理按作者实际终态：本轮各 owned PG/socket/caffeinate已 absent／stop0；不是用旧PID猜当前租约，也没有信号旧88440/19547/36519或其它进程。

## 下一可独占工单（3项）

1. **RES-M-01 benchmark方法准入方案**：bounded A/B已有限交付；pipeline作者先只读方案，独审通过后才新methodfreeze/必要工具与actualS；DATASETS9e810不可暗改，未授权不M/L。
2. **RES-RCV-01/02 身份缺口设计与证据**：content owner独占 received identity/application/store叶与拟新增迁移，test author独占 ABA/legacy负控；先明确 incarnation 与历史兼容，公共 DTO/schema/中央计划由当前集成 owner串行协调。与benchmark实验无生产写集交叉。
3. **RES-P1-EVENT-02/03/04消费者缺口**：生产者窄PG已交；global可见list、空受众平台通知政策、真实SSE→UI按各自独占write set继续。必要平台policy裁决先于实现，保持requiredaudit/outbox与tenant隐私，不重跑既有生产者绿。

父171全完成尚未实现。后续需按现具名缺口继续产品实施与真实门禁，不把本次证据整理当最终验收。文档变更后提醒执行 Code-Index `refresh_index`；本轮未昂贵重复全树索引。
