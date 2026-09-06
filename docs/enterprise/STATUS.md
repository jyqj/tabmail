# 实施状态与交接

分支：`feat/enterprise-mail-governance`；跟踪 Issue #4；Draft PR #5。
起点：远端 `91f61e0e5d3dadbc29f6f18ff6127be846182aef`，之前仅有 CI/测试工具准备，无企业化业务实现。
本记录描述当前源代码检查点；最终提交与远端 CI 证据以 PR Conversation 中的验收记录为准。

## 本轮已实现

- [x] 冻结的版本迁移、启动串行化、校验和校验、历史 grant 表保护、拒绝未知高版本。
- [x] 显式单主域名公司启用；已有账号降为只读、撤销旧平台权限；公开入口与 API Key 收紧；旧邮件保留保护。
- [x] 同公司员工开通、原子激活/重新邀请、负责人保护、管理员授权限制、停用与配额。
- [x] 逐邮箱 read/organize/send/template_only；列表先过滤再分页；共享邮箱多人授权。
- [x] 精确 From 授权；原 /send、模板发送、retry、Worker 不绕过当前公司权限。
- [x] 标量模板、不可变版本、发布/退役、授权范围、服务端预览/变量渲染、展开内存上限。
- [x] 发送任务/配额/来源凭据/内容哈希/审计原子写入；JSONB 规范化哈希避免读回误判。
- [x] 管理员正常读信与例外查阅分开；应急正文/源码读取审计失败则拒绝；跨租户管理员读取被拒。
- [x] SSE 推送前重新授权；修复 Publish/Unsubscribe 通道关闭竞态。
- [x] SMTP DATA 已接受后不因 QUIT 断线重试（不宣称整体投递 exactly-once）。
- [x] `/mail` 员工工作台、`/company` 治理页、`/activate` 激活页、首页跳转、旧凭据清理。
- [x] 去掉构建阶段 Google Fonts 网络依赖，改用系统字体栈；没有把字体文件加入仓库。
- [x] 新增公司 OpenAPI 契约、删除早已不存在的六个旧 grants 路由文档；AST 路由/契约对照测试。

## 验证方式

```bash
# 仅指向隔离测试 PostgreSQL；测试账号需要 CREATEDB。
# 每次创建随机测试数据库，结束后仅删除该测试库，不清空 DSN 指向的数据库。
export TABMAIL_TEST_DB_DSN='postgres://test-user:test-password@127.0.0.1:5432/tabmail_test?sslmode=disable'
go test -race -count=1 ./...
go vet ./...
go build ./cmd/tabmail
cd web
npm ci
npm run lint
npm test -- --run
npx tsc --noEmit
npm run build
```

未设置 `TABMAIL_TEST_DB_DSN` 时真实数据库测试会明确 skip，不能把只跑 FakeStore 当作真实集成测试通过。
本轮本地已执行真实 PostgreSQL 16 测试、Go 全量竞态测试和 vet；前端 18 个测试文件 65 项通过、TypeScript 通过、生产构建通过。
ESLint 0 errors、10 个原有 legacy 页面 warnings；未扩大范围批量重写旧页面。
最终推送前及远端自动化还应在最终业务树重复验证，不沿用准备工具提交的绿灯。

## 回归覆盖

真实 PostgreSQL + 完整 HTTP Router：公司启用、旧公开邮箱封闭、历史及新邮件保护、同域名员工隔离、跨租户管理员隔离、精确代发、旧 API/管理绕过、受限员工原始正文绕过、模板发布与退役、系统变量保护、JSONB 哈希重载、内容篡改、应急查阅审计失败、授权审计回滚、并发配额、实时撤权、停用、负责人/管理员保护、重新发码与并发单次激活、迁移重启和校验和拒绝。

纯单元：模板语法/转义/大小边界、read 与 send 独立、任务摘要、SMTP QUIT 断线、并发 realtime 通道操作。
前端：模板与自由发送 payload 互斥、授权身份下拉、受限员工不能自由写正文、普通员工正常写信、登录后进入工作台及原有回归。

## 下一轮优先级（尚未完成，勿标记为已交付）

1. 每收件人 ingest/outbound 结果持久化与幂等恢复：修复旧传输链路部分失败/重试造成的漏投和重投，加入真实故障注入。它是生产上线门槛。
2. 部门及主管管理范围、员工交接、负责人恢复；随后实现内容快照绑定的审批流程。
3. 草稿、回复/转发、附件、已发送邮件视图、回收站与保留策略；持久化应用事件驱动跨进程实时通知。
4. 会话 Cookie/SSO/MFA、密码重置、服务账号、审计检索/导出、备份恢复与投递验收。

公司管理员界面目前以中文为主，新工作台的双语词条整合仍待补齐。
禁止把本轮称为“所有企业功能完成”或直接标为生产就绪；PR 保留 Draft，主分支及部署环境不变。
