# 权限原子 slice 并行实施与有限运行证据

**WIP，仍10/171；不验收G0/P1。** 用户明确允许在100w测试期间并行施工。原171父任务、依赖、正常/失败/迁移/UI/最终门禁保留，不用局部通过代替完成。

## 已落真实入口

- 既有permissions service新增versioned窄端口，不平行造业务服务。原始profile/nullable overrides、field sources、canonical effective与复合decimal revision在同一U/P锁事务读取。
- 新16 noncycling sequence及trigger提供稳定user/profile版本；override/profile同ID删除重建、分配变化不拿UpdatedAt/rowID当版本。历史1–15未改，Down拒绝破坏fence。
- GET/PATCH `admin/users/{id}/permission-editor`；POST `permission-editor/assignment`；profile CAS PATCH、删除preview与显式成员确认DELETE。strict unknown/dup/case/trailing/required-nullpair、64KiB上限与missing/stale区分；省略保持、个人NULL继承、false/0、域名四mode不混。
- 旧个人PUT/DELETE与旧UpdateUser的profile字段旁路409退出。正常display/role管理不借新协议放宽；profile POST/query/scan返回真实SQLrevision，非时间戳。
- None经canonical `ZoneScope/AllowsZone/NarrowZones`及实际资格/列表拒绝，不以空数组假全域；旧NULL/[]的legacy all语义不暗改。editor展示raw canonical；JWT admin `/auth/me/permissions`复用已存在loader principal policy，owned admin Key不豁免profile，显示不替代正式写入重授权。
- 成功override/assignment/profile update/delete使用既有required audit及`company.admin.changed` outbox，同Tx失败回滚；empty/noop不消耗版本/不造事件。跨tenant global-profile event fanout未验，具名保P1-130/G7。

## 实际证据分层

| 源／阶段 | 实际结果 | 不证明什么 |
|---|---|---|
| b927 PG | 23新P1+9历史迁移相关top=32，race0fail/skip/missing | 非fullsuite/父任务通过 |
| b927 HTTP | 8top，6pass/2fail | 不称HTTP全绿 |
| 081a strict修 | 仅原2失败case：duplicate parser已pass，assignment仍因内部intent误Marshal成wire失败 | 不丢原红、不伪production Marshal讨绿 |
| 0ecb wire修 | 仅该formal assignment/profile-delete case实际pass，显式正式scalar/null/omit wire | 非同源8case全fresh |
| authority修 | 4真实PG+4shipping HTTP race通过，seq fixture显列修 | 不替所有角色与新source整体门禁 |
| 744e stage修 | 必要truth24/owned-admin-key6/ownerless-scope12真实race通过 | nil/[]行为比较与旧fixture覆回profile的修正不当新policy |
| ownerless真实红 | 同key、同真实legacy sender ID：worker SMTP DATA1，而retry send_as403 | 未统一080；不清sender ID或全禁legacy讨绿 |

机器记录及[47成员安全原档](evidence/R5-B02-WIP-PERMISSION-RUNTIME-LOGS.tar.gz)见[原运行摘要](evidence/R5-B02-WIP-PERMISSION-RUNTIME.json)。各源分别记录，不把旧32绿贴新My/outbox/ownerless源。原ownedPG3344历史handoff的alive字样是其记录时状态；01:49 smartshutdown后仅唯一operator恢复owned cluster并转租88440，testpg只借DSN创建/删除子DB，无证据归因停库给某worker。

## 未完成与下一施工

1. Ownerless legacy当前free/无模板的same-key retry须与原resolver真实资格一致；disabled/template_required独立policy约束也不能TenantWide绕。不可变模板实际TemplateForSend要求user identity，未知grant/provenance拒绝。原SMTP/retry红、不同key/owner改变/撤权/范围/过期/跨tenant/ID重用/company mailbox/uncertain负控待新正式源runtime；不由纯41case闭080。
2. Profile/assignment UI当前代码与异步会话/冲突保draft+显式fresh确认有局部测试；source-onlyTSC不等clean Next build/shipping browser。旧opt-in CAS fixtures必须正式观察profile.revision，不猜版本或删除case。
3. 新source完整PG/HTTP/protocol/契约/必跑/前端门禁及逐010/020/040/060/070/080/090条款审查仍需集成；不能据本报告勾选。
4. 原S2只初始采集，搜索p95约5.6/5.9s保P7/P10缺口；原M/M2均未有完整baseline。M2 runner EPERM缺go.exit/budget/result如实unknown，不把1M入库/47k index当M。100/G0/PR与P1–P11余scope不缩。
5. 新bounded20真fixture pipeline合同/工具证据与历史S2(15)、M2(c9)分开，no ANALYZE/不调planner/不改原规模与预算；1000工具不能外推deep-ready100k/1M或原S/M，资源计划后再验。

## Ownerless 已新增的必要 guard 运行闭包

后续独立product source label087849的4top/23leaf实际race通过：保same-key/同真实legacy mailbox ID的shipping retry与SMTP正控，disabled/template-required阻SMTP、19个foreign/stale/revoke/scope/expiry/地址重用/company mailbox/uncertain等受控拒绝无effects、accepted收件人不重发。复用原resolver/policy/grant治理，不清SenderMailboxID、不是hasPublished自授，ownedKey/JWT原规则不改。原744e真实红保留，不把此target绿贴旧source或关闭完整080。

[必要guard原运行review](evidence/R5-B02-OWNERLESS-GUARD-REVIEW.json)／[9文件安全档](evidence/R5-B02-OWNERLESS-GUARD-LOGS.tar.gz)与pipeline工具source3d07分开。生产源标签亦按作者canonical closure标识说明，不伪Git树；payload全部保持原字节，仅安全交付剔除Mac AppleDouble `._` sidecars，私有原tar及其hash留在review。完整global fanout/130/G7与P1/G0仍未验。
