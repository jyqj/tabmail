# 原 M 绝对10800秒失败attempt与M后队列

**唯一新原M真实运行已timeout，未形成1M基线。** source852900/METHODd286/schema16在原10800s绝对期限被owned SIGTERM终止，观察进程return-15、runner1/strict rejected、无result或性能样本。不是GoJSON FAIL/0、不unknown退出，也不把753k准备计数冒1M。10/171、P0-100/120/G0均保持未完成；没有自动retry、改预算、降规模或L。

## Evidence：不可变source与真实终态

| ID | 观察 | 原件与范围 |
|---|---|---|
| E-M-SOURCE | canonical closure SHA1 `8529007132f1d61a2571e43aaabfe15b8452baa1`（非Git），closureSHA256 `49d04f3695101c07dcb092fd6294151a873bb9993f6396ed668877720ac97581`；freeze前后相等 | [作者原review](evidence/R5-B01-AJ-M-ABSOLUTE-TIMEOUT-REVIEW.json)、[34原payload档](evidence/R5-B01-AJ-M-ABSOLUTE-TIMEOUT-LOGS.tar.gz)。原METHOD SHA256 `d286ae9f5f4b68033ba9739a1276de22f1e0f7ef88e15642d52633da943b49a7`，DATASETS9e810原冻不变；[本次M冻结METHOD](evidence/R5-B01-AJ-M-ABSOLUTE-TIMEOUT-FROZEN-METHOD.json)保持档内原字节 |
| E-M-DEADLINE | 原requested1000/5000/1000000、C20/pool24/window20、disk80GiB、absolute10800s；deadline UTC `2026-10-02T06:47:53.594719+00:00`，观察wall=`10800.004051923752s` | `M/execution-budget.json`／`M/timeout.json`／`M-controller-receipt.json`，明确timed_out=true；不把S线性预测当这次事实 |
| E-M-EXIT | 父wait实际观察原Go进程return **-15**，owned SIGTERM已发；GoJSON存在run/output但无test终态；runner1 | 档内`M/go.exit=-15`、`M/go.jsonl`无testPASS/FAIL、`runner/M-runner.exit=1`。不伪造Go FAIL1/Go PASS0或使用M2 unknown代替已观察-15 |
| E-M-PARTIAL | Go join后native stop前实际SQL：stored753380、documents/sourcebound durable ready753360、processing20、mailboxes5000、users1002（1000employee+2admin） | `partial-actualSQL-counts.txt`及原review；未达到requested1M，0已完成workload样本、14×200未完成、完整六stream fingerprint未计算，`M/result.json`缺失 |
| E-M-CLEANUP | PG56228/assertion56215/Go56516/runner56512全部known owned PID/group/socket实际absent，native stop0 | `cleanup-receipt.json`与terminal DB report；残`tm_benchmark_7f0de3284219` partial synthetic DB及GoTempDir对象树留在已停private root，**不是DB0/objects gone**。没有触及外部资源 |

过程资源仅保timed du/resource observations；没有完成result metric matrix，不填写假finalRSS/disk admission。PG周期checkpoint总时长不等单Commit latency，不拿旁观日志简单归因Store或fsync下界。

## Finding：实际准备预算失败，不是完整性能结果

**F-M（validated/high，限定本attempt）**：原10800秒绝对预算已真实到达且采集不完整。这次失败已不是“Store×10规划风险”推测；但也不证明所有实现/其它方法都不可能完成M、没有M search/list候选达标或失败值，更不定义普适Store下界。

`PREPARATION_TERMINAL`这个名字不是完成证书。实际捕获为：

- `phase=before_claim`、`producer_done=false`、`final_empty_claim=false`，last batch20；
- workers joined_inflight0、parser joined_inflight0/quiescenttrue，parser实际753360次join、最小起跑余量31.166936s；
- stats里的`sql_ready_count=0`是未执行最终核验的默认值，不覆盖独立actualSQL753360；
- Stage pipeline10752.669983s、Store9509.146712s、parse1054.858389s、claim累计171.778819s为本次captured preparation观察，不补成result/sample matrix。

这只说明started worker/parser已join，不说明producer完成、最后空claim、全1M/source fingerprint/workload采样完成。GoTempDir的大量文件清理随后被original process watchdog截断，部分private合成状态因此留存。

原executor review七个`actual_claim_joined_seconds=null`是摘要完整性缺口，不是原raw没有测量。新独立只读review发现并直接解析原GoOutput相邻`PREPARATION_CHECKPOINT`+`PREPARATION_ACTUAL_CLAIM_JOINED`：同参数、requested20/batch20、ready20/1000/10000/100000/250000/500000/750000实际分别0.000600/0.000544/0.000830/0.000625/0.003775/0.008557/0.009925s。它们不是EXPLAIN/preceding claim替代，也不ready1M。原review不改，纠正旁证见[只读补充](evidence/R5-B01-AJ-M-ABSOLUTE-TIMEOUT-READONLY-CORRECTIONS.json)。

原raw先在`2026-10-02T14:47:28.938305+08:00`报parser admission要求remaining>31s/context deadline exceeded，随后parent绝对deadline14:47:53.594719硬终止。因此不能说“直到deadline运行无失败”；也不因此伪造缺失GoJSON FAIL终态。

P-M（E-M-SOURCE→DEADLINE→EXIT→PARTIAL→CLEANUP）：批准source/method freeze→唯一原人口/预算M→bounded preparation/parser31s余量拒绝→absolute guard SIGTERM/parentwait→runner rejected/noresult→partial SQL与source pre/post证据→停止所有known owned live resources、保private failed-state。每步来自原件，不靠复制旧S/M或未来candidate证据。

## Registry与历史原件归属

[runs registry](evidence/R5-BENCHMARK-RUNS.json)仅追加独立`M-LOOKAHEAD-ABSOLUTE-TIMEOUT-20261002` failed_attempt：original population/source/method、known -15/无GoJSON终态、runner1/noresult/0samples/partial counts/cleanup边界均原样层级。

- `M_attempted=true`，`M_executed=false`的明确含义是**没有完整有效M baseline**，不是没实际启动。
- 三成功S及既有failed S/M/M2字典原封保；旧M2 go_exit=null/missing observation不被这次-15覆盖。S4074 archived87c METHOD不回填currentd286。
- 原M result_sha256=null是实际缺失，不伪造空result hash/fingerprint；rawGo hash沿作者`9a233eb511a3c15ab4b5cf4d76aa4970165936ee69675556af1fadd475c92ef5`，archive原SHA `0ab84228f6dcdc4049045303682385fbaf340d7b115fa57da35962d84fbcee76`沿delivery receipt，未重复全文件hash。
- 新独立只读review已 **ACCEPTED_AS_ACTUAL_FAILED_ATTEMPT_ONLY**；current generic adapter仍缺该Mreview typed/knownscope专用准入，不能把此分类当strict-admission PASS，**不让现S adapter泛吞未来M review**。记录真实failed_attempt不等当前完整registry suite已绿；此前21shape错误和候选34targets边界保。

## M后rolling queue：SOURCE/prepared不能写PASS

[具名prepared来源清单](evidence/R5-POST-M-PREPARED-SOURCE-QUEUE-20261002.json)保存parent精确handoff，短pin仅作作者reported prefix，不伪造完整SHA或新runtime：

| Family | 已交SOURCE/packet定位 | 仍需actual/集成 |
|---|---|---|
| ProfileCreate | 5prod71d/11c+fake2closure72b，nilpure7top source审已补 | freshanchor/role/key/nilport/globalempty主audit与tenantoutbox真实PG/H，不归旧AST802/M8529 |
| Restore | actual2pin `/private/tmp/tm-restore-author-pin-i6fur37n` ddce/e5b，独审已绑定 | compile仅transcript/no-file；不是“无pin”，也不是PG PASS |
| GC | a186/42bf，7top9leaf SOURCE | 公平推进/原件保护/重启等必要新target，无test数量结案 |
| Lifecycle | 8e85/ac53 `/tmp/r5-offboarding-compound-profile-prepared.hzk22pb8`，11top42leaf | 旧f11/f3 HOLD不回贴新源，compound profile/预览/commit竞争待actual |
| P5DraftWriter | d6b0/4ce7两文件，41prepared | 保存/提交/未知/late response真实组件/HTTP边界，不oldsource拼全 |
| P6Checkpoint | 21c/fb94，7prepared | accepted/checkpoint/lease/commit故障新receipt，非PUBLICSMTP或完整G6 |
| Registry | 91bb/4f32，34+realCLI static签 | actualsuite及本次newM failed-version独立审批未完成，旧review不加伪字段 |
| A普通回执 | 18closure6af4+A4/A5独立consumer包，新A22+5 fresh SOURCE accepted | 未compile/PG/wholecontent matrix，不普通DTO/BCC OpenAPI已同步 |
| B已发送收件人 | B17actual5pin `/tmp/r5-sent-recipient-B-prepared-20261002/`，selector5roots | 旧schema16/BC03真正upgrade fixture适配与实际BCC来源/完整性验证 |
| UI | 12source z30om+pagebcfe/3sdpy独立delta source审 | 不oldcold/M/currentbrowser整体green |
| ordinaryDTO/BCC OpenAPI | owner未唤醒的threadlimit分支scope gap | 不132注册集合/SSE专用schema假补ordinary契约 |

Soleexecutor已获准这些prepared新审targets **rolling串行**；只在收到各family新command/raw/source/exit/cleanup后升级其层级，不重复旧通过测试/每文件hash。原fullGo2aa9Go1、shortfix局部pass与其他CI fail继续分源保。

## 当前新增缺口与依赖

| 工单 | 依赖与验收 |
|---|---|
| RES-M-PREP-BUDGET-01 | 本次实际failure后单独批准的短诊断/方法设计；保原Store/Claim/parser/durability/ACL/source/fingerprint/人口/10800预算，改变方法另freeze+独审+tool/S必要证据；不得自动再开3h、延长预算/降人口 |
| RES-M-TERMINAL-CLEANUP-01 | workerjoin与GoTempDir文件清理分别有界/观察；failed private tree保留不是active泄漏也非已删除，不靠无证rm全树讨“全清理” |
| RES-REGISTRY-M-FAILVARIANT-01 | 独立原M8529/d286 failed-format identity/raw/negative exit/absence-result/partialstats/cleanup强绑定，unknownfuture拒；不现Sreview泛化、无伪archive或result字段 |
| RES-POST-M-FAMILY-01 | 上表每family独占Source→新独审→sole必要target→独立actual receipt，不aggregated unknown0，不把prepared数量当父任务完成 |
| RES-OPENAPI-ORDINARY-BCC-01 | ordinaryA/B实际DTO+权限与BCC未知来源政策→专门OAS/TS/实际response contract；branch未启动是实scope缺，不132count假同步 |
| 原P0/G0/171与全CI | 无有效1M baseline，原full/协议/registry/事务/catalog16/全角色/跨平台/冷build/实际browser等仍按原具名计划验，不由753k或局部Go0关闭 |

## 安全交付与只读复核

[新M交付清单](evidence/R5-B01-AJ-M-ABSOLUTE-TIMEOUT-DELIVERY.json)包含34原UTF8 payload，原tar直接copy，无AppleDouble/私有DSN/JWT/rawmail/object/partialDB tree或profiling binary。原private目录保未删，不上传其中正文/对象；没有把残DB改为0或说“所有资源已移除”。本次无Go/PG/validator重测、无commit/push/PR/部署。

```sh
# cwd=docs/company-mail，仅离线读取
python3 -m json.tool evidence/R5-BENCHMARK-RUNS.json >/dev/null
tar -xOf evidence/R5-B01-AJ-M-ABSOLUTE-TIMEOUT-LOGS.tar.gz M/go.exit
tar -xOf evidence/R5-B01-AJ-M-ABSOLUTE-TIMEOUT-LOGS.tar.gz runner/M-runner.exit
```

预期-15/1；档内没有M/result.json或GoJSON test terminal。修改后刷新Code-Index文件索引，不deepbuild；**10/171、P0-100/120/G0未闭，无M/L有效容量结论**。
