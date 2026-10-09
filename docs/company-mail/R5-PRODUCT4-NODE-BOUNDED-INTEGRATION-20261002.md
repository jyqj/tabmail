# M后四族与Node新增实际证据、原TODO叶级裁决

**四族30top/96leaf/111pass均有实际race Go0；这是独立审署的bounded runtime，不是完整产品/父门禁通过。** P6是FakeStore注入的shipping函数，不PG/SMTP。Node另有P5当前41PASS、旧14RED；receipt初whole54 CLI1的真实TDZ故障只补exact2新源PASS，不冒freshwhole54/36。原M失败、full2aa9失败及其字典不改。

root新要求允许真正满足原171全部条件的叶合法勾选；本批独立逐叶核对后**没有可新增勾选项**，原因是具体scope/依赖缺证，不是机械维持10。原171要求均保。

## Evidence：四族同immutable源，旧红与infra分别保

| ID/phase | source与实际结果 | 原件 |
|---|---|---|
| E-4-SOURCE | canonical source `7ffe88737b4b846ad96978b2a69a105caccf3914`（非Git），closure `567f05e8d5edd1067afb16e80f5b5a8638e5aeb49c292aad122925fc737729db`；8签pins与snapshot/main对应、pre/post相等，官方New实际schema17 | [原review](evidence/R5-PRODUCT4-RESTORE-GC-LIFECYCLE-P6-REVIEW.json)、[120原payload档](evidence/R5-PRODUCT4-RESTORE-GC-LIFECYCLE-P6-LOGS.tar.gz)、[交付清单](evidence/R5-PRODUCT4-RESTORE-GC-LIFECYCLE-P6-DELIVERY.json) |
| Restore | 5top/30leaf/35pass，raceGo0，0fail/skip/missing | `main/phases/restore` command/JSONL/classification/exit |
| GC | 7top/9leaf/10pass，raceGo0，0fail/skip/missing | `main/phases/gc` |
| Offboarding | 11top/42leaf/47pass，raceGo0，0fail/skip/missing | `main/phases/offboarding` |
| P6 current | 7top/15leaf/19pass，raceGo0，0fail/skip/missing；shipping deliverRecipients+FakeStore故障屏障 | `main/phases/p6-green`，不实际SMTP或PG durable证明 |
| P6 canonical RED | source `0911cd77e838d495db190e27e14f5e04d802c87e`，仅production recipient_delivery.go精确overlay，tests/依赖同源；Go1，**19nodes=6PASS13FAIL、15leaf=5PASS10FAIL、7top=2PASS5FAIL** | `main/p6-red-source-receipt.json`/overlay/baseline.go/`main/phases/p6-red`；不是9失败leaf，也不是infra红 |
| Bootstrap infra | 两次ConnMaxLifetime=0→officialNew1，**0产品test**；修executor配置300s匹配应用默认后才真正运行四族，不产品/预期改动 | 两个`infra-*`原raw/cleanup全部保，不升成product RED |

新独立`remaining_gate_dispatch_audit`逐raw/source复核签署**BOUNDED_RUNTIME_PASS**、本族无源码/raw blocker，并核三新owned cluster清理；并未重复产品测试。继承的compile/build0没有重跑或拼为运行验收。

## Finding→Path：限定关闭的技术子层

- **F-R（validated/high，D）**：received restore硬期限不被清空，personal历史期限豁免、shared0/NULL未知但实际期限保守处理、expired/purge等于截止拒、无read/organize无effects；identity/mailbox/audit/outbox等锁后clock重验及GC竞争有真实屏障。P-R：当前资格→锁/截止→原子restore或拒绝→对象关系/副作用核验。不是sent restore/全部历史迁移。
- **F-G（validated/high，D）**：draft/outbound/sent保护prefix第101孤儿继续推进、allprotected有界、busy/batch/并发draft fence、orphan INSERT失败回滚、reference查错failclosed、held原件元数据保护。P-G：选择候选→当前引用复检/锁→metadata+orphan同Tx→保持保护对象。逻辑candidate100不等物理scan100；未实际object删除/commit未知全链/21tenant多实例。
- **F-O（validated/high，D/H）**：frozen现代预览/执行与qualified replay、actor/target/successor role/session/freeze/reentry、user/profile复合ABA及nullablepair、profile锁/重建、plan/audit/preview deadline等待后拒绝、shipping HTTP tenantwait重核。P-O：当前生命周期资格→compound snapshot/locks→当前计划执行或拒绝→receipt重放核验。旧f11/f3 HOLD不贴新源，不完整legacy迁移/全queue/key/grant/双管理员失答证明。
- **F-P6（validated/high，U/F）**：known outcome checkpoint先于非必要telemetry、blocked telemetry取消不抹accepted、checkpoint失败/lease/cancel保持uncertain与原error链、markjob错误非telemetry、触网前token/lease/tenant/senderfence。P-6：shipping函数→FakeStore/checkpoint barrier→结果/遥测排序→拒重触网断言；RED只替production一文件形成对拍，不伪PG durable或真实250回复后重启。

## 独立原TODO完整性裁决

[逐叶裁决](evidence/R5-PRODUCT4-BOUNDED-VERDICT-TODO-ASSESSMENT-20261002.json)将cover与未闭具名分开：

| 原叶 | 本批已证明 | 仍缺正式全部条件 |
|---|---|---|
| P3-030 | received技术A05 | P3-010/020 common收发政策/可靠历史迁移前置未验 |
| P3-040 | received等待/GC原子边界 | sent restore、旧restore-first期限夹具 |
| P3-060 | 一个并发draft参考保护 | 多附件逆序、finish/consume/transfer全引用建立竞争 |
| P3-070 | 101孤儿/allprotected/logicalcandidate100 | physical scan成本不是100cap；P3-050/060未闭 |
| P3-080 | 不由本包代证 | 21+tenant/scanner restart/多scanner公平 |
| P3-090 | metadata/orphan原子、held authority | commit uncertain、真实对象删除故障全链 |
| P4-030 | frozen现代+successor bounded | P4-020可靠legacy completion迁移/唯一事实 |
| P4-040/050/060 | compound/clock/qualified replay部分 | 全queue/key/grantseed rollback、同数量换资产、双管理员lostresponse、legacy一步完成映射 |
| P4-100/110 | reentry/epoch相关部分 | 全重新入职与harddelete资产保护 |
| P6-060 | 生产函数排序/error-chain | PG durable checkpoint、真实SMTP250→取消/lease变化/worker重启无重复发送 |

所以本批复选框不新增，**10/171因这些真实缺口保持**；不是把111pass数当完整叶，亦不以依赖未关掩盖有证技术进展。

## Node：P5真RED/green与TDZ首次失败必须保

[Node初次档](evidence/R5-NODE-P5-RECEIPT-UI-INITIAL-LOGS.tar.gz)/[原review](evidence/R5-NODE-P5-RECEIPT-UI-INITIAL-REVIEW.json)为source`d6bd8c8df1051e7cea11b334074f05d005dfb507`、closure`f7034be7135e4e71c5bf0364dcff5fa15b3fa7577454ba1b0fbbda3e8c3d9dd0`，187ordinarywebfiles+15签pins；只是原lock private模块复用，不新cold。

- P5旧production-only writer overlay41：27PASS/14FAIL、CLI1；current41PASS/CLI0，实际clone/UUID/revision/pinned payload/late关闭/serde边界证明，原RED不删。
- receipt-types35：34PASS/**1真实production TDZ FAIL**（headers143）；receipt-consumer19PASS，两个file合54 **CLI1**。不是fixture失败、不整体green。
- page只3签describe/8PASS，另3oldfiltered未执行，不整page/UI旅程。
- [TDZ修复原review](evidence/R5-RECEIPT-TDZ-REPAIR2-REVIEW.json)/[11原payload档](evidence/R5-RECEIPT-TDZ-REPAIR2-LOGS.tar.gz)：source`f00be8daa71b2e865c23e6401c1750e0f27904a4`、closure`595b8d559987598e7e0e40f9e3112185811e8dc3c6594185489e8b5d12aa0dfe`，仅prod/test454db/d2e delta。actual accepts-safeheader+原reject-leaf exact2PASS/CLI0/0.731932s；34filtered不复，原whole54CLI1不改，不新wholefile36/54green；新独立receipt_ui_independent_review复核原pin/argv/36注册2PASS0FAIL34filter与cleanup后接受only2 bounded runtime。

Node是production TSconsumer+controlled transport/Vitest，非API/Go/PG/SMTP/browser；全部4原Nodegroups与修复group真gone。P5单文件通过不代Go/TS全canonical/草稿保存提交父scope，receipt修复不代ordinaryDTO/BCC OAS同步。

## 当前具名剩余与安全交付

1. **RES-PRODUCT4-FORMAL-SCOPE-01**：上表每原叶缺证单独施工/验，不合桶消unknown；已有received/GC/compound/FakeStore子层不反复测试。
2. **RES-P6-DURABLE-SMTP-01**：原P6-050/060依赖下真实PG+SMTP/checkpoint/lease/restart场景，不把fakeStore当persisted接收字节。
3. **RES-NODE-CURRENT-CHAIN-01**：TDZ只exact2 newsource与原54FAIL组合，稳定当前源UI/API/GoPG/shipping旅程/当前cold另验，SOURCE与不同源码结果不拼freshwhole。
4. **RES-OPENAPI-ORDINARY-BCC-01**：正式OAS/普通回执/BCC长期资产policy与真实response仍精确缺口，132注册/SSEschema不覆盖。
5. **RES-TX-CURRENT-CATALOG17/RES-PROTOCOL-CURRENT/RES-CI-CURRENT17-FRESH**：新family/当前17 actualcatalog与AST、protocol政策/fixture、registrytyped failedM admission及完整secure/CI门禁未关；旧catalog15/AST802/full2aa9不是新源全验收。
6. **原M/P0/G0**：唯一M8529实际预算失败/noresult保、不retry；7ffe/Node与M16不混，原171 scope全保。

product4原120payload档直接copy，Node36/TDZ11原payload直接copy；[Node交付](evidence/R5-NODE-P5-RECEIPT-TDZ-DELIVERY-20261002.json)和product4清单分别保原wrapper hash，无每文件hash/测试复跑。没有objects/DB/socket/DSN/JWT/rawmail/CPUtrace/node_modules/source树上仓；当前三owned PG最终92502 stop0/tempDB空/PID/socketgone，旧M残停Root未触。无commit/push/PR/部署。

文档后只refresh Code-Index；**本批有限子层实际进展已登记，formal叶缺证具名，不父fullgreen**。
