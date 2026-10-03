# R5 显式方法新 S、profile 可见性与 native cold 有限集成

**显式 opt-in lookahead 方法的新完整原 S 已独审接受，另有 profile 可见性窄 U/D/H 与 native Node22 cold 实测。仍10/171，P0-100/120/G0未关闭。** 新S不是B诊断改名，历史S/M失败/unknown及精确source/method tuple不改。M只有后续条件准入：新方法合同/源码、最小目标与全新独审通过后才可原10800s/80GiB/1M；本轮未启动M，L不批准。

本次只整合作者新证据、必要raw终态与安全包装；不重跑已通过测试、benchmark/validator或全文件hash。新增METHOD文件独占作者为`baseline_dispatch/lookahead_contract_validator`，本文未修改METHOD或原DATASETS。前两轮来源见[初次集成](R5-BOUNDED-INTEGRATION-20261002.md)、[B诊断/fanout后续](R5-BOUNDED-FOLLOWUP-20261002.md)。

> 后续consumer/transport/coldatomic已有新有限终态，见[最新短窗报告](R5-EVENT-COLDATOMIC-INTEGRATION-20261002.md)。本文件prepared表按当时source保留，不代表最新判定；既有S87c/nativecold旧源事实不改。

## Evidence：新source与实际层级

| ID | source 身份／精确运行合同 | 原始终态与交付 |
|---|---|---|
| E-OPTIN-TOOL | canonical closure SHA1 `4074282496c0c60e3b6e0c6f6c1407858939c95c`，closureSHA256 `83a7da1045b8ccdca4f18ed2b722fdcf59c7c352bcf65846a922728adb12272a`；METHOD SHA256 `87c41f7a841399f25a9e7eb7017bee59c57d3d340d30d8569cad59f66bf30c19`，DATASETS `9e81020680dbd6e990cbb35ab1aac16413885e65306d949a5889c4bafda00b36` | [tool原review](evidence/R5-B01-AJ-LOOKAHEAD-TOOL-REVIEW.json)、[19原payload档](evidence/R5-B01-AJ-LOOKAHEAD-TOOL-LOGS.tar.gz)；真实20/100/1000、ready1000、14×200=2800、SQL20/7safety、Go0/strictCLI0，21.158307/300s；旧工具未重跑 |
| E-OPTIN-S | 同一qualified tool source407428/closure83a7/METHOD87c41/DATASETS9e810 | [S原review](evidence/R5-B01-AJ-LOOKAHEAD-S-REVIEW.json)、[27原payload档](evidence/R5-B01-AJ-LOOKAHEAD-S-LOGS.tar.gz)、[单独result绑定receipt](evidence/R5-B01-AJ-LOOKAHEAD-S-RESULT-BINDING.json)；原100/500/100000/schema16/ready100000、Go/runner/strictCLI0、14×200/SQL20/7safety、native清理齐；独立只读review接受，非本agent复验 |
| E-VISIBILITY | canonical closure SHA1 `5c879876f2713fd754d9e6bcd5627beb145f2ea4`，closureSHA256 `32fc6c04d7f780079296e2ee12c52ed0b04346aed3b10825ad570f334ee89182`；由abea fanout快照加独占6文件delta | [visibility原review](evidence/R5-P1-PROFILE-VISIBILITY-REVIEW.json)、[19原payload作者clean档](evidence/R5-P1-PROFILE-VISIBILITY-LOGS.tar.gz)；3必要service纯top/5leaf，4PG+3HTTP top/40leaf，各phase race/Go0；不拼其它旧suite |
| E-COLD | **不是Go canonical身份**：tracked web archive SHA256+明确17个dirty delta receipt；HEAD `587ba75c0d76b26d3f5a060b131d77fbc12495ac`，source pin `532a491c673dfd7f384f514041b465dbbdb1ae63ac4dc018bb80eb3d2046aa59`，实际runtime runner `86645b99a56ece120fa80873cc88a8e7d9f81b44208f8491fed0d18b2bcaef1e` | [cold原review](evidence/R5-NATIVE-NODE22-COLD-REVIEW.json)、[22原payload安全档](evidence/R5-NATIVE-NODE22-COLD-LOGS.tar.gz)；Darwin arm64 Node22.23.2/npm10.9.8原lock冷安装+Next source build，非Docker/browser/G0 |

新Go source标识均为canonical closure SHA1，**非Git tree/commit**。目录名、主checkout HEAD或后来cold/SSE/M修改不等这些不可变运行源。source closure/proof、redactedenv、command/exits、method-selection/原contract、清理都随对应档；不得把当前dirty树或新候选runner贴旧22秒runtime。

## Finding：新方法原 S 已实际完整采集

- 原Go execution wall=`1377.669084/1800s`；作者Go→package wall=`1386.695192s`、raw package pass=`1387.807s`，独审观察runner wall=`1393.058s`。起点不同，不互换为一个时钟。
- 同原人口100 employees/500 mailboxes/100000 messages；index-ready100000；14行每行200，actual SQL20/7安全正控/当前撤权与foreign拒绝齐。disk=`1892809751bytes`、processRSS=`395558912bytes`，不声称整机/PG/Redis memory。
- actual scheduler permutation全100000、生成/emit/completed齐、buffer≤100、durable window/active≤20、FIFO/joined inflight0；原合法Claim20、Parser4/30s、31s原wall余量guard、所有started join/no ANALYZE/planner/durability改造不变。
- 实际PG catalog UID列0、received_at/ordinal投影100000 messages/400 mailboxes、FIFO违例0；这是实际PG投影，不是只相信scheduler元数据。**仍不认证protocolUID等价、所有persistent/timestamps/UUID/lease/physical顺序全等**。
- 与原S3d07直接对原档：六canonical streams/aggregate/parameter精确相等。未重跑旧S、没有增normalize把差异抹掉；两个source包含不同shipping SQL/资格/分页改动，性能变化不得纯归因lookahead。

| 本次S观测 | 值与边界 |
|---|---|
| inbox p95 app cold/hot | 14.449/7.839ms，通过**S list≤300ms**候选；相对旧3d07的9.118/4.145ms有所上升，relative≤10%未cert |
| indexed_search p95 app cold/hot | 16.251/15.802ms，属于新source真实S值；不是M≤1s或隔离调度因果证明 |
| same-parameter actual Claim20 | ready99980时0.502ms；最终ready100000 empty0.303ms |
| M规划风险 | 本次Store×10=`11731.872s`、pipeline×10=`13509.740s`仍大于原M10800s；仅规划风险，非已测M失败下界/成功保证/扩预算授权 |

**F-S（validated/high，限新selected S）→P-S**：用户显式opt-in批准→独占方法实现/合同→独立review→fresh完整tool门禁E-OPTIN-TOOL→唯一fresh原S E-OPTIN-S→原bytes/result sidecar绑定→独立只读验收→只追加新S registry。原S2、3d07/v1两条历史成功与M/M2失败dict保留，不补method字段。B4k仍诊断，不变正式scale。

### 精确历史方法与hash归属

- 新S实际method=`lookahead100_unpersisted_inputs_distinct_mailbox_windows20_ordinal_fifo_v1`，必须显式CLI/env选中；默认`bounded_store_claim_complete_window_v1`不改变。新[冻结S METHOD副本](evidence/R5-B01-AJ-LOOKAHEAD-S-FROZEN-METHOD.json)与档内`S/method-contract.json`都是原87c41字节，`S/method-selection.json`是实际runner argv/env/source/digest。
- 同名current `R5-BENCHMARK-LOOKAHEAD100-METHOD.json`后续为M扩allowedscale时会产生**新digest/source**；历史S准入只读档内原dict+87c41与closure绑定，不要求current同名文件永远87c41，更不回填S字段或放宽schema泛豁免。
- S review原无result/go hash；executor仅对**两已采文件一次有界SHA**补独立receipt，没有source sweep或重validator/S。result=`24c54c89d149e13d3f60eb8104eada14fc88261a0ea3ed1b586ad10f14fb1c44`；rawGo=`968b0bb4d45a22302c4b88418454c258748b369ace3e93a935e6e4b649fe2790`。原review/tar不改，本agent沿receipt登记，不把tarSHA伪作resultSHA。

## Finding：profile 可见性窄入口已实证

**F-V（validated/high，U/D/H限定）**：service正式専reader port/failclosed、shipping GET→service→PgStore，在current platform/global/selected-tenant与tenant-local范围区分可见性；被global profile引用的normal admin/reader不能借此裸读所有global profile。

实际覆盖当前降级/freeze/epoch、API Key/非交互身份、selected company缺失/foreign/zero；真实`UpdateUserGuarded`写authority暂停在required audit，reader `user FOR SHARE`按exact writer PID实block，等锁后fresh重新检查role/epoch/frozen。没有独立compile或旧32PG/完整role/allAPI重新跑，也不当列表父scope和P1-130/G7全关闭。

P-V：fresh principal+selected company→profile reader authoritative scope→current actor FOR SHARE等待与postwait重核→限定列表/HTTP projection；E-VISIBILITY证明这些步骤。平台空受众通知政策、SSE consumer及最终浏览器仍没有本轮实际runtime。

## Finding：native cold 是实测成功，不是所有路径600秒合同全认证

**F-C（validated/high，限E-COLD）**：owned fresh source/HOME/npm cache、empty npm config，不复用workspace `node_modules/.next`、不改原package/lock；npmci0=`12.327s`、Nextbuild0=`9.263s`，controller execution+capture/cleanup观测=`22.306615s<600`，各stage原300s；真实BUILD_ID/standalone/server.js/static存在，ownedgroups实际no-live。

- Native Darwin arm64 source cold build，不启动Go/PG/server/Docker；没有shipping image/browser/G0。旧16pure、2新pin、4budget修复及npmrc目标各source/repair分层，不拼最新all-suite。
- 新只读v2 review接受本次22秒预算内观察，但指出source capture在execute deadline外、cleanup observe_group有独立2s reserve风险，**通用hard600全路径未认证**。
- 后续runner候选`baccc2...`/tests`447e778...`只有新增pure targets7+2 PASS，无fresh npm runtime；另实际tinyCLI1与machine PASS冲突原反例留在`/tmp/tabmail-r5-cold-finalreceipt-counterexample.funRtyBT`，作者正修terminal atomic候选。不能把旧86645/532a coldPASS贴候选新hash，也不把candidate PASS当hard600全部关闭。

P-C：独占capture pin→cold npmci→Next build→presence+original lock复核→实际ownedgroups清理；E-COLD只证明本次原v2运行。后续terminal receipt/absolute deadline/unknown cleanup需新源码与必要反例验收。

## 当前具名缺口与下一独占三项

| 工单 | 原父项/依赖 | 现状态与验收 |
|---|---|---|
| RES-M-01 | P0-100，原040/090已验 | **S方法实现→独审→完整tool→一次原S已有限交付**，不勾100；默认v1/旧档不改。后续M条件准入另RES-M-03，不能借87c41 S-only合同直接M |
| RES-M-03｜新M条件准入 | P0-100；依本次S及新合同/最小Go+runner+METHOD targets/全新独审 | 新M current合同候选digest=`d286ae9f5f4b68033ba9739a1276de22f1e0f7ef88e15642d52633da943b49a7`，4新scope/CLI/history负例target纯PASS；只prepared，Go/runtime/全新独审未cert，current作者独占。独审slots或政策不足时不启动。准入后才唯一operator原1M/C20/10800s/80GiB/14×200/ready1M与全terminal；L拒，原M2unknown保，不延长原预算 |
| RES-P1-EVENT-02 | P1-010/030/090、P7-010；原父依赖保 | 本轮service+PG+HTTP可见性窄证已交；父项整体/全角色/消费者/浏览器仍待，不把10top45leaf数量当全完成 |
| RES-P1-EVENT-03 | P1-110/130、P7-070；原依赖保 | 空global audience的平台invalidation/通知范围仍需显式政策；不私自全tenant广播 |
| RES-P1-EVENT-04 / RES-SSE-TRANSPORT-01 | P7-080/090/100、P9-130；原依赖保 | 当前read→release批鉴权到actual flush窗口的静态blocker在修；singleframe FOR SHARE+actualFlush+2sdeadline只是candidate，new target未runtime。consumer29含EOF/refresh3新增但fakeclock反例还在修，prepared不PASS；唯一executor下一短窗必要runtime |
| RES-COLD-02｜terminal atomic/hard deadline | P11-130、P9-130；新候选源独审/原依赖保 | 旧22秒实际cold只旧v2，tinyCLI1/machinePASS冲突保原raw；新atomic8候选与boundeddeadline/unknown非absence规则待必要纯/真实最小CLI验收，不重跑旧npm讨绿 |
| RES-BROWSER-01 | P9-130、P11-130；Go/PG/standalone及资格依赖保 | 7pure只frozen532a旧UI qualification；全新独审受threadlimit可能阻塞，实际browser尚未执行，不dev/mock代shipping |
| RES-RCV-01/02/03、RES-GATES-01、RES-G0-01 | 原P2/P7/P11/P0依赖 | legacyunpin/incarnation无schema、received53leaf composed≠最新singlefullGo0，完整PG/Go/protocol/迁移/当前source fullbuild/全终审和原M/G0仍开；其它原171 scope不取消 |

下一三独占工单：**RES-M-03新M条件准入、RES-P1-EVENT-04/RES-SSE-TRANSPORT-01消费者与frame transport必要短窗、RES-COLD-02 terminal atomic验收**。主线程不重复已通过命令；source/contract共有文件继续由对应owner串行整合。

## 安全包装与离线检查

[交付清单](evidence/R5-OPTIN-S-COLD-DELIVERY-20261002.json)分列四档22/19/19/27原payload。visibility用作者已剔21sidecar的clean包；tool/S原Python tar无sidecar原封复制。cold原7ce476 tar也含AppleDouble，本agent仅剔sidecar重包，22regular payload原字节保，新wrapper hash另列，不公开原脏包。无private DSN/JWT/rawmail/objects/DB、CPUtrace/pprof、完整source/`node_modules/.next/HOME/cache`上仓；cold presence凭原receipts，不上传build产物。

```sh
# cwd=docs/company-mail；只读，不启动产品
python3 -m json.tool evidence/R5-BENCHMARK-RUNS.json >/dev/null
tar -xOf evidence/R5-B01-AJ-LOOKAHEAD-S-LOGS.tar.gz S/go.exit
tar -xOf evidence/R5-B01-AJ-LOOKAHEAD-S-LOGS.tar.gz runner/S-runner.exit
tar -xOf evidence/R5-P1-PROFILE-VISIBILITY-LOGS.tar.gz pg-http/go.exit
```

预期0/0/0。完整runtime重现仍需owner私有只读source/native工具/ownedDSN。文档更新后刷新Code-Index文件索引，不另建deep index或重跑product gate。本轮无commit/push/PR/merge/部署；**10/171与P0-100/120/G0未完成保留**。
