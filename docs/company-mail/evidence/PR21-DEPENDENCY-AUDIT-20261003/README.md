# PR21 固定 head 前端依赖审计与兼容升级

基线：`41b015c30c66b3ba58a3c1395e8559ebcd27a65f`。独立分支：`audit-pr21-41b015c`。采集日期：2026-10-03 UTC。Node v24.19.0、npm 11.9.0；CI 配置为 Node 22，本地结果不替代该 CI。只修改 web/package.json、web/package-lock.json 和本专项证据目录；不修改业务、browser、scanner、source attestation、CI 或审计门。

用户提供的 run 37145582728 在 npm ci 报告 13 项（1 moderate、11 high、1 critical），随后 catalog 失败，audit gate 未运行。这是历史起始告警，不能等同于 13 个可利用生产漏洞。本次 `gh api repos/jyqj/tabmail/actions/runs/37145582728` 返回 Forbidden，已停止 GitHub API 操作，未下载或独立验证历史 run 日志。当前官方 registry 的完整机器报告在同一原始锁上返回 **26 项：4 moderate、21 high、1 critical**；其中多数是沿依赖链传播的包级记录，公告数据库会变化，不能与历史输出混用。

## 最小兼容升级

全部使用 `https://registry.npmjs.org`，不换源、不执行 audit fix --force、不引入 override、不升级 major、不删门禁。锁定变更只涉及下表包及 Next 配套 env/SWC/plugin。其余锁定版本保持不变。

| 包 | 初始实际版本 | 最终实际版本 | 官方修复边界 / 约束 |
|---|---|---|---|
| next（runtime direct） | 16.3.3 | **16.3.6** | [Node ImageResponse RCE 公告](https://github.com/advisories/GHSA-vcvr-r3jv-pc5j)：16.2.0 ≤ v < 16.3.6；修复 16.3.6 |
| eslint-config-next（dev direct） | 16.3.3 | **16.3.6** | 和 Next patch 配套；仍传播 braces 未修复项 |
| brace-expansion（prod/dev transitive） | 1.1.18、5.0.9 | **1.1.21、5.0.12** | [公告](https://github.com/advisories/GHSA-q2hr-2g5m-vwhr)：修复 1.1.21 / 5.0.12，满足现有 ^1.1.7 / ^5.0.2 |
| fast-uri（prod-declared transitive） | 3.1.6 | **3.1.8** | 三个公告中最晚修复边界 3.1.8，满足 ajv 的 ^3.0.1 |
| ip-address（prod-declared transitive） | 10.5.0 | **10.7.3** | 当前公告受影响至 10.7.0；10.7.1 起修复，现有 ^10.2.0 内更新 |
| undici（dev transitive） | 7.29.0 | **7.30.0** | 当前公告修复边界 7.29.1；现有 jsdom 的 ^7.24.5 内更新 |

执行命令（在 web/，每条附 `--cache=/tmp/tabmail-npm-cache --registry=https://registry.npmjs.org`）：

```sh
npm install --package-lock-only --save-exact next@16.3.6
npm install --package-lock-only --save-dev --save-exact eslint-config-next@16.3.6
npm update brace-expansion fast-uri ip-address undici --package-lock-only
npm ci
npm audit --json
npm audit --omit=dev --json
npx --no-install tsc --noEmit
npm test
npm run lint
npm run build
```

初次未指定 cache 的 npm ci 因 `/home/agent/.npm/_cacache` 不存在而 ENOENT，产生重试告警；后改为允许写入的 /tmp cache 成功，非权限拒绝绕过。`initial-install-diagnostic.txt` 保留失败尾部。还曾在仓库根误运行 audit 得到 ENOLOCK，已在 web/ 重新采集有效完整报告；不把失败报告当作 clean。

## 分类、依赖链与实际风险

[inventory-table.md](inventory-table.md) 列出全部 26 个起始包级条目的实际前后版本、prod/dev、direct/transitive、自身公告或传播项。`classified-inventory.json` 包含每个安装节点的版本、完整根依赖链、公告范围与 URL；`baseline-chains.json` 是原 head 安装后的 npm explain 原始输出。prod-declared 表示 npm dependency 分类，**并不证明代码在生产请求路径执行**。

核心依赖链：

- web → next：runtime direct。RCE 前提是 Node `next/og` ImageResponse 对攻击者控制的 SVG 内容、属性或样式生成图像。`rg -n 'ImageResponse|next/og|@vercel/og' app components features lib` 未发现使用；这是源码范围内的可达性观察，不是对已部署服务的证明。官方缓解是避免传入这些攻击者控制值，现已采用 patch 修复。
- web → shadcn@4.1.0 → ts-morph@26.0.0 → @ts-morph/common@0.27.0 → minimatch@10.2.4 → brace-expansion@5.0.9，以及 dev 的 eslint-config-next → typescript-eslint → typescript-estree → minimatch；另有 dev minimatch@3.1.5 → brace-expansion@1.1.18。全部 brace-expansion 节点已 patch 修复。
- web → shadcn → @modelcontextprotocol/sdk@1.27.1 → ajv@8.18.0 / ajv-formats@3.0.1 → ajv → fast-uri，以及 SDK → express-rate-limit@8.6.2 → ip-address。都是声明 prod 的 CLI/MCP 工具依赖；不是本项目 Go API 的 HTTP 库。实际公开调用、SSRF 信任判断或 URI 序列化风险需要对应工具处理不可信数据的入口，不能从 npm 分类直接推断。
- web → jsdom@29.0.1 → undici：dev 测试依赖，已兼容升级。测试工具的恶意网络响应风险与生产 Next 请求路径不同。

**最终未解决：8 high 包记录，0 moderate / 0 critical；omit-dev 为 6 high。** 全部根因是 [braces 深度嵌套模式导致栈耗尽 DoS](https://github.com/advisories/GHSA-vfj7-8cjw-p6xm)，受影响 `<=3.0.3`，官方 registry 已发布版本最高仍为 3.0.3，无可安装修复。保留的 8 项为 braces、micromatch、fast-glob、@ts-morph/common、ts-morph、shadcn，以及 dev 的 @next/eslint-plugin-next、eslint-config-next。它们并非 8 个独立漏洞。

链路：web → shadcn → fast-glob（或 ts-morph → @ts-morph/common → fast-glob）→ micromatch@4.0.8 → braces@3.0.3；dev：web → eslint-config-next → @next/eslint-plugin-next → fast-glob → 同一 micromatch / braces。

项目唯一直接使用 shadcn 的位置为 `app/globals.css` 中静态 `@import "shadcn/tailwind.css"`。本次 standalone 生产构建中 `find .next/standalone -type d` 未发现 shadcn/braces/micromatch/fast-glob/ts-morph/undici 目录；这是当前产物未追踪这些工具包的证据，不保证其他打包部署方式同样不包含。`npm ci --omit=dev` 仍会安装 shadcn 工具依赖，所以 omit-dev audit 仍报 6 项，不能称 production audit clean。

缓解：在开发/CI 工具执行时限制不可信 glob / registry / 项目配置输入，避免运行不可信 shadcn CLI/MCP；本次已验证 standalone 产物未带上述工具目录。若采用完整 node_modules 部署，要保留该差异。长期处理需等待 braces 官方修复；或由负责人评估工具依赖拆分/替代及 CSS 引入方式（会跨业务或架构范围）。audit 建议 eslint-config-next@14.2.35、shadcn@1.0.0，是 major 降级并改变工具架构，不执行；ts-morph 的 fixAvailable=true 也不代表现有整条链有兼容干净解。完整 clean audit gate **仍将失败**。

## 验证与阻塞

| 验证 | 退出码 / 结果 | 证据 |
|---|---|---|
| 原锁 npm ci（显式临时 cache） | 0 | baseline-ci.log |
| 新锁 npm ci | 0；760 packages | final-ci.log |
| 完整 npm audit --json | 1；8 high | after-audit.json |
| npm audit --omit=dev --json | 1；6 high | after-prod-audit.json |
| TypeScript | 0 | tsc.log |
| npm test 首次 | 1；502 passed、3 skipped，系统 /usr/bin/go 为其他命令 | test.log |
| npm test 使用已有 Go 1.25.7 后 | 0；38 suites、505 tests 全通过 | test-go.log |
| lint 新锁 | 1；9 errors、4 warnings | lint.log |
| lint 原 head 对照 | 1；完全相同 9 errors、4 warnings | baseline-lint.log |
| production build | 0；Next 16.3.6 optimized build 成功 | build.log |

完整测试重跑命令：

```sh
PATH=/workspace/tabmail-cloud/tools/go/bin:$PATH \
GOCACHE=/workspace/tabmail-cloud/gocache \
GOMODCACHE=/workspace/tabmail-cloud/gomod \
GOPATH=/tmp/tabmail-audit-gopath npm test
```

没有 skip/exclude 新增、没有伪造协议 fixture。既有 Vite configLoader native / CJS 配置告警保留。lint 错误位于 lib/wire-response-input.test.ts（empty object type）、profile-refresh-browser/run.cjs 与 review-independent/run.cjs（require imports）；4 warnings 位于两个业务组件和两个 browser entry。原 head 使用独立 /tmp detached worktree + 原锁 npm ci 复现，对照只存在路径差别。由对应任务负责人处理，不在本分支越界修复。

读取了 web/AGENTS.md 及安装包 next/dist/docs/01-app/03-api-reference/06-cli/next.md；仓库无 .agents/skills，/workspace/.agents 为空。未执行 catalog/scanner/source-attestation/browser/生产服务、邮件、凭据、注册发布、merge 或 deploy。
