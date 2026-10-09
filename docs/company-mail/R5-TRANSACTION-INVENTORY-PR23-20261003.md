# PR23 transaction inventory 修复证据

生产源码固定 `aeed47f0e1998d9925acb680992b051463821d69`，工作分支 `fix/r5-transaction-inventory`。本次仅修改 transaction inventory、其测试和独立证据。

参考整合证据固定 [e5e23e21abfe4c7e9f082405e56a95a7f346fa9a](https://github.com/jyqj/tabmail/blob/e5e23e21abfe4c7e9f082405e56a95a7f346fa9a/docs/company-mail/R5-INTEGRATION-BATCH4-20261003.md)，不重做全套集成，不继承其 PASS 为本任务执行结果。

## 结果与真实支持范围

原始目标测试以基线脚本、基线 manifest 和当前固定生产源码运行，保留 [original-test-fail.log](evidence/PR23-TRANSACTION-INVENTORY-20261003/original-test-fail.log)：`check_r5_transactions.py:122` 的 `postgres function set drift`，45 missing / 2 obsolete。原始 CLI [FAIL JSON](evidence/PR23-TRANSACTION-INVENTORY-20261003/original-fail.json) 原样保存；它是本地复现，未冒称下载到 CI 原日志。

刷新后 62 PG 文件 / 395 函数 / 498 SQL 执行调用 / 134 字面直接写候选 / 154 名称闭包写候选 / 19 迁移。26 项 scoped unittest 全通过，inventory CLI PASS；`task_complete=false`、`runtime_verified=false`。原 guard 与 AST producer 原字节未改，无新增白名单。

除45个新增和2个删除外，33个旧函数体变化，86个旧函数 syntax变化（含行号移动）、137个旧条目caller变化全部重抽取。5个新增migration Up source与hash刷新，未执行迁移或读取真实DB catalog。

PostgreSQL：**NOTRUN**。本次未连接现有/生产DB、邮件或Redis；不宣称死锁消除、动态dispatch、所有调用者授权、审计回滚或各期限边界实测。结构清单不能替代事务动态验收。历史 top-level acceptance/runtime records 原值归档到 `historical_review_metadata`；33个变更体及45新增均 source-only，SaveParsedMessage旧历史cache关系证据不替新期限实现背书。

## 逐项新增函数分析

每条 exact ID、源体 SHA、caller候选、字面效果/锁、执行表达式、手工角色/顺序/未验证风险见 [function-review.json](evidence/PR23-TRANSACTION-INVENTORY-20261003/function-review.json) 与当前 [manifest](evidence/R5-TRANSACTION-COVERAGE.json)。下表人工角色不覆盖/篡改原AST heuristic分类。

| 函数（文件；receiver以exact JSON为准） | 真实角色 | 生产顺序／边界 |
|---|---|---|
| `apikeys.go:*PgStore:touchAPIKeyObservation` | autocommit-observation-write | pool.Exec(apiKeyUsageUpdateSQL): UPDATE usage timestamp/IP as a whole only when newer; never INSERT or read authority; unchanged FK key. AST direct_write=false because SQL is a package constant. |
| `company_admin_events.go:*PgStore:ListCompanyAdminEvents` | authorized-transaction-reader | companyReadTx(admin=true) reloads actor; bounded outbox invalidation projection by tenant; no delivery-state filter or content grant. |
| `company_admin_events.go:*PgStore:WithCompanyAdminEventAccess` | transaction-external-callback | companyReadTx(admin=true) holds current authority fences while checking context then emit(); emit is an external callback, not transactional delivery. |
| `company_attachment_gc_scheduler.go:*PgStore:sweepCompanyAttachmentTenants` | session-lock-scheduler | Dedicated connection; pg_try_advisory_lock(1414350659,1); cursor read and bounded tail/head tenant scan; each cursor UPDATE autocommits before sweepCompanyAttachmentsWith on SAME connection; connection close releases session lock. |
| `company_mail.go:*capturedRestoreDeadline:check` | borrowed-transaction-deadline-check | capturedRestoreDeadline.check reads exact message/expiry equality plus receivedContentEligible and original purge deadline with clock_timestamp; no new row lock or commit. |
| `company_mail.go::restoreMessageMutation` | borrowed-transaction-write | message NO KEY UPDATE NOWAIT; require trashed; capture expires/purge; check original deadlines; clear deleted_at/purge_after while retaining expires_at; affected-row check; check again; return guard to outer caller. |
| `company_ops.go::sweepCompanyAttachmentsWith` | transaction-owner-gc | db.Begin; tenant lock; exclude outbound/sent/draft pins BEFORE LIMIT 100; attachment UPDATE SKIP LOCKED; recheck expiry/pins in DELETE CTE and register orphan_objects; Commit. |
| `content_eligibility.go:*PgStore:authorizeReceivedMailboxTx` | borrowed-transaction-authorization | lockMailboxAuthorization then requireReceivedMailboxLiveTx then mailboxAccessTx CanRead; outer companyReadTx must already fence actor/profile. |
| `content_eligibility.go::readReceivedPageTx` | borrowed-transaction-dynamic-reader | One materialized eligible CTE serves count and page; explicit JSON message projection and user states; fragments/placeholders supplied by caller, no write/commit. |
| `content_eligibility.go::requireReceivedContentTx` | borrowed-transaction-eligibility-reader | tenant/mailbox/message SELECT with receivedContentEligible; no row lock; missing maps to NotFound; outer caller provides authority and fences. |
| `content_eligibility.go::requireReceivedMailboxLiveTx` | borrowed-transaction-deadline-reader | tenant/mailbox SELECT checks recorded mailbox expires_at against clock_timestamp; no new row lock; absent/expired -> NotFound. |
| `employee_disposition.go:*PgStore:offboardingPlanTarget` | borrowed-transaction-plan-qualifier | Delegates offboardingQualifiedTarget(...,allowInactiveTarget=true); formal planner admits frozen target under identity/epoch qualification. |
| `employee_disposition.go:*PgStore:offboardingQualifiedTarget` | borrowed-transaction-locking-qualifier | offboardingSubjectsTx; target/successor epochs; reject executed target epoch; guardMemberRemoval; lock relevant outbound jobs ORDER BY id FOR UPDATE including uncertain ledger. |
| `employee_disposition.go::offboardingAuthorityEpochTx` | borrowed-transaction-epoch-reader | Read tenant/user identity tuple plus profile ID; assigned profile revision FOR SHARE NOWAIT; reject unavailable/busy profile; hash binding. |
| `employee_disposition.go::offboardingEpochsTx` | borrowed-transaction-epoch-composition | Read target epoch then successor epoch via offboardingAuthorityEpochTx in same borrowed transaction. |
| `employee_disposition.go::offboardingPreviewDeadlineTx` | borrowed-transaction-deadline-reader | SELECT clock_timestamp() >= preview expiresAt; Conflict when elapsed; invoked after potential waits by planner/executor. |
| `employee_disposition.go::offboardingSubjectsTx` | borrowed-transaction-locking-qualifier | Validate distinct IDs; target users FOR UPDATE; active policy branch and current actor role check; successor users FOR SHARE must be active. |
| `member_guard.go::guardMemberHistoricalIdentity` | borrowed-transaction-retention-reader | EXISTS retained plans/creation receipts/attachments/sealed drafts/outbound jobs; reject retained identity; final FK remains deletion fence. |
| `member_guard.go::memberIdentityDeleteError` | pure-error-mapper | Map pgconn.PgError 23503 to ErrMemberHasHistoricalIdentity; otherwise preserve error. |
| `outbound.go::outboundJobsScopeSQL` | pure-sql-builder | Tenant conjunct plus explicit owner branch; optional readable-mailbox OR and zone restriction; unknown principal returns visible=false. |
| `outbound_inspection.go:*PgStore:InspectOutboundRecovery` | transaction-owner-audited-inspection | Begin; recoveryReferencedActor tenant KEY SHARE/current actor SHARE; job SHARE NOWAIT then ordered bounded recipients SHARE NOWAIT; explicit DTO; companyAudit/outbox then Commit; busy/serialization -> Conflict. |
| `permission_assignment.go:*PgStore:AssignPermissionEditor` | authorized-transaction-cas-write | companyTx; writable member snapshot and expected revision; selected profile SHARE NOWAIT and revision; optional user assignment CAS; apply patch; reread snapshot; companyAudit; no-op emits no audit. |
| `permission_assignment.go::permissionAssignmentChangedFields` | pure-audit-diff | Compare profile ID, override presence, nullable values and domain intent; synthesize inherited baseline when nil; return field names. |
| `permission_editor_patch.go:*PgStore:PatchPermissionEditor` | authorized-transaction-cas-write | companyTx; writable permissionEditorSnapshotTx; expected tuple equality; empty patch returns snapshot; patch, reread, companyAudit in same transaction. |
| `permission_editor_patch.go::applyPermissionPatchTx` | borrowed-transaction-write | Validate patch; empty no-op; listed domain KEY SHARE NOWAIT; one overrides upsert, omitted columns retained; explicit inherit/list/none/all parameters; trigger mutates users revision. |
| `permission_editor_patch.go::permissionDomainPatchParams` | pure-parameter-builder | Absent -> nil/nil; inherit -> inherit/nil; list -> list/zone array; all/none -> mode/empty array. |
| `permission_editor_patch.go::permissionFieldParam` | pure-parameter-builder | Generic nullable patch field: absent or inherit -> nil, explicit value -> value. |
| `permission_editor_patch.go::permissionPatchFields` | pure-audit-projection | Enumerate Present fields including domain_access; no value computation, DB call or lock. |
| `permission_editor_snapshot.go:*PgStore:GetPermissionEditorSnapshot` | authorized-transaction-reader | companyReadTx(admin=true); permissionEditorSnapshotTx(write=false); returns only after helper transaction result. |
| `permission_editor_snapshot.go::permissionEditorSnapshotTx` | borrowed-transaction-locking-reader | Target user SHARE or NO KEY UPDATE by write flag; CanManageTenantMember; user revision; assigned profile SHARE NOWAIT and revision; raw overrides plus effectivePermission projection. |
| `permission_profile_cas.go:*PgStore:DeletePermissionProfileCAS` | authorized-transaction-cas-delete | companyTx; mutable profile UPDATE NOWAIT and expected revision; affected members NOWAIT and exact confirmation set; DELETE CAS; FK conflict mapping; permissionProfileAudit. |
| `permission_profile_cas.go:*PgStore:GetPermissionProfileDeletionPreview` | authorized-transaction-locking-reader | companyReadTx; profile UPDATE NOWAIT despite preview being read; system immutable; permissionProfileMembersTx; effective permissions before/without profile for every member. |
| `permission_profile_cas.go:*PgStore:UpdatePermissionProfileCAS` | authorized-transaction-cas-write | companyTx; profile UPDATE NOWAIT/revision/scope; member locks; listed zones KEY SHARE NOWAIT; compute diff; no-op returns existing; UPDATE CAS then permissionProfileAudit. |
| `permission_profile_cas.go::permissionPositiveRevision` | pure-input-validator | Parse positive base-10 int64 and require canonical decimal rendering. |
| `permission_profile_cas.go::permissionProfileAudit` | borrowed-transaction-audit-write | Tenant-local -> companyAudit; global -> audit INSERT under actor tenant then distinct affected tenant IDs sorted, INSERT company.admin.changed outbox for each. |
| `permission_profile_cas.go::permissionProfileChangedFields` | pure-audit-diff | Reflect comparisons of mutable profile fields including allowed_zone_ids; returns changed field names. |
| `permission_profile_cas.go::permissionProfileMembersTx` | borrowed-transaction-locking-reader | Affected users ORDER BY id FOR NO KEY UPDATE NOWAIT; per-member manageable role check; tenant KEY SHARE NOWAIT; return confirmation tuples with profile revision. |
| `permission_profile_cas.go::permissionProfileMutationTx` | borrowed-transaction-locking-reader | Scope-filtered profile SELECT with dynamic FOR UPDATE NOWAIT; reject absent/system immutable profile. |
| `permission_profile_create.go:*PgStore:CreatePermissionProfileGuarded` | authorized-transaction-write | Validate requested profile; preliminary persistent home locator not authority; companyTx anchor and current actor; recheck home/super role; target tenant KEY SHARE NOWAIT, zones SHARE NOWAIT; INSERT RETURNING persisted row; permissionProfileAudit. |
| `permission_profile_visibility.go:*PgStore:ListVisiblePermissionProfiles` | authorized-transaction-reader | companyReadTx(admin=true) current role; selected tenant existence; super sees global list, company admin local/global; ORDER BY name,id. |
| `permissions.go::effectivePermissionAfterProfileRemoval` | borrowed-query-policy-reader | effectivePermissionPolicy(...,withoutProfile=true) excludes assigned profile, keeps overrides/defaults; deletion preview simulation only. |
| `permissions.go::effectivePermissionPolicy` | borrowed-query-policy-reader | QueryRow users LEFT JOIN profile unless withoutProfile, overrides; COALESCE fallback and domain_access_mode projection; querier may be pool or transaction. |
| `sent_content_selector.go::readSentContentTx` | borrowed-transaction-content-reader | Locate original sent asset mailbox; lockMailboxAuthorization; then original sentContentFrom/submissionContentScope projection; completeness controls BCC nil vs empty; safe display headers. |
| `sent_recipient_snapshot.go:*PgStore:BackfillSentRecipientSnapshotsV1` | transaction-owner-maintenance-write | Validate tenant/limit 1..1000; Begin; keyset candidates legacy_unknown; reliable exact-source match jobs FOR SHARE; UPDATE still-unknown assets to COMPLETE; aggregate progress; Commit. |
| `submission_receipt_privacy.go::loadSubmissionReceiptRecipients` | borrowed-transaction-outcome-reader | Empty IDs returns empty map; query tenant/job states ordered job/address; project only states, no recipient addresses/content; no commit/locks. |

## 删除条目与原角色

| 原函数 | 原角色／当前替代 |
|---|---|
| `internal/store/postgres/submissions.go::applySubmissionOutcome` | pure-outcome-projection-no-sql; applySubmissionOutcome was receipt projection; now company.ProjectOutboundReceipt owns outcome projection. No production PostgreSQL function remains. |
| `internal/store/postgres/submissions.go::loadSubmissionRecipients` | borrowed-transaction-recipient-reader-no-commit; loadSubmissionRecipients removed address-bearing receipt helper; loadSubmissionReceiptRecipients now returns only ledger states. No alias/allowlist retained. |

## 不可误读的抽取器边界与反例

- `touchAPIKeyObservation` 实际 UPDATE usage，SQL在包级常量，函数字面heuristic仍是dynamic-sql-review；明确手工标为autocommit write，包文件hash独立拒绝常量漂移。没有把constant遗漏解释成无写。
- `WithCompanyAdminEventAccess` 在read transaction内emit；外部发送不随SQL失败撤销。`GetPermissionProfileDeletionPreview` 仅读但取得profile UPDATE NOWAIT及成员NO KEY UPDATE NOWAIT；read名称不代表无锁。
- GC dedicated session advisory lock覆盖多次cursor自动提交和每tenant自有事务；无round原子性，100行candidate上界不等物理query-work上界。
- Permission override触发器会写user revision，profile删除有assignment/FK副作用。migration16 source保留这些间接效果；migration17 CREATE OR REPLACE function正文由全文件hash保护，原行扫描器不是SQL parser。
- `effectivePermissionPolicy`是querier接口，旧callback语法heuristic可能过度近似；`Scan`同名候选闭包不是resolved dispatch。纯parameter/diff函数没有SQL、锁或独立事务。
- 反例覆盖missing、obsolete、wrong receiver、重复、未来纯函数/写函数、新文件、旧体新增写、包常量hash、caller、callback、FK/trigger/new migration、classification、精确effects和runtime/task越权声明。未关闭任何guard。

## 可复核运行方式

```sh
export GOCACHE=/workspace/r5-inventory-owned/gocache
export GOMODCACHE=/workspace/r5-inventory-owned/gomodcache
export R5_GO=/workspace/tabmail-cloud/tools/go/bin/go
python3 -B scripts/check_r5_transactions.py
python3 -m unittest discover -s scripts/tests -p test_r5_transactions.py -v
```

Go extractor仅用标准库；从首次Go调用即owned caches，不使用默认readonly cache。基线仓库无`.agents/skills`，workspace `.agents`为空，web AGENTS不在本任务修改范围。

## 发布限制与未处理失败

gh auth probe token无效；随后CI job read实际返回GitHub API Forbidden，已停止API相关动作，不换身份/route。该拒绝不等普通origin git拒绝，exact fetch成功。Draft PR body已准备，API拒绝使实际创建受阻，不能报告已建PR。

整合prepared的26旧schema16prep errors、10migration/schema drift、平台路径1、compatibility1、wire1及10 failure events不在本任务修复范围；独立inode复用观察只为线索，未作为各失败因果。全量集成/CI未重跑，不报告全绿。不merge/deploy，不改中央TODO或生产锁。
