# R5 架构深化：九组优化实施与验收

## 来源与范围

用户于本轮明确要求一次性执行上传《TabMail 架构深化审查》的九组优化。实施起点为 `17fd8e4e570958fcf41f6b4e9a9f7341dd0e15d3`，分支 `refactor/company-mail-architecture-20260929`；起点工作区干净，保留全部 B01 历史工作。附件审查所用基线分支为 `test/company-mail-r5-b01j-index-validation`；本文件按附件九组候选保存实际实施边界、验证证据与未完成项。

本轮范围是下面九组代码优化，不是把 R5 全部 171 项、G0–G11 或生产发布标记为完成。唯一活跃清单仍是 R5-TODO.md；在其中添加 AR01–AR09 细分包并登记依赖调整，不提前勾选未验收的父项。架构结构性子项可按本轮明确授权提前实施；涉及既有协议行为变化必须说明兼容性与实际验证。禁止通过跳过测试、放宽授权、删除断言或重写历史证据取得绿灯。

## 九组实施边界

1. **AR01 凭证与审计输入单源**：统一注册、改密、员工激活的密码规则（现有改密为 12–72 字节，避免将字符数与 bcrypt 字节上限混同）；统一审计 reason 的 TrimSpace 后 8–1000 字节校验；邀请随机值签发/摘要收敛，保留一次消费与已有签发期限语义。模块依赖单向，禁止 store→app→store 循环。增加 Unicode、空白、上下界与各入口回归。
2. **AR02 邮箱 SQL 资格与时钟**：复用 postgres 谓词构造器，至少覆盖 mailboxes/outbound/submissions 可读列表；tenant 是顶层且无 OR 绕过；邮箱存活与 owner/read grant 统一。管理元数据的 send-only、留存保护不能被误改为普通阅读授权，区别需命名并测试。仅统一存活/租约判断时钟，禁止无差别替换所有 now()；保留事务时间用于合法审计时间。真实 PG 与 authz 语义矩阵验证跨租户、过期、owner、grant。
3. **AR03 投递状态规则单源**：共享纯状态转移/汇总逻辑，贯通 Go 状态常量、Pg/Fake 和 submission 投影；保留 SQL 的原子 lease/token/state 守卫和锁顺序，不用纯函数替代数据库并发防护。uncertain 不自动重发；SMTP accepted 不等于最终送达。覆盖非法转移、重放、租约过期、混合收件人。
4. **AR04 退役出站 legacy**：FakeStore 实现逐收件人 ledger 三方法，单测走生产 deliverRecipients；新任务总启用 ledger，删除 deliverDomains 和运行时双路径。新增迁移（不改 00001–00013）处理 recipient_ledger=false 历史行：保留已接受/失败证据，无法证明安全重发的 processing/in-flight 必须进入受控 uncertain，而非重置 pending。加入迁移与重启/部分成功回归。
5. **AR05 app/permissions 与错误映射**：真实落地应用服务、窄端口、zone/tenant/system-profile 规则单点；handler 仅协议与响应。复用 app.FromAuthz 和统一 HTTP mapper，但保留有意隐藏存在性的 404 与明确操作拒绝的 403 区别。不把只搬代码称为 P1 revision 协议完成；如实现协议则必须前后端、增量迁移、原子 CAS/审计/旧写入拒绝成套完成。
6. **AR06 入站公共投递内核**：保留 Durable=false 配置的兼容外壳，本轮不静默删除部署选项；统一 raw/headers/MIME/OTP 与逐目标投递核心，durable/non-durable 仅记录/执行外壳不同。复用 mailcontent 有界解析（现有 25MB/512 parts），两个路径都包含 In-Reply-To/References；不削弱持久接受、固定目标、配额或恢复语义。测试两路径等价、超限、坏 MIME、缓存、配额、失败。
7. **AR07 company Go/TS/HTTP 契约**：当前 B01-J 证据已显示 16 共享模型+32 公司 DTO 检查，不直接照搬附件“零覆盖”。核对实际 checker 的字段、嵌套/可选/类型及 OpenAPI 盲区并补齐，用故意漂移负例证明门禁会失败；复用现有脚本与 CI，不另建平行类型引擎。
8. **AR08 窄端口与共享策略**：按现有 company 角色端口模式收窄实际消费者；store.Store 只留装配边界，不机械给每个函数加接口。不再在 Pg/Fake 重写最后管理员/actor 层级纯决策，保留 Pg 事务内重新加载、锁、计数和审计。补接口断言和策略表测。
9. **AR09 快速收口包**：删除确无调用的 retention ListExpiredObjectKeys/DeleteExpiredMessages（核验全库）；mailbox revision 绕行改为已有 helper，锁顺序不变；mailtoken 并入 authn 时保留令牌用途隔离、现有 wire format、签名校验及兼容测试，不能将不同凭证草率换成 JWT；第二段脱敏并入 RedactOutboundJob 且保持 copy/no leak；前端 errorCode/isConflict 复用并测试；记录 workqueue outbound MarkDone/no-op 与 outbox MarkDead/retry 的明确协议，不重写健康协调器。

## 验证与交付要求

- 先复核源码与规范，实际缺陷做红绿对照；纯结构重构跑行为等价回归。
- 不改变 Go 模块、前端 lockfile 与历史迁移来逃避编译/验收。
- 使用 scripts/testenv 与现有不可变测试工具镜像、隔离 PostgreSQL、只读缓存；禁止读取 .env/生产凭据/员工邮件，禁止公网 SMTP、生产迁移、发布、强推或清理其他项目。
- 本轮代码写完后执行真实 PostgreSQL 的 Go 全量/race/build/vet、必跑证据门禁、定向新增回归、Python/Node/checker、前端 Vitest/tsc/lint/build。浏览器与远端 CI 未运行则明确列出，不复用旧成绩。
- 原始日志、实际退出码、pass/fail/skip、源码树/文件哈希、资源身份和清理结果留存；失败不覆盖。
- 最终将每组准确标成完成/部分/阻塞，未解决边界具体写出。提交由主会话审查后完成；不自动 push/merge/deploy。

## 执行结果

### 总体结论

本轮已完成九组架构优化的代码实施与预提交验收；其中 **AR07 的 Go↔TS 契约校验已完成，但 OpenAPI YAML 尚未成为可执行类型源**，因此严格按「Go / TS / HTTP 三方同源」口径标记为部分完成。其余八组均在本轮约定边界内完成。这里的“完成”仅指本轮架构收敛包，不改变 R5 原 171 项的统计口径，也不代表 G0–G11、P1 revision/CAS 协议或生产发布已完成。

| 工作包 | 状态 | 本轮落地 |
|---|---|---|
| AR01 凭证与审计输入单源 | 完成 | `internal/app/credentials` 统一 12–72 字节密码、TrimSpace 后 8–1000 字节审计理由和 256-bit 邀请秘密签发/摘要；注册、改密、激活、离职、恢复、break-glass、suppression 删除等入口均引用同一策略。 |
| AR02 邮箱读取资格与时钟 | 完成 | `readableMailboxPredicate` 统一普通读取列表的 tenant/alive/owner/read-grant SQL 资格，存活判断使用 `clock_timestamp()`；管理元数据与留存保护保持独立语义，没有机械替换所有事务时间。 |
| AR03 投递状态机单源 | 完成 | `internal/delivery` 统一 recipient 转移、worker 终态、对账汇总和 submission 投影；Pg/Fake 共享纯策略，数据库原子 token/lease/state/row-lock 守卫保留，claim/requeue SQL 状态绑定 Go 常量。 |
| AR04 退役 legacy 出站路径 | 完成 | 删除 `deliverDomains` 运行时分支，outbound consumer 编译期要求 recipient ledger；FakeStore 实现真实 ledger 链；`00014_recipient_ledger_required.sql` 保守迁移存量行，歧义接受结果进入 `uncertain`/人工复核而非自动重发，并禁止旧 writer 再写 `recipient_ledger=false`。 |
| AR05 app/permissions 与错误映射 | 完成（架构边界） | 新增窄端口 `app/permissions`，zone/tenant/system-profile 规则单点；handler 从 439 行收敛到协议适配；app error mapper 统一 400/403/404/409/429/500 契约并保留存在性隐藏。未把本次结构重构冒称为 P1 revision/CAS 协议完成。 |
| AR06 ingest 公共投递内核 | 完成 | durable/non-durable 共享有界 MIME 解析、9 个 header、OTP、store policy、配置/大小门禁和 retention/message 构造；两种外壳仍分别维护其 durable ledger 与同步 reservation/persistence 语义，`Durable=false` 兼容选项未静默删除。 |
| AR07 Go↔TS↔HTTP 契约 | 部分完成 | 现有 checker 扩展为字段、optional/null、标量、数组、嵌套引用、重复模型/字段校验，覆盖 16 个共享模型和 32 个 company DTO，并由 Make/CI 调用；负例测试证明漂移会失败。`internal/api/openapi.yaml` 仍未编译，HTTP spec 级漂移继续作为 `R5-P8-080` 明确盲区。 |
| AR08 角色端口与共享策略 | 完成（消费方边界） | outbound 与 permissions 等实际消费者改用窄端口；最后管理员/actor 刷新和投递状态策略由 Pg/Fake 共享。`store.Store` 继续作为装配聚合边界，未为追求形式纯度机械拆散。 |
| AR09 快速收口 | 完成 | 删除 retention 死方法，revision 绕行归并 helper 并以真实 PG 锁序测试固定；mailtoken 并入 authn 且保留 wire format/用途隔离；出站脱敏集中到 submissions；前端 `errorCode/isConflict` 单点；workqueue adapter 非对称行为获得明确契约说明。 |

### 兼容性与迁移

- 历史迁移 `00001`–`00013` 未修改；新增 `00014` 为协调停写迁移，使用表锁阻止旧 writer 与回填并发。无法证明未被 SMTP 接受的历史行不会回到 pending/retry，而是保留既有证据并进入受控复核。
- mailbox token 仍使用既有 `payload.hexhmac` 两段 wire format，不改成三段 JWT；access token 与 mailbox token 即使共享 secret 也通过身份 claim 相互拒绝。
- 入站 `Durable=false` 仍受支持；本轮只合并公共内核，不改变两种持久化承诺。
- Go module、前端 lockfile、历史迁移和现有健康深模块未为通过测试而改写。

### 预提交验收

被测源码快照包含 587 个文件，manifest SHA-256 为 `5c754d573ef577b28fe87084ba9ba10d4c56a368a2719700098a6ab074cd9027`。验证使用隔离 PostgreSQL、只读 Go 模块缓存和固定前端 lockfile；未读取生产 `.env`、员工邮件或生产数据。

- `go build -mod=readonly ./...`：通过。
- `go vet -mod=readonly ./...`：通过。
- `go test -mod=readonly -json -race -count=1 -timeout=180s ./...`：722 个测试事件，721 pass、0 fail、1 skip；唯一 skip 为需要浏览器环境的 `TestR3BrowserJourney`。37 个 package 通过、0 个 package 失败。
- `check_go_test_evidence.py --suite backend`：通过；必跑证据门禁接受该轮结果。
- Python：88/88 通过；契约 checker 报告 16 shared models + 32 company DTOs 通过。
- Node 源码扫描：23/23 通过；API client inventory 为 125 个调用分支、7 个显式 transport forwarder。
- 前端：`tsc --noEmit` 通过；Vitest 25 个文件、115 个测试通过；ESLint 通过；Next.js production build 通过并生成 29 个静态页面。
- 全变更集：78 个新增/修改 Go 文件通过 gofmt；`git diff --check` 通过。

前端第一次 production build 因验证容器把 `node_modules` 链接到项目根之外而被 Turbopack 拒绝；改为同文件系统 hardlink 副本后，同一代码快照构建通过。该失败属于验证装配，不是产品代码回归，原日志未被覆盖。

### 明确未完成 / 未执行

- OpenAPI YAML 尚未由 checker 编译，HTTP spec 级字段/nullable 漂移仍待 `R5-P8-080`。
- 浏览器 journey、远端 CI、生产迁移演练、部署、性能基准和当前依赖审计未在本轮执行。
- 本轮未 push、未创建远端 PR、未 merge、未 deploy；生产数据与服务未触碰。
- 临时验证日志位于工作区外或 `artifacts/architecture-20260929`，后者在提交前删除，不作为产品源码提交。
