# R5 consumer、SSE transport 与 cold atomic 新证据

**新增三个有限证据层已交：consumer29分源组合、backend transport4top/23leaf分源组合、cold atomic8新fixture。不是最新whole29/whole4一次退出0，不是新真实cold build或完整Go→PG→UI/browser。仍10/171，P0-100/120/G0/G7未关。** 原失败、test-only修复、精确source/selector与清理分开登记；本agent只集成必要raw/元数据，未重跑测试/validator或每文件hash。

M仅prepared，受全新agent thread-limit独审阻塞，未实际执行；current METHOD变化不污染已冻结S4074/87c41。历史S2/S3d07/S4074三条及M/M2 failed/unknown精确tuple不改。此前层级见[显式S/cold/visibility报告](R5-OPTIN-S-COLD-INTEGRATION-20261002.md)。

## Evidence与不可合并的终态

| ID | 精确source/selector | raw终态与安全交付 |
|---|---|---|
| E-CONSUMER-A | 原cold source532a/原lock依赖复用+7捕获文件；consumer test SHA `dd8515a83ad490d6d917b91cfcf5138d98c1ed4bd4ec38f76623fc3dd813ef5b`。`vitest run lib/api/company-events.test.ts features/company/company-event-consumer.test.tsx --maxWorkers=1` | **whole29 CLI1：16PASS/13SidebarProvider fixtureFAIL**，5.219093/180s；原stdout/stderr/JSON/exit/source receipt保。见[consumer原review](evidence/R5-COMPANY-EVENT-CONSUMER-REVIEW.json)、[18选中原payload安全档](evidence/R5-COMPANY-EVENT-CONSUMER-LOGS.tar.gz) |
| E-CONSUMER-B | 只test SHA `18fd4000778be21a0dce311c9ad29729cc597fe97d62d3f5ead12ffbfde27afb`；12render sites包真实SidebarProvider，13leaf。`repair13/command.json`为13精确fullName anchored regex | **仅exact13 CLI0/13PASS**，3.429019/180s；Vitest14注册中1个先前已PASS的permission-editor case显式排除/skip，不是必跑目标跳过；16原PASS未复跑。生产/expect/clock不额外改，不声称latestwhole29exit0 |
| E-TRANSPORT-A | canonical closure SHA1 `a9e8a47238201fde4b903e47574b3ff3b0a6f678`，closureSHA256 `15b4acc7166c809bf42be9cc6ee4ad227e540335bcc36c4ef97282c7dcf69038`，非Git；四精确top selectors见下文 | **pure17 Go0，PGscope+fence4 PASS；原combinedPG/HTTP Go1，HTTP oracle FAIL**。见[transport原review](evidence/R5-COMPANY-EVENT-TRANSPORT-REVIEW.json)、[34选中原payload安全档](evidence/R5-COMPANY-EVENT-TRANSPORT-LOGS.tar.gz) |
| E-TRANSPORT-B | canonical closure SHA1 `bc82341749cd54a0a97dcfb595d5b8dd75c0001e`，closureSHA256 `cceb1e7ee020b0e60b5a7478ded3634afa92e3c3a3a9844bfb49c0d9b02b517b`；仅API test SHA `bf38c9aabc41d6a954ed0d6ee101192313b9e48cc4d0a6d43aebdd9da8f1623a` | **sole仅HTTP top -race Go0**，test2.350s/package4.457s；pure/PG旧绿不复，生产不改。4top/23leaf是composed provenance，非newwhole4统一一次Go0 |
| E-COLD-ATOMIC | runner SHA `b4db8da6207512ca30333430204a2a62331629d2fc17fec9d1f63f8c98726a7a`、tests SHA `02185f041b06d830a36e5855512a49c7b9315ea82e8322d3942628b59a4d969d`；有效v2 packet路径在原source receipt，旧basename packet已invalidated | **8new fixtures首次PASS/CLI0**，unittest4.925s/controller5.028105s<10s、ownedgroupabsent；[原review](evidence/R5-COLD-ATOMIC8-REVIEW.json)、[7原payload档](evidence/R5-COLD-ATOMIC8-LOGS.tar.gz)、[作者final delivery](evidence/R5-COLD-ATOMIC8-FINAL-DELIVERY.json)。不npm/Node/Go/PG/Next/browser |
| E-COLD-OLD-FAIL | 前候选runner `baccc2...`，真实最小mock CLI | [原counterexample review](evidence/R5-COLD-TERMINAL-COUNTEREXAMPLE-REVIEW.json)：CLI1但machine cold_receipt.pass=true、缺lifecycle receipt，1.009738s，counterexample confirmed；原FAIL不覆盖、不贴新b4db8 |

两个consumer阶段不具有Go canonical source身份，只使用原532a base+明确7文件receipt及test-only delta，不伪造新Git/tree/全source SHA。两transport canonical身份源文件映射独立；原receipt错继承visibility tops仅**包装元数据纠正**，保`source-receipt-inherited-fields-original.json`及`packaging-fields-correction.json`，不改变files/source label/raw。

Transport原执行精确selectors（第三个阶段只跑HTTP那一个）：

```text
^TestR5CompanyAdminStreamUnitContract$
^TestR5CompanyAdminEventsOriginalOutboxCurrentScope$
^TestR5CompanyAdminEventsReleaseFence$
^TestR5CompanyAdminStreamHTTPDurableScopeReconnectAndRevocation$
```

Cold精确8selectors包含atomic actual alarm、parser guard、observation expiry/bounded failure、single complete publication、post-publication revoke、raw-read expiry、BaseException revoke、reject timeout abbreviation；完整argv在档内`command.json`，没有运行旧9/16+2或真实npm build。

## Finding→Path：只能关闭对应子层

### F-CONSUMER：validated/high，29 unique各有actualPASS来源

E-CONSUMER-A/B证明严格最小metadata/tenant-bound envelope与wrong tenant/unknown/private metadata拒绝、duplicate/cursor advisory、正常EOF重连、现有refresh成功/终失败、stable-principal token rotation、dirty CAS draft不自动rebase、late scope403不驱逐新tenant、confirmation invalidation不自动DELETE等组件/ReadableStream线控。

P-C：controlled authenticated ReadableStream→真实consumer/parser→required provider包裹mount→authoritative资源重读或scope stop/清private draft。每步为Node/Vitest组件输入证据；**不是后端Go/PG actualSSE与UI同一次全链**，不真实shipping browser。29unique组合被独立只读review接受；当前修后全whole29没有重新执行，也没有捏造CLI0。

### F-TRANSPORT：validated/high，窄U/D/H分源通过

E-TRANSPORT-A证明纯17断言/current Auth-Revalidate-handler、原outbox current tenant/read scope及role/session/write-error/cancel release-fence。新singleframe权限锁/actualflush bounded port不借批量先读后释放延续旧权利；纯flush-timeout/取消负控不等所有平台网络时限认证。

首次HTTP在正式降级后看到`id:`帧而非立即EOF，原oracle未消费全部已合法释放的precommit帧；保原Go1/FAIL，**不据此虚判新的product RED**。E-TRANSPORT-B只改test oracle，生产未改，实际串联：

1. 消费initial15已知帧到synthetic末barrier；
2. 正式role demotion commit完成；
3. fresh qualified super经正式`ConfigureCompany`产生唯一原outbox marker；
4. 旧stream clean EOF且拒任何新frame；
5. fresh current stream精确marker正控。

P-T以上步骤均由sole ownedPG+shipping HTTP/-race新selector证明，不是仅allowlist源码检查。PG/HTTP全部ownedresources真实stop0、DB/conn0、PID/socket/ownedgroupsabsent。仍不证明25s periodic长期撤权、retention/cursor提交倒序、多实例claim/ack-loss、outbox index/capacity或browser联动。

### F-COLD-ATOMIC：validated/high，仅新guard adversarial fixtures

E-COLD-OLD-FAIL中的CLI/machine结果不一致有真实反例；E-COLD-ATOMIC与作者final delivery证明新guard/parser前alarm+noabbrev、full receipt后唯一atomiccanonical publish、BaseException revoke、work-expiry传播/原remaining hard-budget bounded失败路径的**8个新mock targets**通过，独立只读review接受此子范围。

P-A：命令解析前guard→bounded work/remaining→完成全部receipt→唯一atomic publish；异常/超时→撤销可见PASS→outerexit0+controllercompleteproof门禁。SIGKILL/不可恢复OS故障不能保证Python必写终态，必须保外层证明，不宣“杀进程必有receipt”。

新b4db8没有fresh真实npm/Next；旧native22秒实际cold仍source532a/runner86645，不能移贴b4db8。旧候选9pass、原counterexample CLI1、最终8mockPASS各分层，不拼一个最新全suite或generic hard600所有路径认证。

## 当前剩余具名工单

| ID | 原父项/依赖 | 剩余与验收 |
|---|---|---|
| RES-EVENT-UI-CHAIN-01 | P7-100/130、P9-130；现consumer/transport子层与原父依赖 | 将实际Go+PG/company events接shipping standalone UI，同tenant/company切换、role/epoch/freeze、EOF/refresh/late403验证private eviction与dirtydraft；没有同次真实全链，不由29组件或HTTP单top替代 |
| RES-EVENT-PERIODIC-01 | P7-090/100；原依赖保 | 实际25s周期、retention cursor、提交顺序倒置、断线/缺口/resync与当前授权终态；不能用2秒frame/模拟clock覆盖长期runtime |
| RES-EVENT-MULTIINSTANCE-01 | P7-080/130；原依赖保 | 两实例claim互斥、ackloss/duplicate恢复、跨实例cursor切换与无私字段泄漏；单进程ownedPG+HTTP不是多实例证明 |
| RES-EVENT-CAPACITY-01 | P7-060、P10-020/060；原依赖保 | 原outbox read索引/计划/积压/foreground影响在冻结源真实观测；已有100k mail search/Claim20不证明event index/capacity |
| RES-P1-EVENT-03 | P1-110/130、P7-070；原依赖保 | 空global audience平台通知/invalidation政策仍显式裁决，不全tenant广播或noevent冒平台刷新完成 |
| RES-COLD-03 | P11-130、P9-130；新guard源/现web源及原依赖 | b4db8 guard已新mock资格，但当前新consumer/UI源码未由旧532a实际cold覆盖。新的受影响source冷build/standalone与outer完整证明及Docker/browser必要层尚未实测，不重复旧源green冒新源 |
| RES-M-03 / RES-M-02 | P0-100；新METHOD/最小targets/全新独审→sole原M | M prepared，new-agent threadlimit阻全新独审，不继承S或复用旧review冒fresh；M未run、L拒，10800s/80GiB/1M原budget不变，S87c历史不受current方法变化污染 |
| RES-RCV-01/02/03、RES-GATES-01、RES-G0-01 | 原P2/P7/P11/P0完整依赖 | legacyunpin/incarnation无schema、received分源53leaf不singlefreshfull，fresh全Go/PG/迁移/协议/最终scope与原M/G0仍待；原171其它父项具名要求全部保 |

本次RES-P1-EVENT-04组件子层、RES-SSE-TRANSPORT-01窄U/D/H子层、RES-COLD-02新mock guard资格已交；**不勾父项、不删除失败、不给completed129等新分母**。下一三独占：**RES-EVENT-UI-CHAIN-01、RES-EVENT-PERIODIC-01、RES-COLD-03**；M独审资源条件另保真实blocked/pending，不因这些短target绿启动M。

## 安全交付与离线复核

[交付清单](evidence/R5-EVENT-COLDATOMIC-DELIVERY-20261002.json)新增18/34/7选中原payload。consumer原failed test源码含credential形状Bearer fixture literals，预防性不随安全档公开该source文件（不判断为真实凭据）；原件私有保，source before/after receipt与失败/repair原raw全保。transport排除owned server `postgres.log`避免query/params外泄，其余selector/JSONL/exit/source/cleanup原字节不改。cold7文件原Python tar直接复制。

原archive hash与新增safe-wrapper hash分列，不逐文件重hash；没有private DSN/JWT、邮件/对象、DB目录、CPUtrace/pprof、full source/dependency/build artifacts上传。历史档不改，未测试/validator复跑、未commit/push/PR/部署。

```sh
# cwd=docs/company-mail；只读，不重新运行产品
python3 -m json.tool evidence/R5-EVENT-COLDATOMIC-DELIVERY-20261002.json >/dev/null
tar -xOf evidence/R5-COMPANY-EVENT-CONSUMER-LOGS.tar.gz tests.exit
tar -xOf evidence/R5-COMPANY-EVENT-CONSUMER-LOGS.tar.gz repair13/tests.exit
tar -xOf evidence/R5-COMPANY-EVENT-TRANSPORT-LOGS.tar.gz pg-http/go.exit
tar -xOf evidence/R5-COMPANY-EVENT-TRANSPORT-LOGS.tar.gz http-oracle-repair/go.exit
tar -xOf evidence/R5-COLD-ATOMIC8-LOGS.tar.gz tests.exit
```

预期1/0/1/0/0；把两个1删掉或改0就是错误证据合并。完整runtime复现仍需owner私有source与native工具/ownedDSN。本次只文件索引刷新，不deepbuild；**10/171、P0/G0/G7仍未验**。
