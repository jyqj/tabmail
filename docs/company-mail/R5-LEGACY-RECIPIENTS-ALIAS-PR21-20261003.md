# PR21 legacy recipients alias 专项 TODO 附录

固定 base：[PR21](https://github.com/jyqj/tabmail/pull/21) head `41b015c30c66b3ba58a3c1395e8559ebcd27a65f`，分支 `fix/r5-legacy-recipients-alias-20261003`。本任务仅修改 `web/lib/legacy-outbound.ts`、直属测试和本专项附录/证据；中央 TODO、scanner、中央接口清单、reply 组件、后端路由、依赖与锁文件不改。中央 **10/171 保持**。

## 语义核对与最小修复

历史 `364925d` 的 legacy wrapper 仅 list/detail；`93005b6` 首次加入 recipients helper，并明确所有兼容函数返回相同 closed OrdinaryReceipt。历史 OpenAPI/router 对 `/api/v1/outbound/{id}/recipients` 的精确字符串检索没有注册记录；当前 OpenAPI 仅 list/detail/attempts/retry，router detail 继续由 RequireTenantKeyOrAdmin + send:read + GetJob 授权。`/api/v1/company/outbound/{id}/recipients` 是独立 company 接口，不能据此推导 legacy 路由存在。

当前生产调用点：LegacyReceiptFolder 仅使用 list/detail；recipients 仅已有 consumer test 调用，该测试的宽 outbound mock 接受了不存在的路径。未发现逐地址返回契约。按父架构裁决，保留 `legacyOutboundRecipients(id)` 函数名，改为 `aggregate(id)` 请求已有受权 GET detail；attempts/retry 保持原路由。aggregate 原 ID 编码、tenant/ID response scope、session capture/assert 和 closed parser 字节不改；不增加 recipients/BCC/raw DTO 字段或敏感端点。

## 实际验证

- 已检查空 `.agents` / `.codex` 技能目录及 `web/AGENTS.md`，读取安装内 Next.js client directive/Vitest 文档。官方锁定 `npm ci --registry=https://registry.npmjs.org --cache=/workspace/tabmail-cloud/npm-cache --no-audit --no-fund` 成功，760 packages；Node v24.19.0 / npm 11.9.0，锁文件保持。
- 新直属测试 **23 个**，只替换 fetch，使用真实 request/session/parser；成功 mock 仅接受精确 GET detail，无 query/body，检查当前 tenant/JWT 与 AbortSignal。覆盖 ID 编码、未知进度正负例、跨 ID/tenant、缺少 known tenant、数组/raw recipient/BCC/嵌套字段/扩展 envelope 拒绝、401/403/404/500 传播无 fallback、tenant/actor/role/epoch 变更及读取 body 期间变更拒绝、同 scope token 更新允许且后续读使用新 token。
- 旧实现负控制：临时恢复 `/recipients` suffix，精确路径测试 **预期 FAIL / exit 1**；finally 恢复修复字节。原失败日志保留，没有将 negative control 的21项未选测试当成通过。
- 首轮相关三套 78/78 PASS（补 token 正例前）。全量首轮 38 suites / 524 PASS，但共享 Go producer suite 因 PATH 中同名非 Go 工具报 `Go: Unknown option: test`，3项 setup skip，exit1，原日志保留。
- 使用现有官方 Go1.25.7 的 PATH/GOMODCACHE/GOCACHE，`GOTOOLCHAIN=local GOWORK=off GOENV=off GOFLAGS=-mod=readonly GODEBUG=asynctimerchan=0 npm test` 完整重跑：**39 suites / 527 tests PASS，0 skip/failed**。共享 producer 使用 FakeStore；未 source 环境秘密或启动邮件/数据库服务。
- `tsc --noEmit`、变更文件 eslint、`npm run build`、`git diff --check` **PASS**。
- 全仓 `npm run lint` **FAIL：9 errors / 4 warnings**。errors 是基线已有 wire-response-input.test.ts 的 no-empty-object-type 和 profile-refresh-browser/review-independent 两套 harness 的8个 require import；这些文件与固定 base 完全相同，未修改/排除。当前锁定官方 `npm audit` **26 项：4 moderate / 21 high / 1 critical**，exit1；保留原 JSON，未改依赖或宣称 strict audit 绿。

完整日志及源文件哈希：[receipt](evidence/R5-LEGACY-RECIPIENTS-ALIAS-20261003/receipt.json)。这里只授予 synthetic fetch transport/parser 和本地 Web type/test/build 资格，不授予真实 API scope/HTTP/PG、browser reply、四 CI、wholePG、shipping 或 SOURCE-attestation 新资格。既有 unknown/失败保留。

## Scanner 归属任务待办输入

以本分支实际 head 的 `web/lib/legacy-outbound.ts` 再生成；原 scanner `f2f887de47b743c654ab3fc9936e8d4d78d91eb6` 仅作只读诊断，没有应用其文件或提交。该 scanner 对本源码只读 scan 实际为 **134 branches**（其原输入135），aggregate 第18行仅空 suffix 和 `/attempts` 两分支；移除未注册 GET `/api/v1/outbound/{id}/recipients`，保留 detail/attempts 注册映射。retry request 因新增说明行移动到第29行。直属 `.test.ts` 被原 scanner 排除，无新增生产调用位置。

父/归属任务须在组合源码上刷新 `R5-CLIENT-CALLS.json`、检查 exact mapping 和 `--check`；任何并行 reply 变动须重新以组合树计算，134不能硬写为组合总数。本分支原 scanner `node scripts/collect_api_calls.cjs --check` 实际 **exit1 client inventory count drift**；未改 scanner 或中央清单，不能称此 gate 已通过。

未执行 merge/deploy/force push；commit、普通 push、draft PR 已授权，交付结果以实际工具回执为准。
