# P0 验证记录

业务基线 `e153631cbcbf03fb5738d3846f4eff1edf39e772`。测试不是生产部署、浏览器验收或外网投递验证。

## 红灯证据

在修改业务代码前增加七项回归测试，实际运行并观察到失败：

| 测试 | 原始行为 |
|---|---|
| TestP0EmployeeCannotUseUnassignedMailboxFrom | 未授权员工可使用租户内其他邮箱 From |
| TestP0TenantAdminCannotResolveAnotherTenantMailbox | 普通管理员可解析其他租户私有邮箱 |
| TestP0BreakGlassRequiresPersistedAudit | 审计写入失败仍返回例外读取结果 |
| TestP0DurableFailureRetainsOriginal | 零投递失败任务不保留可恢复原件 |
| TestP0DataAcceptedQuitDisconnectedIsSuccess | DATA 成功后的 QUIT 断开返回失败 |
| TestP0ZeroRetentionMeansNullExpiry | 0 小时仍生成到期时间 |
| TestP0BaselineDoesNotDropHistoricalGrants | baseline 会删除三个历史授权表 |

上述七项在修复后本地已通过。零留存测试使用实际计划更新，而不是重复 SeedMailbox，以免测试配置未真正生效。所有新增测试仍保留在源代码中。

## 验证层次与限制

本地使用 Go 1.25.7 和匹配 go.mod 的 vendor 依赖；vendor 不提交。已运行 P0 定向测试（含完整 Router）及 Go/TypeScript 共享字段检查，16 个共享类型一致。

真实数据库测试使用 `TABMAIL_TEST_DB_DSN` 创建单独 `tm_test_*` 数据库，不清理传入 DSN 的数据库。没有 DSN 时显式跳过，不能把跳过记为数据库验证成功。隔离数据库的场景包括：

- 部分消息写入、审计失败的事务回滚；每日配额/完成墓碑/outbox 与消息一致。
- 重复收件人、部分投递后重试、删除已投递消息后重试，不重复成功目标。
- 固定目标的删除/地址复用不重定向历史邮件；原件丢失/校验失败进入可观察的保留状态。
- DB 时钟租约和替换令牌拒绝旧 worker；旧队列接口不能绕过新账本。
- 已知成功出站域在重新领取后保留；过期未确认域禁止自动或手工盲目重发。
- Owner / grant 的跨公司外键，整理必须含读权限，NULL 永久留存和存量 owner 邮件 TTL 豁免。
- 全新 Goose 初始化、真实 v1→v2 升级、重启、旧同名授权表安全中止并保留数据。

远端验证以本 PR 当前业务提交的 Actions 结果为准：必须分别记录 build、race tests（含 PostgreSQL）、vet、契约检查，以及前端类型/测试/lint/build。历史 PR #5/#7/#8 的结果不替代这些检查。

原主线的 `check_i18n_keys.py` 与现有 locale 对象格式不匹配，本地报 `missing const zh: Messages object`；本轮未修改本地化目录，未把该检查标为通过。模板治理/邀请/前端工作台不是本轮的已完成内容。

## 不得扩大解释的保证

按域恢复不是逐收件人端到端 exactly-once；未收到 DATA 最终确认或接受后本地进度无法确认时，failed + 非空 in_flight_domain 保留不确定性并禁止重放。恢复 UI、人工核对和细化账本属于后续。

`template_only` 在正式发布模板治理落地之前拒绝发送。共享公司邮箱还需要 P1 明确资源/留存类型。无主集成 API Key 的租户级边界保留，不应分发给员工替代个人授权。

非持久化入站兼容路径不提供持久化账本的恢复保证。生产保留 `TABMAIL_INGEST_DURABLE=true`。失败原件保留不是备份，容量、隔离区处置和联合恢复演练仍需运营验收。
