# R5 原规模性能数据集与基准准备（P0-100）

**尚未执行S/M/L；P0-100不完成。** 依赖P0-090的正式验收；工具校验、Go编译、随机种子小验证不算原规模性能数据。运行registry初始为空，禁止写入假S/M通过。

## 原契约，不允许降规模挑基线

| 规模 | 员工 | 邮箱 | 邮件 | 执行预算提案 |
|---|---:|---:|---:|---|
| S | 100 | 500 | 100,000 | 30min，磁盘10GiB预算 |
| M | 1,000 | 5,000 | 1,000,000 | 180min，磁盘80GiB预算 |
| L | 1,000 | 5,000 | 10,000,000 | 本批不执行；12h/450GiB扩展演练预算，发布必要性须P0独立冻结 |

原DESIGN139–141：20并发，S/M常规列表p95候选≤300/500ms，index-ready M正文查询p95≤1s；相同数据/安全条件关键路径相对基线不得无解释恶化>10%。这些是候选，需实测冻结。附件字节下载与公网SMTP不套列表阈值；未达阈值必须保留原值和调整理由，不能改脚本或小数据冒通过。

固定seed=3893945；2tenant按80/20分布，每员工1personal+4shared，总邮箱数严格匹配；每shared4个合法grant。raw sizes为80%4KiB/18%32KiB/1.9%256KiB/0.1%2MiB的固定随机分布，消息含唯一ordinal确保不是一份小blob冒百万条。80%inbox/10%archived/5%trash已到purge/5%shared硬过期；个人永久的历史期限不能被错误重新解释。详见 [datasets](evidence/R5-BENCHMARK-DATASETS.json)。

## 已落代码，未运行原规模

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

当前7项工具测试及纯generator实际通过，编译通过；尚未有actual SQL校准或任何S/M性能成绩。完整运行只能由integration operator在独立窗口安排：

```sh
# 仅在owner预先批准预算、私有owned DSN已提供、无其它suite占PG时
python3 -B scripts/run_r5_benchmark.py --scale S --output-dir "$FRESH_EVIDENCE" --source-sha "$FROZEN_SOURCE" --execute-approved-budget
```

S/M必须各至少一轮实际完整baseline、original raw JSONL/exit和result统计；L不执行是本批预算选择，不取消10M规格或后续必要性裁决。timeouts/OOM/磁盘预算/SQL未知/冷热点缺项均失败，保原输出，不靠降scale重命名成功。

## 资源与剩余验收

只读本机：Mac15,8、Apple M3 Max、16physical/logical CPU、64GiB、macOS26.6.2；取样时/tmp卷约776GiB可用，执行前须fresh重测。建议RSS上限16GiB、S/M磁盘安全余量≥10GiB，L≥200GiB；L平均raw估计151GiB，加DB/index可能到450GiB，不宜与其它fullsuite并行。

独立执行还需：预flight冻结硬件/seed/source/schema/规格hash；初始测量定位与SQL tracer开销校准；正常/错误安全断言不因性能被禁用；S/M原raw与候选阈值裁决；reference基线后P10同条件复测。source变化必须新run，不用当前parser2个microbench或工具小样本代替。 [runs registry](evidence/R5-BENCHMARK-RUNS.json) 保持空直到真实run归档。

## Pre-run真值纠正与工具样本（不算S/M）

初版按ordinal给life但只在shared上执行5%expiry，导致声明80/10/5/5与真实生成约81/10/5/4不一致；参数hash也不能叫actualdataset fingerprint。旧SQL20只证明instrument能数20，不证明这个错误dataset合同。原日志保留，真实原S/M还没跑。

新版合同冻结：每20条exact16/2/1/1，expiry槽只选shared；tenant80/20按整life桶blocks分配，避免所有expiry都落foreign；每1000 size桶exact800/180/19/1，seed仅shuffle次序。必须实际SQL核对employees/mailboxes/personal/shared、四distinct legal grants、tenant/life/size/index-ready，流式canonical actual行计算datasetSHA；parameterSHA单列不可混。计划模型计数或随机生成器纯样本不替SQL观测。

工具真实小校准为 **20员工/100邮箱/1000邮件**，保证foreign4人可合法four grants；明确`TOOL_ONLY_DATASET_CALIBRATION`，不写S/M成功registry、不改原规模。观察raw filesystem+parsed index、shipping API、actual SQLcounts及capturedquery同bound parameters真实EXPLAIN JSON。source/schema/scale/index-ready、querySHA/parametersSHA记录，无credential/raw params输出。

metric边界：GC workload只读真实raw references，不声称sweep/delete/fairness吞吐；SQL只应用/worker池可见query（不计DB trigger内部statement）；memory是Goalloc+本进程high-water RSS，不声称测了PG/Redis全系统。未测system memory显式`not_measured`；getrusage失败/RSS未知与walk错误不可以0冒成功。缓存只受控app/ownedRedis reset/reuse，DB/OS完全cold未证明。

L具体裁决：P0原验收仅强制S/M各实际一轮；此批冻结10M规格和12h/450GiB候选预算而不执行，也不宣称支持10M。后续P10/最终发布如果声明10M容量，必须执行L并保真实结果，不能援引该预算字段或S/M外推。该裁决不降低S/M人口、20并发、size/ACL/原候选阈值。

Go实现与runner/tests分别由integration operator及独立reviewer独占，三份metadata由protocol owner持有；下次fresh小probe后才锁一致的field/schema/source契约。当前registry仍为空，SQL20与1000生成器/小dataset校准都不是S/M成绩。
