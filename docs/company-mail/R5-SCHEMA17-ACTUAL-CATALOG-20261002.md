# source1a15 实际schema17 catalog与升级子层

**本次是正式New/Migrate与O_EXCL实采catalog，不是旧JSON把版本改成17。** 原15/16历史catalog不变；source1a15与M852900/schema16、product4的7ffe分开。3top/3leaf/4racePASS仅本次catalog/restart与两受影响upgrade范围，fullCI/原171父门禁未关。

## Evidence→Finding→Path

- **E-C17**：sole source `1a15faf7d4bfe0b1d0cdd610ad4d9fb14618b7f4`，closure `77714ddf99157760e9da1162362316a514a4acec6602bf383fef1374fb9a318f`，before/after相等。原[运行review](evidence/R5-SCHEMA17-CATALOG-UPGRADES-REVIEW.json)、[48原payload档](evidence/R5-SCHEMA17-CATALOG-UPGRADES-LOGS.tar.gz)、[交付清单](evidence/R5-SCHEMA17-CATALOG-UPGRADES-DELIVERY.json)保source/argv/raw/exit/cleanup。
- **E-CATALOG**：[新实际catalog](evidence/R5-TRANSACTION-DB-CATALOG-SCHEMA17-ACTUAL-20261002.json)，194779 bytes，作者SHA256 `398e67a97eff3b4e10c01db3a043b6eef0cc4512e953a0e764c6692b4fbc4dd7`；`TestR5SchemaInventoryAndRestart`实际O_EXCL输出，Goose17与17个migration hashes、restart catalog unchanged。不是拷旧catalog/静态宣数量。
- **E-UPGRADE**：`TestP0GooseRestartAndHistoricalGrantSafety/upgrade_and_restart`及`TestArchitectureUpgradeBackfillsExistingEmployeeAssets`真实race Go0。精确3top/3leaf/4test-subtest PASS，无fail/skip/missing；bootstrap officialNew0单列，不重复旧tests。

| 实际类目 | 数量 |
|---|---:|
| tables | 50 |
| columns | 505 |
| constraints（所有类型） | 183 |
| FK（其中foreign key） | 75 |
| indexes | 153 |
| triggers | 11 |
| trigger functions | 11 |
| sequences | 3 |

**F-C17（validated/high，限上述source范围）→P-C**：fresh owned schema17由officialNew/Migrate应用→原catalog SQL读取真实结构→O_EXCL写原bytes→restart一致及两upgrade目标→cleanup。DDL定义/实际约束存在性有证，不等每trigger运行时effect、每caller dispatch、全upgrade恢复或完整release验收。

PG44570 native stop0、testDB0/conn0、PIDfile/socket/known groupgone；不触旧M16残停root。新catalog原bytes另文件存档，不改旧`R5-TRANSACTION-DB-CATALOG.json`的Goose15/7trigger provenance；旧16运行字段也不升级或拼新17源。

## schema18只占号，不回贴

root新GC tenant公平writer独占outerSweepCompanyMetadata/internal cursor方案与**migration18唯一占号**。目前18尚未落/验证，无实际catalog18；17实测仅绑定source1a15与原17migration hashes。后18必须独立source/freeze/New/Migrate/catalog/upgrade/公平性目标，不把当前17 counts/hash改称18。

## 当前缺口与精确依赖

1. **RES-TX-CURRENT-INVENTORY-01**：新P1/Create/B5/GC等production family owner逐source/Tx/locks/FK-trigger/callback effect分类→fresh AST→合法coverage/assertions/caller候选派生；当前official coverage旧scope不能凭新catalog存在清unknown或换hashPASS。
2. **RES-GC-TENANT-FAIRNESS-18-01**：原P3-080要求21+tenant、scanner重启、多scanner公平、单tenant持续写与限定轮数推进；新18只有占号，待source/实际升级与专项runtime，不用product4第101孤儿子层代证。
3. **RES-OPENAPI-ORDINARY-BCC-01**：新ordinary OAS独占writer已恢复施工，旧P1/SSE/inspection冻结保；实际A/B schema/entry map交中央后精确更新，writer没有交最终packet/target结果前不称同步或staticPASS。
4. **RES-REGISTRY-CLOCK-TYPED-M-01**：历史S执行墙钟与Go test含setup/cleanup分界需已知版本强binding。原row/review/raw不改，M原archive始终0ab842，27fe12是registry FAIL包，绝不构造M repack链；新typed failed M专审及实际suite仍待。
5. **RES-CI-CURRENT-FRESH-01**：原full2aa9 Go1、protocol/ordinary/registry原fail、Node分源及未做真实browser/currentcold全门禁保，catalog3PASS不关这些父scope。

安全档48原UTF8 payload直接copy，无AppleDouble/privateDSN/JWT/rawmail/objects/DB/socket/profiling/node_modules/fullsource上仓；hash沿作者delivery，没有产品测试或每文件hash重跑，无commit/push/PR/部署。文档后只Code-Index文件refresh。

**原171 scope保，10/171不因catalog数或3tests新增勾选。**
