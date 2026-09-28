# AR07 后续：OpenAPI 可执行契约与路由接线

## 本批范围与源码身份

接续用户要求的下一轮优化。开始时 HEAD 为 `eb8382ab04daf28137460b4f695cbb36fc29dbd1`，分支为 `refactor/openapi-contract-gate-20260929`。已有四份未提交 OpenAPI 工作：CI、checker、checker 测试和 `requirements-contract.txt`。先保存其 patch 与哈希，再继续实现；没有 reset、stash、覆盖其他分支或重复执行上一轮九组重构。

本批完成 AR07 的 OpenAPI 静态验证与实际路由绑定子包，不关闭整个 `R5-P8-080`、P0/G0 或 P1 权限 revision/CAS。原任务统计保持 6/171。

被测 Git **tree**：`f03d0cbaa11376a7995a034651ffd3eac6ac14e2`。这是暂存源码树，不是假称已经存在的 commit。589 份快照文件在测试完成后逐一核验，哈希差异为 0。最终提交仅在此树之上补充本报告、TODO、历史报告后续说明和证据；程序、schema、测试、CI 与测试输入不再变化。

## 实际改动

| 范围 | 实现与校验 |
|---|---|
| YAML 与引用 | 固定 `PyYAML==6.0.3`，使用 SafeLoader；拒绝重复键、别名、递归/超深文档、超限文档和外部引用。解析失败/依赖缺失返回失败，不跳过 OpenAPI。所有文档内 `$ref` 使用 JSON Pointer 解析。 |
| DTO | 保留 16 个共享模型 Go↔TS 检查；32 个 company DTO 加实际授权明细 `MailboxGrant`，共 33 个投影，接入 Go↔TS↔OpenAPI 字段、required/omitempty、指针 null、标量、数组/字典形态、嵌套引用与 TS 字符串枚举检查。 |
| 真实漂移 | 补齐缺失的 company schema、必填字段、submission capabilities 和状态；修正 `template_only` 与 `template_required` 的错误枚举；把旧 MailboxGrant 角色描述替换为当前用户授权字段。前端 WorkGrant 补充可选只读元数据，编辑命令兼容不变。 |
| 路由与响应 | 复用既有 Go AST 路由矩阵，不另写 router 解析器。67 个公司 HTTP 操作全部有 OpenAPI 登记；36 个响应绑定校验具体 DTO、成功码、data 包装及分页 meta。补齐 10 个遗漏操作，含域名管理、SSE、草稿提交、submission 正文与附件。 |
| 请求/响应分离 | 新增 7 个输入 schema。公司首次设置允许 revision 零；私人草稿允许未填完整；模板保存仍要求有效主题和正文；对账只要求明确收件人结果，不要求服务端 attempt/SMTP 时间字段。初始未配置公司设置的 `data: null` 单独建模。 |
| 开发与 CI | 既有 company-p0 workflow 安装固定 parser；增加显式 `make contract-deps` 与隔离环境说明。既有 API 调用矩阵经 Node AST 重新核验，只更新 company.ts 增加 6 行后的 18 处行号，125 个分支和 7 个转发器未变。 |

## 红绿对照

继承的 checker 单测 44 项通过，但运行真实仓库检查失败，说明原工作还不能交付。新增十项 fail-closed 反例后，原实现出现 7 项错误放行和 3 项异常崩溃：混合标量/数组类型、嵌套 union、3.1 中旧 nullable 写法、重复 required、缺失 TS 字段、错误 components、缺 parser、非 schema 引用和远端引用。

修复后这十项通过。另加入真实仓库 mutation 测试，逐一破坏 33 个 DTO 字段、67 个操作和 36 个响应绑定，并检查分页、创建输入、对账输入、null 设置、额外路由、枚举重排、私密字段混入以及 YAML 安全限制。契约相关共 71 个测试方法；不能把循环内 mutation 数量再重复累加成独立方法数。

## 本次验证结果

环境：Go 1.25.7 linux/arm64、Node 22.23.1、npm 10.9.8、Python 3.12.14、PyYAML 6.0.3。工具镜像与 PostgreSQL 镜像精确 digest 记录于机器证据。只读复用 Go 模块缓存；源码复制进临时容器；依赖准备结束即断开外部网络，测试只连接本批 internal 网络与临时 PostgreSQL。

| 实际命令/门禁 | 结果 |
|---|---|
| `python3 -B -m unittest discover -s scripts/tests -p 'test_*.py' -v` | 128 方法通过；其中契约 71 方法。 |
| `python3 -B scripts/check_contract_drift.py` | 16 共享模型、33 company 投影、67 操作和 36 响应绑定通过。 |
| `go build -mod=readonly ./...` / `go vet -mod=readonly ./...` | 通过。 |
| `go test -mod=readonly -json -race -count=1 -timeout=180s ./...` | 722 run、721 pass、0 fail、1 skip。 |
| `check_go_test_evidence.py --suite backend` | 54 必跑齐全，0 缺失；唯一允许 skip 为 `TestR3BrowserJourney`，不冒称 browser 已跑。 |
| `node --test scripts/tests/*.test.cjs` | 23/23 通过。 |
| i18n / API client inventory | 通过；125 branches、7 forwarders。 |
| `tsc --noEmit` / Vitest | 通过；25 文件、115 测试通过。 |
| ESLint / Next production build | 通过；29 个静态页面。 |
| 源码与差异 | 测试后 589 文件哈希一致；`git diff --check` 通过；Go module、npm lockfile、历史迁移均未修改。 |

## 证据、清理与复现

[机器记录](evidence/R5-OPENAPI-VALIDATION.json) 包含被测树、源码 manifest 摘要、门禁结果、资源身份和未覆盖范围。[原始证据包](evidence/R5-OPENAPI-LOGS.tar.gz) 保留 54 份记录：继承 patch/manifest、原始失败、修复后结果、完整 Go JSONL、前端日志及清理记录。没有用最终绿灯覆盖原始失败。

证据包 SHA-256：`7987e5189dd3adac19a1e051876d689f604c706ef74a446dc1b4819b6561c58a`；大小 145365 字节。源码 manifest SHA-256：`916fa6a329c0666b2e6dcc1dbd7f6ecb7aab34612e3364f4a2a78527789da105`。

本批两只容器、关联匿名卷和 internal 网络均按 session 标签与完整 ID 核验后删除，再验证不存在；共享镜像、只读模块缓存与其他项目资源未清理。原始工作目录移出仓库留存，产品提交不携带临时源码 tar 或 node_modules。

后续复现应使用 `scripts/testenv/README.md` 的临时源码与数据库流程，先安装 `scripts/requirements-contract.txt`。Go 路由矩阵测试与 Python/OpenAPI 检查必须一起运行：仅跑 Python，不能证明手写矩阵仍对应新改动后的 Go 路由。

## 明确边界

31 个公司操作当前只获得路由/引用覆盖，尚未逐个绑定完整响应形状。16 个共享模型也仍是 Go↔TS 检查，不把持久化对象所有字段机械公开进 OpenAPI。静态字段检查不推导任意 JSON/custom marshaler、nil 集合归一化或 handler 任意返回表达式，更不替代真实 HTTP JSON Schema 全响应验证；这些属于 `R5-P8-080` 的后续完整验收范围。

本批没有运行 shipping-image browser journey、远端 CI、性能基准、当前依赖漏洞审计或生产迁移演练，也没有重新执行 A01–A07 独立复现。未 push、未创建 PR、未 merge、未部署，未读取生产配置、凭据或员工邮件。
