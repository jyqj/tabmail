# R5 目录修订 11：最终 COMPLETE 与并行 CONTINUE 源码对账

本修订处理 [Issue #149](https://github.com/jyqj/tabmail/issues/149)。当前目录最终绑定公开产品源 `33f61fde`，**新增完成的实现 TODO 为 0**。`task_complete`、`runtime_verified`、`product_green` 均保持 `false`；原 R5 父任务仍为 **10/171，剩余 161**。目录检查只证明相应源码事实与人工登记对齐，不代表业务并发、升级、发布或父门禁完成。

## 固定来源与保留的中间阶段

| 用途 | commit | tree |
| --- | --- | --- |
| revision10 不可变目录快照 | `20c39ab3ebaee46e5ac51e25d90a455166c9a31d` | 各历史 blob 单独验证 |
| revision10 审查的产品源码 | `703a572864296120fff3efe0880c560ad9c74d57` | `40e1d2d16258e0e2bc3afea29553dbb148920e4e` |
| 本批十项 COMPLETE 的永久产品范围 | `4d2de05aa4bda226840fd591f2f356e8dd39ac20` | `52c8b95130b31d75b53d01010b333621ffc644eb` |
| 本修订先前实际审查阶段的产品源 | `3a103cda4363cb8f32839eaaa73ab065eb967f79` | `5e454667e0c99e816d20c499514d0e17f645bd22` |
| 最终目录审查的产品源 | `33f61fde3cf37d3cec2dd360146369c291eb0930` | `d982493e28431e644fd4018d8f746d8940280268` |

`3a103cda` 合入 CONTINUE #133/#134/#135；`33f61fde` 再合入 #136/#137/#139。它们作为已批准的并行产品改动进入当前目录，**不计入本批十项 COMPLETE 的完成数**。最新增量仅涉及 typed 域名冲突、既存 raw object 完整性验证、Next 16.3.8 及其受审锁文件准入。

先前目录及完整运行证据已经冻结，并发布了同树可读快照：

| 实际本地身份 | 公开同树快照 | 完整 tree |
| --- | --- | --- |
| 执行完整 runner 的核心 `8e4fd58f63f4e111eca8e5482ae91abbec0484b2` | [9bcc542](https://github.com/jyqj/tabmail/commit/9bcc542dcaa0bb7c280c380ea278c1a2a52717d3) | `c86a86f0a252159e068c8458e343eb59248ff979` |
| 保存原结果的 `fd87031aa117dc48346bbdc22b663e0385704c20` | [f271336](https://github.com/jyqj/tabmail/commit/f2713367dd949487c632ebee51de3a0cc1d2c6f7) | `7d1ca2357292cc35160011afe91fdbde011106c1` |

公开证据 `f271336` 的 parent 是 `9bcc542`。**公开同树快照不是另一次执行**；原 runner JSON 仍记实际执行身份 `8e4fd58`，不重写成公开 counterpart 或最终 `33f61fde`。当前测试只读取上述公开快照及原公开历史/产品 anchors，不要求本机特有的 `8e4fd58`/`fd87031` Git 对象存在。

[先前 README](intermediate-3a-README.md) 与[先前 reconciliation](intermediate-3a-reconciliation.json) 保留 `f271336` 中的原字节；原五函数人工说明、完整 runner 四个 artifact、两份独审记录和三份独审日志共九个文件同样逐字保留。最终 [reconciliation.json](reconciliation.json) 记录精确同树映射和全部 SHA256，并单独说明最新阶段范围。

## 当前事实与增量

| 原 producer 的事实 | revision10 | 先前 3a 阶段 | 最终 33 阶段 |
| --- | ---: | ---: | ---: |
| PostgreSQL 源文件 | 62 | 62 | 63 |
| PostgreSQL 函数 | 395 | 400 | 401 |
| 字面 SQL 写函数 | 134 | 138 | 138 |
| 按名称推导的写调用闭包函数 | 154 | 158 | 158 |
| SQL 执行表达式 | 499 | 501 | 501 |
| migration 文件 | 19 | 19 | 19 |
| 注册路由 | 132 | 133 | 133 |
| 客户端调用分支 | 134 | 135 | 135 |
| 显式 transport forwarder | 7 | 7 | 7 |
| 原兼容性 producer 的有限源码闭包 | 94 | 95 | 95 |

相对 revision10，旧 **395 个函数体中 394 个不变、1 个 `CreateZone` 明确变化**；19 份 migration 全部不变。完整差异为 79 个旧函数的 caller 候选变化、83 个旧条目变化、20 个 syntax 变化（19 个原队列函数的位置变化及 1 个 `CreateZone` 体变化）。原分类、旧证据等级和历史运行来源保持各自准确边界；增加的字段也纳入完整条目摘要，不因只遍历旧字段而漏记。

### 原五个 Claim 函数与新增域名错误分类

[function-review.json](function-review.json) 保持原 `3a103cda` 人工审查字节，五个 Claim 函数及整个 `queue.go` 在 `33f61fde` 中仍逐字相同。四个入口将固定 SQL 交给 `markQueueClaim`，先取得目标单行 `FOR UPDATE`，再用独立语句检查 `processing`、匹配 attempt 和数据库未过期租约，更新后提交。生产 hooks 明确传 claim ID/Attempts，不回退旧 ID-only mark。

原 AST 分类继续保留：四个入口是 direct-write/explicit-lock；helper 经参数执行 SQL，因此原分类是 transaction-or-callback、dynamic SQL，字面 direct-write/write-closure/explicit-lock 为 false。人工说明明确 helper 实际持锁和写入。原 78 个 PG 叶用例按入口分配 15/15/16/32，helper 共享这些叶而非再增加 78；本轮目录维护没有运行 PG，也未证明所有 FK/父删除交叉等待、等待中取消、提交结果确定性、任意时钟偏差或接收方去重。

[zone-function-review.json](zone-function-review.json) 单列 `33f61fde` 的两个实际变化：

- 新增 `classifyZoneCreateError` 只对 `errors.As` 得到的非 nil `*pgconn.PgError`、精确 SQLSTATE `23505` 和约束 `domain_zones_domain_key`，join `ErrDomainAlreadyExists` 与完整原 cause；其他约束、状态、文本、取消及 nil 原样返回。它不执行 SQL、锁、事务、回调、重试或外部 I/O，原分类是 read-or-pure。
- 既有 `PgStore.CreateZone` 只把返回 `err` 改为返回上述纯分类结果。UUID、default visibility、CreatedAt、原 INSERT SQL 与 15 个参数均不变；仍是单条 pool.Exec 自动事务。该既有条目继续为 source-only，不能因 helper 单元测试而声称 live INSERT 已运行。

人工审查定位原 domain 全局 UNIQUE，以及不应被翻译的 tenant-identity 约束；tenant/owner/parent FK 与插入等待边界保留。活跃 service 用 `errors.Is` 转为受限 conflict/internal，保留 cause，失败不继续身份创建、成功审计或缓存失效。另一位 agent 独立执行相关 **49 个 race 叶用例全通过**，涵盖 13 个 pgconn 对象 mapper、4 个 FakeStore、28 个 service/HTTP 和 4 个授权用例；**实际 PostgreSQL 使用为 false**。

### 客户端、路由与完整来源范围

3a 阶段保留原 134 个逻辑调用并增加原始租户覆盖 GET。草稿 GET/DELETE 在数组 47/48 重排，路由映射跟随具体方法和路径；删除仍传原 revision。activation 的 AST owner、消息窗格行号及两个 base forwarder 的位置变化保持原分支和 HTTP 方法。

最新 33 增量的 **135 个客户端行、133 个 route facts、API matrix、OpenAPI 和有限 95-file closure 均与 3a 阶段逐字相同**，两个 client 目录文件也没有字节变化。不能把该阶段的正对照写成新的 client/compatibility 漂移。新租户 GET 仍在 Auth/PermissionLoader/RequireSuperAdmin 组，返回七个必现 nullable 整数；原 compatibility producer 的 named DTO binding 仍为空、route-specific runtime fixture 仍未登记，其他 contract/HTTP 测试不因此变成该目录的运行验收。

按原产品路径过滤规则，revision10 产品源至最终 33 有 **40 个 source paths：9 个在有限闭包、31 个在闭包外**。该旧过滤规则不包括 package JSON，所以另以 `additional_build_metadata_changes` 精确绑定 `web/package.json` 与 `web/package-lock.json` 的前后 commit/blob/SHA256/长度。Next/eslint-config-next 及 12 个相关 release 包从 16.3.6 升至 16.3.8；新锁 SHA256 为 `c4f70934466c07c0a2589ead49f120d1b2278d9775e13fdbf7386913b1658869`。目录不把这次依赖准入等同于全量安全审计、冷构建或 G0 通过。

## 历史与检测规则

revision1–10 的九组历史目录共 **47 文件**，文件集合、blob 与 SHA256 保持原字节。原历史运行记录、wire 快照、归档 registry 和模块 marker 不重签。

原 `check_r5_transactions.py`、`check_r5_compatibility.py`、`collect_api_calls.cjs` 与底层两套 AST producer 未改。20c→3a 的保护源范围是两项已批准修改加五个新增；**20c→最终 33 为三项既有修改加五个新增**。三个修改是 `r5_source_runner_prepare.py`、`check_contract_drift.py` 和 `test_r5_reviewed_web_locks.py`，其中最后一轮只给 prepare 的既有受审 lock 集合追加精确 hash 并演进相应准入测试。全部来源差异逐项记录。

本次维护仍只演进三个当前正例文件：事务正例精确更新 395→401、62→63；兼容性正例精确更新 132→133、134→135；目录正例保持原 23 个方法并增加的两项 current revision11 正例，合计 25。小正例文件除上述四个数字外的字节全部固定；另外 **167 个**保护源码文件严格等于公开 33。原五个 `test_unapproved_*` 的源码与 AST 不变。旧阶段验收读取其原公开 Git 快照，缺失对象仍是错误，不抓取、不跳过或回退当前同名文件。

新正例检查公开 9bcc/f271 的同树关系、先前目录快照与九个保留 artifact。针对 33 的增量，先前 transaction 目录必须拒绝新 helper/变化体；原先的 client/compatibility facts 必须继续作为通过的正对照，不能为了演示 RED 而制造漂移。

## 验证与复现

最终 33 阶段原三条 CLI 均 **exit 0**，Python 五模块实际 **92/92 PASS、0 failure、0 error、0 skip**（包含原 85 项相关检查及 7 项已批准的 reviewed-lock 测试）。[原始 Python 日志](current-33-python.stderr.txt) 与每条命令的输入前后 SHA256、环境、输出摘要和耗时均保留。新增的两项当前正例还先单独通过。最终 33 阶段的原命令与精确候选输入摘要记入 `validation`；先前 3a 阶段的 85/85、Node 27/27、archive 边界通过与两份独立 25/25 审查完整保留在中间 packet，不移用为最新源码的实跑结果。

基线分两种：原公开 33 自带 revision10 旧目录会被原门禁拒绝；本轮增量实测在已保留 3a revision11 目录、产品等于 33 的干净合并源上，transaction CLI exit1，compatibility 与 client CLI 均 exit0。`incremental_cli_baseline` 保存这一真实 1/0/0 结果和前后输入摘要，符合客户端及有限 closure 字节未变的事实。

### 最终 33/d2a 核心的两份独立审查

两位 reviewer 均 **ACCEPT** 核心 `d2a4f21d3cc6742bc2cb7e831eeedd771ddd1c71`、tree `ef5a5548c47cc7cb489ad082ec31033b42473242`。新的[最终独审记录](current-33-independent-review.json) 保留准确执行身份、原命令、环境选择器、结果、原始输出摘要及人工审查范围。

| 独立审查 | 实际执行范围 | 结果 |
| --- | --- | --- |
| frontend reviewer | 两个 revision11 当前正例 + 五个原始 unapproved 负例 | **7/7 PASS**，0 failure/error/skip，13.836 秒；[原始日志](current-33-frontend-review.stderr.txt)。本轮没有再次执行完整 25 项，也不移用作者的 92 项。 |
| storage reviewer | 只含公开历史的独立仓库，在同一精确 tree 上执行原 3 CLI + 完整 25 项目录测试 | **3 CLI PASS、25/25 PASS**，0 failure/error/skip；unittest 32.509 秒、外层 32.911 秒；[原始日志](current-33-storage-review.stderr.txt)。 |

第二位将候选 tree 单独导入仅有公开 `33f61fde` 与 `f2713367` 父提交的仓库，生成本地审查提交 `8c4b7fd6cd4d8ef32bbb4269b6155cfc4c5a4597`。执行前后私有 `d2a4f21`、`8e4fd58`、`fd87031` 提交对象均不存在，也没有 Git object alternates、共享对象文件或对象环境覆盖。原门禁与目录测试仍全部通过，直接验证了新测试的历史读取可由公开同树快照完成。

双审还核对 47 份历史文件、当前 167 份固定保护源码、五项原负例、九份原证据及两份中间报告的字节保留；唯一旧 PG body 变化、新 pure mapper、40 条过滤源码加两份 build JSON、135 条 client 与 95 条 closure 的范围说明均一致。最后附加记录仅更新证据和 README，已验收目录、产品、测试、原 CLI/guard 及原完整 runner 记录保持不变。

### 先前 3a/8e 的两份独立审查

[原独立审查记录](independent-review.json) 只绑定核心 8e/公开同树 9bcc。frontend 和 storage reviewer 分别执行原 25 个目录测试全通过；第二位还以同样三条原 CLI 实际得到 3a revision10 旧目录全拒绝、8e 新目录全通过。第一位首次漏设 PYTHONPATH 仅导致 loader 导入失败，25 个实际目录测试未派发；单独保留该错误后按正确环境执行全通过，源码未变。

### 原完整 source-version runner：冻结的 3a/8e 本机结果


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


最终 33 的源码及新的受审 lock/preparer 不在上述 8e 完整执行范围。原 `original_preparer_unchanged_from_product_anchor` 特指冻结的 3a anchor；最终 packet 另加 `frozen_product_anchor` 和 `applies_to_current_source_anchor=false`。最新公开候选应由原 CI 按固定依赖安装后实际执行完整 runner，本机未重复这次缺依赖运行。

复现使用原命令和已安装的工具链：

```sh
export R5_TEST_GO=/path/to/go
export R5_TEST_CACHE=/path/to/go-build-cache
export R5_TEST_MODULECACHE=/path/to/go-module-cache
export GOPROXY=off
export GOMAXPROCS=2
unset GOTOOLCHAIN
python3 -m pip install --disable-pip-version-check -r scripts/requirements-contract.txt

python3 -B scripts/check_r5_transactions.py
python3 -B scripts/check_r5_compatibility.py
node scripts/collect_api_calls.cjs --check
PYTHONPATH=scripts:scripts/tests python3 -B -m unittest \
  test_r5_transactions test_r5_compatibility \
  test_r5_catalog_reconciliation test_r5_current_source_inventory \
  test_r5_reviewed_web_locks -v
python3 -B scripts/run_r5_source_version_tests.py --root . --output /private/source-version-tests.json
```

维护改动不关闭业务真实 PG、旧客户端升级、M/G0、发布和父任务门禁；各阶段原始失败、未执行范围和独立来源继续保留。
