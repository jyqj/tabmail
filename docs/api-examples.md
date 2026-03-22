# API 示例（curl）

以下示例默认：

```bash
export BASE_URL='http://127.0.0.1:8080'
export ADMIN_KEY='changeme'
```

---

## 1. 健康检查

```bash
curl "$BASE_URL/health"
```

## 2. 创建套餐

```bash
curl -X POST "$BASE_URL/api/v1/admin/plans" \
  -H "X-Admin-Key: $ADMIN_KEY" \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "starter",
    "max_domains": 5,
    "max_mailboxes_per_domain": 200,
    "max_messages_per_mailbox": 500,
    "max_message_bytes": 10485760,
    "retention_hours": 24,
    "rpm_limit": 120,
    "daily_quota": 20000
  }'
```

## 3. 创建租户

```bash
curl -X POST "$BASE_URL/api/v1/admin/tenants" \
  -H "X-Admin-Key: $ADMIN_KEY" \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "tenant-a",
    "plan_id": "00000000-0000-0000-0000-000000000002"
  }'
```

## 4. 为租户创建 API Key

```bash
export TENANT_ID='<tenant-id>'

curl -X POST "$BASE_URL/api/v1/admin/tenants/$TENANT_ID/keys" \
  -H "X-Admin-Key: $ADMIN_KEY" \
  -H 'Content-Type: application/json' \
  -d '{
    "label": "default key",
    "scopes": [
      "domains:read",
      "domains:write",
      "routes:read",
      "routes:write",
      "mailboxes:read",
      "mailboxes:write",
      "messages:read",
      "messages:write"
    ]
  }'
```

返回里的 `data.key` 只会显示一次：

```bash
export TENANT_API_KEY='<tenant-api-key>'
```

---

## 5. 绑定域名

```bash
curl -X POST "$BASE_URL/api/v1/domains" \
  -H "X-API-Key: $TENANT_API_KEY" \
  -H 'Content-Type: application/json' \
  -d '{
    "domain": "mail.example.com"
  }'
```

## 6. 查看域名列表

```bash
curl "$BASE_URL/api/v1/domains" \
  -H "X-API-Key: $TENANT_API_KEY"
```

## 7. 触发域名验证

```bash
export DOMAIN_ID='<domain-id>'

curl -X POST "$BASE_URL/api/v1/domains/$DOMAIN_ID/verify" \
  -H "X-API-Key: $TENANT_API_KEY"
```

## 8. 查看域名验证状态

```bash
curl "$BASE_URL/api/v1/domains/$DOMAIN_ID/verification-status" \
  -H "X-API-Key: $TENANT_API_KEY"
```

---

## 9. 创建 route

### wildcard route

```bash
curl -X POST "$BASE_URL/api/v1/domains/$DOMAIN_ID/routes" \
  -H "X-API-Key: $TENANT_API_KEY" \
  -H 'Content-Type: application/json' \
  -d '{
    "route_type": "wildcard",
    "match_value": "*.mail.example.com",
    "auto_create_mailbox": true,
    "access_mode_default": "public"
  }'
```

### sequence route

```bash
curl -X POST "$BASE_URL/api/v1/domains/$DOMAIN_ID/routes" \
  -H "X-API-Key: $TENANT_API_KEY" \
  -H 'Content-Type: application/json' \
  -d '{
    "route_type": "sequence",
    "match_value": "box-{n}.mail.example.com",
    "range_start": 1,
    "range_end": 1000,
    "auto_create_mailbox": true,
    "access_mode_default": "token"
  }'
```

## 10. 查看 routes

```bash
curl "$BASE_URL/api/v1/domains/$DOMAIN_ID/routes" \
  -H "X-API-Key: $TENANT_API_KEY"
```

---

## 11. 创建 mailbox

### public mailbox

```bash
curl -X POST "$BASE_URL/api/v1/mailboxes" \
  -H "X-API-Key: $TENANT_API_KEY" \
  -H 'Content-Type: application/json' \
  -d '{
    "address": "demo@mail.example.com",
    "access_mode": "public"
  }'
```

### token mailbox

```bash
curl -X POST "$BASE_URL/api/v1/mailboxes" \
  -H "X-API-Key: $TENANT_API_KEY" \
  -H 'Content-Type: application/json' \
  -d '{
    "address": "secure@mail.example.com",
    "password": "Passw0rd!",
    "access_mode": "token"
  }'
```

## 12. 列出 mailbox

```bash
curl "$BASE_URL/api/v1/mailboxes" \
  -H "X-API-Key: $TENANT_API_KEY"
```

---

## 13. 为 token mailbox 换取 token

```bash
curl -X POST "$BASE_URL/api/v1/token" \
  -H 'Content-Type: application/json' \
  -d '{
    "address": "secure@mail.example.com",
    "password": "Passw0rd!"
  }'
```

返回：

```json
{
  "data": {
    "token": "....",
    "expires_in": 86400
  }
}
```

```bash
export MAILBOX_TOKEN='<mailbox-token>'
```

---

## 14. 查看消息列表

### public mailbox

```bash
curl "$BASE_URL/api/v1/mailbox/demo@mail.example.com"
```

### token mailbox

```bash
curl "$BASE_URL/api/v1/mailbox/secure@mail.example.com" \
  -H "Authorization: Bearer $MAILBOX_TOKEN"
```

### tenant API key mailbox

```bash
curl "$BASE_URL/api/v1/mailbox/secure@mail.example.com" \
  -H "X-API-Key: $TENANT_API_KEY"
```

## 15. 查看消息详情

```bash
export MESSAGE_ID='<message-id>'

curl "$BASE_URL/api/v1/mailbox/secure@mail.example.com/$MESSAGE_ID" \
  -H "Authorization: Bearer $MAILBOX_TOKEN"
```

## 16. 查看原始邮件

```bash
curl "$BASE_URL/api/v1/mailbox/secure@mail.example.com/$MESSAGE_ID/source" \
  -H "Authorization: Bearer $MAILBOX_TOKEN"
```

## 17. 标记已读

```bash
curl -X PATCH "$BASE_URL/api/v1/mailbox/secure@mail.example.com/$MESSAGE_ID" \
  -H "Authorization: Bearer $MAILBOX_TOKEN"
```

## 18. 删除单封邮件

```bash
curl -X DELETE "$BASE_URL/api/v1/mailbox/secure@mail.example.com/$MESSAGE_ID" \
  -H "Authorization: Bearer $MAILBOX_TOKEN"
```

## 19. 清空邮箱

```bash
curl -X DELETE "$BASE_URL/api/v1/mailbox/secure@mail.example.com" \
  -H "Authorization: Bearer $MAILBOX_TOKEN"
```

---

## 20. 管理员以某租户身份调用 tenant-scoped 接口

```bash
curl "$BASE_URL/api/v1/domains" \
  -H "X-Admin-Key: $ADMIN_KEY" \
  -H "X-Tenant-ID: $TENANT_ID"
```

---

## 21. 查看系统状态

```bash
curl "$BASE_URL/api/v1/admin/stats" \
  -H "X-Admin-Key: $ADMIN_KEY"
```

## 22. 拉取 monitor 历史

```bash
curl "$BASE_URL/api/v1/admin/monitor/history?page=1&per_page=20&type=message" \
  -H "X-Admin-Key: $ADMIN_KEY"
```

## 23. 订阅全局 monitor SSE

```bash
curl -N "$BASE_URL/api/v1/admin/monitor/events" \
  -H "X-Admin-Key: $ADMIN_KEY"
```

## 24. 获取当前 SMTP policy

```bash
curl "$BASE_URL/api/v1/admin/policy" \
  -H "X-Admin-Key: $ADMIN_KEY"
```

## 25. 更新 SMTP policy

```bash
curl -X PATCH "$BASE_URL/api/v1/admin/policy" \
  -H "X-Admin-Key: $ADMIN_KEY" \
  -H 'Content-Type: application/json' \
  -d '{
    "default_accept": true,
    "accept_domains": [],
    "reject_domains": ["blocked.example.com", "*.trash.test"],
    "default_store": true,
    "store_domains": [],
    "discard_domains": ["devnull.example.com"],
    "reject_origin_domains": ["*.spam.test"]
  }'
```

---

## 26. SMTP 收件测试

```bash
nc 127.0.0.1 2525
```

手工发送：

```text
EHLO localhost
MAIL FROM:<sender@example.org>
RCPT TO:<demo@mail.example.com>
DATA
Subject: hello
From: sender@example.org
To: demo@mail.example.com

hello tabmail
.
QUIT
```

## 27. STARTTLS 测试

```bash
openssl s_client -starttls smtp -crlf -connect 127.0.0.1:2525
```
