# R5 browser reply journey专项附录 — 2026-10-03

## 边界

正常 Git `fetch origin pull/21/head` 返回并核对 `41b015c30c66b3ba58a3c1395e8559ebcd27a65f`；新独立分支 `fix/browser-reply-journey-20261003` 从该提交创建。只改本专项 journey 脚本、Go browser test 和本附录/证据。未改中央 R5-TODO、脚本接口清单、依赖或 source attestation。仓库和工作区没有 `.agents/skills`；已读取 `web/AGENTS.md` 以及已安装 Next 的 output/useRouter 文档。

## 因果结论

用户提供的 CI run `37145582728` / browser job `111268707939`：`TestR3BrowserJourney` 等待 `getByRole('button', {name: /Re: Browser welcome/})`，5 秒失败。本机在固定源码的生产 standalone 前端上重现同一失败（10.96 秒总测试时间）。原 JSONL、DOM、截图和错误保留在 [专项证据](evidence/R5-BROWSER-REPLY-JOURNEY-20261003/original-failure/)。

真实业务链路是：`Compose.send` 先持久化草稿，再原子 submit 返回 UUID；`MailWorkspace.onSent` 关闭编辑器、切换 `folder=receipts` 并刷新 session-scoped SWR 列表。`ReceiptFolder` 按 `Task: UUID` 命名按钮，普通回执不含主题或正文。主题按钮仅出现在独立的 `SentFolder` 邮件资产列表。原测试把回执与资产列表混同，等待超时不能靠延时解决。后续精确名称 `View content` 也已陈旧，当前名称是 `View content (re-authorized)`。

修复先注册刷新响应等待，再触发 send：验证刷新回执确实含新提交 ID、UI 位于 Delivery status、回执不显示主题/正文；打开该 UUID 回执后显式重新鉴权读取内容并校验附件下载字节。之后点击 Sent mail，验证真实资产列表包含同一 UUID 和 `Re: Browser welcome`，保留原主题按钮可见/点击断言，再次读取正文和附件。没有增加超时或删除主题断言。Go 测试还解码 loopback SMTP 收到的 MIME，校验单次发送、主题、RFC 回复头、引用正文、附件名称和字节。

原失败修复后完整 browser 通过；这里的证据支持测试陈旧，未发现这条发送与刷新链路的生产缺陷，因此无生产代码变更。

## 执行环境与精确验证

Go `go1.25.7 linux/amd64`，`GODEBUG=asynctimerchan=0`、`GOFLAGS=-mod=readonly`。保留 `third_party/enmime-v2.3.0` / `third_party/go-smtp` replace 和 schema19。普通官方 npm registry 通过隔离 writable cache 完成 `npm ci --ignore-scripts --no-audit --no-fund` 和 `@playwright/test@1.56.1` 安装；未更新 lockfile。

新建本机 PostgreSQL 17 cluster `/workspace/browser-reply-runtime/pgdata`，仅监听 `127.0.0.1:15432`；测试通过 `testpg.NewPostgres` 创建并清理独立随机数据库。真实 API 仅监听 `127.0.0.1:18080`，SMTP sink 使用随机 loopback 端口，内存对象存储；无生产邮件、数据库、凭据。Next 16.3.3 使用 `INTERNAL_API_URL=http://127.0.0.1:18080 npm run build` 构建 production standalone，通过其 `server.js` 服务，未使用 dev server。

Playwright 管理的 Chromium 1194 下载返回明确 `403 Domain forbidden`（工具自身自动尝试的源记录在 browser-download-denied.log）。未手动换下载源；本机已经安装 `/usr/bin/chromium`，版本 `151.0.7922.173`，使用原脚本已有 `TABMAIL_CHROMIUM_PATH` 接口运行。这个版本差异是本机证据限制；CI 的锁定 Chromium 141 尚未在此运行。

| 验证 | 结果 | 证据 |
| --- | --- | --- |
| 原源码 `go test -json -count=1 -timeout=180s -run '^TestR3BrowserJourney$' ./internal/store/postgres` | exit 1；同一主题按钮 5000 ms 超时，总 10.96 s | original.jsonl / original-failure |
| 只修正 journey 后，同一命令 | exit 0；7.42 s | candidate.jsonl |
| 加强 SMTP MIME 校验后，同一命令 | exit 0；7.26 s，1 test pass，0 skip | final.jsonl |
| `go test -json -count=1 -timeout=180s -run '^(TestR3CompanyHTTPJourney\|TestR3DraftRevisionAndAttachmentAuthorization\|TestR3SubmissionIdempotencyTemplateOnlyAndCodec)$' ./internal/store/postgres`（正则实际使用普通 `|`） | exit 0；3 pass，0 skip | regression.jsonl |
| `npm test -- --reporter=verbose components/company/compose.test.tsx 'app/(dashboard)/mail/page.test.tsx' features/mail/components/receipt-consumer.test.tsx` | exit 0；3 files / 33 tests pass | components.log |
| `INTERNAL_API_URL=http://127.0.0.1:18080 npm run build` | exit 0；production standalone | build.log |
| `npx tsc --noEmit` | exit 0 | tsc.log |
| `node --check scripts/browser_company.cjs`、`git diff --check` | exit 0 | 本机执行 |

未运行：全库 Go/frontend 套件、race、Docker 镜像构建、远端 CI、CI 锁定 Chromium 141。未合并或部署。`gh pr view 21 --json headRefName,baseRefName` 返回 `Post https://api.github.com/graphql: Forbidden`；没有换 API 路径绕过该拒绝。正常 Git `ls-remote` 确认固定提交对应 PR 源分支 `ci/company-mail-r5-wiring-20261003`，作为专项 draft 的目标分支。
