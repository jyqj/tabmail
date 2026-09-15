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

上述七项在修复后本地及远端均已通过。零留存测试使用实际计划更新，而不是重复 SeedMailbox，以免测试配置未真正生效。所有新增测试仍保留在源代码中。

## 远端业务树验证

发布门禁 [34883934079](https://github.com/jyqj/tabmail/actions/runs/34883934079) 实际应用并校验业务补丁后，再分别验证旧树红灯和新树绿灯；不是借用旧主线 CI。业务补丁 SHA-256：`0fc0af2e608651f2be5f72af06e51a2d1877b415a484d9d8e71feda08868aeae`。

- 旧业务树：七项回归测试精确失败，失败名称均与预期一致。
- 新业务树：`go build ./...`、`go vet ./...`、`go test -json -race -count=1 -timeout=180s ./...` 通过。
- 真 PostgreSQL 16：**231 项顶层测试 / 347 项含子测试 / 30 个有测试的包，0 失败、0 测试跳过**。无测试文件的包不计为测试或跳过的测试。
- Go / TypeScript 字段契约：16 个共享类型通过。
- 门禁完成后才发布业务提交 `0732eedeee9bcc2fb6427733506e7eadf432944d`，并移除临时补丁传输目录；没有合并 main 或部署。
- 日志保存在该 run 的 `p0-publication-evidence` artifact（编号 `10364656101`）。原件为 JSONL，包含红灯与绿灯结果。

首次真库门禁 34883457498 揭示了 FakeStore 不能暴露的 `outbound_state` 枚举转换问题；该次验证失败且没有发布业务代码。修正 SQL 显式转换后，完整门禁重新运行通过。

最终分支保留普通、只读的 `company-p0.yml` CI，覆盖后端与前端；一次性发布门禁被移除。此处的业务树门禁结果不冒充尚未结束的最终分支 / PR CI，最终结果在 PR 检查与验收评论中记录。

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
