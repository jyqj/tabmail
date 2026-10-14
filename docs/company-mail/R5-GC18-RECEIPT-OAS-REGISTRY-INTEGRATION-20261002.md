# GC18、普通receipt/OAS与registry追加证据

**仅新增实际有界层，不把分源局部绿拼成当前全套绿。** [安全交付清单](evidence/R5-GC18-RECEIPT-OAS-REGISTRY-DELIVERY-20261002.json)登记七个原档；原M失败、三S、旧catalog和初次失败日志保持原字节。canonical source标签不是Git commit。无产品测试或每文件哈希重跑，无发布。

## Evidence → Finding → Path

| 实际证据与路径 | 验收层级与边界 |
|---|---|
| [GC18 raw review](evidence/R5-GC18-FAIRNESS-REVIEW.json)、[39原payload](evidence/R5-GC18-FAIRNESS-LOGS.tar.gz)，source `cb03f7a27e9e9cf45fd815c141ad2cb645e08134` / closure `2c3ea72f90f201c7282f212e26a44e4188cdd084e9db1f56d00ec6f4a60ea9cc` | 六GC签文件+三current-head签文件；实际race 15top/21leaf/24test-subtest PASS、0fail/skip/missing，pkg22.846s，原600s窗含初始化/清理36.043427s。公平持久cursor、21+tenant、失败/空批、持续写、实际OS重启与双process等bounded通过；稳定eligible集合完整轮数ceil(N/20)，中断仅推进尝试前缀。无限新tenant无固定轮数；busy跳过不等GC执行，候选20/100不是物理SQL扫描上限。 |
| [新实际18 catalog](evidence/R5-TRANSACTION-DB-CATALOG-SCHEMA18-ACTUAL-20261002.json) | 正式Migrate/官方O_EXCL实采196143B，作者SHA `02a5850d03a8909ee95720e56d755128b4e6f923ab43cbeffb35fc0de1f14f94`；Goose18及18migration hashes、restart一致，51tables/507columns/185constraints/75FK/154indexes/11triggers/11functions/3sequences。旧1a15/schema17及15/16catalog不改，不静态升version。 |
| [GC18 metadata独立纠正](evidence/R5-GC18-SOURCE-RECEIPT-METADATA-CORRECTION.json) | 原receipt继承test-only=true不准确：7ffe→cb03含company_ops、scheduler、migration18三production delta及tests。原raw/tar不改，正式以signed GC18六+三source delta解释，不回贴旧7ffe PASS。 |
| [helper-entry affected raw](evidence/R5-GC-HELPER-ENTRY-AFFECTED-REVIEW.json)、[69原payload](evidence/R5-GC-HELPER-ENTRY-AFFECTED-LOGS.tar.gz)，source `9f58e14c09e3dcf5ea334db93b0cde13bb5db3eb` / closure `4be2c8b152b1e20a90c518016cb581df418ddc276a1a3ebe1465d90bf88786b4` | 仅两test-entry变更、产品/migration同cb03，实际race4top/9leaf/11PASS，0skip/fail，pkg14.302s，只有一次main目标run；未复旧GC12/三head全套。首次controller stalehead_packet NameError=0产品tests，PG69781已清；修controller后PG70212实际目标通过，原infra失败保留。 |
| [helper时间独立纠正](evidence/R5-GC-HELPER-ENTRY-TIMING-CORRECTION.json) | 原137.106437s依据清理前receipt.utc，不能称完整最后清理墙钟；原source-postcheck给137.262806s，原origin+outer终点给137.263358s，两后置观测均严格原600内，无延期。原raw/local clocks/tar不改。 |
| [A初whole失败](evidence/R5-RECEIPT-A-CONSUMER9-FAILED-REVIEW.json)、[A六fixture修复](evidence/R5-RECEIPT-A-FIXTURE6-REVIEW.json) | 初8aea/ce502实际24top/81leaf有6failedleaf、whole CLI1保：五PG/HTTP错误fixture及一consumer不确定扫描fixture。新f759/95be只有两signed testdelta、production bytes未变，exact3top/6leaf/8events raceCLI0；不称业务fix、未复其他初绿，不latestwhole81exit0。 |
| [ordinary OAS typed/wire raw](evidence/R5-ORDINARY-OAS-TYPED-WIRE-REVIEW.json) | source8bff/879af签三file，Go exporter1top race0导21typed与shipping-envelope，Python8PASS0，fresh shipping wire目标真执行0skip。wire18228B、作者SHA9fdd0d73…；capabilities构造，不实际PG/HTTP身份授权；不是generic checker/fullOAS/全字段caller coverage绿。ordinary/inspection/currentcontent例外不互相替代。 |
| [receipt status唯一新leaf](evidence/R5-RECEIPT-STATUS1-REVIEW.json) | source66618/d819，三pins bdd7/3248/a8aa保护；only1PASS/CLI0/1.350284s，另36filtered未执行。actual交付可信，额外独审暂容量blocked不伪补PASS；falseContent新2targets仍prepared未执行。 |
| [registry34初失败](evidence/R5-REGISTRY34-FAILED-REVIEW.json)、[17原payload](evidence/R5-REGISTRY34-FAILED-LOGS.tar.gz) | 实际requested34中13exec=12PASS+1ContractERROR，Registry21 classsetup未exec；realCLI1原强等elapsed拒。S3d row1518.898014/4074 row1377.669084各来自result execution wall，原GoTest1528.68/1386.62含不同setup/cleanup边界，不改旧摘要讨绿。27fe12是本registry失败工具档，M原档始终0ab842，不存在M repack关系。 |

新全独立GC审已核actual raw/pins/两定点archive与catalog；执行过的wire lost C/Z、process/restart/double/lock失败以已签断言+实际PASS为证，成功child stdout/逐帧trace没独立落盘，不写PCAP证明。GC18/helper均有testDB空、native stop0、ownedgroup/PID/socket absent；**无独立停库前connection count=0查询，不写其为观测值**。postgres.log/src在private rawroot，不随公开安全档上传。

## Registry修复是SOURCE准入，不是运行通过

额外明确授权仅runner/test两file；基于原d947/872560字节备份，补package PASS.Elapsed unique/finite/nonbool/预算与S2 review两个elapsed分别原raw绑定，typedM十三机器摘要字段逐项绑定可信-15/no terminal/incomplete raw、optional task_complete/product_green真值拒绝。原三方法各加一mutation组，方法数及25selector不变；最后runner `cf156af977167d9b4ece53b61e48c7e941e6ccbeff5c0279e247d7f0844e1d02`、tests `79b3d443a04198b27a44dde978bbd4742c3c8145745d05ef40d6a93c7f04d482`，ASTparse/diffcheck0，原全新独审tiny SOURCE PASS。freeze `/tmp/tabmail-registry-proof-repair-final/source-freeze.json`，两file已停写release。sole原25+realCLI尚待实际receipt，不复旧12PASS，不写registry green。原METHOD/rows/reviews/raw没有改。

## 具名剩余与下一独占工单

1. **RES-REGISTRY-CLOCK-TYPED-M-01**：新准入source首次25+CLI已实际尝试，但capture遗漏真实declared corrections dependency，见下节；修输入closure后只新23affected+CLI，不复两PASS或原12PASS。不旧clock改值，不fake M result/Go terminal。原M10800.004052超时、753380stored/753360ready/20processing/0workload/partial私留保持failed attempt。
2. **RES-TX-CURRENT-INVENTORY-01**：实际18catalog已取得，仍需新Create/B5/GC18等生产family owner分类、fresh AST与合法派生coverage、caller候选/trigger/FK关系；catalog结构存在不能泛清unknown，也不能用旧AST802/旧15coverage作当前全证明。
3. **RES-CONTRACT-CURRENT-ENTRY-01**：ordinary schema→正式operation/源别名精确映射、独占alias checker新target及当前generic contract gate；既有132与SSE/inspection不被普通receipt覆盖，typed fixture不代PGHTTP权限。

P3-080技术专项已显著推进，但原依赖P3-070仍缺P3-050/060全引用清点/建立竞争及物理扫描界；叶级依赖裁决单列，不凭15/24数字勾父。原171范围、10个已勾与fresh fullCI/G0、当前cold/shipping/browser、真实PG/SMTP P6链仍未闭；原full2aa9、Node whole54、registry/A初FAIL与各新partial PASS分层保留。

## 第一次新registry25窗口：输入closure遗漏，不洗为产品RED或绿

sole `/private/tmp/tm-r5registry25.yfe_9fyz` source9b777bea/cdc39ed：[实际review](evidence/R5-REGISTRY25-INPUT-INCOMPLETE-REVIEW.json)、[原安全档](evidence/R5-REGISTRY25-INPUT-INCOMPLETE-LOGS.tar.gz)，作者SHA `e648710788b56ee4748c2e210f832663b6187226686dde78b4472dfb767a93de`。实际4exec=Clock/Mversion2PASS、typedMpositive1ERROR+Contract1ERROR，Registry21 setUpERROR未exec，CLI1 missing evidence，ownedgroupsgone。28-ref collector没纳row真实 `readonly_reporting_corrections`，不改fixture预期讨绿，不声称runner修复被产品反例推翻。

原corrections文件确实存在，6252B：[单缺dep receipt](evidence/R5-REGISTRY-CORRECTIONS-DEPENDENCY-RECEIPT-20261002.json)，本次明确请求仅该file一次有界SHA `d98be876f27193ebc42a0f0cf8faa140b283ea52b58578c20290a893f2d44d0a`。它绑定source852900、原M review与`R5-B01-AJ-M-ABSOLUTE-TIMEOUT-LOGS.tar.gz:M/go.jsonl`七paired原Output统计；不是给旧review补字段，原bytes未动。后续只补declared-key allowlist capture manifest与new capture source/argv，root准仅typedMpositive1+Contract1+Registry21=23affected及CLI；原两PASS和原12PASS不复，无M/GoPG重跑。未新实际receipt前registry仍未通过。
