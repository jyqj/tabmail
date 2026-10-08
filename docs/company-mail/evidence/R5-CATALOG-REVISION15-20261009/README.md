# R5 目录 revision 15：ADVANCE 三轮公开源码对账

本目录将静态目录对齐 [ADVANCE 第三轮实际公开 merge](https://github.com/jyqj/tabmail/commit/26a30e3c6963d553efdd8a8b33c095feb924c758)。[PR #209](https://github.com/jyqj/tabmail/pull/209) 已包含第三轮产品和固定产品证据；本次目录与历史测试维护 **新增实施 TODO 为 0**。十个限定实施 Issue 的完成、三轮剩余数量和产品验证由 ADVANCE 总账与整合 PR 单独说明。原父任务仍为 **10/171 已验收、剩余 161**。

## 固定输入与冻结历史

| 对象 | 精确身份 |
| --- | --- |
| 当前产品实际公开 merge | `26a30e3c6963d553efdd8a8b33c095feb924c758` |
| 当前产品完整 tree | `437de0716fc9247e188f0409654f4f178905dc33` |
| merge 第一 parent | `f05ce6c0cb60035f4eb5a9d4438522b9fcd92203` |
| merge 第二 parent／公开候选 | `f8ad0533246fdf0d9257a31ade956b581a0c4426` |
| revision 14 公开目录 merge | `fdea2178759ce9842844926374ea98517bc4188b` |
| revision 14 目录 tree | `e37024707b91a0e79363c73b2dce1c8feebcb2da` |
| revision 14 原产品 source | `740126660526db987b7914c50a8731b7cac9bf42` |
| revision 14 原产品 tree | `5ee1ec138c6854b3068ec3576ea1e824510a9ee2` |

本次在独立普通 clone 中读取已公开的 Git 对象。原采集前后均核对同一个 HEAD、完整 tree 和干净状态。四份 revision 14 目录保存公开 commit、Git blob、SHA256 和长度。revision 1–14 的 **120 个历史文件**保持原路径和原字节；旧结果、日志、时间和来源身份不回写。

冻结后执行三个原 CLI，事务、兼容性与客户端目录均为 **exit 1**。事务检查实际拒绝新增函数、旧函数分类和调用位置漂移；兼容性检查拒绝当前客户端源清单不一致；客户端检查首先报告模板列表的实际行号变化。原命令参数、退出码、耗时及 stdout/stderr 长度和哈希见 [reconciliation.json](reconciliation.json) 的 `baseline_validation`，完整输出保留于本次执行证据。

## 原采集器得到的当前事实

| 静态事实 | 数量 |
| --- | ---: |
| PostgreSQL 源文件 | 64 |
| PostgreSQL 函数 | 404 |
| 词法 SQL 执行调用 | 505 |
| direct-write 函数 | 139 |
| write-closure 函数 | 160 |
| migration 文件 | 19 |
| 注册路由 | 133 |
| 客户端调用分支 | 136 |
| 兼容性源闭包文件 | 95 |

事务目录对比 revision 14 有 **52 个调用者列表、9 个语法记录、8 个派生断言记录**变化，合计影响 **54 个既有条目**；另有 **2 个新增函数**。只有既有 `CreateAPIKey` 的函数体及分类发生变化。19 个 migration 的文件和定义全部保持。新增授权函数包含 4 个直接 SQL 执行调用；旧函数中的 2 个 INSERT 移入新 helper，因此总 SQL 调用数由 501 增至 505。

客户端仍为 136 个分支，其中 **9 个分支只有行号变化**，分别来自模板列表、编辑器、版本列表和员工页面。路由集合不变，**13 条路由事实**更新：9 条更新客户端位置；注册、管理员用户修改及两种 API key 发行路由更新请求契约，后两者同时更新响应引用和 schema 组件。兼容性闭包有 **10 个文件哈希**变化。原 OpenAPI 缺口、无客户端路由、人工升级判断、未知字段和未满足的运行条件全部保留。

对比记录包含 **22 个 Go／Web 源路径**，其中包括 `internal/testutil` 适配实现；OpenAPI 另行记录。`excluded_closure_product_paths` 明确列出 95 文件兼容性闭包之外的路径，不能用这个有限闭包表示整个产品源码覆盖。新的 API key issuance workflow、runner 和 runner 单元测试进入 **174 个保护源文件**清单。生产及测试完整源码的运行绑定仍由未修改的 source-version runner 与产品门槛负责。

## 三个受影响 PostgreSQL 函数的手工边界

`internal/store/postgres/apikeys.go` 是唯一变化的 PostgreSQL 生产文件；函数净增加两个。

| 函数 | 分类和职责 |
| --- | --- |
| `CreateAPIKey` | 既有可信 seed/import 事务入口。`Begin → insertAPIKeyRows → Commit`，错误由原回滚路径清理。其本体不再直接执行 INSERT，分类由 `direct-write` 变为 `write-call-closure`。不重载 JWT 身份、权限或 zone，不写发行 audit；正式 HTTP 发行使用必需的授权接口。 |
| `insertAPIKeyRows` | 新增 `direct-write` helper。使用调用者传入的 `pgx.Tx`，按需分配 UUID 和时间、编码 scopes，依次写 key 与 usage。没有 Begin、Commit、权限锁、身份检查或 audit，外层调用者负责这些边界。 |
| `CreateAPIKeyAuthorized` | 新增 `direct-write` 事务入口。锁定当前身份与权限依据，计算可签发范围，在同一事务写 key、usage 和必需 audit；Commit 确认后才更新输出对象。 |

精确函数体 SHA256 分别为：

| 函数 | SHA256 |
| --- | --- |
| `CreateAPIKey` | `63d0e139223af39dcaae5e12c17ec4bd5688655b8e1ce678feb8179f71737ec0` |
| `insertAPIKeyRows` | `293516617d7e09d2e86cc1877d45da7ee2eba6829f4b78b8ff8f9124c66c3ddf` |
| `CreateAPIKeyAuthorized` | `63d0d21220f001ee79261adb93a6506c3deeb6b7727bc55673fcc3ecd8cebbe2` |

授权事务先对目标 tenant 取得 `FOR KEY SHARE`，再按真实 JWT user ID 读取并锁定 user `FOR SHARE`。`issuer.Refresh` 复核实际 home tenant、用户 ID、active、原 role 和 session proof；selected tenant 与目标 tenant 不替代实际身份。非管理员通过 `effectivePermissionSnapshot` 对当前 profile 取得 `SHARE NOWAIT` 并重载合并权限；已有 user 锁约束权限 override 写者。

`ConfigureIssuedAPIKey` 按当前能力和 zone 上限设置 owner。无法在 key 中表示的 deny-all zone 范围直接拒绝，避免存成无自身限制的 key。实际 zone UUID 排序、去重后逐行取得 `FOR SHARE NOWAIT` 并核对 tenant，再由 helper 写 key、usage，最后写必需的 audit。该顺序是源码中的局部锁与写入关系；目录采集不会执行数据库，也不证明所有未来写者遵守同一协议。

本函数外层 defer **直接观察到原始 `PgError` 的 55P03／40001** 时，保留 cause 并映射为 Conflict。现有 profile helper 的 55P03 已自行转为 typed Conflict，且没有附带底层 cause，不能把外层的错误链保证扩展到这一路径。失败不返回 key secret；提交应答丢失仍不代表能够确定事务未提交。没有新增自动重放或 exactly-once 保证。

key 的 tenant 外键和可选 owner 外键引用 tenant/user，usage 外键引用 key；owner 外键的 `NOT VALID` 不免除新写检查。audit 的 tenant 外键仍遵守原定义。zone ID 数组没有这些关系型外键，授权入口依靠显式 zone 校验及锁。目录保留 FK 父锁和局部顺序的手工说明，不把静态 SQL 片段解释为已执行的完整 SQL 或并发正确性。

旧 `CreateAPIKey` 的四个人工字段——`lock_fk_wait_fence`、`evidence`、`unverified_risks`、`file_family_context`——连同 **完整原 `source_review`**，逐值归档到 `historical_revision14_review`。当前说明重新绑定精确函数体哈希。其他旧条目的人工字段和未知字段保持；`apikeys.go` 原文件级三个审查字段不改，只增加 `revision15_additions`。新字段不撤销旧风险或提升验收结论。

[Issue #204](https://github.com/jyqj/tabmail/issues/204) 与 [PR #209](https://github.com/jyqj/tabmail/pull/209) 的 18 个实际 PostgreSQL 回归，以及独立 197 个 authz／HTTP 测试，属于另行固定来源的产品证据。它们只支持各自限定场景，不由本目录重新计数为全局 runtime、发布、性能或父任务验收。

## 原检查与历史正控

原采集器、校验器、CI、执行身份守卫、必跑 manifest 和五个 `test_unapproved_*` 负控保持。已有 `REVISION*` 常量全部保留。revision 14 的两个原正向断言正文在精确公开 fdea217 普通 clone 中执行，历史采集前后核对 HEAD、tree 和干净状态。增加两个 revision 15 当前正例；目录模块保留原 31 个方法后共 33 个方法。

事务模块保留全部 26 个原测试方法，当前计数更新为 404 函数／64 文件。唯一仍要求 `CreateAPIKey` 保持 PR23 旧函数体与旧分类的历史正控，完整原正文放入精确 fdea217 普通 clone 上下文执行；临时使用该 clone 的 `tx.ROOT`、`tx.CATALOG` 和数据，前后核对 HEAD、tree、干净状态及独立 Git 目录。原正文的字节和 AST 不变，其他测试继续使用当前目录。revision 15 正控从 fdea 原测试全文构造唯一允许的计数、import、context 和 wrapper 变化，并逐字比对；同时独立核验旧正文 AST 与去除 wrapper 缩进后的字节。当前 `CreateAPIKey` 的新分类、函数哈希、人工判断和完整旧审查归档仍由当前 revision 15 正控验证。兼容性测试文件及 133／136 计数保持原字节。

继续使用原命令：

```bash
python3 -B scripts/check_r5_transactions.py
python3 -B scripts/check_r5_compatibility.py
node scripts/collect_api_calls.cjs --check
PYTHONPATH=scripts:scripts/tests python3 -B -m unittest \
  test_r5_transactions test_r5_compatibility \
  test_r5_catalog_reconciliation test_r5_current_source_inventory \
  test_r5_reviewed_web_locks -v
```

三条原 CLI 对本次已生成目录均为 **exit 0**。这次执行时 HEAD 仍为产品 merge `26a30e3c6963d553efdd8a8b33c095feb924c758`，目录是尚未提交的同一组候选字节；执行前后 HEAD、tree 和状态一致。随后仅这四份目录及 reconciliation 固定为 `53d0d925601620f7062792d13d55b9a73837c5e5`，tree 为 `f4030eeb47dc59c8c4e03180a7349b677e0bfa54`。本段说明没有把此前执行回溯成在后续 commit 上运行。

第一次完整五模块执行绑定干净候选 `d79c5ba04ce43f420ea95d28f2367a6aa27228c2`／tree `01bd88c23bb18a494e6727a5418b140e44037ad7`，实际为 **100 个方法、99 PASS、1 FAIL**；唯一失败就是上述 PR23 历史正控对当前 `CreateAPIKey` 的旧态要求。原完整输出及同源码独立复现均保留，不将其回写为通过。冻结这一原正文后，五个模块仍是 100 个方法；修正后的完整执行及独立定向复核分别绑定各自实际候选，不与旧失败结果拼接为新的全量通过。

修正后的干净候选为 `0e329e872685209486e966fe7d5cf031e3020034`，完整 tree 为 `0135d90c89bedef6bf9a44c4738944a485feea3f`。一次并发执行在历史普通 clone checkout 时遇到工作盘 `ENOSPC`，原日志没有完整总数，不计作一次完成的 100 项验收。回收已完成且干净的临时工作树后，以相同提交和测试字节串行执行原五模块，实际结果为 **100 PASS、0 failures、0 errors、0 skipped**；unittest 报告 165.013 秒，外层命令耗时 166.081 秒。执行前后 HEAD、tree、干净状态及五个模块字节全部相同。

三次作者执行的原始 JSON 回执、stdout 和 stderr 已分别按原字节归档，见 [author-validation.json](author-validation.json)。最终完整执行的 stderr 为 18,540 字节，SHA256 为 `774ad64339dc13b41ae6287718604e92a4bcd284fcbbbd8fd349e33bf7614f7e`。空 stdout 也显式保留。上述运行发生在 `0e329e8`；随后追加本段说明及回执，不回溯声称在后续文档提交上运行。独立审查方的初次完整失败复现及修正后定向执行由各自回执单独记录。

完整模块执行、维护提交的独立复核及精确 source-version CI 按各自实际源码身份记录。原完整产品 CI 门槛继续有效；目录当前不使其他失败门槛自动通过。`runtime_verified=false`、`product_green=false`、`task_complete=false`，#56 保持 draft。

## 最终交叉审查与交付核对

frontend 在同一个干净 `0e329e872685209486e966fe7d5cf031e3020034`／tree `0135d90c89bedef6bf9a44c4738944a485feea3f` 上独立执行完整事务模块 26 项及 revision14／15 四个正控，最终 **30 PASS、0 FAIL／ERROR／SKIP**，结论 **ACCEPT**。它还打开作者原始回执并核对了 100 个不同通过方法、全部模块字节及日志；作者完整 100 与独审定向 30 分别归属各自执行，不相加。独审原 99P／1F、第一次 ENOSPC 的 28P／2E 和最后串行 30P 均保留。见 [独审说明](independent-frontend/README.md)、[机器回执](independent-frontend/review-summary.json)及 [41 成员原证据包](independent-frontend/catalog15-independent-review.zip)，ZIP SHA256 `2d2021aaa69dec2d90489ef18e55d8deec66ce8340f72f8c4124161c51415aaf`。

root 将三轮总账、真实 PG 原包与 #56 完整原文归档加入后，在干净 `b7a7b07b476f501d9d25c7225436f53c887fe13d`／tree `72c50244f3cd060456541103a9da5f15cf1bbc37` 上执行三个原 CLI，结果 **0／0／0**，执行前后 HEAD、tree、clean 一致，见 [root 原执行回执](root-cli/receipt.json)。所需当前目录、检查器与五模块输入和受审源保持同字节；这一步没有重跑或重新归属产品测试。

环境恢复还包括 root 从既定 Go build cache 清理 65 个确认属于 Tabmail 的大型、可重建编译归档，共 1,172,124,994 字节；源码、Go module cache、编译器、stdlib／AST 工具缓存和全部原始证据保留。此环境说明与独审派生回执已对齐，原失败日志没有变化。

本次公开交付的完整 head／tree、实际 merge 与后续完整 source-version CI 状态在 [PR #56](https://github.com/jyqj/tabmail/pull/56) 的最终回执中记录。原父任务的所有 checkbox 及当前全部 checkbox 行保持原字节，三轮实施任务仍为 **10／10 完成、剩余 0**，维护新增数仍为 **0**。
