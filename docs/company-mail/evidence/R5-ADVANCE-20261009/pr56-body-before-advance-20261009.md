<!-- R5-FINISH-20261009:BEGIN -->
## FINISH：三轮十项与最终目录维护均已实际合入

**本批 10/10 个独立实施 TODO 完成，剩余 0；三轮剩余 6 → 3 → 0。** 原父任务仍 **10/171 已验收、剩余 161**。原 176 条 checklist 与起始公开 `be3a6bf4` 逐字节相同；目录、文档、CI 和 PR 管理不增加实施计数。

| 轮次 | 实际合入 | 已 closed/completed | 累计完成 | 本批剩余 |
| --- | --- | --- | ---: | ---: |
| 1 | [#193](https://github.com/jyqj/tabmail/pull/193) | #183 / #184 / #185 / #186 | 4/10 | 6 |
| 2 | [#194](https://github.com/jyqj/tabmail/pull/194) | #187 / #188 / #189 | 7/10 | 3 |
| 3 | [#195](https://github.com/jyqj/tabmail/pull/195) | #190 / #191 / #192 | 10/10 | 0 |
| 最终维护 | [#196](https://github.com/jyqj/tabmail/pull/196) | revision14、README、唯一父清单入口、完成账本 | 计 0 新项 | 0 |

root 与三个原生 subagent 分别实施和交叉审查。十项覆盖：邮箱发送策略的明确修改意图、模板状态/授权明确布尔值、注册密码 UTF-8 字节边界、完整 mailbox grant 替换、登录提交去重与 continuation 归属、管理员邀请输入契约、真实邮箱冲突错误边界、改密后的新会话保护、带引号收件地址的回复生成/编辑、S3 流长度/结束/重播边界。审查中发现并修复了正常 Provider 重挂载丢成功反馈的回归，并纠正了最初对重复邮箱错误的诊断。

完整行为、固定失败到通过和实际执行身份见 [FINISH 账本](https://github.com/jyqj/tabmail/blob/fdea2178759ce9842844926374ea98517bc4188b/docs/company-mail/R5-FINISH-20261009.md)；目录事实及历史守卫见 [revision14](https://github.com/jyqj/tabmail/blob/fdea2178759ce9842844926374ea98517bc4188b/docs/company-mail/evidence/R5-CATALOG-REVISION14-20261008/README.md)。

### 实际源码与验证

第三轮产品 merge `740126660526db987b7914c50a8731b7cac9bf42` / tree `5ee1ec138c6854b3068ec3576ea1e824510a9ee2` 已核对与受审产品完整树相同。最终目录维护 merge **`fdea2178759ce9842844926374ea98517bc4188b`** / tree **`e37024707b91a0e79363c73b2dce1c8feebcb2da`** 与公开 head `7d481bf0cca050114a7db390a14128626a399ff5`、本地受审候选以及 GitHub PR 测试 merge `0e150345ededb5b707f65fdcce54fdbecd8caf37` 整树相同；实际 merge 双 parent 为 `74012666` 与 `7d481bf0`，Git API 和真实 fetch 均核验。

- 第三轮六个完整相关 Go 包实际 **1247 race 叶 PASS / 0 FAIL/SKIP**，相关前端 **107 个不同用例 PASS**；默认 Go build 在同源普通 clone 成功，完整 TypeScript 和相关静态检查成功。
- 邮箱创建真实 PostgreSQL [37812188584](https://github.com/jyqj/tabmail/actions/runs/37812188584) 原 job 日志确认：同一固定十例，公开有效基线 **7 PASS / 3 FAIL → 候选 10 PASS / 0 FAIL/SKIP**。正常重复地址原本就是 409；修复的是无关唯一性错误误报 409 及原因丢失。原 artifact ZIP 下载失败的限制单独保留，没有冒称读取内部 receipt。
- 目录原三 CLI 维护前真实 **1 / 1 / 0**，维护后 **0 / 0 / 0**。98 项相关 Python 测试通过，真实执行在未提交的 `74012666` 候选；root 原三 CLI 则绑定干净 `b4c2ad5`，随后只增加三份文档。两位独审分别对目录和最终三份文档 ACCEPT。
- 最终公开源 [CI 37816104999 的 frontend 原日志](https://github.com/jyqj/tabmail/actions/runs/37816104999/job/113445033355)已读取：实际 checkout `0e150345ededb5b707f65fdcce54fdbecd8caf37` / tree `e37024707b91a0e79363c73b2dce1c8feebcb2da` 与最终合入完整树相同。原完整 source-version runner 实际 **812 当前＋4 冻结历史＝816 PASS / 0 failures/errors/skips**，两组原日志分别 OK，步骤 SUCCESS。完整 UI 实际 **1955 PASS / 1 必需私有 fixture FAIL**，唯一为 `r5-external-batch-probe.test.tsx:9` 缺 `TABMAIL_R5_PROTOCOL_COMPONENT_FIXTURE`；完整 tsc/lint/build 成功，严格 audit 仍 **8 high**。frontend job 因审计和 fixture 仍 FAILURE。
- 最终公开 head 的 [source evidence 37816104965](https://github.com/jyqj/tabmail/actions/runs/37816104965)、[PG 登录 37816104997](https://github.com/jyqj/tabmail/actions/runs/37816104997)、[邮箱 PG 37816105029](https://github.com/jyqj/tabmail/actions/runs/37816105029)、[Webhook 37816104993](https://github.com/jyqj/tabmail/actions/runs/37816104993) 全部 SUCCESS。production-web / browser-journey 已 SUCCESS；本次完整 PG race 已 FAILURE，backend 其他步骤观察时仍运行。逐范围结果保持独立，完整发布 CI 没有全绿。
- 已结束的第三轮产品 [完整 CI 37813096036](https://github.com/jyqj/tabmail/actions/runs/37813096036) 保留真实结果：完整前端 **1955 PASS / 1 必需私有 fixture FAIL**、严格 audit **8 high**、完整 tsc/lint/build 成功；维护前 source-version 当前 810 tests 为 4 failures/3 errors。新目录维护的通过不改写旧运行。

### PR 队列与发布边界

#56 保持 **draft**；main 独立 GET 仍为 `d6d512172fb6b874c3283d9df3f4d56758684a13`，未合入本批集成。#17/#20/#24/#40 各保留独立历史 benchmark、拒绝证据、Goose 实验和收口材料，仍 open/draft；#4/#6/#9 仍 open。完整 PG、必需独立私有 fixture、严格审计、当前有效 M baseline/G0 继续开放。目录 `runtime_verified`、`product_green`、`task_complete` 保持 false。

以下此前批次历史正文保持。
<!-- R5-FINISH-20261009:END -->

<!-- R5-DELIVER-20261008:BEGIN -->
## DELIVER：三轮十项完成，最终台账与目录维护已合入

root 与 backend、frontend、storage 三个原生子代理按4＋3＋3推进，固定真实失败、同字节回归、交叉审查、精确树合入后逐项关闭Issue。

| 轮次 | 已实际合入PR | 已closed/completed | 累计完成 | 本批剩余 |
|---|---|---|---:|---:|
| 1 | [#179](https://github.com/jyqj/tabmail/pull/179) | #169/#170/#171/#176 | 4/10 | 6 |
| 2 | [#180](https://github.com/jyqj/tabmail/pull/180) | #172/#173/#177 | 7/10 | 3 |
| 3 | [#181](https://github.com/jyqj/tabmail/pull/181) | #174/#175/#178 | 10/10 | 0 |

**本批10/10完成，剩余0；三轮剩余6→3→0。原父任务仍10/171、剩余161。** 十项分别覆盖独立日配额、真实PG登录签发竞争、模板隐藏变量、合法quoted-local收件地址、批量设置验证、副作用可控的草稿重载、SMTP准入查询期限、预览必需内容、六类邮件列表状态、SMTP累计接收预算及截断确认。

最后实际产品merge `9b73b13f376f7e8d589a06fc7078dae4c342d5d5` / tree `8e3f458f8cec51c031a06fd280151116164244ec`。第三轮root925 Go race叶全PASS、默认全Go build/相关vet通过；UI240独立PASS，其先行执行源和最终相同web字节分别记录。真实PG登录固定9叶由3 PASS/6 FAIL变为9 PASS；不能用局部结果替代全量PG。

### 最终目录维护与实际合入

[PR #182](https://github.com/jyqj/tabmail/pull/182) 已实际合入 **`be3a6bf41daa198a39306c90fd03411e38a017d8`**，完整tree **`350a848972e02ef8f7edc8a1a43db087c844405c`**。Git API双parent核对为9b73与公开head d2caf；root实际fetch后，与最终本地ad799及受审公开head全树相同，工作树clean。

revision13保留116个历史文件、169个保护源、原collector/五个unapproved负控/旧REVISION常量。原三个目录CLI先真实拒绝新源，更新后全部通过；root相关96方法PASS，两位独审分别7和3方法PASS并ACCEPT。测试执行身份2aff与最终仅README一行修正ad799分别保留。目录、CI、PR维护计 **0个新增实施TODO**。

[DELIVER完成账本](https://github.com/jyqj/tabmail/blob/be3a6bf41daa198a39306c90fd03411e38a017d8/docs/company-mail/R5-DELIVER-20261008.md) · [中央TODO](https://github.com/jyqj/tabmail/blob/be3a6bf41daa198a39306c90fd03411e38a017d8/docs/company-mail/R5-TODO.md) · [revision13说明](https://github.com/jyqj/tabmail/blob/be3a6bf41daa198a39306c90fd03411e38a017d8/docs/company-mail/evidence/R5-CATALOG-REVISION13-20261008/README.md)。

### 最终受审源码的原完整 CI

[原完整run37773560989](https://github.com/jyqj/tabmail/actions/runs/37773560989) 已 **completed/FAILURE**；实际checkout `af7010df6cc41ab44e82ab6d02b418f1329f3dc5` 的tree与最终合入完整一致。

- 原完整source-version runner **814 PASS＝810 current＋4 frozen**；日志外层OK、814唯一观察ID、无重叠，两个revision13新方法实际出现。没有声称读取未取得的artifact JSON字段。
- 原目录/TS源码检查 **49 PASS**；API inventory、i18n、非增量tsc、lint、生产build、production-web、真实browser-journey均SUCCESS。
- backend原regressions、DTO、compatibility、transaction四项均SUCCESS；随后protocol baseline、真实PG HTTP契约、vet和证据上传均SUCCESS。
- 完整UI **1871 PASS/1 FAIL**（107 files：106 PASS/1 FAIL），唯一失败仍是必需独立realm私有fixture未提供。
- **严格依赖审计和完整PostgreSQL race仍FAILURE**。backend只有Race步骤失败；未取得完整Go逐用例日志，不给该run伪造叶数或未经核验的失败归因。原旧run失败继续保留。

原PR head的[source evidence](https://github.com/jyqj/tabmail/actions/runs/37773560941)、[PG登录](https://github.com/jyqj/tabmail/actions/runs/37773560985)、[Webhook](https://github.com/jyqj/tabmail/actions/runs/37773560972)均SUCCESS。合入后实际be3a head的[source evidence](https://github.com/jyqj/tabmail/actions/runs/37775003900)、[PG登录](https://github.com/jyqj/tabmail/actions/runs/37775003967)、[Webhook](https://github.com/jyqj/tabmail/actions/runs/37775003966)也均SUCCESS。

最后状态截点 **2026-10-08 12:12:30 UTC**：另触发的[合并后完整run37775003912](https://github.com/jyqj/tabmail/actions/runs/37775003912)仍in progress，上述逐项数字绑定已完成的原PR run，不改写为新run结果。开放PR仅#56/#17/#20/#24/#40，全部draft；历史四个head保留。**本PR继续draft，main仍 `d6d512172fb6b874c3283d9df3f4d56758684a13`。** #4/#6/#9、完整PG、严格审计、必需私有fixture及原M/G0发布门槛继续开放。
<!-- R5-DELIVER-20261008:END -->

<!-- R5-PROGRESS-20261008:BEGIN -->
## PROGRESS 批次：三轮十项已完成，最终维护已合入

root 与三个原生 subagent 从公开 `58d0c9cf` 继续，完成固定失败复现、实现、同字节回归、独立交叉审查、精确树合入及逐项 closed/completed。[最终 PROGRESS 账本](https://github.com/jyqj/tabmail/blob/b7247c3f66bd5f0ec5c6390305e2ff7268c135f8/docs/company-mail/R5-PROGRESS-20261008.md) 已回写。

| 轮次 | 实际合入 PR | 已关闭任务 | 累计完成 | 本批剩余 |
|---|---|---|---:|---:|
| 1 | [#164](https://github.com/jyqj/tabmail/pull/164)，merge `66f14c11` | #154 / #155 / #156 / #157 | 4/10 | 6 |
| 2 | [#165](https://github.com/jyqj/tabmail/pull/165)，merge `083451ae` | #158 / #159 / #160 | 7/10 | 3 |
| 3 | [#167](https://github.com/jyqj/tabmail/pull/167)，merge `ed81ee2f` | #161 / #162 / #163 | 10/10 | 0 |

三轮 root 联合验证分别为 **217 Go race＋30 UI、553 Go race＋64 UI、235 Go race＋最终 97 UI**，以上限定范围均零 FAIL/SKIP；各轮准确执行 SHA、重叠范围及保留失败见账本。最后一轮全 Go 编译、完整 TypeScript/ESLint/DTO/i18n 和默认生产 Next build 通过。设置响应截断的独审阻塞已在同项 #163 修复，原 probe 复验通过；十项均 ACCEPT。

### 来源目录与 PR 收尾

[维护 #166](https://github.com/jyqj/tabmail/issues/166) 已 closed/completed，[PR #168](https://github.com/jyqj/tabmail/pull/168) 已实际合入 **`b7247c3f66bd5f0ec5c6390305e2ff7268c135f8`**，tree **`d9cce4ad1fe27c66e8954346894b69516364da4d`**。root 已 fetch 验证完整树与验收交付相同，双 parent 为 `ed81ee2f` / `31f98a3`。维护计 **0** 个实施 TODO。

revision12 保留 65 个历史文件、167 份保护源码、原 collector 和五个拒绝测试；原 135 个客户端逻辑调用保留，唯一新增域设置显式 GET /settings，总计 136，路由仍 133。原三个 CLI 通过；storage 独审 94/94，frontend 独立公开网络 clone 27/27，均 ACCEPT。报告追加后 27/27 再验通过。

[原完整源码检查 37759167593](https://github.com/jyqj/tabmail/actions/runs/37759167593) **812/812 PASS（808 current＋4 frozen），零 failure/error/skip/missing/overlap**。实际 CI source `83c91dff` 与公开目录核心 `b87df7a` 全树相同，且已作为最终提交的公开祖先保留；原执行 SHA、报告和 artifact hash 未重写。另一次本地完整运行的 4 FAIL / 7 ERROR、7 missing 及各自环境/边界错误原样保留。完整结果见 [revision12 证据](https://github.com/jyqj/tabmail/tree/b7247c3f66bd5f0ec5c6390305e2ff7268c135f8/docs/company-mail/evidence/R5-CATALOG-REVISION12-20261008)。

同一 CI 的生产镜像与浏览器流程通过；完整 UI **1758 PASS / 1 必需私有 fixture FAIL**，严格审计 **8 high / 0 critical**，PostgreSQL race 步骤仍失败。完整发布条件保持，不能把 source-version 通过写成 release 全绿。最终维护 head 的 [source evidence 37760868748](https://github.com/jyqj/tabmail/actions/runs/37760868748) SUCCESS，后续 workflow 状态以各自实际运行记录为准。

**本批 10/10，剩余 0；三轮余量 6 → 3 → 0。原父任务 10/171，剩余 161。** 十个 Issue 已再次逐项核实 closed/completed。开放 PR 仅本 PR 及历史 #17/#20/#24/#40，历史独立证据继续保留。本 PR 保持 draft，main 为 `d6d512172fb6b874c3283d9df3f4d56758684a13`，原 M/G0 与完整发布门槛未变。
<!-- R5-PROGRESS-20261008:END -->

<!-- R5-COMPLETE-20261008:BEGIN -->
## COMPLETE 批次：10/10 已完整关闭，剩余 0（2026-10-08）

root 与三个原生 subagent 在独立 worktree 按 4＋3＋3 多轮实施，逐项冻结缺陷基线、实现、交叉审查、验证精确公开树，再通过独立 PR 合入并关闭 Issue。并发 CONTINUE 及前批工作不重复计数。

| 轮次 | 已合入 PR | 实际 closed/completed | 累计 | 本批剩余 |
| --- | --- | --- | ---: | ---: |
| 1 | [#145](https://github.com/jyqj/tabmail/pull/145) | #128 / #130 / #131 / #132 | 4/10 | 6 |
| 2 | [#147](https://github.com/jyqj/tabmail/pull/147) | #138 / #140 / #141 | 7/10 | 3 |
| 3 | [#148](https://github.com/jyqj/tabmail/pull/148) | #142 / #144 / #146 | 10/10 | 0 |

三轮实际 merge 依次为 `edce9ea6479bcbf499959fea328e58e28be301e0`、`52e791ddf0dfae4d18d5c0994bc829755ad41fc0`、`23eda8a73549e16c8d149da7628b3e7261de8151`；完整树与公开审查源及双 parent 均已核对。十项包括 procfs 持有 FD 执行、SSE 续期恢复、Webhook 串行 claim 与失租写入 fencing、邮件异步视图归属、monitor 四条生产取消路径和指标、S3 对象身份、激活表单完整生命周期、MIME Close/缓存边界、并发 Dispatcher.Run。

真实 PostgreSQL [37739497288](https://github.com/jyqj/tabmail/actions/runs/37739497288) 固定基线 **15 PASS / 63 FAIL**，候选 **78 PASS / 0 FAIL / 0 SKIP**；实际缺陷原因、原日志 hash、执行前后源身份已核验。三轮各自 source evidence 和真实 PG 回归均 SUCCESS。root 第二轮共同源 **273 Go PASS / 0 FAIL / 10 既有 PG SKIP、21 UI PASS**；末轮六个相关 Go 包 **591 PASS / 0 FAIL/SKIP**，最终 UI **31 PASS / 0 FAIL/SKIP**，完整 Go build 和非增量 TypeScript 通过。激活项两次独审追加修复会话替换和排队 hash ABA，仍计同一项 #142。

### 最终目录维护与 PR 收尾

[维护 #149](https://github.com/jyqj/tabmail/issues/149) 已 closed/completed，[PR #153](https://github.com/jyqj/tabmail/pull/153) 已实际合入 **`58d0c9cf274569258319ff4b5dcab7912b0174f4`**，tree **`e4c4550481d9dc9efc5d4d849f85d496b99f65c4`**，与审查的公开 head 完整树一致。实际 parent `a0baabaa`＋`8774404f` 经 root 与独审分别核对。并行 #152 账本及产品变更均保留，维护计 **0 个额外 TODO**。

最终产品 anchor `33f61fde` 的目录为 **63 个 PG 文件 / 401 个函数 / 133 条路由 / 135 个客户端调用**。作者 **92/92 Python**、root 原三个 CLI 通过；两位独审分别 **7/7** 原拒绝与当前正例、**25/25** 完整目录回归通过。后者只依赖公开 Git 历史，执行前后私有实现 commit 对象均不存在。历史 revision 1–10、原拒绝测试、CLI/producer 与受保护来源保留。

公开 PR153 的原 [完整 CI 37749071915](https://github.com/jyqj/tabmail/actions/runs/37749071915) 已实际完成 frontend 步骤 8 完整 source-version 和步骤 9 目录校验，均 **SUCCESS**；[source evidence 37749071891](https://github.com/jyqj/tabmail/actions/runs/37749071891) 与 [真实 PG 37749071929](https://github.com/jyqj/tabmail/actions/runs/37749071929) 另行 SUCCESS。原完整 runner 与 source-evidence 工作流分别记录，最终原报告为 **806 个当前测试＋4 个冻结测试全部 PASS，0 FAIL/ERROR/SKIP/MISSING**，无重复、无重叠；旧 804 个当前测试全部保留，原 11 项目录错误均实际执行一次并通过，仅新增两个 revision11 正例。实际 CI checkout `826bf23a` 的 tree 与公开 head、实际 merge 完全一致；原报告 hash 与逐项对照见 #149。旧 3a 本地完整运行的失败、7 missing 和已知环境/清理边界原样保留，没有改写成最终公开源结果。

本整合 merge `58d0c9cf` 的最新 [source evidence 37749877627](https://github.com/jyqj/tabmail/actions/runs/37749877627) 与 [真实 PG 37749877795](https://github.com/jyqj/tabmail/actions/runs/37749877795) 均 SUCCESS；[完整 CI 37749877798](https://github.com/jyqj/tabmail/actions/runs/37749877798) 在本次更新时仍进行中，不能用稳定 PR153 的报告代替它。

**原父级仍 10/171 已验收，剩余 161。#56 保持 draft；main、原 M/G0 与完整发布门槛不变。** 完整 PG 生命周期预算、必需私有 fixture 与完整依赖审计仍未验收，PR153 本次原 audit 为 **8 high**，完整前端 **1655 PASS / 1 必需私有 fixture FAIL**；TypeScript、lint、build 通过。详细固定回归与三轮证据见 [最终 COMPLETE 账本](https://github.com/jyqj/tabmail/blob/58d0c9cf274569258319ff4b5dcab7912b0174f4/docs/company-mail/R5-COMPLETE-20261008.md)；本批十个 Issue 全部 completed，目录维护不新增完成数。
<!-- R5-COMPLETE-20261008:END -->

<!-- R5-CONTINUE-20261008:BEGIN -->
## CONTINUE：新十项已全部完成并关闭（2026-10-08）

本批通过三轮原生 multi-subagent 实施、同字节实际失败基线、独立交叉审查、联合验证及精确树合入，十个 Issue 现已逐项核实 **closed/completed：10/10 完成，剩余 0**。上一批和并行 COMPLETE 的任务不重复计数；准备、测试、文档及目录维护计 0 TODO。原父级验收仍为 **10/171，剩余 161**。

| 轮次 | 独立 Issue | 实际状态 | 累计完成 | 本批剩余 |
| --- | --- | --- | ---: | ---: |
| 1 | #125 / #126 / #127 / #129 | 全部 closed/completed；[PR #143](https://github.com/jyqj/tabmail/pull/143) | 4/10 | 6 |
| 2 | #133 / #134 / #135 | 全部 closed/completed；[PR #150](https://github.com/jyqj/tabmail/pull/150) | 7/10 | 3 |
| 3 | #136 / #137 / #139 | 全部 closed/completed；[PR #151](https://github.com/jyqj/tabmail/pull/151) | 10/10 | 0 |

第三轮实际 merge `33f61fde3cf37d3cec2dd360146369c291eb0930`，完整树 `d982493e28431e644fd4018d8f746d8940280268` 与最终独审本地 `5c72ccac90d6f3f6c54d7b51bcb92994053840d7` 完全一致；Git API 核对 parents 为第二轮 actual merge `3a103cda4363cb8f32839eaaa73ab065eb967f79` 和公开 head `3ba8968edfdde1a56384d785dd17e417a63037e0`。完整保留并行 COMPLETE 三轮实现，未改 main。

三轮合并产品的同一干净受测源 `b0a5e0075cd4cae52446b1962a680ba4f84cf9ef` / tree `78c3a3da185852ca9131cd533b1fa1dee7c7e94c` 实际 **320 Go 叶 PASS / 0 FAIL / 0 SKIP**（317 本批回归+3 原协议）、**144 UI PASS**（6 个完整定向文件，含真实 Go producer→UI 三例）、**99 Python PASS**、原完整契约 CLI PASS；四次前后 source/clean 完全一致。最终仅追加两份文档，18 个产品/测试文件与 lane 最终源同字节，两名原生最终独审 ACCEPT。源证据 [37745039711](https://github.com/jyqj/tabmail/actions/runs/37745039711) SUCCESS。

Next 16.3.8 修复六个旧公告命中；完整 audit **9 high → 8 high**，残余 braces 链仍阻断 #9。原完整 UI 单 worker 首跑 **1648 PASS/5 FAIL/3 未执行**仍保留；53 编辑器定向复验和最终 144 联合通过不冒充全量绿。原件完整性 46 同字节实际 3 PASS/43 FAIL→46 PASS；域名 typed conflict 49 同字节实际 19 PASS/30 FAIL→49 PASS。PG 跳过、编译零执行及早期诊断均保留。

第三轮[完整 CI 37745039833](https://github.com/jyqj/tabmail/actions/runs/37745039833) 已结束 **FAILURE**：browser/production SUCCESS，frontend/backend FAILURE；精确同树前端 current 804 中6 failure/5 error、4 frozen PASS，完整 UI **1654 PASS/2 FAIL**（私有fixture及模板授权迟到结果的读取计数），tsc/lint/build通过、完整audit失败。模板授权原文件在干净公开33f同字节单次定向 **13 PASS**；CI没有第四个GET路径，可能的SWR缓存后台刷新尚未证实，原完整失败保留。后端失败步骤是PG race与三项目录/工具检查，原完整日志仍Transport closed。独立继承的真实Webhook claim [37745039777](https://github.com/jyqj/tabmail/actions/runs/37745039777) SUCCESS，不能代替完整PG。第二轮 [37741454774](https://github.com/jyqj/tabmail/actions/runs/37741454774) 已结束 FAILURE：browser/production SUCCESS，frontend/backend FAILURE；前端完整 UI 1655 PASS/1 必需 fixture FAIL，source-version 800 current 有6 failure/5 error、4 frozen PASS，strict audit失败；backend完整日志获取 Transport closed，未推断具体 PG 原因。目录漂移将以最终实际源在独立零 TODO 维护中核对，不改写旧失败。

本 PR 保持 **draft**；main 保持 `d6d512172fb6b874c3283d9df3f4d56758684a13`；不部署或发布。原 PostgreSQL、完整 audit、必需 private fixture、有效 M/G0 等门槛及 #4/#6/#9 保持开放。实际关闭结果与最终证据已通过独立文档 [PR #152](https://github.com/jyqj/tabmail/pull/152) 合入，actual merge `a0baabaa39d8dec0a9bf8cb54f2e71092bbd3b62`，完整树 `80fb455ded3449d3aa4e739d5728eb1564357309` 与独审文档源完全一致，parents 精确为 `33f61fde3cf37d3cec2dd360146369c291eb0930` / `2f96fa650f47a34a3d94b5f960ebebbb00db8105`；精确 head [source evidence 37747901597](https://github.com/jyqj/tabmail/actions/runs/37747901597) SUCCESS。该 PR 仅改两份账本文档，计 0 TODO。全部范围、命令、固定来源及完整 CI 的实际失败见 [CONTINUE 最终账本](https://github.com/jyqj/tabmail/blob/a0baabaa39d8dec0a9bf8cb54f2e71092bbd3b62/docs/company-mail/R5-CONTINUE-20261008.md)。

最后核对时，独立目录维护 [#149](https://github.com/jyqj/tabmail/issues/149) 仍 open，最终 revision11 尚未发布；3a 中间证据不作为 33f 最终目录验收。本批已完成原生产器的最终源预采集与固定依赖准备，但没有执行尚未公开的最终目录全量 runner，因此不声称其全量通过，也不抢写目录修订。
<!-- R5-CONTINUE-20261008:END -->

<!-- R5-ADVANCE-20261008:BEGIN -->
## ADVANCE：三轮十项完整交付，10/10 完成、剩余 0

本批从公开 `f2215611158d33ff9caed468bf6314e32dcf5046` 开始，root 与三个原生 subagent 使用独立工作树，多轮实现、同一固定红绿用例、交叉审查、精确完整树合入。上一批 CLOSE 十项不重复计数。本次最终读取确认 #111–#120 全部 **closed / completed**。

| 轮次 | Issues | 实际交付 | 本批剩余 |
| --- | --- | --- | ---: |
| 1 | #111 / #112 / #113 / #114 | #121 merged；四项 completed | 6 |
| 2 | #115 / #116 / #117 | #122 merged；三项 completed | 3 |
| 3 | #118 / #119 / #120 | #123 merged；三项 completed | 0 |
| 最后维护（0 TODO） | revision10 与关闭账本 | #124 merged | 0 |

最新实际整合 merge **`1322c9284d5dad8984adb61d2b4627b331c3740f`**，完整 tree **`a2eec6efe120fe65c4522369e747e3131cbe46fa`**，与最终本地审查 `5f0c366425207776043af631e3cfab00b190be3f` 完全一致，两个 parents 经 Git API 核验。#124 公开 head `20c39ab3ebaee46e5ac51e25d90a455166c9a31d` 的 [source-evidence 37729580509](https://github.com/jyqj/tabmail/actions/runs/37729580509) SUCCESS。实际产品三轮 merge 为 `d15a1e37`、`908c8418`、`703a5728`。

三轮 root 各自同一干净整合源实际通过 **64 Go / 28 UI**、**49 Go / 96 UI**、**140 Go / 25 UI**；最后实际 cmd 构造编译通过，运行 0 测试。各项均有独立 ACCEPT。全部十项完整行为、固定红绿、来源 hash、相关验证和逐轮关闭已回写 [ADVANCE 账本](https://github.com/jyqj/tabmail/blob/1322c9284d5dad8984adb61d2b4627b331c3740f/docs/company-mail/R5-ADVANCE-20261008.md)。

[revision10](https://github.com/jyqj/tabmail/blob/1322c9284d5dad8984adb61d2b4627b331c3740f/docs/company-mail/evidence/R5-CATALOG-REVISION10-20261008/README.md) 由 root/backend/frontend 独立复核 ACCEPT：三原 CLI、83 Python / 27 Node 全通过。原 clients 本来通过，未虚构漂移；旧 transaction/compatibility 的真实漂移已按实际来源更新。原 45 历史文件、164 保护源和 5 mutation 全保留。目录维护及报告计 0 TODO。

原完整 source-version runner 在本地精确核心 `3d1f9b0` 实际尝试一次，88.9 秒 exit 1，原 `Linux pinned executable FD unavailable` 前置检查拒绝：**774 发现、0 分派、774 missing**。原 loader/预算/FD 条件保留，没有把这次结果记为完整通过。

状态截点：**2026-10-08 04:55:14 UTC**。原父级仍 **10/171 已验收、剩余 161**；本 PR 保持 draft，main 保持 `d6d512172fb6b874c3283d9df3f4d56758684a13`。第三轮完整 [CI 37728167445](https://github.com/jyqj/tabmail/actions/runs/37728167445) 已 FAILURE；前端原日志 1501 PASS / 1 必需 external-batch 私有 fixture FAIL，另严格依赖审计与 PG race 门槛未通过。最后维护公开 head 的完整 [CI 37729580501](https://github.com/jyqj/tabmail/actions/runs/37729580501) 正在执行：production-web 已 SUCCESS，其余未结束；不赋予完整发布资格。原始日志、私有夹具、selection 和运行包未发布。
<!-- R5-ADVANCE-20261008:END -->

<!-- R5-CLOSE-20261008:BEGIN -->
## CLOSE 批次已完成：三轮 10/10 实际关闭，剩余 0

三个原生 subagent 与 root 使用独立工作树，分轮实现、固定失败基线验证、交叉审查、整合并逐项关闭：

| 轮次 | 已合入 PR | 实际 closed-completed | 累计 | 本批剩余 |
| --- | --- | --- | ---: | ---: |
| 1 | [#106](https://github.com/jyqj/tabmail/pull/106) | #96 / #99 / #100 / #103 | 4/10 | 6 |
| 2 | [#107](https://github.com/jyqj/tabmail/pull/107) | #97 / #101 / #104 | 7/10 | 3 |
| 3 | [#109](https://github.com/jyqj/tabmail/pull/109) | #98 / #105 / #108 | 10/10 | 0 |

十个 Issue 均在实际合入后关闭为 completed。#102 的保留服务经调用链检查发现没有当前生产消费者，撤销为 not_planned、计 0 completed；#108 修复实际活跃登录和旧密码验证的共同根因，作为一个替代实施项。测试、审查、报告、目录维护和 PR 管理不增加实施数。

### 最终目录与关闭账本已合入

静态维护 [#110](https://github.com/jyqj/tabmail/pull/110) 已合入，最终整合 **f2215611158d33ff9caed468bf6314e32dcf5046**，完整 tree **0f25acd0d6f2ae90f7ab883b205c57f3bb7973be**；与已审本地 bbe35755985ce68767703ace8428cb37c6586c04 逐字节相同，Git API 已核对两个 parents 和 tree。实际产品来源仍为 25b0b294f4fa0bb93b0c82304a570eae43a0923a，目录维护新增 **0 TODO**。

revision9 原三个 CLI 先真实拒绝旧目录，更新后全部通过。四个完整选定 Python 模块 **81 PASS / 0 FAIL/ERROR/SKIP**，原 Node collector **27 PASS / 0 FAIL/SKIP**；root 整合后再次顺序执行原三个 CLI 全部通过、执行前后同一干净源码。frontend/backend 对核心目录及最后结果文档均独审 ACCEPT。

PG 原源码、62 文件/395 函数/19 迁移/499 SQL、手工审查和证据等级保持。仅 35 个条目的 49 处 caller 行号、一个客户端 POST 行号及对应 source hash 更新。134 客户端分支、7 转发、132 routes、有限 94 路径 closure 的范围保持。原 19 个目录测试 ID 和五个 mutation 源码/AST 保留，新加两条当前保护测试；43 个历史文件、164 个原保护源完整保留。

原完整 source-version runner **实际尝试一次但未通过**：本地 9b0ce8d 候选在 TypeScript/Go selection/测试二进制编译完成后，被原 Linux pinned executable FD guard 拒绝。**772 discovered、0 dispatched/executed**，typed-wire 未启动；没有绕过 guard/loader/预算或重复尝试。81/27 通过不替代该完整门禁失败。最后只记录实际结果，四目录及受测 Python 字节不变。

### 产品验证与完整 CI

第三轮 root 同一干净整合源的 **27 Go 叶（race/count=1）和 51 UI 例全部通过，0 FAIL/SKIP**。作者相关组分别为权限表单 174 PASS、成员操作 73 PASS且无 unhandled、auth handler 84 PASS、authn/credentials 42 PASS；重叠运行不相加。每项独立 Issue 保留自身同一最终回归的失败基线和实现范围。

三个产品 PR 的 source-evidence 工作流及维护 [37720154106](https://github.com/jyqj/tabmail/actions/runs/37720154106) 均成功。最终整合 head 的 [source evidence 37720230077](https://github.com/jyqj/tabmail/actions/runs/37720230077) 也已 completed / SUCCESS；[完整 CI 37720230134](https://github.com/jyqj/tabmail/actions/runs/37720230134) 在最终核对时仍 in progress，未称全绿。

此前三个产品 full-release runs 37714614579、37716330081、37718845381 均已 failure；其中 PG、依赖审计、完整前端及当时的 source/catalog 门禁失败分别保留。新目录对账的有限通过不替代完整 PG/必跑 fixture、依赖审计或发布资格。

**原父任务仍 10/171 验收、161 未验收，原 171 个复选框与批次基线逐行一致。** 有效原规模 M/G0 及完整门禁仍待实际验收；本 PR 继续 draft，main 保持 d6d512172fb6b874c3283d9df3f4d56758684a13。历史 #17/#20/#24/#40 保留各自独立范围。新原始日志、运行包、二进制和私有 fixture 保持本地。

[最终关闭账本](https://github.com/jyqj/tabmail/blob/f2215611158d33ff9caed468bf6314e32dcf5046/docs/company-mail/R5-CLOSE-20261008.md) · [中央 TODO](https://github.com/jyqj/tabmail/blob/f2215611158d33ff9caed468bf6314e32dcf5046/docs/company-mail/R5-TODO.md) · [revision9 记录](https://github.com/jyqj/tabmail/blob/f2215611158d33ff9caed468bf6314e32dcf5046/docs/company-mail/evidence/R5-CATALOG-REVISION9-20261008/README.md)。
<!-- R5-CLOSE-20261008:END -->

<!-- R5-QUALITY-20261008:BEGIN -->
## QUALITY 批次完成：三轮原生 multi-agent，10/10

[Issue #90](https://github.com/jyqj/tabmail/issues/90) 已关闭为 completed。后端、前端和存储／PR 审查使用独立工作树分轮实施、交叉审查，三个交付 PR 均已合入：

| 轮次 | 整合 PR | 本轮完成 | 累计完成 | 本批剩余 |
|---|---|---:|---:|---:|
| 1 | [#92](https://github.com/jyqj/tabmail/pull/92) | 4 | 4/10 | 6 |
| 2 | [#93](https://github.com/jyqj/tabmail/pull/93) | 3 | 7/10 | 3 |
| 3 | [#94](https://github.com/jyqj/tabmail/pull/94) | 3 | 10/10 | 0 |

**本批完成 10/10，剩余 0；原父级清单完成 10/171，剩余 161。** 原 171 个父任务勾选逐项保持不变；审查、目录维护、报告和重复运行不增加实施计数。

十项修复分别覆盖：完整整数设置、密码 UTF-8 字节规则、S3 bucket 错误、原件引用 key 一致性、原子滑动窗口、邮箱保留期输入、失效缓存的在途代际、附件返回字节所有权、留存取消交接、收件选择失效。每项均有固定失败基线、同用例通过和独立审查。

最终公开产品 `2d23755f9b2cdfa3549705e15ca2b4783d74829b`、合并 `e0cd175996ca4ee314d7d8b8cf836023b680346c` 与本地交付同为完整 tree `5307be3cf057104d1bf1529e38235bbaf0c2bcdf`。并行 #84、#91 和前两轮均完整保留。

根代理最终验证：九个完整相关 Go 包以 race/count=1 运行，**464 叶 PASS / 0 FAIL / 10 个既有 PostgreSQL SKIP**；十九个相关前端文件 **265 PASS / 0 FAIL / 0 SKIP**。完整 Go build/vet、非增量 TypeScript 和全量 lint 均通过；lint 为 0 errors、4 个未改动文件中的既有 warnings。不同轮次和作者／根代理的重叠用例不相加。

三个原始 current-source CLI 均实际拒绝当前 source／caller／client 漂移，仍需目录对账；原校验器、历史与拒绝条件完整保留。完整 PostgreSQL／必跑及私有 fixture、依赖审计、最终 CI 与 M/G0 尚未验收。**本整合 PR 继续保持 draft。** 留存取消仅对已返回的已知 key 提供一次共享 5 秒有界交接，保留非原子提交／入队、未知结果、重试上限等限制；没有宣称新的真实 PG 故障、live Redis 或浏览器验收。

第三轮 [source evidence 37671936609](https://github.com/jyqj/tabmail/actions/runs/37671936609) 已 SUCCESS；[PR-head 全 CI 37671936648](https://github.com/jyqj/tabmail/actions/runs/37671936648) 在合并时仍进行中。最终整合 head `e0cd175996ca4ee314d7d8b8cf836023b680346c` 的 [source evidence 37672071238](https://github.com/jyqj/tabmail/actions/runs/37672071238) 已 completed / SUCCESS；[完整 CI 37672071473](https://github.com/jyqj/tabmail/actions/runs/37672071473) 在本次最终检查时仍 in progress。#84 已合入；历史 #17/#20/#24/#40 保留各自独立范围。

[完整 QUALITY 报告](https://github.com/jyqj/tabmail/blob/e0cd175996ca4ee314d7d8b8cf836023b680346c/docs/company-mail/R5-QUALITY-20261008.md) · [中央 TODO](https://github.com/jyqj/tabmail/blob/integration/company-mail-r5-pr-management-20261007/docs/company-mail/R5-TODO.md)。公开交付仅含代码、合成回归测试和简明记录，原始执行日志及证据包未上传。
<!-- R5-QUALITY-20261008:END -->

<!-- R5-PARALLEL-20261008-929:BEGIN -->
## Completed parallel batch 929 — three native multi-agent rounds

[#85](https://github.com/jyqj/tabmail/pull/85), [#86](https://github.com/jyqj/tabmail/pull/86) and [#89](https://github.com/jyqj/tabmail/pull/89) are merged: **4 + 3 + 3 = 10/10 distinct implementation tasks complete, 0 remaining**. The remaining count after each round was **6, 3, 0**. [Issue #81](https://github.com/jyqj/tabmail/issues/81) is closed as completed. Original parents remain **10/171 accepted, 161 remaining**.

This batch fixes retry overflow, current template-grant reads, DKIM key boundaries, stable webhook identifiers, template mutation ownership, full plus-address validation, index cancellation, template library recovery, valid domain hyphens and root-dot policy matching. Additional stale-retry and glob/parser corrections remain within their original task IDs. Concurrent #87 and #88 are preserved.

Public product `9b12c93cb03285298267e27893f74aebe742a8a2` and implementation delivery `87c5e6001f8e895d20d493eedd32aa038e232999` match their reviewed local whole trees. Final static maintenance [#91](https://github.com/jyqj/tabmail/pull/91) is merged at `f77c31e2da38bb926dfe6fa134eabad652e94f8c`, tree `60720ef19187344f442b51db18e0a8f3a9c1ddd6`, also identical to the final local delivery.

Own frozen product: nine complete Go packages with fresh race detection, **558 leaf PASS / 0 FAIL / 0 SKIP**; template group **150 PASS**; full Go build/vet, full lint and nonincremental TypeScript checks pass. Its full default frontend run is **1122 PASS / 8 FAIL / 0 SKIP** (one missing private fixture and seven original 5000 ms timeouts). After preserving #87: ten-package race **620 PASS / 0 FAIL / 10 existing PG-SKIP**, related UI **194 PASS / 0 FAIL / 0 SKIP**, and Go build/vet/TypeScript pass. The scopes and original raw failures remain separate.

Revision 7 reconciles current source catalogs and preserves revision-6 history. Author checks: three original CLIs pass, **62 Python / 27 Node controls pass**. Independent review: **ACCEPT**, three original CLIs pass, **17 reconciliation / 15 source-policy fixture methods pass**, with no skips. Integrated-candidate CLIs also pass. The fixture module is not full source-version or runtime qualification; overlapping runs are not added together. Five original tools, 35 historical files and negative mutation controls are retained. This maintenance adds **zero** implementation TODOs.

[Completed report](https://github.com/jyqj/tabmail/blob/e16bbe7bc85d0ae5a5b2d0be813974d59af22d88/docs/company-mail/R5-PARALLEL-20261008-929.md) · [Implementation evidence](https://github.com/jyqj/tabmail/tree/87c5e6001f8e895d20d493eedd32aa038e232999/docs/company-mail/evidence/R5-PARALLEL-20261008-929) · [Revision 7 evidence](https://github.com/jyqj/tabmail/tree/e16bbe7bc85d0ae5a5b2d0be813974d59af22d88/docs/company-mail/evidence/R5-CATALOG-REVISION7-20261008).

Pre-revision-7 [CI 37666141082](https://github.com/jyqj/tabmail/actions/runs/37666141082) is complete: production-web/browser-journey **SUCCESS**, frontend/backend **FAILURE**. At the final refresh, [source evidence 37668264382](https://github.com/jyqj/tabmail/actions/runs/37668264382) is **completed / SUCCESS**; integration [CI 37668264218](https://github.com/jyqj/tabmail/actions/runs/37668264218) remains **in progress**. This integration remains **draft** pending its full PostgreSQL, required/private fixtures, dependency audit, final CI and M/G0 qualification. Historical #17/#20/#24/#40 and separate STREAM #84 retain their own scope.
<!-- R5-PARALLEL-20261008-929:END -->

<!-- R5-STREAM-20261008:BEGIN -->
## STREAM 批次完成：三轮原生 multi-agent，10/10

[第一轮 #83](https://github.com/jyqj/tabmail/pull/83)、[第二轮 #87](https://github.com/jyqj/tabmail/pull/87)、[第三轮 #84](https://github.com/jyqj/tabmail/pull/84) 已全部合入。**本批 10/10 个独立实施 TODO 完成，剩余 0。原父级仍 10/171 验收、161 剩余。**

| 轮次 | 本轮完成 | 累计完成 | 本批剩余 |
|---|---:|---:|---:|
| 1 | 4 | 4/10 | 6 |
| 2 | 3 | 7/10 | 3 |
| 3 | 3 | 10/10 | 0 |

十项分别覆盖同步 suppression 地址规范化、租户 API key 会话归属、协议工件布局、原件流所有权、实际 ingest 恢复重放、邮箱成员授权冲突复核、JSON no-store 默认、冻结员工模板授权撤销、发送策略版本复核、Preview 资格与来源。并行批次与重叠计划已去重；目录、报告、审查和重复运行不增加实施数。

### revision8 已合入，原完整源码门禁实际通过

静态维护 [#95](https://github.com/jyqj/tabmail/pull/95) 已合入，当前整合提交为 **`1d856bd8a552c30dfb48b4902858edad7e53aaf5`**，完整 tree **`ed2af4702ad72d4b583901706bd31a078ebc54ae`**。它保留 #91 revision7 及 #92/#93/#94 QUALITY，通过公开 e0 最终产品源增量复核；这些并行任务保持独立计数。本维护新增 **0** 个 TODO。

公开交付 head `bc888e5d0ae54bc0377a62f8738343d0dfec3857`、已审本地交付 `268a89a6219e4e84ad0fe675b1fa04060dacea0c`、真实 CI checkout `ef74a0f22226288ba4156d20c3866bc887cd324e` 与最终合并的完整 tree 全部相同，已分别核对 GitHub Git 对象。目录只新增一处实际 SQL 执行调用（498→499），核对完整 caller 增量及一处 client 行号；134 条 client（含 7 转发）、132 条 route、94 路径有限 closure 均保持原范围。40 个历史文件、五个原 producer/validator、17 个原目录测试 ID 和五个 mutation 方法源码/AST 完整保留。固定哈希的公开统计快照消除了本地未发布 Git 对象依赖。

[原 CI 37674832207](https://github.com/jyqj/tabmail/actions/runs/37674832207) 已 **completed / failure**，以下结果由 root 和独立 CI subagent 分别读取原 artifact 后核实：

| 验证范围 | 实际结果 |
|---|---|
| 前端完整 source-version runner | **766 current + 4 frozen = 770 PASS** |
| 后端完整 source-version runner | 同样 **770 PASS**；两个运行不重复增加 distinct 用例数 |
| 源码执行完整性 | actual/loaded/requested IDs 逐项一致，fail/error/skip/missing/extra/overlap 全 0；原 pinned-FD wire fixture 实际 started/pass 各 1 |
| 原 Node 全命令及当前目录 | **49 PASS**；client/i18n/DTO/compatibility/transaction 检查均 PASS |
| 定向真实 PG 冻结员工撤销 | **9 叶 PASS / 0 FAIL / 0 SKIP**，另有父项 PASS；required manifest 与受测树逐字节相同 |
| 完整默认前端 | **1261 PASS / 1 FAIL / 0 pending**；唯一失败为明确的 required private fixture |
| 构建与静态检查 | Go build/vet、非增量 tsc、lint、原 frontend build 全通过 |
| 原 production-web / browser-journey | SUCCESS；browser 必跑 1/1 实际通过 |
| 依赖审计 | **8 high**，其余级别 0；clean-audit gate FAIL |

完整 PG 的唯一失败 package 为 PostgreSQL。247 项必跑中，67 同时满足 test/package PASS；76 虽 test PASS 但 package FAIL；104 test FAIL。因此 **180 项未满足门禁，不能称为 180 未执行**。首个错误为 migration version 5 的 context deadline exceeded，随后大量 fixture lifecycle budget exhausted；未观察到 data-race 警告或全局 test-timeout panic。原 protocol 局部运行通过，但仍 `task_complete=false`，保留 components 与后续迁移缺口。

合并后，当前整合 head `1d856bd8a552c30dfb48b4902858edad7e53aaf5` 自动触发的 [source evidence 37676221848](https://github.com/jyqj/tabmail/actions/runs/37676221848) 已 SUCCESS；[完整 CI 37676221788](https://github.com/jyqj/tabmail/actions/runs/37676221788) 在最终状态检查时仍 in progress。上表绑定已完成的原 PR CI 与完全相同的源码树，没有把仍在运行的新检查写成完成。

原 [source evidence 37674832313](https://github.com/jyqj/tabmail/actions/runs/37674832313) 也已成功。两次本地 full-runner FD 准备失败仍按原身份保留：770 discovered、0 executed，没有被改写成 PASS；GitHub 环境的真实完整执行记录独立存在。相关本地 79 Python、27 Node 与最终 19 reconciliation 复跑，以及 root 独立三个原 CLI 的通过，也分别记录。

### 历史产品验证与当前边界

本批原固定产品 `2aa4f4c18c18ef5417e36bc997034422b27a03c7` 与本地 `71f9fb8`、原 CI checkout `d1e5874dd91f1a48fa51528a313a2238f1177912` 同为 tree `cac18fef263d85b193372bbab9ff4fac6b95d31e`。它的真实 PG 9 叶由有效红基线 7 PASS/2 FAIL 转为 9 PASS；相关 UI 77 PASS、九个完整 Go 包 1149 叶 PASS/10 个本地 DSN-SKIP，完整 build/vet/tsc/lint/29 页构建通过。保留并行 #91/#92 的 `53cd5de` 另有十二完整 Go 包 1220 叶 PASS/10 个 PG-SKIP 及 build/vet 通过；这两个历史运行未冒称 e0 的同次执行。

**本整合继续 draft。** 完整 PG/必跑、必需私有 fixture、依赖审计、完整 CI 和有效 M/G0 仍需实际验收。静态目录的 task/runtime/product 资格标志保持 false，父级余数仍 **161**。main 保持 `d6d512172fb6b874c3283d9df3f4d56758684a13`；历史 #17/#20/#24/#40 继续保留独立范围。原生 subagent 采用独立工作树、多轮实施与交叉审查，新本地原始日志保留私有。

[完整 STREAM 报告](https://github.com/jyqj/tabmail/blob/1d856bd8a552c30dfb48b4902858edad7e53aaf5/docs/company-mail/R5-STREAM-20261008.md) · [revision8 记录](https://github.com/jyqj/tabmail/blob/1d856bd8a552c30dfb48b4902858edad7e53aaf5/docs/company-mail/evidence/R5-CATALOG-REVISION8-20261008/README.md) · [最终 CI 核验说明](https://github.com/jyqj/tabmail/pull/95) · [中央 TODO](https://github.com/jyqj/tabmail/blob/1d856bd8a552c30dfb48b4902858edad7e53aaf5/docs/company-mail/R5-TODO.md)。
<!-- R5-STREAM-20261008:END -->

<!-- R5-NEXT-20261008:BEGIN -->
## Completed NEXT ten-task batch — 2026-10-08

Three native multi-agent rounds are merged: [#79](https://github.com/jyqj/tabmail/pull/79), [#80](https://github.com/jyqj/tabmail/pull/80), [#82](https://github.com/jyqj/tabmail/pull/82). **10/10 new bounded tasks complete, 0 remaining; original parent checklist remains 10/171 accepted, 161 remaining.**

| Round | Completed this round | Cumulative | Remaining |
|---|---:|---:|---:|
| 1 | 4 | 4/10 | 6 |
| 2 | 3 | 7/10 | 3 |
| 3 | 3 | 10/10 | 0 |

Frozen NEXT product `ff8872b737a947c158035a0b07d78396397ad626`, tree `87d50267a34657765f666594aa56195539bccfc3`, exactly matches tested local db7702d. Round 3 root checks: 54 Go leaf cases with race detection, 84 Python controls, 35 domain UI cases and separately 22 invitation cases pass; full Go build/vet and nonincremental tsc pass. Each implementation received independent review.

Final evidence/report [#88](https://github.com/jyqj/tabmail/pull/88) is merged at `c3e1419e6245baf0190291ea868787cb2b0177ca`, complete tree `82fc2eec9ddd98dd4674cf1db0ca3e9916dcd909`, identical to the local final delivery. [Issue #78](https://github.com/jyqj/tabmail/issues/78) is closed completed. The complete 274-member implementation archives and the separately checked 49-member original CI artifact are preserved.

Revision6 is an accepted checkpoint at **f413a9138d305cf154ed2cecaddcf9b9a2397666**: original three CLI checks exit 0 and the final 15 reconciliation + two current-source methods all pass, zero skips. Five original validators, historical files and rejection guards are retained. Subsequent STREAM #87 at **f3ea4e9** is preserved; **all three original current-source CLIs actually reject its new drift**. Those failed outputs are retained, and f3 still requires incremental source reconciliation. No older result is presented as current f3 acceptance.

The fully inspected [#80 CI run 37654826526](https://github.com/jyqj/tabmail/actions/runs/37654826526) proves actual PostgreSQL protocol execution: 19 top-level / 88 leaf PASS, zero failures/skips, unchanged source manifest before/after. Whole CI remains failed. #82's later backend/frontend jobs were cancelled; production-web/browser-journey succeeded. The raw run identities and distinct qualifications remain explicit.

[Completed report](https://github.com/jyqj/tabmail/blob/c3e1419e6245baf0190291ea868787cb2b0177ca/docs/company-mail/R5-NEXT-20261008.md) · [Central TODO](https://github.com/jyqj/tabmail/blob/integration/company-mail-r5-pr-management-20261007/docs/company-mail/R5-TODO.md) · [Final review and raw records](https://github.com/jyqj/tabmail/tree/c3e1419e6245baf0190291ea868787cb2b0177ca/docs/company-mail/evidence/R5-NEXT-20261008/final)

Concurrent #81 and STREAM retain independent tasks and counts. Duplicate #77 was closed after exact comparison with #76; historical #17/#20/#24/#40 remain draft, and independent #84 remains a draft baseline. This integration remains **draft**. Whole PG/required and opt-in execution, private frontend fixture, dependency audit, subsequent source reconciliation and effective M/G0 remain open.
<!-- R5-NEXT-20261008:END -->

<!-- R5-FOLLOWUP-20261007:BEGIN -->
## Previous follow-up batch — 2026-10-07

Three native multi-agent rounds are implemented, independently reviewed and merged: [#74](https://github.com/jyqj/tabmail/pull/74), [#75](https://github.com/jyqj/tabmail/pull/75), and [#76](https://github.com/jyqj/tabmail/pull/76).

| Round | Newly completed | Cumulative complete | Remaining |
|---|---:|---:|---:|
| 1 | 4 | 4/10 | 6 |
| 2 | 3 | 7/10 | 3 |
| 3 | 3 | 10/10 | 0 |

**All 10 new bounded follow-up tasks are complete. Parent TODOs remain 10/171 complete, with 161 remaining.** Catalog maintenance, reports and repeated checks add no implementation TODOs.

Current integration head: `bcef70553ae0e2c541981e41edaa5e6993542d63`; complete tree `d6e6e0e3a2d3758a2ff6cff733847c35d0e65821` matches the final local delivery.
Final product source: `79738c17d185f1cd5cd1c8d550a9f15a5501f31c`; tree `1621d5fefb443d9d314abc2f9e0fab94914ce4c4` matches locally tested `9bdac9f`. Subsequent changes are reviewed catalogs and reports.

### Verified bounded results

- Four complete related Go packages: **760 leaf PASS, 0 FAIL/SKIP**, with race detection; whole-repository Go build and vet pass.
- Related frontend: **123 PASS, 0 FAIL/SKIP**; TypeScript, full lint and the exact-final-source production build pass.
- Revision 5 source catalogs: original related Python selections **73 PASS**, original Node collector selection **27 PASS**, and three original CLIs exit 0. Root separately reran all **13 reconciliation tests** successfully. Original validators, historical packets, mutation rejection checks and qualification boundaries are preserved.

### Remaining acceptance gates

The final default frontend run is **989 PASS / 14 FAIL / 0 SKIP**: one missing private fixture and 13 existing 5000 ms time-limit failures. One four-file, single-worker diagnostic gives **83 PASS / 1 FAIL / 0 SKIP** and does not replace the default full suite. Test timeouts, default worker configuration and skip rules are unchanged.

[Completed first-round CI 37642833794](https://github.com/jyqj/tabmail/actions/runs/37642833794) demonstrated backend independent checks and evidence upload after PostgreSQL failure. Production image and browser jobs passed, while backend/frontend remained failed. Full PG execution and opt-in fixtures, required protocol source-policy/manifest wiring, clean dependency audit, final CI and effective M/G0 acceptance remain incomplete. Revision 5 locally resolves reviewed catalog drift; these related checks do not claim a complete final versioned-source run.

The final [source evidence run 37651075246](https://github.com/jyqj/tabmail/actions/runs/37651075246) completed successfully. The new [integration CI 37651075233](https://github.com/jyqj/tabmail/actions/runs/37651075233) is still in progress at the final status check; its actual result remains authoritative.

Typed enum validation now rejects existing stored definitions containing invalid typed choices through the existing Render validation path. Repair the draft and publish a new version; historical immutable versions are not rewritten.

This PR remains **draft**. Main remains `d6d512172fb6b874c3283d9df3f4d56758684a13`. Independent historical/experimental drafts #17, #20, #24 and #40 are retained; the open queue contains these four and #56.

See [the completed follow-up report](https://github.com/jyqj/tabmail/blob/integration/company-mail-r5-pr-management-20261007/docs/company-mail/R5-FOLLOWUP-20261007.md), [central TODO](https://github.com/jyqj/tabmail/blob/integration/company-mail-r5-pr-management-20261007/docs/company-mail/R5-TODO.md), and [revision 5 reconciliation](https://github.com/jyqj/tabmail/blob/integration/company-mail-r5-pr-management-20261007/docs/company-mail/evidence/R5-CATALOG-REVISION5-20261007/README.md). Prior batch notes follow as history.
<!-- R5-FOLLOWUP-20261007:END -->

## 上一批历史进度（2026-10-07）

本 PR 是 R5 的活跃工作整合入口，**保持 draft，尚未取得完整 CI／发布资格**。

- 按用户要求完成了 **3 轮原生 multi-subagent、10 项独立实施改动：10/10 已完成，剩余 0**。
- 原始父级清单仍为 **10/171 已验收，剩余 161**；P0 为 10/12。实施子项分别绑定父任务，未直接扣减父级余数。
- 十项产品整合源码为 `e32564304c0c84aa80b1184e72129fb0316d989d`。目录 #73 与最终文档 #72 均已独立审核并合入；当前 head 为 **`28052dff454d04c8d28c4abe4bb8ed3a8bd5f6fa`**，tree 为 `8c076ad520f53a2a0ba31a27cd2df638d451ca7a`。
- 每一后续轮次继续同时报告本批与父级剩余数。main 保持 `d6d512172fb6b874c3283d9df3f4d56758684a13`；未发布或部署。

## 本批已经合入的十项实施 PR

| PR | 实际行为改进 |
|---|---|
| #61 | IPv6 relay 地址正确拼接，真实 IPv4/IPv6 完整 SMTP 投递与取消 |
| #63 | required TLS 检查前显式 SMTP Hello，保留 EHLO/HELO 协议、网络、取消与超时错误原因 |
| #67 | 收件人 uncertain、checkpoint 与临时失败保留原错误链，已接受目标不重复发送 |
| #65 | 恢复操作遇 409 时使旧检查和目标选择失效，重检后才允许继续 |
| #69 | 会话续期按账号／租户／epoch scope 隔离，同 scope 去重，旧任务不能覆盖新会话 |
| #71 | 恢复队列明确区分加载、错误和真正空结果，旧队列不继续提供操作 |
| #64 | 拒绝危险的非正 SMTP／retention 配置，保留已定义的合法边界 |
| #68 | 真正缺失附件返回 404；存储、取消与 MIME 解析失败保留原原因并返回受限错误 |
| #70 | 附件读取无进展时有界退出，取消阻止后续副作用，服务拥有的下载流仅关闭一次 |
| #62 | 前端独立检查失败后继续执行后续检查并上传原始证据，原失败仍使 job 失败 |

九项产品改动共有 **80 个冻结行为场景：基线 18 PASS / 62 FAIL → 候选 80 PASS**。CI 子项另有 3 条契约断言和真实 GitHub 执行证据。独立审核已核对全部十项的本地／发布／受测 tree 一致、merge 祖先关系、31 份原始证据哈希以及 171 父级勾选不变。#66、#72 的记录和 #73 的目录校准不另计实施项。

## 组合验证与实际阻塞

- 产品整合 `e325643…` 上的四个完整 Go 包和精确 8 个 handler 控制：**148 个顶层、829 个叶用例全部 PASS，0 FAIL / 0 SKIP**。两种计数口径不能相加。相关 vet PASS。
- 同一源码的普通独立 clone 上，原 `go build -mod=readonly ./...` PASS，默认 VCS stamping 保留；首次 linked worktree 的 VCS 环境失败日志独立保留。
- 完整默认前端：**886 PASS / 1 FAIL / 0 pending或todo**；唯一失败仍是原 `r5-external-batch-probe.test.tsx` 的 `explicit private fixture required` 前置条件。全量 lint 0 errors、4 个既有 warnings；默认 Next build 29/29 页面通过，完整非增量 tsc 已通过。
- 目录更新前产品源的 [GitHub CI 37631117018](https://github.com/jyqj/tabmail/actions/runs/37631117018)：**production-web、browser-journey SUCCESS；backend、frontend FAILURE**。前端 tsc／完整 Vitest／lint／build 及证据上传均实际执行；Vitest 同为 886/1，证明 #62 的失败后续行有效。
- 本次后端原始 artifact 绑定实际 checkout `8a6fe55db05bb11f9b6b0d21edd3331490d9d055`：PG 包 **180.059 秒超时**，105 个测试 skip 事件；247 必跑中 180 项未通过／未完成，原执行门禁保持失败。未把父子事件数冒称独立场景数。
- 同一运行的完整依赖审计仍为 **8 high / 0 critical**。revision3 的目录漂移已由 #73 精确修复并通过下述实际 source/客户端目录门禁；有效 M baseline、G0 与完整父项资格仍待取得。

附件上传保留调用方 reader 所有权；任意永久阻塞且没有取消协议的 `Read` 不能被强行中断。下载取消依赖底层 `Close` 能解除阻塞。此边界没有被局部测试掩盖。

## revision4 完整远端结果与最终收口

[已完成的 CI 37633385810](https://github.com/jyqj/tabmail/actions/runs/37633385810) 实际 checkout 为 `57ae4946ea60f0021c531aefc0c091dd984ca784`。它与本地目录提交、API 发布提交及目录整合 `9bcfa4f…` 的整个 Git tree 均为 `1eb98c6185e7b8b1e0be5967eb3fa741f84f0097`，root 与独审者分别以真实 Git 对象确认。

| 实际执行 | 结果 |
|---|---|
| 当前 source tests | **713/713 PASS，0 failure/error/skip**；源码 57ae4946 |
| 冻结历史 source tests | **4/4 PASS**；独立原源码 41b015c30c66b3ba58a3c1395e8559ebcd27a65f |
| 717 个发现 ID 的分发 | 无 missing/overlap，实际执行数与请求一致 |
| 前端 catalog／tsc／lint／build／失败后证据上传 | 全部 SUCCESS |
| 完整默认 Vitest | **886 PASS / 1 原 private-fixture preflight FAIL** |
| 严格 dependency audit | FAILURE，**8 high / 0 critical** |
| production-web／browser-journey | SUCCESS；浏览器必跑 1 PASS、0 FAIL/SKIP |
| backend | FAILURE，PG 包 **180.061 秒超时**，105 skip 事件，180/247 必跑项未通过／未完成 |

整体 workflow 仍为 FAILURE。backend 超时后的显式 compatibility/transaction CLI、DTO、协议、HTTP 与 vet 步骤是 SKIPPED；未把前端的成功结果当作这些步骤的远端通过。

最终文档提交与受审 `737bc632…` 逐字节同 tree。它相对目录整合只变更 README、中央 TODO、执行报告及本批证据，**产品、测试、原校验器、目录和工作流字节均与已审 9bcfa4f 保持一致**。最终同步后的 **86 份证据**逐一验证长度与 SHA-256，包含完整原始 CI ZIP、source-runner/Vitest JSON、独立复核及本地组合日志。原 31 份冻结证据的独审仍明确绑定其历史检查点，未扩张为对后续文件的同次审核。

当前 PR 队列只剩本 PR 与 #17、#20、#24、#40，全部为 draft。最终集成头 `28052df…` 的 [CI 37636514128](https://github.com/jyqj/tabmail/actions/runs/37636514128) 已于最终核对时 **completed / FAILURE**：production-web 和 browser-journey SUCCESS；frontend 的 source-version、catalog、tsc、lint、build 与失败后证据上传均 SUCCESS，严格 audit 与完整 Vitest 步骤 FAILURE；backend 在 Race tests with PostgreSQL 处 FAILURE。该头的 [源码归档 37636514098](https://github.com/jyqj/tabmail/actions/runs/37636514098) SUCCESS。

这里新增的是最终 head 的实际 job/step 终态；上表逐用例数字及 180.061 秒诊断仍绑定原实物运行 37633385810，未复制成对新运行的逐用例重新分析。仓库报告中的较早 in_progress 状态保留为当时的观察。完整 CI／发布资格仍未取得。

- [完整三轮实施与组合验证报告](https://github.com/jyqj/tabmail/blob/28052dff454d04c8d28c4abe4bb8ed3a8bd5f6fa/docs/company-mail/R5-MULTI-ROUND-20261007.md)
- [最终中央 TODO](https://github.com/jyqj/tabmail/blob/28052dff454d04c8d28c4abe4bb8ed3a8bd5f6fa/docs/company-mail/R5-TODO.md)
- [最终证据与哈希清单](https://github.com/jyqj/tabmail/tree/28052dff454d04c8d28c4abe4bb8ed3a8bd5f6fa/docs/company-mail/evidence/R5-MULTI-ROUND-20261007)

## PR 队列与历史工作

原 #17–#55 共 39 个 draft 已逐个核对；关闭了其中 35 个被整合内容完整覆盖的 PR。#17、#20、#24、#40 仍有独立历史内容，继续保留 draft。本 PR 保留原完整祖先链，未 force push 或删除历史。

此前 #57（DATA 最终回复）、#58（写信归属）、#59（兼容依赖更新）及 #60（revision3）已合入，其原验证范围与实际失败保留在 [PR 管理记录](https://github.com/jyqj/tabmail/blob/4065c4909c8f21a401a9a1af6370fa3f72670b99/docs/company-mail/R5-PR-MANAGEMENT-20261007.md)。

唯一活跃任务状态为 [R5-TODO](https://github.com/jyqj/tabmail/blob/integration/company-mail-r5-pr-management-20261007/docs/company-mail/R5-TODO.md)。后续优先推进真实 PG fixture／预算与必跑接线、剩余依赖审计和有效 S/M／G0。