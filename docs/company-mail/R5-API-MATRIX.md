# R5 全入口调用与资格矩阵（B01-B）

基线：`99f1e21fe1feee13a4c9cc4c997b1a2c7d3fbfa9`。本矩阵记录当前实现，不把 R5 目标当成已实现规则。Go AST 清点得到127条显式路由/外层wrapper入口；TypeScript AST得到122个请求调用点、展开后125个分支，其中118个映射具体路由，7个是通用传输转发。

## 1. 范围、证据与守卫解释

生产组合入口为 `internal/api/router.go` 与 `internal/api/handlers/company_routes.go`。`TestR5RouteInventory` 比较源码注册的HTTP方法、完整路径、handler、继承middleware与条件；不是运行时权限形式化证明。语义字段由本轮逐入口/分组沿handler及store复核，源码变化需重新审核，不能只重新生成JSON自动认可新权限。

`NewRouter` 返回外层HTTP wrapper，因此不能把它强转chi.Router后假称Walk枚举成功。`GET /health`、`GET /ready` 在外层绕过认证；ready未配置/依赖异常返回503。CORS OPTIONS、框架404/405不是独立业务端点。条件 `cfg.CompanyRepository != nil`、`oh != nil`、`c.Domains != nil` 原样登记；生产公司组合启用公司端口，合法关闭outbound时旧outbound路由不注册。

公司路由的RequireAuth只证明JWT互动主体。多数管理边界位于companyTx/companyReadTx内，不能凭router未写RequireAdmin判为可被员工执行。Key与JWT不同：Key需要scope交集，JWT的RequireScopes会直接通过，suppression另在handler要求JWT管理员。普通管理权、正文read、整理organize、send_as、模板使用和恢复例外读取分开。

所有表格都是服务端实际边界；UI隐藏从不作为授权依据。已知缺口A01/A02/A03/A04/A05均写入对应行。客户端没有调用的API-only入口仍保留，不能据无调用推断安全或直接删除。

机器可读注册/语义见 [R5-API-MATRIX.json](evidence/R5-API-MATRIX.json)，客户端每一调用位置/表达式/分支见 [R5-CLIENT-CALLS.json](evidence/R5-CLIENT-CALLS.json)。源码自动检查是有边界的当前chi写法解析，不声称可以分析任意Go元编程；新增装配方式应扩展收集器和失败测试。

## 2. 每个入口的资格、资源、版本、审计与内容

公共主体仍受全局CORS/限流等影响；health/ready例外。相同下游规则重复列入各行，避免只写“同上”遗漏API。

| 方法与路径 | 服务端主体/权利 | 资源范围 | 版本/幂等 | 审计 | 内容分类 | 源码链 |
|---|---|---|---|---|---|---|
| `DELETE /api/v1/admin/permissions/{id}` | JWT admin/super_admin；匿名401，员工/Key403 | 个人覆盖限定目标user同tenant；profile区分system/global/tenant | 无revision；A01局部覆盖丢失/A02陈旧写入风险 | 此权限写入路径没有原子required audit | 原始配置/覆盖或effective权限，不含邮件正文 | internal/api/handlers/permissions.go + internal/store/postgres/permissions.go |
| `DELETE /api/v1/admin/plans/{id}` | JWT super_admin；匿名401，其他主体403 | 平台全局设置/套餐/统计；不是邮箱内容权 | 无客户端revision | app.InsertAudit后置尽力记录；不等价同事务required audit | 管理元数据 | internal/api/handlers/admin.go + internal/app/admin/service.go |
| `DELETE /api/v1/admin/tenants/{id}` | JWT super_admin；匿名401，其他主体403 | 平台范围；显式tenant ID，不要求等于当前选择tenant | 无客户端revision | app.InsertAudit后置尽力记录；不等价同事务required audit | 管理元数据 | internal/api/handlers/admin.go + internal/app/admin/service.go |
| `DELETE /api/v1/admin/tenants/{id}/keys/{keyId}` | JWT super_admin；匿名401，其他主体403 | 平台范围；显式tenant ID，不要求等于当前选择tenant | 无客户端revision | app.InsertAudit后置尽力记录；不等价同事务required audit | 管理元数据 | internal/api/handlers/admin.go + internal/app/admin/service.go |
| `DELETE /api/v1/admin/users/{id}` | JWT admin/super_admin；匿名401，员工/Key403 | 当前tenant中的员工ID，admin只可管理user，super可管理更高层级 | 无请求CAS；事务重读当前身份/角色，最后管理员保护 | guarded成员写入与user.update/user.delete同事务 | 管理元数据 | internal/api/handlers/users_admin.go + internal/store/postgres/member_guard.go |
| `DELETE /api/v1/admin/users/{id}/permissions` | JWT admin/super_admin；匿名401，员工/Key403 | 个人覆盖限定目标user同tenant；profile区分system/global/tenant | 无revision；A01局部覆盖丢失/A02陈旧写入风险 | 此权限写入路径没有原子required audit | 原始配置/覆盖或effective权限，不含邮件正文 | internal/api/handlers/permissions.go + internal/store/postgres/permissions.go |
| `DELETE /api/v1/company/domains/{id}` | JWT当前tenant-admin；路由RequireAdmin与handler requireCompanyAdmin双重检查 | 当前tenant+domain zone ID；删除检查company primary/mailbox/receipt等资产引用 | 创建/验证无请求CAS；删除持有资产保护锁 | 删除使用受保护事务与必要审计；创建/验证沿既有domain service审计，不冒称全部companyTx | 公司域名与DNS验证元数据，不授予员工邮箱内容权限 | internal/api/handlers/company_domains.go + internal/app/domains/service.go + internal/store/postgres/domain_delete.go |
| `DELETE /api/v1/company/drafts/{id}` | JWT有效员工主体；选定tenant，Key不能走互动公司接口；当前draft owner与邮箱send资格（管理权不替代） | tenant+user+draftID+mailbox+附件归属 | create稳定UUID/墓碑；update/delete revision；submit expected_revision+Idempotency-Key | 保存按既有workspace事务；submit消费/归档/账本/必要审计原子 | 私人草稿/保存结果；submit为发送回执 | internal/api/handlers/company_drafts.go + internal/store/postgres/company_mail.go + internal/app/submissions/service.go |
| `DELETE /api/v1/company/invitations/{id}` | JWT有效员工主体；选定tenant，Key不能走互动公司接口；当前tenant-admin | tenant+邀请ID/开通地址 | 创建/撤销邀请，token单次使用 | employee invitation与companyAudit同事务 | 邀请元数据；创建返回activation_token且no-store | internal/api/handlers/company_setup.go + internal/store/postgres/company_members.go |
| `DELETE /api/v1/keys/{keyId}` | JWT员工（user/admin/super_admin）；Key拒绝；创建还检查can_create_api_keys和scope/zone | 当前tenant+当前user owner；admin可管理tenant范围 | 无请求CAS；按key ID核对归属 | 旧平台路径；非统一companyTx，不能当作原子required audit | Key元数据；创建返回一次性原始Key | internal/api/handlers/admin.go:UserCreateAPIKey/UserListAPIKeys/UserDeleteAPIKey |
| `DELETE /api/v1/suppression/{id}` | JWT需tenant-admin；API Key需suppression read/manage scope | 当前tenant抑制地址/显式suppression ID | 无revision，必填reason | DeleteSuppressionAudited：删除与必要审计同事务 | 抑制收件人地址及原因 | internal/api/handlers/outbound.go:requireSuppressionAdmin/DeleteSuppression |
| `GET /api/v1/admin/audit` | JWT super_admin；匿名401，其他主体403 | 当前选定tenant；路径ID仍需handler/store作用域校验 | 只读 | 该读取不消费业务审计版本 | 管理元数据 | internal/api/handlers/admin.go + internal/app/admin/service.go |
| `GET /api/v1/admin/domains` | JWT admin/super_admin；匿名401，员工/Key403 | 员工/Key按scope与域名可见范围，admin列表为管理可见 | 只读 | 该读取不消费业务审计版本 | 域名管理元数据，不授予邮箱read/send | internal/api/handlers/domains.go + internal/app/domains/service.go |
| `GET /api/v1/admin/ingest/jobs` | JWT super_admin；匿名401，其他主体403 | 当前选定tenant；路径ID仍需handler/store作用域校验 | 只读 | 该读取不消费业务审计版本 | 平台队列/投递诊断元数据，不能按普通正文授权理解 | internal/api/handlers/admin.go + internal/app/admin/service.go |
| `GET /api/v1/admin/monitor/events` | JWT super_admin；匿名401，其他主体403 | 当前选定tenant；路径ID仍需handler/store作用域校验 | 只读 | 该读取不消费业务审计版本 | 平台监控事件/历史（可能含发收件元数据），不是普通员工入口 | internal/api/handlers/monitor.go |
| `GET /api/v1/admin/monitor/history` | JWT super_admin；匿名401，其他主体403 | 当前选定tenant；路径ID仍需handler/store作用域校验 | 只读 | 该读取不消费业务审计版本 | 平台监控事件/历史（可能含发收件元数据），不是普通员工入口 | internal/api/handlers/monitor.go |
| `GET /api/v1/admin/permissions` | JWT admin/super_admin；匿名401，员工/Key403 | 个人覆盖限定目标user同tenant；profile区分system/global/tenant | 只读effective或profile列表；非原始覆盖编辑快照 | 无逐次读取审计 | 原始配置/覆盖或effective权限，不含邮件正文 | internal/api/handlers/permissions.go + internal/store/postgres/permissions.go |
| `GET /api/v1/admin/plans` | JWT super_admin；匿名401，其他主体403 | 平台全局设置/套餐/统计；不是邮箱内容权 | 只读 | 该读取不消费业务审计版本 | 管理元数据 | internal/api/handlers/admin.go + internal/app/admin/service.go |
| `GET /api/v1/admin/policy` | JWT super_admin；匿名401，其他主体403 | 平台全局设置/套餐/统计；不是邮箱内容权 | 只读 | 该读取不消费业务审计版本 | 管理元数据 | internal/api/handlers/admin.go + internal/app/admin/service.go |
| `GET /api/v1/admin/runtime-config` | JWT super_admin；匿名401，其他主体403 | 平台全局设置/套餐/统计；不是邮箱内容权 | 只读 | 该读取不消费业务审计版本 | 管理元数据 | internal/api/handlers/admin.go + internal/app/admin/service.go |
| `GET /api/v1/admin/settings` | JWT super_admin；匿名401，其他主体403 | 平台全局设置/套餐/统计；不是邮箱内容权 | 只读 | 该读取不消费业务审计版本 | 管理元数据 | internal/api/handlers/admin.go + internal/app/admin/service.go |
| `GET /api/v1/admin/stats` | JWT super_admin；匿名401，其他主体403 | 平台全局设置/套餐/统计；不是邮箱内容权 | 只读 | 该读取不消费业务审计版本 | 管理元数据 | internal/api/handlers/admin.go + internal/app/admin/service.go |
| `GET /api/v1/admin/status` | JWT super_admin；匿名401，其他主体403 | 平台全局设置/套餐/统计；不是邮箱内容权 | 只读 | 该读取不消费业务审计版本 | 管理元数据 | internal/api/handlers/admin.go + internal/app/admin/service.go |
| `GET /api/v1/admin/tenants` | JWT super_admin；匿名401，其他主体403 | 平台范围；显式tenant ID，不要求等于当前选择tenant | 只读 | 该读取不消费业务审计版本 | 管理元数据 | internal/api/handlers/admin.go + internal/app/admin/service.go |
| `GET /api/v1/admin/tenants/{id}/config` | JWT super_admin；匿名401，其他主体403 | 平台范围；显式tenant ID，不要求等于当前选择tenant | 只读 | 该读取不消费业务审计版本 | 管理元数据 | internal/api/handlers/admin.go + internal/app/admin/service.go |
| `GET /api/v1/admin/tenants/{id}/keys` | JWT super_admin；匿名401，其他主体403 | 平台范围；显式tenant ID，不要求等于当前选择tenant | 只读 | 该读取不消费业务审计版本 | 管理元数据 | internal/api/handlers/admin.go + internal/app/admin/service.go |
| `GET /api/v1/admin/users` | JWT admin/super_admin；匿名401，员工/Key403 | 当前tenant中的员工ID，admin只可管理user，super可管理更高层级 | 只读 | 该读取不消费业务审计版本 | 管理元数据 | internal/api/handlers/users_admin.go + internal/store/postgres/member_guard.go |
| `GET /api/v1/admin/users/{id}/permissions` | JWT admin/super_admin；匿名401，员工/Key403 | 个人覆盖限定目标user同tenant；profile区分system/global/tenant | 只读effective或profile列表；非原始覆盖编辑快照 | 无逐次读取审计 | 原始配置/覆盖或effective权限，不含邮件正文 | internal/api/handlers/permissions.go + internal/store/postgres/permissions.go |
| `GET /api/v1/admin/webhooks/deliveries` | JWT super_admin；匿名401，其他主体403 | 当前选定tenant；路径ID仍需handler/store作用域校验 | 只读 | 该读取不消费业务审计版本 | 平台队列/投递诊断元数据，不能按普通正文授权理解 | internal/api/handlers/admin.go + internal/app/admin/service.go |
| `GET /api/v1/auth/me` | JWT员工（user/admin/super_admin）；Key拒绝 | 当前账号或请求中受验证凭据绑定的账号 | 只读 | 该读取不消费业务审计版本 | 身份/会话凭据或当前用户信息 | internal/api/handlers/auth.go + internal/store/postgres/refresh_rotation.go |
| `GET /api/v1/auth/me/permissions` | JWT员工（user/admin/super_admin）；Key拒绝 | 当前账号或请求中受验证凭据绑定的账号 | 只读 | 该读取不消费业务审计版本 | 当前有效权限 | internal/api/handlers/permissions.go:MyPermissions |
| `GET /api/v1/company/attachments/{id}` | JWT有效员工主体；选定tenant，Key不能走互动公司接口；上传需send，已提交附件需当前read，未提交按上传者规则 | tenant+upload ID+mailbox+当前user；sent pin按存活内容条目 | 只读 | 普通读取不要求独立审计 | 附件上传结果或附件字节 | internal/store/postgres/company_mail.go + internal/app/companymail/service.go |
| `GET /api/v1/company/audit` | JWT有效员工主体；选定tenant，Key不能走互动公司接口；数据库当前tenant-admin | tenant范围统计/审计；access还需mailbox/user同tenant | 只读 | 普通读取不要求独立审计 | 管理统计/审计/权利解释；不授予正文权 | internal/store/postgres/company_console.go + internal/store/postgres/company_access_explanation.go |
| `GET /api/v1/company/domains` | JWT当前tenant-admin；路由RequireAdmin与handler requireCompanyAdmin双重检查 | 当前tenant+domain zone ID；删除检查company primary/mailbox/receipt等资产引用 | 只读域名及验证状态 | 无逐次读取审计 | 公司域名与DNS验证元数据，不授予员工邮箱内容权限 | internal/api/handlers/company_domains.go + internal/app/domains/service.go + internal/store/postgres/domain_delete.go |
| `GET /api/v1/company/domains/{id}/verification` | JWT当前tenant-admin；路由RequireAdmin与handler requireCompanyAdmin双重检查 | 当前tenant+domain zone ID；删除检查company primary/mailbox/receipt等资产引用 | 只读域名及验证状态 | 无逐次读取审计 | 公司域名与DNS验证元数据，不授予员工邮箱内容权限 | internal/api/handlers/company_domains.go + internal/app/domains/service.go + internal/store/postgres/domain_delete.go |
| `GET /api/v1/company/drafts` | JWT有效员工主体；选定tenant，Key不能走互动公司接口；当前draft owner与邮箱send资格（管理权不替代） | tenant+user+draftID+mailbox+附件归属 | 只读 | 普通读取不要求独立审计 | 私人草稿/保存结果；submit为发送回执 | internal/api/handlers/company_drafts.go + internal/store/postgres/company_mail.go + internal/app/submissions/service.go |
| `GET /api/v1/company/drafts/{id}` | JWT有效员工主体；选定tenant，Key不能走互动公司接口；当前draft owner与邮箱send资格（管理权不替代） | tenant+user+draftID+mailbox+附件归属 | 只读 | 普通读取不要求独立审计 | 私人草稿/保存结果；submit为发送回执 | internal/api/handlers/company_drafts.go + internal/store/postgres/company_mail.go + internal/app/submissions/service.go |
| `GET /api/v1/company/invitations` | JWT有效员工主体；选定tenant，Key不能走互动公司接口；当前tenant-admin | tenant+邀请ID/开通地址 | 只读 | 普通读取不要求独立审计 | 邀请元数据；创建返回activation_token且no-store | internal/api/handlers/company_setup.go + internal/store/postgres/company_members.go |
| `GET /api/v1/company/mailboxes` | JWT有效员工主体；选定tenant，Key不能走互动公司接口 | tenant+当前actor；普通权利与管理可见分别投影 | 只读 | 普通读取不要求独立审计 | 邮箱元数据及CanRead/CanOrganize/CanSend，管理可见不是正文资格 | internal/store/postgres/company_members.go:ListWorkMailboxes |
| `GET /api/v1/company/mailboxes/{id}/access/{user}` | JWT有效员工主体；选定tenant，Key不能走互动公司接口；数据库当前tenant-admin | tenant范围统计/审计；access还需mailbox/user同tenant | 只读 | 普通读取不要求独立审计 | 管理统计/审计/权利解释；不授予正文权 | internal/store/postgres/company_console.go + internal/store/postgres/company_access_explanation.go |
| `GET /api/v1/company/mailboxes/{id}/events` | JWT有效员工主体；选定tenant，Key不能走互动公司接口；当前mailbox read，定期重新鉴权 | tenant+mailbox+event cursor | sequence cursor，resync语义仍需P7验证 | 普通读取不要求独立审计 | 邮箱变更通知，不含正文/BCC | internal/api/handlers/company_stream.go |
| `GET /api/v1/company/mailboxes/{id}/grants` | JWT有效员工主体；选定tenant，Key不能走互动公司接口；当前tenant-admin及目标属主成员层级保护 | tenant+mailboxID及目标owner/grant user | 只读 | 普通读取不要求独立审计 | 邮箱管理结果/授权快照 | internal/api/handlers/company_mailboxes.go + internal/store/postgres/company_members.go |
| `GET /api/v1/company/mailboxes/{id}/index-status` | JWT有效员工主体；选定tenant，Key不能走互动公司接口；当前mailbox read及zone | tenant+mailboxID+messageID/partID；内容缓存不能代替当前授权 | 只读 | 普通读取不要求独立审计 | 收件列表/正文/EML/附件/会话/索引元数据 | internal/store/postgres/company_mail.go + internal/store/postgres/company_message_read.go + internal/store/postgres/mail_content.go + internal/app/companymail |
| `GET /api/v1/company/mailboxes/{id}/messages` | JWT有效员工主体；选定tenant，Key不能走互动公司接口；当前mailbox read及zone | tenant+mailboxID+messageID/partID；内容缓存不能代替当前授权 | 只读 | 普通读取不要求独立审计 | 收件列表/正文/EML/附件/会话/索引元数据 | internal/store/postgres/company_mail.go + internal/store/postgres/company_message_read.go + internal/store/postgres/mail_content.go + internal/app/companymail |
| `GET /api/v1/company/mailboxes/{id}/messages/{message}` | JWT有效员工主体；选定tenant，Key不能走互动公司接口；当前mailbox read及zone | tenant+mailboxID+messageID/partID；内容缓存不能代替当前授权 | 只读 | 普通读取不要求独立审计 | 收件列表/正文/EML/附件/会话/索引元数据 | internal/store/postgres/company_mail.go + internal/store/postgres/company_message_read.go + internal/store/postgres/mail_content.go + internal/app/companymail |
| `GET /api/v1/company/mailboxes/{id}/messages/{message}/attachments` | JWT有效员工主体；选定tenant，Key不能走互动公司接口；当前mailbox read及zone | tenant+mailboxID+messageID/partID；内容缓存不能代替当前授权 | 只读 | 普通读取不要求独立审计 | 收件列表/正文/EML/附件/会话/索引元数据 | internal/store/postgres/company_mail.go + internal/store/postgres/company_message_read.go + internal/store/postgres/mail_content.go + internal/app/companymail |
| `GET /api/v1/company/mailboxes/{id}/messages/{message}/attachments/{index}` | JWT有效员工主体；选定tenant，Key不能走互动公司接口；当前mailbox read及zone | tenant+mailboxID+messageID/partID；内容缓存不能代替当前授权 | 只读 | 普通读取不要求独立审计 | 收件列表/正文/EML/附件/会话/索引元数据 | internal/store/postgres/company_mail.go + internal/store/postgres/company_message_read.go + internal/store/postgres/mail_content.go + internal/app/companymail |
| `GET /api/v1/company/mailboxes/{id}/messages/{message}/conversation` | JWT有效员工主体；选定tenant，Key不能走互动公司接口；当前mailbox read及zone | tenant+mailboxID+messageID/partID；内容缓存不能代替当前授权 | 只读 | 普通读取不要求独立审计 | 收件列表/正文/EML/附件/会话/索引元数据 | internal/store/postgres/company_mail.go + internal/store/postgres/company_message_read.go + internal/store/postgres/mail_content.go + internal/app/companymail |
| `GET /api/v1/company/mailboxes/{id}/messages/{message}/parts/{attachment}` | JWT有效员工主体；选定tenant，Key不能走互动公司接口；当前mailbox read及zone | tenant+mailboxID+messageID/partID；内容缓存不能代替当前授权 | 只读 | 普通读取不要求独立审计 | 收件列表/正文/EML/附件/会话/索引元数据 | internal/store/postgres/company_mail.go + internal/store/postgres/company_message_read.go + internal/store/postgres/mail_content.go + internal/app/companymail |
| `GET /api/v1/company/mailboxes/{id}/messages/{message}/source` | JWT有效员工主体；选定tenant，Key不能走互动公司接口；当前mailbox read及zone | tenant+mailboxID+messageID/partID；内容缓存不能代替当前授权 | 只读 | 普通读取不要求独立审计 | 收件列表/正文/EML/附件/会话/索引元数据 | internal/store/postgres/company_mail.go + internal/store/postgres/company_message_read.go + internal/store/postgres/mail_content.go + internal/app/companymail |
| `GET /api/v1/company/mailboxes/{id}/sent` | JWT有效员工主体；选定tenant，Key不能走互动公司接口；当前mailbox read+zone+存活sent item | tenant+mailboxID+assetID | 只读 | 普通读取不要求独立审计 | 已发送资产列表或文件夹状态 | internal/store/postgres/sent_archive.go |
| `GET /api/v1/company/mailboxes/{id}/templates` | JWT有效员工主体；选定tenant，Key不能走互动公司接口；精确mailbox send+可用模板usage | tenant+mailboxID+user | 只读 | 普通读取不要求独立审计 | 可用已发布模板，不因管理身份放行 | internal/store/postgres/company_templates.go:ListUsableTemplates |
| `GET /api/v1/company/outbound/{id}/recipients` | JWT有效员工主体；选定tenant，Key不能走互动公司接口 | tenant+job ID+receipt可见资格 | 只读 | 普通读取不要求独立审计 | 逐目标结果；无当前read时过滤BCC/diagnostic，A03资格期限缺口仍适用 | internal/api/handlers/company_recovery.go:Recipients + internal/app/submissions/service.go |
| `GET /api/v1/company/overview` | JWT有效员工主体；选定tenant，Key不能走互动公司接口；数据库当前tenant-admin | tenant范围统计/审计；access还需mailbox/user同tenant | 只读 | 普通读取不要求独立审计 | 管理统计/审计/权利解释；不授予正文权 | internal/store/postgres/company_console.go + internal/store/postgres/company_access_explanation.go |
| `GET /api/v1/company/recovery` | JWT有效员工主体；选定tenant，Key不能走互动公司接口；数据库recoveryActor当前super_admin | 当前tenant下receipt固定目标或outbound job ID | inspect理由；retry/reconcile观察updated_at+原件hash/收件人证据 | inspect/retry/reconcile必需审计，失败回滚 | 恢复元数据/核对结果；原件校验并非普通正文读取 | internal/api/handlers/company_recovery.go + internal/store/postgres/company_ops.go |
| `GET /api/v1/company/settings` | JWT有效员工主体；选定tenant，Key不能走互动公司接口 | 当前tenant公司配置与verified主域名 | 只读 | 普通读取不要求独立审计 | 公司元数据 | internal/api/handlers/company_setup.go + internal/store/postgres/company_members.go:ConfigureCompany |
| `GET /api/v1/company/submissions` | JWT有效员工主体；选定tenant，Key不能走互动公司接口 | tenant+原提交ID+原发送mailboxID | 只读 | 普通读取不要求独立审计 | 最小回执/状态，历史submitter不授予内容 | internal/store/postgres/submissions.go + internal/store/postgres/sent_archive.go |
| `GET /api/v1/company/submissions/{id}` | JWT有效员工主体；选定tenant，Key不能走互动公司接口 | tenant+原提交ID+原发送mailboxID | 只读 | 普通读取不要求独立审计 | 最小回执/状态，历史submitter不授予内容 | internal/store/postgres/submissions.go + internal/store/postgres/sent_archive.go |
| `GET /api/v1/company/submissions/{id}/attachments` | JWT有效员工主体；选定tenant，Key不能走互动公司接口；当前原邮箱read+zone+存活sent item，失败404 | tenant+原提交ID+原发送mailboxID | 只读 | 普通读取不要求独立审计 | 正文或附件元数据/字节；不依赖job存活 | internal/store/postgres/submissions.go + internal/store/postgres/sent_archive.go |
| `GET /api/v1/company/submissions/{id}/attachments/{aid}/download` | JWT有效员工主体；选定tenant，Key不能走互动公司接口；当前原邮箱read+zone+存活sent item，失败404 | tenant+原提交ID+原发送mailboxID | 只读 | 普通读取不要求独立审计 | 正文或附件元数据/字节；不依赖job存活 | internal/store/postgres/submissions.go + internal/store/postgres/sent_archive.go |
| `GET /api/v1/company/submissions/{id}/content` | JWT有效员工主体；选定tenant，Key不能走互动公司接口；当前原邮箱read+zone+存活sent item，失败404 | tenant+原提交ID+原发送mailboxID | 只读 | 普通读取不要求独立审计 | 正文或附件元数据/字节；不依赖job存活 | internal/store/postgres/submissions.go + internal/store/postgres/sent_archive.go |
| `GET /api/v1/company/templates` | JWT有效员工主体；选定tenant，Key不能走互动公司接口；当前tenant-admin管理权 | tenant+template/version/mailbox/user ID | 只读 | 普通读取不要求独立审计 | 模板草稿/已发布快照/授权元数据 | internal/api/handlers/company_templates.go + internal/store/postgres/company_templates.go |
| `GET /api/v1/company/templates/{id}/grants` | JWT有效员工主体；选定tenant，Key不能走互动公司接口；当前tenant-admin管理权 | tenant+template/version/mailbox/user ID | 只读 | 普通读取不要求独立审计 | 模板草稿/已发布快照/授权元数据 | internal/api/handlers/company_templates.go + internal/store/postgres/company_templates.go |
| `GET /api/v1/company/templates/{id}/versions` | JWT有效员工主体；选定tenant，Key不能走互动公司接口；当前tenant-admin管理权 | tenant+template/version/mailbox/user ID | 只读 | 普通读取不要求独立审计 | 模板草稿/已发布快照/授权元数据 | internal/api/handlers/company_templates.go + internal/store/postgres/company_templates.go |
| `GET /api/v1/domains` | JWT用户或有效API Key；Key还需记录中的scope；JWT不按Key scope限制 | 员工/Key按scope与域名可见范围，admin列表为管理可见 | 只读 | 该读取不消费业务审计版本 | 域名管理元数据，不授予邮箱read/send | internal/api/handlers/domains.go + internal/app/domains/service.go |
| `GET /api/v1/keys` | JWT员工（user/admin/super_admin）；Key拒绝；创建还检查can_create_api_keys和scope/zone | 当前tenant+当前user owner；admin可管理tenant范围 | 只读 | 该读取不消费业务审计版本 | Key元数据；创建返回一次性原始Key | internal/api/handlers/admin.go:UserCreateAPIKey/UserListAPIKeys/UserDeleteAPIKey |
| `GET /api/v1/outbound` | JWT用户或有效API Key；Key还需记录中的scope；JWT不按Key scope限制 | 当前tenant+原job ID；receipt按submitter/key归属，content需当前原邮箱read和zone | 只读 | 该读取不消费业务审计版本 | 投递回执；旧接口ContentAllowed仍缺sent-item期限(A03)，body/BCC/诊断可能保留 | internal/api/handlers/outbound.go + internal/app/submissions/service.go |
| `GET /api/v1/outbound/{id}` | JWT用户或有效API Key；Key还需记录中的scope；JWT不按Key scope限制 | 当前tenant+原job ID；receipt按submitter/key归属，content需当前原邮箱read和zone | 只读 | 该读取不消费业务审计版本 | 投递回执；旧接口ContentAllowed仍缺sent-item期限(A03)，body/BCC/诊断可能保留 | internal/api/handlers/outbound.go + internal/app/submissions/service.go |
| `GET /api/v1/outbound/{id}/attempts` | JWT用户或有效API Key；Key还需记录中的scope；JWT不按Key scope限制 | 当前tenant+原job ID；receipt按submitter/key归属，content需当前原邮箱read和zone | 只读 | 该读取不消费业务审计版本 | 投递回执；旧接口ContentAllowed仍缺sent-item期限(A03)，body/BCC/诊断可能保留 | internal/api/handlers/outbound.go + internal/app/submissions/service.go |
| `GET /api/v1/suppression` | JWT需tenant-admin；API Key需suppression read/manage scope | 当前tenant抑制地址/显式suppression ID | 只读 | 该读取不消费业务审计版本 | 抑制收件人地址及原因 | internal/api/handlers/outbound.go:requireSuppressionAdmin/DeleteSuppression |
| `GET /docs` | 匿名可到达；认证失败仍受统一middleware影响 | 无业务资源 | 只读 | 无强制逐次读取审计 | 公开文档/健康状态 | internal/api/router.go |
| `GET /docs-assets/*` | 匿名可到达；认证失败仍受统一middleware影响 | 无业务资源 | 只读 | 无强制逐次读取审计 | 公开文档/健康状态 | internal/api/router.go |
| `GET /health` | 匿名GET，外层wrapper先于身份lookup | 进程存活/依赖ready | 只读；ready5秒超时 | 无强制逐次读取审计 | 最小health/ready状态 | internal/api/router.go:360-381 |
| `GET /metrics` | 配置metrics bearer token或已认证super_admin，非公开 | 平台低基数运行指标 | 只读 | 无强制逐次读取审计 | 运行指标，不是员工正文 | internal/api/router.go:metricsAuthorized |
| `GET /openapi.yaml` | 匿名可到达；认证失败仍受统一middleware影响 | 无业务资源 | 只读 | 无强制逐次读取审计 | 公开文档/健康状态 | internal/api/router.go |
| `GET /ready` | 匿名GET，外层wrapper先于身份lookup | 进程存活/依赖ready | 只读；ready5秒超时 | 无强制逐次读取审计 | 最小health/ready状态 | internal/api/router.go:360-381 |
| `GET /redoc` | 匿名可到达；认证失败仍受统一middleware影响 | 无业务资源 | 只读 | 无强制逐次读取审计 | 公开文档/健康状态 | internal/api/router.go |
| `PATCH /api/v1/admin/permissions/{id}` | JWT admin/super_admin；匿名401，员工/Key403 | 个人覆盖限定目标user同tenant；profile区分system/global/tenant | 无revision；A01局部覆盖丢失/A02陈旧写入风险 | 此权限写入路径没有原子required audit | 原始配置/覆盖或effective权限，不含邮件正文 | internal/api/handlers/permissions.go + internal/store/postgres/permissions.go |
| `PATCH /api/v1/admin/plans/{id}` | JWT super_admin；匿名401，其他主体403 | 平台全局设置/套餐/统计；不是邮箱内容权 | 无客户端revision | app.InsertAudit后置尽力记录；不等价同事务required audit | 管理元数据 | internal/api/handlers/admin.go + internal/app/admin/service.go |
| `PATCH /api/v1/admin/policy` | JWT super_admin；匿名401，其他主体403 | 平台全局设置/套餐/统计；不是邮箱内容权 | 无客户端revision | app.InsertAudit后置尽力记录；不等价同事务required audit | 管理元数据 | internal/api/handlers/admin.go + internal/app/admin/service.go |
| `PATCH /api/v1/admin/settings` | JWT super_admin；匿名401，其他主体403 | 平台全局设置/套餐/统计；不是邮箱内容权 | 无客户端revision | app.InsertAudit后置尽力记录；不等价同事务required audit | 管理元数据 | internal/api/handlers/admin.go + internal/app/admin/service.go |
| `PATCH /api/v1/admin/tenants/{id}` | JWT super_admin；匿名401，其他主体403 | 平台范围；显式tenant ID，不要求等于当前选择tenant | 无客户端revision | app.InsertAudit后置尽力记录；不等价同事务required audit | 管理元数据 | internal/api/handlers/admin.go + internal/app/admin/service.go |
| `PATCH /api/v1/admin/users/{id}` | JWT admin/super_admin；匿名401，员工/Key403 | 当前tenant中的员工ID，admin只可管理user，super可管理更高层级 | 无请求CAS；事务重读当前身份/角色，最后管理员保护 | guarded成员写入与user.update/user.delete同事务 | 管理元数据 | internal/api/handlers/users_admin.go + internal/store/postgres/member_guard.go |
| `POST /api/v1/admin/invite` | JWT super_admin；匿名401，其他主体403 | 平台admin invitation；不是公司员工入职 | 创建邀请，不含CAS | 旧邀请创建无companyAudit；公司模式注册入口拒绝此激活链路 | invite_code凭据及邀请元数据 | internal/api/handlers/users_admin.go:InviteAdmin |
| `POST /api/v1/admin/permissions` | JWT admin/super_admin；匿名401，员工/Key403 | 个人覆盖限定目标user同tenant；profile区分system/global/tenant | 无revision；A01局部覆盖丢失/A02陈旧写入风险 | 此权限写入路径没有原子required audit | 原始配置/覆盖或effective权限，不含邮件正文 | internal/api/handlers/permissions.go + internal/store/postgres/permissions.go |
| `POST /api/v1/admin/plans` | JWT super_admin；匿名401，其他主体403 | 平台全局设置/套餐/统计；不是邮箱内容权 | 无客户端revision | app.InsertAudit后置尽力记录；不等价同事务required audit | 管理元数据 | internal/api/handlers/admin.go + internal/app/admin/service.go |
| `POST /api/v1/admin/tenants` | JWT super_admin；匿名401，其他主体403 | 平台范围；显式tenant ID，不要求等于当前选择tenant | 无客户端revision | app.InsertAudit后置尽力记录；不等价同事务required audit | 管理元数据 | internal/api/handlers/admin.go + internal/app/admin/service.go |
| `POST /api/v1/admin/tenants/{id}/keys` | JWT super_admin；匿名401，其他主体403 | 平台范围；显式tenant ID，不要求等于当前选择tenant | 无客户端revision | app.InsertAudit后置尽力记录；不等价同事务required audit | 管理元数据 | internal/api/handlers/admin.go + internal/app/admin/service.go |
| `POST /api/v1/auth/change-password` | JWT员工（user/admin/super_admin）；Key拒绝 | 当前账号或请求中受验证凭据绑定的账号 | 无客户端revision | 旧平台路径；非统一companyTx，不能当作原子required audit | 身份/会话凭据或当前用户信息 | internal/api/handlers/auth.go + internal/store/postgres/refresh_rotation.go |
| `POST /api/v1/auth/login` | 有效账号密码和active状态；无需既有JWT | 当前账号或请求中受验证凭据绑定的账号 | 新登录会话 | 登录会话状态，非required管理审计 | 身份/会话凭据或当前用户信息 | internal/api/handlers/auth.go + internal/store/postgres/refresh_rotation.go |
| `POST /api/v1/auth/logout` | JWT员工（user/admin/super_admin）；Key拒绝 | 当前账号或请求中受验证凭据绑定的账号 | 无客户端revision | 旧平台路径；非统一companyTx，不能当作原子required audit | 身份/会话凭据或当前用户信息 | internal/api/handlers/auth.go + internal/store/postgres/refresh_rotation.go |
| `POST /api/v1/auth/refresh` | 有效refresh cookie/token family；无需既有JWT | 当前账号或请求中受验证凭据绑定的账号 | 单次旋转/重放家族撤销 | 刷新事务，不是companyAudit管理事件 | 身份/会话凭据或当前用户信息 | internal/api/handlers/auth.go + internal/store/postgres/refresh_rotation.go |
| `POST /api/v1/auth/register` | 入口注册仍存在，但CompanyOnly在handler返回403；兼容模式按注册配置处理 | 当前账号或请求中受验证凭据绑定的账号 | 公司模式不写入 | 公司模式拒绝，无激活写入 | 身份/会话凭据或当前用户信息 | internal/api/handlers/auth.go + internal/store/postgres/refresh_rotation.go |
| `POST /api/v1/company/activate` | 匿名+有效未消费邀请token，tenant由邀请绑定 | 邀请ID/哈希及其tenant/employee/mailbox | 单次消费+过期检查 | 激活事务记录与身份/邮箱创建 | 激活结果，不返回邮件 | internal/api/handlers/company_setup.go + internal/store/postgres/company_members.go |
| `POST /api/v1/company/domains` | JWT当前tenant-admin；路由RequireAdmin与handler requireCompanyAdmin双重检查 | 当前tenant+domain zone ID；删除检查company primary/mailbox/receipt等资产引用 | 创建/验证无请求CAS；删除持有资产保护锁 | 删除使用受保护事务与必要审计；创建/验证沿既有domain service审计，不冒称全部companyTx | 公司域名与DNS验证元数据，不授予员工邮箱内容权限 | internal/api/handlers/company_domains.go + internal/app/domains/service.go + internal/store/postgres/domain_delete.go |
| `POST /api/v1/company/domains/{id}/verify` | JWT当前tenant-admin；路由RequireAdmin与handler requireCompanyAdmin双重检查 | 当前tenant+domain zone ID；删除检查company primary/mailbox/receipt等资产引用 | 创建/验证无请求CAS；删除持有资产保护锁 | 删除使用受保护事务与必要审计；创建/验证沿既有domain service审计，不冒称全部companyTx | 公司域名与DNS验证元数据，不授予员工邮箱内容权限 | internal/api/handlers/company_domains.go + internal/app/domains/service.go + internal/store/postgres/domain_delete.go |
| `POST /api/v1/company/drafts` | JWT有效员工主体；选定tenant，Key不能走互动公司接口；当前draft owner与邮箱send资格（管理权不替代） | tenant+user+draftID+mailbox+附件归属 | create稳定UUID/墓碑；update/delete revision；submit expected_revision+Idempotency-Key | 保存按既有workspace事务；submit消费/归档/账本/必要审计原子 | 私人草稿/保存结果；submit为发送回执 | internal/api/handlers/company_drafts.go + internal/store/postgres/company_mail.go + internal/app/submissions/service.go |
| `POST /api/v1/company/drafts/{id}/submit` | JWT有效员工主体；选定tenant，Key不能走互动公司接口；当前draft owner与邮箱send资格（管理权不替代） | tenant+user+draftID+mailbox+附件归属 | create稳定UUID/墓碑；update/delete revision；submit expected_revision+Idempotency-Key | 保存按既有workspace事务；submit消费/归档/账本/必要审计原子 | 私人草稿/保存结果；submit为发送回执 | internal/api/handlers/company_drafts.go + internal/store/postgres/company_mail.go + internal/app/submissions/service.go |
| `POST /api/v1/company/employees/{id}/offboard` | JWT有效员工主体；选定tenant，Key不能走互动公司接口；当前tenant-admin及成员层级、最后管理员约束 | tenant+目标员工+有效接收人+actor-owned plan | preview生成15分钟指纹计划；execute必须plan_id，重复返回receipt | preview/执行及转移/撤权同事务 | 资产影响数量/处置回执，不含私人草稿正文 | internal/api/handlers/company_lifecycle.go + internal/store/postgres/employee_disposition.go；A04当前inactive目标拒绝 |
| `POST /api/v1/company/employees/{id}/offboard/preview` | JWT有效员工主体；选定tenant，Key不能走互动公司接口；当前tenant-admin及成员层级、最后管理员约束 | tenant+目标员工+有效接收人+actor-owned plan | preview生成15分钟指纹计划；execute必须plan_id，重复返回receipt | preview/执行及转移/撤权同事务 | 资产影响数量/处置回执，不含私人草稿正文 | internal/api/handlers/company_lifecycle.go + internal/store/postgres/employee_disposition.go；A04当前inactive目标拒绝 |
| `POST /api/v1/company/index/retry` | JWT有效员工主体；选定tenant，Key不能走互动公司接口；当前tenant-admin | 选定tenant+不可变资源ID；companyTx/companyReadTx重载主体 | 有理由、最多100条当前failed工作 | 索引重排与审计/outbox同事务 | 公司元数据 | internal/store/postgres/mail_content.go + internal/store/postgres/company_index_recovery_test.go |
| `POST /api/v1/company/invitations` | JWT有效员工主体；选定tenant，Key不能走互动公司接口；当前tenant-admin | tenant+邀请ID/开通地址 | 创建/撤销邀请，token单次使用 | employee invitation与companyAudit同事务 | 邀请元数据；创建返回activation_token且no-store | internal/api/handlers/company_setup.go + internal/store/postgres/company_members.go |
| `POST /api/v1/company/mailboxes` | JWT有效员工主体；选定tenant，Key不能走互动公司接口；当前tenant-admin及目标属主成员层级保护 | tenant+mailboxID及目标owner/grant user | 创建无旧revision；handover/convert-shared/grant/send-policy均要求mailbox revision | 邮箱管理写入与companyAudit同事务 | 邮箱管理结果/授权快照 | internal/api/handlers/company_mailboxes.go + internal/store/postgres/company_members.go |
| `POST /api/v1/company/mailboxes/{id}/attachments` | JWT有效员工主体；选定tenant，Key不能走互动公司接口；上传需send，已提交附件需当前read，未提交按上传者规则 | tenant+upload ID+mailbox+当前user；sent pin按存活内容条目 | reserve/put/finish，预算/大小/摘要；非客户端CAS | 对象与DB非单事务，失败对象走恢复/回收 | 附件上传结果或附件字节 | internal/store/postgres/company_mail.go + internal/app/companymail/service.go |
| `POST /api/v1/company/mailboxes/{id}/convert-shared` | JWT有效员工主体；选定tenant，Key不能走互动公司接口；当前tenant-admin及目标属主成员层级保护 | tenant+mailboxID及目标owner/grant user | 创建无旧revision；handover/convert-shared/grant/send-policy均要求mailbox revision | 邮箱管理写入与companyAudit同事务 | 邮箱管理结果/授权快照 | internal/api/handlers/company_mailboxes.go + internal/store/postgres/company_members.go |
| `POST /api/v1/company/mailboxes/{id}/handover` | JWT有效员工主体；选定tenant，Key不能走互动公司接口；当前tenant-admin及目标属主成员层级保护 | tenant+mailboxID及目标owner/grant user | 创建无旧revision；handover/convert-shared/grant/send-policy均要求mailbox revision | 邮箱管理写入与companyAudit同事务 | 邮箱管理结果/授权快照 | internal/api/handlers/company_mailboxes.go + internal/store/postgres/company_members.go |
| `POST /api/v1/company/mailboxes/{id}/messages/{message}/actions` | JWT有效员工主体；选定tenant，Key不能走互动公司接口；当前mailbox read及zone；seen/star只read，trash/archive/restore需read+organize | tenant+mailboxID+messageID/partID；内容缓存不能代替当前授权 | 收件动作没有请求revision；restore仍清expires_at(A05) | message state、companyAudit与outbox同事务 | 收件列表/正文/EML/附件/会话/索引元数据 | internal/store/postgres/company_mail.go + internal/store/postgres/company_message_read.go + internal/store/postgres/mail_content.go + internal/app/companymail |
| `POST /api/v1/company/mailboxes/{id}/messages/{message}/compose` | JWT有效员工主体；选定tenant，Key不能走互动公司接口；当前mailbox read及zone；原信read+目标From可发送，模板限制仍生效 | tenant+mailboxID+messageID/partID；内容缓存不能代替当前授权 | 回复/转发预填，不直接发送 | 没有投递事务 | 收件列表/正文/EML/附件/会话/索引元数据 | internal/store/postgres/company_mail.go + internal/store/postgres/company_message_read.go + internal/store/postgres/mail_content.go + internal/app/companymail |
| `POST /api/v1/company/mailboxes/{id}/sent/{message}/actions` | JWT有效员工主体；选定tenant，Key不能走互动公司接口；当前mailbox read+zone+存活sent item；organize | tenant+mailboxID+assetID | sent item revision CAS+期限条件 | sent操作、审计、mailbox event同事务 | 已发送资产列表或文件夹状态 | internal/store/postgres/sent_archive.go |
| `POST /api/v1/company/outbound/{id}/inspect` | JWT有效员工主体；选定tenant，Key不能走互动公司接口；路由super_admin+AccessibleOutboundJob可见 | 选定tenant+job ID | reason 8–1000bytes；非正文期限CAS | InsertAuditRequired成功后返回；不是普通read豁免 | 例外job内容与收件人，移除delivery token/raw MIME | internal/api/handlers/company_recovery.go:InspectOutbound |
| `POST /api/v1/company/outbound/{id}/reconcile` | JWT有效员工主体；选定tenant，Key不能走互动公司接口；数据库recoveryActor当前super_admin | 当前tenant下receipt固定目标或outbound job ID | inspect理由；retry/reconcile观察updated_at+原件hash/收件人证据 | inspect/retry/reconcile必需审计，失败回滚 | 恢复元数据/核对结果；原件校验并非普通正文读取 | internal/api/handlers/company_recovery.go + internal/store/postgres/company_ops.go |
| `POST /api/v1/company/recovery/{id}/inspect` | JWT有效员工主体；选定tenant，Key不能走互动公司接口；数据库recoveryActor当前super_admin | 当前tenant下receipt固定目标或outbound job ID | inspect理由；retry/reconcile观察updated_at+原件hash/收件人证据 | inspect/retry/reconcile必需审计，失败回滚 | 恢复元数据/核对结果；原件校验并非普通正文读取 | internal/api/handlers/company_recovery.go + internal/store/postgres/company_ops.go |
| `POST /api/v1/company/recovery/{id}/retry` | JWT有效员工主体；选定tenant，Key不能走互动公司接口；数据库recoveryActor当前super_admin | 当前tenant下receipt固定目标或outbound job ID | inspect理由；retry/reconcile观察updated_at+原件hash/收件人证据 | inspect/retry/reconcile必需审计，失败回滚 | 恢复元数据/核对结果；原件校验并非普通正文读取 | internal/api/handlers/company_recovery.go + internal/store/postgres/company_ops.go |
| `POST /api/v1/company/templates` | JWT有效员工主体；选定tenant，Key不能走互动公司接口；当前tenant-admin管理权 | tenant+template/version/mailbox/user ID | 保存/发布/停用/撤销使用template revision；usage grant当前无请求revision | template变更与companyAudit同事务 | 模板草稿/已发布快照/授权元数据 | internal/api/handlers/company_templates.go + internal/store/postgres/company_templates.go |
| `POST /api/v1/company/templates/preview` | JWT有效员工主体；选定tenant，Key不能走互动公司接口；精确发送邮箱权+当前template usage及版本状态，非管理豁免 | tenant+template/version/mailbox/user ID | 固定template_version_id+变量校验 | 预览无实际发送/入队 | 模板草稿/已发布快照/授权元数据 | internal/api/handlers/company_templates.go + internal/store/postgres/company_templates.go |
| `POST /api/v1/company/templates/{id}/publish` | JWT有效员工主体；选定tenant，Key不能走互动公司接口；当前tenant-admin管理权 | tenant+template/version/mailbox/user ID | 保存/发布/停用/撤销使用template revision；usage grant当前无请求revision | template变更与companyAudit同事务 | 模板草稿/已发布快照/授权元数据 | internal/api/handlers/company_templates.go + internal/store/postgres/company_templates.go |
| `POST /api/v1/company/templates/{id}/retire` | JWT有效员工主体；选定tenant，Key不能走互动公司接口；当前tenant-admin管理权 | tenant+template/version/mailbox/user ID | 保存/发布/停用/撤销使用template revision；usage grant当前无请求revision | template变更与companyAudit同事务 | 模板草稿/已发布快照/授权元数据 | internal/api/handlers/company_templates.go + internal/store/postgres/company_templates.go |
| `POST /api/v1/company/templates/{id}/versions/{version}/revoke` | JWT有效员工主体；选定tenant，Key不能走互动公司接口；当前tenant-admin管理权 | tenant+template/version/mailbox/user ID | 保存/发布/停用/撤销使用template revision；usage grant当前无请求revision | template变更与companyAudit同事务 | 模板草稿/已发布快照/授权元数据 | internal/api/handlers/company_templates.go + internal/store/postgres/company_templates.go |
| `POST /api/v1/keys` | JWT员工（user/admin/super_admin）；Key拒绝；创建还检查can_create_api_keys和scope/zone | 当前tenant+当前user owner；admin可管理tenant范围 | 无请求CAS；按key ID核对归属 | 旧平台路径；非统一companyTx，不能当作原子required audit | Key元数据；创建返回一次性原始Key | internal/api/handlers/admin.go:UserCreateAPIKey/UserListAPIKeys/UserDeleteAPIKey |
| `POST /api/v1/outbound/{id}/retry` | JWT用户或有效API Key；Key还需记录中的scope；JWT不按Key scope限制；当前发送身份资格+可重试state，uncertain禁止 | 当前tenant+原job ID；receipt按submitter/key归属，content需当前原邮箱read和zone | 状态/token条件写入；无客户端revision | 重试状态与授权由outbound命令控制，不能把capability当授权 | 投递回执；旧接口ContentAllowed仍缺sent-item期限(A03)，body/BCC/诊断可能保留 | internal/api/handlers/outbound.go + internal/app/submissions/service.go |
| `PUT /api/v1/admin/users/{id}/permissions` | JWT admin/super_admin；匿名401，员工/Key403 | 个人覆盖限定目标user同tenant；profile区分system/global/tenant | 无revision；A01局部覆盖丢失/A02陈旧写入风险 | 此权限写入路径没有原子required audit | 原始配置/覆盖或effective权限，不含邮件正文 | internal/api/handlers/permissions.go + internal/store/postgres/permissions.go |
| `PUT /api/v1/company/drafts/{id}` | JWT有效员工主体；选定tenant，Key不能走互动公司接口；当前draft owner与邮箱send资格（管理权不替代） | tenant+user+draftID+mailbox+附件归属 | create稳定UUID/墓碑；update/delete revision；submit expected_revision+Idempotency-Key | 保存按既有workspace事务；submit消费/归档/账本/必要审计原子 | 私人草稿/保存结果；submit为发送回执 | internal/api/handlers/company_drafts.go + internal/store/postgres/company_mail.go + internal/app/submissions/service.go |
| `PUT /api/v1/company/mailboxes/{id}/grants` | JWT有效员工主体；选定tenant，Key不能走互动公司接口；当前tenant-admin及目标属主成员层级保护 | tenant+mailboxID及目标owner/grant user | 创建无旧revision；handover/convert-shared/grant/send-policy均要求mailbox revision | 邮箱管理写入与companyAudit同事务 | 邮箱管理结果/授权快照 | internal/api/handlers/company_mailboxes.go + internal/store/postgres/company_members.go |
| `PUT /api/v1/company/mailboxes/{id}/send-policy` | JWT有效员工主体；选定tenant，Key不能走互动公司接口；当前tenant-admin及目标属主成员层级保护 | tenant+mailboxID及目标owner/grant user | 创建无旧revision；handover/convert-shared/grant/send-policy均要求mailbox revision | 邮箱管理写入与companyAudit同事务 | 邮箱管理结果/授权快照 | internal/api/handlers/company_mailboxes.go + internal/store/postgres/company_members.go |
| `PUT /api/v1/company/settings` | JWT有效员工主体；选定tenant，Key不能走互动公司接口；当前tenant-admin | 当前tenant公司配置与verified主域名 | company settings revision CAS | company.configure与配置同事务 | 公司元数据 | internal/api/handlers/company_setup.go + internal/store/postgres/company_members.go:ConfigureCompany |
| `PUT /api/v1/company/templates/{id}` | JWT有效员工主体；选定tenant，Key不能走互动公司接口；当前tenant-admin管理权 | tenant+template/version/mailbox/user ID | 保存/发布/停用/撤销使用template revision；usage grant当前无请求revision | template变更与companyAudit同事务 | 模板草稿/已发布快照/授权元数据 | internal/api/handlers/company_templates.go + internal/store/postgres/company_templates.go |
| `PUT /api/v1/company/templates/{id}/grants` | JWT有效员工主体；选定tenant，Key不能走互动公司接口；当前tenant-admin管理权 | tenant+template/version/mailbox/user ID | usage grant当前无请求revision（P5治理收口） | template变更与companyAudit同事务 | 模板草稿/已发布快照/授权元数据 | internal/api/handlers/company_templates.go + internal/store/postgres/company_templates.go |

## 3. 实际前端请求映射

记录API facade、直接请求、下载与SSE；页面字符串中的curl样例不是浏览器请求。参数名规范化只用于匹配route占位符，不证明参数已授权。7个转发调用不算未知业务端点，必须由其上层具体调用决定路径和方法。

| 调用位置 | 请求分支 | 对应注册/转发职责 |
|---|---|---|
| `web/app/(dashboard)/company/recovery/page.tsx:22` (分支0) | `GET /api/v1/company/recovery` | GET /api/v1/company/recovery |
| `web/app/(dashboard)/company/recovery/page.tsx:27` (分支0) | `GET /api/v1/admin/runtime-config` | GET /api/v1/admin/runtime-config |
| `web/app/(dashboard)/company/recovery/page.tsx:96` (分支0) | `POST /api/v1/company/recovery/{dynamic}/inspect` | POST /api/v1/company/recovery/{id}/inspect |
| `web/app/(dashboard)/company/recovery/page.tsx:185` (分支0) | `POST /api/v1/company/recovery/{dynamic}/retry` | POST /api/v1/company/recovery/{id}/retry |
| `web/app/(dashboard)/company/recovery/page.tsx:242` (分支0) | `POST /api/v1/company/outbound/{id}/inspect` | POST /api/v1/company/outbound/{id}/inspect |
| `web/app/(dashboard)/company/recovery/page.tsx:321` (分支0) | `POST /api/v1/company/outbound/{dynamic}/reconcile` | POST /api/v1/company/outbound/{id}/reconcile |
| `web/app/(dashboard)/company/templates/page.tsx:36` (分支0) | `GET /api/v1/company/templates` | GET /api/v1/company/templates |
| `web/app/(dashboard)/company/templates/page.tsx:75` (分支0) | `POST /api/v1/company/templates/{dynamic}/retire` | POST /api/v1/company/templates/{id}/retire |
| `web/app/(dashboard)/company/templates/page.tsx:97` (分支0) | `POST /api/v1/company/templates/{dynamic}/publish` | POST /api/v1/company/templates/{id}/publish |
| `web/app/auth/activate/page.tsx:52` (分支0) | `POST /api/v1/company/activate` | POST /api/v1/company/activate |
| `web/components/company/compose.tsx:81` (分支0) | `POST /api/v1/company/drafts` | POST /api/v1/company/drafts |
| `web/components/company/compose.tsx:81` (分支1) | `PUT /api/v1/company/drafts/{dynamic}` | PUT /api/v1/company/drafts/{id} |
| `web/components/company/compose.tsx:84` (分支0) | `GET /api/v1/company/drafts/{dynamic}` | GET /api/v1/company/drafts/{id} |
| `web/components/company/compose.tsx:99` (分支0) | `GET /api/v1/company/mailboxes/{id}/templates` | GET /api/v1/company/mailboxes/{id}/templates |
| `web/components/company/compose.tsx:472` (分支0) | `POST /api/v1/company/templates/preview` | POST /api/v1/company/templates/preview |
| `web/components/company/compose.tsx:550` (分支0) | `POST /api/v1/company/mailboxes/{id}/attachments` | POST /api/v1/company/mailboxes/{id}/attachments |
| `web/components/company/compose.tsx:573` (分支0) | `GET /api/v1/company/attachments/{dynamic}` | GET /api/v1/company/attachments/{id} |
| `web/components/company/grants.tsx:22` (分支0) | `GET /api/v1/company/mailboxes/{id}/grants` | GET /api/v1/company/mailboxes/{id}/grants |
| `web/components/company/grants.tsx:103` (分支0) | `PUT /api/v1/company/mailboxes/{id}/grants` | PUT /api/v1/company/mailboxes/{id}/grants |
| `web/components/company/grants.tsx:167` (分支0) | `POST /api/v1/company/mailboxes/{id}/handover` | POST /api/v1/company/mailboxes/{id}/handover |
| `web/components/company/grants.tsx:176` (分支0) | `POST /api/v1/company/mailboxes/{id}/convert-shared` | POST /api/v1/company/mailboxes/{id}/convert-shared |
| `web/components/company/templates/editor.tsx:60` (分支0) | `PUT /api/v1/company/templates/{dynamic}` | PUT /api/v1/company/templates/{id} |
| `web/components/company/templates/editor.tsx:60` (分支1) | `POST /api/v1/company/templates` | POST /api/v1/company/templates |
| `web/components/company/templates/editor.tsx:308` (分支0) | `POST /api/v1/company/templates/preview` | POST /api/v1/company/templates/preview |
| `web/components/company/templates/grants.tsx:42` (分支0) | `GET /api/v1/company/templates/{dynamic}/grants` | GET /api/v1/company/templates/{id}/grants |
| `web/components/company/templates/grants.tsx:90` (分支0) | `PUT /api/v1/company/templates/{dynamic}/grants` | PUT /api/v1/company/templates/{id}/grants |
| `web/components/company/templates/grants.tsx:133` (分支0) | `PUT /api/v1/company/templates/{dynamic}/grants` | PUT /api/v1/company/templates/{id}/grants |
| `web/components/company/templates/versions.tsx:37` (分支0) | `GET /api/v1/company/templates/{dynamic}/versions` | GET /api/v1/company/templates/{id}/versions |
| `web/features/company/access-explanation.tsx:15` (分支0) | `GET /api/v1/company/mailboxes/{dynamic}/access/{dynamic}` | GET /api/v1/company/mailboxes/{id}/access/{user} |
| `web/features/company/api.ts:56` (分支0) | `POST /api/v1/company/employees/{id}/offboard/preview` | POST /api/v1/company/employees/{id}/offboard/preview |
| `web/features/company/api.ts:57` (分支0) | `POST /api/v1/company/employees/{id}/offboard` | POST /api/v1/company/employees/{id}/offboard |
| `web/features/company/audit.tsx:12` (分支0) | `GET /api/v1/company/audit` | GET /api/v1/company/audit |
| `web/features/company/domain-settings.tsx:12` (分支0) | `GET /api/v1/company/settings` | GET /api/v1/company/settings |
| `web/features/company/domain-settings.tsx:51` (分支0) | `PUT /api/v1/company/settings` | PUT /api/v1/company/settings |
| `web/features/company/employees.tsx:12` (分支0) | `GET /api/v1/company/settings` | GET /api/v1/company/settings |
| `web/features/company/employees.tsx:14` (分支0) | `GET /api/v1/company/invitations` | GET /api/v1/company/invitations |
| `web/features/company/employees.tsx:36` (分支0) | `POST /api/v1/company/invitations` | POST /api/v1/company/invitations |
| `web/features/company/employees.tsx:79` (分支0) | `DELETE /api/v1/company/invitations/{dynamic}` | DELETE /api/v1/company/invitations/{id} |
| `web/features/company/mailbox-admin.tsx:48` (分支0) | `POST /api/v1/company/mailboxes` | POST /api/v1/company/mailboxes |
| `web/features/company/overview.tsx:13` (分支0) | `GET /api/v1/company/overview` | GET /api/v1/company/overview |
| `web/features/company/overview.tsx:21` (分支0) | `POST /api/v1/company/index/retry` | POST /api/v1/company/index/retry |
| `web/features/mail/api.ts:20` (分支0) | `GET /api/v1/company/drafts` | GET /api/v1/company/drafts |
| `web/features/mail/api.ts:21` (分支0) | `GET /api/v1/company/mailboxes/{id}/sent` | GET /api/v1/company/mailboxes/{id}/sent |
| `web/features/mail/api.ts:22` (分支0) | `GET /api/v1/company/mailboxes/{id}/index-status` | GET /api/v1/company/mailboxes/{id}/index-status |
| `web/features/mail/api.ts:23` (分支0) | `POST /api/v1/company/mailboxes/{id}/sent/{id}/actions` | POST /api/v1/company/mailboxes/{id}/sent/{message}/actions |
| `web/features/mail/components/conversation-list.tsx:13` (分支0) | `GET /api/v1/company/mailboxes/{id}/messages/{id}/conversation` | GET /api/v1/company/mailboxes/{id}/messages/{message}/conversation |
| `web/features/mail/components/draft-folder.tsx:20` (分支0) | `GET /api/v1/company/drafts/{dynamic}` | GET /api/v1/company/drafts/{id} |
| `web/features/mail/components/draft-folder.tsx:22` (分支0) | `DELETE /api/v1/company/drafts/{dynamic}` | DELETE /api/v1/company/drafts/{id} |
| `web/features/mail/components/message-pane.tsx:25` (分支0) | `GET /api/v1/company/mailboxes/{id}/messages/{dynamic}/attachments` | GET /api/v1/company/mailboxes/{id}/messages/{message}/attachments |
| `web/features/mail/components/message-pane.tsx:27` (分支0) | `POST /api/v1/company/mailboxes/{id}/messages/{dynamic}/actions` | POST /api/v1/company/mailboxes/{id}/messages/{message}/actions |
| `web/features/mail/components/message-pane.tsx:38` (分支0) | `POST /api/v1/company/mailboxes/{id}/messages/{dynamic}/compose` | POST /api/v1/company/mailboxes/{id}/messages/{message}/compose |
| `web/features/mail/components/message-pane.tsx:48` (分支0) | `GET /api/v1/company/mailboxes/{id}/messages/{dynamic}/source` | GET /api/v1/company/mailboxes/{id}/messages/{message}/source |
| `web/features/mail/components/message-pane.tsx:62` (分支0) | `GET /api/v1/company/mailboxes/{id}/messages/{dynamic}/parts/{id}` | GET /api/v1/company/mailboxes/{id}/messages/{message}/parts/{attachment} |
| `web/features/mail/components/message-pane.tsx:62` (分支1) | `GET /api/v1/company/mailboxes/{id}/messages/{dynamic}/attachments/{dynamic}` | GET /api/v1/company/mailboxes/{id}/messages/{message}/attachments/{index} |
| `web/features/mail/components/submission-content.tsx:39` (分支0) | `GET /api/v1/company/submissions/{id}/attachments/{id}/download` | GET /api/v1/company/submissions/{id}/attachments/{aid}/download |
| `web/features/mail/components/submission-pane.tsx:96` (分支0) | `POST /api/v1/outbound/{dynamic}/retry` | POST /api/v1/outbound/{id}/retry |
| `web/features/mail/workspace.tsx:48` (分支0) | `GET /api/v1/company/mailboxes/{id}/events` | GET /api/v1/company/mailboxes/{id}/events |
| `web/lib/api/admin.ts:25` (分支0) | `GET /api/v1/admin/monitor/events` | GET /api/v1/admin/monitor/events |
| `web/lib/api/admin.ts:35` (分支0) | `GET /api/v1/admin/monitor/history` | GET /api/v1/admin/monitor/history |
| `web/lib/api/admin.ts:43` (分支0) | `GET /api/v1/admin/settings` | GET /api/v1/admin/settings |
| `web/lib/api/admin.ts:47` (分支0) | `PATCH /api/v1/admin/settings` | PATCH /api/v1/admin/settings |
| `web/lib/api/admin.ts:56` (分支0) | `GET /api/v1/admin/policy` | GET /api/v1/admin/policy |
| `web/lib/api/admin.ts:60` (分支0) | `PATCH /api/v1/admin/policy` | PATCH /api/v1/admin/policy |
| `web/lib/api/admin.ts:67` (分支0) | `GET /api/v1/admin/tenants` | GET /api/v1/admin/tenants |
| `web/lib/api/admin.ts:71` (分支0) | `POST /api/v1/admin/tenants` | POST /api/v1/admin/tenants |
| `web/lib/api/admin.ts:78` (分支0) | `PATCH /api/v1/admin/tenants/{dynamic}` | PATCH /api/v1/admin/tenants/{id} |
| `web/lib/api/admin.ts:85` (分支0) | `DELETE /api/v1/admin/tenants/{dynamic}` | DELETE /api/v1/admin/tenants/{id} |
| `web/lib/api/admin.ts:89` (分支0) | `GET /api/v1/admin/tenants/{dynamic}/config` | GET /api/v1/admin/tenants/{id}/config |
| `web/lib/api/admin.ts:96` (分支0) | `POST /api/v1/admin/tenants/{dynamic}/keys` | POST /api/v1/admin/tenants/{id}/keys |
| `web/lib/api/admin.ts:103` (分支0) | `GET /api/v1/admin/tenants/{dynamic}/keys` | GET /api/v1/admin/tenants/{id}/keys |
| `web/lib/api/admin.ts:107` (分支0) | `DELETE /api/v1/admin/tenants/{dynamic}/keys/{dynamic}` | DELETE /api/v1/admin/tenants/{id}/keys/{keyId} |
| `web/lib/api/admin.ts:115` (分支0) | `POST /api/v1/keys` | POST /api/v1/keys |
| `web/lib/api/admin.ts:122` (分支0) | `GET /api/v1/keys` | GET /api/v1/keys |
| `web/lib/api/admin.ts:126` (分支0) | `DELETE /api/v1/keys/{dynamic}` | DELETE /api/v1/keys/{keyId} |
| `web/lib/api/admin.ts:130` (分支0) | `GET /api/v1/admin/plans` | GET /api/v1/admin/plans |
| `web/lib/api/admin.ts:134` (分支0) | `POST /api/v1/admin/plans` | POST /api/v1/admin/plans |
| `web/lib/api/admin.ts:141` (分支0) | `PATCH /api/v1/admin/plans/{dynamic}` | PATCH /api/v1/admin/plans/{id} |
| `web/lib/api/admin.ts:148` (分支0) | `DELETE /api/v1/admin/plans/{dynamic}` | DELETE /api/v1/admin/plans/{id} |
| `web/lib/api/admin.ts:152` (分支0) | `GET /api/v1/admin/stats` | GET /api/v1/admin/stats |
| `web/lib/api/admin.ts:156` (分支0) | `GET /api/v1/admin/audit` | GET /api/v1/admin/audit |
| `web/lib/api/admin.ts:168` (分支0) | `GET /api/v1/admin/ingest/jobs` | GET /api/v1/admin/ingest/jobs |
| `web/lib/api/admin.ts:180` (分支0) | `GET /api/v1/admin/webhooks/deliveries` | GET /api/v1/admin/webhooks/deliveries |
| `web/lib/api/admin.ts:188` (分支0) | `POST /api/v1/admin/invite` | POST /api/v1/admin/invite |
| `web/lib/api/admin.ts:195` (分支0) | `GET /api/v1/admin/users` | GET /api/v1/admin/users |
| `web/lib/api/admin.ts:204` (分支0) | `PATCH /api/v1/admin/users/{dynamic}` | PATCH /api/v1/admin/users/{id} |
| `web/lib/api/admin.ts:211` (分支0) | `DELETE /api/v1/admin/users/{dynamic}` | DELETE /api/v1/admin/users/{id} |
| `web/lib/api/auth.ts:5` (分支0) | `POST /api/v1/auth/change-password` | POST /api/v1/auth/change-password |
| `web/lib/api/auth.ts:12` (分支0) | `POST /api/v1/auth/login` | POST /api/v1/auth/login |
| `web/lib/api/auth.ts:19` (分支0) | `POST /api/v1/auth/register` | POST /api/v1/auth/register |
| `web/lib/api/auth.ts:26` (分支0) | `POST /api/v1/auth/logout` | POST /api/v1/auth/logout |
| `web/lib/api/base.ts:98` (分支0) | `DYNAMIC {dynamic}` | 通用request/company/download/SSE传输转发，不独立赋权 |
| `web/lib/api/base.ts:123` (分支0) | `POST {dynamic}` | 通用request/company/download/SSE传输转发，不独立赋权 |
| `web/lib/api/base.ts:205` (分支0) | `DYNAMIC {dynamic}` | 通用request/company/download/SSE传输转发，不独立赋权 |
| `web/lib/api/base.ts:290` (分支0) | `POST /api/v1/auth/refresh` | POST /api/v1/auth/refresh |
| `web/lib/api/base.ts:363` (分支0) | `GET {dynamic}` | 通用request/company/download/SSE传输转发，不独立赋权 |
| `web/lib/api/base.ts:380` (分支0) | `GET {dynamic}` | 通用request/company/download/SSE传输转发，不独立赋权 |
| `web/lib/api/domains.ts:5` (分支0) | `GET /api/v1/domains` | GET /api/v1/domains |
| `web/lib/api/domains.ts:9` (分支0) | `GET /api/v1/admin/domains` | GET /api/v1/admin/domains |
| `web/lib/api/permissions.ts:12` (分支0) | `GET /api/v1/admin/permissions` | GET /api/v1/admin/permissions |
| `web/lib/api/permissions.ts:16` (分支0) | `POST /api/v1/admin/permissions` | POST /api/v1/admin/permissions |
| `web/lib/api/permissions.ts:23` (分支0) | `PATCH /api/v1/admin/permissions/{dynamic}` | PATCH /api/v1/admin/permissions/{id} |
| `web/lib/api/permissions.ts:30` (分支0) | `DELETE /api/v1/admin/permissions/{dynamic}` | DELETE /api/v1/admin/permissions/{id} |
| `web/lib/api/permissions.ts:36` (分支0) | `GET /api/v1/admin/users/{dynamic}/permissions` | GET /api/v1/admin/users/{id}/permissions |
| `web/lib/api/permissions.ts:40` (分支0) | `PUT /api/v1/admin/users/{dynamic}/permissions` | PUT /api/v1/admin/users/{id}/permissions |
| `web/lib/api/permissions.ts:47` (分支0) | `DELETE /api/v1/admin/users/{dynamic}/permissions` | DELETE /api/v1/admin/users/{id}/permissions |
| `web/lib/api/permissions.ts:53` (分支0) | `GET /api/v1/auth/me/permissions` | GET /api/v1/auth/me/permissions |
| `web/lib/api/system.ts:4` (分支0) | `GET /health` | GET /health |
| `web/lib/company.ts:273` (分支0) | `GET /api/v1/company/domains` | GET /api/v1/company/domains |
| `web/lib/company.ts:275` (分支0) | `POST /api/v1/company/domains` | POST /api/v1/company/domains |
| `web/lib/company.ts:281` (分支0) | `POST /api/v1/company/domains/{id}/verify` | POST /api/v1/company/domains/{id}/verify |
| `web/lib/company.ts:287` (分支0) | `GET /api/v1/company/domains/{id}/verification` | GET /api/v1/company/domains/{id}/verification |
| `web/lib/company.ts:292` (分支0) | `DELETE /api/v1/company/domains/{id}` | DELETE /api/v1/company/domains/{id} |
| `web/lib/company.ts:304` (分支0) | `PUT /api/v1/company/mailboxes/{id}/send-policy` | PUT /api/v1/company/mailboxes/{id}/send-policy |
| `web/lib/company.ts:317` (分支0) | `POST /api/v1/company/templates/{id}/versions/{dynamic}/revoke` | POST /api/v1/company/templates/{id}/versions/{version}/revoke |
| `web/lib/company.ts:327` (分支0) | `POST /api/v1/company/drafts/{id}/submit` | POST /api/v1/company/drafts/{id}/submit |
| `web/lib/company.ts:340` (分支0) | `DYNAMIC /api/v1/company{dynamic}` | 通用request/company/download/SSE传输转发，不独立赋权 |
| `web/lib/company.ts:344` (分支0) | `GET /api/v1/company/mailboxes` | GET /api/v1/company/mailboxes |
| `web/lib/company.ts:351` (分支0) | `GET /api/v1/company/mailboxes/{id}/messages` | GET /api/v1/company/mailboxes/{id}/messages |
| `web/lib/company.ts:357` (分支0) | `GET /api/v1/company/mailboxes/{id}/messages/{id}` | GET /api/v1/company/mailboxes/{id}/messages/{message} |
| `web/lib/company.ts:362` (分支0) | `GET /api/v1/company/submissions` | GET /api/v1/company/submissions |
| `web/lib/company.ts:368` (分支0) | `GET /api/v1/company/submissions/{id}` | GET /api/v1/company/submissions/{id} |
| `web/lib/company.ts:371` (分支0) | `GET /api/v1/company/submissions/{id}/content` | GET /api/v1/company/submissions/{id}/content |
| `web/lib/company.ts:376` (分支0) | `GET /api/v1/company/submissions/{id}/attachments` | GET /api/v1/company/submissions/{id}/attachments |
| `web/lib/company.ts:383` (分支0) | `GET /api/v1/admin/users` | GET /api/v1/admin/users |
| `web/lib/company.ts:402` (分支0) | `GET /api/v1/company{dynamic}` | 通用request/company/download/SSE传输转发，不独立赋权 |

## 4. 未发现当前Web调用的注册入口

这些是当前源码调用清点的结果，不代表外部集成不存在。清理前需P0-110/P8-100确认支持范围。

- `DELETE /api/v1/suppression/{id}`
- `GET /api/v1/admin/status`
- `GET /api/v1/auth/me`
- `GET /api/v1/company/outbound/{id}/recipients`
- `GET /api/v1/outbound`
- `GET /api/v1/outbound/{id}`
- `GET /api/v1/outbound/{id}/attempts`
- `GET /api/v1/suppression`
- `GET /docs`
- `GET /docs-assets/*`
- `GET /metrics`
- `GET /openapi.yaml`
- `GET /ready`
- `GET /redoc`

## 5. 本轮发现的重要分类差异

`/company/overview`、`/company/audit`、access解释在数据库层要求管理员；`/company/settings` GET允许已登录公司成员读取设置。`/company/outbound/{id}/recipients`按回执/当前read进行过滤，不因位于恢复相关handler就一律要求super_admin。`/company/outbound/{id}/inspect`才是带理由和required audit的例外内容路径，仍需要AccessibleOutboundJob资格。

`/admin/invite`在当前router依然注册且InviteAdmin可创建旧邀请；CompanyOnly关闭的是旧注册/激活使用链路，不能写成“创建端点未注册”。这属于兼容入口清理地图，不在本批改变行为。监控、全局配置/套餐及tenant管理是平台面，与公司员工日常使用分离。

## 6. 本轮验证与后续边界

矩阵完整性测试覆盖缺行、新行、重复行、守卫变更、handler变更、条件变更和缺少语义字段。实际HTTP审计复现覆盖权限覆盖/配置、过期内容、冻结交接、恢复期限，真实数据库还覆盖GC推进与BCC持久化。并非127个入口都完成了全部主体×资源×生命周期运行时穷举；这种全量安全回归归P1–P11的各专项验收。

P0-020交付的是完整入口/资格/调用地图；P0-110仍需OpenAPI字段与实际响应类型契约，P0-070仍需锁顺序，P0-030的A01/A02仍需真实组件与请求联动。不要将这些未完成证据混入本矩阵完成数。
