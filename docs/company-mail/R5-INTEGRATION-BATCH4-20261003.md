# R5 第四批实际组合回执

组合源码冻结 `aeed47f0e1998d9925acb680992b051463821d69`，分支 `integration/company-mail-r5-batch4-20261003`，[draft PR #23](https://github.com/jyqj/tabmail/pull/23) 精确 base 为 batch3 `5ae68c2d037603cd0151e1b9188f2df4db1e45a1`。从固定 PR21 `41b015c` 起无冲突合并 archive66eb5e8、scanner d9713c1、alias139b701、browser1be7c84、depsd6640db、linte044808；随后明确授权纳入 SMTP tests-only c083d05 和 router-touch tests-only989171d。保 ancestry 去重，不合209d stablefix，不修改fork生产或PG性能实现。各中间checkpoint/失败/receipt均保留在本批raw evidence，未转写为最新source成功。

## 最新 source 实际阶段

| 阶段 | 状态 | 证据与范围 |
|---|---|---|
| archive guard | PASS | 六module（root+两fork+三个档案）、三个archive marker、36history bytes精确，root ./...不缩小 |
| fresh v4/v2 | PASS | 新独立空cache/modulecache，default602pkg/589local、race+r5protocol604pkg/592local，64生产目录；各capture→warmvalidate；绑定两个新test实际hash |
| Go build / vet | PASS | Go1.25.7，实际GODEBUG=asynctimerchan=0，原mod/sum/两replace；私有cache |
| 两fork完整race | PASS | 原两个fork/...，SMTP16owner顶层方法通过；无测试的debugserver package skip为Go正常无测试package，非漏跑owner测试 |
| 新touch文件三项race | PASS | 独占PG17/loopback55443，含usage_held_allows_retry及四scenarios；不代整包性能 |
| root PG整包race /真实性gate | FAIL | 原180s/完整 ./...，API与postgres各包累计180s截止，有未完成mandatory tests；非单个busy-job等待180s。本批PG已停止 |
| Web test / type / lint / build | PASS | 最新source静稳39files/527tests全PASS、tsc0、lint0（4warnings）、build0；初轮521P/3F/3skip、后轮518P/9F timeout日志均保留。耗时原因不作无证因果结论 |
| Node validation / DTO / i18n | PASS | Node validation49/49，strict客户端134branches/7forwarders/127mapped，Go AST route132/negative drift tests，DTO16+36，i18n1365keys |
| 版本全量clean clone | FAIL | 631实际启动/9failure events/43error events/1skip/9未启动；历史baseline4/4PASS。完整报告保留 |
| 明确工具prepared版本全量 | FAIL | current640全部实际启动、10failure events/40errors/1skip；frozen4/4PASS；644/644无漏/无重。不是默认CI runner全绿 |
| compatibility/transactions历史gate | FAIL | 当前API/client source map另生成；原127-route compatibility/wire/source和transaction evidence漂移不伪重签 |
| npm audit | FAIL | 实际8high/0critical；omitdev6high；官方braces3.0.3无patch结论保留，原audit gate不关 |

## 工具准备与真实剩余错误

正常原pinned requirements全部安装到 `/workspace/r5-batch4/venv`，没有写 `/home/agent/.local` 或系统权限，也没有实际workspace安装审批拒绝。本地Node24.19.0/npm11.9.0、PG17与CI Node22/PG16分开。全部build/capture使用互不污染的私有cache；正式cold/warm没有其他build共用该cache。

现有runner无tool prep hook，未修改它/扫描门/白名单。另用原公开child接口在当前aeed与冻结PR21checkout执行prepared suites：官方锁TypeScript5.9.3 tar SHA512 integrity严格匹配，tar SHA256 `10e108c9cf7d5f2879053dff18515fb405abf2ccef63eaaf017d9c571687a1d3`、全部regular files包hash `157973716ed7eea8fef4816c93432cb12febb4c31b02360394d7767304bdb4e5`；无链接/Go/module/native文件，只materialize必要相对TypeScript包，未装整npm tree/生命周期/.bin。前后完整Go package/module/variants/all input fields逐项相等。短owned `/workspace/t4` 排除额外AF_UNIX长度错误；dirtycaller整npm tree拒绝测试仍实跑通过。

Prepared 40 errors严格分为：26个旧schema16 preparation pipeline合同漂移；10个实际migration schema与旧evidence漂移；1个既有/Users平台路径；1个历史compatibility缺/重route；1个历史wire spec/case不匹配；1个transaction structure/file-type coverage漂移。10failure events为旧ambient method opt-in负例1、旧excluded regular positive/open-guard1、descriptor regular-replacement8子例（同一方法）。这些拒绝预期未满足的raw output保留；独立8次workspace零字节unlink/touch观察dev/inode/type均复用，仅支持filesystem-sensitive线索，不是每个failure的因果证明或原子FS保证。当前新archive v4 byte boundary与v2 suite各新增项仍有实际通过；不因bounded PASS升级整体资格。唯一skip是缺fresh typed shipping-envelope fixture，未执行记NOTRUN，未补假fixture。

## 原字节、清单与独审

36受保护历史文件全部不变；528个基线evidence中526路径字节不动，两个明确授权的当前generated API-MATRIX/CLIENT-CALLS刷新，两个PR21原始文件另外原样保存为本批historical-*。人工六类route boundary说明按精确method/path保原字节。当前132route/134client逐项handler/middleware/OpenAPI/client/test/source closure见[当前source map](evidence/R5-INTEGRATION-BATCH4-20261003/current-api-client-source-map.json)；旧兼容wire/registry/schema19/history tars未改，不伪给新spec盖runtime章。

Archive独审引用固定[de4ae883 review](https://github.com/jyqj/tabmail/blob/de4ae883b33b5f903821502592f0568649d57bd2/docs/company-mail/R5-ARCHIVE-INDEPENDENT-REVIEW-20261003.md)，其bounded archive/static/selected/build结论与全量2F/40E/1skip/28未启动分别记录；没有倒退production或整合未知祖先。旧v1 helper与原四actual-root语义原封保留，旧冷roundtrip缺陷仍history；不合209d。generated/externalcache/native/compiler未知资格不升级。

## exact source真实CI

[run37151630259](https://github.com/jyqj/tabmail/actions/runs/37151630259) 对aeed source已终态FAIL：production-web PASS、browser-journey PASS、backend在root PG race FAIL、frontend在versioned source tests FAIL；各后续未执行步骤列入机器回执NOTRUN，不能以本地结果冒认Node22的type/test/lint/build。独立[source evidence run37151630289](https://github.com/jyqj/tabmail/actions/runs/37151630289) PASS。GitHub job log download曾transport closed，保该读取错误，不换身份/route；正常状态读取成功，确切job steps保存[CI回执](evidence/R5-INTEGRATION-BATCH4-20261003/exact-source-ci.json)。

所有阶段、失败IDs、工具pins、请求/实际执行IDs和完整raw日志见[机器回执](evidence/R5-INTEGRATION-BATCH4-20261003/receipt.json)、[payload hashes](evidence/R5-INTEGRATION-BATCH4-20261003/payload-sha256.json)、[执行证据包](evidence/R5-INTEGRATION-BATCH4-20261003/execution-evidence.tar.gz)。中间错误invocation对带npm caller的capture被guard拒绝（dirty-6a前缀），已用精确clean root新捕获，未擦失败或复用旧receipt。

**10/171保持，父任务不关闭，不称ready merge。** 没有merge/deploy/force、Method19/SML/performance扩张、真实邮件服务或被拒global安装重试。旧权限/失败原日志保留；新的workspace依赖安装与普通gitpush/连接GitHub draftPR动作分别记录。本文及最终中央TODO/evidence提交是source freeze之后的docs-only交付，不能把新docs HEAD的未观察CI当成已PASS。
