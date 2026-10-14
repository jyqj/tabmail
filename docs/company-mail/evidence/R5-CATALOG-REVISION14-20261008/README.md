# R5 目录 revision 14：FINISH 三轮公开源码对账

本目录将静态目录对齐 [FINISH 第三轮实际公开 merge](https://github.com/jyqj/tabmail/commit/740126660526db987b7914c50a8731b7cac9bf42)。[PR #195](https://github.com/jyqj/tabmail/pull/195) 已包含最终产品及本轮实施记录；本次目录与历史测试维护 **新增实施 TODO 为 0**。十个限定实施 Issue 的关闭、各轮剩余数量与独立产品验证由 FINISH 记录和整合 PR 单独说明。原父任务仍为 **10/171 已验收、剩余 161**。

## 固定输入与保留的历史

| 对象 | 精确身份 |
| --- | --- |
| 当前产品实际公开 merge | `740126660526db987b7914c50a8731b7cac9bf42` |
| 当前产品完整 tree | `5ee1ec138c6854b3068ec3576ea1e824510a9ee2` |
| merge 第一 parent | `ffcb2111dc42f51fd1a7c5512e0474aba971ecd3` |
| merge 第二 parent／公开候选 | `805ee8c3a7c897dea9fc413e5604bf5d99622c41` |
| revision 13 公开目录 merge | `be3a6bf41daa198a39306c90fd03411e38a017d8` |
| revision 13 目录 tree | `350a848972e02ef8f7edc8a1a43db087c844405c` |
| revision 13 原产品 source | `9b73b13f376f7e8d589a06fc7078dae4c342d5d5` |
| revision 13 原产品 tree | `8e3f458f8cec51c031a06fd280151116164244ec` |

本次使用普通 clone，读取实际公开 Git 对象；原采集前后均核对同一个 HEAD、完整 tree 和干净状态。四份 revision 13 目录分别保留公开 commit、Git blob、SHA256 和长度。revision 1–13 的 **118 个历史文件**保持原字节，旧日志、时间、源码身份及失败结果不回写。

冻结后的三个原 CLI 结果分别为：事务目录 **exit 1**、兼容性目录 **exit 1**、客户端目录 **exit 0**。前两个实际拒绝了新增 PostgreSQL helper、函数／调用位置及请求契约漂移；本轮客户端调用集合和行号没有变化，客户端原检查本来就通过。原命令参数、来源、退出码、耗时及 stdout/stderr 哈希见 [reconciliation.json](reconciliation.json) 的 `baseline_validation`，完整原输出保留在本轮执行证据中。

## 原采集器得到的当前事实

| 静态事实 | 数量 |
| --- | ---: |
| PostgreSQL 源文件 | 64 |
| PostgreSQL 函数 | 402 |
| 词法 SQL 执行调用 | 501 |
| migration 文件 | 19 |
| 注册路由 | 133 |
| 客户端调用分支 | 136 |
| 兼容性源闭包文件 | 95 |

事务变化为 **43 个调用者列表、28 个语法记录、22 个派生断言记录**，合计影响 **51 个既有条目**；另有 **1 个新条目**。其中只有下述 **5 个既有函数体**发生变化，既有分类均未改变。新条目为纯错误分类函数，不执行 SQL，因此 SQL 调用数保持 501。19 个 migration 的文件和定义全部保持。

兼容性目录仅有 **3 条路由的 `request_contract`** 变化：管理员邀请、邮箱授权和邮箱发送策略。另有 **6 个源闭包文件哈希**变化；路由集合不变。两份客户端生成目录与 revision 13 保持完全相同字节。原 OpenAPI 缺口、无客户端路由、人工升级判断、未知字段及未完成的运行条件均保留。

本轮对比记录 **13 个产品源码路径**，另行记录 OpenAPI 变更。`excluded_closure_product_paths` 明确列出 95 文件兼容性闭包以外的路径，包括 PostgreSQL、S3、回复生成和认证界面；不能用这个限定闭包冒充整个产品源码覆盖。新的 mailbox provisioning workflow 与 runner 已进入 **171 个保护源文件**的清单。新增 PostgreSQL 测试与其余产品测试的运行／完整源码绑定仍由原 source-version runner 和产品验证负责。

## PostgreSQL 五个变化函数与新 helper 的手工审查

`company_members.go` 是唯一变化的既有 PostgreSQL 生产文件。

| 函数 | 变化及保留的边界 |
| --- | --- |
| `companyTx` | 显式传 `uniqueConflicts=true`，保留原租户 `FOR UPDATE`、当前主体复核及通用 23505 映射。 |
| `companyReadTx` | 显式传 `true`，保留无预取租户锁的原路径；函数名称不证明 callback 只读。 |
| `companyReferencedTx` | 显式传 `true`，保留租户 `FOR KEY SHARE`、主体／条件权限快照与原 callback 范围。 |
| `companyTxScope` | 新布尔参数只选择 callback 错误的通用 23505 映射；原 Begin、租户锁、当前主体和管理员复核、条件权限快照、Commit／Rollback 路径保持。40001、55P03 和 42501 的原映射保持；没有新增自动重试。 |
| `CreateWorkMailbox` | 使用同一个管理事务及租户写锁，但传 `false`。只有 `mailboxes INSERT` 的错误交给地址分类器，审计和后续未知唯一性错误保留原始原因及服务端错误路径。 |

新文件 `mailbox_errors.go` 中的 `classifyWorkMailboxCreateError` 只对真实 `*pgconn.PgError` 的 **SQLSTATE 23505 + 精确约束名 `mailboxes_full_address_key`** 生成带原始错误链的 Conflict。其他错误原样返回，不根据本地化消息猜测，不执行数据库或外部操作。调用位置限定实际 INSERT 阶段；helper 本身不识别 SQL 阶段。原公司域和可选 owner 校验仍是普通读，不声称增加了这些行的锁或完整生命周期不变性。

五个变化函数的原 `lock_fk_wait_fence`、`evidence`、`unverified_risks`、`file_family_context` 完整保存在 `historical_revision13_review`，当前说明绑定精确函数哈希。其他既有函数的人工判断与未知字段保持。`company_templates.go` 没有函数体变化；HTTP 布尔意图校验不被写成 PostgreSQL 模板事务的新实现。

Issue #189 的固定 PostgreSQL 回归属于独立产品证据。本次目录采集不运行这些数据库场景，也不据错误分类变更宣称全局无死锁、跨事务授权或完整父项验收。

## 原检查器、历史测试与运行资格

原采集器、校验器、CI、执行身份守卫、必跑 manifest 和五个 `test_unapproved_*` 负控保持。原 `REVISION*` 常量保留。revision 13 的两个原正向断言正文在精确公开 be3a clone 中按原字节执行，历史采集前后核对 HEAD、tree 和干净状态。只增加两个 revision 14 当前正例，目录模块为 **31 个方法**。小型事务正例只将当前源计数调整为 402 函数／64 文件；兼容性计数仍是 133／136。

维护继续使用原命令：

```bash
python3 -B scripts/check_r5_transactions.py
python3 -B scripts/check_r5_compatibility.py
node scripts/collect_api_calls.cjs --check
PYTHONPATH=scripts:scripts/tests python3 -B -m unittest \
  test_r5_transactions test_r5_compatibility \
  test_r5_catalog_reconciliation test_r5_current_source_inventory \
  test_r5_reviewed_web_locks -v
```

本地本次执行时，HEAD 仍是产品 merge `740126660526db987b7914c50a8731b7cac9bf42`，目录和测试更改尚未提交。三个原 CLI 更新后均 **exit 0**；上述五个必要模块共 **98 PASS、0 failures、0 errors**，耗时 131.555 秒。测试结束后对同一组源码、目录和测试字节构造的 Git tree 为 `8cda7b620b6bace98441960be9a6db86e584f868`。这记录的是实际未提交候选，不能回溯声称在后续维护 commit 上运行；本段验证说明是测试结束后唯一追加的内容。原测试输出、退出码和文件哈希另存本轮执行证据，整合端继续核对精确核心文件并运行原检查。

维护提交的实际验证和独立复核记录于维护 PR，精确 source-version CI 另行绑定维护源。完整产品 CI 的 PostgreSQL race、默认前端私有 fixture、严格依赖审计和其余未过门槛保持原结论；目录当前不使完整产品 CI 变绿。`runtime_verified=false`、`product_green=false`、`task_complete=false`，#56 保持 draft。
