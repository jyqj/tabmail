# TabMail

TabMail 是一个**面向多租户、自托管、API 优先**的域名邮箱接收服务。

它提供：

- SMTP 收件
- 多租户套餐 / 限额 / API Key
- 域名绑定与 DNS 验证
- 路由规则：`exact / wildcard / deep_wildcard / sequence`
- 自动创建 mailbox
- public / mailbox token / tenant API key / admin 四层访问模型
- Web 收件箱与管理台
- SSE 实时 monitor
- SMTP policy
- retention / 自动清理
- webhook 事件投递
- OpenAPI / Swagger / ReDoc

---

## 适用场景

适合：

- 自托管临时邮箱
- 验证码 / 回执 / 注册邮件接收
- 内部测试环境
- 多租户开发或中小规模生产场景

不建议直接无压测用于：

- 超大规模公网 SaaS
- 强 SLA / 多机强一致场景

---

## 当前核心能力

### 多租户

- `plans`
- `tenants`
- `tenant_overrides`
- `tenant_api_keys`

支持：

- 套餐默认限制
- 租户覆盖限制
- RPM / 日配额
- 管理员代理租户

### 域名与路由

- `domain_zones`
- `domain_routes`

支持的路由类型：

- `exact`：精确命中单个域名/子域
- `wildcard`：单层通配，如 `*.mail.example.com`
- `deep_wildcard`：多层通配，如 `**.mail.example.com`
- `sequence`：序列路由，如 `box-{n}.mail.example.com`

### mailbox / message

- `mailboxes`
- `messages`

支持：

- `access_mode = public / token / api_key`
- mailbox retention override
- mailbox expires_at
- 原始邮件 `.eml` 对象存储

### SMTP / policy / monitor

支持：

- 发件域拒绝
- recipient accept / reject
- store / discard
- admin monitor 实时事件流
- 审计日志

---

## 路由优先级

当前命中顺序已固定，不再依赖创建顺序：

1. `exact`
2. `sequence`
3. `wildcard`
4. `deep_wildcard`

同类型下会进一步按更具体规则优先匹配。

---

## 性能与存储策略

当前版本已经包含这些关键优化：

- Resolver zone / route 短 TTL 缓存
- SMTP 会话内 RCPT → DATA 结果复用
- SMTP policy 共享缓存
- tenant config 会话内缓存
- **同一封原始邮件只存一份**
- **按原始内容 SHA-256 做跨会话去重**
- 对象删除时按引用计数清理，避免共享 `.eml` 被误删

存储分层：

- PostgreSQL：元数据 / 审计 / monitor / policy
- Redis：HTTP 限流与日配额计数
- 文件对象存储：原始 `.eml`

---

## 运行要求

- Go 1.25+
- PostgreSQL 14+
- Redis 7+

或直接使用 Docker Compose。

---

## 快速开始

### 1. Docker Compose

```bash
docker compose up -d --build
```

默认暴露：

- HTTP API: `http://127.0.0.1:8080`
- SMTP: `127.0.0.1:2525`

验证：

```bash
curl http://127.0.0.1:8080/health
curl http://127.0.0.1:8080/openapi.yaml
```

### 2. 本地开发

先启动 PostgreSQL / Redis，再执行：

```bash
make migrate
TABMAIL_ADMINKEY=changeme go run ./cmd/tabmail
```

---

## 关键环境变量

| 变量 | 说明 |
|---|---|
| `TABMAIL_ROLE` | 进程角色：`all / api / smtp / worker / retention` |
| `TABMAIL_ADMINKEY` | 超级管理员 `X-Admin-Key` |
| `TABMAIL_MAILBOX_TOKEN_SECRET` | mailbox bearer token 签名密钥 |
| `TABMAIL_AUTOCREATE_ROUTE_RPM` | 单路由自动建箱 RPM |
| `TABMAIL_AUTOCREATE_TENANT_RPM` | 单租户自动建箱 RPM |
| `TABMAIL_DB_DSN` | PostgreSQL DSN |
| `TABMAIL_REDIS_ADDR` | Redis 地址 |
| `TABMAIL_DATADIR` | 原始邮件存储目录 |
| `TABMAIL_SMTP_ADDR` | SMTP 监听地址 |
| `TABMAIL_SMTP_DOMAIN` | SMTP banner / 期望 MX 主机名 |
| `TABMAIL_SMTP_TLSENABLED` | 是否启用 STARTTLS |
| `TABMAIL_SMTP_TLSCERT` | TLS 证书路径 |
| `TABMAIL_SMTP_TLSKEY` | TLS 私钥路径 |
| `TABMAIL_SMTP_FORCETLS` | 是否启用 implicit TLS |
| `TABMAIL_MAILBOXNAMING` | `full / local / domain` |
| `TABMAIL_STRIPPLUSTAG` | 是否剥离 `+tag` |
| `TABMAIL_MONITORHISTORY` | monitor 历史缓冲条数 |
| `TABMAIL_HTTP_ADDR` | HTTP 监听地址 |
| `TABMAIL_HTTP_ALLOWED_ORIGINS` | 允许的 CORS Origins |
| `TABMAIL_HTTP_ALLOWED_HEADERS` | 允许的 CORS Headers |
| `TABMAIL_HTTP_TRUSTED_PROXIES` | 信任的反向代理 IP/CIDR |
| `TABMAIL_WEBHOOK_URLS` | webhook 地址列表 |
| `TABMAIL_WEBHOOK_SECRET` | webhook 签名密钥 |
| `TABMAIL_WEBHOOK_POLL_INTERVAL` | outbox/webhook worker 轮询间隔 |
| `TABMAIL_WEBHOOK_BATCH_SIZE` | outbox/webhook worker 批处理大小 |
| `TABMAIL_INGEST_DURABLE` | 是否启用 durable ingest |
| `TABMAIL_INGEST_POLL_INTERVAL` | ingest worker 轮询间隔 |
| `TABMAIL_INGEST_BATCH_SIZE` | ingest worker 批处理大小 |
| `TABMAIL_INGEST_MAX_RETRIES` | ingest job 最大重试次数 |

---

## 鉴权模型

### 1. Public

不带任何 key：

- 自动归属 public tenant
- 只能访问 public mailbox
- 使用更严格的 IP 限流

### 2. Tenant API Key

请求头：

```http
X-API-Key: <tenant-api-key>
```

用于：

- 域名 / 路由 / mailbox 管理
- 同租户受保护 mailbox 访问

### 3. Admin

请求头：

```http
X-Admin-Key: <admin-key>
```

可选：

```http
X-Tenant-ID: <tenant-id>
```

用于以管理员身份代理某个 tenant 调用 tenant-scoped 接口。

### 4. Mailbox Token

对 `access_mode=token` 的邮箱，可通过：

```bash
POST /api/v1/token
```

换取 mailbox token，后续使用：

```http
Authorization: Bearer <mailbox-token>
```

---

## 常用接口

- `POST /api/v1/token`
- `GET /api/v1/domains`
- `POST /api/v1/domains`
- `POST /api/v1/domains/{id}/verify`
- `GET /api/v1/domains/{id}/routes`
- `POST /api/v1/mailboxes`
- `GET /api/v1/mailbox/{address}`
- `GET /api/v1/mailbox/{address}/events`
- `GET /api/v1/mailbox/{address}/{id}`
- `GET /api/v1/mailbox/{address}/{id}/source`
- `GET /api/v1/admin/stats`
- `GET /api/v1/admin/monitor/events`
- `GET /api/v1/admin/monitor/history`
- `GET /api/v1/admin/policy`
- `PATCH /api/v1/admin/policy`

更多示例：

- `docs/api-examples.md`

---

## 文档入口

启动后可访问：

- `http://127.0.0.1:8080/openapi.yaml`
- `http://127.0.0.1:8080/docs`
- `http://127.0.0.1:8080/redoc`
- Web docs 页面：`/docs`

更多说明：

- `docs/deployment.md`
- `docs/api-examples.md`
- `docs/operations.md`

---

## 测试

```bash
make test
go vet ./...
```

前端：

```bash
cd web
npm test
npm run build
```

---

## 当前状态

从功能完整度看，TabMail 已经具备：

- 可运行的后端收件主链路
- 可用的 Web 控制台
- 可用的多租户与权限模型
- 可用的 deep wildcard 与对象去重能力

如果你要进一步做大规模生产化，建议继续补：

- 后端压测
- 多机部署一致性策略
- 对象存储从本地文件切换到 S3 / 兼容后端
