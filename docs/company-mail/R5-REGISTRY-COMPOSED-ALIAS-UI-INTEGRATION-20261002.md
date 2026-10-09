# Registry有限adapter闭合与alias/UI追加层

## 已闭合：已知历史版本adapter子层，不是完整性能或fullCI

原全新 `registry_clock_failure_independent_audit` 已独立对原selector AST与实际raw做集合对照，接受**三窗口 composed 25unique PASS，0missing/extra/duplicatePASS**。共同runner/tests为cf156af9/79b3d443且三快照及当前字节相等；不称single fresh25 exit0。详[独审交付的中央摘要](evidence/R5-REGISTRY-COMPOSED-BOUNDED-ACCEPTANCE-20261002.json)，此JSON明确不是伪造独审机器raw。

| 窗口 | 原实际结果与来源 |
|---|---|
| 原25 | source9b777bea/cdc39ed，4exec=2PASS+2ERROR，Registry21 setUpERROR未exec，CLI1，漏真实corrections输入；[原review](evidence/R5-REGISTRY25-INPUT-INCOMPLETE-REVIEW.json)/[原档](evidence/R5-REGISTRY25-INPUT-INCOMPLETE-LOGS.tar.gz)保。 |
| depfix23 | source `a4e4093d153da174fabe92d622cff22100762db4` / closure `4e684771a1215d77c9fb5392db8f077f0a9403dda357b6d2740a8633f73a0cc3`，23exec22PASS+1缺actual migrations静态input ERROR；real registry CLI0。原两PASS/旧12PASS未复。[原分类sidecar](evidence/R5-REGISTRY23-DEPFIX-REVIEW.json)/[原安全档](evidence/R5-REGISTRY23-DEPFIX-LOGS.tar.gz)。 |
| only1 | source `58c7cfe30aed11dfff91e0c32a03aae58a55a5ed` / closure `aecaee0a4dad378849e9ce0d51b8b82b26b6481a4cefbde9f3d0579d9c8b3587`，仅原FrozenS2否定目标1PASS/CLI0/0skip，完整capture/cleanup0.276173s≤180；[原review](evidence/R5-REGISTRY1-MIGRATIONS-DEPENDENCY-REVIEW.json)/[原安全档](evidence/R5-REGISTRY1-MIGRATIONS-DEPENDENCY-LOGS.tar.gz)。22PASS/两PASS/realCLI0均未重跑。 |

输入28→29只新增原6252B corrections，29→47只新增18真实SQL静态依赖；common manifest digest不变，**没有运行SQL、捏造max18或改fixture期待**。各child groups观察空。depfix23控制器在groupgone后因origin局部变量被Path遮蔽TypeError，原child raw/phase budgets有效，但**原aggregate origin clock UNKNOWN保留**，不得拿only1或phaseclock回填整体绿。

真实CLI0只接受三历史完整S精确identity与852900/d286/schema16 known FAILED M，baseline/task/product均false；不是M baseline成功，也不是泛化未来review/method豁免。原M超时/部分DB与objects私留、缺result/GoJSON terminal、raw observed -15保。原S row/summary clocks不改，27fe12、e64871和8d554工具失败档保，M original archive仍0ab842。没有产品测试/每filehash重跑、无发布。

**本次合规关闭** `RES-REGISTRY-CLOCK-TYPED-M-01` 及原 `RES-REGISTRY-M-FAILVARIANT-01` 的“已知三S/该M失败版本adapter + 原25targets”子scope。这是具名证据适配缺口完成，不新增原171勾、不关M有效baseline/P0-100/120/G0/fullCI，也不清所有未来版本准入。

## 未闭合：真实alias/contract gate

source `ccd389e48c79c2143401bc0d96d6575304e457cf` / closure `010b187252c8dcbd043c107e8f01c1f186b189c3c67ce18e39d3643a7850b165`，26精准input，[alias原review](evidence/R5-ALIAS43-OAS3-FAILED-REVIEW.json)/[原安全档](evidence/R5-ALIAS43-OAS3-FAILED-LOGS.tar.gz)。新43methods全PASS，三受影响原方法1PASS/2FAIL，合46exec44PASS2FAIL/0skip、CLI1；real checker CLI1。**43工具反例绿不等repository合同绿**。实际25 drift涉及TS11个company字段optional、shared optional/nullitems、Stats []AuditEntry投影与三个route/schema映射等；OAS required并非错误，不能削弱schema讨绿。

后续TS input/response生产13files typed编译诊断仍SOURCE/prepared待sole，新Stats投影writer创建两次capacity拒且未实施，不称已派已修。三个mapping小patch由alias专属owner处理。ordinary typed/wire8PASS只是其scope，不覆盖本repo/realCLI失败；原普通metadata、explicit currentcontent与inspection例外保持分离。

**其后首版WireSplit施工诊断已真实执行，不是SOURCE绿：** source `1c976aafb989ffb84b864c5c29c5f766b192d783` / closure `cc079260793ab3b027be4b6fc4e50d278caaea36482a477f26717f257f8aa325`，194input/13writerpins及5既有generated ambient来源/mtime有录。[原诊断review](evidence/R5-WIRE-SPLIT-FIRST-TYPECHECK-DIAGNOSTIC-REVIEW.json)/[原安全档](evidence/R5-WIRE-SPLIT-FIRST-TYPECHECK-DIAGNOSTIC-LOGS.tar.gz)，CLI2/27diagnostics，实际原180s窗5.167697s、Nodegroupgone。10项stale .next/dev validator missing route module，17项真实TS consumer/test shape包括updated_at/id/kind、新DraftInput transport及旧protocol private recipients；不把全部归generated噪声，也不改ignore/fixture讨绿。writer已获原stdout；sourceFreeze稳定不等类型验收。无当前Nextbuild；不以此TS错误虚挡独立backend Go窗口，也不拼四CI全绿。

## UI falseContent仅两新leaf通过

source `3d2217ebd7fa37fc583eb0dd9b2143c8f1f64a1f` / closure `8e9cb3a0f834d7ce18b0f42a78681758fb061726cea05674ef8d333b6ec1d011`，8627fe/754af/4f9f三签delta，[原review](evidence/R5-RECEIPT-FALSECONTENT2-REVIEW.json)/[原安全档](evidence/R5-RECEIPT-FALSECONTENT2-LOGS.tar.gz)。仅2exactleaf PASS/CLI0/1.863821s，47filtered未exec。未改scope epoch/lifetime；page只invalid wire failclosed，不是真实撤权/期限证明；原whole54FAIL、TDZ2、status1各source层保留，不能合成latestwholefile绿。Nodegroupgone、无PG/Go/build/browser。额外原独审续审由root派，未交最终前不伪标其PASS。

## 下一独占工单与依赖

1. **RES-CONTRACT-CURRENT-ENTRY-01**：TS13files真实typed修复与三个mapping→sole affected编译/static/realchecker；不得放宽OAS required/nullable边界。
2. **RES-STATS-AUDITENTRY-PROJECTION-01**：创建新明确owner（当前capacity分支blocked）→真实[]AuditEntry DTO安全投影→新target/actual gate；不是已实现。
3. **RES-TX-CURRENT-INVENTORY/CI-FRESH**：current18生产family分类/freshAST/合法coverage与稳定source完整formalCI、currentsecure/cold/browser/P6 PGSMTP各真实门禁；source capture获授不等wholeGo0。原10/171与全部未闭验收scope保留。

[本次四原档交付](evidence/R5-REGISTRY-COMPOSED-ALIAS-UI-DELIVERY-20261002.json)均沿sole原archive digest，不全树hash；旧历史没有改写。
