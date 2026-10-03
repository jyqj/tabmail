# R5 有界 B 诊断与 global-profile fanout 后续交付

**B4k 诊断和 fanout 生产者窄 PG 范围已实际通过；不等原基准方法准入、M/S 成功或 P1-130/G7。** 父完成率仍10/171，P0-100/120/G0未验。本文延续[初次有限集成](R5-BOUNDED-INTEGRATION-20261002.md)，只检查作者必要原终态与证据范围，不重跑A/PG/Go/Web/benchmark，不逐文件算hash，不改DATASETS原合同，不提交/push/PR/部署。

> 后续已新增显式opt-in新S4074、profilevisibility与nativecold实际分层；最新事实及M条件准入见[最新有限集成](R5-OPTIN-S-COLD-INTEGRATION-20261002.md)。本文件旧source/原阶段时点保留，不能作为最新WIP状态。

## Evidence：本次增加的实际来源

| ID | 来源身份／closure | 实际终态与交付 |
|---|---|---|
| E-B | canonical closure SHA1 `32307f6579ab2e5d7e0241752c388e4c30cf6c5d`，非Git tree/commit；SHA256 `7cfd79e9e047f67cdb05d9b85cdc8f600137807f5cda2bfedd5642ee7d60577e` | [作者原B review](evidence/R5-B01-AJ-STORE-DIAGNOSTIC-LOOKAHEAD-B-REVIEW.json)、[38原payload安全档](evidence/R5-B01-AJ-STORE-DIAGNOSTIC-LOOKAHEAD-B-LOGS.tar.gz)；`^TestR5StoreDiagnosticLookaheadB$` Go0、package43.670s；诊断命令未启用race，不包装成race基准准入 |
| E-A-REF | 复用原A `b45b043d822ddabb536f0a2321a1ce0913ae7137` | B档中 `A-reference/diagnostic-A.json`／`receipt.json`；A原JSON字节原样复制，**A没有重跑** |
| E-B-PREFLIGHT | 首次错误A pin的纯前置边界 | B档 `previous-blocked-preflight-receipt.json`；无native DB、Go execution或outer origin，不当执行RED／runtime失败 |
| E-FANOUT | canonical closure SHA1 `abea755a5d9707d94a7ddc7c37fe0c492dd5e30b`；SHA256 `fc51b9ba45bbe8a9d8bdc45e9fc0e8478ea2240bc1631166c9d22ff349a3c930` | [作者原fanout review](evidence/R5-P1-GLOBAL-PROFILE-FANOUT-REVIEW.json)、[14原payload安全档](evidence/R5-P1-GLOBAL-PROFILE-FANOUT-LOGS.tar.gz)；`^TestR5GlobalProfileFanout`，6top/27leaf/32pass events、raceGo0、PG包8.504s |

Fanout快照从已释放只读B immutable source 加**两个独占文件delta**生成：`permission_profile_cas.go`、新增 `r5_global_profile_fanout_test.go`。两次运行的各source receipt与closure分开，不把B的诊断source贴成fanout生产运行源，也不把最新dirty checkout/CLI状态称已验证。现runner/tool方法准备若晚于这些快照，只属于未验WIP。

## Finding：B4k观测仅支持限定调度方向

- 原执行预算仍180s，native原init-origin到stop总预算仍300s；B execution=`42.801670s`，Go-owned cleanup=`51.478103s`，native init→stop=`51.709025s`，实际ready4000、disk=`99238981bytes`、RSS=`267780096bytes`。时钟起点不同，不互换。
- 方法为`lookahead100_unpersisted_inputs_distinct_mailbox_windows20_ordinal_fifo_v1`；原 Store/durability、合法Claim20、Parser4/30s与31s wallreserve保留，未持久缓冲≤100、window/active≤20，4000个ordinal完整generated/emitted/completed，joined FIFO及parser inflight0。
- A/B aggregate／parameter／六actual canonical SQL streams及各row count精确相等，无新增normalize；**不是每个持久字段相等**。received_at／received-order、lifecycle/indexed_at/next_attempt_at、lease tokens、随机UUID/物理状态等不由六streams证明；observed UID column0不等protocol UID等价，A随机commit顺序也未保存或声称一致。
- Store阶段A=`47.978513s`→B=`38.957427s`（-18.802%）；pipeline54.954370→41.876602s；这是两次非同时、非重复/confounder-controlled运行的本地观测，不当稳定因果收益或M预算保证。
- A quota-lock1652/7563 rows、same-poll matched same-mailbox edges1844（另40 edges未归因）；B quota-lock0/6301、matched edges0，仍有**11其他锁sample rows**。A maxgap1.033992s/B0.581587s；“未采到quota等待”不等零等待、不等墙钟比例0%。edge计数不是独立锁次数或持续时长。
- 文件同步与SQL成本仍存在；B部分commit/insert overlapping call sums反而增加。CPU/sched/File.Sync聚合仍不等fsync I/O、commit/checkpoint墙钟criticalpath。
- 首次A hardpin把`97af8cc...`（原A **Go JSONL** hash）误当A diagnostic JSON；准确原A JSON hash为`b6e2d421424aac265106d2cd111239686c987e25ac639eba9f06c457c25b229e`。作者修pin后新source重冻结，既有A字节／source不改；前置blocked receipt原保，不虚计执行RED。

**F-B：validated/high，限4k diagnostic。** 不入successful-scale `runs`，不是TOOL admission、原S/M候选通过或方法替换。原S合同`9e81020680dbd6e990cbb35ab1aac16413885e65306d949a5889c4bafda00b36`仍冻结；pipeline作者当前只读准备B→新benchmark方法／actualS再采／M预算是否值得的方案，尚无方法替换或新M/L授权、没有相应runtime结果。

## Finding：fanout只证明生产者范围

E-FANOUT实际覆盖：

1. Global profile PATCH/delete 保**一个selected company主audit**；event受众按当前实际引用成员所属tenants排序、去重，不假设selected caller就是recipient。三受影响tenant各一个原`company.admin.changed`，仅三个允许metadata字段，不含跨tenant PII。
2. 空global audience保主audit、无tenant event；local仍原一event。授权CAS raw noop不写、不bump、不audit、不event，既有版本／锁边界不放宽。
3. Required audit及排序event第0/1/2个插入fault，原事务完整回滚profile rows/revision/audit/outbox；当前成员busy fence、已commit reassignment取消受众、阻新assignment phantom均实证。
4. 真当前super与原tenant authority拒绝矩阵保留；不是新增平台通知政策，也不是全HTTP/UI消费链。

**F-FANOUT：validated/high，限6top生产者PG/race。** 原生产者“global受众未证明”缺口有新的实际证据，但全局可见profile list、空受众平台通知语义、真实SSE消费者仍各具名未闭；不勾P1-130/G7、P0、M或父171。旧事件／stage/P1/A/B/S/received/pure/compile gate没有重跑，不把32pass数当父scope全完成。

## Path：后续仍必须做什么

| 工单 | 原父项／依赖 | 当前状态与精确验收 |
|---|---|---|
| RES-M-01｜B到benchmark方法准入 | P0-100；原040/090已验，B4k仅部分证据 | **bounded B观测已交，方法准入pending**；作者只读方案需明确新增schedule/clock/order/protocolUID/fingerprint边界、原测量安全/14×200/C20/预算不减，独立review后新method freeze与必要race/工具准入、actualS再采；不可自动改9e810或借B登记S/M成功 |
| RES-M-02｜原M完整baseline | P0-100；依获准方法／资源窗和唯一operator | 原1000/5000/1M、10800s/80GiB、ready1M/14×200/raw/Go/runner/CLI/clock/resource/cleanup完整；原M2unknownGoexit保。不用B -18.8%或S×10保成功/失败下界 |
| RES-P1-EVENT-02｜global可见profile列表 | P1-010/030/090、P7-010；保原依赖 | 跨tenant引用的global profile实际read/list范围及选公司切换、当前super/tenant-admin/reader/key拒绝/允许明确；PG/HTTP分页total/字段不泄露，不能把event受众正确当列表资格通过 |
| RES-P1-EVENT-03｜空受众平台通知政策 | P1-110/130、P7-070；保原依赖 | 空global audience仍无tenant event是现实现；平台管理者是否需要global invalidation须显式政策决策和窄payload/UI刷新，不随手造全tenant广播、也不把noevent当全平台通知已验 |
| RES-P1-EVENT-04｜真实SSE消费链 | P7-080/090/100、P9-130；保原依赖 | Go+PG/实际SSE→UI重读，与多tenant role／profile切换、撤权、disconnect/cursor/重复事件结合证明最终一致及不扩权；生产者单PG不覆盖多实例dispatcher/consumer |
| RES-RCV-01/02/03 | P2-020/030/140、P7-020；保原依赖 | legacy unpin、完全同字段incarnation无schemafence仍待；53leaf是旧dea20+206d必要两child **composed provenance**，非新source一次fullGo0，原fullGo1与canonical15RED保。集成受影响source后唯一operator必要全prefix验证，不重跑不受影响旧绿 |
| RES-UI-01 / RES-GATES-01 / RES-G0-01 | 原P1/P2/P9/P11-130、P0-120完整依赖 | 静态／pure不shipping browser，freshfullGo/Nextcold/最终审及原M/G0仍缺；原161未勾父项逐项scope全保，不通过本表“吸收”或删除 |

P-B：原A JSON→前置正确hardpin→新B冻结→实际4k Store/Claim/Parse→sixstream等价观测→native清理→**方法方案与独审**；每步证据E-A-REF/E-B-PREFLIGHT/E-B，F-B不允许越过method admission。

P-F：fresh super+selected tenant→profile/CAS/current referenced-member fences→sorted dedup event受众→required primary audit/outbox同Tx→controlled fault回滚→**global可见list/平台通知/SSE真实消费**；生产者步骤E-FANOUT，后续消费者没有新runtime证据，不用F-FANOUT代替。

## Inspection静态证据：只有作者transcript

原UI与contract作者已明确**没有持久raw/report/log**；只补精确transcript locator，不复制聊天伪成raw，不重跑已绿命令，不造source hash。

| 作者 | 精确chunk与含义 | 层级边界 |
|---|---|---|
| inspection_ui | `8f74a3` RED；`99bc57` tracer green；`4fc235` 121 consumer/types+scoped tsc/lint0；`685f6f` scoped tsc；`a3bacf` target diff | 4 inspection专用文件／pure consumer/type/scoped检查，不是完整Nextcold或PG→UI/browser |
| inspection_contract | `c2b8b9` RED10（3fail/6errors/1pass）；`c4dc15` PASS10；`2a934e` ordinary DTO静态对HEAD不变；`4a6709` diffcheck0；`723cae` matrix单条断言 | 127operations中index109，changed_rows1，仅authority/version/audit/content/evidence五字段；普通DTO不扩。静态字符串与schema断言非HTTP实际response证据 |

精确locator亦录入[交付清单](evidence/R5-BOUNDED-FOLLOWUP-DELIVERY-20261002.json)。独立received四gap审查为delivered_static_review；其实际runtime分源已在初次64payload档中，不伪造独审机器文件。

## 安全交付与离线复核

B与fanout新增安全档分别38/14个原payload，仅剔AppleDouble及B trace/CPU/衍生`.pprof`二进制；保A原JSON复制、B hashed samples/aggregate profile文本、原exit/redactedenv/closure/receipt。没有private DSN/JWT/rawmail/objects/DB或完整源码tree上仓。原tarhash沿作者receipt，新包装hash另列；没有逐文件重hash，没有修改历史包。各本轮ownedPG/socket/caffeinate的实际stop/absent在原receipt，未触及旧资源。

```sh
# cwd=docs/company-mail；只读离线，不启动PG或重跑tests
python3 -m json.tool evidence/R5-BOUNDED-FOLLOWUP-DELIVERY-20261002.json >/dev/null
tar -xOf evidence/R5-B01-AJ-STORE-DIAGNOSTIC-LOOKAHEAD-B-LOGS.tar.gz go-run/go.exit
tar -xOf evidence/R5-P1-GLOBAL-PROFILE-FANOUT-LOGS.tar.gz go-run/go.exit
```

预期0/0。完整runtime复现仍需owner私有nativePG／只读source／ownedDSN，不声称safe tar是完整可运行生产tree。

下一可独占三项：**RES-M-01只读方法准入方案、RES-RCV-01/02身份fence、RES-P1-EVENT-02/03/04列表/平台通知/SSE消费**；公共schema/DTO/中央状态按owner串行协调。仍10/171，P0-100/120/G0及full最终门禁未关闭。文档修改后提醒Code-Index `refresh_index`；本次不昂贵全树重建。
