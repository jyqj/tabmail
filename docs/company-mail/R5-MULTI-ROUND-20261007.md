# R5 三轮原生 multi-subagent 实施与组合验证 — 2026-10-07

本批按照用户“继续多轮 multi-subagent、至少推进 10 个 TODO、每轮报告剩余数”的要求，完成了三轮共十项独立实施改动，全部合入 [R5 集成 PR #56](https://github.com/jyqj/tabmail/pull/56) 的工作分支。

**本批完成 10/10，剩余 0；原始父任务完成 10/171，剩余 161，P0 为 10/12。** 原父项的 ID、顺序、复选框及完整验收条件保持不变。这里是一次执行报告；唯一活跃任务状态仍为 [R5-TODO](R5-TODO.md)。目录校准、文档、重复验证与证据文件均不另计实施 TODO。

## 十项交付

| 轮次 | 子项／父项 | 已合入 PR | 行为与固定回归 |
|---|---|---|---|
| 1 | P6-090：IPv6 relay | [#61](https://github.com/jyqj/tabmail/pull/61) | 正确组合 IPv6 host/port；真实 IPv4/IPv6 loopback 完整 SMTP 投递及取消。4 个固定场景，基线 2 PASS/2 FAIL → 4 PASS |
| 2 | P6-040/050：SMTP greeting 原因 | [#63](https://github.com/jyqj/tabmail/pull/63) | required TLS 检查前显式 Hello；保留 EHLO/HELO 回复、传输、取消和超时原因。12 个场景，6/6 → 12 PASS |
| 3 | P6-050/060：收件人错误链 | [#67](https://github.com/jyqj/tabmail/pull/67) | uncertain、持久化失败和临时失败汇总保留原 cause；已接受目标不重复投递。8 个场景，1/7 → 8 PASS |
| 1 | P9-080：恢复冲突后重检 | [#65](https://github.com/jyqj/tabmail/pull/65) | 409 后使旧检查和目标选择失效；成功重检并重新选择后才能继续，保留操作理由。7 个场景，3/4 → 7 PASS |
| 2 | P9-090：会话续期 scope | [#69](https://github.com/jyqj/tabmail/pull/69) | 账号、租户或 epoch 改变时隔离旧续期；同 scope 去重，旧 finally 不清除新 owner。6 个场景，0/6 → 6 PASS |
| 3 | P9-100：恢复队列状态 | [#71](https://github.com/jyqj/tabmail/pull/71) | 区分 loading/error/成功空结果；加载或失败时禁止旧队列操作，检查结果和理由独立保留。8 个场景，2/6 → 8 PASS |
| 1 | P8-110：资源配置边界 | [#64](https://github.com/jyqj/tabmail/pull/64) | 拒绝危险的非正 SMTP 限制、超时和 retention 参数；保留已定义的连接数 0 语义。18 个场景，1/17 → 18 PASS |
| 2 | P8-060：附件源错误分类 | [#68](https://github.com/jyqj/tabmail/pull/68) | 仅真正缺失附件返回 404；存储、取消及 MIME 预算错误保留原 cause，HTTP 500 使用固定受限文案。10 个场景，2/8 → 10 PASS |
| 3 | P5-060：附件读取进度与取消 | [#70](https://github.com/jyqj/tabmail/pull/70) | 连续无进展读取有界退出；取消阻止后续副作用，拥有的下载流仅关闭一次。7 个场景，1/6 → 7 PASS |
| 1–3 | P11-080：前端 CI 证据 | [#62](https://github.com/jyqj/tabmail/pull/62) | 安装成功后，独立检查失败不阻止后续检查和证据上传；失败仍使 job 失败。3 条独立契约断言，另有实际 GitHub 运行验收 |

九项产品改动共 **80 个冻结行为场景：基线 18 PASS/62 FAIL，候选 80 PASS**。CI 三条断言另计，不与产品场景混算。整包、定向回归、父子测试事件会重叠，不能累加为更多独立场景。

三个原生 subagent 分别实施 SMTP、后端和前端三轮工作，随后由不同成员交叉复核；root 负责 CI 改动、唯一 TODO、GitHub 发布和整合。每项完整的本地提交、API 提交、相同 Git tree、PR、merge 与证据映射见 [机器账本](evidence/R5-MULTI-ROUND-20261007/wave-batch-summary.json)。账本独立审核结论为 ACCEPT：十项映射、原 31 份证据字节和 171 父项全部相符，详见 [独立审核](evidence/R5-MULTI-ROUND-20261007/wave-batch-independent-review.md)。

## 被验证的源码

- 本批起点：`4065c4909c8f21a401a9a1af6370fa3f72670b99`。
- 十项合入后的产品提交：`e32564304c0c84aa80b1184e72129fb0316d989d`；tree：`bfa06a61ce4bc8411243d3b57de26db5e161dc8b`。
- 完整前端本地运行：`6c73230d7f3c00ab640e2a8b7ce10b20bb88a2f4`，整个 `web` tree 为 `24f095f234179d15ba692e003fc2f34709026817`，与产品整合逐字节相同。该本地运行中的 Go 支持组件使用它自己的 Go 源码，结果没有移作别的 Go tree 的完整资格。
- 产品 GitHub CI 的实际 checkout：`8a6fe55db05bb11f9b6b0d21edd3331490d9d055`；root 已取回该 Git 对象并确认整个 tree 与 `e325643` 相同。
- revision4 本地提交：`7254f12e833051ce4a6bb6d4cb145db4795f3adc`；API 发布提交：`6b6163dc8941aa36bb9fa2b75f40c101b8f662fe`；同一 tree：`1eb98c6185e7b8b1e0be5967eb3fa741f84f0097`。目录及本报告不改变上述产品字节。

## 组合验证的实际结果

| 范围 | 结果 | 资格边界 |
|---|---|---|
| 完整 config、outbound、companymail、mailcontent 四包 race | 140 顶层、811 叶用例 PASS；0 FAIL/0 SKIP | 在产品整合 `e325643` 实跑完整四包，使用 `-mod=readonly -race -count=1 -timeout=180s` |
| 精确 8 个 handler 控制 | 8 顶层、18 叶用例 PASS；0 FAIL/0 SKIP | 运行前后核对精确名称；真实 HTTP/服务控制加合成存储接缝，不是完整 handler 包或 PostgreSQL 资格 |
| 相关五包 vet | PASS | 一次执行；无重复刷绿 |
| 整仓 Go build | PASS | 普通独立 clone 的同一 `e325643`，默认 VCS stamping，`go build -mod=readonly ./...`，一次补验 9.500 秒 |
| 完整默认 Vitest | 886 PASS、1 FAIL、0 pending、0 todo；63 文件中 62 PASS/1 FAIL | 144.320 秒；未新增过滤、排除、skip 或替代 fixture |
| 全量 ESLint | PASS，0 errors、4 个既有 warnings | warnings 均来自本批未改文件 |
| 默认 Next 生产 build | PASS，29/29 静态页面 | Next 16.3.6/Turbopack，默认 8 个页面 worker |
| 完整非增量 tsc | PASS | 最后一轮相同 web 产品和测试字节上的实跑；组合阶段未重复同一命令，Next build 的 TypeScript 阶段也通过 |

Go 两个执行范围合计 **148 顶层、829 叶用例、896 个测试终态 PASS**；这些是同一执行的不同计数口径，不能相加。首次 linked worktree 的 build 因 Git 状态识别失败 exit 1；保留原日志和原失败摘要，在正常独立 clone 的相同源码补跑唯一失败命令后 exit 0，没有关闭 `buildvcs`。详见 [原组合摘要](evidence/R5-MULTI-ROUND-20261007/wave-final-go/summary.json) 与 [构建补验](evidence/R5-MULTI-ROUND-20261007/wave-final-go-build-clone/summary.json)。

完整前端唯一失败仍是 `web/r5-external-batch-probe.test.tsx` 第 9 行的 `explicit private fixture required`：未提供 `TABMAIL_R5_PROTOCOL_COMPONENT_FIXTURE`，尚未读取 fixture、挂载组件或执行 HTTP/PG。与之前 866 项完整结果按文件、fullName、status 比较，仅新增本批 21 个通过场景，旧结果无删除或变化。详见 [完整前端摘要](evidence/R5-MULTI-ROUND-20261007/wave-combined-web-summary.json)。

## 当前目录 revision4

[目录 PR #73](https://github.com/jyqj/tabmail/pull/73) 校准四份当前 client/transaction/compatibility 目录。旧目录在当前产品源码上使三项原 CLI 全部 exit 1；校准后全部 exit 0。相关 Python **71 PASS**、原 client scanner **27 PASS**，0 FAIL/0 SKIP。

精确变化只有 9 处客户端行号、15 个事务条目的 29 处调用行号、1 个新增和 2 个删除的同名调用候选、7 个兼容路由行中的客户端元数据及 3 个声明闭包哈希。保留 134 个客户端分支、7 个转发器、132 条路由、62 个 PG 文件、395 个函数、498 个 SQL 调用、19 个迁移和原有 review/false flags。`Close`/`New` 是原收集器的同名候选，不能解释为已解析到 PostgreSQL 调用。

revision1/2/3 固定 Git 对象、20 份旧报告及 6 个既有测试方法的 AST 保持；五个原验证器/收集器、原 source-runner dispatch、预算与 skip 策略保持。新版本的两项测试进入原 current 分组；仅 discovery 713 current/4 frozen 不是整个 source runner 的实际执行结果。完整变化、原始日志和边界见 [revision4 报告](evidence/R5-CATALOG-REVISION4-20261007/README.md)。

不同 subagent 的固定 Git/JSON/AST/源码行独审结论为 **ACCEPT**，57 项只读审阅核对成立；该数字不是新增产品测试。目录 #73 已合入集成提交 `9bcfa4f77f27ad4f118f32e92efef740ebac2c1e`。见 [目录独立审阅](evidence/R5-MULTI-ROUND-20261007/catalog-revision4-independent-review.md)。

## GitHub 实物验证与未通过的门禁

[产品整合 CI 37631117018](https://github.com/jyqj/tabmail/actions/runs/37631117018) 实际终态：**production-web 和 browser-journey SUCCESS；backend 和 frontend FAILURE**。前端安装、tsc、完整 Vitest、lint、build 与失败后的证据上传均实际执行；Vitest 同样为 886 PASS/1 FAIL。源码归档 [37631116946](https://github.com/jyqj/tabmail/actions/runs/37631116946) 成功，归档性质不代替发布门禁。

该运行使用旧 revision3 目录，因此 source-version 和目录检查失败；revision4 的校准资格单独按上节报告和其实际 CI 记录判断。依赖审计 artifact 经 ZIP SHA-256 对账后确认仍有 **8 high、0 critical**，严格零漏洞门禁保持失败。

后端原始 artifact 同样核对了下载字节与 GitHub digest：PG 包 **180.059 秒超时**；实际观察到 **105 个测试 skip 事件**，247 个必跑项中有 180 个未通过／未完成。原报告中的 7,977 个 PASS 是含父子层级的事件数，不能冒称全套通过。原执行门禁拒绝这一运行。之前“串行累计耗尽整包预算”的分析是旧固定运行的实物结论；本次只记录新的超时事实，没有将旧逐测试耗时移作本次新分析。见 [本次 CI 摘要](evidence/R5-MULTI-ROUND-20261007/wave-ci/remote-37631117018/acceptance.json)。

#62 另在 [37628534160](https://github.com/jyqj/tabmail/actions/runs/37628534160) 证明原失败后后续检查和证据上传真实执行，原 job 仍失败。该早期证明有独立 checkout 与 artifact 身份，未充当最终产品树的完整资格。

### revision4 完整远端结果

目录合入后对应的 [PR #73 CI 37633385810](https://github.com/jyqj/tabmail/actions/runs/37633385810) 已在2026-10-07 14:10:32 UTC结束。实际受测checkout为 `57ae4946ea60f0021c531aefc0c091dd984ca784`，tree为 `1eb98c6185e7b8b1e0be5967eb3fa741f84f0097`；root和独审者已分别以真实Git对象确认，它与API发布6b6163d、本地7254f12、目录整合9bcfa4f四者整个tree相同。

- **当前源码713/713 PASS**，来源为57ae4946；**冻结历史4/4 PASS**，独立来源仍为原 `41b015c30c66b3ba58a3c1395e8559ebcd27a65f`。两组均0 failure/error/skip，717个发现ID没有missing或overlap。这是实际完整runner结果，与前述本地仅discovery的记录区分。
- 前端Catalog and TypeScript source validation、tsc、lint、build、失败后证据上传均SUCCESS。完整Vitest为886 PASS/1 FAIL，唯一仍为原privatefixture前置失败；完整audit为8 high/0 critical，严格门禁FAIL。
- production-web和browser-journey均SUCCESS；浏览器必跑1项实际PASS、0 FAIL/SKIP。frontend与backend两个job仍FAIL，整体运行FAIL。
- 本次PG包在180.061秒失败，fresh rawlog指出 `TestR5EnqueueProtectsAuthorityThroughInsert/override-insert` 在180秒超时闹钟时运行。严格checker记录7975 PASS、105 SKIP、8093 START事件，247必跑中180未通过/未完成。backend之后的显式compatibility/transaction CLI、DTO、协议、HTTP和vet步骤SKIPPED，不将前端的成功步骤借作这些步骤的远端成功。

四个下载ZIP的完整摘要均与GitHub artifact digest一致；源runner、完整Vitest、audit、backend和browser checker及实际checkout文本已原字节保存，7,230,203字节backend JSONL保留在ZIP中。详见 [远端实物报告](evidence/R5-MULTI-ROUND-20261007/remote-ci-revision4-37633385810/README.md) 和 [机器摘要](evidence/R5-MULTI-ROUND-20261007/remote-ci-revision4-37633385810/summary.json)。源码归档37633385737成功，只记录其41,509,332字节artifact身份而不将归档当作发布资格。

后续文档仅修改README、TODO、执行报告与证据，仍以各次实际受测checkout命名结果。较晚的#56运行37634509934在采集时处于in_progress，记录为状态观察，不改写为本次已结束的结论。

## 保留的实现边界与下一阶段

附件上传保留调用方 reader 所有权；任意永久阻塞、没有取消协议的 `Read`，无法被该包装强行打断。下载源由服务拥有，取消关闭一次，但底层 `Close` 必须能解除读取阻塞。20 MiB、SHA 完整性和授权复查保留。SMTP 的不确定接受仍进入受控恢复；普通回执保留固定字段投影，不能据错误链推导额外内容权限。

下一阶段优先处理 PostgreSQL fixture 成本与预算、必跑用例接线、剩余依赖审计，再完成有效 M baseline 与 G0。完整父项仍按中央清单验收。#56 保持 draft，`main` 未修改，本批未发布或部署。

证据目录的 [文件清单](evidence/R5-MULTI-ROUND-20261007/evidence-files.json) 记录每份持久化证据的长度和 SHA-256。原始日志逐字节保留 ANSI、尾部空格和空行；衍生的 CI 失败事件摘录明确标明并非完整原始日志，未为消除空白诊断改写证据。
