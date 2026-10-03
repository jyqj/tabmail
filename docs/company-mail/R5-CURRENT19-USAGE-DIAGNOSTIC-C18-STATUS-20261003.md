# Current19 usage、限定诊断与C18状态增量

> **后续增量：** D2clean/D3/B/S已限定独审actualP，C0/V8未知与V9 SOURCE待审、编码/worker组件及PGphase/watchdog运行拒绝见[最新报告](R5-USAGE19-C-OBSERVER-SHUTDOWN-ENCODING-STATUS-20261003.md)。下文是原各source当时时点，原A/D2失败不回写，组件不升whole/METHOD父门。

**当前19组件尚未通过：原A失败保，D1通过不补D2/D3/B/S/C；18冻结树与18因果证据不转授19。** [安全交付](evidence/R5-CURRENT19-USAGE-C18-INCREMENTAL-DELIVERY-20261003.json)登记来源/原archive SHA/公开脱敏映射。docs owner只读取必要receipts/独审交付与安全包装，没有源码修改、重测试、全treehash或发布。HEAD587ba75/work分支仍dirty多agent WIP，不能用旧HEAD代表实际运行source。

## 1. Current19 physical usage：SOURCE通过，产品运行仍分层未闭

原九文件与两个addon SOURCE已独审PASS，实际迁移19采用physical `tenant_api_key_usage`，但SOURCE不完整runtime。尚有三个具名边界：**Stop没有显式deadline；原retry只接受401/403/404拒409；auth asyncTouch没有显式join**。不能凭组件准备/单case结束宣全部旧主体动作和后台goroutine均终止。

[SOURCE/运行状态注册](evidence/R5-CURRENT19-USAGE-DIAGNOSTIC-STATUS-20261003.json)明确三组身份。当前主树19不能继承frozen18或旧原M16资格，也不能把new schema编号写回old partialcatalog。

## 2. 原A：真实FAIL，不用回base的六内部PASS转授

source `755a64743213016d42c6a025401368aaa4a36ccf` / closure `95a9c8216aedab9f55a66d4035e51b72f8a879754efe412fe29ea094fb79a0dd`，[公开安全review](evidence/R5-USAGE19-FIRST-A-FAILED-REVIEW.json)/[55公开安全payload](evidence/R5-USAGE19-FIRST-A-FAILED-LOGS.tar.gz)，原safe archive SHA `0725b2229f7ac985da837ff81a9e7ebb9c50c5a6369eedc91c632b6583cc930b`。

12top26leaf32testnodes=25PASS7FAIL（21PASSleaf/5FAILleaf）、Go1/0skip/unfinished。失败是TwoPoolsDeadline、fresh19 migration pair mismatch、upgrade fixture not18、heldusage helper、SchemaInventory缺R4实际runtime输入；最后一个test已run，不把整批叫0tests。**原内部六PASS因ConnString回base不能作为freshDB资格转授**，也不因为后来修fixture就删除原失败。

base官方SQL19/sourcepost相等；496DB sample见26 nonbase下界含九usage19，只按已观察范围。原300总40.779036s/package23.930s，PG35560stop0/PID/socket/groups/sampler gone，terminal仅base tm_usage19_a/otherclient0，base私留stopped，不allDB0。**B/S/C NOT_RUN**，没有SMTP/miniredis/Cobserver证明，外部Redis13082未触碰。

本报告review取自executor已脱敏archive成员，**不直接复制private-derived-review原件**。[公开redaction map](evidence/R5-USAGE19-FIRST-A-FAILED-PUBLIC-REDACTION-MAP.json)绑定private raw SHA→safe SHA，原private raw不改；docs没有重新生成测试日志或脱敏伪装原字节。

## 3. D1 PASS → D2 FAIL → D3 NOT_RUN；clean-context只SOURCE

code source `54a46523480bc42a14e1af3543545846a5ab1ff9` / closure `794b29918be9047190ba3fc685d25b70ecc3ffd5f9930b3074e619b1fd796dd4`；runtime R4JSON+19SQL共20inputs独立closure `4d76b35caa9c5e4c84f4ab438e21b7e2fe3a6a208a4d37ccad79bf0622e68ecc`，joint `bb1621915501653fea2c691b1ccf33af3d502bd76401868e20491d7ae36b0d1a`。双code/runtime prepost稳定，**不能反称old source-policy已经覆盖R4或D3已通过**。

[公开安全review](evidence/R5-USAGE19-D1P-D2F-D3NOTRUN-REVIEW.json)/[111公开安全payload](evidence/R5-USAGE19-D1P-D2F-D3NOTRUN-LOGS.tar.gz)，safe SHA `49889618affb945a4e92b911a7ae606c2b5443599cc4ea98381b3bac63d797d8`，另[private→public映射](evidence/R5-USAGE19-D1P-D2F-D3NOTRUN-PUBLIC-REDACTION-MAP.json)。

- **D1：** 5top9leaf12racePASS/Go0、26连接identity断言、九freshDB。fixtureDSN修正已独审；17.577712s≤原300，package3.995s。D1不得再重跑。
- **D2：** only usageheld 1top1leaf2FAILnodes/Go1/0skip/unfinished，15.169748s≤原300、package2.556s。authority-free after-usage-waiter-join阶段PASS后，independent router404/nonJSON、current500，精确200拒绝保。独审定位**外ctx污染chi.RouteContext**；这是源码/fixture诊断，不等修复后HTTP实际通过，不直接判产品权限退化。
- **D3 NOT_RUN**，不是已捕获runtimeinputs就有schema/inventory验收。
- 两native实际SQL19；PG48630/49858stop0，全DB只各base、otherclient0/PID/socket/groupsgone，bases私留stopped；sampler9+1是下界，不扩大数据库或连接观测数。无SMTP/miniredis/Cobserver。

最新[clean-context SOURCE manifest](evidence/R5-USAGE19-CLEAN-REQUEST-CONTEXT-SOURCE-MANIFEST.json)只一testfile`r5_key_touch_admission_test.go`，after `8158445c6bb9969b49c9f7d00da19b475e320bda781a0dba3882399bcd6a212f`，只clean parent/context与独立GET调用点；**原独审/新D2运行仍待**。D1不复，原A FAILED、D2 FAILED和B/S/C/D3未run不回填，不自动扩大范围。

## 4. C18 pure21：真实限定PASS，不METHOD/fullcatalog

[原review](evidence/R5-C18-RSS-V5-PURE21-ACTUAL-REVIEW.json)/[34原payload](evidence/R5-C18-RSS-V5-PURE21-ACTUAL-LOGS.tar.gz)，SHA `dff289708d95ef0d73cbe20169b3a8df6d10b59b5e921dee8770ebe5ea42ad84`。七source6710e89d/52c2f082与SOURCE V2/post相等；controller174da8与child/protocol签bytes匹配。新独审限定 **RSS_SAMPLED_BOUNDED_PURE_GRAMMAR_PASS**。

精确21start+21PASS、20subtestPASS、62events单调，selector/discovery/start/pass集合一致，0skip/fail/error/unfinished；outer0+child78726exit0+finaladmission联合成立。CPU60/60实际getrlimit、audit773463events/0denial，十RSS samples/peak38,010,880B/maxgap0.110868583s；首尾父接完整handshake早于专属query begin，import在auditCPU/首样本之后，group成员/live为空，无signals/额外5s containment。

时间只能写 **to-terminal1.164346s、finaladmission约1.164481s、原90gate通过**，不把terminal前采样当精确外exit耗时。RSS是采样而非atomic/global quota；CPU回读只该child层，不扩大整树资源保证。原ASsetup INFRA0tests及RSS V2–V5 mock分层全保。可关闭pure grammar/resources这个子scope，**C18 runtimeMethod/fullcatalog/build/tool/S/M/L及171父项未过**。[状态注册已更新](evidence/R5-C18-RESOURCE-PURE21-STATUS-20261003.json)。

## 5. 完整editable18冻结树与catalog15pure：不继承当前19

[完整SOURCE18 receipt](evidence/R5-CURRENT18-COMPLETE-SOURCE-CHECKPOINT-RECEIPT.json)/[九receipt-only原payload](evidence/R5-CURRENT18-COMPLETE-SOURCE-CHECKPOINT-LOGS.tar.gz)，SHA `4f885c675817d5da33a1e5d17a3b34eb7a90e5285378b63baf4dd112308c7314`。private普通editable树 `/private/tmp/tm-r5current18-complete-SOURCE.7vf5gy3_/editable-source-tree`，1309files/20,747,066B、source `c7bf9155f89cd10371665cbd896309385a20dbc0` / closure `47fd3642eefab51ed9c19db7bc31167ce19f87f864781f36ffbcb3d8ecce9f0f`，manifest-byte SHA d870802a…，一次capture/after全同由sole原receipt证明，docs未再hash。SOURCE migrations1..18/max18，不当前19、编译或method签；树本身未另导出runtime/profile/objects/env/caches。

[catalog15原review](evidence/R5-CATALOG-V5-PURE15-ACTUAL-REVIEW.json)/[23原payload](evidence/R5-CATALOG-V5-PURE15-ACTUAL-LOGS.tar.gz)，SHA `8bc52ad10ba6f7ed56ac87afedf7e52c27578f7fdbbb5df1d836ef6e2c0166bb`，相同frozen18/collectorcbc7。15top155children157leaf170racePASS/Go0/zeroSkip/unfinished/owned；tag benchmark、GOMAX2/racep1parallel1/rootMVS readonly。GoList0/402pkg/371local inputs/22embed/34selectedfork/no缺，sourceafter同。package3.917/Go含compile14.395/原300全15.524128s；OS sandbox deny network*两阶段执行0，无DSN/owner/externalref/真实PGSQL。144RSS samples全known/peak499,302,400B/maxgap0.126619s，非hardquota；CPU只是ps观测+GOMAX2，非硬CPU时间限，groups/observergone。

这只synthetic serializer/gates：**runtime_reference_ready/fullsemantic资格false**。C18-09 owned/build/bootstrap profile/material、actual serializer fullreference/Method仍BLOCK，SOURCE V5 collector30/31集合设计不实际采集。额外machine独审报告未定位则如实待证，不伪文件或把作者actual变独审PASS。

## 6. 旧18 KEYTOUCH因果：仅受控fixture，不历史89451

[原review](evidence/R5-KEYTOUCH-CAUSAL-REVIEW.json)，source `42126f8ed039fa143031689ecf3d4c6266721c50` / closure `85fd37c1eb40dcca3779aa06b70a134973eb5e709f17f4aa15f0d7968a8d1977`，1top4leaf5racePASS/Go0/GOMAX2/noSkip。四freshDB，raw PID/query/phase same-key blocker与真实asyncTouch oncecommit四例有见证，不历史holder89451归因、productfix、当前19或whole预算证明。package5.984/Go含compile15.885/原180资源22.929187s，PG97643stop0/PID/socket/groupsgone/tempDB无、base tm_keytouch_validation0conn私留stopped。

原88包SHA3aa021…保private，controller-source有连接URI候选，公开[87原payload安全包](evidence/R5-KEYTOUCH-CAUSAL-SAFE87-LOGS.tar.gz)只剔该成员，[包装receipt](evidence/R5-KEYTOUCH-CAUSAL-SAFE-PACKAGING-RECEIPT.json)原/新wrapper绑定、保其余bytes，仅wrapper一次hash。不是重写或重跑实际证据。

## 7. PGexact8 phase：独审SOURCE BLOCK，绝无新runtime

private derivative `/private/tmp/tm-r5-pg-phase-a331-3r5aftg3`，[静态receipt](evidence/R5-PG-PHASE-A331-SOURCE-STATIC-RECEIPT.json)/[候选packet](evidence/R5-PG-PHASE-A331-SOURCE-PACKET.json)绑定旧a331/schema18：1256files、原1255/十二delta、其它bytes同；after-manifest dd0514e7…/patch2b2f62da…。只gofmt/PythonAST/static，未Go/compile/PG/controller，不能8PASS、budgetfix或current19原因。

原独审`pg_phase_a331_source_review` **BLOCKED**，具名六类：packet selector/controller SHA未绑定；parser completeness/emiterror/unmatched/Go8终态缺；sampler health缺；全DB/group/nativecleanup漏；terminal/postcheck预算异常；wait只有实例聚合且source属性丢弃，不可fixture归因。未定位原machine报告路径时记为PM已交SOURCE verdict/transcript层，不伪落盘报告。[状态](evidence/R5-CURRENT18-CATALOG15-KEYTOUCH-PHASE-STATUS-20261003.json)明确BLOCK。

保旧Suppression expected16 vs18矛盾，若将来运行也可原样失败。当前fixture次数unknown；旧a331 timeline272 terminal（271pass/fail+1允许skip）、19PAUSE0CONT、RequiredOutbox在包179.996s才RUN、104未start，不从舍入elapsed累计或这瞬间推迁移大头/业务deadlock。旧dfb phase metrics只该旧scope。

## 8. 最近正式whole与公共门禁保留

最近已交的frozen18 **a331/da7e97 whole仍Go1**，不是本次current19运行：908/907/51、8230run8206P2FAILnodes（唯一WorkerRevocation/key-zone leaf HTTP409 changing-key）/1允许BrowserSkip21unfinished/104top未start；PG180.689/19白名单PAUSE0CONT，全pkg39pause20cont不得混同。两个classifier1/missing180/395不未run数。原unused source-postcheck copy手握INFRA0test保，同origin恢复不metadata重跑/自动延期；原1200total504.122s/native81837stop0，base及残testDB0conn私留stopped，不DB0。

[原whole review](evidence/R5-WHOLE-a331-RECEIPTFIX-FAILED-REVIEW.json)/[105原payload安全包](evidence/R5-WHOLE-a331-RECEIPTFIX-FAILED-SAFE105-LOGS.tar.gz)保原107包4b1b5bc…私留，剔两个connection-URI controller成员，[receipt](evidence/R5-WHOLE-a331-RECEIPTFIX-SAFE-PACKAGING-RECEIPT.json)绑定包装差异。上述component、C18pure与诊断不修写whole原FAIL。

用户尚未确认Docker实际ready，无新daemon/hostnetwork/ECI/双向probe/shipping receipt；四CI、secure全角色/tenant、完整current事务AST/coverage、full reference/Method/有效M/PG180仍未完成。原M85290010800FAILED与三个成功S只各历史tuple，registry不新增成功scale。

## 按依赖排序的下一可派发任务

| 排序 / 具名工单 | 窄state owner、依赖与验收 |
|---|---|
| 1 RES-USAGE19-CLEAN-CONTEXT-D2 | 原writer只一fixture815 SOURCE→原独审准入→sole onlyD2新actual exact200及连接/ctx证据，D1不复；不改产品期待/拒409或扩大D3/BSC |
| 2 RES-USAGE19-D3-RUNTIME-INPUT | D2通过后按PM批准只D3；code+R4JSON+19SQL独立冻结/joint source、original budget/actualschema19/全DBclient0与ownedcleanup。输入捕获不代D3P，原A不覆盖 |
| 3 RES-USAGE19-BSC-REMAINING | A失败分类/必要门禁合格且原new审再批准后唯一executor滚动B/S/C，各source/selector/SMTP/miniredis/ownedobserver/无join/Stopdeadline与retry401-404拒409全scope，未验每项具名保 |
| 4 RES-PG-PHASE-A331-PROTOCOL | 私有18 derivative原owner修六SOURCE blockers、首尾health/Go8集合/emiterror/原deadline/全cleanup、实例wait不假fixture；全新/原独审再准入后才sole exact8。主树19不并改或借因果，原矛盾保 |
| 5 C18-09 / C18-07-OWNED-REFERENCE | frozen18实际build/tag/source/distribution/initdb/extension/profile外签材料、owned scope及完整外对象定义对拍，缺refs连接前拒。15pure不完整资格；主树19需另签version/current source，不继承18 |
| 6 C18-02/05/06/Method版本 | 依完整reference/MVS与source policy owner-evi-root新签，runner唯一parent origin/earliestD/真实harness/31s±和join/cleanup资格；保持旧METHOD/DATASET/三S/M/已knownstrict，不current未知自动SML |
| 7 RES-CI-CURRENT / RES-SHIPPING | schema19组件合格后的稳定完整source/应跑集/原预算whole及四job，Docker真实ready/原shipping；无root批准不自动retry或放宽180/原million预算 |

待证项明确：catalog15/KEYTOUCH额外machine独审若未交不假报告；PGphase报告路径未定位不说已归档；clean815源码审/运行待；Stop deadline及auth asyncTouch join仍业务状态owner缺口。原171每叶的全部验收条件与父依赖不变，**当前仍10/171**，没有以21/170/12P或诊断编号凑父完成。
