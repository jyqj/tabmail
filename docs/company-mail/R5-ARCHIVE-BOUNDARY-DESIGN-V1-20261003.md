# R5 档案编译边界设计与验收计划 v1（待架构决定）

日期：2026-10-03。分析锚点：PR #21，head `41b015c30c66b3ba58a3c1395e8559ebcd27a65f`；正常 Git 获取 `refs/pull/21/head` 并校验，独立分支 `docs/r5-archive-boundary-v1`。本文为提案，不是实现、构建修复或资格升级。本次归属仅本文和专项进度附录，不编辑中央 `R5-TODO`。

## 结论与待决定事项

推荐方案 A：仅在三个精确历史片段目录新增无依赖的档案 module，并用新的版本化边界清单、全仓 topology 检查、档案哈希和生产覆盖检查约束它们。保留根 `go build ./...` / `go test ... ./...` / `go vet ./...`；两 third_party replace 不变。该方案将历史片段的身份从“根 module 编译候选”显式改为“不可执行历史档案”，原文件字节、路径、压缩包、patch、回执都保持。目录边界是 Go 可见的实际变更，必须公开记录并重新验收。

请决定采用 A，还是采用 B（显式生产包闭包）；实现另发任务。若选 A，本文指定的三个 module 路径、清单范围、新 inventory v4 和新 selected v2 一并进入实现，不允许先加 `go.mod` 再临时放宽 guard。若选 B，必须明确批准 CI 命令契约变更，不能把 `./...` 缩成几个目录后继续称作 whole `./...`。

## 已核实的问题与既有资格

`.github/workflows/company-p0.yml` 的 Build 为 `go build ./...`；Race tests 为 `go test -json -race -count=1 -timeout=180s ./...`，Vet 为 `go vet ./...`。Go1.25.7 本地构建（额外显式 `-mod=readonly`）exit 1，10 个历史包均报缺符号，包括 `mailboxResolver`、`newMailboxResolver`、`sourceProgressReader`、`p.readSource`、`p.cached`、`MaxBytes`、`Parts`、`maxParts`。不能补 stub、复制现行生产代码、改 package、加 ignore build tag、重命名为 `.txt` 或移动目录来使片段“通过”。它们是非完整的 before/after 历史证据；测试文件也不得拼接成新的历史包。

现有 `scripts/r5_selected_source_binding.py` 的 `STATIC_EVIDENCE` 精确授权 21 个 `.go`；`ARGV` 固定实际根 `./...` 加两 fork pattern，selected v1 的记录证明了 Go 选择与本地哈希，不证明编译。成功的 `go list` 不执行 Go 类型检查。这正解释了此前 selected 记录与本次 build 失败可以同时成立。

历史 `r5_current_local_inputs_v2`、`r5_current_local_inputs_smtp_owner_forks_v3` 保持 directory superset 的原语义，selected `r5_root_selected_local_attestation_v1` 保持其原实际范围。旧证据一律在其冻结 Git/head/helper 字节下解释；不要重生成覆盖旧回执、删除旧字段或赋予新边界含义。新 helper 会改变 self-bound 输入身份，旧身份不能靠相同 policy 名假装延续。

当前云 checkout 另有未跟踪、被 Git ignore 的 `web/node_modules/flatted/golang/pkg/flatted/flatted.go`：根 `go list` 实际选中；完整 selected suite 在 actual-root setup 以 `unclassified local Go input` 拒绝它。未删除、挪走或放行。这个环境失败与历史档案编译失败分别记录。未来 clean-checkout 验收要另建正常 Git checkout，明确记录环境；dirty-checkout 负例继续应失败。

Go 的 module root 由 `go.mod` 确定，父 module 不包含带另一个 `go.mod` 的子目录；这是 A 的编译边界机制。参见 [Go Modules Reference](https://go.dev/ref/mod#modules-packages-and-versions) 和 [go package patterns](https://pkg.go.dev/cmd/go#hdr-Package_lists_and_patterns)。实际行为还必须用本仓 pinned Go1.25.7 负例验证，不以最新文档替代运行证据。

## 两方案的明确比较

| 项目 | A：精确档案 module（推荐） | B：显式生产包闭包 |
| --- | --- | --- |
| Go 边界 | 三个精确目录各自成为 module，before/after 共用该档案边界 | 历史目录仍在根 module；命令只用版本化生产 roots/包清单 |
| 根 `./...` | CI 和新 selected 仍保持；实际不进入已声明档案 module | 直接 `go build ./...` 仍失败；必须显式改 CI/selected 命令契约 |
| 新生产包覆盖 | 根递归包含，另用 filesystem 独立覆盖证明防 nested module 隐藏 | 无依赖的新增包也必须被发现并纳入；只算 deps 不够 |
| 漏测风险 | 宽泛放行 nested modules 或把生产文件放入档案 | roots 漏目录、手写清单过期、只测被 import 的生产包 |
| 原历史证据 | 原文件/路径/hash 保留，新增 marker 与新档案身份分离 | 原文件/路径/hash 保留，明确记录仍为根编译候选但不在所执行范围 |
| 成本 | 三 marker + 新 registry + 版本化 inventory/selected + 覆盖 guard | roots/包图 + topology/覆盖 guard + CI/证据命令消费者调整 |
| 两 fork | 保留精确 replace 和 explicit fork selection；root 并不会递归测试 nested forks | 同样需显式两 fork pattern；不能只闭包根 binary |

B 不能仅用 `./cmd/... ./internal/...` 或某个 binary 的 `go list -deps` 作为全部生产资格：后者会漏掉无调用者包、外部测试、某些 tag/平台变体。B 的可接受定义是：版本化生产 source roots、filesystem 枚举所有 tracked `.go` 所在目录（全部 tags/平台变体）、独立分类所有仓内 Go 文件；用真实 `go list -deps -test` 获得每个命令 context 的选中图，并比较没有生产目录被 roots 漏掉。现在 `cmd` / `internal` 是候选 roots，今后新增根级或其他目录的 Go 输入必须失败并要求显式分类，而不是被过滤掉。

B 的新 argv 应明载生产 roots 和两 fork，build/test/vet 分别记录实际 argv、exit 和包集合。保留根 `./...` 失败的诊断基线，不声称其已修复。不能读 metadata 中任意路径后扩大静态读取 authority；继续由受控 filesystem 清单先授权，再分类真实 Go 输出。

## A 的精确 marker 与档案边界

仅允许新增以下三个文件，禁止使用 `docs/go.mod`、`docs/company-mail/go.mod`、整个 `evidence/go.mod` 或 glob allowlist：

| 新文件 | `module` 值 |
| --- | --- |
| `docs/company-mail/evidence/R5-MIME-PREPARSE-SOURCE/go.mod` | `tabmail/archive/r5-mime-preparse-source` |
| `docs/company-mail/evidence/R5-MIME-PREPARSE-SOURCE-V2/go.mod` | `tabmail/archive/r5-mime-preparse-source-v2` |
| `docs/company-mail/evidence/R5-MIME-PREPARSE-SOURCE-V3/go.mod` | `tabmail/archive/r5-mime-preparse-source-v3` |

各 marker 的完整字节为 `module <上表值>\n\ngo 1.25.7\n`（LF、无 BOM）。新 registry 存储 marker 原字节 SHA256 和 module 值，拒绝任何额外指令：无 require/replace/toolchain/exclude/retract/use/未知字段，无 `go.sum`、vendor 或子 `go.mod`。不把它们加入根 require/replace，不加入 go.work；`GOWORK=off`、GOENV=off、GOFLAGS 空、GOTOOLCHAIN=local、GODEBUG=asynctimerchan=0 固定。marker 的“v2/v3”仅引用既有目录名，档案边界 policy 自身是 v1。

新增 `scripts/contracts/r5-archive-boundary-v1.json`：严格 schema，唯一 JSON keys；字段为 `schema_version=1`、`policy=r5_immutable_archive_boundary_v1`、`baseline_commit`、精确 modules 表、`historical_files`（path/size/SHA256）、`protected_go_files`（精确21路径）、`production_roots=[cmd,internal]`、`allowed_new_control_files`。清单权威从已核对 baseline Git bytes 及固定路径生成，不能从实际 selected 输出倒推授权。清单外的新 Go/native/embed/build 控制文件或新 module 一律拒绝；生产 package/import、go:embed、go:generate 等不得指向档案，出现则要求架构变更，不能称作静态历史输入。

清单保护三目录的 **30 个既有文件**（21 Go + before.json/capsule.json/change.patch）和目录外 **6 个 capsule JSON/tar.gz**，共36。新的 marker 是控制输入，不是假造历史输入；不能写回旧 capsule 使它们引用 marker。其余历史 evidence 用 baseline Git diff/tree 验证没有任何删除、移动或字节修改。档案内新文件不自动加入历史白名单，需要另一个明确版本。

## 现有 nested-go.mod 保护怎样调整

1. `r5_source_inventory._check_go_inputs()` 目前只允许两个 fork 的 nested `go.mod`；v3 的 `_current_source_paths()` 又只走 cmd/internal/web/scripts、fork 和固定 docs evidence，不包含这三个档案目录。不能把 marker 塞进 v3 的 names 后扩大老函数 allowlist。新增显式 v4 分支，专门校验 archive contract，纳入 marker/contract/helper/test bytes，并将档案哈希置于独立 `archive_static` 域。
2. v4 建议名称 `r5_current_local_inputs_archive_boundary_v4` / schema 4；保留 v2/v3 dispatcher 与拒绝规则。新分支的 main/fork metadata 校验继续要求恰好两 replace 及版本/path/完整 fork tree。v4 必须独立检查全仓 module topology，而不是只扫描 production roots；恰好 root + 两 fork + 三 marker 六个 module，其他 `go.mod` 失败。
3. 新 selected 建议独立 `scripts/r5_selected_source_binding_v2.py`，policy `r5_root_selected_local_archive_attestation_v2` / schema 2。根/fork argv 保留；授权来自 v4 + 明确 archive contract。独立字段 `archive_static` 与 `selected_local` 不混淆：21 文件仍经 SHA256 绑定，但不再要求它们被 Go 选中；任何 archive package 进入 selected 输出则失败，不能把它从输出后处理删掉。
4. 当前 v1 的 `topology()` 全仓扫描仅准 root+两 fork；不得广泛改成“docs 下所有 module 都准”。新 v2 的 topology 精确检查六个 markers 的路径、bytes/module 值，在档案目录中仍枚举名字/类型并与完整白名单匹配；marker 以下的任何新增 nested module 拒绝。保持 v1 的旧拒绝逻辑及 `STATIC_EVIDENCE` 含义，历史测试在 baseline checkout 运行。
5. `_check_excluded_metadata()` 的 metadata-only、O_PATH/no-follow、identity match、深度/entry budgets 和 excluded-output nested module 拒绝保持。不要在 `EXCLUDED_DIRS` 放 `docs` 或 `evidence`，不要因“档案”放过 symlink/FIFO/device、未知 nested module 或前后漂移；读取 byte 前先确认受控 regular file。现有 excluded metadata/descriptor suites 是防回归门。
6. 新身份分别记录 compile roots、所有生产变体目录、每一 context 实际 selected 包/path/fields/hash、完整 root MVS、两 fork、档案 bytes/markers/contract hash。capture/validate 前后比较 topology、marker、registry、archive/production bytes 和真实 Go metadata。旧 receipts 不自动升级；fresh capture 与 fresh validation 使用明确新 policy。
7. 清单版本与 schema19 是不同系统：不修改 Method/schema19、benchmark workload/cases/receipt 或旧资格。静态 identity 的 PASS 不能变成 external cache/toolchain/native/generated bytes 已验证，也不能赋予 Method19、SML、wholePG、shipping、runtime 资格。

## 生产包闭包验收：不能靠 module 自己证明没漏包

用 filesystem/static 扫描建立全部生产 `.go`、外部 `_test.go`、tag/平台变体和 embeds 的独立 superset，任何未分类仓内 Go 输入失败；归为 archive 的唯一例外为固定21。扫描要保留未知 module 的可见性，不在遇到 `go.mod` 时停止。每一生产目录必须有受控 module 归属；目录内 marker 即使 Go 不再遍历也必须失败。

在精确 command context 下获取 root 全 `./...` 和 explicit production roots 的真实包集合，按 package module identity、Dir、ImportPath、test variant、Go fields 比较，证明两者的 main-module **生产**集合一致，且差集只允许明确验证的静态档案（A 应已无此差集）。全仓 filesystem 已分类且与 Go 输出逐项一致；不允许通过过滤未知 package 达到集合相等。两 fork 集合单独观测并与精确 local module 声明匹配。

平台/tag superset 不等于全平台测试：至少验收 CI default 与 linux/amd64 cgo=1 race+r5protocol 的两 contexts；已有 benchmark/tag 命令保持其参数并分别捕获。其它受支持 tag/OS/ARCH 列出“仅静态覆盖/未执行”，不得由 linux metadata 证明跨平台完整编译。新增可通过的 standalone 生产 fixture 应进入包图；新增带 `undefinedBoundarySentinel` 的 fixture 应让 build exit nonzero。fixture 仅放临时正常 Git checkout，不污染生产仓或既有档案。

## 下一任务的确切修改面（本次均未实现）

| 文件 | 推荐 A 实现内容 |
| --- | --- |
| 上表三个档案 `go.mod` | 新增精确 marker；原36历史文件不变 |
| `scripts/contracts/r5-archive-boundary-v1.json` | 新增严格 versioned registry 与 baseline hashes |
| `scripts/r5_archive_boundary.py` | 新增无服务的 byte/topology/production coverage guard；自绑定 |
| `scripts/r5_source_inventory.py` | 加显式 v4 dispatch，保留 v2/v3原规则与身份语义；新 helper 自绑定 |
| `scripts/r5_selected_source_binding_v2.py` | 新增 selected v2，依赖 v4，actual Go commands与两域分离 |
| `scripts/tests/test_r5_archive_boundary.py` | 新增下面的独立负例与字节保护测试 |
| `scripts/tests/test_r5_selected_source_binding_v2.py` | fresh capture/validate、真实命令、包闭包和漂移测试 |
| `.github/workflows/company-p0.yml` | build 前新 boundary/coverage guard；保留根 build/test/vet；将固定selected测试版本的运行上下文明载，不静默skip历史suite |
| `scripts/tests/test_r5_ci_wiring.py` | 验证 guard在build前、原argv/权限/timer/toolchain仍在 |
| `scripts/run_r5_source_version_tests.py`、`scripts/tests/test_r5_source_version_runner.py` | 新增精确版本测试调度及无suite遗漏的负例，替换CI的裸discovery步骤 |
| `docs/company-mail/R5-ARCHIVE-BOUNDARY-V1-PROGRESS-20261003.md` | 专项追加新的执行证据/精确失败/未运行；中央TODO不改 |

如果通用 unittest discovery 会运行旧 selected actual-root suite：旧 v1 在有新 markers 的 checkout 应按设计拒绝，不能偷偷改其期望成新语义。建议保持旧测试在冻结 baseline独立checkout的兼容性门，同时将通用 discovery拆成明确列出的“当前版本 suites + 冻结历史suite”两组，输出两组命令/计数与exit，新增测试负责证明没有suite遗漏。确切调度建议：新 `run_r5_source_version_tests.py` 从原 `test_*.py` discovery获得测试ID全集，仅把旧 `test_r5_selected_source_binding.ActualRootBindingTests` 四个方法送往固定base的独立正常Git checkout；其余所有方法（包括旧selected八个negative）在实现head运行。冻结checkout只执行这四个actual-root方法，使用原helper/旧21选择断言，不安装node_modules。父进程校验两个集合无交集、并集等于discovery全集，另纳入新增v2/guard/runner测试；报告每组实际ID、源SHA、exit、失败/skip，任一缺失或skip都非绿。新增负例：人为漏一个ID、重跑重复ID、调错baseline SHA、子进程失败、没有fresh v2实际根类，runner必须失败。新runner的精确argv和suite清单进入v4 self-bound源清单；不允许仅 `skip` actual-root。这项runner迁移涉及 `company-p0.yml` 和CI wiring测试，属于A的公开成本。

`check_r5_protocol.py` / `run_r5_benchmark.py` 通过 `SUPPORTED_POLICIES` 选择清单；若在实现任务中执行这些 runners，显式传新 v4 与 fresh manifest/hash，不修改旧 pin、默认身份或 METHOD/数据集。核查所有 `capture_current_source` / `validate_current_source` / `load_current_source` 和 `source_closure` 消费者。仅新增边界并不要求在本次设计任务升级它们。新 guard 若需接入 `check_go_test_evidence.py`，仅添加版本化 package覆盖输入；已有执行真实性检查不放宽。

## 可证伪负例与通过条件

| 负例（独立临时 fixtures） | 必须观测的失败/变化 |
| --- | --- |
| 删除一个 marker、增 `docs/go.mod`、在 cmd/internal 或 fork/build 中加 module、档案内再嵌套 module | boundary guard失败；删除 marker 后 root包集合恢复档案候选且build缺符号 |
| marker路径拼写/case/值改变、加require/replace/toolchain/go.sum/vendor/go.work、第三root replace | guard/MVS失败；不能容忍Go偶然仍能build |
| 任一旧Go/JSON/patch/tar字节修改、重命名、删除，清单duplicate/path traversal、伪造hash、增档案Go | hashes/schema/topology失败，旧receipt保持原值 |
| 档案路径及其祖先symlink、FIFO/device、excluded目录内nested module、预算溢出、不可读取metadata | no-follow/type/budget guard失败，不能读任意raw/secret/cache正文 |
| 在未列root新增独立生产包，或把既有生产包隐藏在nested module | filesystem覆盖guard失败；不能仅依赖go list看不到它 |
| 在cmd/internal新增不被任何binary import的可编译包，再换成undefined sentinel | 前者真实selected/build涵盖；后者build非零，不许用deps-only遗漏 |
| 仅在_test.go、受支持tag、OS/ARCH变体或embed输入中注入遗漏 | 全变体静态域变化/失败；所选context中实际Go输入变化，不支持context明确拒绝 |
| 生产import/embed/generate指向档案，伪造Go metadata授权档案/私密路径 | 静态/selected guard失败，不执行generate，不读私密路径 |
| capture中途更换marker/contract/archive/source，复用v1/v3回执验证v2/v4 | drift/version guard失败；不存在自动fallback/升级 |
| 当前node_modules flatted脏环境 | selected仍拒绝未知本地输入；clean Git checkout的PASS不能抹掉此失败 |

A 的完整验收顺序：在独立正常 Git clean checkout固定实现SHA；先验证相对本分析base所有原证据bytes/path不变；跑新guard及负例、现有excluded/descriptor/nested-module保护；分别在冻结base运行历史身份兼容性和在实现head运行新v4/v2 capture+recapture；观测实际根/fork包集合与生产coverage；实际根 `go build -mod=readonly ./...`、`go vet ./...`；随后在仅临时测试PostgreSQL环境运行原 `go test -json -race -count=1 -timeout=180s ./...` 及现有证据真实性gate，不跳过数据库测试并称whole通过。两fork自己的测试/原protocol/benchmark命令按已授权环境另列，未跑则unknown。记录source SHA、每条argv/env/exit、原始日志hash、缺失/失败/未运行和所支持contexts。registry命中缺缓存允许从官方proxy锁定下载，真实权限拒绝停止该动作、不换路。

通过要求：三个且仅三个新档案module；21片段不被根Go命令选中但36原文件均hash不变；新生产fixture必被覆盖且undefined负例build红；根生产/两fork覆盖无遗漏；default build/vet与CI race结果分别有真实证据；新旧身份拒绝交叉验证；无原始历史bytes改写。通过静态和build仅解决档案边界问题，仍不替代外部依赖字节/ABI/数据库/运行资格。

## 冻结历史文件 SHA256（来自指定Git提交的blob）

以下为本设计可复查锚点，不是实现registry；共 36 个。实现时由指定Git blob重新核对，禁止从修改后的工作树生成“基线”。

```text
f670a5c4a85c8aeb8d4e4a07f2fc0d3954172a7e8ceba199903bf1d8463718ea  docs/company-mail/evidence/R5-MIME-PREPARSE-SOURCE-CAPSULE.json
968607700067c24182250e75516cb7024fe35732ed0f9273154bc366f8faf597  docs/company-mail/evidence/R5-MIME-PREPARSE-SOURCE-CAPSULE.tar.gz
53f42d3258b3bca97d6539faeb39ae54ce3d9aa88acbe06087289ac3a26c60a2  docs/company-mail/evidence/R5-MIME-PREPARSE-SOURCE-V2-CAPSULE.json
4728c65a4300c052f7bdf96963ba884530d6be086933c26cf634b3baaace5957  docs/company-mail/evidence/R5-MIME-PREPARSE-SOURCE-V2-CAPSULE.tar.gz
b405791a97dca02eb6fddf24fa122703909dfb8fdaf96d7bffe287cd82e06a1a  docs/company-mail/evidence/R5-MIME-PREPARSE-SOURCE-V2/after/internal/app/messages/mime_budget_boundary_test.go
a852c17b5051989cc91990f78d95f721e441346f11101e66460360c5ac708460  docs/company-mail/evidence/R5-MIME-PREPARSE-SOURCE-V2/after/internal/app/messages/service.go
b2d93567f4556971b9967ad80157cc9d70ace8dd31a6393075c4eba6e2521364  docs/company-mail/evidence/R5-MIME-PREPARSE-SOURCE-V2/after/internal/mailcontent/mime_budget.go
36858895bd2b84b84b7be6316c9e4bbd950a21fc797e137e46ef5553dc6c6758  docs/company-mail/evidence/R5-MIME-PREPARSE-SOURCE-V2/after/internal/mailcontent/mime_budget_test.go
cab8753b99dd2af3192ad15b5b2c3a99adea830d9665bad2087cea1d96fa31f3  docs/company-mail/evidence/R5-MIME-PREPARSE-SOURCE-V2/after/internal/mailcontent/parser.go
ae5bafa5b6099b345536cc052d69bf62402d3a9710e4cbce4513251a16695bb2  docs/company-mail/evidence/R5-MIME-PREPARSE-SOURCE-V2/before.json
b405791a97dca02eb6fddf24fa122703909dfb8fdaf96d7bffe287cd82e06a1a  docs/company-mail/evidence/R5-MIME-PREPARSE-SOURCE-V2/before/internal/app/messages/mime_budget_boundary_test.go
a852c17b5051989cc91990f78d95f721e441346f11101e66460360c5ac708460  docs/company-mail/evidence/R5-MIME-PREPARSE-SOURCE-V2/before/internal/app/messages/service.go
342ffd1ce567a943bb8b9ddde7b0812f170f143b564a9e4f7a919aca9c150391  docs/company-mail/evidence/R5-MIME-PREPARSE-SOURCE-V2/before/internal/mailcontent/mime_budget.go
cd003b9f058e2d81cc2df3fb298a3b8eddf2fb7ff909b166697d705be4ab9390  docs/company-mail/evidence/R5-MIME-PREPARSE-SOURCE-V2/before/internal/mailcontent/mime_budget_test.go
cab8753b99dd2af3192ad15b5b2c3a99adea830d9665bad2087cea1d96fa31f3  docs/company-mail/evidence/R5-MIME-PREPARSE-SOURCE-V2/before/internal/mailcontent/parser.go
2d2cce07be8d138fb2361fb2cffb81103b8f7dc729c9e4d3920b4f5084e59d71  docs/company-mail/evidence/R5-MIME-PREPARSE-SOURCE-V2/capsule.json
a3b44754edd03dd8538630ffc1c7e0f2986499cbf7cc6244975986c42211e248  docs/company-mail/evidence/R5-MIME-PREPARSE-SOURCE-V2/change.patch
baff1332e6de4c8266d999bc1b0e7a9358704235743ddf50667b6c60e7491055  docs/company-mail/evidence/R5-MIME-PREPARSE-SOURCE-V3-CAPSULE.json
299ed045f0b18f406dd3f2865f474f7dc245501db1b2765f4e45ba955c10ce0d  docs/company-mail/evidence/R5-MIME-PREPARSE-SOURCE-V3-CAPSULE.tar.gz
b92072b2f5fb89a9eea2956e40746a22bbfea22a530fca197da3f65a00288003  docs/company-mail/evidence/R5-MIME-PREPARSE-SOURCE-V3/after/internal/mailcontent/mime_budget.go
8aeb043be4a841a4619e2afec8137e3397f7983c5db3021d833523295faf4b22  docs/company-mail/evidence/R5-MIME-PREPARSE-SOURCE-V3/after/internal/mailcontent/mime_budget_test.go
14c4317b252c03a237ad4e1fd880355c9b7d56eb6c4fa144168739abe92b25a5  docs/company-mail/evidence/R5-MIME-PREPARSE-SOURCE-V3/before.json
b2d93567f4556971b9967ad80157cc9d70ace8dd31a6393075c4eba6e2521364  docs/company-mail/evidence/R5-MIME-PREPARSE-SOURCE-V3/before/internal/mailcontent/mime_budget.go
36858895bd2b84b84b7be6316c9e4bbd950a21fc797e137e46ef5553dc6c6758  docs/company-mail/evidence/R5-MIME-PREPARSE-SOURCE-V3/before/internal/mailcontent/mime_budget_test.go
4165d588422f9c39aa01974f15c3696c35865fbb9350f3a21d37e7bf3a0dc9b3  docs/company-mail/evidence/R5-MIME-PREPARSE-SOURCE-V3/capsule.json
632d1fea6f44608a15f8426a2c33e0bbdd65268978d131fbec854bed783d6d51  docs/company-mail/evidence/R5-MIME-PREPARSE-SOURCE-V3/change.patch
b405791a97dca02eb6fddf24fa122703909dfb8fdaf96d7bffe287cd82e06a1a  docs/company-mail/evidence/R5-MIME-PREPARSE-SOURCE/after/internal/app/messages/mime_budget_boundary_test.go
a852c17b5051989cc91990f78d95f721e441346f11101e66460360c5ac708460  docs/company-mail/evidence/R5-MIME-PREPARSE-SOURCE/after/internal/app/messages/service.go
342ffd1ce567a943bb8b9ddde7b0812f170f143b564a9e4f7a919aca9c150391  docs/company-mail/evidence/R5-MIME-PREPARSE-SOURCE/after/internal/mailcontent/mime_budget.go
cd003b9f058e2d81cc2df3fb298a3b8eddf2fb7ff909b166697d705be4ab9390  docs/company-mail/evidence/R5-MIME-PREPARSE-SOURCE/after/internal/mailcontent/mime_budget_test.go
cab8753b99dd2af3192ad15b5b2c3a99adea830d9665bad2087cea1d96fa31f3  docs/company-mail/evidence/R5-MIME-PREPARSE-SOURCE/after/internal/mailcontent/parser.go
59fc611c0e315b7a2330b4e4fd97d7f71118347643c30d3dd0e3122ded6f9a4b  docs/company-mail/evidence/R5-MIME-PREPARSE-SOURCE/before.json
3d27af40e991de1f2636672e3bbd3a00f24520f821a308012b11e02802161867  docs/company-mail/evidence/R5-MIME-PREPARSE-SOURCE/before/internal/app/messages/service.go
b4b7b3c4cd594d6762716cb0ce868da52b696f307311dd6de4cefea756769cf1  docs/company-mail/evidence/R5-MIME-PREPARSE-SOURCE/before/internal/mailcontent/parser.go
ed383aa4da0b74a9d3aabdb65a3c2c54f7e22ca1b459e1ea2c2b8fe7e18ef48e  docs/company-mail/evidence/R5-MIME-PREPARSE-SOURCE/capsule.json
da1ed2f696fb6b7e158f5f2297640e17e2c219f4872124dd0d3e22dc905f37e4  docs/company-mail/evidence/R5-MIME-PREPARSE-SOURCE/change.patch
```
