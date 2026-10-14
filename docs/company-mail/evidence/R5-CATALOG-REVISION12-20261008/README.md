# R5 目录修订 12：三轮 PROGRESS 最终公开源码对账

本修订处理 [Issue #166](https://github.com/jyqj/tabmail/issues/166)，将四份当前目录绑定已公开合入的三轮产品源码。**本次目录维护新增完成的实现 TODO 为 0**。`task_complete`、`runtime_verified`、`product_green` 均保持 `false`；原 R5 父任务仍为 **10/171，剩余 161**。PROGRESS 批次十项的产品验收与各轮证据由其独立任务登记，本目录不重复计项，也不把语法对账当作并发、旧客户端升级、发布或 G0 验收。

## 公开来源

| 用途 | commit | tree |
| --- | --- | --- |
| revision11 不可变目录快照 | [`58d0c9cf274569258319ff4b5dcab7912b0174f4`](https://github.com/jyqj/tabmail/commit/58d0c9cf274569258319ff4b5dcab7912b0174f4) | `e4c4550481d9dc9efc5d4d849f85d496b99f65c4` |
| revision11 审查的产品源码 | [`33f61fde3cf37d3cec2dd360146369c291eb0930`](https://github.com/jyqj/tabmail/commit/33f61fde3cf37d3cec2dd360146369c291eb0930) | `d982493e28431e644fd4018d8f746d8940280268` |
| revision12 最终产品源码 | [`ed81ee2fcadc9e8cfcd165947561f8b4b65589e6`](https://github.com/jyqj/tabmail/commit/ed81ee2fcadc9e8cfcd165947561f8b4b65589e6) | `3abdbfca4c0318644a01755c6e6bbb1929b74633` |

最终产品源是 [PR #167](https://github.com/jyqj/tabmail/pull/167) 的实际合并提交。目录收集在精确、干净的 `ed81ee2` checkout 上执行；收集前后 HEAD、tree 和工作区状态一致。所有历史读取只使用已公开的完整 Git 对象，缺失对象仍报错，不从当前同名文件回退，也不把私有中间 commit 写成公开来源。

## 原 producer 的当前事实

| 事实 | revision11 | revision12 |
| --- | ---: | ---: |
| PostgreSQL 源文件 | 63 | 63 |
| PostgreSQL 函数 | 401 | 401 |
| 字面 SQL 写函数 | 138 | 138 |
| 按名称推导的写调用闭包函数 | 158 | 158 |
| SQL 执行表达式 | 501 | 501 |
| migration 文件 | 19 | 19 |
| 注册路由 | 133 | 133 |
| 客户端调用分支 | 135 | 136 |
| 显式 transport forwarder | 7 | 7 |
| 原兼容性 producer 的有限源码闭包 | 95 | 95 |

401 个 PostgreSQL 函数的完整语法、分类、人工字段、风险说明、既有证据来源、63 份文件 hash 和 19 份 migration 均保留。只有 **11 组 caller 候选**随已批准产品源码发生变化，完整前后数组与摘要见 [reconciliation.json](reconciliation.json)。10 组候选数量不变，主要是调用位置变化；`postgres.New` 的按名称候选从 179 增至 180，因为 configcache 新增了 `list.New`。它继续明确标为 `name-match-candidate-not-dispatch-proof`，不能把标准库的同名调用写成新的数据库调用或事务执行。

客户端按文件、归一化路径、HTTP 方法、forwarder 和分支组成的多重集核对，旧 135 个逻辑调用全部保留。新增的一个分支是 `DomainSettings` 显式复核中的 `GET /settings`；原初始化 GET 与保存 PUT 仍保留。邀请撤销的 AST owner 与编码表达式变化，placeholder 从 `{dynamic}` 变为 `{id}`，仍关联同一注册 DELETE 路由。审计、邀请、邮箱管理与域名设置的其他 owner/行号变化按原 AST 实际结果登记。

133 条路由注册、API matrix 和路由集合不变；**7 个 route rows 仅变化 `clients` 字段**。每条路由原有的 `legacy_client_disposition`、`runtime_evidence_scope` 及其他非 producer 人工字段全部保留，没有把已发布客户端或未知外部使用声明为不存在。既有 release batches、后续任务绑定、依赖审批、历史 wire 引用及各限制说明也保持原资格。

有限 95-file closure 有 **10 个 hash 变化**，其中包括重新登记的 clients JSON 和 OpenAPI。原产品路径过滤规则收集 **18 个 source paths：8 个在闭包、10 个在闭包外**，明确包含 `web/locales/en.json` 与 `web/locales/zh.json`。OpenAPI YAML 不在该原过滤规则中，因此另以 `additional_api_schema_changes` 记录精确前后 commit/blob/SHA256/长度；它仍在原兼容性闭包内。`web/package.json` 与 `web/package-lock.json` 没有本批变化，`additional_build_metadata_changes` 为空。

闭包外的 10 个产品路径包括 app 内容/域名服务、configcache、DKIM、MIME 源完整性、metrics、admin 页面和两份 locale。目录逐一记录其源码 hash，保留它们不属于原有限 closure 的边界。stats 可选 aggregate 的 Go/TypeScript/OpenAPI 行为应读取产品独审和契约测试；当前目录不以 schema 名称仍相同推定字段语义或线上兼容性已通过。

## 历史与原门禁保留

revision1–11 的十组历史目录共 **65 文件**，文件集合、blob、SHA256 和长度全部逐字固定于公开 `58d0c9cf`。四份 revision11 当前目录另外通过公开 commit/path/blob/SHA256/长度读取，不覆盖原历史报告、负面运行结果、人工说明、wire 快照或模块 marker。

原五个 AST producer/CLI、原完整 source-version runner、Go 选择规则、TypeScript 准入和其他 **167 份保护源码**保持公开最终产品源的字节。原五个 `test_unapproved_*` 方法的源码与 AST 完整保留；原 caller、函数体、client、route 和 closure 变异仍必须被拒绝。当前 catalog 正例由 25 个方法变为 27 个，保留全部原方法。

revision11 原来跟随当前树的两个正例，现在在独立 `git clone --no-hardlinks` 中 checkout 公开 `58d0c9cf`，核验精确 HEAD/tree/clean 状态，再实际运行原 Go/TypeScript 收集器。两方法的原断言体 AST 完全不变；只在真实冻结来源上下文中执行。共享的仅是已安装 TypeScript 编译器，未共享当前应用源码或伪造历史 producer 输出。两个新增 revision12 正例检查实际 fresh facts、全部人工字段保留、完整来源范围、65 个历史文件、167 份保护源码、原负例和原 revision 常量。

事务小正例保持原字节。兼容性小正例只将当前客户端计数 **135→136**；路由仍为 133。所有历史常量保持其原值，原分组 runner 的四项 frozen-v1 归属也保持原规则。

## 已执行验证

原三条 CLI 在干净最终产品源携带 revision11 旧目录时实际得到 **1/1/1**：事务门禁报告 11 组 caller drift；兼容性门禁报告 clients 与受审目录不符；client CLI 报告调用数量漂移。三条原始 stdout/stderr 及耗时逐项保存于 `baseline_validation`。这是原门禁拒绝真实过期登记，不是修改检查器或伪造 producer 的 RED。

重采、保留人工登记并加入当前/冻结正例后，原三条 CLI 实际 **0/0/0**，Python 五模块实际 **94/94 PASS、0 failure、0 error、0 skip**，外层耗时 79.359 秒。包含全部 **27 个**目录 reconciliation 方法及五个原 `test_unapproved_*` 负例；原 route 变异仍到达其具体 producer 拒绝。每条命令使用同一候选目录，记录的全部当前目录与测试输入 SHA256 在执行前后相等。参见 [Python 原始日志](current-python.stderr.txt) 与[执行元数据](current-validation.json)。

本节的实际执行身份是公开产品 `ed81ee2` 上的未提交目录候选，候选内容由执行元数据中的完整文件 hash 固定。不能把这一范围写成之后公开 core 的完整 source-version 运行。

准备环境复用了既有 Go 1.25.7、只读 module cache 与隔离构建 cache。Python 使用自有环境，`scripts/requirements-contract.txt` 的九项固定版本均实际存在；未向公共解释器安装依赖。TypeScript 使用既有 5.9.3，完整 runner 仍执行其原编译器/lock/文件集合准入。运行环境、真实命令、耗时、输出摘要以及候选输入的前后 SHA256 均保留在执行 packet；没有为环境问题改动原准入门槛。

## 完整 source-version runner 与独立审查

### 本机：公开核心上的完整原始运行

目录核心先实际发布为 [PR #168](https://github.com/jyqj/tabmail/pull/168) 的公开 head [`b87df7a77656457549d6f2d678c56d321ae0d869`](https://github.com/jyqj/tabmail/commit/b87df7a77656457549d6f2d678c56d321ae0d869)，tree `312dbed6634b7b173e6ec9e180bbf5d0f1809d9d`，parent 是产品源 `ed81ee2`。它包含已验证目录核心和仅两份中央进度文档的更新。本机在这个精确、干净的公开 core 上执行**一次原完整 runner，外层耗时 289.383 秒，exit 1**；执行前后 HEAD/tree/clean 状态一致。

原 preparation 实际成功：Go 1.25.7 的 default 与 race-r5protocol 两种选择前后一致；从原官方 URL 取得的 TypeScript 5.9.3 archive 通过原 SHA256、integrity 和完整 132-file 检查；`TestOrdinaryReceiptOpenAPIWireFixtures` 经原 pinned proc FD 实际运行通过。这一准备成功单独记录，未算入 Python 方法通过数。

| 原派发组 | 分配/加载 | 实际方法 | 通过方法 | 失败方法 | 错误方法 | skip | 未执行方法 |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| current，公开 `b87df7a` | 808 | 801 | 792 | 4 | 5 | 0 | 7 |
| frozen-v1，原 `41b015c` | 4 | 4 | 4 | 0 | 0 | 0 | 0 |

原始 [source-version-tests.json](source-version-tests.json)、[执行元数据](source-version-execution.json)、[stdout](source-version.stdout.txt)、[stderr](source-version.stderr.txt) 和仅从原 ID 派发/结果计算的[方法统计](source-version-summary.json) 全部保留。共发现 812 个 ID，分组无缺失且不重叠；实际执行有 7 项未派发，所以原 `no_missing=false`、`no_overlap=true`、`status=failed`。这 7 项未执行不是 skip，更不是 PASS。

current unittest 原汇总为 **4 failure、7 error occurrences**。七次错误包含五个实际方法、一个重复的 Unix socket subtest 错误和一个 `setUpClass` 错误，不能用 801−4−7 计算方法通过数。原 runner 的 27 个 catalog 方法及五个 `test_unapproved_*` 均实际通过；嵌套负例自己打印的 FAIL/skip 文本不算作外层结果。

| 原始失败范围 | 实际结果与可证明边界 |
| --- | --- |
| 三项 ColdWeb 假构建测试与一项错误 Node major 控制 | 三项 ERROR 均由 `node-version` 阶段的 `tool version command failed` 提前中止；第四项原本期待 `Node 22 required`，却收到同一提前错误而 FAIL。原测试注入临时 fake node/npm，这些结果不能直接证明安装的 Node 版本错误或真实 Next build 失败。 |
| 三项进程清理控制 | benchmark 的 `cleanup_live_processes_absent`、两项 ColdWeb 的 `cleanup_verified` 实际为 false，三个原断言因此 FAIL。未将未知清理状态视为成功。 |
| 两项 Unix socket fixture 方法 | 三次错误均在创建 `socket.socket(AF_UNIX)` 时收到实际 `EPERM`，两个 fork subtest 各一次，另一个方法一次。 |
| ActualRootBindingV2Tests 类初始化 | 原 `r5_archive_boundary.root_fd` 在检查祖先描述符身份时报告 `boundary root ancestor identity changed`；该类七项方法未启动。原报告没有记录具体哪层或哪个身份字段变化，不能把它与上述进程问题合并归因或推定只是负载。 |

[单独环境观察](environment-observations.json) 保留 root 在另一次诊断中真实观察到的 `ps` self lookup 失败及 PID 与 `/proc/self` 身份不一致，也保留之后 `/bin/ps` 成功的对照。仅针对新拥有的子进程尝试私有 namespace 时，原环境以 `uid_map: Operation not permitted` 拒绝；没有升级权限、改系统 `/proc`、替换 ps 或修改原 guard。该诊断说明进程观察不可靠，**不是首次失败的 per-test receipt 重建**，也不解释独立的 AF_UNIX 与 ancestor identity 错误。

原 ColdWeb 每例临时目录由 unittest 的 `addCleanup` 删除；benchmark 失败断言发生在 receipt 持久化之前。因此首次测试中未保留的临时 receipt 没有事后补造；完整原 trace、返回结果和来源仍可核验。没有在本机重复整套 runner 掩盖失败。

### 公开核心的独立目录审查

storage reviewer 对相同公开 `b87df7a` / tree `312dbed` **ACCEPT**：在独立干净 worktree 中，原三条 CLI 实际 **0/0/0**，原五个 Python 模块 **94/94 PASS、0 failure/error/skip**，外层耗时 **89.160 秒**；其中包括 27 个 catalog 方法与五个原始拒绝测试。执行前后源码身份与 clean 状态一致。完整原[独审回执](storage-review.json)、[静态核对](storage-static-review.json) 与[Python 原始日志](storage-python-related.stderr.txt) 已保留；该 reviewer 没有执行第二套完整 source-version runner。

frontend reviewer 在从网络取得的独立 public clone 上对相同公开 core **ACCEPT**：原三条 CLI 实际 **0/0/0**，完整 **27/27 catalog 方法 PASS、0 failure/error/skip**，unittest 耗时 **41.109 秒**，包括实际执行五个原 `test_unapproved_*`。没有把作者或 storage 的 94 项扩写为本 reviewer 的范围。原[独审回执](frontend-review.json)、[命令与环境](frontend-execution.json)、[目录测试日志](frontend-catalog27.log)、[完整源码核对](frontend-source-review.json) 和[公开来源证明](frontend-public-provenance.json) 均以原字节保存。

这个 public clone 没有 object alternates、partial clone、promisor pack 或共享 pack hardlink；公开 `58d0c9cf`、`9bcc542`、`f271336` 可直接读取，历史的私有中间对象实际不存在。原门禁仍通过，证明历史复现不依赖本机私有 Git 对象。reviewer 辅助审计脚本最初使用错误的 schema 字段而提前停止，修正辅助脚本后完成源码核对；该自有 helper 错误单独记在回执中，没有改产品、原 collector 或测试规则，也没有把它称作原测试失败。

两份独审共同核对 65 份历史文件、167 份保护源码、原 25 个方法及五个负例、两个冻结 revision11 断言体、401 个函数的全部非 caller 字段、133 条路由人工字段和 135 个旧客户端逻辑分支。两人均未另跑完整 runner，以上 ACCEPT 只属于各自已记录的目录审查范围。

### GitHub：同一完整树的原完整 runner 通过

[CI run 37759167593](https://github.com/jyqj/tabmail/actions/runs/37759167593)、frontend job `113251091189` 的原 source-version 步骤实际通过，原 Catalog/TypeScript 步骤也通过。取回 artifact `11542325253` 时已核对完整 ZIP SHA256 `1ab13d395f6c9e4ad25c6d818998fdb7043501dbd1547a7a49e1b163c9c5c286`；原[执行 SHA 文件](ci/frontend-source-sha.txt)、[完整 runner 报告](ci/frontend-source-version-tests.json)、[解码完整 job 日志](ci/ci-frontend-decoded.log)、[来源核验](ci/provenance.json) 和[范围摘要](ci/root-summary.json) 原样保留。

CI 实际执行 SHA 是 **`83c91dff12bc470b613102534d5784cad5df2d1e`**，来自 `refs/pull/168/merge`；不是本机的 `b87df7a`。已实际 fetch 并核对其两个 parents 为 `ed81ee2` 与 `b87df7a`，完整 tree 仍为 **`312dbed6634b7b173e6ec9e180bbf5d0f1809d9d`**，与公开核心逐文件完全相同。原报告不把 CI 执行身份重写为本机 SHA。

| CI 原派发组 | 分配/加载/实际 | 通过 | failure / error / skip / missing |
| --- | ---: | ---: | --- |
| current，实际 `83c91df` | 808 | 808 | 0 / 0 / 0 / 0 |
| frozen-v1，原 `41b015c` | 4 | 4 | 0 / 0 / 0 / 0 |

CI 共 **812/812 实际方法 PASS**，原报告 `status=pass`、`no_missing=true`、`no_overlap=true`；没有 expected failures 或 unexpected successes。原 current/frozen unittest 分别耗时 **203.760 / 13.787 秒**，准备阶段另计。该成功提供原完整源码检查在相同完整树上的真实通过证据，同时保留本机那次失败的原始结果、环境限制和未执行范围；没有替换原 ps、改变源选择、排除失败方法或放宽门槛。

本次维护的通过依据是四份目录与实际源码对齐、三条原 CLI、两份各自明确范围的独审，以及同树 CI 原完整 runner 的实际通过。**整体前端/发布门禁仍未全绿**：同一 CI 的完整 UI 为 1758 PASS / 1 FAIL，唯一失败是原 `explicit private fixture required` 控制；依赖审计仍有 8 high。完整 Vitest JSON 保留在原 artifact，公开摘要明确该范围。目录维护没有关闭或伪造私有 fixture，没有把 source-version 步骤的通过写成整份 workflow、生产部署或 R5 父任务完成。

最终追加仅涉及本 revision12 的 README、reconciliation 和原始证据文件。已执行的公开 core 中四份目录、产品源码、脚本、测试、原 collector/guard 和 revision1–11 历史字节保持不变。

复现使用原命令和已安装的固定依赖：

```sh
export R5_TEST_GO=/path/to/go1.25.7/bin/go
export R5_TEST_CACHE=/path/to/isolated-go-build-cache
export R5_TEST_MODULECACHE=/path/to/readonly-go-module-cache
export GOPROXY=off
export PYTHONDONTWRITEBYTECODE=1

python3 -B scripts/check_r5_transactions.py
python3 -B scripts/check_r5_compatibility.py
node scripts/collect_api_calls.cjs --check
PYTHONPATH=scripts:scripts/tests python3 -B -m unittest \
  test_r5_transactions test_r5_compatibility \
  test_r5_catalog_reconciliation test_r5_current_source_inventory \
  test_r5_reviewed_web_locks -v
python3 -B scripts/run_r5_source_version_tests.py \
  --root . --output /private/source-version-tests.json
```

这项维护不改业务真实 PostgreSQL、旧客户端升级、M/G0、发布或父任务验收门槛；目录同步的通过范围与产品各项验收分别记录。
