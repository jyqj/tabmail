# R5 迁移与数据所有权基线（B01-B）

来源：`99f1e21fe1feee13a4c9cc4c997b1a2c7d3fbfa9` 的业务代码，配合本批 `TestR5SchemaInventoryAndRestart`。仅查询新建可丢弃 PostgreSQL 的 public catalog，不读取现网数据。完整列、约束定义、索引、触发器函数体见 [机器清单](evidence/R4-SCHEMA-INVENTORY.json)；历史文件字节固定在 [13 个迁移哈希](evidence/R4-MIGRATION-HASHES.json)。

## 1. 已执行基线与解释范围

实际观察 Goose 版本 13：50 张表、499 列、177 个约束（其中75个外键）、152 个索引、7个非内部触发器与7个触发器函数。重新执行 Migrate 后 catalog 完全一致。既有 `TestArchitectureUpgradeBackfillsExistingEmployeeAssets` 本轮也实际通过：在独立测试库由00001–00009升级至00013，验证归档、附件、索引和创建回执回填。它不是未来 R5 migration 的升级验收。

所有50张表的 `relrowsecurity/relforcerowsecurity` 均为 false：当前隔离主要由应用资格判定、带租户条件的SQL和复合FK实施；不能把本基线解释为数据库已启用RLS，也不能仅据未启用RLS断言某个入口已经越权。必须逐入口核对。

## 2. 表与数据职责

| 表 | 当前职责分组 | 列数 | 外键数 | 索引数 |
|---|---|---:|---:|---:|
| `admin_invitations` | 身份与管理 | 7 | 1 | 4 |
| `audit_log` | 派生数据与后台设施 | 8 | 1 | 3 |
| `company_settings` | 身份与管理 | 5 | 2 | 1 |
| `domain_routes` | 邮箱与内容资产 | 10 | 1 | 2 |
| `domain_zones` | 邮箱与内容资产 | 16 | 3 | 8 |
| `draft_creation_receipts` | 工作区与模板 | 6 | 1 | 2 |
| `employee_invitations` | 身份与管理 | 12 | 2 | 3 |
| `employee_offboarding_plans` | 工作区与模板 | 13 | 3 | 2 |
| `goose_db_version` | 派生数据与后台设施 | 4 | 0 | 1 |
| `ingest_jobs` | 发送与接收证据 | 19 | 0 | 5 |
| `ingest_recipient_outcomes` | 发送与接收证据 | 10 | 1 | 2 |
| `ingress_daily_usage` | 发送与接收证据 | 3 | 1 | 1 |
| `mail_attachments` | 工作区与模板 | 12 | 2 | 3 |
| `mail_documents` | 派生数据与后台设施 | 12 | 1 | 3 |
| `mail_drafts` | 工作区与模板 | 9 | 2 | 2 |
| `mail_index_jobs` | 派生数据与后台设施 | 9 | 1 | 2 |
| `mail_template_grants` | 工作区与模板 | 6 | 3 | 1 |
| `mail_template_versions` | 工作区与模板 | 9 | 1 | 3 |
| `mail_templates` | 工作区与模板 | 9 | 1 | 3 |
| `mailbox_event_log` | 派生数据与后台设施 | 6 | 1 | 3 |
| `mailbox_grants` | 邮箱与内容资产 | 10 | 4 | 2 |
| `mailboxes` | 邮箱与内容资产 | 17 | 4 | 7 |
| `message_user_states` | 邮箱与内容资产 | 7 | 3 | 3 |
| `messages` | 邮箱与内容资产 | 18 | 3 | 8 |
| `monitor_events` | 派生数据与后台设施 | 8 | 0 | 5 |
| `orphan_objects` | 派生数据与后台设施 | 4 | 0 | 2 |
| `outbound_attachments` | 发送与接收证据 | 3 | 2 | 1 |
| `outbound_attempts` | 发送与接收证据 | 11 | 2 | 3 |
| `outbound_jobs` | 发送与接收证据 | 42 | 5 | 9 |
| `outbound_recipients` | 发送与接收证据 | 8 | 1 | 1 |
| `outbound_templates` | 发送与接收证据 | 8 | 1 | 3 |
| `outbox_events` | 派生数据与后台设施 | 12 | 0 | 3 |
| `permission_profiles` | 身份与管理 | 16 | 1 | 3 |
| `plans` | 身份与管理 | 11 | 0 | 2 |
| `refresh_tokens` | 身份与管理 | 7 | 1 | 5 |
| `runtime_instances` | 派生数据与后台设施 | 3 | 0 | 1 |
| `send_identities` | 发送与接收证据 | 8 | 3 | 5 |
| `sent_asset_attachments` | 邮箱与内容资产 | 3 | 2 | 2 |
| `sent_mail_assets` | 邮箱与内容资产 | 14 | 2 | 3 |
| `sent_mail_items` | 邮箱与内容资产 | 9 | 2 | 4 |
| `smtp_policies` | 派生数据与后台设施 | 9 | 0 | 1 |
| `suppression_list` | 发送与接收证据 | 6 | 1 | 3 |
| `system_settings` | 派生数据与后台设施 | 4 | 0 | 1 |
| `tenant_api_keys` | 身份与管理 | 12 | 2 | 5 |
| `tenant_overrides` | 身份与管理 | 10 | 1 | 2 |
| `tenants` | 身份与管理 | 6 | 1 | 1 |
| `user_permission_overrides` | 身份与管理 | 12 | 1 | 2 |
| `users` | 身份与管理 | 12 | 2 | 4 |
| `webhook_deliveries` | 派生数据与后台设施 | 15 | 1 | 4 |
| `webhook_endpoints` | 派生数据与后台设施 | 9 | 2 | 3 |

## 3. 不能由 FK 单独表达的所有权与保留关系

`messages.raw_object_key`、`mail_attachments.object_key`、`ingest_jobs.raw_object_key` 与 `orphan_objects.object_key` 指向外部对象，而非另一张业务表；数据库删除与外部字节删除不构成单一原子事务。`mail_drafts.payload.attachment_ids` 为 JSON 引用，需要应用检查与回收复检；仅查外键不能宣称附件引用已经完整。

`sent_mail_assets` 不通过外键依赖 `outbound_jobs`；两者保留同一提交来源ID，但业务留存不同。`sent_mail_items` 决定普通员工内容生命周期，仍保留的 job/asset 不自动授予到期正文阅读权（A03）。已发送资产当前缺 BCC（A07），不得从“无字段”推断当时无密送。

`draft_creation_receipts` 是防止已消费/删除UUID复活的墓碑，不是可随草稿删除的缓存。`employee_offboarding_plans` 保存处置指纹/回执，不能仅凭 `users.is_active=false` 推断资产交接完成（A04）。`ingest_recipient_outcomes` 和 `outbound_recipients` 是逐目标持久证据，不能与普通用户删信一起清掉。`mail_documents` / `mail_index_jobs` 可由原件重建，不是原件替代品。封存草稿与创建墓碑本版默认不自动过期。

## 4. 完整外键清单（来自真实 catalog）

`ON DELETE` 未显示时为 PostgreSQL 默认 NO ACTION；不要把默认值读作 CASCADE。完整非FK约束另见机器清单。

| 源表 | 约束 | 定义 |
|---|---|---|
| `admin_invitations` | `admin_invitations_invited_by_fkey` | `FOREIGN KEY (invited_by) REFERENCES users(id) ON DELETE SET NULL` |
| `audit_log` | `audit_log_tenant_id_fkey` | `FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE SET NULL` |
| `company_settings` | `company_settings_tenant_id_fkey` | `FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE` |
| `company_settings` | `company_settings_tenant_id_primary_zone_id_fkey` | `FOREIGN KEY (tenant_id, primary_zone_id) REFERENCES domain_zones(tenant_id, id)` |
| `domain_routes` | `domain_routes_zone_id_fkey` | `FOREIGN KEY (zone_id) REFERENCES domain_zones(id) ON DELETE CASCADE` |
| `domain_zones` | `domain_zones_owner_user_id_fkey` | `FOREIGN KEY (owner_user_id) REFERENCES users(id) ON DELETE SET NULL` |
| `domain_zones` | `domain_zones_parent_zone_id_fkey` | `FOREIGN KEY (parent_zone_id) REFERENCES domain_zones(id) ON DELETE SET NULL` |
| `domain_zones` | `domain_zones_tenant_id_fkey` | `FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE` |
| `draft_creation_receipts` | `draft_creation_receipts_tenant_id_user_id_fkey` | `FOREIGN KEY (tenant_id, user_id) REFERENCES users(tenant_id, id)` |
| `employee_invitations` | `employee_invitations_permission_profile_id_fkey` | `FOREIGN KEY (permission_profile_id) REFERENCES permission_profiles(id)` |
| `employee_invitations` | `employee_invitations_tenant_id_fkey` | `FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE` |
| `employee_offboarding_plans` | `employee_offboarding_plans_tenant_id_fkey` | `FOREIGN KEY (tenant_id) REFERENCES tenants(id)` |
| `employee_offboarding_plans` | `employee_offboarding_plans_tenant_id_successor_id_fkey` | `FOREIGN KEY (tenant_id, successor_id) REFERENCES users(tenant_id, id)` |
| `employee_offboarding_plans` | `employee_offboarding_plans_tenant_id_target_id_fkey` | `FOREIGN KEY (tenant_id, target_id) REFERENCES users(tenant_id, id)` |
| `ingest_recipient_outcomes` | `ingest_recipient_outcomes_job_id_fkey` | `FOREIGN KEY (job_id) REFERENCES ingest_jobs(id) ON DELETE CASCADE` |
| `ingress_daily_usage` | `ingress_daily_usage_tenant_id_fkey` | `FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE` |
| `mail_attachments` | `mail_attachments_tenant_id_mailbox_id_fkey` | `FOREIGN KEY (tenant_id, mailbox_id) REFERENCES mailboxes(tenant_id, id)` |
| `mail_attachments` | `mail_attachments_tenant_id_user_id_fkey` | `FOREIGN KEY (tenant_id, user_id) REFERENCES users(tenant_id, id)` |
| `mail_documents` | `mail_documents_tenant_id_message_id_fkey` | `FOREIGN KEY (tenant_id, message_id) REFERENCES messages(tenant_id, id) ON DELETE CASCADE` |
| `mail_drafts` | `mail_drafts_tenant_id_mailbox_id_fkey` | `FOREIGN KEY (tenant_id, mailbox_id) REFERENCES mailboxes(tenant_id, id) ON DELETE CASCADE` |
| `mail_drafts` | `mail_drafts_tenant_id_user_id_fkey` | `FOREIGN KEY (tenant_id, user_id) REFERENCES users(tenant_id, id) ON DELETE CASCADE` |
| `mail_index_jobs` | `mail_index_jobs_tenant_id_message_id_fkey` | `FOREIGN KEY (tenant_id, message_id) REFERENCES messages(tenant_id, id) ON DELETE CASCADE` |
| `mail_template_grants` | `mail_template_grants_tenant_id_mailbox_id_fkey` | `FOREIGN KEY (tenant_id, mailbox_id) REFERENCES mailboxes(tenant_id, id) ON DELETE CASCADE` |
| `mail_template_grants` | `mail_template_grants_tenant_id_template_id_fkey` | `FOREIGN KEY (tenant_id, template_id) REFERENCES mail_templates(tenant_id, id)` |
| `mail_template_grants` | `mail_template_grants_tenant_id_user_id_fkey` | `FOREIGN KEY (tenant_id, user_id) REFERENCES users(tenant_id, id) ON DELETE CASCADE` |
| `mail_template_versions` | `mail_template_versions_tenant_id_template_id_fkey` | `FOREIGN KEY (tenant_id, template_id) REFERENCES mail_templates(tenant_id, id)` |
| `mail_templates` | `mail_templates_tenant_id_fkey` | `FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE` |
| `mailbox_event_log` | `mailbox_event_log_tenant_id_fkey` | `FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE` |
| `mailbox_grants` | `mailbox_grants_granted_by_fkey` | `FOREIGN KEY (granted_by) REFERENCES users(id) ON DELETE SET NULL` |
| `mailbox_grants` | `mailbox_grants_tenant_id_fkey` | `FOREIGN KEY (tenant_id) REFERENCES tenants(id)` |
| `mailbox_grants` | `mailbox_grants_tenant_id_mailbox_id_fkey` | `FOREIGN KEY (tenant_id, mailbox_id) REFERENCES mailboxes(tenant_id, id) ON DELETE CASCADE` |
| `mailbox_grants` | `mailbox_grants_tenant_id_user_id_fkey` | `FOREIGN KEY (tenant_id, user_id) REFERENCES users(tenant_id, id) ON DELETE CASCADE` |
| `mailboxes` | `mailbox_owner_same_tenant` | `FOREIGN KEY (tenant_id, owner_user_id) REFERENCES users(tenant_id, id)` |
| `mailboxes` | `mailboxes_route_id_fkey` | `FOREIGN KEY (route_id) REFERENCES domain_routes(id) ON DELETE SET NULL` |
| `mailboxes` | `mailboxes_tenant_id_fkey` | `FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE` |
| `mailboxes` | `mailboxes_zone_id_fkey` | `FOREIGN KEY (zone_id) REFERENCES domain_zones(id)` |
| `message_user_states` | `message_user_states_tenant_id_mailbox_id_fkey` | `FOREIGN KEY (tenant_id, mailbox_id) REFERENCES mailboxes(tenant_id, id) ON DELETE CASCADE` |
| `message_user_states` | `message_user_states_tenant_id_message_id_fkey` | `FOREIGN KEY (tenant_id, message_id) REFERENCES messages(tenant_id, id) ON DELETE CASCADE` |
| `message_user_states` | `message_user_states_tenant_id_user_id_fkey` | `FOREIGN KEY (tenant_id, user_id) REFERENCES users(tenant_id, id) ON DELETE CASCADE` |
| `messages` | `messages_mailbox_id_fkey` | `FOREIGN KEY (mailbox_id) REFERENCES mailboxes(id) ON DELETE CASCADE` |
| `messages` | `messages_tenant_id_fkey` | `FOREIGN KEY (tenant_id) REFERENCES tenants(id)` |
| `messages` | `messages_zone_id_fkey` | `FOREIGN KEY (zone_id) REFERENCES domain_zones(id)` |
| `outbound_attachments` | `outbound_attachments_tenant_id_attachment_id_fkey` | `FOREIGN KEY (tenant_id, attachment_id) REFERENCES mail_attachments(tenant_id, id)` |
| `outbound_attachments` | `outbound_attachments_tenant_id_job_id_fkey` | `FOREIGN KEY (tenant_id, job_id) REFERENCES outbound_jobs(tenant_id, id) ON DELETE CASCADE` |
| `outbound_attempts` | `outbound_attempts_job_id_fkey` | `FOREIGN KEY (job_id) REFERENCES outbound_jobs(id) ON DELETE CASCADE` |
| `outbound_attempts` | `outbound_attempts_tenant_id_fkey` | `FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE` |
| `outbound_jobs` | `outbound_jobs_api_key_id_fkey` | `FOREIGN KEY (api_key_id) REFERENCES tenant_api_keys(id) ON DELETE SET NULL` |
| `outbound_jobs` | `outbound_jobs_tenant_id_fkey` | `FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE` |
| `outbound_jobs` | `outbound_jobs_user_id_fkey` | `FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE SET NULL` |
| `outbound_jobs` | `outbound_jobs_zone_id_fkey` | `FOREIGN KEY (zone_id) REFERENCES domain_zones(id)` |
| `outbound_jobs` | `outbound_template_same_tenant` | `FOREIGN KEY (tenant_id, template_version_id) REFERENCES mail_template_versions(tenant_id, id)` |
| `outbound_recipients` | `outbound_recipients_tenant_id_job_id_fkey` | `FOREIGN KEY (tenant_id, job_id) REFERENCES outbound_jobs(tenant_id, id) ON DELETE CASCADE` |
| `outbound_templates` | `outbound_templates_tenant_id_fkey` | `FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE` |
| `permission_profiles` | `permission_profiles_tenant_id_fkey` | `FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE` |
| `refresh_tokens` | `refresh_tokens_user_id_fkey` | `FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE` |
| `send_identities` | `send_identities_mailbox_id_fkey` | `FOREIGN KEY (mailbox_id) REFERENCES mailboxes(id) ON DELETE SET NULL` |
| `send_identities` | `send_identities_tenant_id_fkey` | `FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE` |
| `send_identities` | `send_identities_zone_id_fkey` | `FOREIGN KEY (zone_id) REFERENCES domain_zones(id) ON DELETE CASCADE` |
| `sent_asset_attachments` | `sent_asset_attachments_tenant_id_asset_id_fkey` | `FOREIGN KEY (tenant_id, asset_id) REFERENCES sent_mail_assets(tenant_id, id) ON DELETE CASCADE` |
| `sent_asset_attachments` | `sent_asset_attachments_tenant_id_attachment_id_fkey` | `FOREIGN KEY (tenant_id, attachment_id) REFERENCES mail_attachments(tenant_id, id)` |
| `sent_mail_assets` | `sent_mail_assets_tenant_id_sender_mailbox_id_fkey` | `FOREIGN KEY (tenant_id, sender_mailbox_id) REFERENCES mailboxes(tenant_id, id)` |
| `sent_mail_assets` | `sent_mail_assets_tenant_id_zone_id_fkey` | `FOREIGN KEY (tenant_id, zone_id) REFERENCES domain_zones(tenant_id, id)` |
| `sent_mail_items` | `sent_mail_items_tenant_id_asset_id_fkey` | `FOREIGN KEY (tenant_id, asset_id) REFERENCES sent_mail_assets(tenant_id, id)` |
| `sent_mail_items` | `sent_mail_items_tenant_id_mailbox_id_fkey` | `FOREIGN KEY (tenant_id, mailbox_id) REFERENCES mailboxes(tenant_id, id)` |
| `suppression_list` | `suppression_list_tenant_id_fkey` | `FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE` |
| `tenant_api_keys` | `fk_api_keys_owner_user` | `FOREIGN KEY (owner_user_id) REFERENCES users(id) ON DELETE CASCADE NOT VALID` |
| `tenant_api_keys` | `tenant_api_keys_tenant_id_fkey` | `FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE` |
| `tenant_overrides` | `tenant_overrides_tenant_id_fkey` | `FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE` |
| `tenants` | `tenants_plan_id_fkey` | `FOREIGN KEY (plan_id) REFERENCES plans(id)` |
| `user_permission_overrides` | `user_permission_overrides_user_id_fkey` | `FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE` |
| `users` | `users_permission_profile_id_fkey` | `FOREIGN KEY (permission_profile_id) REFERENCES permission_profiles(id) ON DELETE SET NULL` |
| `users` | `users_tenant_id_fkey` | `FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE` |
| `webhook_deliveries` | `webhook_deliveries_event_id_fkey` | `FOREIGN KEY (event_id) REFERENCES outbox_events(id) ON DELETE CASCADE` |
| `webhook_endpoints` | `webhook_endpoints_created_by_fkey` | `FOREIGN KEY (created_by) REFERENCES users(id) ON DELETE SET NULL` |
| `webhook_endpoints` | `webhook_endpoints_tenant_id_fkey` | `FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE` |

## 5. 业务触发器与不可变边界

| 表 | 触发器 | 函数 |
|---|---|---|
| `mail_template_versions` | `immutable_mail_template_version` | `protect_mail_template_version` |
| `messages` | `enqueue_mail_index` | `enqueue_mail_index` |
| `messages` | `mailbox_change` | `log_mailbox_change` |
| `outbound_attachments` | `pin_sent_attachment` | `pin_sent_attachment` |
| `outbound_jobs` | `archive_outbound_mail` | `archive_outbound_mail` |
| `outbound_jobs` | `fence_employee_enqueue` | `fence_employee_enqueue` |
| `sent_mail_assets` | `protect_sent_asset` | `protect_sent_asset` |

`protect_mail_template_version` 保护已发布模板内容，`protect_sent_asset` 保护发件资产。`fence_employee_enqueue` 是当前员工写入入队的数据库保护；`archive_outbound_mail` 与 `pin_sent_attachment` 将归档/引用纳入现有提交事务；`enqueue_mail_index`、`log_mailbox_change` 负责派生工作和事件。权限修复不得移除这些保护换取测试通过；函数完整定义保存在机器清单，后续变更必须新增 migration。

## 6. 全部索引清单

这些是已存在索引，不是建议新增索引；性能有效性要在P0基准及P10测量，不能仅凭索引存在断言查询有界。

| 表 | 索引 | 定义 |
|---|---|---|
| `admin_invitations` | `admin_invitations_invite_code_key` | `CREATE UNIQUE INDEX admin_invitations_invite_code_key ON public.admin_invitations USING btree (invite_code)` |
| `admin_invitations` | `admin_invitations_pkey` | `CREATE UNIQUE INDEX admin_invitations_pkey ON public.admin_invitations USING btree (id)` |
| `admin_invitations` | `idx_invitations_code` | `CREATE INDEX idx_invitations_code ON public.admin_invitations USING btree (invite_code)` |
| `admin_invitations` | `idx_invitations_email` | `CREATE INDEX idx_invitations_email ON public.admin_invitations USING btree (lower((email)::text))` |
| `audit_log` | `audit_log_pkey` | `CREATE UNIQUE INDEX audit_log_pkey ON public.audit_log USING btree (id)` |
| `audit_log` | `idx_audit_created_at` | `CREATE INDEX idx_audit_created_at ON public.audit_log USING btree (created_at DESC)` |
| `audit_log` | `idx_audit_tenant_time` | `CREATE INDEX idx_audit_tenant_time ON public.audit_log USING btree (tenant_id, created_at DESC)` |
| `company_settings` | `company_settings_pkey` | `CREATE UNIQUE INDEX company_settings_pkey ON public.company_settings USING btree (tenant_id)` |
| `domain_routes` | `domain_routes_pkey` | `CREATE UNIQUE INDEX domain_routes_pkey ON public.domain_routes USING btree (id)` |
| `domain_routes` | `idx_routes_zone` | `CREATE INDEX idx_routes_zone ON public.domain_routes USING btree (zone_id)` |
| `domain_zones` | `domain_zones_domain_key` | `CREATE UNIQUE INDEX domain_zones_domain_key ON public.domain_zones USING btree (domain)` |
| `domain_zones` | `domain_zones_pkey` | `CREATE UNIQUE INDEX domain_zones_pkey ON public.domain_zones USING btree (id)` |
| `domain_zones` | `domain_zones_tenant_identity` | `CREATE UNIQUE INDEX domain_zones_tenant_identity ON public.domain_zones USING btree (tenant_id, id)` |
| `domain_zones` | `idx_zones_domain` | `CREATE INDEX idx_zones_domain ON public.domain_zones USING btree (domain)` |
| `domain_zones` | `idx_zones_owner` | `CREATE INDEX idx_zones_owner ON public.domain_zones USING btree (owner_user_id) WHERE (owner_user_id IS NOT NULL)` |
| `domain_zones` | `idx_zones_parent` | `CREATE INDEX idx_zones_parent ON public.domain_zones USING btree (parent_zone_id) WHERE (parent_zone_id IS NOT NULL)` |
| `domain_zones` | `idx_zones_tenant` | `CREATE INDEX idx_zones_tenant ON public.domain_zones USING btree (tenant_id)` |
| `domain_zones` | `idx_zones_visibility` | `CREATE INDEX idx_zones_visibility ON public.domain_zones USING btree (visibility)` |
| `draft_creation_receipts` | `draft_creation_receipts_pkey` | `CREATE UNIQUE INDEX draft_creation_receipts_pkey ON public.draft_creation_receipts USING btree (id)` |
| `draft_creation_receipts` | `draft_receipts_owner` | `CREATE INDEX draft_receipts_owner ON public.draft_creation_receipts USING btree (tenant_id, user_id)` |
| `employee_invitations` | `employee_invitation_tenant` | `CREATE INDEX employee_invitation_tenant ON public.employee_invitations USING btree (tenant_id, created_at DESC)` |
| `employee_invitations` | `employee_invitations_pkey` | `CREATE UNIQUE INDEX employee_invitations_pkey ON public.employee_invitations USING btree (id)` |
| `employee_invitations` | `employee_invitations_token_hash_key` | `CREATE UNIQUE INDEX employee_invitations_token_hash_key ON public.employee_invitations USING btree (token_hash)` |
| `employee_offboarding_plans` | `employee_offboarding_plans_pkey` | `CREATE UNIQUE INDEX employee_offboarding_plans_pkey ON public.employee_offboarding_plans USING btree (id)` |
| `employee_offboarding_plans` | `offboarding_plan_company` | `CREATE INDEX offboarding_plan_company ON public.employee_offboarding_plans USING btree (tenant_id, created_at DESC)` |
| `goose_db_version` | `goose_db_version_pkey` | `CREATE UNIQUE INDEX goose_db_version_pkey ON public.goose_db_version USING btree (id)` |
| `ingest_jobs` | `idx_ingest_jobs_lease` | `CREATE INDEX idx_ingest_jobs_lease ON public.ingest_jobs USING btree (state, lease_until) WHERE ((state)::text = 'processing'::text)` |
| `ingest_jobs` | `idx_ingest_jobs_pending` | `CREATE INDEX idx_ingest_jobs_pending ON public.ingest_jobs USING btree (state, next_attempt_at, created_at)` |
| `ingest_jobs` | `idx_ingest_jobs_raw_state` | `CREATE INDEX idx_ingest_jobs_raw_state ON public.ingest_jobs USING btree (raw_object_key, state)` |
| `ingest_jobs` | `idx_ingress_managed_ready` | `CREATE INDEX idx_ingress_managed_ready ON public.ingest_jobs USING btree (next_attempt_at, created_at) WHERE (recovery_managed AND ((state)::text = ANY ((ARRAY['pending'::character varying, 'retry'::character varying, 'processing'::character varying])::text[])))` |
| `ingest_jobs` | `ingest_jobs_pkey` | `CREATE UNIQUE INDEX ingest_jobs_pkey ON public.ingest_jobs USING btree (id)` |
| `ingest_recipient_outcomes` | `idx_ingest_recipient_outcomes_tenant` | `CREATE INDEX idx_ingest_recipient_outcomes_tenant ON public.ingest_recipient_outcomes USING btree (tenant_id, job_id)` |
| `ingest_recipient_outcomes` | `ingest_recipient_outcomes_pkey` | `CREATE UNIQUE INDEX ingest_recipient_outcomes_pkey ON public.ingest_recipient_outcomes USING btree (job_id, mailbox_id)` |
| `ingress_daily_usage` | `ingress_daily_usage_pkey` | `CREATE UNIQUE INDEX ingress_daily_usage_pkey ON public.ingress_daily_usage USING btree (tenant_id, day)` |
| `mail_attachments` | `mail_attachments_object_key_key` | `CREATE UNIQUE INDEX mail_attachments_object_key_key ON public.mail_attachments USING btree (object_key)` |
| `mail_attachments` | `mail_attachments_pkey` | `CREATE UNIQUE INDEX mail_attachments_pkey ON public.mail_attachments USING btree (id)` |
| `mail_attachments` | `mail_attachments_tenant_id_id_key` | `CREATE UNIQUE INDEX mail_attachments_tenant_id_id_key ON public.mail_attachments USING btree (tenant_id, id)` |
| `mail_documents` | `mail_documents_pkey` | `CREATE UNIQUE INDEX mail_documents_pkey ON public.mail_documents USING btree (message_id)` |
| `mail_documents` | `mail_documents_search` | `CREATE INDEX mail_documents_search ON public.mail_documents USING gin (search_text gin_trgm_ops)` |
| `mail_documents` | `mail_documents_thread` | `CREATE INDEX mail_documents_thread ON public.mail_documents USING btree (tenant_id, thread_key)` |
| `mail_drafts` | `mail_drafts_pkey` | `CREATE UNIQUE INDEX mail_drafts_pkey ON public.mail_drafts USING btree (id)` |
| `mail_drafts` | `mail_drafts_user` | `CREATE INDEX mail_drafts_user ON public.mail_drafts USING btree (tenant_id, user_id, updated_at DESC)` |
| `mail_index_jobs` | `mail_index_claim` | `CREATE INDEX mail_index_claim ON public.mail_index_jobs USING btree (state, next_attempt_at)` |
| `mail_index_jobs` | `mail_index_jobs_pkey` | `CREATE UNIQUE INDEX mail_index_jobs_pkey ON public.mail_index_jobs USING btree (message_id)` |
| `mail_template_grants` | `mail_template_grants_pkey` | `CREATE UNIQUE INDEX mail_template_grants_pkey ON public.mail_template_grants USING btree (template_id, mailbox_id, user_id)` |
| `mail_template_versions` | `mail_template_versions_pkey` | `CREATE UNIQUE INDEX mail_template_versions_pkey ON public.mail_template_versions USING btree (id)` |
| `mail_template_versions` | `mail_template_versions_template_id_version_key` | `CREATE UNIQUE INDEX mail_template_versions_template_id_version_key ON public.mail_template_versions USING btree (template_id, version)` |
| `mail_template_versions` | `mail_template_versions_tenant_id_id_key` | `CREATE UNIQUE INDEX mail_template_versions_tenant_id_id_key ON public.mail_template_versions USING btree (tenant_id, id)` |
| `mail_templates` | `mail_templates_pkey` | `CREATE UNIQUE INDEX mail_templates_pkey ON public.mail_templates USING btree (id)` |
| `mail_templates` | `mail_templates_tenant_id_id_key` | `CREATE UNIQUE INDEX mail_templates_tenant_id_id_key ON public.mail_templates USING btree (tenant_id, id)` |
| `mail_templates` | `mail_templates_tenant_id_name_key` | `CREATE UNIQUE INDEX mail_templates_tenant_id_name_key ON public.mail_templates USING btree (tenant_id, name)` |
| `mailbox_event_log` | `mailbox_event_cursor` | `CREATE INDEX mailbox_event_cursor ON public.mailbox_event_log USING btree (tenant_id, mailbox_id, sequence)` |
| `mailbox_event_log` | `mailbox_event_expiry` | `CREATE INDEX mailbox_event_expiry ON public.mailbox_event_log USING btree (created_at)` |
| `mailbox_event_log` | `mailbox_event_log_pkey` | `CREATE UNIQUE INDEX mailbox_event_log_pkey ON public.mailbox_event_log USING btree (sequence)` |
| `mailbox_grants` | `mailbox_grants_mailbox_id_user_id_key` | `CREATE UNIQUE INDEX mailbox_grants_mailbox_id_user_id_key ON public.mailbox_grants USING btree (mailbox_id, user_id)` |
| `mailbox_grants` | `mailbox_grants_user` | `CREATE INDEX mailbox_grants_user ON public.mailbox_grants USING btree (tenant_id, user_id)` |
| `mailboxes` | `idx_mailboxes_address` | `CREATE INDEX idx_mailboxes_address ON public.mailboxes USING btree (full_address)` |
| `mailboxes` | `idx_mailboxes_expires` | `CREATE INDEX idx_mailboxes_expires ON public.mailboxes USING btree (expires_at) WHERE (expires_at IS NOT NULL)` |
| `mailboxes` | `idx_mailboxes_tenant` | `CREATE INDEX idx_mailboxes_tenant ON public.mailboxes USING btree (tenant_id)` |
| `mailboxes` | `idx_mailboxes_zone` | `CREATE INDEX idx_mailboxes_zone ON public.mailboxes USING btree (zone_id)` |
| `mailboxes` | `mailboxes_full_address_key` | `CREATE UNIQUE INDEX mailboxes_full_address_key ON public.mailboxes USING btree (full_address)` |
| `mailboxes` | `mailboxes_pkey` | `CREATE UNIQUE INDEX mailboxes_pkey ON public.mailboxes USING btree (id)` |
| `mailboxes` | `mailboxes_tenant_identity` | `CREATE UNIQUE INDEX mailboxes_tenant_identity ON public.mailboxes USING btree (tenant_id, id)` |
| `message_user_states` | `message_user_states_mailbox_user` | `CREATE INDEX message_user_states_mailbox_user ON public.message_user_states USING btree (mailbox_id, user_id)` |
| `message_user_states` | `message_user_states_pkey` | `CREATE UNIQUE INDEX message_user_states_pkey ON public.message_user_states USING btree (mailbox_id, message_id, user_id)` |
| `message_user_states` | `message_user_states_tenant_id_mailbox_id_message_id_user_id_key` | `CREATE UNIQUE INDEX message_user_states_tenant_id_mailbox_id_message_id_user_id_key ON public.message_user_states USING btree (tenant_id, mailbox_id, message_id, user_id)` |
| `messages` | `idx_messages_expires` | `CREATE INDEX idx_messages_expires ON public.messages USING btree (expires_at)` |
| `messages` | `idx_messages_mailbox_rcvd` | `CREATE INDEX idx_messages_mailbox_rcvd ON public.messages USING btree (mailbox_id, received_at DESC)` |
| `messages` | `idx_messages_raw_object_key` | `CREATE INDEX idx_messages_raw_object_key ON public.messages USING btree (raw_object_key) WHERE (raw_object_key IS NOT NULL)` |
| `messages` | `idx_messages_tenant_expires` | `CREATE INDEX idx_messages_tenant_expires ON public.messages USING btree (tenant_id, expires_at)` |
| `messages` | `messages_pkey` | `CREATE UNIQUE INDEX messages_pkey ON public.messages USING btree (id)` |
| `messages` | `messages_tenant_identity` | `CREATE UNIQUE INDEX messages_tenant_identity ON public.messages USING btree (tenant_id, id)` |
| `messages` | `messages_trash_cleanup` | `CREATE INDEX messages_trash_cleanup ON public.messages USING btree (purge_after) WHERE (deleted_at IS NOT NULL)` |
| `messages` | `messages_visible_mailbox` | `CREATE INDEX messages_visible_mailbox ON public.messages USING btree (mailbox_id, received_at DESC, id) WHERE (deleted_at IS NULL)` |
| `monitor_events` | `idx_monitor_events_at` | `CREATE INDEX idx_monitor_events_at ON public.monitor_events USING btree (at DESC)` |
| `monitor_events` | `idx_monitor_events_mailbox` | `CREATE INDEX idx_monitor_events_mailbox ON public.monitor_events USING btree (mailbox)` |
| `monitor_events` | `idx_monitor_events_sender` | `CREATE INDEX idx_monitor_events_sender ON public.monitor_events USING btree (sender)` |
| `monitor_events` | `idx_monitor_events_type` | `CREATE INDEX idx_monitor_events_type ON public.monitor_events USING btree (type)` |
| `monitor_events` | `monitor_events_pkey` | `CREATE UNIQUE INDEX monitor_events_pkey ON public.monitor_events USING btree (id)` |
| `orphan_objects` | `idx_orphan_objects_last_failed` | `CREATE INDEX idx_orphan_objects_last_failed ON public.orphan_objects USING btree (last_failed_at)` |
| `orphan_objects` | `orphan_objects_pkey` | `CREATE UNIQUE INDEX orphan_objects_pkey ON public.orphan_objects USING btree (object_key)` |
| `outbound_attachments` | `outbound_attachments_pkey` | `CREATE UNIQUE INDEX outbound_attachments_pkey ON public.outbound_attachments USING btree (job_id, attachment_id)` |
| `outbound_attempts` | `idx_outbound_attempts_job` | `CREATE INDEX idx_outbound_attempts_job ON public.outbound_attempts USING btree (job_id)` |
| `outbound_attempts` | `idx_outbound_attempts_tenant` | `CREATE INDEX idx_outbound_attempts_tenant ON public.outbound_attempts USING btree (tenant_id)` |
| `outbound_attempts` | `outbound_attempts_pkey` | `CREATE UNIQUE INDEX outbound_attempts_pkey ON public.outbound_attempts USING btree (id)` |
| `outbound_jobs` | `idx_outbound_lease` | `CREATE INDEX idx_outbound_lease ON public.outbound_jobs USING btree (state, lease_until) WHERE (state = 'processing'::outbound_state)` |
| `outbound_jobs` | `idx_outbound_pending` | `CREATE INDEX idx_outbound_pending ON public.outbound_jobs USING btree (state, next_attempt_at, created_at) WHERE (state = ANY (ARRAY['pending'::outbound_state, 'retry'::outbound_state]))` |
| `outbound_jobs` | `idx_outbound_tenant_api_key_date` | `CREATE INDEX idx_outbound_tenant_api_key_date ON public.outbound_jobs USING btree (tenant_id, api_key_id, created_at DESC) WHERE (api_key_id IS NOT NULL)` |
| `outbound_jobs` | `idx_outbound_tenant_date` | `CREATE INDEX idx_outbound_tenant_date ON public.outbound_jobs USING btree (tenant_id, created_at DESC)` |
| `outbound_jobs` | `idx_outbound_user_date` | `CREATE INDEX idx_outbound_user_date ON public.outbound_jobs USING btree (user_id, created_at DESC) WHERE (user_id IS NOT NULL)` |
| `outbound_jobs` | `outbound_jobs_draft` | `CREATE INDEX outbound_jobs_draft ON public.outbound_jobs USING btree (tenant_id, draft_id) WHERE (draft_id IS NOT NULL)` |
| `outbound_jobs` | `outbound_jobs_pkey` | `CREATE UNIQUE INDEX outbound_jobs_pkey ON public.outbound_jobs USING btree (id)` |
| `outbound_jobs` | `outbound_submission_idempotency` | `CREATE UNIQUE INDEX outbound_submission_idempotency ON public.outbound_jobs USING btree (tenant_id, submit_actor, idempotency_key) WHERE (idempotency_key <> ''::text)` |
| `outbound_jobs` | `outbound_tenant_identity` | `CREATE UNIQUE INDEX outbound_tenant_identity ON public.outbound_jobs USING btree (tenant_id, id)` |
| `outbound_recipients` | `outbound_recipients_pkey` | `CREATE UNIQUE INDEX outbound_recipients_pkey ON public.outbound_recipients USING btree (job_id, address)` |
| `outbound_templates` | `idx_outbound_templates_tenant_name` | `CREATE INDEX idx_outbound_templates_tenant_name ON public.outbound_templates USING btree (tenant_id, name)` |
| `outbound_templates` | `outbound_templates_pkey` | `CREATE UNIQUE INDEX outbound_templates_pkey ON public.outbound_templates USING btree (id)` |
| `outbound_templates` | `outbound_templates_tenant_id_name_key` | `CREATE UNIQUE INDEX outbound_templates_tenant_id_name_key ON public.outbound_templates USING btree (tenant_id, name)` |
| `outbox_events` | `idx_outbox_events_lease` | `CREATE INDEX idx_outbox_events_lease ON public.outbox_events USING btree (state, lease_until) WHERE ((state)::text = 'processing'::text)` |
| `outbox_events` | `idx_outbox_events_pending` | `CREATE INDEX idx_outbox_events_pending ON public.outbox_events USING btree (state, next_attempt_at, created_at)` |
| `outbox_events` | `outbox_events_pkey` | `CREATE UNIQUE INDEX outbox_events_pkey ON public.outbox_events USING btree (id)` |
| `permission_profiles` | `idx_perm_profiles_name_system` | `CREATE UNIQUE INDEX idx_perm_profiles_name_system ON public.permission_profiles USING btree (name) WHERE (tenant_id IS NULL)` |
| `permission_profiles` | `idx_perm_profiles_name_tenant` | `CREATE UNIQUE INDEX idx_perm_profiles_name_tenant ON public.permission_profiles USING btree (tenant_id, name) WHERE (tenant_id IS NOT NULL)` |
| `permission_profiles` | `permission_profiles_pkey` | `CREATE UNIQUE INDEX permission_profiles_pkey ON public.permission_profiles USING btree (id)` |
| `plans` | `plans_name_key` | `CREATE UNIQUE INDEX plans_name_key ON public.plans USING btree (name)` |
| `plans` | `plans_pkey` | `CREATE UNIQUE INDEX plans_pkey ON public.plans USING btree (id)` |
| `refresh_tokens` | `idx_refresh_tokens_expires` | `CREATE INDEX idx_refresh_tokens_expires ON public.refresh_tokens USING btree (expires_at)` |
| `refresh_tokens` | `idx_refresh_tokens_user` | `CREATE INDEX idx_refresh_tokens_user ON public.refresh_tokens USING btree (user_id)` |
| `refresh_tokens` | `refresh_tokens_family_idx` | `CREATE INDEX refresh_tokens_family_idx ON public.refresh_tokens USING btree (family_id)` |
| `refresh_tokens` | `refresh_tokens_pkey` | `CREATE UNIQUE INDEX refresh_tokens_pkey ON public.refresh_tokens USING btree (id)` |
| `refresh_tokens` | `refresh_tokens_token_hash_key` | `CREATE UNIQUE INDEX refresh_tokens_token_hash_key ON public.refresh_tokens USING btree (token_hash)` |
| `runtime_instances` | `runtime_instances_pkey` | `CREATE UNIQUE INDEX runtime_instances_pkey ON public.runtime_instances USING btree (id)` |
| `send_identities` | `idx_send_identities_address` | `CREATE INDEX idx_send_identities_address ON public.send_identities USING btree (address)` |
| `send_identities` | `idx_send_identities_tenant` | `CREATE INDEX idx_send_identities_tenant ON public.send_identities USING btree (tenant_id)` |
| `send_identities` | `idx_send_identities_zone` | `CREATE INDEX idx_send_identities_zone ON public.send_identities USING btree (zone_id)` |
| `send_identities` | `send_identities_pkey` | `CREATE UNIQUE INDEX send_identities_pkey ON public.send_identities USING btree (id)` |
| `send_identities` | `send_identities_tenant_id_address_identity_type_key` | `CREATE UNIQUE INDEX send_identities_tenant_id_address_identity_type_key ON public.send_identities USING btree (tenant_id, address, identity_type)` |
| `sent_asset_attachments` | `sent_asset_attachment_references` | `CREATE INDEX sent_asset_attachment_references ON public.sent_asset_attachments USING btree (attachment_id)` |
| `sent_asset_attachments` | `sent_asset_attachments_pkey` | `CREATE UNIQUE INDEX sent_asset_attachments_pkey ON public.sent_asset_attachments USING btree (asset_id, attachment_id)` |
| `sent_mail_assets` | `sent_assets_search` | `CREATE INDEX sent_assets_search ON public.sent_mail_assets USING gin (search_text gin_trgm_ops)` |
| `sent_mail_assets` | `sent_mail_assets_pkey` | `CREATE UNIQUE INDEX sent_mail_assets_pkey ON public.sent_mail_assets USING btree (id)` |
| `sent_mail_assets` | `sent_mail_assets_tenant_id_id_key` | `CREATE UNIQUE INDEX sent_mail_assets_tenant_id_id_key ON public.sent_mail_assets USING btree (tenant_id, id)` |
| `sent_mail_items` | `sent_items_expiry` | `CREATE INDEX sent_items_expiry ON public.sent_mail_items USING btree (expires_at) WHERE (expires_at IS NOT NULL)` |
| `sent_mail_items` | `sent_items_mailbox` | `CREATE INDEX sent_items_mailbox ON public.sent_mail_items USING btree (tenant_id, mailbox_id, created_at DESC, asset_id DESC)` |
| `sent_mail_items` | `sent_items_purge` | `CREATE INDEX sent_items_purge ON public.sent_mail_items USING btree (purge_after) WHERE (purge_after IS NOT NULL)` |
| `sent_mail_items` | `sent_mail_items_pkey` | `CREATE UNIQUE INDEX sent_mail_items_pkey ON public.sent_mail_items USING btree (asset_id)` |
| `smtp_policies` | `smtp_policies_pkey` | `CREATE UNIQUE INDEX smtp_policies_pkey ON public.smtp_policies USING btree (id)` |
| `suppression_list` | `idx_suppression_tenant_addr` | `CREATE INDEX idx_suppression_tenant_addr ON public.suppression_list USING btree (tenant_id, lower((address)::text))` |
| `suppression_list` | `suppression_list_pkey` | `CREATE UNIQUE INDEX suppression_list_pkey ON public.suppression_list USING btree (id)` |
| `suppression_list` | `suppression_list_tenant_id_address_key` | `CREATE UNIQUE INDEX suppression_list_tenant_id_address_key ON public.suppression_list USING btree (tenant_id, address)` |
| `system_settings` | `system_settings_pkey` | `CREATE UNIQUE INDEX system_settings_pkey ON public.system_settings USING btree (key)` |
| `tenant_api_keys` | `idx_api_keys_hash` | `CREATE UNIQUE INDEX idx_api_keys_hash ON public.tenant_api_keys USING btree (key_hash)` |
| `tenant_api_keys` | `idx_api_keys_owner` | `CREATE INDEX idx_api_keys_owner ON public.tenant_api_keys USING btree (owner_user_id) WHERE (owner_user_id IS NOT NULL)` |
| `tenant_api_keys` | `idx_api_keys_prefix` | `CREATE INDEX idx_api_keys_prefix ON public.tenant_api_keys USING btree (key_prefix)` |
| `tenant_api_keys` | `idx_api_keys_tenant` | `CREATE INDEX idx_api_keys_tenant ON public.tenant_api_keys USING btree (tenant_id)` |
| `tenant_api_keys` | `tenant_api_keys_pkey` | `CREATE UNIQUE INDEX tenant_api_keys_pkey ON public.tenant_api_keys USING btree (id)` |
| `tenant_overrides` | `tenant_overrides_pkey` | `CREATE UNIQUE INDEX tenant_overrides_pkey ON public.tenant_overrides USING btree (id)` |
| `tenant_overrides` | `tenant_overrides_tenant_id_key` | `CREATE UNIQUE INDEX tenant_overrides_tenant_id_key ON public.tenant_overrides USING btree (tenant_id)` |
| `tenants` | `tenants_pkey` | `CREATE UNIQUE INDEX tenants_pkey ON public.tenants USING btree (id)` |
| `user_permission_overrides` | `user_permission_overrides_pkey` | `CREATE UNIQUE INDEX user_permission_overrides_pkey ON public.user_permission_overrides USING btree (id)` |
| `user_permission_overrides` | `user_permission_overrides_user_id_key` | `CREATE UNIQUE INDEX user_permission_overrides_user_id_key ON public.user_permission_overrides USING btree (user_id)` |
| `users` | `idx_users_email` | `CREATE UNIQUE INDEX idx_users_email ON public.users USING btree (lower((email)::text))` |
| `users` | `idx_users_tenant` | `CREATE INDEX idx_users_tenant ON public.users USING btree (tenant_id)` |
| `users` | `users_pkey` | `CREATE UNIQUE INDEX users_pkey ON public.users USING btree (id)` |
| `users` | `users_tenant_identity` | `CREATE UNIQUE INDEX users_tenant_identity ON public.users USING btree (tenant_id, id)` |
| `webhook_deliveries` | `idx_webhook_deliveries_event_url` | `CREATE UNIQUE INDEX idx_webhook_deliveries_event_url ON public.webhook_deliveries USING btree (event_id, url)` |
| `webhook_deliveries` | `idx_webhook_deliveries_lease` | `CREATE INDEX idx_webhook_deliveries_lease ON public.webhook_deliveries USING btree (state, lease_until) WHERE ((state)::text = 'processing'::text)` |
| `webhook_deliveries` | `idx_webhook_deliveries_pending` | `CREATE INDEX idx_webhook_deliveries_pending ON public.webhook_deliveries USING btree (state, next_attempt_at, created_at)` |
| `webhook_deliveries` | `webhook_deliveries_pkey` | `CREATE UNIQUE INDEX webhook_deliveries_pkey ON public.webhook_deliveries USING btree (id)` |
| `webhook_endpoints` | `idx_webhook_endpoints_active` | `CREATE INDEX idx_webhook_endpoints_active ON public.webhook_endpoints USING btree (tenant_id) WHERE (is_active = true)` |
| `webhook_endpoints` | `idx_webhook_endpoints_tenant` | `CREATE INDEX idx_webhook_endpoints_tenant ON public.webhook_endpoints USING btree (tenant_id)` |
| `webhook_endpoints` | `webhook_endpoints_pkey` | `CREATE UNIQUE INDEX webhook_endpoints_pkey ON public.webhook_endpoints USING btree (id)` |

## 7. 历史迁移、Down 与增量分配

| 文件 | SHA-256 | 实际 Down 行为 |
|---|---|---|
| `00001_baseline.sql` | `233a6e0a5dea5e08de370aa3d8cd7e69a61f9cfaf2421846e23494de0e4e0549` | SELECT 1；不恢复旧schema，不是有效数据回退 |
| `00002_mailbox_grants.sql` | `51615b38624aaa3010c5b8e7ff84ddafbd22046fe5f8992bfc2c31b5592a6ba5` | RAISE EXCEPTION；显式拒绝降级，要求协调恢复 |
| `00003_refresh_token_family.sql` | `11a168f78cf5b3b11e369f3c089e6252258bfa0a5cdbfc192b1581284d3c541b` | RAISE EXCEPTION；显式拒绝降级，要求协调恢复 |
| `00004_owned_mailbox_private.sql` | `e8abcdd3026d6586afd20d0a7375c9b9a10bcfab94b9a5ddcace0efa3899add1` | RAISE EXCEPTION；显式拒绝降级，要求协调恢复 |
| `00005_company_workflows.sql` | `f6a66202fd0f2bdc9d3b6ceecb05cee5f6bd96f620d665e873222938b0e5ba50` | RAISE EXCEPTION；显式拒绝降级，要求协调恢复 |
| `00006_draft_submission.sql` | `8c02d23d9b06da8524e4ce67ade38cb72aa289f675b8856a831a62915907610a` | RAISE EXCEPTION；显式拒绝降级，要求协调恢复 |
| `00007_message_user_state.sql` | `8a0806eb2367d797a5af2f3f030d4767db9202b4cf7af472e360e8dcaa873caf` | RAISE EXCEPTION；显式拒绝降级，要求协调恢复 |
| `00008_send_policy.sql` | `f97a25c5fd3911bded2d1e63a094027780c34b58a503c27561c7423dc4cbc891` | RAISE EXCEPTION；显式拒绝降级，要求协调恢复 |
| `00009_domain_asset_guard.sql` | `54348b595d017a36edb77930f666aa6a26fadf7e848bf1cf5d1e4190c57fcc2a` | RAISE EXCEPTION；显式拒绝降级，要求协调恢复 |
| `00010_sent_mail_archive.sql` | `57156b73a3f5f4ba4aab55ce0a6e3a670a74b52544e7df13ea38e7dd61fb259f` | RAISE EXCEPTION；显式拒绝降级，要求协调恢复 |
| `00011_mail_content_index.sql` | `6232a3c74b75980d48bcc2f8e2e5f79854c366a89f09743deb5a8ebbfe747042` | 删除索引触发器、mail_index_jobs、mail_documents；会移除派生数据 |
| `00012_employee_disposition.sql` | `2f7d448d04c185a1642399a0f6559d2df7ed9a1e59ac2523167e259e24e3d9b0` | RAISE EXCEPTION；显式拒绝降级，要求协调恢复 |
| `00013_draft_creation_receipts.sql` | `69c605b794be3b3884659c6315d518b94c69ffb8259d00199a45ee4d1e1160c3` | RAISE EXCEPTION；显式拒绝降级，要求协调恢复 |

不能声称“全部历史 Down 都拒绝运行”：00001是no-op、00011含派生表删除。公司升级仍禁止自动执行破坏性Down，不应重写历史SQL来改这个事实。真实测试只重复Up，没有执行Down。

下一个增量编号由实施时当前最大 Goose 版本分配，当前为00014起；并发工作先登记占用，不能两批都提交00014。P1权限revision/ABA、P2完整收件人、P4处置事实是拟新增schema主题，不代表已经有新迁移。每批需交付：目的、旧记录映射、预检、表锁/空间/回填预算、恢复点、API及前端协调条件、空库和升级库测试。已有13个文件只读。

## 8. 主要迁移风险和后续验证

身份/授权：不能把NULL与空集合互换、删除重建不能重置权限revision。内容：BCC仅用可靠来源回填，来源已失去要标未知；留存规则修改先保护历史个人永久邮箱。写入：新旧API/SMTP/worker/index/retention默认不能混跑；触发器/schema/响应契约与客户端同批升级。恢复：数据库与对象存储必须使用一致备份点，非空目标不得被覆盖。大表：索引创建、回填锁与磁盘成本需生产规模隔离副本测量，本轮没有容量认证。

本轮清点不关闭P0-070锁图、P0-080协议真值表、P0-100性能基线或P11最终升级验收。运行命令、被测源码/工具哈希和实际结果见本批验证报告；没有执行生产迁移、DROP或公网发送。
