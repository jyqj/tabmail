# PR #21 客户端接口清单专项附录

基线为正常 Git 获取的 `refs/pull/21/head`，固定 SHA `41b015c30c66b3ba58a3c1395e8559ebcd27a65f`。仅修改客户端收集器、对应 Node 测试、客户端清单及本接口文档；未修改中央 R5-TODO、Web 业务文件、依赖、CI、source inventory、两个 third_party replace 或 schema19。

## 精确差异

旧清单为127分支，当前未修正扫描为133分支。源码身份比较（排除 line 和 routes）为14项加入、8项移除；其中7对为位置/表达式/owner/transport变化，另为6个新调用位置和1个遗漏的有限后缀展开。

新增调用位置：

| 源码位置 | 方法与路由 |
|---|---|
| `web/features/company/company-event-consumer.tsx:58` | GET `/api/v1/company/overview` |
| `web/lib/api/company-events.ts:51` | GET `/api/v1/company/events` |
| `web/lib/api/permissions.ts:35` | GET `/api/v1/admin/permissions/{id}/deletion-preview` |
| `web/lib/api/permissions.ts:69` | GET `/api/v1/admin/users/{id}/permission-editor` |
| `web/lib/api/permissions.ts:75` | PATCH `/api/v1/admin/users/{id}/permission-editor` |
| `web/lib/api/permissions.ts:84` | POST `/api/v1/admin/users/{id}/permission-editor/assignment` |

迁移/修改的7对：recovery inspect 参数 `jobId`→`target`；submission attachment owner `SubmissionContentView`→`LiveSubmissionContent`、`f.id`→`file.id`；retry 从 submission-pane 移至 legacy-outbound；profile PATCH owner→`response`；company submissions owner→`result`；submission 与 submissionContent 从 company wrapper 改为直接 request。其余原有匹配行中74项仅行号变化。

旧 `legacy-outbound.ts` aggregate 的无后缀请求现改为 `${suffix}`，实际私有 helper 调用传入默认空串、`/recipients`、`/attempts`。旧扫描仅产生一个 `{id}{dynamic}` 路径。修正后同一18行产生3分支，总计130个调用位置、135分支：7个显式转发、127个已映射分支、1个未注册分支。已映射路由多重集合无移除，恰新增上述6个位置和 GET `/api/v1/outbound/{id}/attempts`；另暴露 GET `/api/v1/outbound/{id}/recipients` 无映射。

收集器只展开同文件、非导出的具名函数中有字符串默认值的参数，且所有函数引用必须为直接调用、所有参数值可静态确定。未知参数、导出、函数逃逸、spread 均保留 unresolved。扫描不执行模块、不发送 HTTP。计数、源码字段、方法、注册路由和映射检查均保持严格拒绝。

## 实际验证与阻塞

锁定安装：`npm ci --registry=https://registry.npmjs.org --cache=/workspace/tabmail-cloud/npm-cache --no-audit --no-fund` 成功，760 packages；未改锁文件。工具链核对为 Go 1.25.7、Node v24.19.0；本轮仅源码/Node验证，无 Go 执行或服务启动。

初始 `node scripts/collect_api_calls.cjs --check` 在81行失败 `client inventory count drift`，与委派提供的 CI run `37145582728`、frontend job `111268708016` 描述一致。未读取该 CI 日志：`gh run view ... --job ... --log-failed` 被 GitHub API `Forbidden` 拒绝；`gh pr view 21` 同样被拒绝，未换通道获取这些数据。

最终 `GODEBUG=asynctimerchan=0 node --test scripts/tests/*.test.cjs` 为35/35 PASS，0失败/跳过；其中API收集器13项。负例涵盖缺行、加行、方法漂移、路径漂移、未注册路由、错误映射、未知/导出/逃逸/spread helper，以及 recipients 不得继承 aggregate 已注册路径。`GODEBUG=asynctimerchan=0 python3 -B scripts/check_i18n_keys.py` PASS：1365 catalog keys、126 source files、657 literal keys、33 dynamic calls、474 inline calls。`git diff --check` PASS。

最终 `GODEBUG=asynctimerchan=0 node scripts/collect_api_calls.cjs --check` exit 1：`web/lib/legacy-outbound.ts:18 branch 1: GET /api/v1/outbound/{id}/recipients` 未注册；清单对此记录空 routes，非 forwarding。router 第217–221行只注册 list、detail、attempts、retry；132条服务端清单亦无该 recipients 路由。未通过路由检查，不能把测试通过解释为该命令通过。

逐行独立验证：135行全部源码字段与清单严格深相等；127个映射分支+7个forwarding通过原validate，且恰1项因上述未注册路径拒绝。此诊断不替代全量 `--check`，未过滤生产检查。

该未知路径须由后续 Web 或后端归属任务修正；本轮不得修改相应业务文件，故不能宣称 frontend/check 或整体CI绿。未运行 Go测试、Web build/typecheck/browser/Vitest、数据库/邮件/生产操作；未使用生产凭据，未合并或部署。
