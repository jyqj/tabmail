# R5 B01-O：PostgreSQL 验收收口与审计外键死锁修复

2026-09-29。**B01-K 六组数据库测试已实际执行，发现并修复一个真实 40P01；加上三组新锁序测试，九组/22个测试事件连续三轮通过。完整 PostgreSQL backend 为 948 pass、0 fail、1 个明确 browser skip，108 必跑齐全；实际 HTTP 80 响应通过。解除缺测试 DSN 的运行阻塞，但不关闭完整 P0-070、P0-080 或 G0，清单仍为 6/171。**

## 1. 基线、范围与源码身份

开始 commit：`c39fdce62b0e7a0644eaad2361463f504f9ea7a0`，原工作区干净；本批分支 `test/company-postgres-closeout-20260929`。不重复 B01-L/M/N 的下载、解析器和文件后端实现。

最终程序被测 Git tree 为 `73d6b372ad2428a8562295724d63a4ada94d03a3`，包含 630 个受版本管理文件。它是冻结源码树，不冒称 commit。工作区与被测快照逐文件哈希核对；验收后仅增加或更新本报告、TODO、事务地图、历史说明、测试环境 README 和证据。最终提交以 Git 回执为准。

## 2. 真实数据库暴露的新缺陷

此前只编译的 `TestR5SentMutationOrdersGrantRevocation` 在当前 c39fdce 上实际返回 **SQLSTATE 40P01**。服务器日志显示互等的两个语句为：

```text
撤权：UPDATE mailboxes SET lifecycle_revision=...
邮件操作：INSERT INTO audit_log(tenant_id,...)
```

`audit_log.tenant_id` 在 `00001_baseline.sql` 中通过 FK 引用 `tenants.id`。旧顺序为：邮件操作先持 user SHARE、mailbox SHARE、item 锁，最后审计 INSERT 才取得 tenant KEY SHARE；撤权通过 companyTx 先持 tenant UPDATE，随后等待 mailbox。于是形成 `mailbox → tenant` 与 `tenant → mailbox` 的锁环。这里依据实际错误和服务器诊断，不是仅凭静态顺序推断。

原始 c39fdce 的六组 K 测试为 19 个事件：18 pass、1 fail。其余期限与回滚场景并非因没有 DSN 而跳过，而是在本批独立数据库实际通过。

## 3. 原位修复与边界

复用 `companyTxScope`，用明确的内部锁模式替换原 bool，增加 `companyReferencedTx`：

- 管理事务仍先取 tenant UPDATE；纯读取仍不取显式 tenant 行锁。
- 需要新增租户外键引用的 sent 修改先取 tenant KEY SHARE，再重载 actor、读取权限、锁 mailbox 和 item。
- 期限的锁后重验、审计和事件后的原始期限重验、revision 条件与回滚全部保留。

当前已接入的是 `MutateArchivedMail`。不删除 FK，不把审计移到事务外，不吞掉 40P01 或引入自动重试，不改为所有普通操作共用租户排他锁。普通 sent 修改现在可能在开始时等待同公司的管理锁，这是避免晚期外键锁环的有意顺序变化；独立普通修改可以共享父键，已用真实并发验证。没有取得整体性能无回归或全项目无死锁证明。

数据库锁模式语义参见 PostgreSQL 16 [显式锁](https://www.postgresql.org/docs/16/explicit-locking.html)。该文档只解释 KEY SHARE 的兼容性与一致锁序原则，不代替本仓库测试。

## 4. 测试控制与三层前后对照

新增 `r5_audited_parent_test.go` 三组测试：`TestR5SentMutationParentLockPrecedesActor`、`TestR5SentIndependentMutationsShareParent`、`TestR5SentMutationReloadsActorAfterParentWait`。它们分别证明父键在 actor 之前已受保护、不同 item 不被租户排他串行化、等待管理事务后不能接受已冻结的旧 actor。

原 K 撤权测试的屏障改为观察“撤权确实被该 writer 阻塞”，不再强制它必须走到晚期 mailbox UPDATE。正确的前置父键会让撤权更早等待；mailbox NOWAIT 探针、两个完整操作的结果以及撤权后的下一操作拒绝仍保留。新的父锁测试另行断言精确次序。

| 对照 | 实际结果与解释 |
|---|---|
| pre-K commit `1304ee05ecba665df3b09726c0084858ca7098cd` + 原 K 测试文件 | 19 事件：16 fail、3 pass。旧实现期限后仍读取/恢复，且缺少 mailbox 锁；取消和审计失败回滚对照本来就通过。 |
| 本批起点 c39fdce + 原 K 六组 | 19 事件：18 pass、1 fail；新发现 audit FK 与撤权 40P01。 |
| c39fdce + 最终锁序测试和修订屏障 | 4 顶层测试：3 fail、1 pass；死锁仍可复现，独立操作的正常并发对照通过，未靠调整屏障隐藏失败。 |
| 修复后的最终树 | 原 K 六组 + 新三组，共22事件/9必跑，连续三轮均 pass，0 fail、0 skip。 |

计数包含父测试和具名子测试，不是独立漏洞数。使用 `pg_blocking_pids`、NOWAIT 和数据库时钟观察真实等待及截止跨越，不用任意 sleep 推测次序。早期失败记录和修复后结果分别保留。

## 5. 最终验收

| 验证 | 实际结果 |
|---|---|
| 九组 PG 定向 race | 每轮22 pass，连续三轮共66；9个必跑齐全，0 skip。 |
| Go build / vet | 最终树上通过，`-mod=readonly`。 |
| 完整 Go/PG race | 949 started、948 pass、0 fail、1 skip；原包级预算180秒未放宽。 |
| backend 门禁 | 108 必跑，0 缺失；唯一允许 skip 是独立 `TestR3BrowserJourney`。 |
| 真实 HTTP/PG 契约 | 80 响应、65成功操作、66必需成功变体通过；2个实时DNS操作仍显式排除。 |
| Python / 静态契约 | 168测试通过；16共享Go/TS模型、33三方投影、67操作绑定通过。 |
| 缺DSN负例 | Go返回0，但新 content-postgres-boundary 门禁返回1并拒绝未执行/跳过的子测试；父节点pass不替代执行。 |

HTTP 使用生产 Router、应用服务及 PgStore，Redis/对象字节采用既有合成适配器，不运行 SMTP 投递 worker。本轮未重跑前端套件、shipping 浏览器、远端 CI、S/M/L 性能、当前依赖漏洞审计、真实 DNS 或 SSE 重连/跨进程回放。

## 6. 环境、可复现与清理

使用留存的 PostgreSQL **16.13 aarch64-apple-darwin25.6.0** 及同安装的客户端、pgcrypto/pg_trgm，Go 1.25.7 darwin/arm64、Python 3.12.11。没有升级或安装全局工具。全新0700临时目录、0700 Unix socket；TCP关闭，host认证拒绝。Go只使用已有模块/编译缓存、`GOPROXY=off`；未读取生产DSN、环境文件或邮件。

实例启动、实际测试与停止均由本批执行；结束时 `pg_ctl -m fast -w stop` 成功，启动PID文件确认消失。临时源码、失败日志和原始HTTP采集保持私有留存，不删除其他批次的目录或工具。原始 `responses.json` 不进发布归档，整个服务器日志留在私有临时目录，归档只保留所需死锁语句诊断。

可复现的原生隔离流程写入 `scripts/testenv/README.md` 的 Native PostgreSQL 16 fallback 节，复用现有 backend/HTTP门禁，不建立第二套测试引擎。一次进行中日志诊断调用在执行前被工具拦截，未重试该诊断；正式验证通过原 Job 句柄完成并保留结果。

机器摘要：[R5-B01-O-VALIDATION.json](evidence/R5-B01-O-VALIDATION.json)。原始记录：[R5-B01-O-LOGS.tar.gz](evidence/R5-B01-O-LOGS.tar.gz)，106成员、141501 bytes，SHA-256 `581ab28388c5c5dfc7b3dc2d4ec5605835b15b2ea453ea54ae5e7cde86b2373e`；成员清单及哈希见包内 MANIFEST.json，归档逐成员比对完成。

## 7. 后续仍须完成

缺DSN与 B01-K 局部运行阻塞已经解除，不再把它们列为下一轮起点。静态复查仍发现四个 companyReadTx + companyAudit 调用：`MutateWorkMessage`、`SaveMailTemplate`、`SetMailTemplateRetired`、`RevokeMailTemplateVersion`。它们是下一批需独立复现和选择适当锁边界的候选；本批没有给未经对照的函数一律加锁，也不把静态候选宣称为已复现漏洞。

普通收件历史期限、profile/override 一致快照、其余多资源锁图和 P0-080 协议真值表仍未完成，090/110/G0/P1 依赖保持。未改历史迁移、公开OpenAPI、Go module/npm lockfile或前端。仅本地提交，不 push、PR、merge、release、部署或运行生产迁移。
