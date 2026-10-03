# PG并行子层、Legacy/TSC与诊断/shipping准入

**首4与新增15各自限定通过，不是同源全19或180秒整个PG包通过。** [交付](evidence/R5-PG-PARALLEL-LEGACY-MCAUSE-DELIVERY-20261002.json)保三个原runtime安全档、两份真实落盘独审报告及MCAUSE独立SOURCE/TEST层。原dfb整个backend失败/206未start、后续源码与原M/schema16各分层，未重复测试或全树hash，无发布。

## E-PG4 → F：真实两lane重叠与四白名单子例通过

source `74f627f721decd575bf752846b839a23fc9f5a51` / closure `0f92fbe64e997d667d3c0bcceabdfb047b2312390247765b6131aef104a5f894`（canonical source，不Git commit），[原review](evidence/R5-PG-LIFECYCLE-FOUR-TWO-LANE-REVIEW.json)、[40原payload](evidence/R5-PG-LIFECYCLE-FOUR-TWO-LANE-LOGS.tar.gz)、[真实独审报告](evidence/R5-PG-LIFECYCLE-FOUR-INDEPENDENT-REVIEW.json)。

- pure新4top8leaf9race events PASS，是admission/lifecycle mechanics及fake Drop，不真实invalidDSN/CREATE/DROP/Fatal全故障链。
- real原4top/11leaf/15race PASS、0skip，GOMAXPROCS2与原PGpackage180不变。真实pause/cont/acquire/release max2 overlap，结束permits0；非独立第三acquire且test.parallel>2压力认证。
- 11distinct DB全部观察fresh catalog→实际schema18/users3/**mailboxes至少2**→terminal不存在；不能把原metadata mailbox2解释成严格exact2。原actual_overlap_parser_pending继承字段已由独审raw重建完成，另解释不改raw。
- ownedPG25043 stop0/PID/socketgone/testDB空/conn0、所有groupsgone；原300s窗含capture/pure/init/real/observe/cleanup32.459019s。此前M私留失败root未触。

## E-PG15 → F：新增白名单真实执行，不复旧4

source `18bde9aab572886cfa95c472377c15b25f5e1792` / closure `a5049ce041a384698977a9411be05f2c3045585b3ae8dde55e5621006c290877`，[原review](evidence/R5-PG-PARALLEL-ADDED15-REVIEW.json)、[35原payload](evidence/R5-PG-PARALLEL-ADDED15-LOGS.tar.gz)、[真实独审报告](evidence/R5-PG-PARALLEL-ADDED15-INDEPENDENT-REVIEW.json)。新增real15top67child73leaf82race PASS/CLI0/0skip，pkg21.722s，加pure ExactWhitelist1PASS；旧4/旧其他pure未复。

15次pause/cont/acquire/release，max2 overlap/end0。73 DB catalog存在后terminal全消失；**直接schema18完整探针仅15/73，其余58短fixture probe gap保**，不得用执行成功/静态73声明冒73次实际目录观测。原New/Migrate完整seed/断言没缩水。GC额外dedicated session源码保，GC原NoSession断言通过，非独立每连接采样证明。PG42028stop0/PID/socketgone/DB0conn0/groupsgone；完整原600s窗35.470197s。原command.outer300_origin等残名不改变raw origin→deadline真实600，另解释不改原字段。

两窗的资源事实仅限实际scope：cached pgx5.10 default D16、business10、goose3.27.3局部provider/sessionLocker，sql.DB MaxOpenConns0默认不限、MaxIdle2；PG100-reserved3，GC dedicated额外1、这次其他packages0。采样fixture峰值8是下界，首4非observer全部client峰值10亦下界；2×(16+10+16)=84仅配置pgx pools，不包含sql.DB/observer/其他包，**不是7×2或任何总连接硬界**。不以低峰值推跨package正式预算一定适合。

**已推进**：局部lifecycle/admission白名单与两lane实际门禁通过。**未关闭**：整个backend/PG180包fit、应跑全集及旧206尾部、当前默认跨包并行；不拼19同源whole绿。

## E-LEGACY-RC3 → F：新source全部current web TSC0，only三known leaf通过

source `ca194a22eb7002755bb524459f37241b17a90687` / closure `3e06cac7ce14d45c5680ee79c79168ca8f1923b40fb4981fbb7fb3c0476a60eb`，[原review](evidence/R5-LEGACY-RC3-FRESH-GO-TSC-REVIEW.json)/[28原payload](evidence/R5-LEGACY-RC3-FRESH-GO-TSC-LOGS.tar.gz)。新signed8cb及六shared pins保护、tsconfig不排除；source-bound Go fake/shippingProject producer1top3leaf4race PASS/CLI0先行，fresh observations51e48d…与CASEf46d unchanged，显式env传Node，beforeAll自动Go禁止。

官方private Next typegen0→**全该current web tsc0**，原两旧protocol类型错误在此新source闭合→仅RC03/RC04/RC05 known三leaf PASS/CLI0/0skip。没有复旧82/53/TDZ/status/false2，不把known冒unknown，也不把按钮可见当真实retryHTTP成功。原600窗capture/Go/typegen/tsc/Node/cleanup9.445788s/groupsgone，无PG/HTTP鉴权/browser/production build。原27diag/tsc2/dfb backendFAIL仍按其历史source保，不改字典。

可关闭 `RES-TS-LEGACY-PROTOCOL-FIXTURE-01` 的最小fixture适配及fresh typegen/tsc子scope；不关闭whole frontend测试/currentUI权限/真实shipping链或G0。

## E-MCAUSE → F：SOURCE及cached4/fresh1通过；4000执行协议尚缺

[原七file capsule](evidence/R5-MCAUSE-SOURCE-CAPSULE.json)/[原13regular source包](evidence/R5-MCAUSE-SOURCE-CAPSULE.tar.gz)只限定owned source delta，不原M private整体src；没有提供原source包digest，未伪填或再hash。原capsule状态字段保作者冻结时SOURCE待审含义，**后续独审SOURCE_BOUNDED_PASS另登记**：[准确分层摘要](evidence/R5-MCAUSE-SOURCE-AND-PROTOCOL-STATUS-20261002.json)，明确不是伪造机器runtime。

原实际测试六raw file直接包装[原TEST日志](evidence/R5-MCAUSE-NEWTESTS-RAW.tar.gz)，两个exit0，default1+observer4 PASS/无fail/skip/stderr；其中四fileobj为cached，仅Postgres `TestR5MCauseExactInterval` fresh2.162s，**不写五项fresh**。SOURCE before/after/current七pins等，正常build constant nil无clock/ctx lookup，FS顺序/次数/原错误优先级/durability不改；observer量FS call wall而非设备I/O，并发/嵌套累计不可当独立stage wall。

**MCAUSE4000_EXECUTION_PROTOCOL_PENDING，未运行**。唯一可只读定位schema16 base `/private/tmp/tm-r5M-final.TdhqAxe8/src` 原852900/d286；尚无七file注入新身份、具体controller/guard/freeze可审安全命令，禁止把当前schema18直接套schema16诊断或用generic runner补准入。需outer init前起300s含native停止、execution180s含drain、data/WAL/log+objects/profiles合计1GiB实际外层guard；ctx/Go sampler不足。最终actual raw须外验generated/emitted/FIFO4000、buffer≤100、joined0。fresh4000也不能解释原753k深度主因/有效M；原M10800s failed与部分人口保。

## E-SHIPPING → F：只有会话工具输出，环境未准备，不存在新日志包

[transcript-only状态摘要](evidence/R5-SHIPPING-ENVIRONMENT-TRANSCRIPT-STATUS-20261002.json)明确不是原raw receipt/archive。shipping只读agent在2026-10-02 13:46:22Z–13:50:51Z观察：CLI28.5.1/Compose2.40.0可用，Docker Desktop4.48.0 build207573**已安装**且com.docker.backend文件存在；ps仅vmnetd helper无backend，显式本地desktop-linux unix socket version/info exit1 Cannot connect。未探测远端/启动daemon。

公开settings-store.json文件429B存在，但open阻塞后中断，**没有读出内容**；host_networking_enabled/ECI均unknown，不记false。主树Node模块Next16.2.1与lock16.3.3在此时点不一致；executor私有Next16.3.3 typegen证据另源，不冒主树依赖齐。PATH Go1.21.13/go.mod1.25.7，自动解析GOSUMDB=off失败，executor自有工具链另论。

正式source wiring见`.github/workflows/company-p0.yml:136-218`、`company_browser_test.go:31-109`、`scripts/browser_company.cjs`及required browser清单；browser不许skip。`browser_r5_permission_consumer.cjs`是native standalone/synthetic wire，不Go/PG/SMTP/Docker shipping。登记source_wiring_present=true、environment_ready=false、shipping_executed=false、browser_shipping_passed=false，只代表上述snapshot，不臆测之后人工准备结果。

恢复依赖：人工启动/确认Desktop host networking及ECI→sole独占原127.0.0.1:18080/3000双向合成probe→原shipping workflow真实命令。作者所查[官方host network文档](https://docs.docker.com/engine/network/drivers/host/)说明Desktop≥4.34 opt-in及ECI不兼容；[官方settings文档](https://docs.docker.com/desktop/settings-and-maintenance/settings/)给公开配置/UI入口。没有新落盘inventory日志，不能复制会话当raw；本机platform结果也不等Ubuntu/GitHub四job。

## 当前门禁与下一独占工单

1. **RES-CI-CURRENT-FRESH-01**：sole已获新27633d96 source closure、873declared/872required/51pkg，正式180/package与20min总预算不变；**仅已授权/等待实际结果**，不预填PASS，也不把旧dfb206gap照搬成新source实际缺项。
2. **RES-MCAUSE-EXECUTION-PROTOCOL-01**：原M16私有base+七file注入新freeze/独审+外层1GiB/300/180guard与完整4000观察；没有协议不能启动，未M retry。
3. **RES-SHIPPING-ENV-01 / RES-HTTP-FRESH-CAPTURE-01**：环境准备真实receipt与fresh正式capture/specHash门禁，各owned sole实际验，不拿cached测试、readonly81 replay或native synthetic browser代shipping。

ABC/recipients原新独审已接受Go73/旧81×newdoc readonly限定层，原captureCLI1、embedded旧spec与新doc来源保；不另跑验证。原171完整范围、10已勾与M有效baseline/P0/G0/完整CI仍未闭。

## 后续whole实际已失败：执行GOMAXPROCS偏离须具名保留

上节“获授等待”仅当时时点；sole随后真实终态source `27633d96f6bc93d226ce158d71dc215b19ae429a` / closure `ce40c0979d2a93deae5381b4f169a07bb736f84a09d2d02521c4574af7fcff17`，[derived失败review](evidence/R5-CURRENT18-WHOLE27633-FAILED-REVIEW.json)、[53原payload](evidence/R5-CURRENT18-WHOLE27633-FAILED-LOGS.tar.gz)、[原delivery](evidence/R5-CURRENT18-WHOLE27633-FAILED-DELIVERY.json)。作者archive SHA `e3f6b9d578d303c253a3c58518e19bde7e8f5b0e2ad9649e88984bff76f8c367`，没有改旧raw来产假PASS。

- actual Go1，7395run/7373PASS/1FAIL/唯一合法BrowserSkip1/20unfinished；873declared/872required/51packages、201top未run，是本source新实际，不照搬旧dfb206。
- 唯一测试FAIL `TestR5LegacyContentKeyHTTPPreservesSafeReceipt` line233；PG package180.847s累计超时，19白名单已PAUSE但0CONT，尾`ExpiryAfterMailboxWait`当时仅约1s。不能称双lane实际执行或预算改进已认证，也不由这个瞬间判该测试死锁。
- **execution-contract deviation：controller actual GOMAXPROCS=1，批准值为2**。新whole这次不满足已授双lane执行资格；真实Go1/timeout/partial与deviation一并保留，不洗成仅infra、也不据此证明合规GOMAX2的180包一定失败/通过。需owner修capture/controller contract并按PM新裁决才可重做，原180/package/1200总不自动延期。
- job342.683s≤1200、nativePG58695 stop、全部ownedgroupsgone；私有stoppedroot残tm_test62338…观察0conn保留，不DB0。两个classifier1，原source与当前未完成门禁保。

因此新增 **RES-WHOLE-EXECUTION-CONTRACT-01**（freeze真实GOMAX2 argv/env与controller→独立准入→PM裁决下一正式whole）及 **RES-LEGACY-CONTENT-KEY-HTTP-01**（仅该失败leaf沿实际metadata安全契约核fixture/handler，不削弱访问策略）。本次是失败执行收据，未交新的whole独审PASS，不关闭原171/CI/P0/G0/M门禁。

**随后全新whole失败独审已接受 bounded failed attempt，而非PASS：** [准确独审交付摘要](evidence/R5-CURRENT18-WHOLE27633-INDEPENDENT-FAILED-ACCEPTANCE.json)明确是中央摘要非伪raw；reader独算30290events与上述计数、733 manifest closure/失败source、archive53members及cleanup一致。唯一FAIL为HTTP200缺旧fixture期望的private-body marker，不据此直接判生产泄露/拒读漏洞。current872 missing/notpass388、original247为180，是分类器覆盖缺口不是实际FAIL数；二classifier exit1保。actualcommand/controller均GOMAX1且批准2，deviation原证据保持，不能以新独审boundedfailed接受冒双lane认证。
