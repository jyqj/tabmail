# CI backend 180 秒超时限定归因

**结论：现有证据支持 PostgreSQL 包的串行测试累计耗尽 180 秒预算。不能把超时归咎于当时刚启动的 ingress audit 测试，或认定 19 个数据库死锁。**

## 直接证据

- Run：`37619817397`；实际测试 merge SHA：`49f6a43483c9f8f5aca4a615071eb3818975d239`。
- ZIP SHA-256：`1a3e2afaf9240c474151df259b541630d996edb4772c6b2c1ec0461bb8620dbc`；`go-test.jsonl` SHA-256：`5e9748d086701d47251f6a66be9895ef15debaf5b9c62d205de67944e578ba50`。
- 本报告中的 L 为归档内 `go-test.jsonl` 的一基行号。未启动 PostgreSQL，也未修改产品、fixture、预算、选择集或 skip 门禁。

| 事件 | UTC 时间／耗时 | 证据 |
| --- | --- | --- |
| PostgreSQL 包启动 | 12:18:40.332466553 | L30070 |
| 当前 ingress audit 测试启动 | 12:21:39.967149843，包已运行 179.634683 秒 | L32653 |
| 全包闹钟触发 | 12:21:40.368853978，当前测试仅运行 0.401704 秒 | L32655–32657 |
| 包失败 | 180.052 秒 | L33004 |

**181 个已结束顶层测试：173 PASS、8 SKIP；其 Elapsed 合计 179.58 秒。** 此处每个顶层只计一次，不再叠加子测试；Elapsed 存在日志舍入。最后数秒仍连续产生 PASS。最长两个已结束顶层各 7.56 秒并已通过，未出现单个已完成测试等待接近 180 秒的证据。

## 超时现场与等待关系

| 线程／测试 | 当时位置 | 判断 |
| --- | --- | --- |
| goroutine 1 | `testing.T.Run` 等待当前测试（L32665–32679） | 正常测试调度等待 |
| goroutine 19745 | `seedCompany → NewPostgres → postgres.New → Migrate → Goose → sql.Tx.Commit → pgx socket Write`（L32801–32861） | 仍在独立数据库的迁移初始化；栈标为 runnable，不能据此认定长时间数据库锁等待 |
| 19 个尚无终态测试 | `testing.T.Parallel` 的 parent barrier | 19 PAUSE、0 CONT，全部未到两槽 admission acquire；不是 19 个 DB 会话互锁 |
| goroutine 19826 | `testing.M.startAlarm`（L32659–32663） | 原 180 秒全包闹钟 |

Go 1.25.7 的 `testing.go:1708` 等待父 barrier；项目 `r5_parallel_admission_test.go:58` 先调用 `t.Parallel()`，59 行才取得两槽许可。本日志没有任何 `r5_pg_admission: acquired`。串行阶段未结束，所以已经标为并行的 19 项尚未获得运行机会；提高这两个许可的容量也不会使这些暂停项提前越过父 barrier。

## 相关源码

| 位置 | 与现场的联系 |
| --- | --- |
| `internal/store/postgres/r5_ingress_transaction_test.go:149` | Test begins with seedCompany; audit-fault setup at line 152 and DeliverIngress at line 154 have not been reached. |
| `internal/store/postgres/company_workflows_test.go:36` | seedCompany calls NewPostgres before any company seed records. |
| `internal/testpg/fixture.go:55` | NewPostgres creates a unique database, then calls postgres.New with context.Background(). |
| `internal/store/postgres/postgres.go:38` | postgres.New has passed pool.Ping and calls Migrate. |
| `internal/store/postgres/migrate.go:79` | Migrate invokes Goose Provider.Up using the caller context. |
| `internal/store/postgres/r5_parallel_admission_test.go:58` | t.Parallel blocks before line 59 acquires the two-slot admission permit. |

`TestR5IngressAuditFailureRollsBackCheckpointAndQuota` 在 149 行创建 fixture；152 行才注入 audit CHECK，154 行才执行 `DeliverIngress`。现场尚未走到这些业务步骤，不能用测试名推断为业务审计事务卡死。`NewPostgres` 已走过 CREATE DATABASE 和 store pool.Ping，说明该现场并非缺失 DSN 或未连通数据库；具体迁移 SQL／锁 owner 仍未知。

初审时源码映射固定到可读 PR head `cbc8c17ebd3599d5c92711d28088b0bf4edc3f8f`，上述六个文件与工作树字节相同，而实际 merge 对象尚未在本地。随后 root 只读获取精确 `49f6a43483c9f8f5aca4a615071eb3818975d239`，核对它与 PR head 的完整 tree 均为 `25f28cde1bd6ad4bd3238d5c7b75a77431b385d0`，并逐一核对六个文件的 SHA256。因而这些位置现在已绑定实际 tested merge 的原 bytes；初审与后续验证分别保留在 JSON。

## 尚不能确定的部分

- 没有逐阶段计时，不能精确区分 CREATE DATABASE、19 个迁移、公司 seed、业务锁等待和 cleanup 对总时间的贡献。每次 fresh fixture 都走迁移是源码事实；“迁移占了大部分时间”仍不能据此断言。
- 没有 `pg_stat_activity`、`pg_locks`、磁盘或 CPU 采样；当前 COMMIT 的 migration 版本、服务端等待事件与等待持续时间未知。栈中的 runnable socket Write 是瞬间位置。
- 当前用例独立是否通过、19 个暂停项及其后未启动项目需要多少时间未知。没有证据支持通过改小选择集、放宽 timeout 或新增 skip 来获得完整资格。

## 下一批最小可执行范围

1. 在相同测试源码和 CI PostgreSQL 服务上，仅诊断 `TestR5IngressAuditFailureRollsBackCheckpointAndQuota`，保持 `-race -count=1 -timeout=180s`，取得终态。这是排除单用例故障的诊断，不能替代原完整门禁。
2. 对 fixture 的 CREATE／migrate／seed／body／cleanup 添加单调时钟阶段记录，再复核串行调度。先测成本，保持独立新数据库、原迁移和清理责任；不预先换成共享已迁移模板，不把 acquire 放到 `t.Parallel` 之前。
3. 优先测两个已观察到的成本组：`r5_atomic_retry_test.go:58` 的十个连续 fresh-company 子例（总 7.56 秒）；`r5_attachment_gc_tenant_fairness_test.go:281` 的三个中断恢复子例（总 7.56 秒，其中 process_killed 6.02 秒）。后者应分开记录子进程启动／kill／wait／PG session 释放，不能削减真实故障证明。完成后再决定有限实现优化，并重新接受原完整 180 秒门禁。

```sh
go test -json -race -count=1 -timeout=180s ./internal/store/postgres \
  -run '^TestR5IngressAuditFailureRollsBackCheckpointAndQuota$'
```

**本轮没有 PG 实跑或测试重试，没有重新调查已知三个 opt-in 问题，也没有宣称 CI、整套 PostgreSQL、发布或部署通过。**
