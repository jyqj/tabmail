# R5 key touch：双 Router 实测与测试修复

固定 archive source：`66eb5e819170d3508edb07db8ab1e0014555ff8c`。仅修改 `internal/store/postgres/r5_key_touch_admission_test.go`，新增本专项文档与证据。亲读 `internal/api/middleware/apikey_touch.go`：`AuthState.lastTouch` 每 Router 独立；生产逻辑无需修改。

实测使用新建 PostgreSQL 17 cluster、loopback 55439、新数据目录。每个子场景通过 testpg 创建并清理独立数据库。未连接 5432、既有邮件或业务服务。Go 1.25.7、`GODEBUG=asynctimerchan=0`、原两 fork replace、schema19 保持；`-timeout=180s`、场景 12 秒、Touch 5 秒和锁探针 1500ms 均未放宽。

## 实值与因果

[保留原断言的诊断重跑](r5-key-touch-two-router-evidence/diagnostic-original-assertion-corrected.json)中，独立 GET HTTP 200；两个 Router 各调用一次真实 `PgStore.TouchAPIKey`，结果均为 `none`。主 backend 1506 在 after-row gate 持有 usage tuple；第二 backend 1501 的真实 metadata UPDATE 的 `pg_blocking_pids` 精确为 `[1506]`。

| 实际 trace | 主 Router | 独立 Router |
|---|---|---|
| holder PID | 1506 | 1501 |
| used_at | 2026-10-03T20:21:21.822131Z | 2026-10-03T20:21:23.335983Z |
| used_ip | 192.0.2.41 | 192.0.2.41 |
| previous_at | NULL | 主 trace 的 used_at |
| key / phase / usage identity / timestamp bounds / IP | 全 true | 全 true |
| 原 primary_holder / previous_null | true / true | false / false |
| current timestamp 等于此 trace | false | true |
| 原全表 count_one | false | false |

当前 usage 为第二条 trace 的 timestamp/IP。原断言因此错误地要求一个 Router 的提交代表全表；并非跨 Router throttle 应共享。原样首次运行碰巧通过，说明读取 trace 时第二个 Touch 可能尚未提交。[原运行](r5-key-touch-two-router-evidence/original.json)、[诊断 patch](r5-key-touch-two-router-evidence/diagnostic.patch)及所有诊断失败均保留。

第一版诊断还曾过早 Stop 主 Router，使 zone-revoked 的后续请求得到 503；这是诊断改动错误，已修正并保留[该次失败](r5-key-touch-two-router-evidence/diagnostic-original-assertion.json)。修正后的诊断仅 `usage_held_allows_retry` 原聚合断言失败，其余三个子场景通过。

## 最终约束与验证

双 Router 场景通过透明 wrapper 观测各自调用，仍立即委托真实 PgStore，保留原 request context/key/IP/error。第二 backend 在 gate 释放前由真实锁边识别。释放 gate 后物理 join 两个 Router lifecycle owner 和 wrapper workers，再要求全表恰好两条、每个观察到的 PID 恰好一条、两个 calls 各为 1、主 previous_at 为 NULL、第二 previous_at 等于主时间、第二时间递增，以及最终 usage 匹配实际最新合法 trace 的 timestamp/IP。authority 完整 JSON 前后相等断言、usage identity、authority free probe、独立 HTTP 200、retry/audit/outbox 恰一次仍保留。失败 cleanup 同样在 drop trigger 前释放锁并 join 双方。

其余三个子场景仍使用原 `r5TouchCommitted` 的严格单 trace 聚合断言；zone-revoked 后续请求仍使用未停止的主 Router 与真实一分钟 throttle。

- [四场景 race 连续 5 次](r5-key-touch-two-router-evidence/fixed-repeat5.json)：20 个子场景全部通过，package 31.290s。
- [同文件全部 3 个顶层用例](r5-key-touch-two-router-evidence/fixed-file.json)：admission、6 个 lifecycle cascade、metadata store/wire 全通过，package 10.800s。
- `gofmt`、`git diff --check` 通过；以上运行 stderr 均为空。[运行摘要和 SHA256](r5-key-touch-two-router-evidence/summary.json)及[验证时测试源码](r5-key-touch-two-router-evidence/validated-test.go.txt)可复查。

专项 TODO：已完成因果观测、最小测试修复及 race 验证。全量 `./...` 聚合 gate 本任务未跑；原 archive 的聚合失败证据未改，不宣称中央 CI 已通过。中央 TODO/CI 由整合 owner01a10366 负责。未 merge/deploy。

交付：实现 commit `30a0fd7380a11b8f4d6a9fdbd5bd6f3757cb0593` 已推送 `fix/r5-router-touch-oracle`。针对固定 source 所在 `feat/r5-archive-boundary-v1` 创建 draft PR 的 `gh pr create` 请求返回 `Post "https://api.github.com/graphql": Forbidden`；该动作停止，未绕路、重试或进行 auth 探测。draft PR 尚未创建，需要恢复 GitHub PR 写权限后完成。
