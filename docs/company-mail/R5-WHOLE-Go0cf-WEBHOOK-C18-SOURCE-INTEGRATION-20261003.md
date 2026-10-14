# Whole-Go0cf失败、Webhook与C18资格状态

> **后续增量已更新：** 当前主树physical usage19的A FAILED/D1P-D2F/D3BSC未run、C18pure21实际限定PASS/catalog15pure、冻结18 checkpoint及PGphase SOURCE BLOCK见[最新状态报告](R5-CURRENT19-USAGE-DIAGNOSTIC-C18-STATUS-20261003.md)。下文是原各source与时间层；其中申请/未运行记载不覆盖后续actual，也不由新组件回写旧whole/M失败。

**最新正式whole仍Go1；139、22、164及receipt5各子层通过，不合成父绿。** [安全交付](evidence/R5-WHOLE-Go0cf-WEBHOOK-C18-SOURCE-DELIVERY-20261003.json)登记原包及append关系；原M/三S/FAILED/raw不改，未重测试或全treehash，无发布。

## E-WHOLE0cf → F：GOMAX2实际执行，结果真实FAIL

source `0cf5eaa15575ea3b1cb8ef72c18777a07b6e37ca` / closure `53201fc24ef67211ffd4e8b1ec4c30d4305ab6d403a9a251b87bf7022499896c`，1229frozen输入/fullfork/embed/安全直接artifact deps，[原review](evidence/R5-CURRENT-WHOLE-Go0cf-FAILED-REVIEW.json)、[83原payload](evidence/R5-CURRENT-WHOLE-Go0cf-FAILED-LOGS.tar.gz)、[真实落盘独审](evidence/R5-CURRENT-WHOLE-Go0cf-INDEPENDENT-REVIEW.json)。canonical closure非Git commit。

正式`go test -json -race -count=1 -timeout=180s ./...`，actualGOMAX2、readonly/p1、Go1.25.7/GOWORKoff；没有旧27633的GOMAX1偏离，不能混为同一次。908declared/907required/51pkg，8021run/7993PASS/6FAIL（4leaf2parent）/1合法BrowserSkip/21unfinished；raw32829events，51pkg全terminal=38PASS/12无test skip/1PGfail，不所有tests完成。

PG180.693s包累计timeout，19白名单PAUSE0CONT/128top未start，尾ProfileCreate/admin-stale-super-hint约0s进入seed/migrate被alarm截断，不单case三分钟死锁证明，也无双lane收益观察。4失败leaf为LegacyRecipientProjectionRechecksAfterLedgerWait、KeyExpiryAfterQueryWait/attempts、ListMatchesDetailScope、OwnerlessKeyAndRetryScope/ownerless；锁waiter未观察及旧lifetime/hint期待保，不仅凭断言判生产权限回归。

两个classifier1：old247 missing-or-notpass180、fresh907为395；包含failed PG package中已PASS目标，**不395未run**。128是独立raw/declaration未start差集。Go wall330.959617s/原init→cleanup336.792381s≤1200；native53862stop0/observerjoin/PIDsocket/groupsgone。base0conn及tm_test_39f80e35…0conn留private stoppedcluster，不allDB0。1500known samples/531distinct temporaryDB/peak17是下界，不硬97容量或lane2认证。独审接受authentic bounded failed attempt，不whole/four-job/G0绿；旧dfb206/27633的201不回填本source128。

## E-RECEIPT5 → F：两fixture文件窄修，5target actual PASS

新SOURCE独审准入只两个testfiles；旧key10连续块和request helper不变，productionGo不改。F1保ledger lock/exact blocker/deadline，marker改1024字节内WITH visible；F2改真实outbound_jobs锁/actualTTL；F3/F4去陈旧subject/body期待，freeze用state/exactID修相邻false-green，不降低metadata权限。

[sole原review](evidence/R5-RECEIPT-WHOLE4-FIVE-TARGETS-REVIEW.json)，source `5373e23ccdf5aea57a7664f7d565ec1fcea68a7b` / closure `b3d0ea67fdca84a5a22a41389fef680db4de4dc4b4290a281085bbb676f9b76a`，772fee/479468两pins；5top8leaf10racePASS/Go0/0skip/unfinished/GOMAX2，四waiter/blocker/deadline实际日志。实际MVS/fullfork/embed编译输入审计0无缺，selected productionGo与0cf byte相同，不借旧runtime。26.169s≤原360，PG71914stop0/PIDsocket/groupsgone，tempDB无但base tm_receipt5_validation0conn私留，不DB0。原readeractual已bounded接受，不whole0cf/尾部补全。

[exact14追加纠正](evidence/R5-RECEIPT-WHOLE4-FIVE-TARGETS-EXACT14-CORRECTION.json)从已有0cf/new5373 manifests确认14refs全包含且sha相同；原SOURCE仅byte-equal14ref要求，不能说原先已有独立14pinmanifest。原90包138b/new91包fd93仅追加一correction。

**安全重包：** 原两包`executed-controller-source.py`有literal private connection URI，不沿用作者URIscan0。原private90/91/raw不改、不直接入仓；公开保[safe89](evidence/R5-RECEIPT-WHOLE4-ORIGINAL90-SAFE89-LOGS.tar.gz)/[safe90](evidence/R5-RECEIPT-WHOLE4-EXACT14-91-SAFE90-LOGS.tar.gz)，[包装receipt](evidence/R5-RECEIPT-WHOLE4-SAFE-PACKAGING-RECEIPT.json)绑定138b/fd93与新wrapper SHA、剔1成员、其余原bytes不变；仅新包装一次hash，不每memberhash或改原控制器。旧key10不复、whole失败不洗。

## E-WEBHOOK → F：137+BC05-only2独审composed通过

source `ff73f8ea4d26e87f483cb73d5a29cd32f06fe5bf` / closure `97ba558ec6159c10b759624250cfb2299b267f4d3ba01ec134b236baf57c5357`，720source/九file27f5 tuple。hooks22top123leaf132racePASS+config1top4leaf5racePASS，四realLoad child/parentCHILDCASE清理。BC05首0产品test是unquoteduppercase CREATE折lower/DSNuppercase setupERROR，不产品RED或worker/BCC证明。

[首review](evidence/R5-WEBHOOK-V2-HOOKS-CONFIG-PG-INFRA-REVIEW.json)/[原75包](evidence/R5-WEBHOOK-V2-ORIGINAL75-LOGS.tar.gz)/[76append包](evidence/R5-WEBHOOK-V2-HOOKS-CONFIG-PG-INFRA-LOGS.tar.gz)，22.388s≤原420/native32698stop0/groupsgone；tmpquery仅tm_test_未覆盖base tm_webhookv2_validation，不假allDB0。[标签纠正](evidence/R5-WEBHOOK-V2-COMPILE-LABEL-CORRECTION.json)仅说明模板遗留handler3/private490非本scope，真实720/137P/BC05body0；4887原档不变，新4470只添一成员。

BC05-only新PG quotedlowercase name/DSN/actual18，[原review](evidence/R5-WEBHOOK-BC05-ONLY-REVIEW.json)/[83原payload](evidence/R5-WEBHOOK-BC05-ONLY-LOGS.tar.gz)：parent+BC05/webhook2racePASS/Go0/0skip/unfinished，realoutbox worker POSTmetadata/BCCnegative/persistedackdelivered/join。GOMAX2/p1/parallel1/tag/count1/120，childavailable224.820≥120；17.993s≤原240/native41825stop0/PIDsocket/phase-parentgroupsgone，base tm_webhook_v2_validation0conn私留，diagnosticDB无，不allDB0。

新独审接受[composed139摘要](evidence/R5-WEBHOOK-V2-COMPOSED-BOUNDED-ACCEPTANCE-20261003.json)（中央摘要非伪raw）；可闭已批准目的网络策略及bounded137+2子scope，不singlefresh139/双consumer外部exactly-once/P7父项。受众/平台通知/多实例与whole/171依赖保。

## E-SOURCE/MIME → F：composed/native子层接受，不currentMETHOD

[SOURCE V4 last1](evidence/R5-CURRENT-SOURCE-V4-LAST1-REVIEW.json)/[10原payload](evidence/R5-CURRENT-SOURCE-V4-LAST1-LOGS.tar.gz)：fixture-only1d790f/139ca179，单testfile ef0f→50bac；1PASS/Python0/noSkip/subprocessaudit未触发/无GoPGnative，0.517s≤60/groupsgone。仅namedsynthetic platform literal/mockonce及realpreflight余量/预算/sourceMETHOD/ambient/错label/nopolicy拒dispatch保，不再capture或回贴079972。

V2 20PASS+V3 first stderrPASS+V4 lastPASS [composed22已独审接受](evidence/R5-SOURCE-POLICY-COMPOSED22-ACCEPTANCE-20261003.json)。V2两setupERROR、V3 second audit-before-spawn exit97/无terminal-result/unknownargv保，platform调用链只静态候选，不把未知argv改称uname。实际972capture仍仅V2，不fresh22/新checkout/currentSML资格。

MIME forkV5实际25top154leaf164racePASS独审bounded接受，原33包050b保。[34append包](evidence/R5-MIME-FORK-V5-NARRATIVE-CORRECTED-LOGS.tar.gz)/[narrative correction](evidence/R5-MIME-FORK-V5-NARRATIVE-CORRECTION.json)：T3在新V5下实际重执行旧V2二十六counterexample leaves且PASS，没重跑旧V2实现/原run；T2 cancel/success才有active1waiting，refusal仅initialactive2同时release、一份拒另一份success，不声称同一等待断言。messages/整个upstream没复，无Dockercold/全CPUheap/P6父/currentMETHOD资格。

## E-C18 → F：七SOURCE准入不等pure21执行

C18-01/C18-03仅批准七paths，V1四SOURCE BLOCK由V2修闭且原new readerSOURCE PASS，只准申请newpure21，不producer/benchmark资格。maintenance完整字符串统一，synthetic mutation先同源完整正控/具名拒绝；外部canonical parent descriptor bytes/pin与admission exactpin绑定，重origin/内部全rehash不伪授权；Go recordedTime限owned go_start..go_exit/早于cleanup、package/test序与Elapsed相容，不拿parentwait兜底。

**旧actual18是真的但partial：** 原02a585 raw七集合/18migration SHA不变，缺column typmod、triggerenabled、indexvalid-ready、sequencecache-ownership、普通non-trigger functions。旧11是采集trigger_functions非全部functions。[准确分类](evidence/R5-CATALOG18-HISTORICAL-PARTIAL-C18-BOUNDARY-20261003.json)不改原raw/静态补默认值；reference_shape_complete=false/runtime_reference_ready=false，实际admission/archive failclosed待C18-07。结构数字不能fullreference hash资格。

首pure21 source6710e89d/52c2f082七pins等，[原review](evidence/R5-C18-V2-PURE21-RESOURCE-INFRA-REVIEW.json)/[18原payload](evidence/R5-C18-V2-PURE21-ORIGINAL18-LOGS.tar.gz)/[20append包](evidence/R5-C18-V2-PURE21-RESOURCE-INFRA-LOGS.tar.gz)/[infra分类](evidence/R5-C18-V2-PURE21-RESOURCE-INFRA-CLASSIFICATION.json)：Darwin RLIMIT_AS1GiB ValueError在wrapper资源setup，child66823exit1/groupempty，**0tests、discovery/audit未armed**。CPU setrlimit60返回无childgetrlimit回读，memoryguard未建立，不0违规/0fail即PASS。review0.162856817s/交付0.178023815s另界，不90s timeout。991a原包保，2593只append分类/控制器源，原18成员bytes等。独审接受bounded resource-setup INFRAERROR，不纯断言RED。

RSS资源V2 [15SOURCE包](evidence/R5-C18-RSS-V2-SOURCE-BLOCKED-LOGS.tar.gz)/[原receipt](evidence/R5-C18-RSS-V2-SOURCE-BLOCKED-REVIEW.json)：90wall/CPU60/1GiB sampledRSS、100ms target/maxgap250ms；首SyntaxError0test保，括号only修后15newmock P0.003s，child后audit扩项仅AST。新reader三BLOCK：90检查早于sourcepost/receiptseal；ps返回时刻不证明采样晚于ready/completion；尾gap不计maxgap。15mock不实际RSS/native handshake，不准pure21。

RSS V3 [9SOURCE包](evidence/R5-C18-RSS-V3-SOURCE-PREPARED-LOGS.tar.gz)/[receipt](evidence/R5-C18-RSS-V3-SOURCE-PREPARED-RECEIPT.json)：new8mock PASS/old15不复，专属posthandshake query、tail maxgap与晚publication revoke原reader已核修；**仍一SOURCE BLOCK**：WholeClock仅write/seal，loop/exit在另一Freshness且cleanup不推进同owner，退出后小回拨可能漏接。需单一parent observation owner贯穿loop/exit/cleanup/seal及跨状态mock，不能只直接测clock。

随后V4只有SOURCE候选 `/private/tmp/r5-C18-RSS-resource-V4-prepared/V4-SOURCE-resource-protocol-receipt.json`：controller8ad568e1/protocol62642106/child380927不变/tests b93c2844，作者称同一clock对象接loop/remain/write/seal/Freshness exit；首handoff fixtureRSSgap ERROR保，修mock后1P，再bind历史spawn窄fix后新第二case1P，精确testedbytes分层，不singlefresh25，旧15+8未复。**待原reader final SOURCE，pure21/实际controller/GoPG均未运行**。[分层状态](evidence/R5-C18-RESOURCE-PURE21-STATUS-20261003.json)保前三版本，不用作者修声明自动覆盖BLOCK。memory明确sample非硬quota，inter-sample峰值不能排除，未批准不直接retry。

**随后V4原独审仍BLOCK：** 跨Freshness/WholeClock回拨漏报已修，但remain改sticky check后，时钟异常/90s到期会在finally cleanup timeout实参重新抛出，使身份核验/TERM/KILL/join可能未执行；最终receipt路径再调用未保护remain也会中止。需qualification sticky失败永远不PASS，与有界containment/失败封证分离；不能资格失败阻止安全清理。4pins/317f42 archive/811f96 patch及mock分层已核，不是实际controller或21通过。

**随后RSS V5新SOURCE复审准入，runtime仍0：** [12成员源包](evidence/R5-C18-RSS-V5-SOURCE-ADMITTED-LOGS.tar.gz)/[原receipt](evidence/R5-C18-RSS-V5-SOURCE-ADMITTED-RECEIPT.json)，controller174da8fb/child3809276f/protocol62642106/tests77fad549与1f3ca13b原包一致。V4 block已闭：qualification失败由finally捕获，contain_owned_child不调用stickyremain，身份检查/TERM-KILL/join/finalobs/失败receipt独立有界；原90内不扩，只有已失败且到原deadline才root批准最多5s monotonic containment，永不恢复PASS。mock原2PASS+后cutoff1PASS分层，不finalfresh3/全suite。仅可申请root另授sole pure21一次；仍需exit0/finaladmission/原90/21exact/auditCPU/RSS握手/尾gap/cleanup全部实际证据，未controller/tests/GoPG/signals。

## 下一五项具名依赖与可派发brief

| 工单 | state owner / 窄写集 / 验收依赖 |
|---|---|
| C18-02｜唯一attempt runner/信任根 | 新runner模块+newtests，旧runner/history只读。在nativeinit/compile前封一次origin/所有earlierdeadline最早D，外可信descriptor bytes/pin/owner-evi-root准入与15phase/current source精确绑定；noresult/cleanup failclosed。先SOURCE+新pure，不运行SML |
| C18-05｜Go新harness/producers | 新Go benchmark harness及newtargets，先核exclusive write set；legacy M16/诊断不全局换18。真实Goose/fork/UIDFIFO/2800/7safety与parent descriptor/typed inputs-output/source绑定，尚无接线/运行资格 |
| C18-06｜临界parent clock/join | 与05划清文件/函数边界。31s±/compile偏移/更早parentD/4flight+16waiters真join/suspend/rollback/cleanup-noresult，取最早授权deadline，不MCAUSE4k min114秒代临界；pure→sole bounded actual另批 |
| C18-07｜真正fullcatalog reference | 新readonly完整SQL采集/schema/parser/tests及新catalog，旧02a585/15/17raw不改。typmod/triggerenabled/indexvalid-ready/seqcache-ownership/普通functions等全fields与18migration及runtime source绑定、canonical payload签准；不补默认/只count或qualhash升full |
| C18-09｜rootMVS/compile/embed/cache/toolchain资格 | 新readonly admission verifier/newtests/receipt；actual build context/fullfork/selected inputs/22embed/generatedtestmain/Go选入与externalcache version-checksum和freshbyte层分开。现GoList/组件编译不是统包证明，先独审再sole必要newscope，不复972capture/旧绿 |

当前schema18/fork/current-local-inputs-v2与frozen16/旧LOOKAHEAD files-only不同合同。新命名METHOD/workload/catalog/result SOURCE不currentproducer/tool/S/M/L；旧METHOD/DATASET/constants/三S/失败M/knownstrict分支不改。后续archive producers/currentregistry/cold/crossplatform具名依赖，完整300tool-only双method资格不替M，freshS/M逐次新签、L拒。RSS资源guard未准入前七合同pure21依旧未运行。

Docker用户将启动非readyreceipt，不预填环境/shippingPASS。原百万10800FAILED与MCAUSE4k/新组件分层；原171/10已勾不因139/164/972或小诊断变化，whole0cf失败/full四job/G0/secure/全角色/事务coverage/shipping仍未闭。
