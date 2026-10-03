# Current18全backend失败与后续限定证据

**最新整backend实际失败，后续Stats/TS/ABC/只读对档不拼成full green。** 六个原安全档与原review见[交付](evidence/R5-CURRENT18-BACKEND-STATS-WIRE-ABC-DIAG-DELIVERY-20261002.json)。各canonical source标签非Git commit；HEAD仍587ba75，来源按immutable closure与raw归属。原失败/M/S/capture bytes未改，无重产品测试/每filehash/发布。

## E-BACKEND → F：当前18完整默认命令失败

[原review](evidence/R5-CURRENT18-BACKEND-FULLGO-FAILED-REVIEW.json)、[59原payload](evidence/R5-CURRENT18-BACKEND-FULLGO-FAILED-LOGS.tar.gz)：source `dfb054d7988e42de93e0ee2e14638b3525c2443b` / closure `3c5d9994caa800ac8e2fe682c5577425eba7c5918a64c451af121b8eef0f2968`，724真实taskdeps，source before/after相等。真实Go AST863声明top/862 required/51packages，不旧773固定目录。

- 默认整个backend Go1，outer313.629729s≤1200；7425run、7416PASS、6FAIL、1合法BrowserSkip及2无terminal。51package均有terminal，不等所有test执行完。
- PostgreSQL整个package180.682s达到**包累计180s**截止，Invitation/invite-first当时才运行约1s；206个该source top尚未start、两test没terminal。不是Invite死锁证据，也未延180/自动补跑206尾部。
- 失败簇：HTTPContract缺GET/company/events成功capture；两legacy普通receipt fixture；PG旧HTTP receipt形状；Goose18而旧expected17。两个classifier均exit1；old247 missing181/current862 missing445包含failed package目录效应，**不得将所有missing一律说成从未执行**。
- nativePG16851 stop0/PID/socket gone、Go/helpers/miniredis groups gone；私有停库root保残 `tm_test_6e4584f9`，观察0connections但不是DB0。Stats新source在dfb截点后，不把它当本次全Go结果或归因为本次失败。

P：保持失败收据→按已命名fixture/capture与累计测试生命周期缺口施工→新独审/sole affected→稳定source重新正式full gate。五文件PG lifecycle与四top/two-parallel当前只SOURCE prepared/独审中，不能称原180包预算已解决；不自行放宽timeout/改workflow验收。

## E-STATS → F：后cut值投影与handler限定通过

source `fc7c7c554f281e15ce257530656ba369248c8e9e` / closure `f0cfe3463617c6b56cb2e3c34db70af480d1679c002fe8b933cd3775ecf179bf`，[原review](evidence/R5-STATS-PUBLIC-PROJECTION5-REVIEW.json)/[16原payload](evidence/R5-STATS-PUBLIC-PROJECTION5-LOGS.tar.gz)。四SOURCE pins497558/dc469/0ac56/d327b稳定；app3top5leaf6PASS+handler2top6leaf7PASS=5top11leaf13PASS，另两packagePASS、两CLI0、0fail/skip，实际race/count1。新全独立Stats审bounded接受。

仅service将storage pointer audit投影完整值数组、空集合[]、nil entry失败关闭、handler envelope及独立RequireSuperAdmin缺身份/audit calls=0；**不证明全router鉴权矩阵、全部store调用阻断或嵌套深拷贝**。没有PG/Node/browser/build；owned PGID33948/34028无成员。可把原Stats“尚未实施”更新为已实现/限定runtime层，不回贴dfb whole绿。

## E-WIRE-V2 → F：11文件82通过，但全TS compiler仍2错

source `7d829da2afb3a939127f8350541ec6b579d6cea9` / closure `423d94281ea2867d16b174ace9738fb20975bfd4cc39006acf042abc98de492e`，[原review](evidence/R5-WIRE-V2-TYPEGEN-TSC-11FILES-REVIEW.json)/[32原payload](evidence/R5-WIRE-V2-TYPEGEN-TSC-11FILES-LOGS.tar.gz)。18signed pins/tsconfig SHA稳定；installed Next16.3.3 official typegen0，用四fresh ambient实际来源/hash编译，旧ambient独立capture但不输入，用户.next不动，非production Nextbuild。

仅11files actual82PASS/CLI0/0skip：wire2、snapshot41、draft10、compose3、versions3、page11、grants2、domain1、adminpolicy2、admintenant3、identity4。tsc exit2仍旧 `r5-protocol.test.tsx:56` recipients字段不存在/implicit any两个错误，原27诊断中的10 stale generated与17已授权类型错误在此源不再出现。原独审bounded runtime PASS不是whole frontend/typecheck绿；旧protocol单fixture仍prepared待实际修。原600窗含capture/typegen/tsc/tests/cleanup12.246758s、allgroupsgone，无Go/PG/browser/build，不借旧41绿。

## E-ABC-C2 → F：Go17top通过，正式capture validator失败

source `33307f7bfd87a24b758f5020669c9b313337e3d3` / closure `68cf6eabf087a5d9b206fd478bd3493fef915b78eb9959b9c0abec9514a8c386`，[原review](evidence/R5-ABC-C2-GO-PASS-CAPTURE-FAIL-REVIEW.json)/[58原payload](evidence/R5-ABC-C2-GO-PASS-CAPTURE-FAIL-LOGS.tar.gz)。实际17top/59leaf/73racePASS/0skip，含本次formalCLI内部TestCompanyHTTPContract Go0/gate evidence pass；**formalcapturevalidator1**两相关error同根：recipients实际data为aggregate DTO，而原OAS仍array及该GET200成功variant未承认。81原response byte captures私留，不公开body/邀请secret/objects/DB。

本次GET/company/events200真实完整ready，Cache-Control private/no-store/no-transform、X-Accel-Buffering no、仅tenant_id字段：[安全hashproof](evidence/R5-ABC-C2-SSE-READY-SAFE-PROOF.json)。不是借旧SSE PASS；只本次ready/body/header范围，不完整transport/长留存/UI链。nativePG64862 stop0/PID/socket gone/testDB0conn0与innerGo65521 group gone；总46.096282s≤600。旧dfb失败/206未start不回填。

## E-RECIPIENTS-READONLY → F：旧81wire与新doc对档通过，不是fresh formal CLI0

新三SOURCE contract/7pure实际通过；只读对档source `459010f34ace72f207e05ebed0a236dcd2443722` / closure `a07ca5e04d693f1b93b517a080d09afc9e032e530a75d21bcb9ff12f3d851f0b`，[原context repair review](evidence/R5-RECIPIENTS-OLDCAPTURE-NEWDOC-READONLY-REVIEW.json)、[原result](evidence/R5-RECIPIENTS-OLDCAPTURE-NEWDOC-READONLY-RESULT.json)、[19原payload](evidence/R5-RECIPIENTS-OLDCAPTURE-NEWDOC-READONLY-LOGS.tar.gz)。81/81 validate_response PASS、67/67 successvariants/66operations、check_company_operations0/no errors；live DNS两操作按原excluded边界，不冒全部网络provider证据。

新doc9afe1749637dc9cc2f66956f4026a89244735f1242f203a75003390723a0656f，checker a1199…；原capture b09e44518f7e4c97e9dcc81c30df29ebe5adc40ddbc5da4b804d778ed90f6ced、embedded spec a4935966…、producer source33307与原CLI1/result全不动，原stale spec hash守卫不删。**只读validator对新doc成功，不是新capture/GoPG producer/freshformalCLI0**。

第一次orchestrator缺scripts sys.path import ERROR=0cases/CLI1与旧b70包保；只修orchestrator导入path、无schema/validator产品再改，同原180窗两attempt及cleanup141.248612s、actual只读复验一次/groupgone；不重已PASS5+7/17Go。

## E-PGDIAG → F：旧dfb派生真实失败诊断，不是180整包修复

derived closure `6183c130818b10fa82ff758c08a7ba8ecffe0064fce5054badc22a9ca9ba5d83`，parent dfb；[原review](evidence/R5-PG-PHASE-OLDDFB-DIAGNOSTIC-REVIEW.json)、[实际metrics](evidence/R5-PG-PHASE-OLDDFB-DIAGNOSTIC-METRICS.json)、[27原payload](evidence/R5-PG-PHASE-OLDDFB-DIAGNOSTIC-LOGS.tar.gz)。8top/21leaf/27run=24PASS3FAIL、Go1、0skip，各8top真实执行无Go60/outer180 timeout。Suppression两个子项及父失败，raw只打印旧migration16断言失败，**实际18仅由已审源含18且成功Migrate推断，不标成原始SQL版本输出**。

472phase events=221paired spans+30observations、15fixtures，v2按fixture/span或phase/event去重，0unknown/duplicate/unpaired/unfinished。median CREATE35.772625ms、migration_up172.930125ms、**new_migrate_total175.162ms、store_new_total179.0985ms**、DROP47.964417ms；fixture return→cleanup2023.042166ms包括seed/body/test defers，不pure body，不累加nested spans当wall，不据此推整PG包timeout因果。sampling .2s峰值7conn是下界、unknown0，SQL wait只类型聚合无query/内容。

原全新PG phase审接受有限真实失败诊断。[独立措辞纠正](evidence/R5-PG-PHASE-OLDDFB-DIAGNOSTIC-WORDING-CORRECTION.json)另文件，不改raw：approved v2 packet执行端JSON重序列化字节/hash不同，但reader确认parsed JSONequal；不把semantic equality叫byte equality、不替换author pin。outer含init/compile/cleanup40.377948s≤180、PG70675stop0/PID/socket gone/testDB0conn0/Go groupgone。不是主树/ABC/Stats新源、不是206尾部补测、不是产品whole PASS。

## 中央契约更新与下一独占工单

[两matrix语义行](evidence/R5-API-RECIPIENTS-STATS-SEMANTICS-UPDATE-20261002.json)仅改GETrecipients与GETadmin/stats的content/evidence；132注册method/path/handler/middleware/condition/source/位置/authority等不变，其余130rows不动。Recipients普通route只聚合metadata，即使current content-read也不在此路由透出逐地址/BCC/SMTP诊断；unknown counts省略不造零，独立currentcontent/inspection边界保。

1. **RES-PG-FIXTURE-LIFECYCLE-180-01**：五file/fourtop parallel当前SOURCE prepared独审→sole新affected生命周期证据→稳定wholebackend，原180累计、两无terminal/206未start保，不延期/假Invite死锁。
2. **RES-TS-LEGACY-PROTOCOL-FIXTURE-01**：原r5-protocol单fixture shape最小适配→新独审/sole affected及fresh typegen/tsc，不改普通metadata或降低OAS；82旧PASS不重。
3. **RES-HTTP-FRESH-CAPTURE-01**：新精确recipients对象contract与currentsource→正式producer fresh capture+原specHash严格validator，readonly旧wire对档不代fresh门禁。

current18 fresh backend仍FAIL，完整formalCI/secure roles/shipping cold/browser/current事务AST/coverage、P6真实PG/SMTP等依旧具名缺口。原171完整验收范围/10已勾不因73/82/7416数量关父，M失败/无有效baseline不变。
