# TabMail

TabMail 是一个面向多租户的域名邮箱接收服务，提供：

- SMTP 收件
- STARTTLS / ForceTLS
- HTTP API
- 系统状态 / 指标
- 全局实时 Monitor
- 域名绑定与验证
- 子域路由规则
- 无鉴权 public 访问
- API Key 租户访问
- mailbox token 访问
- 自动清理与审计日志
- 收件事件 webhook
- SMTP policy 可视化管理

## 当前能力

- 多租户：`plans / tenants / tenant_overrides`
- 域名模型：`domain_zones / domain_routes`
- 邮箱模型：`mailboxes`
- 命名策略：`full / local / domain` + `+tag strip`
- SMTP policy：
  - 发件域拒绝
  - recipient accept / reject
  - store / discard
- 邮件存储：
  - PostgreSQL：元数据
  - 文件对象存储：原始 `.eml`
  - Redis：限流 / 日配额计数
- API 文档：
  - `/openapi.yaml`
  - `/docs`
  - `/redoc`
  - `/api/v1/admin/stats`
  - `/api/v1/admin/status`
  - `/api/v1/admin/monitor/events`
  - `/api/v1/admin/monitor/history`
  - `/api/v1/admin/policy`

## 运行要求

- Go 1.25+
- PostgreSQL 14+
- Redis 7+

或直接使用 Docker Compose。

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

## 重要环境变量

| 变量 | 说明 |
|---|---|
| `TABMAIL_ADMINKEY` | 超级管理员 `X-Admin-Key` |
| `TABMAIL_DB_DSN` | PostgreSQL DSN |
| `TABMAIL_REDIS_ADDR` | Redis 地址 |
| `TABMAIL_DATADIR` | 原始邮件存储目录 |
| `TABMAIL_SMTP_ADDR` | SMTP 监听地址 |
| `TABMAIL_SMTP_DOMAIN` | SMTP banner / 期望 MX 主机名 |
| `TABMAIL_SMTP_TLSENABLED` | 是否启用 STARTTLS |
| `TABMAIL_SMTP_TLSCERT` | TLS 证书路径 |
| `TABMAIL_SMTP_TLSKEY` | TLS 私钥路径 |
| `TABMAIL_SMTP_FORCETLS` | 是否启用 implicit TLS |
| `TABMAIL_MAILBOXNAMING` | `full/local/domain` |
| `TABMAIL_STRIPPLUSTAG` | 是否剥离 `+tag` |
| `TABMAIL_MONITORHISTORY` | monitor 历史缓冲条数 |
| `TABMAIL_HTTP_ADDR` | HTTP 监听地址 |
| `TABMAIL_WEBHOOK_URLS` | webhook 地址列表 |
| `TABMAIL_WEBHOOK_SECRET` | webhook 签名密钥 |

更多部署说明见：

- `docs/deployment.md`
- `docs/api-examples.md`
- `docs/operations.md`

## 鉴权模型

### 1. Public

不带任何 key：

- 自动归属 public tenant
- 只能访问 public mailbox
- 受更严格 IP 限流

### 2. Tenant API Key

请求头：

```http
X-API-Key: <tenant-api-key>
```

用于租户级域名、route、mailbox 管理，以及同租户受限 mailbox 访问。

### 3. Admin

请求头：

```http
X-Admin-Key: <admin-key>
```

可选：

```http
X-Tenant-ID: <tenant-id>
```

用于以某个 tenant 身份调用 tenant-scoped 接口。

### 4. Mailbox Token

对 `access_mode=token` 的邮箱，可通过：

```bash
POST /api/v1/token
```

换取 mailbox token，后续使用：

```http
Authorization: Bearer <mailbox-token>
```

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

完整 curl 示例见 `docs/api-examples.md`。

## 测试

```bash
make test
go vet ./...
```

当前已包含基础测试覆盖：

- router 鉴权
- mailbox token 流程
- 域名验证 handler
- SMTP → mailbox → message 基础生命周期
