# R5 目录修订 11：COMPLETE 最终产品与并行 CONTINUE 整合

本修订处理 [Issue #149](https://github.com/jyqj/tabmail/issues/149)，同步当前源码事实与目录，**新增完成的实现 TODO 为 0**。`task_complete`、`runtime_verified`、`product_green` 均保持 `false`；R5 原父任务仍为 **10/171，剩余 161**。目录通过不代表业务行为、并发、升级、发布或父门禁验收。

## 固定来源

| 用途 | 公开提交 | tree |
| --- | --- | --- |
| 上一版目录的不可变快照 | `20c39ab3ebaee46e5ac51e25d90a455166c9a31d` | 通过 Git 对象验证各目录 blob |
| revision 10 当时审查的产品源码 | `703a572864296120fff3efe0880c560ad9c74d57` | `40e1d2d16258e0e2bc3afea29553dbb148920e4e` |
| 本批十项 COMPLETE 的永久产品范围 | `4d2de05aa4bda226840fd591f2f356e8dd39ac20` | `52c8b95130b31d75b53d01010b333621ffc644eb` |
| 本目录最终审查的整合源码 | `3a103cda4363cb8f32839eaaa73ab065eb967f79` | `5e454667e0c99e816d20c499514d0e17f645bd22` |
| 本目录核心候选与完整 runner 的执行源 | `8e4fd58f63f4e111eca8e5482ae91abbec0484b2` | `c86a86f0a252159e068c8458e343eb59248ff979` |

`3a103cda` 还包含已批准的 CONTINUE #133/#134/#135：整数配置校验、原始租户覆盖快照、RCPT 缓存时效。它们进入当前目录，**不计入本批十项 COMPLETE 的完成数**。先前在 `4d2de05` 上执行的原 CLI 拒绝结果及原始 AST/routes/clients 已作为中间记录保留；最终四目录重新采集自 `3a103cda`。

[reconciliation.json](reconciliation.json) 记录四个旧目录的 commit/blob/SHA256、完整来源差异、每个变更条目的前后摘要、当前拒绝结果、历史文件与受保护源码清单。[function-review.json](function-review.json) 保存五个新增 PG 函数逐项人工审查。

## 当前事实与变化

| 原始 producer 的事实 | revision 10 | revision 11 |
| --- | ---: | ---: |
| PostgreSQL 源文件 | 62 | 62 |
| PostgreSQL 函数 | 395 | 400 |
| 字面 SQL 写函数 | 134 | 138 |
| 按名称推导的写调用闭包函数 | 154 | 158 |
| SQL 执行表达式 | 499 | 501 |
| migration 文件 | 19 | 19 |
| 注册路由 | 132 | 133 |
| 客户端调用分支 | 134 | 135 |
| 显式 transport forwarder | 7 | 7 |
| 原兼容性 producer 的有限源码闭包 | 94 | 95 |

旧 395 个 PG 函数体 SHA256 和全部 19 个 migration 定义保持不变。19 个函数只有位置变化；67 个函数的 caller 候选发生变化，72 个旧条目的完整差异均逐项列入 packet。`queue.go` 的公共文件说明及 19 个旧条目的共享风险说明同步区分当前 Claim mark 与仍保留的 ID-only 接口；原人工身份、分类、历史证据等级及历史运行材料保持原范围。

### 五个新增 PG 函数

四个 Claim mark 将固定 SQL 交给 `markQueueClaim`：在独立事务中先取得目标单行 `FOR UPDATE`，再用第二条语句检查 `processing`、匹配的 `attempt`、`lease_until > clock_timestamp()`，更新对应状态后提交。ID/attempt 无效、行不存在或确认已失去所有权时返回 `store.ErrClaimLeaseLost`；取消和数据库错误保持真实原因。hooks 的生产 adapter 明确传递 claim 返回的 ID/Attempts，且不回退到旧 ID-only 方法。

人工说明覆盖 outbox 的 done/retry、delivery 的 delivered/retry/dead、实际调用者、清理哪些 claim 字段、FK 来源、锁后校验、提交及回滚边界。**原分类不被人工改写**：四个入口含 SQL 字面量，被原 producer 分类为 direct-write/explicit-lock；helper 通过参数执行 SQL，其原分类仍是 transaction-or-callback、dynamic SQL，且字面 direct-write/write-closure/explicit-lock 为 false。这不表示 helper 只读或无锁。

冻结 PG 测试共 78 个不同叶用例，按四个入口分配为 15/15/16/32；helper 覆盖的是同一组 78，不能再重复加总。测试包括成功、失权、新一代结果、入口取消、UPDATE 约束拒绝、真实等待行锁直到数据库租约过期。这里仅链接既有测试，**本次目录审查没有运行 PG**。提交结果不确定、等待中取消、所有父删除/FK 交叉等待、任意系统时钟偏差、接收方去重均不由本目录证明；不能宣称 exactly once。

### 客户端与路由

原 134 个逻辑调用全部保留。草稿页 GET/DELETE 从旧数组位置 47/48 换为新位置 48/47；路由映射跟随方法与路径重新生成。删除调用抽到 `remove(draft)`，仍传原 revision；没有把 GET、DELETE 或条件下载分支互相混用。activation 的 AST owner 变为 `response`；消息窗格及两个 base.ts forwarder 的位置变化保持各自方法、分支与 forwarder 身份。

唯一新增客户端调用在位置 66：`getTenantOverrides` 发起 `GET /api/v1/admin/tenants/{id}`。新增接口处于原 Auth/PermissionLoader/RequireSuperAdmin 边界，读取显式 tenant ID 的原始覆盖配置，返回 `tenant_id` 和七个必现 nullable 整数；不执行写入。最新 API matrix 的 authority/resource/version/audit/content/evidence 字节直接保留，未重新生成该矩阵。

46 条既有 compatibility 路由的变化只涉及 clients 或 route_line；新增一条 GET。原 schema、middleware、release batch、旧客户端处置、依赖与 runtime 范围均保留。并行新增的专用 contract checker 负责原始快照 DTO 与 GET 契约检查；原 compatibility producer 对新路由仍报告空的 named Go/TS DTO binding 和自身未登记路由运行 fixture，不能把别处的测试抬升为本目录的 wire 或权限运行证明。

有限闭包只新增 `internal/api/handlers/admin_tenant_override.go`。34 个完整产品源码变化中，9 个处于此闭包，25 个在其外；新 overrides dialog、admin DTO、resolver、workqueue、monitor、mailcontent 等仍明确列在完整来源差异与排除清单中。不能由 95 个文件推断整个产品没有其他变化。

## 历史与检测规则保留

revision 1–10 的九组历史目录共 **47 文件**，文件集合、blob 和 SHA256 均保持原字节。原历史运行记录、wire 快照、归档边界 registry、Go 模块 marker 不重签、不修改。

原 `check_r5_transactions.py`、`check_r5_compatibility.py`、`collect_api_calls.cjs` 及两套底层 AST producer 保持字节不变。上一快照至 `3a103cda` 的受保护源码按旧口径从 164 到 169：两个已批准的修改为 `r5_source_runner_prepare.py` 和 `check_contract_drift.py`，另有五个已批准的新 workflow/runner/test 文件；没有删除。这些变化分别来自 COMPLETE 的 procfs 修复和 CONTINUE 的新快照契约，明确记录来源。

本次只演进三个当前正例文件：事务计数 395→400、兼容性计数 132/134→133/135，以及目录修订正例。其余 **167 个**脚本、workflow、collector/route test 文件严格对齐 `3a103cda` 字节。小计数正例的全部其他字节受精确比较保护。目录测试保留原 23 个方法，增加两项当前修订正例；五个 `test_unapproved_*` 的源文本及 AST 完全不变。旧修订对当时受保护源码的验收改为读取其原公开 Git 快照；旧期待值、历史 SHA 和验收项目保留，当前源码由新增 revision 11 正例独立绑定。缺失历史 Git 对象仍然是错误，测试不抓取、不跳过、不退回同名当前文件。

## 验证与复现

基线：在未改目录的 `3a103cda` 上，三套原 CLI 全部实际拒绝旧目录，分别报告缺失五函数/调用者和位置漂移、客户端 producer 库存不一致、客户端目录数量漂移。其命令、exit code、输入前后摘要与原 stdout/stderr 摘要见 packet。

当前核心候选的原 3 CLI 均 exit 0；Python 四模块 **85/85 PASS、0 failure、0 error、0 skip**；原 Node collector 测试 **27/27 PASS**；原 archive boundary 在真实 TypeScript 目录上 exit 0。每条命令的候选输入前后 SHA256、原输出摘要、环境选择和耗时列在 `validation.current_commands`。

两个首次未通过的开发检查也保留：archive 正确拒绝作者创建的 `web/node_modules` 软链接，随后仅替换为同版本 132 文件真实目录；Python 84/85 通过，唯一失败来自新增 revision11 正例对相邻 caller 行号进行就地移动。修正先消耗原位置再集中加入移动项，保持唯一匹配与完整多重集合比较，同一 85 项复验全部通过。两次修正均未修改原 CLI、原 producer、历史检测或五个未批准变更负例。

### 两份独立审查

[独立审查记录](independent-review.json) 绑定同一核心 `8e4fd58` / tree `c86a86f`。frontend reviewer 与 storage reviewer 分别执行完整 25 项目录测试，均 **25 PASS、0 failure、0 error、0 skip**。第二位 reviewer 还在独立工作区用同样三条原 CLI 实际得到 `3a103cda` 旧目录全拒绝、核心新目录全通过。两位均检查原五个负例、47 个历史文件、167 个固定当前源码、精确计数正例、完整 caller 多重集合和新增接口/PG 人工说明。

第一位 reviewer 的首次命令漏设 `PYTHONPATH`，导致 unittest loader 的 `ImportError(check_r5_transactions)`；**25 个实际目录测试均未派发**。其原始导入拒绝日志单独保存，随后按照本 README 环境运行原命令全通过，期间源码及目录未变。

### 原完整 source-version runner：本机实跑未通过

在干净 `8e4fd58` 上只执行一次原完整 runner，耗时 **494.6 秒，exit 1**；执行前后 HEAD、tree 和 Git clean 状态一致。preparation 成功，随后当前与冻结组均实际派发。原始 [runner 报告](source-version-tests.json)、[执行元数据](source-version-execution.json)、[stdout](source-version.stdout.txt) 和 [stderr](source-version.stderr.txt) 均保留。

| 组 | 分配/加载 | 实际方法 | 通过 | 失败 | 错误方法 | skip | 未派发 |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 当前 `8e4fd58` | 802 | 795 | 765 | 4 | 26 | 0 | 7 |
| 冻结 v1 `41b015c` | 4 | 4 | 4 | 0 | 0 | 0 | 0 |

当前 unittest 原始汇总为 **4 failure、30 error occurrences**。30 次错误包含重复 subtest 和一项 `setUpClass` 错误，因此不能直接从 795 减去 30 来计算方法通过数。上表使用原报告 `actual_test_ids` 与失败/错误 ID 的精确对应，保留多次错误及类初始化记录。806 个发现 ID 全部分组且无重叠，但 7 个方法未执行，所以原报告 `no_missing=false`、`no_overlap=true`；这 7 项不写成 skip，也不算通过。runner 自身负例故意打印的嵌套 FAIL/skip 不计为外层失败。

| 实际问题 | 原始结果与可证明原因 |
| --- | --- |
| HTTP 契约依赖未安装 | 24 次错误涉及 21 个已执行方法，`ModuleNotFoundError: jsonschema` 被原 validator 转成“安装 requirements-contract.txt，不能跳过验证”的错误。另一个类初始化因此中止，其 7 个方法未派发。原 CI 已固定安装该依赖文件，本次本机调用未具备它。 |
| Unix socket fixture | 3 次错误涉及 2 个方法，均在 `socket.socket(AF_UNIX)` 创建时收到 `PermissionError: EPERM`。这是该环境拒绝创建 fixture，并非目录检查器拒绝目录。 |
| Cold-web 模拟命令阶段 | 3 个方法在 `node-version` 阶段收到 `tool version command failed`；另一项期待“Node 22 required”的测试收到同一提前错误而失败。该阶段通过条件也包括清理验证，现有堆栈不能证明实际 Node 版本错误、子进程非零退出或真实 Next 构建失败。 |
| 进程清理验证 | benchmark 超时测试的 `cleanup_live_processes_absent` 为 false，另两个 ColdWeb 测试的 `cleanup_verified` 为 false，共 3 个失败。日志没有证明更深层原因；本次未重复运行、放宽检查或将未知进程状态视作成功。 |

四动态目录、原 3 CLI 与两位独立 reviewer 的目录测试通过，和这次本机全量运行未通过是不同范围。该记录不能写成 full-source green、source-evidence 上传成功、业务 PG、G0 或父任务完成。公开维护 PR 应继续使用原 CI 的固定依赖准备和原完整 runner 验证其实际公开源码。

当前原 CLI、针对性测试、独立审查和完整 source-version runner 的实际结果分别保存，互不替代。

复现环境使用已安装的 Go 与模块缓存，不变更 producer 或门禁：

```sh
export R5_TEST_GO=/path/to/go
export R5_TEST_CACHE=/path/to/go-build-cache
export R5_TEST_MODULECACHE=/path/to/go-module-cache
export GOPROXY=off
export GOMAXPROCS=2
unset GOTOOLCHAIN
# 原 CI 的运行时契约依赖准备；本次本机 full run 未执行此安装。
python3 -m pip install --disable-pip-version-check -r scripts/requirements-contract.txt

python3 -B scripts/check_r5_transactions.py
python3 -B scripts/check_r5_compatibility.py
node scripts/collect_api_calls.cjs --check
PYTHONPATH=scripts:scripts/tests python3 -B -m unittest \
  test_r5_transactions test_r5_compatibility \
  test_r5_catalog_reconciliation test_r5_current_source_inventory -v
node --test scripts/tests/api_calls.test.cjs
python3 -B scripts/r5_archive_boundary.py --root .
python3 -B scripts/run_r5_source_version_tests.py --root . --output /private/source-version-tests.json
```

`validation` 与 `complete_source_runner` 只记录各自实跑结果。完整 runner preparation 的有限 typed wire fixture 不扩大目录的 runtime 等级；业务真实 PG、旧客户端升级、M/G0、发布和父任务验收保持未执行范围，本修订不关闭它们。
