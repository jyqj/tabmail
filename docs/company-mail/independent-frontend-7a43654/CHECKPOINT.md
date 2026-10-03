# 独立限定 frontend review — 7a43654

结论：**REQUEST CHANGES，1 个 P2 行为问题**。description-only stale 表单的主要修复通过独立浏览器验证；同字段确认后的显式选择仍可静默丢失。此结论仅限下列 frontend 作用域，不改变父 **10 / 171**、原失败或 unknown。

## 固定来源与执行范围

- 候选：`7a436549ace77d1baf30f33f6fb8e163077fe34c`，远端来源 `r5-permission-audit-20261003`。
- 对照：`93005b644c1f39fa85072fe913156cf3d6c798e1`；handoff `29a388a3c7d6da03641abe79bb5c728b90b75273` 相对此版本仅增加 `docs/company-mail/HANDOFF-LOCAL-CHECKPOINT.md`，已读。对照仅静态比较，本次没有对其运行浏览器用例。
- 自有分支 `review/independent-frontend-7a43654` 从候选精确 OID 创建。生产、候选源码、DTO、Go producer、中央 TODO、原审计附件未修改。
- 已读 `web/AGENTS.md` 和已安装 Next **16.3.3** 的 `node_modules/next/dist/docs/01-app/02-guides/testing/vitest.md`、`01-app/01-getting-started/05-server-and-client-components.md`。没有升级依赖；原 npm lock SHA256 `bd31660c52baa389e6d66a4ebf02a7ecd952bab72da6b16460f9d484b0be38c0` 与候选一致，复用当前安装依赖的 symlink 不入 Git。
- **实际 Chromium 151.0.7922.173 / Playwright**：通过 Vite 加载候选 `PermissionsPage`，保留真实 Base UI、英文 locale、SWR、API 序列化/验证、session owner、401 refresh 和 SSE consumer。auth hook 与 PageHeader 为合成替身，样式为测试布局；fetch 完全由本地合成 API 替换（非真实 HTTP 服务），合成 CAS 检查 revision 并更新内存 profile。请求/响应错误及延迟由 fixture 控制；延迟 fixture 刻意允许已 abort 的 Promise 继续完成，检验实际 session/assertion fencing。
- 浏览器上下文按用例隔离，外部网络请求全部阻断。没有真实账号、私人邮件、SMTP 或 DB；没有启动/连接本机已有后台服务。**不是 jsdom 独立交互证据，也不是 shipping Next E2E、PG 授权或 durable 业务验收。**

## P2：确认远端变化后，明确选择原值会被静默省略

位置：`web/features/company/profile-management.tsx:34–41` 的 `profileFieldPatch`，以及刷新后仍固定的 `editBaseline`。

真实浏览器步骤（独立用例 6）：

1. 打开原 quota **10** 的 A 表单，description 改为 `keep description`，quota 改为 **15**。
2. 第一次 PATCH 得到 synthetic HTTP409；草稿保持且禁止直接重试。
3. 远端 quota 改为 **20**、revision 为 `9007199254740999`。用户刷新，界面展示最新 quota20，保留 quota15，并要求人工勾选确认。
4. 勾选后，用户明确把 quota 从15输入为 **10**，点击 Save。
5. 实际第二次 PATCH 只有 `{description:"keep description", expected_revision:"9007199254740999"}`，**缺少 daily_send_quota**。合成 CAS 接受 description 更新，弹窗关闭，表格 quota 仍为20。

独立 oracle 是实际用户输入10应进入确认后的命令；没有调用或复刻作者 patch helper 得出期望。测试实际失败 `undefined !== 10`，原请求和结果见 `browser-results.json`，浏览器画面见 `failure-6.png`。原始 baseline 的值比较无法区分「未动旧值」与「看过远端后主动选回原值」。

建议维持 untouched stale 字段绝不提交的约束，同时独立记录刷新 review 后的明确字段编辑/选择，或提供逐字段保留/接受远端的可见选择。不要简单把整个 baseline 改为 fresh snapshot。关闭后重开当前版本再编辑可以作为恢复路径，但不能消除当前一次保存成功且未应用显式输入的问题。

## 真正运行的结果

| 检查 | 结果 | 有效边界 |
|---|---:|---|
| 自有独立 Chromium 用例 | **24 passed / 1 failed / 0 skipped** | candidate component + 合成 API；退出码1保留失败 |
| 既有作者 profile-management consumer suite | **28 passed / 0 failed** | jsdom 支持性回归，不作为独立 oracle；没有复核或继承作者“150 pass” |
| `node --check web/review-independent/run.cjs` | passed | runner 语法检查 |
| 原 package/lock、固定来源 diff | passed | 无 dependency / production source 修改；不是重装或完整依赖审计 |

独立 passed 覆盖：description-only409后所有远端 can_send/其他权限及四种 quota 保持撤销/修改；显式 false/0/[]；profile NULL zones 未动不写、未引入 user override/domain_access inheritance 命令；无改动无命令；同字段 local/remote description 可见且必须人工确认；刷新500/409保持草稿、冻结写入且成功重试恢复；read和write401/403锁当前session、同账号logout/relogin可恢复；取消关闭重开与A→B；旧刷新跨account、tenant、relogin不污染新草稿；旧PATCH完成不能关闭或解除新PATCH锁；连续409无自动replay且重新刷新可恢复；SSE使旧确认失效；目标消失可关闭选择B恢复；取消确认或再次刷新禁写；500后页面重载恢复；incomplete、foreign-tenant、system fresh snapshot拒绝且有效读可恢复。

**not run / unknown：** shipping Next 完整应用与真实登录页面、生产或 PG 授权、durable业务、真实邮件/SMTP、完整frontend150/whole build/四CI、Go/producer、user override独立面板与真实权限继承链。profile NULL保存的验证不转授这些层级。handoff 引用但当前云中没有的私人 raw/原运行树均 unknown，不补造。

## Fixture 开发错误（不计产品失败、不隐藏）

`fixture-development-history.json` 保留前三次开发运行的请求和错误摘要，将 fixture 错误独立标为 `fixture_error`：缺少测试 CSS 导致 switch 无尺寸、page.evaluate 未显式传入 status/seam/mode（status意外读到浏览器window.status）、Saving标签省略号错误、modal aria-hidden 的外部alert role定位错误、SSE frame未含tenant、确认checkbox选择器误取Base UI隐藏switch input。修正这些夹具后只剩用例6产品失败，且在多个运行中重复出现。最后冻结 runner/source 的 SHA256 见 `source-manifest.json`。开发截图不保留；最终产品失败截图保留。

## 重现与交付

在候选加本review证据的分支，保持原lock与依赖（Playwright及Chromium为环境工具，不是新增npm依赖）：

```bash
cd web
node node_modules/vite/bin/vite.js --config review-independent/vite.config.mjs
# 另一个终端，从仓库根目录执行
node web/review-independent/run.cjs
```

runner 只访问 `http://127.0.0.1:4179/review-independent/index.html`，每次重写本review目录中的结果与失败截图，失败返回1。环境需提供可被Node解析的 `playwright` 和 `/usr/bin/chromium`，可通过 `REVIEW_CHROMIUM` 指定本地可执行路径。没有修改npm manifest/lock以补这些工具。

提交/推送/自有draft PR已在本次授权范围内；只交付本review证据，绝不重试作者被拒的PR、切身份或换route。完成独立结论即停止，不merge/deploy。
