# 2026-10-07 R5 PR 管理与并行推进

## 当前结论

活跃工作集中到 [PR #56](https://github.com/jyqj/tabmail/pull/56)，分支为 `integration/company-mail-r5-pr-management-20261007`，目标为 `main`。该 PR 保持 draft，尚未获得完整 CI／发布资格。原 39 个草稿中，35 个已经核对覆盖关系后关闭，4 个保留独立历史内容。本轮 SMTP、Compose 和兼容依赖修复已分别经独立复核，以普通 merge 合入工作分支。

父任务仍为 **10/171，P0 为 10/12**。本轮完成的是具体缺陷修复、PR 归属整理与验证底座维护，不能代替有效 S/M 基准、G0 或后续整个产品工作包。`main` 未合入 R5，未执行发布或部署。

## PR 队列处理依据

审计对象为原 #17–#55 共 39 个 open draft，逐一核对 head/base、提交包含关系、changed-file 的 mode/type/blob、已有评审及实际 head 的 CI。仅凭标题相似或 GitHub 显示 mergeable 不足以关闭或准入。完整逐 PR 记录在 [pr-triage.json](evidence/R5-PR-MANAGEMENT-20261007/pr-triage.json)；其中 state/CI 是管理前的观察快照。

原整合来源为 #55 的 `cbc8c17ebd3599d5c92711d28088b0bf4edc3f8f`，tree 为 `25f28cde1bd6ad4bd3238d5c7b75a77431b385d0`。它相对 `main` 的 `d6d512172fb6b874c3283d9df3f4d56758684a13` 领先 252 commits、落后 0 commits。创建 #56 时原样采用这个 tree，保留完整祖先链。

| 处理 | PR | 依据 |
|---|---|---|
| 关闭为已被 #56 收录 | #18、#19、#21–#23、#25–#39、#41–#55，共 35 个 | 其中 33 个 head 已是原整合来源的祖先或相同提交；另外两个有完整内容证明，见下文。关闭前重新核对 head，说明中保留原文并指向 #56 |
| 保留 draft | [#17](https://github.com/jyqj/tabmail/pull/17) | 12 个独立 benchmark 历史证据文件尚未收录；产品代码已演进，整个旧 PR 不能认定等价，机械合并存在冲突 |
| 保留 draft | [#20](https://github.com/jyqj/tabmail/pull/20) | 12 个独立 selected-attestation 首次采集漂移审查／失败证据文件尚未收录，不能冒充当前源资格 |
| 保留 draft | [#24](https://github.com/jyqj/tabmail/pull/24) | 51 个独立 Goose 实验文件尚未收录；两组 whole-PG 比较均超时且 cwd 不同，没有有效的性能改进结论，也未改变正式 Go 依赖图 |
| 保留 draft | [#40](https://github.com/jyqj/tabmail/pull/40) | 9 个独立 closure／检查器／controller 方案文件尚未收录；证据限定于旧源，controller 方案还需修正实际 PostgreSQL fixture/DSN 前提 |

#18 的全部 13 个变更路径，在原整合 tree 中 mode/type/blob 完全相同。#26 的实现提交已是祖先，其余两份文档的完整 blob 相同。除此之外，没有用“当前似乎实现了相同功能”替代包含关系证明。原 PR 分支与历史没有由本轮清理删除；新子 PR 的合并按仓库自身行为完成。

原 39 个 head 中，36 个没有当前 head 的 PR-triggered workflow run。多层 stack 的 base 经常不匹配现有 CI 分支过滤。新的 `integration/company-mail-r5-*` 分支已经匹配原有规则，**无需修改 workflow、减少测试或放宽门禁** 即可触发完整检查。

## 本轮实际合入的修复

所有子 PR 都以当时的真实远端整合 head 为 base，并在 merge 时校验 expected head SHA；合并目标均为 #56 的工作分支。

| PR | 问题与结果 | 实际验证 |
|---|---|---|
| [#57](https://github.com/jyqj/tabmail/pull/57) SMTP 最终回复分类 | DATA 最终回复只有明确 4xx/5xx 才当作确定拒收。非 250 的其他数字回复、协议／传输不确定错误保守保留 `ErrOutboundUncertain`，阻止继续尝试其他 MX 或自动重发，并保留原错误链 | 冻结新增 38 leaf 用例在旧实现 18 pass/20 fail；修复后通过。完整 outbound race 为 71 top-level/520 test events pass，0 fail/skip，包含 9 个现有真实 loopback TCP listener/dial 测试；vet 通过 |
| [#58](https://github.com/jyqj/tabmail/pull/58) 写信身份与异步归属 | 发件身份失效后保留明确的无效选项，避免 UI 显示另一身份却提交旧 ID；撤权／移除／卸载／会话变化后的附件与模板预览结果不能回写。预览的 subject/text_body/html_body 在实际回调内校验字符串类型，畸形结果不破坏编辑器 | 冻结 ownership 21 项由旧实现 7 pass/14 fail 变为全 pass；畸形预览 19 项由 5 pass/14 fail 变为全 pass。最终 5 个 Compose 文件 84/84 pass，完整非增量 tsc 和定向 lint 通过 |
| [#59](https://github.com/jyqj/tabmail/pull/59) 兼容依赖与精确锁准入 | 更新现有 30 个 lock entries，0 新增／删除；维持 package.json、框架与 TypeScript 5.9.3。源准备器只额外允许已复核的新完整 lock SHA256，并记录实际被准入摘要 | 审计从 12 项（11 high/1 critical）降至 8 high/0 critical；新锁／拒绝未知锁测试 3/3 pass，原 preparation boundary/source runner 10/10 pass，npm ci/tsc/build 通过。独立审阅的 44 个变化父依赖边均兼容 |

详细边界分别见 [SMTP 报告](R5-SMTP-FINAL-REPLY-CLASSIFICATION-20261007.md)、[Compose 报告](COMPOSE-SENDER-OWNERSHIP-20261007.md)、[依赖报告](R5-DEPENDENCY-AUDIT-20261007.md)。对应独立审阅材料保存在各 evidence 目录。上述计数包含 top-level 与 subtest 的不同口径，不能相加作为产品覆盖总数。

三项合并后的组合 source 为 `97d6b71bb092c4a1fb7af9cc605f32559c4bec71`，tree 为 `53e28562bbbdc80dbba764e72555d783a0bf7f66`。依赖工作树本地提交 `e90c9ce0683dc9b9273962499cbed6713e60c5a2` 与其 tree 完全相同，因此完整前端组合检查绑定这份 tree；提交元数据不同不被混称为同一个 commit。

随后真实 API collector 发现 `senderOperation` 的局部参数 `request` 及其零参数调用被当成 API transport，制造了第 135 条空路径 GET。仅将参数及唯一调用重命名为 `performRequest`，不改扫描器；collector 实测 135→134，唯一删除该伪行、0 新增。两组 ownership／preview 共 40 项回归在此修正上再次通过，Compose 作者独立确认两处 rename 无行为差异。远端提交为 [`7b7dbfeaad5c87e875bf19e5a9e213867fda2db3`](https://github.com/jyqj/tabmail/commit/7b7dbfeaad5c87e875bf19e5a9e213867fda2db3)，tree 为 `c77ac064af69390ef493f6a7f1599b3ed7173738`；当前来源目录以这个精确 source 为准。见 [修正证据](evidence/R5-PR-MANAGEMENT-20261007/compose-collector-followup.json)。

## 完整前端组合验证

在上述 `53e28562…` tree 上原样运行一次默认全套 Vitest（最多 2 workers），没有新增排除或改变测试预算。结果为 **60 个文件中 59 pass/1 fail；866 个测试中 865 pass/1 fail/0 skip**。非增量 TypeScript、完整 production build、全 lint 均 exit 0；build 完成 29/29 静态页面，lint 为 0 errors、4 个原有 warnings。

唯一失败为 `web/r5-external-batch-probe.test.tsx` 的 `R5 batch independent realm loopback PostgreSQL`，错误 `explicit private fixture required`。未提供 `TABMAIL_R5_PROTOCOL_COMPONENT_FIXTURE`，该测试在 fixture 前置检查即失败，未进入网络／产品行为。该测试和默认 Vitest config 相对 #55 未变，不能归为本轮产品回归，也不能据此把默认套件计为全绿。原本排除的 shared-components 协议 fixture 套件保持原配置，未补跑 PostgreSQL。

这份组合验证包含新的 ownership 21 项、preview 19 项，以及已有真实 Go-backed protocol 的 3 项，均通过。两处 rename 后另跑的 40/40 仅代表相应范围；完整 suite/build 没有冒称在 `c77ac064…` tree 上重跑。完整 argv、环境、日志哈希及每项实际 exit code 见 [组合验证 summary](evidence/R5-PR-MANAGEMENT-20261007/combined-web-summary.json)，同目录保留实际日志及 Vitest result JSON。

## 当前目录与历史证据

当前来源目录已针对 `7b7dbfe…` 的组合源码更新到 revision3。审查确定，漂移包含 API 调用位置、事务 caller 位置和 source closure hashes；附件调用的 owner 从旧局部变量 `a` 变为 `Compose`，对应本轮已经复核的 completion wrapper；还包含此前 #52 已实现的 refresh 请求体可省略／cookie 刷新契约，不能将全部差异归为纯行号移动。

旧 revision2 位于固定提交 `4540fb91ba742443dcf0a872b80261e58af7b3ab`。在独立 scratch clone 上，原始目录测试文件的 6 个方法已全部通过、0 skip/error，tracked diff 为空；这是旧版本语义的实测记录，不是新版本资格。当前更新用固定 commit/path/blob/SHA256 读取旧版原 bytes，保留 revision1→revision2 的原约束，当前版本继续使用真实 collectors 与原始严格 validators。缺 Git 对象直接失败，不新增 fetch/skip/fallback 或全局 frozen 测试选择。

三个原始 CLI 在当前源码的旧目录上分别实际失败，更新后全部通过：事务目录覆盖 62 个 PostgreSQL 文件／395 个函数／19 个迁移；兼容目录为 132 routes／134 client branches／94 source files，API 调用扫描保留 7 个显式 transport forwarders。相关三模块 54 个 Python 测试及原 API scanner 的 27 个 Node 测试全部通过（0 failure/error/skip），包括真实 AST、未审调用位置／route／closure／SQL 变化拒绝，以及旧 wire 不得变成当前运行证据的反例。`task_complete`、`runtime_verified`、`product_green` 仍为 false。详 [revision3 报告](evidence/R5-CATALOG-RECONCILIATION-20261007/README.md) 与 [独立审查](evidence/R5-CATALOG-RECONCILIATION-20261007/independent-review.md)，同目录保留实际基线／候选／历史检查日志。

## 真实 CI 结果与剩余阻塞

首次整合 CI [37619817397](https://github.com/jyqj/tabmail/actions/runs/37619817397) 绑定原 head `cbc8c17…`；backend 的实际 PR merge source 为 `49f6a43483c9f8f5aca4a615071eb3818975d239`。它不是后续三项修复或最终目录版本的完整验收。

| Job | 结果 | 可以得出的结论 |
|---|---|---|
| production-web | PASS | 当时的 shipping standalone 构建和 Compose 配置检查通过 |
| browser-journey | PASS | 当时已有的真实 API/loopback SMTP 浏览器旅程通过，不能覆盖所有新修复场景 |
| backend | FAIL | PostgreSQL package 在 180.053s 超时；记录 7898 pass、105 skip、0 单条 assertion fail 的 test events，247 个必跑中 180 个未通过或未执行。package timeout 仍是失败 |
| frontend | FAIL | current source runner 702 tests 中 2 failures/4 errors，主要是当前 API client／事务目录已过期；后面的审计、完整前端等步骤未因此自动取得通过资格 |

归档实物及其摘要见 [CI summary](evidence/R5-PR-MANAGEMENT-20261007/ci-37619817397-summary.json)。`Pull request source evidence` 成功只说明源码归档完成；完整安全／发布 workflow 的结果必须单独读取。

backend 的独立复核进一步明确：181 个已结束顶层测试的 Elapsed 单独合计 179.58s（173 PASS/8 SKIP）；闹钟触发时唯一 running 的 ingress audit 测试刚运行约 0.4s，仍在 fresh fixture 的迁移初始化。另 19 项为 `t.Parallel` 的 PAUSE、0 CONT，尚未获取本地并发许可；不是 19 个数据库会话互锁。证据支持串行累计耗尽整包预算，具体 CREATE/migrate/seed/body/cleanup 占比仍缺阶段计时。root 后续只读获取实际 tested merge，完整 tree 与 #55 相同，并核对六个源码摘要；见 [timeout 审查](evidence/R5-PR-MANAGEMENT-20261007/ci-backend-timeout-review.md)。

本地工具链为校验过官方摘要的 Go 1.25.7、Node 24.19.0、Python 3.12.14；CI 使用 Node 22。本地没有完成真实 PostgreSQL 的全套验证，远端 CI 才提供本轮实际 PG 结果。原 source runner 的一个 Linux pinned executable FD 单测在本工作区失败，精确旧源码同样失败；未改检查或把它计为 pass。

后续仍需完成：

1. 依据真实 timeout 堆栈与测试时间线，修复／优化 PostgreSQL suite 的执行预算消耗及 fixture 生命周期，保留原 180s 验收要求；不能以扩大 timeout 或删除必跑项作为已修复结论。
2. 完成 LF01／successor／pair 所需显式 opt-in 与私有 fixture 的一致接线，并提供默认前端 probe 所需的显式私有 PG fixture。pair 还要求 `127.0.0.1:55447`，当前 CI 服务为 5432；仅补几个环境开关不足以建立合法执行条件。
3. 对组合源码完成严格来源目录与 versioned runner 检查，并在最终远端 source 上读取完整 CI 的实际终态。
4. 继续解决剩余 8 项 high 依赖审计项；当前复核时 braces 的 registry 最新版本仍未提供兼容修复，零漏洞门禁仍失败。未强制降级框架、未添加审计豁免。
5. 继续 P0-100 的有效 S/M benchmark、G0，以及完整 realPG/Redis caller、真实角色终止、持久恢复、权限链等父任务验收；历史 usage19、phase/watchdog、received 等未验边界保持原记录。

## 协作与可复核性

使用本工作区的三个原生 subagent，分别推进 SMTP、Compose、PR／来源目录；root 负责兼容依赖、共享 TODO、串行集成与 PR 状态。实现与复核交叉安排，按独立 worktree 划分写集。管理状态已同步到 README 与 TODO 的执行控制台，历史执行日志和父任务复选框保持原意。

发布 Git 对象时核对远端 tree 与本地已审 tree 的完全相等，创建子 PR 后使用 expected head SHA 合并；更新已有分支需使用 expected_sha，保留完整父提交链。以上只描述代码和证据的实际传递，不将上传或 merge 本身视作测试通过。
