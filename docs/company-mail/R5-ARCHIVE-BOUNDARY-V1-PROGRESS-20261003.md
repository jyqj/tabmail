# R5 档案边界专项进度附录 v1 — 2026-10-03

仅记录设计任务；架构决定待用户另发实现任务。设计：[R5-ARCHIVE-BOUNDARY-DESIGN-V1-20261003.md](R5-ARCHIVE-BOUNDARY-DESIGN-V1-20261003.md)。不抢中央R5-TODO，不改实现、schema19、两third_party replace、既有历史证据或旧selected/v2/v3回执。

## 源与环境

正常Git `git fetch origin refs/pull/21/head` 成功，FETCH_HEAD=`41b015c30c66b3ba58a3c1395e8559ebcd27a65f`；`git ls-remote origin refs/pull/21/head` 再确认同SHA。新独立分支 `docs/r5-archive-boundary-v1` 基于该提交。工作目录 `/workspace/tabmail`。仓库无 `.agents` / `.agents/skills`，`/workspace/.agents`为空；查无相关SKILL.md，未借此阻塞。`web/AGENTS.md`属于web修改范围，本次没有web编辑。

使用 `/workspace/tabmail-cloud/tools/go/bin/go`：`go version go1.25.7 linux/amd64`；不是 `/usr/bin/go`（该命令非预期Go CLI，`go version` exit1，随后选择已安装固定版本工具）。未读取/使用 local-secrets.sh、env.sh、生产数据库或邮件/凭据。未安装额外依赖；官方registry可用，没有把缺缓存当权限拒绝。

本次build/list以清空继承变量的 `env -i` 执行：PATH=/workspace/tabmail-cloud/tools/go/bin:/usr/bin:/bin，HOME=/tmp，GOTOOLCHAIN=local，GOENV=off，GOWORK=off，GOFLAGS空，GODEBUG=asynctimerchan=0，GOPROXY=https://proxy.golang.org，GOSUMDB=sum.golang.org，GOMODCACHE=/workspace/tabmail-cloud/gomod；build GOCACHE=/tmp/tabmail-archive-analysis-cache，list GOCACHE=/tmp/tabmail-archive-analysis-list-cache。不传任何服务DSN。selected现有helper也只构造其固定metadata环境。

## 精确执行结果

| 实际命令（cwd如上） | 结果 |
| --- | --- |
| 固定绝对Go路径 `version` | exit0：Go1.25.7 linux/amd64 |
| `go build -mod=readonly ./...`，上述env | **exit1**：10个历史包编译报缺符号；不声称CI实际workflow已运行 |
| `go list -mod=readonly ./...`，上述env | **exit0**：包括10历史包及未跟踪flatted包；metadata成功不是build成功 |
| `python3 -B -m unittest discover -s scripts/tests -p 'test_r5_ci_wiring.py' -v` | exit0，4/4 PASS |
| `python3 -B -m unittest discover -s scripts/tests -p 'test_r5_source_v3_boundary_negatives.py' -v` | exit0，5/5 PASS |
| `python3 -B -m unittest discover -s scripts/tests -p 'test_r5_selected_source_binding.py' -k SelectedNegativeTests -v` | exit0，8/8 negative methods PASS |
| `python3 -B -m unittest discover -s scripts/tests -p 'test_r5_selected_source_binding.py' -v` | **exit1**：8 negative methods PASS；ActualRootBindingTests.setUpClass ERROR，4 actual-root methods未运行；unclassified `web/node_modules/flatted/golang/pkg/flatted/flatted.go` |

构建报错具体覆盖SOURCE before/after messages/mailcontent，V2 before/after messages/mailcontent和V3 before/after mailcontent，全部10历史包；缺`mailboxResolver/newMailboxResolver/sourceProgressReader/Parser.cached/Parser.readSource/MaxBytes/Parts/maxParts`。未重命名、补stub或改变这些片段。

明确未运行：新module/guard/inventory-v4/selected-v2（尚未实现）；方案A/B实施实验；生产包缩范围build；go vet、全部Go tests、race/PG/HTTP/protocol/benchmark、前端CI、完整unittest discovery、真实GitHub CI、任何部署/合并。没有用小范围通过替代全根失败。schema19保持原字节，未声称它的运行验收通过。

## 交付边界与远端状态

本次仅新增设计文档和此专项附录。提交前验证设计的36历史hash均等于指定Git blob及工作树，全部既有tracked文件不变；`git diff --check`。推荐精确三个档案module + 新版本policy + filesystem生产coverage guard，等架构选择后再实施。

`gh pr view 21 --json headRefName,baseRefName,headRefOid,url` 返回 `Post https://api.github.com/graphql: Forbidden`。查询失败作为权限失败保留，不改用其他API、token或插件绕过。commit/push以及draft创建的最终结果在交付报告中记录，文档不伪造远端SHA或PR。
