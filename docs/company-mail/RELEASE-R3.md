# R3：公司邮箱业务闭环与一致性修复

审计基线：`20a7aabac8d6825fb6932f13f01562ad2290477b`（PR #11）。本轮沿用既有 users、tenant、mailbox_grants、原件对象、收件恢复账本和 outbound queue。公司业务不是另一套平行引擎。历史路线图的 P0/R2 阶段说明保留；本文件描述本轮交付和运行边界。

## 已落地的审计项

| 审计项 | 实现与验证位置 |
|---|---|
| 过期 JWT 被当成匿名 403、续期不一致 | `middleware/auth.go`；匿名受保护入口 401，已认证无权 403；原有 API 回归用例 |
| 刷新失败误登出、旧响应跨身份写回、多标签页竞争 | `web/lib/api/base.ts`、`session.ts`、`session-safety.test.ts`；Web Locks 串行刷新／登录／退出直至 cookie 和本地状态提交；会话版本和 AbortController；仅凭据失效清登录 |
| 长连接停用、撤权和过期不生效 | `revalidate.go`、`company_stream.go`；每 2 秒使用当前身份／权限；数据库事件和周期 resync；真实 HTTP stream 测试 |
| owner 隐式决定共享留存 | `mailbox_kind=personal/shared/legacy`；共享明确私有、默认永久；旧 ownerless 资源须显式转换，不擅自改全部历史资源 |
| 删除不可恢复、审计和事件未原子提交 | `company_mail.go`；回收站／恢复／归档／未读；事务审计和数据库变更通知；30 天后才清除 |
| 原件读取／解析失败伪装空正文 | 消息 service 返回真实错误，HTML 清理失败单独表述；HTTP 故障测试 |
| 员工无法开通和交接 | 单次哈希邀请、72 小时有效、激活及个人邮箱原子开通；独立 grants；带理由和版本的交接；停用撤销旧访问、个人 keys 和共享授权 |
| 模板只是渲染器，没有治理和 UI | `company/templates.go`、Pg version/grant 存储、管理员编辑器；变量必填／类型／长度／枚举；服务器身份变量；不可变版本及内容摘要；template_only 实际执行 |
| 工作台缺少日常邮件功能 | `/mail`、`/account`；回复／全部回复／转发由服务端 RFC 地址解析；草稿 CAS、附件授权／完整性、搜索、归档、回收站、发件详情 |
| 一个坏收件人拖累同域其他人 | `recipient_delivery.go`、`outbound_recipients.go`；每个收件人独立 SMTP 事务和检查点；真实 loopback SMTP 450/550 用例 |
| 重复提交产生重复任务 | Idempotency-Key 按租户、当前调用者和规范化请求摘要绑定；并发数据库唯一性；重复请求返回相同任务，不同内容同 key 返回冲突 |
| 不确定投递不能安全恢复 | 保留 pre-delivery marker；不自动重放；受审计核对 accepted/temporary/permanent；全部确认接受可直接完成，无网络重发 |
| API 与 worker 进程内 Hub 分裂 | PostgreSQL invalidation log + 2 秒轮询游标 + 25 秒 resync；跨独立 DB 写入和当前 SSE 测试；事件不包含正文和 BCC |
| 入站恢复底层存在但无法操作 | `/company/recovery`；super_admin、选定租户、理由、原件哈希校验、检查版本和固定目标；只重试未完成目标，审计失败回滚 |
| 运行配置和生产健康不透明 | `/admin/runtime-config` 区分启动快照和 DB 设置；公司模式强制安全边界；`/health` 存活、`/ready` 依赖／worker 就绪；数据库队列指标和告警 |
| 备份与恢复未闭环 | `company_snapshot.py`，停止写入后的 DB+原件+哈希 manifest；只允许空目标恢复；真实 PostgreSQL 备份恢复测试；旧破坏性脚本要求额外显式 opt-in |

## 产品与权限边界

第一版公司组织采用三个角色，不增加部门树、审批引擎、账单或订阅体系。邮箱 `read`、`organize`、`send_as` 与模板使用权分别判断；`organize` 需要 `read`，但 `read` 不意味着 `send_as`。所有模板发送仍须获准使用精确 From。个人 API key 继承有效属主权限，无主集成 key 不可冒充公司个人／共享邮箱。

公司管理员可以管理普通员工，但不能操作同级／更高权限账号。平台管理员必须选择公司上下文。受审计的例外检查不等于获得常规正文访问权。运营审计不存 BCC 地址和原始 in-flight marker，逐收件人详情遵守发件正文授权；投递 fencing token 从不序列化。

员工账号停用会使新 HTTP 请求和长连接失效，排队任务在实际发送前重新鉴权。修改密码递增会话版本并撤销所有刷新会话；登录／退出与刷新在浏览器跨标签页锁内串行。已开始且对端已经接受的 SMTP 事务无法被“撤回”。

浏览器必须处于支持 Web Locks 的安全上下文（正式环境 HTTPS；localhost 仅测试）。不支持时认证操作明确报错，不退化到容易触发令牌重放的伪安全锁。401 后仅 GET 和具有幂等键的提交自动重放；其他 POST 要求显式重试。

模板占位符为 `{{.变量名}}`。只允许受声明的标量变量；不执行 Go 模板函数、循环或嵌套模板。`employee_name`、`company_name`、`sender_address` 由服务器设置。发布内容不可变，旧版本可追溯；模板停用／使用授权撤销后待发送任务失败关闭。预览和实际发送共用校验与渲染，任务保存最终内容和来源摘要。

附件限制：每封最多 10 个、合计 20 MiB。草稿版本冲突返回 409，不覆盖较新编辑。上传中／未引用附件默认 7 天清理，但被草稿或发送任务引用的附件受保护。入站附件解析与下载走当前邮件权限，转发附件重新绑定当前编辑者和发送邮箱。元数据搜索是明确的首期能力，并非正文全文检索。

## 升级和首次使用

1. 记录当前 commit、Goose 版本、队列状态和对象存储。停止所有旧 API、SMTP、worker、retention 写入端。不要只停止 Web UI。
2. 联合备份数据库与原件，验证哈希和恢复到隔离空目标。文件系统模式可使用下述脚本；S3 应冻结写入、记录对象版本／快照，协调恢复同一时点，不能只恢复 PostgreSQL。
3. 用隔离数据库演练迁移。新迁移只有 `00005_company_workflows.sql`，已发布 00001–00004 不变。Down 故意拒绝破坏身份、版本和投递证据的回退。
4. 统一重建、升级全部角色。生产 Compose 固定 `COMPANY_ONLY=true`、full 地址命名、durable ingress；旧 DB 的开放注册设置无法绕过该模式。不要混跑旧二进制。
5. 设置真实 HTTPS origin／可信代理／强 secrets／relay 身份及 TLS；默认匿名 RPM=120，可按实际反向代理规模调整。`TABMAIL_OBJECT_STORE` 是标准变量，旧 `TABMAIL_OBJECTSTORE` 只作兼容回退。
6. bootstrap 平台管理员使用至少 12 字节密码；跨角色同时启动仍只创建一个完整身份与租户，不因重启重置现有密码。选择公司租户和已验证主域名；管理员在 `/company` 邀请员工、分配共享授权，然后维护／发布模板。
7. `/ready` 必须成功。配置变更是否需重启见 `/admin/runtime-config`：环境变量及进程构造参数需要协调重启；公司名称、模板、grant 和数据库工作流配置按请求读取。不要把后台保存成功解释为所有启动参数已热更新。

```sh
# 所有写入端已停止后；此确认不代替实际停止服务。
export TABMAIL_WRITERS_STOPPED=yes
export TABMAIL_OBJECT_STORE=fs
export TABMAIL_DATADIR=/path/to/mail-data
# TABMAIL_DB_DSN 指向当前数据库；生产建议使用 pg_service/.pgpass 管理凭据。
python3 scripts/company_snapshot.py backup /secure-backups/company-r3

# 仅在隔离、空数据库和空对象目录上演练。切勿指向当前生产实例。
export TABMAIL_DB_DSN='postgres://USER@HOST/EMPTY_RESTORE_DATABASE'
export TABMAIL_DATADIR=/path/to/empty-restored-mail-data
python3 scripts/company_snapshot.py restore /secure-backups/company-r3
```

脚本拒绝源／目标目录嵌套、符号链接、损坏快照和非空恢复目标。它不停止运行中的写入端，也不自动修改生产连接配置。恢复后应验证身份、邮箱、模板版本、附件引用、队列／幂等证据及原件，并重新启动全套一致版本。

公网入站需要正确的 25 端口映射或上游 MTA；Compose 中的 2525 是应用监听端口，不是公网邮件服务器会自动选择的端口。应用直接接入 STARTTLS 时，使用 `docker-compose.smtp-tls.yml` 并配置 `TABMAIL_SMTP_CERT_DIR` 下 `cert.pem`、`key.pem`；TLS 被启用但证书加载失败将拒绝启动，不回落明文。外置 MTA 的 TLS、防垃圾／反恶意附件策略由运营方验收。

## 投递状态和恢复操作

`POST /send` 的 201 只表示入队。`sent` 和收件人 `accepted` 仅代表下一跳 SMTP 接受；不代表最终进入收件箱。对某个收件人的 550 不阻塞同域其他人，450 仅对临时失败目标重试。前端显示逐收件人状态，但管理员没有常规正文权限时不可看到 BCC。

连接在 DATA 最终确认处断开、进程在外部发送后本地检查点之前死亡等情况保留 `uncertain`。普通重试禁止；平台恢复界面要求理由和检查版本，操作员核实对端日志后再标记接受、临时失败或永久失败。**不知道就保持 uncertain**，不要为清空告警编造证据。幂等键和数据库 fencing 不能把外部 SMTP 变成端到端 exactly-once。

入站已接收原件不会因失败自动清除。恢复中心验证原件大小／SHA256；更名或地址复用不能重定向历史目标；只可恢复当前公司未完成、仍匹配原快照的目标。原件损坏、目标已换人等情况应保留审查，不通过修改目标 ID 绕过保护。

## 验证和证据

仓库的 `Company mail security and release checks` 在 PR 和 main 运行：Go build、真实 PostgreSQL race tests、29 个模型／公司 DTO 字段契约、vet、TypeScript、Vitest、lint、Next production build、Compose 展开和实际 standalone 镜像 API origin 检查；独立 `browser-journey` job 在真实 API、隔离 PG 与 loopback SMTP 上运行 Chromium UI 流程。

```sh
go build ./...
TABMAIL_TEST_DB_DSN='postgres://TEST_USER:TEST_PASSWORD@localhost/TEST_DATABASE?sslmode=disable' \
  go test -race -count=1 -timeout=180s ./...
go vet ./...
python3 scripts/check_contract_drift.py
(cd web && npm ci && npx tsc --noEmit && npm test && npm run lint && npm run build)
```

`TestR3BrowserJourney` 仅在 `TABMAIL_BROWSER_E2E=1` 时运行；CI 独立 job 安装固定版本 Playwright 并设置 API 18080 端口，测试产物只包含合成账号／邮件。普通测试中的该 skip 不等于浏览器已验证，必须同时查看 browser job。备份演练需要匹配的 PostgreSQL client，CI 显式安装 16 版。

本地执行记录：Go build、417 项 Go 测试（`-race`）、vet、29 个字段契约、TypeScript、73 项 Vitest、lint 和 Next 生产构建均通过。浏览器测试在普通 Go 测试中按设计跳过；本地浏览器被运行环境策略阻止访问 localhost，因此浏览器结果以 PR 的独立 CI job 为准，不沿用一次未执行的验证。

本轮不会自动部署、改 DNS、向公网地址发测试邮件或迁移生产数据。生产登录、公网收发、SPF／DKIM／DMARC、实际 relay 凭据与信誉、对象存储容量、HTTPS／TLS、恢复 RPO/RTO 和真实负载仍须在部署环境验收。这些不是可以用本地绿灯代替的事实。
