# R5 目录 revision 13：DELIVER 三轮公开源码对账

本目录将当前静态目录对齐 [DELIVER 第三轮实际合入源](https://github.com/jyqj/tabmail/commit/9b73b13f376f7e8d589a06fc7078dae4c342d5d5)。本批十个限定实施 Issue 已分别由 PR #179、#180、#181 合入并关闭；本次目录、测试历史迁移和记录维护 **新增实施 TODO 为 0**。父任务仍为 **10/171 已验收、剩余 161**，运行和发布资格另行验收。

## 输入与历史身份

| 对象 | 固定身份 |
| --- | --- |
| 当前产品公开 source | `9b73b13f376f7e8d589a06fc7078dae4c342d5d5` |
| 当前产品完整 tree | `8e3f458f8cec51c031a06fd280151116164244ec` |
| revision 12 公开目录 commit | `b7247c3f66bd5f0ec5c6390305e2ff7268c135f8` |
| revision 12 目录 tree | `d9cce4ad1fe27c66e8954346894b69516364da4d` |
| revision 12 原产品 source | `ed81ee2fcadc9e8cfcd165947561f8b4b65589e6` |
| revision 12 原产品 tree | `3abdbfca4c0318644a01755c6e6bbb1929b74633` |

root 先核对 GitHub 实际 merge、完整 tree 及两个 parent，再 fetch 到普通 clone。冻结和原采集器执行前后均为同一个干净的当前产品 source。四份旧目录分别保留公开 commit、Git blob、SHA256 和长度；revision 1–12 的 **116 个历史文件**保持原字节。生成不会回写旧快照或把旧版本断言改成当前预期。

原三个 CLI 在冻结源上均实际 exit 1：事务目录报告调用/源码位置漂移，兼容性检查报告客户端产物与旧目录不同，TypeScript 采集器报告模板编辑器调用行号漂移。原输出保留在工作证据包；这些是实际旧目录拒绝新源码的结果，未用环境错误代替失败基线。

## 当前事实

[reconciliation.json](reconciliation.json) 包含四份生成文件的哈希、逐项变化、旧快照身份、历史/保护源清单和各历史目录对当前源码的实际拒绝结果。

| 原采集器事实 | 当前值 |
| --- | ---: |
| PostgreSQL 源文件 | 63 |
| PostgreSQL 函数 | 401 |
| 词法 SQL 执行调用 | 501 |
| migration 文件 | 19 |
| 注册路由 | 133 |
| 客户端调用分支 | 136 |
| 兼容性源闭包文件 | 95 |

此次实际变化包括 **61 个调用者列表、19 个函数语法记录、17 个派生断言记录**，合计影响 **70 个事务条目**。兼容性目录有 **6 条路由的源字段**变化和 **8 个闭包文件哈希**变化，路由集合没有增删。原 OpenAPI 缺口、没有客户端的路由、人工升级判断和运行未验收条件均保留，未给出新豁免。

从上一产品 source 到当前产品 source 共记录 **27 个源码路径**，包括生产代码、共享类型和 FakeStore 支撑模块。95 文件闭包只表示原兼容性采集器的限定覆盖；`excluded_closure_product_paths` 明确列出闭包外路径，不将其解释为整个产品的完整覆盖。两份小型正向测试仍分别断言 401 个函数、63 个 PostgreSQL 文件、19 个 migration，以及 133 条路由、136 条客户端调用分支；这些预期未变，因此两个测试文件字节没有改变。

## 唯一 PostgreSQL 函数体变化的手工审查

`internal/store/postgres/users.go` 是唯一内容发生变化的 PostgreSQL 源文件；其中只有 `CreateRefreshToken` 的函数体哈希变化，当前为：

```text
66e6bd4224e13273b7bf3904de303f128c1d4cfb980f1773b24ebf879198f1d0
```

携带签发证明时，真实路径为 Begin → 用户行 `FOR SHARE` → 比较用户/租户 ID、密码哈希、session_version 和 active → refresh token INSERT → Commit。用户行锁保持到事务结束；当前生产入口 `issueTokenPair` 总是提供证明。nil-proof 路径保留既有内部调用兼容行为，不能据此声称无证明的交互登录也受同一签发协议保护。

原 AST 采集器按调用名称识别 SQL，可以看到 `tx.QueryRow`，但不会对 `exec(ctx, INSERT...)` 这种函数值别名做类型解析。`exec` 由 `pool.Exec` 或 `tx.Exec` 赋值，事务归属经源码手工审查；**501 是原词法采集结果，不是完成别名解析的证明**。该条目的分类新增显式锁、事务调用及动态 SQL 标记，仍为 direct-write。

该函数原来的锁边界、证据、风险和文件族说明完整保存在 `historical_revision12_review`。当前手工说明只更新这个已审查的函数；其他函数的人工判断和未知字段保持。保留的旧审查文字带有历史边界，当前语法位置和派生事实由新采样负责。没有迁移变更，也没有声称其他事务关系、外部触发器或提交确认丢失已经验收。

真实 PostgreSQL 登录签发回归由 [PR #179](https://github.com/jyqj/tabmail/pull/179) / [Issue #170](https://github.com/jyqj/tabmail/issues/170) 单独提供：固定 9 叶红绿。其取消控制停在用户读取屏障，没有扩大为 INSERT 正在执行期间的取消证明。目录本身仍明确 `runtime_verified=false`、`product_green=false`、`task_complete=false`。

## 检查和证据边界

原采集器、校验器、CI、执行身份守卫和必跑 manifest 保持；保护清单含 **169 个源文件**，原五个 `test_unapproved_*` 负控和原 `REVISION*` 常量保持。revision 12 的两个原正向断言正文在真实公开 b724 clone 中按原字节运行，clone 在原采集器执行前后检查 HEAD、tree 和干净状态。当前增加两个 revision 13 正例，目录模块共 29 个方法，分别核验新事实/手工变化与历史/保护源保全。没有把生成成功等同于测试通过。

维护验证使用原命令；执行身份、作者/独立审查和 GitHub 完整 source-version runner 的真实结果记录于维护 PR，并从 [活跃整合 PR #56](https://github.com/jyqj/tabmail/pull/56) 链接。原始日志属于各自实际执行源，不能换成后来的 commit 身份。

```bash
python3 -B scripts/check_r5_transactions.py
python3 -B scripts/check_r5_compatibility.py
node scripts/collect_api_calls.cjs --check
PYTHONPATH=scripts:scripts/tests python3 -B -m unittest \
  test_r5_transactions test_r5_compatibility \
  test_r5_catalog_reconciliation test_r5_current_source_inventory \
  test_r5_reviewed_web_locks -v
```

Go、Python 依赖和 TypeScript 采集器采用仓库原版本约束与既定准备过程；完整 source-version runner 和原负控不作降级。发布仍需完整 PostgreSQL race、严格依赖审计、必需私有 fixture 及其余原门槛通过。#56 保持 draft，main 未合入。
