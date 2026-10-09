# R5 官方 CI、132 路由与依赖闭包维护

**原正式fullGo仍失败；新五簇affected Go通过不等最新整树fullGo0。** 本批完整补5条真实路由而非只改132数字，补benchmark14个直接安全依赖并保后续21项真实fixture错误，事务目录未用换hash伪造验收。父完成仍10/171，P0/G0/最终门禁未闭；无发布动作。

本agent独占中央docs/evidence；OpenAPI专用SSE写集交permission_contract，生产修复各owner独占。所有Go/PG/target由soleexecutor实际运行，本文未重复测试/validator/每文件hash。

## Evidence：原失败与新必要修复分源

| ID | source与原始层级 | 交付/实际结果 |
|---|---|---|
| E-FULL-FAIL | canonical actual current-dirty backend dependency closure SHA1 `2aa9a91a5c80d3d926861bc5e90cf25e13ded5fd`，504依赖pre/post相等，非Git commit | [原review](evidence/R5-CURRENT-DIRTY-FULLGO-FAILED-REVIEW.json)、[133选中原payload安全档](evidence/R5-CURRENT-DIRTY-FULLGO-FAILED-LOGS.tar.gz)、[包装清单](evidence/R5-CURRENT-DIRTY-FULLGO-FAILED-DELIVERY.json)；默认`./... -race -count=1`实际Go1/7test fail events/5簇/51包均终态，295.936s<1200，original247/current773 classifiers均1 |
| E-ROUTE-AST | permission_contract只读复用正式collector导出，actual132/docs127，missing恰5、extra/old composition changed皆[]；没有运行测试 | [actual132 AST](evidence/R5-API-MATRIX-132-AST-ACTUAL-20261002.json)、[原缺5 delta](evidence/R5-API-MATRIX-132-AST-DELTA-20261002.json)、[doc freeze](evidence/R5-API-MATRIX-132-DOC-FREEZE-20261002.json) |
| E-SHORT | 新source `5d1b723b7dae6d4fbc94bb0405bd35af91ceb396`，23explicit delta+14原字节safe refs，非原2aa9 relabel | [cutoff原review](evidence/R5-M-PRE-CUTOFF-REVIEW.json)、[50原payload档](evidence/R5-M-PRE-CUTOFF-LOGS.tar.gz)、[包装清单](evidence/R5-M-PRE-CUTOFF-DELIVERY.json)；api-null/goose16/received2/fixtures2/route132五簇affectedGo各0，protocol与registry仍1 |
| E-AST-802 | sole仅静态 `cmd/r5txinventory` 新原AST `/tmp/tm-r5txast.scRc2qsk/rawAST.json`，作者SHA256 `802eb7f397cc6dfe1e8ee0643739f959fff11f4633af732d574fae19a6fee33e`、exit0，无PG/tests | [当前snapshot审查/未核目录](evidence/R5-TRANSACTION-CURRENT-SNAPSHOT-REVIEW-20261002.json)：当时380PG函数，352旧+28新、14body真实变、47仅位置变；不当前officialcoverage已通过 |

原FULL五簇：ReleaseMemberPatchNullVersusAbsent旧null协议200/当前409、route132/127、Goose实际16/旧expected15、R3搜索缺结果、cacheexpired旧夹具未进入等待。7fail含parent/child事件，不是7个新漏洞。build/vet/http-contract/contract-drift各0与official4负验真实Go0/0/0/1→gate1/unknown0保独立phase，不据这些洗fullGo。

原其它CI：r5fixtures2旧usersPATCH profile409/expected200；Python421/25errors中21缺registry archive capturedep，另route/transaction drift；audit-db A04–A07 target红保、A01–A03缺exact assertion未接纳；protocol非Git128发生在tagged Go之前，不当PG产品红。原failed cluster已stop，但`tm_test_94902cbe`残余DB在datadir、0connections，**不能写DB0**。

## Finding→Path：完整132 operation修复

**F-ROUTE（validated/high，注册集合限定）**：正式文件是 [R5-API-MATRIX.md](R5-API-MATRIX.md) 与 [JSON](evidence/R5-API-MATRIX.json)，不存在此前口述的OPERATION文件。新5每条包含完整request/response/auth/resource/version/audit/content/source/TODO：

1. GET `/admin/permissions/{id}/deletion-preview`：UUID，无body，完整profile revision/members/change before-after锁快照；无mutationaudit。
2. GET `/admin/users/{id}/permission-editor`：raw nullableprofile/overrides/effective+来源/复合revision/capabilities，不替旧effective-only GET。
3. PATCH同editor：64KiB strict expected五required revision+patch，patch整个null/array/missing拒；fieldomit/null/false/0/四mode不混，fresh授权CAS/required audit+outbox，empty noop不消费。
4. POST同editor `/assignment`：四required，旧compound与新profile版本/范围同Tx，assignment+override原子。
5. GET `/company/events`：条件CompanyRepository非nil且assert reader ok；RequireAuth+RequireAdmin与下游fresh frame fence，LastID只advisory，text/event-stream ready/resync+strict minimal management invalidation，不平台全播或正文read。

旧127 **method/path/handler/middleware/conditions/source tuple不变**；source line只同步collector位置提示。仅受影响6旧P1语义更新：legacyPUT/DELETE handler直接409、profileUpdate/Delete CAS+影响确认/required audit、fresh profile list。既有inspection操作不属missing，freshselected platformsuper/required audit+outbox/专用安全projection保持，ordinary DTO不扩。TypeScript建档122/125调用统计明确历史，不凭新5声明当前全量TS AST。

P-R：独立AST缺5→逐source合同→完整132矩阵/六字段边界与新请求响应TODO→两owned文档最终freeze→sole **`^TestR5RouteInventory$`** -race实际Go0。不改/放宽checker，不重旧127suite，不将132×所有角色runtime认证。当前矩阵语义freeze在5d1b cutoff；当时CreateProfile仍legacy pool单INSERT、非fresh actor/companyTx/required audit。后续新的CreatePermissionProfileGuarded是另外WIP/source，不直接移贴此注册target证据。

OpenAPI作者只补真实GETcompany/events+3専schema/4new静态targets，未跑这些targets，不声称全OpenAPI/currentresponse通过。已知profilequota schema int64上限与现service/DB int32差异具名待裁决，不偷偷泛扩OAS或lower验收。

## Finding→Path：registry依赖修≠21测试通过

[14直接引用closure](evidence/R5-BENCHMARK-REQUIRED-ARTIFACT-CLOSURE-20261002.json)只含三个成功S row直接引用safe tar/review/原frozen contracts、S87 METHOD副本/resultbinding/tool review及direct delivery refs。原字节加入newshort snapshot，合法新dependency身份，不全拷evidence/private数据、不fakeGit HEAD、不污染历史S87与currentM d286。

**F-DEP（validated/high，capture修复）**：缺archive问题已解决。sole只重21之前失败setUp用例，现真实 **KeyError `review['archive']` line472、21setup errors/CLI1**；不是仍缺files，也不是registry PASS或产品RED。原author reviews不改造假archive字段，fixture必须按真实registry row/archive+各代review形式读取并保严格source/raw/hash/method验证。

P-D：successfulrow directrefs→new snapshot依赖closure→仅21受影响target→fixture-schema错误显露→M后owner窄修。原21missing与新21shape错误各有原stderr，不靠重写旧review讨绿。

## Transaction目录：不能只更新hash与unknown0

现合法入口为`cmd/r5txinventory`一次AST，`scripts/check_r5_transactions.py`只有read-only `--catalog`，无`--write`；合法派生为classify(ast)/migration_inventory()/精确四类assertions/caller候选。callback内SQL已被AST遍历，字符串/constant源表达式不是runtime全序，caller仍name-match-not-dispatch-proof。

E-AST-802当时28新增：P1 owner20+transport owner2+principal owner的新predicate1+inspection1，共24新函数已有逐函数map；其余4 received helpers仍在具名owner读审收口，14body变化及47位置变化分别保，未统一unknown清零。旧68manual slots/旧catalog/已accept父baseline不等新current实现运行时全绿。

migration16三trigger明确ON users/profile/overrides，global noncycling bigint sequence/defaultnextval，assignment DISTINCT/override identity23514/FK SETNULL及cascade/noaction、sequence rollback gaps合法/Down failclosed。现migration_inventory最后ALTER child_context误继承到后续trigger function，不能把这个提示当实际作用表；人工ON-table source map分列。旧DB catalog仍officialGoose15/7usertriggers，**不静改为16/10**。

后续root批准新guarded profile创建family已落但不在raw802：`permission_profile_create.go:CreatePermissionProfileGuarded`+ProfileCreationStore/Service路由；也不在M immutable8529。需该owner独审/actual后新AST与新catalog再合法更新officialcoverage，不把旧Create pool语义或802当当前新family审绿。读catalogSQL/requiredfields已交sole，M后窗口才采，不在M前cutoff无限插scope。

## 当前具名依赖/缺口

| 工单 | 精确依赖与验收 |
|---|---|
| RES-CI-FULL-FRESH-01 | 五簇affected已过，但原2aa9fullGo1未重；稳定最新source并收口其它正式CI后，唯一freshfullGo/classifier/全phase，不由组合局部0冒整树0 |
| RES-REGISTRY-FIXTURE-01 | 14dep已真capture；owner仅修BenchmarkRegistryTests.setUp支持原row/archive/不同review/sidecar，保21严格反例、不改变历史review；sole仅受影响21，真实pass才接 |
| RES-TX-CURRENT-01 | owner逐new/changed函数source/effect分类，后来Createguard family独审→freshAST→精确syntax/classify/assertions/caller/FKtrigger派生；unknown具名保，official current未final，不hash-onlyPASS |
| RES-TX-CATALOG16-01 | M后freshowned DB经official New/Migrate，实际FK/triggers/defs/checks/allocator/source/Goose/cleanup provenance；新文件保旧15catalog历史，不预填计数 |
| RES-API-QUOTA32-01 | profilequota OAS/Go/service/DB int32边界精确合同与外部客户端兼容决策；原SSE4static不吸收旧quota差异 |
| RES-PROFILE-CREATE-01 | 新guarded create owner source/freshactor/tenantanchor/required audit-outbox及global空audience政策；newleaf不旧802，不当前API freeze全runtime；raw旧lowlevel留接口但formal no fallback须实际证 |
| RES-PROTOCOL-CURRENT-01 | explicitmanifest644接头已合法accepted并真正19tagged PG尝试，但旧409/retention/BCC等fail保；formal currentexpectedtarget矩阵与各source政策对齐，不能nonGit128修成taggreen |
| RES-M-03/02 | root已批准stable8529/d286唯一10800s80GiB/1M；截止本批cutoff原receipt明确未start，新M runtime start/result另接，旧S87不污染/L拒；不拿laterCreate/main树贴M |
| 原P1/P2/P7/P9/P11与G0 | SSE长期/多实例/容量/平台通知、GoPG→shippingUI/browser、currentcold、receivedlegacy/incarnation、全最终scope照原171保；browser2newpure0不是实际browser |

## 安全归档与状态

FULL安全档133原payload，排除credential-shaped CI YAML/source diff（原件私有保）；cutoff50原Python tar直接copy，原source/raw/exit保。原tarhash及safe wrapper hash分列，不每文件再hash。原536等历史包不覆写；无privateDSN/JWT/rawmail/objects/CPUtrace/DB/fullsource上仓。

```sh
# cwd=docs/company-mail；只读，非测试重跑
python3 -m json.tool evidence/R5-API-MATRIX.json >/dev/null
tar -xOf evidence/R5-CURRENT-DIRTY-FULLGO-FAILED-LOGS.tar.gz phases/default-full-go/go.exit
tar -xOf evidence/R5-M-PRE-CUTOFF-LOGS.tar.gz phases/route132/go.exit
tar -xOf evidence/R5-M-PRE-CUTOFF-LOGS.tar.gz phases/registry21/go.exit
```

预期1/0/1。本轮无commit/push/PR/部署；**10/171不变，事务/catalog当前未final、原fullGo未绿**。文档后只刷新Code-Index，不deepbuild。下一优先M后**registry21 fixture、currenttransaction+16catalog、protocol/currentfullCI**；M不等无限扩队列，实际状态需新owner receipt。


Browser另有[2newpure原packet](evidence/R5-BROWSER-REPAIR2-PURE-LOGS.tar.gz)/[交付清单](evidence/R5-BROWSER-REPAIR2-PURE-DELIVERY.json)：driver746d/test02e，实际tests.exit0、controller0.221409s、stdout两case/stderr空；新独审限定接受，旧7不拼9，没有actualbrowser/server或GoPG/Docker全链，packet未给独立groupabsence不推全当前groups已无活进程。
