# 运维手册

本文面向运维与管理员，覆盖：

- 故障排查
- 监控指标解释
- webhook 调试
- SMTP / Monitor / Policy 常见问题

---

## 1. 快速检查清单

当你发现系统异常时，建议按这个顺序看：

1. 服务是否启动
2. PostgreSQL / Redis 是否正常
3. HTTP 健康检查是否通过
4. SMTP 端口是否可连
5. `/api/v1/admin/stats` 是否可访问
6. `/api/v1/admin/monitor/events` 是否有实时事件
7. webhook 是否出现 dead letter

---

## 2. 基础排查命令

### 2.1 健康检查

```bash
curl http://127.0.0.1:8080/health
```

期望：

```json
{"status":"ok"}
```

### 2.2 查看系统状态

```bash
curl http://127.0.0.1:8080/api/v1/admin/stats \
  -H "X-Admin-Key: <admin-key>"
```

重点关注：

- `metrics.smtp.sessions_active`
- `metrics.smtp.messages_rejected`
- `metrics.webhooks.failed`
- `metrics.webhooks.dead_letter_size`
- `metrics.realtime.subscribers_current`

### 2.3 查看 monitor 历史

```bash
curl "http://127.0.0.1:8080/api/v1/admin/monitor/history?page=1&per_page=20" \
  -H "X-Admin-Key: <admin-key>"
```

### 2.4 实时订阅 monitor

```bash
curl -N "http://127.0.0.1:8080/api/v1/admin/monitor/events" \
  -H "X-Admin-Key: <admin-key>"
```

### 2.5 查看日志

```bash
docker compose logs -f tabmail
```

或本地运行时直接查看进程输出。

### 2.6 查看 Prometheus 指标

```bash
curl http://127.0.0.1:8080/metrics
```

重点关注：

- `tabmail_ingest_backlog`
- `tabmail_webhooks_backlog`
- `tabmail_webhooks_dead_letter_size`
- `tabmail_smtp_messages_rejected_total`
- `tabmail_smtp_deliveries_failed_total`

### 2.7 查看 migration 状态

```bash
make migrate-status
```

或：

```bash
psql "$TABMAIL_DB_DSN" -c "SELECT version, name, applied_at FROM schema_migrations ORDER BY version;"
```

如果应用启动时报 schema version 过旧，先执行：

```bash
make migrate
```

---

## 3. 监控指标解释

## 3.1 SMTP 指标

位于：

- `metrics.smtp`

字段解释：

### `sessions_opened`
累计打开的 SMTP 会话数。

### `sessions_active`
当前活跃 SMTP 会话数。

如果长时间居高不下，可能说明：

- 客户端没有正常断开
- SMTP 连接卡死
- 有异常脚本或攻击流量

### `recipients_accepted`
RCPT TO 阶段被接受的收件人数量。

### `recipients_rejected`
RCPT TO 阶段被拒绝的收件人数量。

升高通常说明：

- 域名未验证
- recipient policy 拒绝
- mailbox/route 不存在
- 命名策略导致地址不合法

### `messages_accepted`
DATA 阶段整体成功的消息数。

### `messages_rejected`
DATA 阶段整体失败的消息数。

升高说明：

- 所有收件人都失败
- store/discard / quota / message size 等策略导致不入库

### `deliveries_succeeded`
最终成功写入 message metadata 的投递次数。

### `deliveries_failed`
最终投递失败次数。

升高说明：

- PostgreSQL 写入失败
- object store 写入失败
- 单条消息在某些收件人分支失败

### `bytes_received`
累计接收的 SMTP 原始字节数。

---

## 3.2 Realtime 指标

位于：

- `metrics.realtime`

### `subscribers_current`
当前 SSE 订阅者数量。

包括：

- mailbox SSE
- admin monitor SSE

### `events_published`
累计广播事件数。

---

## 3.3 Webhook 指标

位于：

- `metrics.webhooks`

### `enabled`
是否启用 webhook。

### `configured`
配置的 webhook URL 数量。

### `queued`
累计排队 webhook 事件数。

### `delivered`
成功投递到 webhook endpoint 的次数。

### `failed`
最终失败次数。

### `retried`
累计重试次数。

### `dead_letter_size`
当前死信队列数量。

如果这个值持续上升，说明 webhook 目标端长期异常。

---

## 3.4 时间序列

Admin Dashboard 中的时间序列图是：

- 分钟级内存采样
- 最近约 60 个点

用途：

- 快速观测最近一小时的收件/失败变化
- 不是长期监控系统替代品

如果你需要长期趋势，建议再接：

- Prometheus
- Loki
- Grafana

## 3.5 基线告警建议

最小告警基线建议：

### 告警 1：ingest backlog 持续堆积

- 指标：`tabmail_ingest_backlog`
- 建议阈值：连续 5 分钟 `> 100`
- 说明：worker 跟不上、对象存储异常、数据库写入异常都会导致 backlog 增长

### 告警 2：webhook backlog 持续堆积

- 指标：`tabmail_webhooks_backlog`
- 建议阈值：连续 5 分钟 `> 100`
- 说明：下游 webhook 服务不可用或 worker 处理速度不足

### 告警 3：dead letter 非 0

- 指标：`tabmail_webhooks_dead_letter_size`
- 建议阈值：`> 0`
- 说明：说明已有事件进入人工处理区，应尽快排查目标 URL / 网络 / 签名错误

### 告警 4：SMTP reject 激增

- 指标：`tabmail_smtp_messages_rejected_total`
- 建议方式：结合 5 分钟增量观察异常跳升
- 说明：常见原因包括域名未验证、配额超限、对象存储异常

### 告警 5：投递失败激增

- 指标：`tabmail_smtp_deliveries_failed_total`
- 建议方式：结合 5 分钟增量观察异常跳升
- 说明：优先排查 PostgreSQL、对象存储、worker 与业务日志

---

## 4. webhook 调试

## 4.1 启用 webhook

```bash
export TABMAIL_WEBHOOK_URLS='https://example.com/tabmail-hook'
export TABMAIL_WEBHOOK_SECRET='change-me'
export TABMAIL_WEBHOOK_MAXRETRIES='3'
export TABMAIL_WEBHOOK_RETRYDELAY='1s'
export TABMAIL_WEBHOOK_DEADLIMIT='100'
```

## 4.2 请求头

TabMail 会发送：

- `Content-Type: application/json`
- `X-TabMail-Event`
- `X-TabMail-Attempt`
- `X-TabMail-Signature`（如果配置了 secret）

## 4.3 调试接收端

可以先用一个测试服务接收：

```bash
python3 -m http.server 9009
```

或者用自定义 echo 服务 / webhook.site。

## 4.4 为什么会进入 dead letter

进入死信通常有几类原因：

1. 目标 URL 不通
2. TLS / 证书错误
3. 对方超时
4. 返回非 2xx
5. DNS 解析失败

## 4.5 如何判断是临时失败还是永久失败

看这些：

- `metrics.webhooks.retried`
- `metrics.webhooks.failed`
- `dead_letter_size`
- admin 页面 Dead-letter queue

如果只偶发 retry，但 delivered 仍增长，通常是短暂波动。  
如果 failed 和 dead letter 持续增长，说明目标端稳定性有问题。

## 5. 备份与恢复

### 5.1 PostgreSQL 备份

```bash
make backup-db
```

默认输出到：

- `backups/postgres-<timestamp>.dump`

也可以指定文件名：

```bash
TABMAIL_DB_DSN='postgres://...' ./scripts/backup_postgres.sh backups/manual.dump
```

### 5.2 PostgreSQL 恢复

```bash
TABMAIL_DB_DSN='postgres://...' ./scripts/restore_postgres.sh backups/manual.dump
```

说明：

- 恢复会执行 `pg_restore --clean --if-exists`
- 恢复前请确保目标实例可接受覆盖
- 恢复后建议立即执行 `make migrate-status`

### 5.3 文件对象存储备份

仅适用于 `TABMAIL_OBJECTSTORE=fs`：

```bash
make backup-obj
```

默认输出到：

- `backups/objectstore-<timestamp>.tar.gz`

### 5.4 文件对象存储恢复

仅适用于 `TABMAIL_OBJECTSTORE=fs`：

```bash
TABMAIL_DATADIR=/data ./scripts/restore_files_objectstore.sh backups/objectstore-xxxx.tar.gz
```

### 5.5 S3 / MinIO 备份建议

当 `TABMAIL_OBJECTSTORE=s3` 时，可以直接使用脚本：

```bash
make backup-obj-s3
make restore-obj-s3 FILE=backups/objectstore-s3-xxxx.tar.gz
```

或：

```bash
TABMAIL_OBJECTSTORE=s3 make backup-obj
TABMAIL_OBJECTSTORE=s3 make restore-obj FILE=backups/objectstore-s3-xxxx.tar.gz
```

同时建议配合对象存储自身能力：

- S3 Versioning
- Bucket Replication
- 生命周期策略
- MinIO bucket replication / snapshot

数据库备份仍需单独执行，不能只备份 bucket。

---

## 6. SMTP 常见问题

## 6.1 RCPT TO 被拒绝

常见返回：

- `unknown recipient domain`
- `domain is not verified`
- `recipient domain rejected by policy`
- `recipient not provisioned`

检查顺序：

1. domain zone 是否存在
2. zone 是否 `is_verified=true`
3. zone 是否 `mx_verified=true`
4. route 是否匹配
5. recipient accept/reject policy 是否命中
6. mailbox naming policy 是否导致地址规范化后找不到

## 6.2 DATA 后消息没看到

检查：

1. `messages_rejected` 是否上涨
2. `deliveries_failed` 是否上涨
3. `default_store / store_domains / discard_domains` 是否把消息丢弃
4. `max_message_bytes` 是否超限
5. `daily_quota / max_messages_per_mailbox` 是否超限
6. object store 是否写入失败

## 6.3 STARTTLS 不工作

检查：

- `TABMAIL_SMTP_TLSENABLED=true`
- `TABMAIL_SMTP_TLSCERT`
- `TABMAIL_SMTP_TLSKEY`
- 证书文件路径是否正确

验证：

```bash
openssl s_client -starttls smtp -crlf -connect 127.0.0.1:2525
```

## 6.4 implicit TLS 不工作

检查：

- `TABMAIL_SMTP_FORCETLS=true`
- 证书是否加载成功

---

## 7. mailbox naming 常见问题

当前支持：

- `full`
- `local`
- `domain`

以及：

- `+tag` 剥离（可配置）

### 示例

原地址：

```text
alice+test@example.com
```

#### `full` + stripPlus=true

```text
alice@example.com
```

#### `local`

```text
alice
```

#### `domain`

```text
example.com
```

如果你切换了命名模式，注意：

- 老 mailbox key 可能和新策略不兼容
- 前端访问 inbox 时应使用规范化后的 mailbox key

---

## 8. Monitor 常见问题

## 8.1 SSE 没事件

检查：

1. admin key 是否正确
2. 是否真的有 SMTP 收件 / delete / purge 事件
3. `subscribers_current` 是否变化
4. 反向代理是否缓冲了 SSE

Nginx 常见需要关闭 buffering。

## 8.2 实时流有，历史没有

说明：

- SSE 用的是内存 ring + 实时广播
- 历史分页走的是数据库 `monitor_events`

检查：

- 数据库迁移是否已执行到 `003_monitor_and_policy.sql`
- `monitor_events` 是否存在

```bash
psql "$TABMAIL_DB_DSN" -c '\d monitor_events'
```

---

## 9. Policy 常见问题

## 9.1 明明域名存在，但收件被拒

优先检查：

- `default_accept`
- `accept_domains`
- `reject_domains`

如果 `default_accept=false`，只有 `accept_domains` 命中的域名才会被接受。

## 9.2 明明收件 accepted，但 inbox 里没有邮件

优先检查：

- `default_store`
- `store_domains`
- `discard_domains`

这类情况通常是**被接受但不存储**。

## 9.3 发件人被拒

检查：

- `reject_origin_domains`

支持简单通配符：

- `*.spam.test`
- `mail?.evil.test`

---

## 10. 数据库与对象存储

## 10.1 查看 monitor / policy 表

```bash
psql "$TABMAIL_DB_DSN" -c '\dt'
```

应至少看到：

- `smtp_policies`
- `monitor_events`

## 10.2 查看最新 monitor 记录

```bash
psql "$TABMAIL_DB_DSN" -c 'select type, mailbox, sender, subject, at from monitor_events order by at desc limit 20;'
```

## 10.3 查看最新审计

```bash
psql "$TABMAIL_DB_DSN" -c 'select action, actor, resource_type, created_at from audit_log order by created_at desc limit 20;'
```

---

## 11. 建议的运维习惯

1. 上线前先检查：
   - health
   - stats
   - STARTTLS
   - webhook endpoint

2. 每次策略改动后：
   - 用 monitor 页面观察几分钟
   - 检查 dead-letter queue

3. 域名接入后：
   - verify domain
   - 发一封测试邮件
   - 看 monitor / history / inbox

4. 出现“邮件丢失”反馈时：
   - 先看 policy
   - 再看 monitor
   - 再看 webhook / object store / DB

---

如果你后面继续完善，我建议下一份文档可以补：

- **安全基线手册**
- **备份恢复手册**
- **Prometheus / Grafana 接入手册**
