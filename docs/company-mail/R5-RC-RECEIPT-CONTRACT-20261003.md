# RC receipt 组件契约诊断与验收修复 — 2026-10-03

基线 `e5e6a2b4a429dbaf07f34a4f37a6d489d7e0422c`；首次修复提交 `5387b2c08e56e0f8db9a9ed0ea9b6fdc8fe6a152`。本包仅修 receipt 验收和必要 RC fixture，未发现需要恢复产品内容披露的理由。机器交接证据见 [safe metadata](evidence/R5-RC-RECEIPT-CONTRACT-20261003.json)。

## 历史失败和设计依据

[原 external-runtime 报告](evidence/R5-EXTERNAL-RUNTIME-20261003/README.md)记录真实 Go/PG/HTTP→Vitest RC01 在 `Attachments:` 等待失败，实际 submission GET 200；没有目标 marker，属于原用例未分类失败，原报告/哈希不覆盖、不重新标绿。

当前 `internal/company/outbound_receipt.go` 的 OutboundReceipt、`web/lib/receipt-types.ts` 的 closed parser 和 `SubmissionPane/LegacyReceiptFolder` 同用安全 aggregate。普通 DTO 禁止 subject、收件地址/BCC、正文、附件和协议秘密；view_content 只提示独立 live-content 授权。固定 RC01 case 明确安全投影且 view_content=false；RC02 禁止兼容/replay 内容旁路，RC03–RC05 要求完整 ledger 状态而非披露地址。因此 RC01 的附件等待、RC02 的 subject/旧 content 文案等待、RC03–RC05 共用附件及旧 retry/uncertainty 文案均是验收债，不是恢复旧披露的产品需求。固定 JSON/markers 保持。

## 修改边界

- 真实组件用例等待 `ordinary-receipt-aggregate`，检查捕获的真实 HTTP closed envelope、tenant/job 身份、时间/attempt 元数据、完整 ledger 计数、状态、全部 capabilities；UI 显示身份/计数/标签/retry/uncertainty，并禁止内容控件与 content/attachments 自动请求。
- RC02 检查原始 list 和 detail；same-key replay 返回身份绑定后续 list/detail。HTTP 和 UI 私密 canary 检查覆盖 subject、全部地址、正文；RC03–RC05 另覆盖诊断和 delivery token。失败只给布尔/安全类别，不打印 response/fixture/token。
- RC01/RC02 的 receipt_state/counts 来自作者种子 oracle，RC03–RC05 从原 case ledger 生成预期；不从被测返回生成 expected。Go 仅改 RC fixture setup，不改 launch/cache/sharedDB 修复。
- 初次比较使用 JSON.stringify，Root 复核指出合法 key order 可误报。本次改为 RC-only helper：精确 own 字段集合、逐字段 Object.is；counts 和 capabilities 都不依赖属性顺序。shipping closed parser 继续负责完整 DTO 的类型/范围/未知字段拒绝。

## 验证和未完成项

首次 41 unit 全是既存测试：`receipt-types.test.ts` 的 closed parser 与 `r5-protocol.test.tsx` 的三个 Go 单元投影/受控 hook UI case；没有新测试计入该 41。此次新增 `r5-receipt-contract-oracle.test.ts` **22 unit**：合法 counts/capabilities 重排各1；各字段缺失/错误值18；额外字段各1。合法重排同时覆盖 actual 和 expected 顺序。三文件 **63/63 PASS**，是 unit/受控渲染证据，不是正式 HTTP/PG/组件证据。

TypeScript noEmit、三文件 ESLint、diff check PASS。首次 tagged Go handler 编译 PASS，选择零测试，不算 runtime。日志只发布 safe SHA-256 与元数据，精确命令和源码哈希在上述 JSON。第一次 TypeScript 复核捕获 expected retry reason 过宽的 string 类型；已收窄为 RetryBlockReason 后检查通过。

本包不再 formal run；Root 等 cacheowner 返回后安排同 source 真实 Go/PG/Vitest RC 整合。RC02/BC03 sharedDB `89e7af39aa53408225b284548bffe39ce467aa2d` 由其 owner 管理，本分支不合入/改写。原 source-local cache blocker、RC01 原失败与缺层保持历史事实；正式结果 pending，task_complete/product_green=false。Go1.25.7、two replaces、锁、原120/180预算、external helper、LF/权限和固定 case/markers 不变。

正常 origin 首次 commit/push 成功；唯一 draft PR 创建返回 GraphQL `Forbidden`，未创建 PR，立即停止 API 动作，此次不重试。无 merge/deploy。中央 **10/171** 与全部父项门禁不变。
