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

本核心提交阶段，revision12 完整 source-version runner **尚未执行**。先发布精确目录核心，再以公开核心 SHA 在干净 checkout 执行原 runner，随后只追加真实报告、原始输出和独审结果。不得预先写成通过，也不得用 revision11 冻结的历史运行替代。

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
