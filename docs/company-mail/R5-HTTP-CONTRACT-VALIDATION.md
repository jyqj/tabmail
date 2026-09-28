# AR07 HTTP 契约闭环与集合输出修复

## 范围、基线与源码身份

接续用户要求的下一轮优化，基线 commit 为 `c0e4795119aca7b296e43d5b69724549860ffe44`，分支 `test/company-http-contract-20260929`。本批保留并审查了前序中断留下的改动和原始记录，完成响应绑定、真实 HTTP 验证、采集器边界以及一个实际序列化缺陷的修复。没有重做上一轮架构拆分或覆盖其他分支。

最终被测 **Git tree** 为 `6f0bbdf5f5be29914be470fa529894fb12c285c2`，598 个受版本管理的快照文件在测试后与工作区逐字哈希核对一致。后续提交仅增加本报告、进度说明与证据；不把 tree 写成 commit，也不把较早候选的结果当成最终验收。

本批为 AR07/`R5-P8-080` 的 HTTP 响应子包。67 个公司操作的静态响应绑定已齐，65 个操作执行了真实 HTTP 成功路径；两个实时 DNS 操作明确排除。完整 P8-080 的前置、旧协议和所有错误分支、P0/G0、P1 权限 revision/CAS 均未因此关闭。原清单保持 6/171。

## 实际实现

| 范围 | 实现与边界 |
|---|---|
| 剩余响应绑定 | 原有 36 个具体响应绑定扩展为全部 67 个公司操作：61 个 JSON、6 个非 JSON；新增 handler 无明确绑定、遗漏成功码或包装错误均使静态门禁失败。提交同时校验新建 201 与幂等重放 200。 |
| OpenAPI 修正 | 补齐操作回执、revision、分页和错误包装；下载区分原始 EML 的 `message/rfc822` 与附件的二进制类型，事件流不伪装为 JSON。受约束回执采用具体字段而非空 `data: {}`。共享存储对象的全面 DTO 退役仍是后续工作。 |
| 实际响应验证 | `TestCompanyHTTPContract` 使用生产 Router、应用服务和 PostgreSQL 适配器，通过 loopback HTTP 捕获状态码、头和响应字节。对象存储及 Redis 使用合成测试适配器，不启动 SMTP 投递 worker。 |
| JSON Schema | `check_http_contract.py` 使用固定依赖的 JSON Schema 2020-12 验证器，不另写 schema 引擎；UUID/date-time 校验缺失即失败，引用仅在内存中文档内解析，不下载远程 schema。 |
| 采集器安全 | 限制整份采集和单响应大小，base64 在解码分配前检查长度；拒绝重复 JSON 字段、非有限浮点、超过 64 层嵌套、错误路由/状态、重复/缺失场景和未经解释的 handler。场景名称不能替代实际 method/route/status。 |
| 下载与事件流 | 下载必须 private/no-store、nosniff、强制附件且字节非空，Go 侧比较合成附件和原件的准确字节。SSE 必须具有完整帧和 TabMail JSON 对象，校验缓存、防缓冲及敏感字段；仅注释或半帧不能计为成功。 |
| 执行证据 | 新增 `http-contract` 必跑套件，Go 全量 backend 必跑项由 54 增至 55。缺 DSN、skip、没有 capture 或测试失败都不能通过。超时回收整个测试进程组，失败结果单独保留。 |
| CI 与 Make | 新增实际 HTTP 门禁并沿用现有工作流；原始 `responses.json` 为本地 0600 文件，证据目录 0700，CI 显式排除原始响应，只发布结果、哈希和执行日志。 |

## 真正修复的业务问题与红绿对照

草稿的必需 `payload.to`、无变量模板的 `variables` 及嵌套版本快照原先可输出 `null`，与前端和 HTTP 数组契约不一致。新增 `companyWireValue`，仅在 HTTP envelope 边界复制并规范化这些集合为 `[]`。覆盖值、指针、列表、草稿内固定模板版本及 compose payload。

不能为解决此问题给领域类型添加 `MarshalJSON`：既有模板哈希及草稿创建幂等记录也依赖原 JSON 编码。本次保持持久化字节、摘要、缓存对象不变；未配置公司仍返回 `data: null`，与本次不相关的响应也不被泛化修改。

在原基线加入完全相同的新测试、不加入修复，12 个集合子用例全部出现目标失败；领域摘要/nullable 保持测试通过。修复后 12 个子用例及两个顶层测试均通过，纳入最终全量 race。12 个子用例不是 12 个独立业务漏洞。

采集器的浮点溢出、SSE 敏感字段、冲突缓存指令、过大 base64、嵌套深度、原始响应上传等反例也已保留。前序候选的失败与最终通过分开归档，不修改或删除失败断言换取通过。

## 最终验收

| 验证 | 实际结果 |
|---|---|
| Python 完整测试 | 168 个测试方法通过。 |
| 静态契约 | 16 shared Go/TS 模型、33 三方投影、67 个操作绑定通过；61 JSON + 6 非 JSON。 |
| `go build -mod=readonly ./...` / `go vet -mod=readonly ./...` | 通过。 |
| `go test -mod=readonly -json -race -count=1 -timeout=180s ./...` | 737 started、736 passed、0 failed、1 skipped。 |
| backend 必跑门禁 | 55 项齐全，0 缺失；唯一允许 skip 为单独的 `TestR3BrowserJourney`。 |
| 独立实际 HTTP 门禁 | 80 份响应，65 个成功操作，66 个必需成功变体（含提交重放）；全部通过，2 个实时 DNS 操作显式排除。 |
| 缺 DSN 反例 | Go 本身 exit 0、目标测试 skip；HTTP 门禁 exit 1，明确报告 1 个必跑缺失，证明 skip 不会成为通过。 |
| Node / i18n / API inventory | 23 个 Node 测试通过；语言与调用矩阵通过，125 调用分支、7 转发器不变。 |
| 前端 | TypeScript、Vitest 25 文件/115 测试、ESLint、Next production build 全部通过；29 个静态页面。 |
| 源码完整性 | 598 文件哈希一致，gofmt 与 diff check 通过；Go module、前端 lockfile、00001–00014 迁移未改变。 |

实际 HTTP 流程覆盖公司初始化、邀请与激活、邮箱授权和发送策略、模板保存/发布/预览/撤销、私人草稿、发送/重放、收件详情/原件/附件、已发送资产、恢复/对账、交接/离职、审计、SSE ready 以及 400/401/403/404/409/500 响应。它不证明每个操作的所有授权组合和错误分支，也不等价于真实浏览器旅程或公网投递验收。

## 环境、失败历史与证据

Docker daemon 本批不可用，没有启动或重启全局 Docker。表格统一引用最终 `6f0bbd…` 独立运行：Go 1.25.7 darwin/arm64、Node 22.23.1、Python 3.12.11、PostgreSQL 16.11（实际服务器报告 x86_64 构建）。服务只监听本批 0700 Unix socket，禁用 TCP；测试库由 `testpg` 单独创建。另一路较早 `7f931a…` 运行使用 PostgreSQL 16.13，原记录保留为历史，不与最终环境混用；客户端的具体版本以各次执行记录为准。本报告不是 shipping 镜像验收。

较早候选有依赖/原生客户端编译失败、过期源码快照、采集器负例失败、共享测试环境不可用引起的全量失败。这些均保留为候选/环境历史，不冒充最终产品失败，也不以较早 HTTP 成功替代最终树。最终在独立数据库重跑了完整后端、独立 HTTP 及前端；未放宽原 180 秒 Go 包级预算。

机器结果见 [R5-HTTP-CONTRACT-VALIDATION.json](evidence/R5-HTTP-CONTRACT-VALIDATION.json)，完整非敏感执行记录及前序失败见 [R5-HTTP-CONTRACT-LOGS.tar.gz](evidence/R5-HTTP-CONTRACT-LOGS.tar.gz)。原始捕获包含合成邮件和已消费/撤销的测试邀请值，只在本地受限目录留存，压缩包不包含 `responses.json`；其哈希用于溯源。资源清理状态以机器记录的逐项确认结果为准。

独立证据审阅逐一验证了最终测试目录的 598 份源码 SHA-256、Go 全量日志与证据门禁的日志哈希、原始 capture 哈希，并用最终 checker 离线重新验证 80 份响应。最终压缩包保留原有候选记录并追加 `verified-final-6f/` 与 `independent-review/`，共 241 份记录；早期 JSON receipt 也保留，未覆盖失败日志。原始 capture 仅参与本地离线校验，不写入归档。

## 未执行与下一步

两个实时 DNS 端点需要独立 loopback DNS fixture；当前 SSE 仅验证初始事件而非断线重连和跨进程回放。浏览器 shipping journey、远端 CI、性能基准、当前依赖漏洞审计、生产迁移/部署未执行。P0-070/080 及 P1 权限编辑事务协议仍按原依赖推进，不因本次响应契约检查通过而宣布完成。

本批仅提交本地代码、测试及证据；无 push、PR、merge、release 或 deploy，无生产配置、员工邮件或生产数据操作。
