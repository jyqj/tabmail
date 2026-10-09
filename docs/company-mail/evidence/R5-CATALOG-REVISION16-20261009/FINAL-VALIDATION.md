# R5 catalog revision 16：已提交候选的最终验收

**目录维护已取得原门禁的完整通过资格；本次维护计 0 个实施 TODO。** 三轮实施已经完成 10/10，剩余 0；原父任务仍为 10/171 已验收、161 未验收。目录更新不授予父任务、运行时兼容性或发布资格。

本记录绑定实际已验证提交 `167e38b1541c027107fc81900f3e37fda7289677`，完整 tree 为 `2e254469358541c8aedb1e98c88d5d2edf6d44f8`。后续文档补充保留全部九个生成文件及产品、检查器、测试、工作流的原字节；最后公开 head 的检查和实际合并记录见 [PR #225](https://github.com/jyqj/tabmail/pull/225)。

## 已执行的原门禁

[维护工作流 37884385919](https://github.com/jyqj/tabmail/actions/runs/37884385919) 的 inventory、source-version、proposal 三个 job 均实际 SUCCESS。

| 检查 | 实际结果 |
| --- | --- |
| 原 transaction CLI | exit 0；64 PostgreSQL 文件、405 函数、506 SQL 调用、140 direct write、161 write closure、19 migrations |
| 原 compatibility CLI | exit 0；133 routes、136 client branches、97 finite closure files；原 runtime/product/task false 边界保留 |
| 原 client CLI | exit 0；使用原 `collect_api_calls.cjs --check` |
| 原五模块 | 102 个不同测试全部 OK：transaction 26、compatibility 19、catalog reconciliation 35、current source inventory 15、reviewed web locks 7 |
| 完整原 source-version runner | 871 个不同 ID 全部执行并通过：867 current + 4 frozen-v1 |
| 完整性 | 请求、加载、实际执行集合一致；零遗漏、重复、failure、error、skip、expected-failure、unexpected-success |
| 源码状态 | 两个验收 job 的前后 commit、tree、完整 status 一致；status 为空 |

五模块的 102 个 ID 全部包含在完整 runner 的 871 个 ID 中，不能相加为独立测试总数。固定 frozen-v1 来源仍是 `41b015c30c66b3ba58a3c1395e8559ebcd27a65f`，四个原测试 ID 不变。原 runner、准备逻辑、采集器、独立普通 clone、测试发现与分组规则、20 分钟 job 预算保持不变。准备阶段真实执行成功，源码选择前后不变。

root 独立读取完整日志中的原 JSON，逐项比较 discovered/requested/loaded/actual 四组 ID，而非仅采信 workflow 绿色状态。runner 自身的负向测试会打印其受控失败或 skip 子样本；顶层两组报告中没有实际失败或跳过，原日志完整保留。

## 原始漂移与生成来源

产品源是第三轮的实际 merge `b6516f9ac873f4464894933b5c3a0e046bc9a4a5`，完整 tree 为 `4034b36faa89a421f2a21b525cf35e93da4a1670`。目录修订没有变更这份产品代码。

[首次 author job 113669399515](https://github.com/jyqj/tabmail/actions/runs/37883870675/job/113669399515) 在初稿 `8dae88b51504b0bd8b3af59ef7b27911a752ba89` 中启动，创建独立普通 clone 并固定在上述实际产品源。它先实际执行原三个 CLI，记录旧 revision15 目录的退出码 **1 / 1 / 1** 与具体漂移诊断；工具链或解析故障不能代替这些预期诊断。随后调用未改动的原生产者，只在生成副本更新四份目录，再实际确认三个 CLI 的退出码 **0 / 0 / 0**。

该临时生成副本的成功与后来已提交候选的验收分别记录。root 与 frontend 各自从完整原 job log 的 **1306 个 JSON 分块**重建九个文件，逐一验证 Git blob、SHA256 和 UTF-8 字节数。九个文件保持原输出字节，完整 pins 见 [原 author 回执](author-proposal/receipt.json) 和 [reconciliation.json](reconciliation.json)。原 job 文本为 6,367,638 字节，SHA256：

`14ce49f7fc4c98cb0a171fad796b0047a2edf448c904e59a64a820814f389fdd`

原始日志保留时间戳、BOM、stdout、stderr 和退出信息。通过 GitHub API 获取的是完整 decoded job text；没有声称下载或独立验证未读取的 artifact ZIP。

## 失败与历史保护

初稿工作流 [37883870675](https://github.com/jyqj/tabmail/actions/runs/37883870675) 保留真实失败：

- 原三个 CLI 全部失败，五模块实际执行 100 个测试，出现 3 failures、4 errors。
- 完整原 runner 实际发现并执行全部 869 个 ID：865 current 中 3 failures、4 errors，4 frozen-v1 全通过；没有遗漏、重复或跳过。
- 更新目录后，以上 869 个 ID 全部保留，只增加两项 revision16 当前守护，形成最后 871 个完整执行 ID。

静态独立审查核对了 author V2、工作流、原脚手架、实际生产文件及手工审查字段；真实生成时的 Python compile、AST 和原文字节约束也通过。随后 pr_audit 独立重读最终生成 manifest 与测试模块，对照原模块及受审模板逐字比较，全部规定变化精确相符，结论 ACCEPT；见 [最终输出独审回执](independent-review.json)。33 个原 reconciliation 方法全部保留：30 个完整方法原字节不变，两个 revision15 正控的原正文在固定历史源执行，一个当前正控只更新五处限定的版本绑定。五个原未批准变更负控保持。84 个原顶层赋值中只有三个当前绑定更新，其余 81 个值保持。新增加两个方法使该模块达到 35。

142 个历史文件与 186 个受保护来源条目完整固定。产品原有受保护路径与两个维护来源分别绑定；没有用新增维护文件掩盖产品源变化。SaveMailTemplate 的四个旧手工字段逐字归档；其他人工字段、未知字段和非派生断言保留。事务目录只新增一个 SeedSetting 条目，旧函数体变化限定为 SaveMailTemplate，人工审查与最终源码及建表 SQL 一致。

## 整体发布状态

目录维护通过后的 [Company 37884386030](https://github.com/jyqj/tabmail/actions/runs/37884386030) 已确认源码版本检查和当前目录通过，production-web、browser-journey 也成功。整体发布资格仍未通过：

- backend 的 PostgreSQL race、Validation tool regressions、Real protocol baseline 三步失败。这里只记录实际 job/step 状态；该 job 的完整日志 API 返回 Transport closed，没有据此猜测详细失败原因。
- frontend 完整 Vitest 实际为 **2088 PASS / 1 FAIL**；唯一失败是 `r5-external-batch-probe.test.tsx` 要求显式私有 fixture。
- 依赖安装报告 **8 个 high severity 问题**，严格零漏洞审计失败。

[#56](https://github.com/jyqj/tabmail/pull/56) 保持 draft；main 仍为 `d6d512172fb6b874c3283d9df3f4d56758684a13`，未合入或发布。旧 draft #17、#20、#24、#40 保留其独立证据缺口。三轮实施 PR #222 / #223 / #224 的实际完成数为 4 / 3 / 3，逐轮剩余 **6 → 3 → 0**。

## 原始证据入口

| 内容 | 原文或回执 |
| --- | --- |
| 原 author 生成过程 | [完整原 job log](author-proposal/github-job-113669399515.raw.log) · [九文件 pins 与三 CLI 前后回执](author-proposal/receipt.json) |
| 已提交候选原三 CLI 与 102 测试 | [完整原 job log](committed-inventory-167e38b/github-job-113670991398.raw.log) · [102 个完整 ID 和独立回执](committed-inventory-167e38b/receipt.json) |
| 已提交候选完整 871 测试 | [完整原 job log 与内嵌原报告](source-version/github-job-113670991637.raw.log) |
| 初稿完整 869 测试及真实失败 | [完整原 job log 与内嵌原报告](source-version/github-job-113669399510.raw.log) |
| 原完整 runner 的独立验收 | [全部集合、分组、原日志 hash 与失败 ID 回执](source-version/receipt.json) |
| 初稿三个 CLI 与五模块失败 | [完整原 job log](initial-8dae88b/github-job-113669399308.raw.log) |
| 当前整体发布边界 | [frontend 完整原 job log](release-snapshot-167e38b/github-job-113671069779.raw.log) · [实际状态回执](release-snapshot-167e38b/receipt.json) |
| 最终生成输出的独立静态复审 | [ACCEPT 回执](independent-review.json) |

全部结果归属各自记录的真实执行源。文档归档没有把原失败写成通过，也没有把临时生成副本或较早提交的结果回溯归属到较晚的公开提交。
