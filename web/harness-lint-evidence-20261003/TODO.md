# PR21 测试 / 浏览器 harness lint 专项 TODO

范围：固定 PR21 `41b015c30c66b3ba58a3c1395e8559ebcd27a65f`，审计分支 `audit-pr21-41b015c` / `d6640db3f1b6833f082d7d85948300c8aebdc186`。只收口既有 9 个 lint errors；不关闭中央 R5 TODO 的产品叶项或父门禁。

- [x] 先读取审计 `lint.log` 与 `baseline-lint.log`：两者均 9 errors / 4 warnings；原样副本为 `audit-lint.txt`、`audit-baseline-lint.txt`。
- [x] `lib/wire-response-input.test.ts:12:44`：用 `Pick<T,K> extends Required<Pick<T,K>>` 判定必填属性，保留所有 wire/input 类型断言和运行测试。
- [x] 两个 `run.cjs:1–4`：CommonJS async 入口动态加载 Node 内置模块；Playwright 用 `createRequire(__filename)` 保持现有 `NODE_PATH` 独立工具解析合约。`import('playwright')` 不保持 NODE_PATH，故未采用。未关规则、降级错误或修改 ESLint 配置。
- [x] 新增可选 `REVIEW_OUTPUT_DIR`：缺省输出路径不变，本轮指定新目录避免覆盖历史 checkpoint / JSON / 截图。
- [x] 官方 registry 原 lock `npm ci` 成功；Playwright 1.58.2 单独安装于 `/tmp/tabmail-harness-playwright`，未改 package / lock。首次默认 cache ENOENT 的原日志保留；改用工作区 cache 后成功，无 EACCES / 权限拒绝、无换源。
- [x] `node --check` 两 runner、`git diff --check` 均 exit0。
- [x] 相关 unit：`npm test -- --reporter=dot lib/wire-response-input.test.ts features/company/profile-management.consumer.test.tsx`，2 files / 31 tests passed，exit0。
- [x] `tsc --noEmit --incremental false` exit0（空 `tsc.txt`）。
- [x] 全 web lint：`npm run lint` exit0，0 errors / 4 warnings，原 warnings 全部保留。位置：profile-management.tsx:277、submission-content.tsx:52、两个 browser entry.jsx:34。
- [x] profile-refresh-browser 原 28 用例：Chromium 151.0.7922.173，28 passed / 0 failed，exit0。新结果 `profile/browser-results.json`；旧 before-results/results 未改。
- [x] review-independent 原 26 用例：Chromium 151.0.7922.173，26 passed / 0 failed，exit0。新结果 `independent/browser-results.json`。
- [ ] draft PR 创建：`gh pr view 21 --json number,headRefOid,headRefName,baseRefName,state,url` 返回 `Post "https://api.github.com/graphql": Forbidden`，已停止该 API 动作，不换 API 路径绕过。独立交付分支 `fix/pr21-harness-lint-41b015c`。

验证只用 loopback Vite + synthetic API/auth，真实组件 / Base UI / locale / SWR / API session transport；不是 shipping Next / PG / durable 验收。没有真实邮件、外部服务或 SMTP。本轮没有 Go 写入或 Go 测试需求，不宣称 Go 回归。

已检查 `/workspace/.agents`、`/workspace/.codex`（空）、仓库中 skills / AGENTS（只有 `web/AGENTS.md`），并遵循 web 指引。生产业务、sourceattestation、browser_company.cjs、catalog、package/lock 及旧证据未改。

浏览器 CLI（repo 根目录，先在 web 启动对应 Vite config，端口 4179）：

```sh
NODE_PATH=/tmp/tabmail-harness-playwright/node_modules REVIEW_OUTPUT_DIR=/workspace/tabmail/web/harness-lint-evidence-20261003/profile PROFILE_CANDIDATE=41b015c-plus-harness-lint-fix node web/profile-refresh-browser/run.cjs
NODE_PATH=/tmp/tabmail-harness-playwright/node_modules REVIEW_OUTPUT_DIR=/workspace/tabmail/web/harness-lint-evidence-20261003/independent PROFILE_CANDIDATE=41b015c-plus-harness-lint-fix node web/review-independent/run.cjs
```

两 Vite 进程串行启动；切换时首次旧进程仍占端口的失败日志保留，停止本轮自己的旧进程后重新启动成功。
