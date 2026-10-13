# Tabmail 原始 TODO 核对与验收阻塞推进 — 2026-10-12

## 计数与本记录边界

本请求要求累计新增完成至少 10 个原始 TODO，且不把修复 Issue、PR、测试、文档或子门计入。本轮通过多轮原生 multiagent 完成核对、实现和交叉审查，**原始 TODO 新增关闭 ID 为无，累计新增完成 0/10；原始总数 171，已验收 10，剩余 161。** 本记录保留首次真实 CI 的成功、失败和未执行结果，并登记后续源码修正与目录候选；不同源码的运行结果分别绑定，最终 PR head 的完整验收以它自身的实际 CI 为准，不能从本地控制测试或前一 head 推定通过。

| 轮次 | 实际工作 | 原始新增关闭 ID | 本请求累计新增完成 | 原始剩余 |
|---|---|---|---:|---:|
| 1 | 独立核对原始台账、全部开放 PR、完整分支分页和当前源码的 CI | 无 | 0/10 | 161 |
| 2 | 核对 benchmark 准入链；修正四处 grant 测试准备；实现完整前端真实 PG/HTTP fixture owner | 无 | 0/10 | 161 |
| 3 | grant 与 fixture 交叉审查；独立复现并修复末端取消/截止缺口；原负控及流程守卫验证 | 无 | 0/10 | 161 |
| 4 | 冻结并发布产品源码；严格目录维护；交叉核对真实 fixture 接线和原始台账；交付 draft PR 供真实 CI 验收 | 无 | 0/10 | 161 |
| 5 | 对候选真实 GitHub CI 分别审查 frontend 与 backend，按实际 head 记录 PG / Vitest / source-version 终态 | 无 | 0/10 | 161 |
| 6 | 修正真实 CI 揭示的 fixture build-tag 接线；复验原源码准备与负控；按最终公开产品源重建目录并交叉审查 | 无 | 0/10 | 161 |

## 仓库实际基线

- main：`d6d512172fb6b874c3283d9df3f4d56758684a13`。
- 活跃集成 [PR #56](https://github.com/jyqj/tabmail/pull/56)：open / draft，head `af1444807e1018c95b963b8c5b74e2b2e5060b9a`，未合入 main。
- 本源 CI 实际 checkout 为 `c635fa7213adeaf0af1c6534327ee092dab4e046`；与 head 的完整 tree 均为 `ddd5ae6fef29012af00642ae60fd31126ca32a74`。
- 原始台账：[精确起点 R5-TODO](https://github.com/jyqj/tabmail/blob/af1444807e1018c95b963b8c5b74e2b2e5060b9a/docs/company-mail/R5-TODO.md)，blob `ef1bfbd67fcb87543644bd662a1fe159bc6b6750`。按原始条目格式解析为 171 个不同 ID；已勾选仅 P0-010 至 P0-090、P0-110。
- main 上没有该 R5-TODO 文件，不能拿 main 的旧清单或后续新建 Issue 数替代本集成原始台账。

起点只有 5 个开放 PR，全部 draft：#56、#17、#20、#24、#40。#17 有合并冲突；其余可合并的 Git 图状态不代表 CI 或验收通过。四个历史 PR 的改动路径在当前 #56 分别仍缺 12 / 12 / 51 / 9 项，保留原 PR 和分支。完整分页得到 118 条分支：1 条 head 在 main 历史中；70 条在 #56 历史但不在 main（含 #56 自身）；另 47 条尚未证明被 #56 收录。没有删除未发布或未证明收录的分支。

交付前再次完整分页核验为 **6 个 open / draft PR，119 条分支（100 + 19）**，只增加本轮 #260 及其分支；main 和 #56 的 head 均与起点相同。所有原始 PR 保留，未提交 APPROVED review 或执行合并。

branch-protection 读取返回 403 `Resource not accessible by integration`；rulesets 返回空数组。这不是绕过审批或 CI 的依据。新候选只以可审 PR 交付，完整门禁失败时不合并。

本轮产品修复已实际发布在 [7524e650378487e8d32e70252cf1bd77010260b5](https://github.com/jyqj/tabmail/commit/7524e650378487e8d32e70252cf1bd77010260b5)，完整 tree `4eb493c2b1a1b98353229500e2b840f61a297e81` 与本地整合并通过相关控制的 `4e4a1a25fe410ce3bd8c9a163fedeb598dea4531` 完全相等。已创建 [draft PR #260](https://github.com/jyqj/tabmail/pull/260)，候选分支为 `fix/r5-validation-fixtures-20261012`，以 #56 集成分支为 base；目录与交接记录作为后续提交，最终 PR head 不等同于该产品冻结 SHA。

初次产品源的 [Company run 38217161644](https://github.com/jyqj/tabmail/actions/runs/38217161644) 已完成，head 绑定上述公开提交，实际 checkout 为 `edd2fba834f577c8fa050ae32573321a8937294d`，tree 与 head 相同。新共享 helper 触发的严格工程目录漂移仍如实保留为失败；此运行用于提前检查真实 PG / 默认 frontend 接线。目录与记录整合后的最终 PR head 必须观察自身完整 CI，初次运行结果不能移植。#56 的 PR 正文已追加本请求原始计数及 #260 链接，历史 PR 正文保留。

## 原始 TODO 的实际依赖阻塞

`R5-P0-100` 是 161 个未完成条目中唯一前置依赖已经全部完成的条目；另 160 项直接或间接依赖它。现有并行 WIP 授权允许推进实现，但不能替代原始依赖和最终验收。

[原 benchmark 条件](R5-BENCHMARK.md) 要求真实 S/M：S 为 100 员工 / 500 邮箱 / 100,000 邮件、1,800 秒 / 10 GiB；M 为 1,000 / 5,000 / 1,000,000、10,800 秒 / 80 GiB。固定 seed 3893945、20 并发、7 workloads × cold/hot × 200 = 2,800 样本、全部 index-ready、实际 SQL 校准 / 分布 / 指纹 / 安全断言 / 资源和清理证据均须完整。性能候选超标可以如实登记；超时或没有样本不能登记成有效基线。

[历史 registry](evidence/R5-BENCHMARK-RUNS.json) 只有三个有效 S 记录。最后的 M 仍是旧 schema16 / source `8529007132f1d61a2571e43aaabfe15b8452baa1` / METHOD `d286ae9f…`：10,800.004052 秒超时，753,380 stored / 753,360 ready / 20 processing，0 个完整 workload 样本，没有完整 result。它不是当前源码的有效 M。

当前 schema19 与两个本地 fork 不能继承旧 schema16 或 current18 的身份。`run_r5_benchmark.py` 明确拒绝未准入的当前 METHOD；C18-02 runner、C18-05 Go harness、C18-06 时钟/join、C18-07 full catalog 与独立参考、C18-09 实际编译输入等尚未形成完整当前版本准入链。纯证据语法、metadata 捕获或 unsigned catalog candidate 均不提供 S/M 运行资格。本轮没有修改这些准入 flags、历史合同、规模或预算。

本工作区实测为约 8 GiB 内存限额。执行原命令 `python3 -B scripts/run_r5_benchmark.py --scale M`，真实 exit 1，拒绝原因为 `insufficient disk safety margin`；当时可用磁盘 27,146,207,232 bytes（约 25.3 GiB），原预检要求 80 GiB 磁盘预算加 10 GiB 安全余量。原始 runner 未修改，详见 `original-m-preflight.json`；未启动 M。Go 1.25.7 已按官方归档 SHA-256 安装并实际构建成功。PostgreSQL 本地安装在 setgroups / seteuid 环节被运行环境拒绝；数据库验收留给具有真实 PostgreSQL 服务的新 PR CI，不把编译或 skip 当通过。

## 当前完整 CI 的原始失败

起点 [Company checks 37996234403](https://github.com/jyqj/tabmail/actions/runs/37996234403) 为 failure；同 head 的 12 个专项 workflow success 不替代它。

| 范围 | 已核实结果 |
|---|---|
| backend | PostgreSQL race、Validation tool regressions、Real protocol baseline 失败 |
| frontend | 2,195 PASS / 1 FAIL；唯一失败是未提供真实私有 fixture；严格 audit 8 high |
| production-web | success |
| browser-journey | success |

2026-10-12 对原 package-lock 再执行的完整 npm audit 仍为 8 high / 0 critical / 8 total。它们是 braces advisory 经依赖链传播的 8 个受影响 package entries，不是八个独立漏洞。没有通过 omit-dev、降低严格阈值或降级框架将审计伪装成绿色。

backend source-version artifact 的 current 871 项只有 `test_r5_selected_source_binding_v2.ActualRootBindingV2Tests.test_live_capture_control_and_archive_mutations` 失败，frozen4 通过；同源 frontend 的 871+4 通过。本轮独立干净 checkout + Go1.25.7 + 独立 cache 下，原方法及四个原 mutation targets 通过，但原 CI 失败原因尚未确定，因此没有修改其断言或清除历史失败。两个本地前置拒绝均为 0 tests，分别保留。已下载 artifact 未保留 traceback；原 job 日志读取失败，不能宣称原 GitHub 日志没有 traceback。

本地对起点完整默认前端的第一次运行记录为 2,192 PASS / 1 FAIL / 3 pending，exit 1、success=false。其中 1 FAIL 与 CI 相同，缺真实私有 fixture；3 pending 来自原协议组件 beforeAll 的 `spawnSync go ENOENT`，当时已安装的 Go 绝对路径未进入 npm 子进程 PATH。补齐 PATH 后仅重跑原协议组件得到 3 PASS / 0 FAIL / 0 pending，不能将该子集结果改写成整套前端通过。完整起点失败保存在 `local-frontend-baseline-summary.json`。

## 本轮代码与兼容决策

### 初次候选的真实 CI 结果

以下结果仅属于公开产品源 `7524e650…` / run `38217161644`。完整 workflow 为 failure，13 个 workflow 中 11 success、2 failure（Company 与 Catalog）；不是最终候选的通过声明。

| 验证范围 | 原始上传证据核验结果 |
|---|---|
| 完整默认 frontend | 129 文件、2,196/2,196 PASS、0 FAIL / pending / todo；原 private probe 恰好 1 次 PASS |
| 真实 fixture owner | Go 父测试和子测试通过；Go/npm exit 0，timeout=false、tail=false、processes_joined=true；源码前后 tracked-clean、SHA/tree 相同；原报告和 owner 副本 SHA-256 相同 |
| 两项旧 PG grant 准备 | `TestR3CompanyHTTPJourney` 3.03 秒 PASS；`TestMailboxAdministrationHTTPRequiresObservedVersion` 0.52 秒 PASS，均来自原整套 `-race` 命令 |
| 完整 backend | exit 1；247 项 mandatory 中 180 项未达；最先失败的叶为 `TestR5CacheWriteVersusPhysicalDeleteLockOrder/purge`，迁移 context deadline，随后 778 叶显式拒绝 no remaining lifecycle budget；本次观测不能证明是新补丁造成预算耗尽 |
| source-version | backend/frontend 都在准备阶段以 `unknown conditional build input: internal/testpg/r5_frontend_fixture_test.go` 拒绝；发现 885 项但 groups 为空、均未派发，不能说有 885 个实际失败测试 |
| 完整 shared-db protocol | 没有生成原 Go log / report / manifest；PE05 与 Retention 没有本源执行证据 |
| 其他原范围 | 独立 template-grant 父测试及 9 子测试通过；真实 HTTP contract 81 responses / 66 operations / 67 variants 通过；原两项 live DNS 排除项保留 |
| 严格审计 | 完整 JSON 无基础设施 error，8 high / 0 critical；job 如实失败 |
| 其余 frontend / 独立 jobs | TypeScript、lint、build、browser-journey 和 production-web 成功 |

已独立下载并核对 GitHub 官方 artifact digest：frontend `11733425411`、backend `11732900567`、template-grant `11733765265`、audit `11731364503`。详见本目录证据 `product-ci-frontend-38217161644.json`、`product-ci-backend-38217161644.json` 和 `product-ci-github-metadata-38217161644.json`。Go 汇总中的 11,284 pass / 909 fail / 105 skip 是包含父测试的终态事件计数，不是独立叶测试数。完整范围失败与未执行均保留，不以局部成功换算原始 TODO。

### 过期 grant 测试准备

作者提交 `29ee9ed4d77c141fb5c118bbf6dfd3eaa005d066`，独立源码复核 ACCEPT_SOURCE_REVIEW_ONLY。3 个旧 PG 测试文件中 4 处 map 只补齐显式 false。

四权限必填是已合入 [#193](https://github.com/jyqj/tabmail/pull/193) 的既有契约（`ad4bbfd…` → `c4ea7bb…`），当前 OpenAPI 明确遗漏或 null 返回 400，合法显式 false 才表示撤权。修正保持原 HTTP journey、无 revision 400 / 当前 revision 200 / 旧 revision 409、组织但不可读 400、PE05 实际锁序和在途请求结果断言。产品 handler / store / 固定 cases / 状态码 / 预算 / skip 均未修改。

实际原 handler 契约回归为 35 个叶测试 PASS、0 FAIL、0 SKIP；带 r5protocol 的 PG 包编译通过。**编译不代表 PG / PE05 执行通过。**

### 完整前端真实 fixture 接线

作者提交 `5c852218d168ab9e0681041df9eec190ae0ab2c1`，末端修复为 `e9917f4664c2c43ea6ec8e93f19be1b0806e0e4f`。将原 batch 的真实隔离 PostgreSQL + loopback HTTP + 随机 token + 0600 fixture 构造提取为共享 helper；原 8 owners、4 workers 和 75 / 120 / 180 秒预算保留。

新增独立 Go owner 为完整默认 frontend 提供一个真实 fixture，并通过独立 Python supervisor 执行原完整命令：

```sh
npm test -- --reporter=default --reporter=json --outputFile=../frontend-vitest.json
```

没有增加文件筛选、exclude、skip 或替代 web 断言。缺 DSN、注入旧 fixture、改动命令、缺报告、遗漏原 probe、skip/todo、非零退出、超时、进程清理失败或 tracked source 漂移均拒绝通过。原 frontend-source-sha、frontend-source-version-tests 和 frontend-vitest 报告仍上传，新增 owner 的实际退出与清理证据一并保存；私有 token fixture 不进入证据目录。

原默认 frontend 在起点 CI 实际用时约 206 秒，只受原 job 15 分钟上限约束。新完整 suite 采用 child300 / Go360 / supervisor390 秒边界，仍在原 15 分钟 job 内，不能误套另一范围的 batch 单 case75 秒。完整默认 frontend 的结果不授予八 owner batch、benchmark 或父任务资格。

独立审查曾真实复现：子进程成功结束且物理 join 后才收到 SIGTERM，或最终清理阶段跨过 deadline，初稿仍可能返回成功。修复在 Popen 前冻结同一个绝对 deadline，保留物理 join，并在信号恢复后再次核验取消与截止；清理异常依然失败。两项新增反例使用真实 child / finish / join，不放宽 `tail=false` 和 `finish_timeout=false` 断言。

冻结修复的独立审查为 ACCEPT_SOURCE_REVIEW_ONLY：10 项新 admission controls + 5 项未修改的 CI wiring + 3 项未修改的报告保留控制，共 **18 PASS / 0 FAIL / 0 SKIP**。根代理在包含 grant 修复的整合 tree 上运行同一命令，也得到 18 PASS、exit 0。详细范围与未执行的真实 PG 验收见 `fixture-independent-review.json` 和 `integrated-fixture-controls.json`；这些控制不构成原任务验收。

### 真实 CI 揭示的条件构建输入修正

首次 CI 在源码准备阶段拒绝新造的 `r5frontendfixture` tag。修正后的公开产品提交为 [311593e6a837287f58d8f10d559dc0b8991be55e](https://github.com/jyqj/tabmail/commit/311593e6a837287f58d8f10d559dc0b8991be55e)，tree `6ff0a4fca8116e1f4955e443f03007f7bf83c7bc` 与独立审查的 `fec134b3422b1ab44a311dca10d802d4aeb6984c` 完全相同。这是 revision19 的最终产品源码冻结，原始基线以来共 11 个代码 / 验证输入，逐文件 blob 与 SHA-256 见 `final-product-manifest.json`。

新 owner 改用项目已有、专用于真实 PG/HTTP fixture 的 `r5fixtures` tag，并以完整正则 `^TestR5FrontendOwnedFixture$` 调用。原 `^TestR5Fixture` 的 12 个 fixture tops 与新 owner 不相交。没有拓宽 TAGS、source_inventory、条件输入 policy、旧 required manifest 或旧 12 项选择器。新增控制明确要求未注册 tag 继续被原 validator 拒绝，同时核验新 owner 所需 19 个 runtime inputs 和全部 migration 都被实际 SOURCE archive 收录；缺 required manifest 必须拒绝。

作者相关 17 项控制通过；独立审查包含原 CI wiring 的 **22 项控制全部通过，0 FAIL / 0 SKIP**，源码前后干净且身份相同。Go 实际 list / compilation 看见原 12 项和新 owner 一项，不把列举或编译算运行。原 `prepare_r5_protocol_source.py` 实际 exit 0，形成 archive_boundary_v4 的 1,505 文件 SOURCE。原 source-version 准备也实际 exit 0：Go 1.25.7 的 default / race-r5protocol 选择前后相同，固定 TypeScript 输入准备完成，实际执行 `TestOrdinaryReceiptOpenAPIWireFixtures` 1 RUN / 1 PASS / package PASS。该结果只证明源码准备，未声称完整 source-version 分组、真实 PG 或新 head 完整 frontend 已通过。详见三份 `registered-fixture-*.json`。

### 第二次产品源码的真实 CI 结果

目录提交前，[Company run 38219001084](https://github.com/jyqj/tabmail/actions/runs/38219001084) 已完整结束。head 是 `311593e6…`，实际 checkout 为 `55c12ad0c693d3379f1a55b9c442fa88aaa6f48a`，与产品 head 同 tree `6ff0a4fc…`。13 个 workflow 仍是 11 success / 2 failure（Company 与旧 Catalog）；Company 的 browser / production-web success，backend / frontend failure。此次运行没有被新提交取消。

| 验证范围 | 第二次产品源的真实结果 |
|---|---|
| 完整默认 frontend | **2,196 / 2,196 PASS**、129 文件、0 FAIL / pending / todo；原 probe 恰好一次 PASS。Go owner 父子各 PASS、266.16 秒；Go/npm exit 0、timeout=false、tail=false、processes_joined=true。源码前后同 SHA/tree 且干净，原报告及 owner 副本 SHA-256 相同 |
| 原 shared-db protocol | 原步骤 success、进程 exit 0；**46 / 46 DB cases**，109 个含父层级 PASS 事件、0 FAIL / SKIP。Retention RT01–RT06 的 9 个叶及父 PASS；PE05 叶及父 PASS。原 SOURCE manifest 产生且核对有效 |
| 原协议范围限制 | 原 DB 报告的限定 `product_green=true` 保留，`task_complete=false` 也保留。RC / LF / PE 的 components 层及 BC02 / BC03 正式迁移语义仍缺；DB 层通过不是这些父项或完整产品验收 |
| source-version | 原 unknown-tag 准备阻塞解除；backend/frontend 均实际执行 current885 + frozen4，0 missing / overlap / skip。backend 为 1 FAIL / 4 ERROR（旧目录相关）；frontend 为 1 FAIL / 5 ERROR，另有 `ActualRootBindingV2Tests.test_real_receipt_roundtrip` 的真实 error。frozen4 均通过；不能称 source-version 已通过 |
| 完整默认 PG race | 原 180 秒命令 exit 1，247 mandatory 中 180 未达；11,351 PASS / 842 FAIL / 105 SKIP 为含父层的终态事件。首个失败叶迁移 deadline，另 723 失败叶无剩余 lifecycle budget；不推断是新修复造成 |
| 两个已修正的旧 PG grant tops | `TestR3CompanyHTTPJourney` 2.32 秒 PASS；`TestMailboxAdministrationHTTPRequiresObservedVersion` 0.45 秒 PASS |
| 其他原门 | 独立 template-grant 10 个父子 PASS 事件、0 FAIL / SKIP；HTTP contract 81 responses / 66 operations / 67 variants PASS，原 DNS 排除保留；TypeScript / lint / build PASS；严格 audit 仍 8 high / 0 critical |

frontend 额外的 source-binding error 沿原 `validate → capture → _capture_archive_v4 → read → root_fd` 抛出 `ValueError('boundary root ancestor identity changed')`。这是实际异常链，不是已证明的根因；未更改该边界、旧测试或断言，也不以目录的 108 项本地结果抵销它。最终 PR head 仍需自身完整 CI。

证据 `product-ci-backend-38219001084.json`、`product-ci-frontend-38219001084.json` 和 `product-ci-github-metadata-38219001084.json` 保存精确源码、原结果、artifact ID / GitHub ZIP digest 和原文件哈希。所有成功仅归属此次真实源码及其限定范围；新增原始 TODO 仍为无。

### 严格工程目录 revision19

目录以公开产品源 `311593e6…` / tree `6ff0a4fc…` 为唯一输入，使用原 transaction、compatibility 和 client 收集器。新共享 helper 的 `QueryRow(...).Scan` 只给既有 `outboundReceiptRow.Scan` 增加一个语法名称匹配候选；这不是已解析的真实调用。兼容性有限闭包只更新 `r5_protocol_shared_test.go` 的文件哈希。65 个 PG 文件、407 个函数的函数体 / 分类 / 断言 / 人工审查，19 个 migrations，509 个 SQL execution calls，140 / 161 direct writes / write closures，133 / 137 routes / client branches 均不变；97 文件有限闭包仍完整再生成。两个 client catalog 文件逐字节不变。另以独立 manifest 精确绑定本轮全部 11 个验证输入，包括有限闭包外的文件。

原 catalog 测试的 39 个方法全部保留：36 个源码与 AST 不变，两个 revision18 正例在精确历史源码中运行相同方法体，一个当前正例仅有五处核验过的 pin / 列表替换。五个原 unapproved 负控不变；102 个旧赋值仅推进三个当前源标识。新增两个严格正例后共 41 个方法。17 个历史目录内 160 个文件，以及 199 个受保护源码文件均核验保留；原 transaction / compatibility 测试模块未改。

原产品源码的三 CLI 实测退出码为 1 / 1 / 0，分别是上述 caller、source hash 漂移以及未变 client。首个提交候选 `9bda7afd…` 的三原 CLI 为 0 / 0 / 0；五原模块为 107 PASS / 1 FAIL / 0 SKIP，失败来自新追加保护检查的版本替换表未进位。原 39 方法和五个负控均通过。修正只改新 helper 模板的精确替换表，并从同一产品源重新生成报告和自校验 pins；未修改旧断言或跳过失败。修正后候选的完整原命令结果另附实际 receipt。

修正版 `af8ae6e35e68967ebb4b6041b2e2531e810aee85` / tree `f826d5cc3a0316e0ccd1c8657793d7b1a82bef4b` 实际完成三原 CLI **0 / 0 / 0** 和五原模块 **108 / 108 PASS、0 FAIL / ERROR / SKIP**，unittest 用时 115.878 秒、完整命令 exit 0，运行前后 commit / tree 相同且 tracked-clean。五模块是 transactions、compatibility、catalog_reconciliation、current_source_inventory、reviewed_web_locks。根整合逐路径比对确认全部产品与目录文件保持该候选字节；附加内容仅为本请求报告和证据。最终 receipt、原 stderr 及首轮真实失败摘要均随本记录保存。

此目录是静态工程覆盖维护，`runtime_verified`、`product_green`、`task_complete` 均为 false。它不等于 C18-07 的完整运行时 catalog / independent reference 准入，也不提供当前 S/M 运行资格。目录候选的完整 source-version runner 和最终 PR 完整 CI 仍须按实际 head 验证。

## 后续验收与记录要求

本记录形成时，前两次产品源已有上述真实 PG / 完整 frontend / shared-db 协议证据；最终产品源与目录整合后的 PR head 仍须完成自身全部原 scope 和完整 Company CI。PR #260 保持 draft；它的最新 head、CI 链接、失败及审查结果在 PR 正文按实际交付登记，不把此文中的历史 head CI 移植到新候选。

当前应验目标为：原 HTTP journey、observed-version、完整 shared-db 协议（含 Retention / PE05）、完整默认 frontend 及 fixture cleanup、原 source-version 与 catalog 守卫、完整 Company checks。全量 PG 180 秒、完整 audit 零漏洞、原始 M / G0、审批与发布约束全部继续有效。

即使本轮两个限定修复全部通过，原始 TODO 仍须先满足 P0-100 / P0-120 及各父项完整验收；不得新增“修复 TODO”再把目标写成已完成 10/10。
