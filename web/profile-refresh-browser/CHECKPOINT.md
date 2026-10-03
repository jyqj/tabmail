# 独立单缺陷修复：profile refresh 三方 rebase

基线 integration/company-mail-r5-20261003 @ eb366eecb6413f5aa025afb4ee63bd1eea7fdf7d；其组合被测 source 4016720586be817e5f1edcf0e45ef7d0cccc605a。集成 profile component SHA256 849ffaa1d4d0475a56d331f91d616500e30da17b817a72e58554525af70a55d1，与 frontend 7a43654 相同。独立 review 08ea0791d3694fa0f85ae415c76eb7aa78170c99 的原失败证据未修改。

## 实现

将 observed profile/revision、baseline 和 draft 放入单个 React editor state。有效 session+operation 的刷新在 state updater 内再次确认归属，用旧 baseline→最新 draft 的 wire 差分识别用户已改字段；以 fresh snapshot 的表单作为新 baseline 和初始 draft，逐字段叠加用户已改值。一并提交 profile/revision、base、form；仍需人工确认，未自动重放。失败/缺失/非法/越租户/旧 session 结果不推进 editor。function updater 保留 GET 等待期间的新编辑。

用户 10→15、远端20，刷新后 baseline20/draft15；确认后主动10会提交 quota10。description-only 时未改 can_send 承接远端 false，补丁仍只有 description。false/0/[] 通过键存在性差分保留；NULL zones 映射成原有 [] UI 语义，不新增 NULL 清空/继承命令，未改仍省略并保留存储 NULL。

## 真实检查

- before-results：原 integration 组件，自有 Chromium synthetic fixture，24 passed / 1 failed，exit1；用例6 undefined !== 10。失败 JSON/截图/terminal 单独保留。
- results：相同25条独立 browser cases + 3条新增，Chromium 151.0.7922.173，28 passed / 0 failed，exit0。包括连续两次不同 revision 409/rebase、失败刷新不推进 baseline、刷新途中新编辑与未改撤权承接。
- 原 profile consumer + 新 quota 回原值测试：29 passed / 0 failed，exit0（jsdom 支持性回归）。
- tsc --noEmit exit0；eslint 两个 source/test 文件 exit0，0 errors / 1条既存 cleanup ref warning；node --check runner 与 git diff --check exit0。
- 已读 web/AGENTS.md、安装版 Next16.3.3 use-client 和 testing/vitest 文档。原 package/lock 与依赖未改。Playwright1.58.2 独立安装于 /tmp/profile-refresh-tools，未入 repo dependency。

浏览器覆盖 description-only 权限撤销、dirty false/0/[]、NULL、连续409、500/409刷新失败、401/403读写锁定与同账号重登录、A→B、关闭重开、跨account/tenant/session/relogin的延迟GET、延迟PATCH、SSE失效、非法或消失目标。不访问真实账号、邮件、外部SMTP或生产DB。真实组件经 Vite 加载，Base UI/locale/SWR/API serializer/session/event consumer 为真实实现，auth/header 与 fetch 为 synthetic。**不是 shipping Next/PG/durable 验收。**

## 叶项映射（不关闭）

| 原叶项 | 本块证据 | 仍保留的边界 |
|---|---|---|
| P1-090 | session/operation 归属、tenant/account/relogin/延迟响应 | 完整原始权限加载与 shipping/PG 归属验收未替代 |
| P1-100 | 仅dirty字段PATCH；false/0/[]；NULL不制造新命令 | 用户override继承全scope未替代 |
| P1-110 | profile409手动刷新确认、三方rebase、连续冲突无重放 | 删除影响及完整shipping旅程未替代 |

中央 TODO 未改；父10/171及既有失败/unknown不关闭。不merge/deploy。

## 复跑

```sh
cd web
./node_modules/.bin/vite --config profile-refresh-browser/vite.config.mjs
# 另一终端（repo根目录），复用 /tmp 独立工具：
NODE_PATH=/tmp/profile-refresh-tools/node_modules node web/profile-refresh-browser/run.cjs
# web目录：
npm test -- --reporter=dot features/company/profile-management.consumer.test.tsx
./node_modules/.bin/tsc --noEmit
./node_modules/.bin/eslint features/company/profile-management.tsx features/company/profile-management.consumer.test.tsx
```

源文件精确 SHA256 与检查终态见 results/source-manifest.json。PR依赖明确 integration/company-mail-r5-20261003；新分支 fix/profile-refresh-three-way-20261003。
