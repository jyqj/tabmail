# R5 B01-A：验证底座实施与证据

> 2026-09-28；B01 的第一组实现，不是 G0 / 全 R5 完成。唯一进度见 [R5-TODO](R5-TODO.md)。本批已完成 010、040、050 的本地实现与验收；未完成项目不冒充通过。

## 范围与源码归属

审计主线 `d6d512172fb6b874c3283d9df3f4d56758684a13`；本地规划提交 `68641047a043e9a16bf34c24c81a76132da2a758`，tree `2e43b9050a54e0e59118a38be8c6e394d24783db`。规划提交只增加/修改文档，不改变业务实现。开始前工作区干净，fetch 后 main/origin/main 均为审计主线，GitHub open PR 为 0。

本批独立分支 `fix/company-mail-r5-b01-validation` 从规划提交开始，保留原规划分支。改动仅为验证脚本、脚本测试、测试环境、现有 CI/Makefile 和进度文档；Go/前端业务代码、依赖锁文件、历史迁移均不改。本轮不发布、不合并、不部署，不读取真实员工邮件。

实现提交：`c4199e0b27a390ef352fd7b06555f7d33b6c72cb`。本轮运行结果与源码/日志哈希见 [机器可读证据摘要](evidence/R5-B01-A.json)。435 个业务/依赖源文件已对照被测副本，所有字节一致；13 个历史迁移哈希也已留存。这不等于完成 P0-060 的完整数据模型/风险清点。

执行使用 `git archive` 的临时源码副本，再覆盖本批精确脚本文件；不挂载工作区根目录、`.env`、生产对象目录或 Docker socket。副本地址 `/tmp/tabmail-r5-b01-X4NL55` 是本次本机证据位置，不是仓库内容或跨机器下载链接。Go/前端业务代码被测版本是上述规划提交；脚本结果还需结合本批脚本哈希，而不能声称规划提交中已存在新校验器。

## 本批实现

### i18n 检查恢复并扩大真实覆盖

`check_i18n_keys.py` 改读实际 `web/locales/zh.json` 和 `en.json`。缺失、无效 UTF-8、坏 JSON、重复键、空目录、无效值、两语言键集合不一致或缺少源码目录都明确失败。扫描增加 `features`、`hooks`，不将测试文件和构建缓存当生产源码。

`collect_i18n_keys.cjs` 使用 lockfile 安装的 TypeScript 编译器解析 AST，按绑定区分 `useI18n().t(key)` 与 `useText(zh,en)`，支持当前明确的双字符串回调类型。注释/字符串示例不会当调用；单引号、双引号、无插值模板及转义键都检查。混合文件不整体免检，也不能通过导入另一个包的同名 useText 规避。计算生成的 key 只记录数量，不宣称完整静态验证。

新增 Python 目录/错误边界测试，以及 Node AST 和实际 Python→Node 子进程集成测试。没有为了通过检查修改任何语言内容或关闭异常。

### 必跑证据门禁

`check_go_test_evidence.py` 对一次 fresh `go test -json -count=1` 的逐包/逐用例事件及真实退出码作检查：必须有 run、pass 和成功包终结；跳过、缺失、0 用例、截断、无效字段、未知事件、重复运行和失败后再 PASS 都不能通过。并发包、子测试及 pause/cont 正常处理；Output 中的 PASS 文本不被当作结果。

`required_go_tests.json` 当前包含 11 个 backend 基线哨兵（真实 PG、空库/升级、恢复、隐私/草稿/交接）和 1 个单独 browser 哨兵。它不是全量未来 R5 用例清单；后续实现时必须补相应必跑项。backend 唯一明确可跳过项是独立 browser job 负责的 `TestR3BrowserJourney`；browser suite 自身不允许跳过它。所有其他实际报告的失败或 skip 仍阻塞。

报告包含输入源码 SHA、日志和 manifest 的 SHA-256、执行计数及明确的 skip 原因。调用方仍须绑定实际被测源码并保存原始日志；此工具不把任意外来日志认证为可信，也不证明测试断言本身完整。

### 验证入口与隔离环境

保留已有四个 job、审计门禁和 shipping-image 浏览器流程，只增加证据校验；捕获 Go/管道失败码，不能被 tee 成功覆盖。browser 输出改为 JSONL，证据/源码标识和日志仍由 always artifact 步骤保留。Makefile 增加脚本回归和 AST 回归，并将它们纳入 check。

[测试工具镜像与运行说明](../../scripts/testenv/README.md) 使用 go.mod 的 Go 版本、CI 的 Node 22 major 和 PostgreSQL 16，设定 GOTOOLCHAIN=local、GOSUMDB、GOPATH 与共享构建缓存。工具不会安装到宿主全局环境，不使用生产 Compose。

## 环境与已执行检查

| 项目 | 本次实测 |
|---|---|
| 工具平台 | Docker Linux/arm64；宿主 macOS 的 Python 3.9 也执行脚本单测 |
| Go | 1.25.7，与 go.mod 一致 |
| Node / npm | v22.23.1 / 10.9.8；lockfile npm ci 成功，未重解依赖 |
| 最终镜像内 Python | 3.12.14；具有 tarfile 安全提取 filter |
| PostgreSQL client | 16.15；测试数据库是独立 PostgreSQL 16 容器 |
| 网络 | Go 数据库测试只连新建 internal 网络；无宿主发布端口；前端/脚本验证 network none |
| 资源 | Go 至多 4 CPU / 6 GiB；前端至多 2 CPU / 6 GiB；独立 PG 至多 1 GiB |

| 检查 | 当前实际结果 | 证据边界 |
|---|---|---|
| 原始 i18n 基线 | 退出 1：missing const zh: Messages object | 目标旧检查缺陷，不是新增功能失败 |
| Python 脚本回归 | 43 / 43 通过，0 skip | 包含 i18n 和 Go 事件校验反例；部分传输替身明确在测试中声明 |
| Node AST + CLI 集成 | 13 / 13 通过，0 skip | 其中 2 项实际启动 Python→Node，验证合法目录和 features 缺键 |
| 当前完整 i18n 扫描 | 118 个生产文件、1,325 个语言键、632 个不同字面键通过 | 30 个动态调用另报；452 个内联双语调用不当成字典键 |
| DTO 字段检查 | 16 个共享模型、32 个公司 DTO 通过 | 不是完整 OpenAPI 类型验证 |
| Go build / vet | 干净镜像与依赖副本通过 | 尚不单独代表 DB / HTTP / 浏览器回归 |
| 前端 Vitest | 108 / 108 通过，0 pending；60 / 60 suites 通过 | 既有前端测试，不是新七项审计修复证据 |
| TypeScript / ESLint / Next build | 全部通过；生产构建生成 29 个静态页面 | 未执行 shipping-image 浏览器旅程 |
| Workflow YAML / 接线 | 已解析并确认四个 job、退出码传递与新门禁，20 个 shell 块通过 bash -n | 本地解析检查，不是 GitHub Actions 实际运行 |
| 真实 PG 完整回归及证据 | 49 个包，539 pass / 0 fail / 1 skip；11 个 backend 必跑项全部通过 | 唯一 skip 为本轮未执行的 TestR3BrowserJourney；空库/升级/联合恢复基线实际通过 |
| 缺 DSN / 0 用例 / browser 未启用 | 三个实际 Go 进程均 exit 0，新门禁均 exit 1；正式驱动重新执行通过 | 分别观察到 95 个用例中 86 skip、0 用例、1 个 browser skip；不是合成 JSON 代替实际进程 |

完整 backend JSONL 的 SHA-256 为 `8deefc98e8492b6dcd148c5fdf90e91c19111a9f9da30d5cab8115a0a5213c71`，必跑 manifest 的 SHA-256 为 `5823dd01007d9e33d55dce90e28fc0c9237dc990db7c750bccf549e223a81652`。最终工具镜像 ID 为 `sha256:65934079f212a76983064db24e7a26066106a7e173cf2b05e4f1621aebfdb5c7`。

本机原始证据在上述副本的 `.validation/`：`backend.jsonl`、`backend-evidence-final.json`、`gates-final.log`、`web-validation.log`、`vitest.json`、`negative-verified/`、`source-integrity-final.json`。临时目录可能被操作系统清理，仓库保留紧凑摘要与复现脚本；不要把临时路径冒充永久 CI artifact。

## 本批过程中发现并处理的验证问题

Mac 临时目录的 `/var` 与 `/private/var` 别名使一个路径断言失败，测试夹具已统一 resolve，随后完整单测通过。首次离线 Go 测试发现缓存实际落在默认 `/root/go` 而非挂载的 `/go`，导致离线无法取得依赖；修正了测试工具镜像的 GOPATH/GOCACHE 后重新准备缓存，原环境失败日志保留，不算作业务缺陷复现。 随后的真实 PG 运行又发现 Python 3.11.2 不支持备份脚本的 `extractall(filter="data")`；升级隔离解释器并在 Dockerfile 构建时验证 data_filter，未修改备份脚本。最终完整重跑通过，失败版本日志单独保留为 `backend-python311-failure*`。Workflow 本地解析改用 lockfile 已安装的 js-yaml，不将不存在的 yaml 模块当已就绪工具。

这些修正没有放松任何业务鉴权、测试断言、依赖审计或生产网络策略。工具测试的预期失败应与不可预期的环境失败分列。

结束前已核对本批资源的 session label，仅移除了本批创建的临时 PostgreSQL 容器及 internal 网络。工具镜像、临时源码缓存与日志保留供复现；未停止或修改任何已有公司服务。

## 尚未交付

本批不关闭 A01–A07，也未完成路由/资格全矩阵、真实七项失败复现、完整迁移/锁图、协议真值表及 S/M 性能基线。B01/G0 保持未完成，后续从 `R5-P0-020` 等依赖已满足项继续，而不是进入 P1。

本地未推送，未触发 GitHub CI，未运行 shipping-image 浏览器旅程、公网投递、生产迁移或部署。下一轮的实际业务改动必须重新运行对应验证，不能沿用本批业务基线的绿灯。
