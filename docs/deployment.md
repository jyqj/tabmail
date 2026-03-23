# 部署文档

## 1. Docker Compose 部署

最简单方式：

```bash
docker compose up -d --build
```

服务：

- `tabmail`
- `postgres`
- `redis`

默认端口：

- HTTP: `8080`
- SMTP: `2525`
- PostgreSQL: `5432`
- Redis: `6379`

查看状态：

```bash
docker compose ps
docker compose logs -f tabmail
```

## 2. 初始化数据库

项目使用 `migrations/*.sql`。

手工执行：

```bash
make migrate
```

等价于按顺序执行：

```bash
for f in migrations/*.sql; do
  psql "$TABMAIL_DB_DSN" -f "$f"
done
```

> 如果你是从旧版本升级，请确保执行到最新 migration。  
> 当前新增了 `004_deep_wildcard_route.sql`，用于支持 `deep_wildcard` 路由类型。

## 3. 关键环境变量

### 必填

```bash
export TABMAIL_ADMINKEY='change-this-admin-key'
```

### 常用

```bash
export TABMAIL_DB_DSN='postgres://tabmail:tabmail@127.0.0.1:5432/tabmail?sslmode=disable'
export TABMAIL_REDIS_ADDR='127.0.0.1:6379'
export TABMAIL_DATADIR='/data'
export TABMAIL_HTTP_ADDR='0.0.0.0:8080'
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
```

## 4. 手工运行

```bash
go run ./cmd/tabmail
```

或：

```bash
make build
./bin/tabmail
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

TabMail 会优先读取 `X-Real-IP`。

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
