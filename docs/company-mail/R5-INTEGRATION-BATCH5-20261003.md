# R5 第五批三线独立组合回执

从 PR23 docs head `e5e23e21abfe4c7e9f082405e56a95a7f346fa9a` 派生 `integration/company-mail-r5-batch5-20261003`，组合测试源冻结为 `de1b7d37edbb83004592e7a8f2d78c8434278457`。产品源仍为 `aeed47f0e1998d9925acb680992b051463821d69`。三个完整分支范围用 no-ff merge 保留：benchmark `e9b2433`；transaction `1c197e88` → `5bcfe6c`；descriptor `879e203` → `f87830c`。没有只取末尾文档而漏掉修复，原失败、反例、作者运行日志及单任务 Forbidden 历史保持原字节。

## 本组合实际执行

| 阶段 | 实际结果 | 范围 |
|---|---|---|
| benchmark | PASS | 111/111；相邻 current benchmark/archive/CI wiring/version runner 45/45 |
| transaction | PASS | 26/26；真实 inventory CLI：62文件、395函数、498 SQL调用、134直接写、154名称闭包写、19迁移；task_complete/runtime_verified仍false |
| descriptor | PASS | 两改动模块及六相邻模块81/81；全部替换拒绝断言保留，O_PATH metadata不等bodyread；fixture anchor避免旧inode复用，不称原8fail已逐项证明原因 |
| 默认完整 versioned runner | FAIL | 658 discovered，current654 requested/645 started、0 failure events/3 error events/1 skip/9 notstarted；frozen-v1原41b015c四项4/4PASS；实际649项无重复，9项缺执行如实FAIL |
| TypeScript准备后完整 versioned runner | FAIL | current654/654全部实际启动、0 failure events/3 error events/1 skip；frozen-v1四项4/4PASS；658/658无漏无重，两个runner退出码均1 |

默认runner三ERROR分别是 `/Users` 平台路径不存在、compatibility setUpClass 的 Node客户端扫描未有TypeScript工具、历史wire spec/case不同。九NOTSTARTED全部是该compatibility类方法；typed shipping-envelope方法已进入runner但skip，实际fixture body没有执行。

Prepared三ERROR精确为 `test_r5_cold_web.ColdWebTests.test_output_refuses_existing_checkout_and_non_temporary_path`（/Users）、`test_r5_compatibility.CompatibilityGateTests.test_current_source_map_is_not_product_or_dependency_completion`（missing/duplicate routes）、`test_r5_compatibility.SafeWireJoinTests.test_exact_safe_join_remains_metadata_not_product_approval`（wire spec/case differs）。无current failure events，唯一skip为 `test_ordinary_receipt_contract.OrdinaryReceiptContractTests.test_fresh_typed_shipping_envelope_bytes`，不写假fixture或删test。

## 工具准备与剩余缺口

原requirements-contract锁完整安装到 owned `/workspace/r5-batch5/venv`；Go1.25.7与 owned GOCACHE/GOMODCACHE 使用原module字节，短TMPDIR为 `/workspace/t5`。不写系统或HOME，不改锁、Go版本、两replace、audit规则及normal PG180门禁。

另用本批外部 `prepared_runner.py` 包装**原runner的checkout接口**，先精确clean clone，再 materialize 官方锁TypeScript5.9.3完整regular package。原tar integrity、SHA256及所有regular文件hash与batch4 pin逐项匹配；没有整npm tree、生命周期、.bin、链接、Go/module/native文件。准备前后default与race-r5protocol实际capture逐项比对：selected local全部hash/fields、package records、module graph、coverage、variants、base source、Go环境及generated/external/native/toolchain清单相等。正式versioned runner、test IDs分区与扫描门原字节不动；此prepared成绩不能冒称默认CI runner成绩。外部wrapper与原始日志随安全证据归档交付。

原batch4的26个schema16 preparation及10个 `observed schema differs from actual migration source` ERROR事件留在原报告/归档，本批另列其完整具名沿革。新schema16 mock只限两个synthetic grammar类；相应最新fixture PASS不解除schema19产品与旧schema16 pipeline/METHOD的迁移准入缺口。current-source/registry/CLI测试不受mock，旧fixture对当前source拒绝的具名回归PASS。没有执行真实benchmark、S/M/L或新migration runtime，不给历史性能结果重签。

历史compatibility127-route/wire source/case缺口、`/Users`平台路径和缺fresh typed shipping-envelope fixture继续保留。原whole-PG180及audit失败只引用batch4历史，本批**NOTSTARTED**；wholePG由另owner验证。未执行新全量Web/type/lint/build、独立Go/vet/fork race、browser或真实邮件服务，不套用旧成功为新组合PASS。

## 字节、远端与CI

1,051个受保护源码文件相对aeed字节不变；36个受保护历史文件相对PR23 docshead字节不变；591个基线evidence只有获准刷新的 `R5-TRANSACTION-COVERAGE.json` 变化，其余590原样。所有五条作者提交均为组合祖先。transaction清单仅source结构验收，不等并发/行为等价或父任务完成。

普通origin Git首次push成功，ls-remote固定确认 `de1b7d37edbb83004592e7a8f2d78c8434278457`。新三线组合的**唯一一次** draft PR 请求使用原gh路由，base为 `integration/company-mail-r5-batch4-20261003`，实际 exit1：`Post "https://api.github.com/graphql": Forbidden`。立即停止PR/API动作，没有重试旧单任务PR、替换身份/connector/remote/路由。reviewable PR body保留在证据目录。draft PR未创建，CI新增运行及exact-head结果未观察；原main-only push不触发该分支，不能宣称PR CI已启动或已绿。现有PG180正常门禁和workflow不改。

`git diff --check e5e23e..组合源` 指出作者保留的两份benchmark原raw日志各一处尾空格；不修改原证据讨绿。新维护文档另检查。原 `.agents/skills` 在仓库不存在，workspace `.agents`为空。

**10/171保持，无父项关闭，无merge/deploy/force。** 中央TODO只记具名synthetic fixtures/source inventory/descriptor fixtures的有证scope。本附录和交付receipt是组合测试源冻结后的docs-only维护，不把交付HEAD的未观察CI冒称PASS。

完整requested/actual/notstarted IDs及执行paths见[默认报告](evidence/R5-INTEGRATION-BATCH5-20261003/versioned-default.json)、[prepared报告](evidence/R5-INTEGRATION-BATCH5-20261003/versioned-prepared.json)；[机器回执](evidence/R5-INTEGRATION-BATCH5-20261003/receipt.json)、[真实剩余错误](evidence/R5-INTEGRATION-BATCH5-20261003/remaining-errors.json)、[原10migration事件逐项沿革](evidence/R5-INTEGRATION-BATCH5-20261003/historical-migration-drift.json)、[原字节/完整范围证明](evidence/R5-INTEGRATION-BATCH5-20261003/preservation.json)、[执行日志与外部准备wrapper](evidence/R5-INTEGRATION-BATCH5-20261003/execution-evidence.tar.gz)、[payload hashes](evidence/R5-INTEGRATION-BATCH5-20261003/payload-sha256.json)均保留。
