# 部署文档

## 1. Docker Compose 部署

最简单方式：

```bash
cp .env.example .env
# 编辑 .env，填入真实 secrets
docker compose up -d --build
```

服务：

- `tabmail-migrate`
- `tabmail`
- `postgres`
- `redis`

默认端口：

- HTTP: `8080`
- SMTP: `2525`

注意：

- 默认 compose **不暴露 PostgreSQL / Redis 到宿主机**
- 没有真实 secrets 时，Compose 会直接拒绝启动
- 如需生产部署，优先使用 `docker-compose.prod.yml`

查看状态：

```bash
docker compose ps
docker compose logs -f tabmail
docker compose logs -f tabmail-migrate
```

## 2. 初始化数据库

项目现在使用 `tabmail-migrate` 工具管理 schema。

执行最新 migration：

```bash
make migrate
```

查看状态：

```bash
make migrate-status
```

回滚最近一步：

```bash
make migrate-down STEPS=1
```

也可以直接调用二进制：

```bash
go run ./cmd/tabmail-migrate status
go run ./cmd/tabmail-migrate up
go run ./cmd/tabmail-migrate down -steps 1
```

## 3. 关键环境变量

### 必填

```bash
export TABMAIL_ADMINKEY='change-this-admin-key'
export TABMAIL_MAILBOX_TOKEN_SECRET='change-this-mailbox-token-secret'
export POSTGRES_USER='tabmail'
export POSTGRES_PASSWORD='change-this-postgres-password'
export POSTGRES_DB='tabmail'
export TABMAIL_REDIS_PASSWORD='change-this-redis-password'
export TABMAIL_AUTOCREATE_ROUTE_RPM='60'
export TABMAIL_AUTOCREATE_TENANT_RPM='300'
```

### 常用

```bash
export TABMAIL_DB_DSN='postgres://tabmail:tabmail@127.0.0.1:5432/tabmail?sslmode=disable'
export TABMAIL_REDIS_ADDR='redis:6379'
export TABMAIL_OBJECTSTORE='fs'
export TABMAIL_DATADIR='/data'
export TABMAIL_HTTP_ADDR='0.0.0.0:8080'
export TABMAIL_HTTP_ALLOWED_ORIGINS='http://127.0.0.1:3000,http://localhost:3000'
export TABMAIL_HTTP_ALLOWED_HEADERS='Authorization,Content-Type,X-API-Key,X-Admin-Key,X-Tenant-ID'
export TABMAIL_HTTP_TRUSTED_PROXIES='127.0.0.1/32,::1/128'
export TABMAIL_SMTP_ADDR='0.0.0.0:2525'
export TABMAIL_SMTP_DOMAIN='mail.example.com'
export TABMAIL_SMTP_TLSENABLED='false'
export TABMAIL_SMTP_TLSCERT='/etc/ssl/tabmail.crt'
export TABMAIL_SMTP_TLSKEY='/etc/ssl/tabmail.key'
export TABMAIL_SMTP_FORCETLS='false'
export TABMAIL_MAILBOXNAMING='full'
export TABMAIL_STRIPPLUSTAG='true'
export TABMAIL_MONITORHISTORY='50'
export TABMAIL_WEBHOOK_URLS='https://example.com/tabmail-hook'
export TABMAIL_WEBHOOK_SECRET='change-me'
export TABMAIL_WEBHOOK_MAXRETRIES='3'
export TABMAIL_WEBHOOK_RETRYDELAY='1s'
export TABMAIL_WEBHOOK_DEADLIMIT='100'
export TABMAIL_WEBHOOK_POLL_INTERVAL='1s'
export TABMAIL_WEBHOOK_BATCH_SIZE='100'
export TABMAIL_INGEST_DURABLE='true'
export TABMAIL_INGEST_POLL_INTERVAL='1s'
export TABMAIL_INGEST_BATCH_SIZE='100'
export TABMAIL_INGEST_MAX_RETRIES='5'
export TABMAIL_ROLE='all'
```

### S3 / MinIO 对象存储（可选）

多实例部署建议改用 S3 / MinIO 兼容对象存储：

```bash
export TABMAIL_OBJECTSTORE='s3'
export TABMAIL_S3_ENDPOINT='minio:9000'
export TABMAIL_S3_REGION='us-east-1'
export TABMAIL_S3_BUCKET='tabmail'
export TABMAIL_S3_ACCESS_KEY='minioadmin'
export TABMAIL_S3_SECRET_KEY='change-this-s3-secret'
export TABMAIL_S3_USE_TLS='false'
export TABMAIL_S3_FORCE_PATH_STYLE='true'
```

说明：

- 启动时会检查 bucket 是否存在
- 当前不会自动创建 bucket
- AWS S3 与 MinIO 都可使用同一套配置

## 3.1 生产推荐 Compose

生产建议：

```bash
cp .env.example .env
# 编辑 .env
docker compose -f docker-compose.prod.yml up -d --build
```

生产 Compose 默认会拆成：

- `tabmail-migrate`
- `tabmail-api`
- `tabmail-smtp`
- `tabmail-worker`
- `tabmail-retention`

优势：

- API / SMTP / Worker 可独立伸缩
- migration 与应用启动顺序解耦
- PostgreSQL / Redis 不对宿主机暴露端口
- 没有真实 secrets 时无法启动
- 便于后续迁移到外部对象存储

## 4. 手工运行

```bash
go run ./cmd/tabmail
```

或：

```bash
make build
./bin/tabmail
```

迁移工具：

```bash
make build-migrate
./bin/tabmail-migrate status
```

## 5. 生产部署建议

### HTTP

建议前面放：

- Nginx
- Caddy
- Traefik

并由反代处理：

- TLS
- gzip
- 访问日志
- 真实 IP 透传

### SMTP

确保你的 MX 指向 `TABMAIL_SMTP_DOMAIN` 对应的主机。  
域名验证接口会将 `TABMAIL_SMTP_DOMAIN` 作为期望 MX 主机名。

### STARTTLS / ForceTLS

#### STARTTLS

```bash
export TABMAIL_SMTP_TLSENABLED='true'
export TABMAIL_SMTP_TLSCERT='/etc/ssl/tabmail.crt'
export TABMAIL_SMTP_TLSKEY='/etc/ssl/tabmail.key'
export TABMAIL_SMTP_FORCETLS='false'
```

#### Implicit TLS

```bash
export TABMAIL_SMTP_TLSENABLED='true'
export TABMAIL_SMTP_FORCETLS='true'
```

说明：

- `TLSEnabled=true` 且证书可用时，SMTP 会 advertise `STARTTLS`
- `ForceTLS=true` 时，连接建立即进入 TLS

### 反代真实 IP

建议设置：

- `X-Real-IP`
- `X-Forwarded-For`

TabMail 仅会在 `TABMAIL_HTTP_TRUSTED_PROXIES` 命中的代理来源上信任这些头。

## 5.1 监控栈

仓库提供示例监控编排：

```bash
docker compose -f docker-compose.monitoring.yml up -d
```

包含：

- Prometheus
- Alertmanager
- Grafana

Grafana 默认加载 `deploy/monitoring/grafana/dashboards/tabmail-overview.json`。
Alertmanager 默认使用示例 webhook，需要按你的告警通道改成真实 receiver。

## 6. 域名接入步骤

1. 管理员创建 tenant / API key
2. 使用 tenant `X-API-Key` 绑定域名
3. 按返回结果配置 TXT record
4. 将 MX 指向 `TABMAIL_SMTP_DOMAIN`
5. 调用：

```bash
POST /api/v1/domains/{id}/verify
```

6. 再查看：

```bash
GET /api/v1/domains/{id}/verification-status
```

### 路由建议

- 单层子域：`wildcard`，如 `*.mail.example.com`
- 多层子域：`deep_wildcard`，如 `**.mail.example.com`
- 有限编号批量地址：`sequence`，如 `box-{n}.mail.example.com`

## 7. 数据与清理

### PostgreSQL

存：

- tenant
- domain
- route
- mailbox
- message metadata
- audit log
- persisted monitor history
- smtp policy

### 文件对象存储

存：

- 原始 `.eml`

当前版本已支持：

- 同一封原始邮件单份落盘
- 按内容 SHA-256 做跨 SMTP 会话去重
- 基于引用计数的对象删除保护

### 自动清理

由 retention scanner 周期执行：

- 删除过期 message metadata
- 删除对应 raw `.eml`

## 8. API 文档

启动后可访问：

- `http://127.0.0.1:8080/openapi.yaml`
- `http://127.0.0.1:8080/docs`
- `http://127.0.0.1:8080/redoc`
- `http://127.0.0.1:8080/metrics`
- `http://127.0.0.1:8080/api/v1/admin/stats`
- `http://127.0.0.1:8080/api/v1/admin/status`
- `http://127.0.0.1:8080/api/v1/admin/monitor/events`
- `http://127.0.0.1:8080/api/v1/admin/monitor/history`
- `http://127.0.0.1:8080/api/v1/admin/policy`

## 9. Monitor / Policy

### 全局 Monitor

- SSE：`/api/v1/admin/monitor/events`
- 历史分页：`/api/v1/admin/monitor/history`

历史会同时使用：

- 内存 ring buffer：新连接快速回放
- PostgreSQL `monitor_events`：分页/筛选/持久化

### SMTP Policy

当前支持：

- reject origin domains
- recipient accept / reject policy
- store / discard policy

并可通过 admin 页面或 API 动态更新，无需重启。

## 10. 排错

更完整的运维说明见：

- `docs/operations.md`

### 健康检查

```bash
curl http://127.0.0.1:8080/health
```

### 查看日志

```bash
docker compose logs -f tabmail
```

### 查看 SMTP 端口

```bash
nc -vz 127.0.0.1 2525
```

### 查看数据库连接

```bash
psql "$TABMAIL_DB_DSN" -c '\dt'
```
