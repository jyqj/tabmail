# R5 原规模性能数据集与基准准备（P0-100）

**历史S2、schema16 v1 S3d07与显式opt-in lookahead S4074分别完整采集；原M/M2与新852900原M均failed/incomplete，M/L无完整baseline，P0-100/G0未验。** 旧v1 S3d07列表9.118/4.145ms、search5932.551/6092.991ms保历史；新4074 S列表14.449/7.839ms、search16.251/15.802ms只cert S列表300ms，不能归因lookahead/M或relative绿。A/B4k不正式scale，默认v1不变。当前事实见末节及[runs registry](evidence/R5-BENCHMARK-RUNS.json)，中间旧执行段落只代表当时状态。

## 原契约，不允许降规模挑基线

| 规模 | 员工 | 邮箱 | 邮件 | 执行预算提案 |
|---|---:|---:|---:|---|
| S | 100 | 500 | 100,000 | 30min，磁盘10GiB预算 |
| M | 1,000 | 5,000 | 1,000,000 | 180min，磁盘80GiB预算 |
| L | 1,000 | 5,000 | 10,000,000 | 本批不执行；12h/450GiB扩展演练预算，发布必要性须P0独立冻结 |

原DESIGN139–141：20并发，S/M常规列表p95候选≤300/500ms，index-ready M正文查询p95≤1s；相同数据/安全条件关键路径相对基线不得无解释恶化>10%。这些是候选，需实测冻结。附件字节下载与公网SMTP不套列表阈值；未达阈值必须保留原值和调整理由，不能改脚本或小数据冒通过。

固定seed=3893945；2tenant按80/20分布，每员工1personal+4shared，总邮箱数严格匹配；每shared4个合法grant。raw sizes为80%4KiB/18%32KiB/1.9%256KiB/0.1%2MiB的固定随机分布，消息含唯一ordinal确保不是一份小blob冒百万条。80%inbox/10%archived/5%trash已到purge/5%shared硬过期；个人永久的历史期限不能被错误重新解释。详见 [datasets](evidence/R5-BENCHMARK-DATASETS.json)。

## 冻结准备实现与历史未验边界

- `r5_benchmark_test.go` 独立 `r5benchmark` tag。实际Go公司命令建合法actor/mailbox/grant；owned测试DB、生产filesystem object port与rawobject引用端口，不使用M-sized内存blob map。原始消息经真实mailcontent parser及PG index claim/complete完成，SQL核对employees/mailboxes/messages及index-ready全部计数。
- test-only `PgStore{pool: tracedPool}` 使用同真实PgStore实现；pgx QueryTracer记录实际调用数量，不拷业务SQL或使用Repository方法数假SQL数。校准实际20个SELECT1必须观察20；统计覆盖该pool上的request/worker/事务/控制SQL，不记录query文本或参数。
- shipping `api.NewRouter`、signed current JWT、owned Redis和真实loopback relay；actual workloads为list/count、index-ready search/message、permission/mailboxes、draft save/delete、submit+real worker、GC raw reference safety。20并发，每冷/热阶段200样本，记录p50/p95/p99、actualSQL count、alloc、RSS高水位、对象+DB磁盘。
- 每workload先验证foreign/current revoked source拒绝；seed永久/生命周期保真实源。fixture seeding中的有限UPDATE仅构造本ownedDB lifecycle输入，不复制业务查询。当前政策缺陷不会为benchmark偷偷修复或放权。
- cache标签明确：fresh router/owned Redis与同进程热阶段，DB/OS缓存未声明完全cold；不全局清生产缓存。cold采样前只清该owned miniredis namespace，不能叫完全物理冷态。

工具 `run_r5_benchmark.py` 默认仅budget preflight；显式执行必须fresh目录、40hex被测源、private ownedDSN和预算确认。它检查原规模、seed/concurrency/sizes/ACL、全部workload×cache、index-ready、SQL校准与完整metrics/真实Go run/pass/source无漂移。工具验证数据仅测试validator，不模拟HTTP或伪造性能结果。

## 先验证工具，不抢PG或大样本

```sh
python3 -B -m unittest scripts.tests.test_r5_benchmark
# 纯generator契约，仅1000个size抽样；不连DB、不seed原规模
 go test -mod=readonly -tags=r5benchmark -run '^TestR5BenchmarkGeneratorContract$' ./internal/store/postgres
python3 -B scripts/run_r5_benchmark.py --scale S
```

历史初版7项工具测试、纯generator与编译曾通过；暂停上传材料已记录真实SQL20校准通过。历史TOOL_ONLY dataset在mailboxes.kind错误列处42703失败，无result／EXPLAIN／validator成功。该旧失败发生时尚无S/M成绩；后来历史S2与末节新S各有真实完整采集，不把旧状态当当前。完整运行只能由integration operator在独立窗口安排：

```sh
# 仅在owner预先批准预算、私有owned DSN已提供、无其它suite占PG时
python3 -B scripts/run_r5_benchmark.py --scale S --output-dir "$FRESH_EVIDENCE" --source-sha "$FROZEN_SOURCE" --execute-approved-budget
```

S/M必须各至少一轮实际完整baseline、original raw JSONL/exit和result统计；L不执行是本批预算选择，不取消10M规格或后续必要性裁决。timeouts/OOM/磁盘预算/SQL未知/冷热点缺项均失败，保原输出，不靠降scale重命名成功。

## 资源与剩余验收

只读本机：Mac15,8、Apple M3 Max、16physical/logical CPU、64GiB、macOS26.6.2；取样时/tmp卷约776GiB可用，执行前须fresh重测。建议RSS上限16GiB、S/M磁盘安全余量≥10GiB，L≥200GiB；L平均raw估计151GiB，加DB/index可能到450GiB，不宜与其它fullsuite并行。

独立执行还需：预flight冻结硬件/seed/source/schema/规格hash；初始测量定位与SQL tracer开销校准；正常/错误安全断言不因性能被禁用；S/M原raw与候选阈值裁决；reference基线后P10同条件复测。source变化必须新run，不用当前parser2个microbench或工具小样本代替。 [runs registry](evidence/R5-BENCHMARK-RUNS.json) 仅登记完整真实scale：现保历史S2及新S，failed M与缺观察M2另列，不把诊断放成功runs。

## Pre-run真值纠正与工具样本（不算S/M）

初版按ordinal给life但只在shared上执行5%expiry，导致声明80/10/5/5与真实生成约81/10/5/4不一致；参数hash也不能叫actualdataset fingerprint。旧SQL20只证明instrument能数20，不证明这个错误dataset合同。原日志保留，原S首次预算失败未形成完整baseline、M尚未跑。

新版合同冻结：每20条exact16/2/1/1，expiry槽只选shared；tenant80/20按整life桶blocks分配，避免所有expiry都落foreign；每1000 size桶exact800/180/19/1，seed仅shuffle次序。必须实际SQL核对employees/mailboxes/personal/shared、四distinct legal grants、tenant/life/size/index-ready，流式canonical actual行计算datasetSHA；parameterSHA单列不可混。计划模型计数或随机生成器纯样本不替SQL观测。

工具真实小校准为 **20员工/100邮箱/1000邮件**，保证foreign4人可合法four grants；明确`TOOL_ONLY_DATASET_CALIBRATION`，不写S/M成功registry、不改原规模。观察raw filesystem+parsed index、shipping API、actual SQLcounts及capturedquery同bound parameters真实EXPLAIN JSON。source/schema/scale/index-ready、querySHA/parametersSHA记录，无credential/raw params输出。

metric边界：GC workload只读真实raw references，不声称sweep/delete/fairness吞吐；SQL只应用/worker池可见query（不计DB trigger内部statement）；memory是Goalloc+本进程high-water RSS，不声称测了PG/Redis全系统。未测system memory显式`not_measured`；getrusage失败/RSS未知与walk错误不可以0冒成功。缓存只受控app/ownedRedis reset/reuse，DB/OS完全cold未证明。

L具体裁决：P0原验收仅强制S/M各实际一轮；此批冻结10M规格和12h/450GiB候选预算而不执行，也不宣称支持10M。后续P10/最终发布如果声明10M容量，必须执行L并保真实结果，不能援引该预算字段或S/M外推。该裁决不降低S/M人口、20并发、size/ACL/原候选阈值。

Go实现与runner/tests分别由integration operator及独立reviewer独占，三份metadata由protocol owner持有；下次fresh小probe后才锁一致的field/schema/source契约。本节工具校准时registry尚空；当前见末节，SQL20与1000生成器/小dataset仍都不是S/M成绩。

## 恢复执行与证据版本

用户恢复多轮multiagent实施，起点HEAD `587ba75`，已验10/171。旧[暂停记录](R5-PAUSED-20261001.md)是历史，不再禁止当前获派工作。operator持有Go/PG，reviewer持有runner/tests，contract owner持有本文件、datasets/runs和TODO；任何一方改变被测闭包必须先同步并重锁source。

本轮先真实TOOL_ONLY校准，随后S/M独立完整窗口；不把7项历史工具测试、SQL20或1000条校准冒原规模baseline。P0-120/G0逐项审表见[当前审查清单](R5-P0-G0-REVIEW-20261001.md)，待真实证据后更新裁决而非预先勾选。

## 原S首次失败与下一准备调度

原100/500/100000、1800s首次窗口实际Go1800.600s超时1，无result/2800测量样本；100k message/document/ready仅准备。约25m32 seeding及4m27索引后在observer截止，真实失败与资源watch见[S失败独审](evidence/R5-B01-AJ-S-FIRST-FAILED-REVIEW.json)。不放宽预算、不重命名小数据、不用工具延迟冒原S。

原合同没有准备必须串行条款，准仅fixture准备≤20worker/pool24调度：ordinal/seed/tenant/life/size/raw身份及真实StoreMessage/parser/claim-complete保持，race/精确观测/stream fingerprint等价必须新证明；不改产品/schema或14×200测量/安全。新源fresh tool→原S→原M。M仍必须实测，当前10/171不变。

## 历史原S2采集与M门槛

原S2同tree68cafcc、原100/500/100000、1800s预算实际1270.63s测试pass/1271.658s包pass，Go/runner均0，14×200及SQL/实际分布/plan/指纹/安全/资源齐。S cold/hot列表p95=10.968542/3.983833ms，候选300ms通过；index-ready搜索p95=5602.020292/5865.429958ms，明确慢，不声称S整体性能达标。采集归档与性能缺口见[当前G0审表](R5-P0-G0-REVIEW-20261001.md)。

M仍同源原1000/5000/1M、10800s/80GiB预算必须完整实际跑。M原列表500ms、index-ready搜索/正文1s候选不放宽：若完整采集但超候选，如实performance candidate false，冻结原值/环境与原因，保P7/P10具体性能缺口；100初始基线采集不等于G10最终达标。timeout/缺采样/未知失败不构成baseline，不允许用S或小数据替M。

冻结datasets的execution_state是该source生成时状态；当前实测状态以runs registry及新验收材料为准，不在M窗口改dataset hash、把旧结果贴新closure。

## 历史首原M真实失败与当时下一窗口

首M原1M在host Clamshell/Maintenance Sleep受扰，绝对10800s截止时无法执行kill，首次resume20:52观察wall11515.358s后owned TERM/KILL；Go-15/runner1、无result/完整index/2800sampling，实际484744 messages不是M基线。保[失败及清理证据](evidence/R5-B01-AJ-M-FIRST-FAILED-REVIEW.json)，不称按时停或性能候选红。

用户已同意接电开盖下一原3h窗口。先新严格wall-budget、wall/monotonic交叉采样及真实registry审核工具、冻结新source再双校准，随后原M2。原人口/seed/10800s/80GiB/20并发/14×200/候选阈值不变；S2旧source/contract单独真实归档，不能用新元数据改旧结果。100/G0与完整P1–P11未完成。

## Clock合同与历史M2准备边界（不改性能目标）

新clock_contract固定wall elapsed采样并与monotonic交叉，row max_wall_monotonic_gap_ms有限0..100；异常gap/负wall拒整轮。execution_wall实记录UTC起止/elapsed/budget，包含suspend，不因monotonic漏计延长原10800s。host睡眠时无法执行kill则醒来拒overrun/清理，不能称按时stop。

100ms是仪表异常guard，不是300/500/1000ms性能候选调整。人口/大小/共享资格/seed/14×200/C20/SQL与安全仍原契约。旧S2历史合同0b828真实单列，按当时source/raw证明，不附伪新字段；下一工具/原M2需新9af57数据合同与新source实际验证。

准备调度的实际batch边界：历史M2冻结fixture调用`ClaimMailIndexJobs(ctx,100)`只是requested输入；正式`mail_content.go`将超过20的limit归为5，故当前实际每次最多5jobs、lease90s。20workers/queue20表示准备调度上限，不冒实际每次claim100或始终20并发；parser原4gate/pool24保持。原M2后来确有failed/incomplete终态：runner1/cleanupEPERM，Goexit缺观察；不补造退出码或性能红。后备合法Claim20/pipeline的工具和新S已分源记录。

## Schema16 bounded pipeline 工具交付（当时仅工具，不外推原规模）

新合同SHA `9e81020680dbd6e990cbb35ab1aac16413885e65306d949a5889c4bafda00b36`固定12键：window/active20、合法Claim20、原Parser4/30s、31s原wall起跑余量guard、所有started join、deep-ready checkpoints、no显式ANALYZE/planner override、六实际SQLstream。预算、seed、人口、14×200和性能候选不改。

新唯一runtime owner一次执行SQL20、dataset1000、两freshDB完整14×200（共5600样本）与3真实lease/token/source fault+positive，Go/strictCLI均0；六actualstream及canonical b36f相等，prep约13.55/12.27s，terminalReady1000/inflight0/parserquiescent。safe交付见[工具原review](evidence/R5-B01-AJ-PIPELINE-TOOL-REVIEW.json)／[44个实际证据文件安全档](evidence/R5-B01-AJ-PIPELINE-TOOL-LOGS.tar.gz)。此工具批次没有重复pure/旧green，当时未运行S/M/L；随后同源新S见末节，不反改该工具review中的false。

source label `3d07b72b3732cf289410aefaaba49f8d8423f746`是canonical closure SHA1，非Git tree/commit；closureSHA256 `3e3e5d35af81f1f000eaa998f2b7232a8af2388ab78dcbbc36d27df7785ed173`。未来成功registry必须真实identity kind/proof，不冒git_write_tree。工具1000本身不证明deep-ready100k/1M、预算或容量；后续新S实际证明100k，仍无1M/容量外推，100/G0保持未验。


## 本轮原 S 与4k诊断实际终态（2026-10-02）

新原 S 完整：100/500/100000、schema16，合同SHA9e810、canonical closure SHA1 `3d07b72b3732cf289410aefaaba49f8d8423f746`（**非Git tree/commit**）、actualclosureSHA256 `3e3e5d35af81f1f000eaa998f2b7232a8af2388ab78dcbbc36d27df7785ed173`。墙钟1518.898014/1800s，外层processguard1530.726965/1800s，Go/runner/strictCLI0，ready100k/14×200/SQL20/六actualstream/实际分布/权限安全/原plan及清理齐。新registry保真实identity proof及[原冻结合同](evidence/R5-B01-AJ-S-PIPELINE-FROZEN-CONTRACT.json)，不改历史S2/failedM/M2旧字段。见[原S review](evidence/R5-B01-AJ-S-PIPELINE-REVIEW.json)／[27原payload安全档](evidence/R5-B01-AJ-S-PIPELINE-LOGS.tar.gz)。

| 观测 | 实际值／结论 |
|---|---|
| S inbox list app cold/hot p95 | 9.118/4.145ms，仅S≤300ms候选通过 |
| S index-ready search app cold/hot p95 | 5932.551/6092.991ms；原S无searchcutoff，不套M≤1s判S失败/认证M；非性能全绿 |
| 资源 | RSS328728576、owned objects+DB disk1892809751bytes；不是整机内存 |
| 准备 | wall1380.357542s，其中Store阶段1203.257564s、claim合计8.837165s；parser/join终态quiescent/inflight0 |
| deep actual claim | ready99980时actualClaim20=1.085ms；ready100000末空claim=0.563ms；不是1M计划/产品Claim改造 |
| M规划风险 | Store×10=12032.576s>原10800s只线性规划；不是实测M失败下界/改预算/准入证据 |

DIAGNOSTIC4k sourceb45仅20/100/4000，Go0、ready4000、exec55.760752/180s、native init→stop63.462329/300s、disk97820075bytes。quota lock21.843%为1652/7563 sampled rows，target100ms实际maxgap1.033992s；并发嵌套callspans/profileaggregate不等墙钟criticalpath/fsync时长。[原review](evidence/R5-B01-AJ-STORE-DIAGNOSTIC-4K-REVIEW.json)／[39原payload安全档](evidence/R5-B01-AJ-STORE-DIAGNOSTIC-4K-LOGS.tar.gz)不含私有trace/CPU/pprof二进制。本段A交付时lookahead100未实run；后续B4k实际终态见下节。A/B都不是工具准入/S/M成功，不进successful-scale runs。

**当前原M/M2均未有完整baseline，M2Goexit缺观察照保；未新run M/L。** bounded B已有限实际交付，下一RES-M-01只读benchmark方法方案/独审→新freeze/必要race/tool/actualS再裁决RES-M-02唯一operator原M窗口；不得暗改9e810原方法、原人口/预算/C20/14×200/候选阈值或盲再开3h。P7/P10 shipping indexed_search具名性能缺口保留，L原预算/后续容量宣称必要性不取消。全部source分层/安全包装/具名依赖验收见[本轮有限报告](R5-BOUNDED-INTEGRATION-20261002.md)，P0-100/120/G0仍false、10/171不变。


## 后续 lookahead B4k 实测（仅诊断，不换原S/M方法）

新canonical source32307/closure7cfd，实际Go0/ready4000、exec42.801670/180s、nativeinit→stop51.709025/300s、disk99238981bytes；A原JSON复制保、A未重跑。A aggregate/parameter/六actualstream精确相等，但timestamps/received-order/randomUUID/lease/protocolUID未由streams证明全字段等价。Store47.978513→38.957427s(-18.802%)只两次非同时单次诊断观测；A sampledquota1652/7563与matchedsameMailboxedges1844→B0/6301，但B仍11其它锁sample、maxgap0.582s，不叫零wait或wall0%。新B诊断命令未race，不当benchmark准入。

首次A硬pin误用GoJSONL97af而非原AJSONb6e2，纯前置blocked无PG/Go/origin，不计runtimeRED；原receipt保。见[原B review](evidence/R5-B01-AJ-STORE-DIAGNOSTIC-LOOKAHEAD-B-REVIEW.json)、[38原payload安全档](evidence/R5-B01-AJ-STORE-DIAGNOSTIC-LOOKAHEAD-B-LOGS.tar.gz)、[后续报告](R5-BOUNDED-FOLLOWUP-20261002.md)。

原S9e810合同/方法未替换，原成功S registry不变；诊断单列，M/M2失败/Go缺观察保，未新增S/M/L候选成功。pipeline作者当前只读准备新benchmark方法与actualS/M预算方案；没有方法替换/M/L授权，不能把未观察的最新CLI/state称已验证。P0-100/120/G0仍false，10/171未变。


## 显式 opt-in 方法完整原 S（最新actual，不改历史）

root批准显式新方法实现/独审→fresh完整tool门禁→唯一fresh原S。METHOD独占writer为baseline_dispatch/lookahead_contract_validator，方法ID `lookahead100_unpersisted_inputs_distinct_mailbox_windows20_ordinal_fifo_v1`，实际S合同SHA87c41、DATASETS9e810保持；default bounded_store_claim_complete_window_v1不改变。原S2/v1 tuple、failedM/M2/unknown保。

新qualified tool与原S同source407428 canonicalclosureSHA1（非Git）/closure83a7，工具1000完整2800样本/SQL20/7safety/ready/GoCLI0后，再原100/500/100000/schema16/ready100000/14×200实际Go/runner/strictCLI0；S wall1377.669084/1800s、runner独审1393.058s、RSS395558912/disk1892809751bytes。newregistry只追加该S，B诊断不改label；resulthash24c54、rawGohash968b0由executor独立两文件bindingreceipt，不重S/validator/全树hash。

actualschedulerN100k/permutation/FIFO/join0/buffer100/window20；actualPG UIDcatalog列0/received100k400boxes违例0。六streams/aggregate/params直接等原S3d07，但timestamp/receivedorder/randomUUID/lease/protocolUID不全字段等价。source有shipping SQL资格/分页差异，搜索~6s→16ms不纯归因调度；列表14.449/7.839ms只过S300ms候选，relative10%不cert，不拿M1s认证M。

sameparamactualClaim20 ready99980为0.502ms/terminal100kempty0.303ms；本次Store×10=11731.872s/pipeline×10=13509.740s仍只M10800s规划风险非failure下界/预算扩张。root后续新M仅条件准入（最小Go/runner/METHOD+全新独审后sole原1M10800s80GiB），目前未run；L拒。新M current METHOD改allowedscale会换digest/source，**历史S只读档内87c41原bytes+closure/argv/env，不要求current同名文件永久87c41，也不回填S**。

原review/档与freeze：[newtool](evidence/R5-B01-AJ-LOOKAHEAD-TOOL-REVIEW.json)、[newS](evidence/R5-B01-AJ-LOOKAHEAD-S-REVIEW.json)、[27原payload](evidence/R5-B01-AJ-LOOKAHEAD-S-LOGS.tar.gz)、[原S METHOD](evidence/R5-B01-AJ-LOOKAHEAD-S-FROZEN-METHOD.json)、[两文件binding](evidence/R5-B01-AJ-LOOKAHEAD-S-RESULT-BINDING.json)。独审只读接受有限S；P0-100/120/G0仍未验，父10/171。具体其它层级/当前prepared不PASS及原sourcecold边界见[最新有限集成](R5-OPTIN-S-COLD-INTEGRATION-20261002.md)。


## 新短窗不改变scale registry（后续状态）

consumer29分源（首29CLI1/16PASS13fixtureFAIL→exact13CLI0）与backendtransport4top23leaf分源（初PGHTTPGo1→test-only仅HTTPGo0），以及coldatomic8newmock资格，各有原raw/修复scope交付；不是原S/M性能数据，不加successfulscale。Mprepared受new-agent threadlimit全新独审阻，未实际run；currentMETHOD变化不污染S4074/87c41原archive tuple，原M2missingGoexit不变。准确source/selector与父G0/G7/browser未验边界见[事件/coldatomic新报告](R5-EVENT-COLDATOMIC-INTEGRATION-20261002.md)，10/171继续保。


## 官方CI依赖与当前M窗口（不可污染历史S）

14成功S row直接safe archive/review/frozencontract/sidecar refs已原字节加入newshort source5d1b，非原2aa relabel。仅21之前失败RegistryTests现在不缺files而是KeyError review['archive']，CLI1/21setUp errors保；owner正在窄productionadapter/fixture支持原S3d safe-delivery与S4074 unchanged作者review/sidecar/canonicalreceipt，candidate尚新独审/实际32targets未验，不改authoroldreview造archive字段，不宣registrysuite绿。

原fullGo2aa9Go1/7fails保；newshort5d1b五簇affectedGo含132route各0不是wholeGo绿，protocolmanifest644接头真通但19PG旧policy/fixtureFAIL保。原S2/S3d07/S4074三精确tuple及M/M2failed/unknown不动。root新M fresh独审条件已可推进并授stable8529/d286唯一10800s80GiB/1M，截止cutoffraw尚未start，实际start/result后另接；laterProfileCreate/mainWIP不入immutableM，L拒。事务currentAST802/catalog16/adapter与其他新目标M后独立窗口，不M前无限scope。见[官方CI维护](R5-CI-132-DEPENDENCY-MAINTENANCE-20261002.md)；10/171、P0-100/120/G0仍未关。


## 本次原M实际预算失败（不是只S线性预测）

原M source852900/closure49d04/METHODd286/schema16/DATA9e810唯一实际10800s/80GiB/C20/1000+5000+1M attempt达到absolute10800.004052，observed-parent-wait Go process-15/ownedSIGTERM、runner1/rejected、noresult/noGoJSON terminal，stored753380/ready753360/processing20/0workloads未1M。先parser31s余量guard失败再parent绝对限kill，worker/parserjoined0/quiescenttrue与TERMINALbeforeclaim/producerfalse/finalemptyfalse不是完成。PG/assertion/knownGo/runner/socket真stop/absent，但privatepartialDB/object树保，不假DB0/allgone；不上传部分正文对象。

独立review接受actualfailedattempt-only，非p95候选红/非已证infra因果；M_attemptedtrue、M_executedfalse仍无completebaseline，resultSHA null不fakeEmptyResult，oldM2missingGoexit与old3S各tuple保。原summary七actualClaim null由原GoOutput只读纠正，ready20/1k/10k/100k/250k/500k/750k同参actualClaim20分别0.000600/0.000544/0.000830/0.000625/0.003775/0.008557/0.009925s，不EXPLAIN/precedingclaim替代或1Mcapacity。原review/tar不改，currentgenericSadapter不泛纳newMfailedreview，typedversion严格准入仍pending，retry/扩预算/降人口/L未授。

[原Mreview](evidence/R5-B01-AJ-M-ABSOLUTE-TIMEOUT-REVIEW.json)／[原34payload](evidence/R5-B01-AJ-M-ABSOLUTE-TIMEOUT-LOGS.tar.gz)／[只读纠正](evidence/R5-B01-AJ-M-ABSOLUTE-TIMEOUT-READONLY-CORRECTIONS.json)／[失败报告](R5-M-ABSOLUTE-TIMEOUT-20261002.md)为当前事实。S×10本身仍仅规划风险，但此次实际budgetfail有独立运行证，不需外推。M后schema17/产品targets另source不改变此M16结果；10/171/P0-100/120/G0未闭，无M/L有效容量结论。


## M后产品/Node子层不改变performance registry

new7ffe/schema17 product4 Restore5/30、GC7/9、Offboard11/42、P6fakeStore7/15实际raceGo0，newNodeP5current41PASS与old14RED、receipt初54CLI1的TDZonly2newsource0各有独立原raw；不performance run、不M基线、不原M失败覆盖，不新增successfulscale。见[product4/Node报告](R5-PRODUCT4-NODE-BOUNDED-INTEGRATION-20261002.md)。独审叶级核真实原scope不足，非僵化守10；当前10/171/P0-100/G0未闭，普通OAS/currentASTcatalog/protocol/secure/fullCI/shippingbrowser等仍具名缺证。
## 追加registry clock / typed失败准入层（2026-10-02）

三成功S和旧失败字典不变。原registry34实际13executed=12PASS/1ERROR、21 classsetup未执行与realCLI1保；S3d执行墙钟1518.898014及S4074 1377.669084不等原Go test1528.68/1386.62，不能为了validator改旧源摘要。最后runner cf156af9/tests79b3d443只新增已知版本强绑定：S package elapsed finite/nonbool/预算、S2 review两个clock分别原raw；typedM observed -15/no Go terminal/rejected与incomplete stats/joins/reserve逐字段绑定、完成flag拒。原全新独审SOURCE PASS与静态AST/diff0不代表sole25+realCLI实际通过，待原命令新receipt。M original archive仍0ab842，27fe12只是registry工具失败档，无M repack链。当前18catalog或新产品/UI不回贴M852900/schema16及旧S源。详[追加报告](R5-GC18-RECEIPT-OAS-REGISTRY-INTEGRATION-20261002.md)；M有效baseline、P0-100/120/G0仍false。

首次新25窗口actual4exec=2PASS/2ERROR、Registry21 classsetup未exec与CLI1保e64871；capture漏真实declared corrections dependency，原source852900/6252B文件实际存在，onlyexplicit单filehash receipt d98be876…已交sole。只修输入capturemanifest/freshsource后23affected+CLI，不复2PASS/旧12PASS；非产品RED也不通过，旧S/M tuple不变。

**随后有限adapter实际完成：** depfix23=22P+1静态migrations缺input ERROR/realCLI0，only1补18actual SQL staticcopy后1PASS/.276173s≤180，原新独审对25uniquePASS无missing/extra/duplicate签composed接受，不singlefresh25exit0。cf156/79b共同源字节不变；depfix23 aggregate clock UNKNOWN/PathTypeError与原fails保。真实CLI只接受三exactS及852900/d286/schema16 known FAILED M，baseline/task/productfalse；可关闭已知clock/typedfailure adapter子scope，M有效baseline/P0/G0/fullCI不关。[原证据与有限裁决](R5-REGISTRY-COMPOSED-ALIAS-UI-INTEGRATION-20261002.md)。

**最新非benchmark运行层：** dfb054d当前18整个backendGo1/7425run7416P6F1BrowserSkip/2无terminal、PG包累计180.682s到限206未start；后cutStats5/13raceP、TSv2 11file82P但tsc2、ABC-C2 17top73P但正式captureCLI1、旧81×newdocreadonly0error各source保。PGphase旧dfbderived24P3FAIL/472phases是诊断非因果/产品PASS，未原M retry/S新run。[分层证据](R5-CURRENT18-BACKEND-STATS-WIRE-ABC-DIAG-INTEGRATION-20261002.md)，不performance/Mbaseline/P0/G0闭合。

后续parallel首4与新增15分别bounded实际PASS/32.459s与35.470s，不whole19/wholePG180认证；Legacyca194新source currentwebTSC0及known3leaves0，不覆盖旧TS2/source。MCAUSE7file SOURCE及cached4/fresh1 TEST P，但4000执行协议未freeze/未run，原M16base注入外层1GiB/300/180必须新审，不用current18代M16，不解释753k因果或补M有效baseline。新whole27633d96获授873decl/872required/51pkg、原180/package/1200总待实际，shippingobserved daemon未备无执行，不预填PASS。详[追加报告](R5-PG-PARALLEL-LEGACY-MCAUSE-SHIPPING-INTEGRATION-20261002.md)，三S/原Mfailed rows字节不动。

随后27633 actualwhole Go1/7373P1FAIL1BrowserSkip20unfinished/201top未run、PG180.847timeout，actualcontrollerGOMAX1偏批准2。真实失败+执行合同deviation保，不双lane/预算效果证明，不performance/Mrun；新失败档e3f6b9与原dfb/M/S分开，whole/P0/G0仍未闭。

## 2026-10-03 MCAUSE490微诊断与current METHOD阻断

原M852900/d286/schema16/10800.004052 FAILED、753380stored/753360ready/20processing/0samples完整保。首MCAUSE487仅0test build-INFRA缺三go:embed，不FS/SQL负例；新490/7091/a9c6/f5b单次原schema16 1000/5000/4000/ready4000、25spans/dirclose12000/FIFO4000/buffer100/join0/六streams/controllerGo0独审bounded实际P，body59.749/preNative→parent71.887522原300内，sampled344551424B非quota，parserparent-adjustedmin114.475917不一般reserve。ClientRead/单Lockedge与schedFsync runnable不能归因锁等待或硬盘IO，不推753k因果/有效M/新S/M。原114profile私包4ec6保，安全109payload另包装剔5binarytrace，不改原review。

当前sourcePolicyV2唯一972captureP，但synthetic20P2legacy16setupERROR/CLI1；V3 only2body SOURCE审后待actual，不singlefresh22。MIME完整forkV5四target164P有限组件，不currentMETHOD/GoList全source/baseline准入。current18vs fixed16及LOOKAHEAD旧files-only source-binding仍BLOCK，需新命名合同/显式opt-in/sourcePolicyV2 namespace独审，不覆盖旧METHOD/DATASET/三S/失败M或自动允许S/M/L。详[准确层级](R5-MCAUSE-HISTORICAL-MIME-CURRENT-SOURCE-INTEGRATION-20261003.md)，runs registry没有新增成功scale，也没有修改旧flags。

随后SOURCE20+V3first1+V4last1独审composed22、actual972capture仍仅V2；MIME164及narrative纠正/新34append接受，不fresh22/当前METHOD。C18-01/03新七SOURCE V2只准申请pure21，首DarwinAS资源setupINFRA0test/audit未armed、memory未建；RSS V4仍SOURCE cleanup/封证failclosed BLOCK。actual18 raw02a585七集合是partial，待C18-07 fullfields与reference；新runner02/Go05/临界clock06/compileMVS09未实际资格。最新whole0cf GOMAX2实Go1/PG180.693/128top未start与receipt5、Webhookcomposed139分层，不M/S benchmark，原M10800FAILED与三S字典保。[最新硬依赖/证据](R5-WHOLE-Go0cf-WEBHOOK-C18-SOURCE-INTEGRATION-20261003.md)。

随后RSS V5 SOURCE独审准入闭V4清理block，但actualpure21仍未执行：qualification90 sticky与失败-only最多5s monotonic containment分离，超原deadline不恢复P，不成功扩预算；mock2+后1分层非fresh3。仅可root另授sole21一次，当前METHOD/runner/Go/fullcatalog/MVS与SML资格仍BLOCK，旧AS0test/raw不改。

## 2026-10-03 current19与C18已测语法层

C18pure21随后真实限定RSS-sampled grammar独审P：21exact+20subtests/CPU60回读/audit0denial/RSS10peak38MB/maxgap.1109，to-terminal1.164346/finaladmission约1.164481原90gate，不外exit精确耗时或METHOD资格。catalog15purefrozen18c7bf170raceP无Owned/SQL/fullreference，18完整editable树1309/20MB SOURCEcheckpoint不当前19。C18-09/profile/build/owned/fullcatalog/currentversion Method仍BLOCK；当前19不能继承18，更不旧16S/M。

Current19 physicalusage原9+2SOURCE审P但A实际21P5F leaf/Go1；D1 12raceP后D2 2F、D3BSC未run，815 cleanctx只SOURCE待审/重验，R4+19SQLruntime独立captured不D3P。旧18 KEYTOUCH5P仅因果fixture非holder89451/产品fix；旧18PGexact8 phaseSOURCE六BLOCK无runtime。最近formalwhole frozen18a331仍Go1/PG180.689104未start，所有新grammar/components不补whole或有效M。原million10800FAILED、三S/4失败tuple与原METHOD/DATASET保持。[更新完整层级与下一依赖](R5-CURRENT19-USAGE-DIAGNOSTIC-C18-STATUS-20261003.md)，Docker无ready确认/四CI未闭。

随后D2clean2raceP、D3BS10raceP各独审actual接受，原A/旧D2F不改；C0/V8仍pretestINFRA_UNKNOWN/V9只SOURCE待审。编码34nonraceP与workqueue28raceP仅组件，packageElapsed不是body/compile、自报clock非外钟，legacyStop/caller/SMTPshutdown链未接。PGphaseR3 B2/B5、watchdogdrained/terminalflush仍formalexact8 BLOCK/RUNTIME_DENIED，33/37/25/19只是pure开发层，HARD1200不删。无新benchmark/Method当前19资格/M有效baseline，三S/原M失败tuple保。[最新限定证据](R5-USAGE19-C-OBSERVER-SHUTDOWN-ENCODING-STATUS-20261003.md)。
