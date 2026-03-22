# TabMail Web

TabMail 的管理前端与邮箱查看前端，基于 Next.js App Router。

## 功能

- public inbox 查看
- mailbox token 登录
- tenant console
  - domains
  - routes
  - mailboxes
- admin console
  - stats
  - global monitor
  - smtp policy
  - tenants
  - plans
- 后端 Swagger 文档嵌入页

## 开发

```bash
npm install
npm run dev
```

默认地址：

```bash
http://127.0.0.1:3000
```

## 环境变量

### `NEXT_PUBLIC_API_URL`

指向 TabMail 后端 HTTP API，例如：

```bash
NEXT_PUBLIC_API_URL=http://127.0.0.1:8080
```

未设置时：

- 浏览器请求优先走当前站点 rewrite
- docs 页会回退到 `http://localhost:8080/docs`

## 鉴权模式

前端支持 3 种连接方式：

1. **Mailbox**
   - 输入邮箱地址和密码
   - 调 `POST /api/v1/token`
   - 获取 mailbox token

2. **Tenant API Key**
   - 用于 tenant-scoped console

3. **Admin Key**
   - 可额外设置 `X-Tenant-ID`
   - 用于代理某个 tenant 调用 tenant-scoped 接口

## 当前页面

- `/`
- `/docs`
- `/inbox/[address]`
- `/console/domains`
- `/console/mailboxes`
- `/admin`
- `/admin/monitor`
- `/admin/policy`
- `/admin/tenants`
- `/admin/plans`

## 质量检查

```bash
npm run lint
npm run build
```
