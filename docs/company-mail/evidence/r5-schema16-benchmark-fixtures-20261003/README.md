# 本批 benchmark synthetic fixture 修复附录

固定 source `aeed47f0e1998d9925acb680992b051463821d69`。只修改 `scripts/tests/test_r5_benchmark.py` 及本新证据目录。原 `.agents/skills` 不存在，工作区 `.agents` 为空；web/AGENTS.md 已读取，本批没有 web 修改。

## 诊断与修复

生产 `expected_schema_version()` 从实际 migration 最大版本取得 **19**。旧 preparation validator 在 `run_r5_benchmark.py:390` 先验证迁移来源，再在 `:391` 要求 exact schema16。旧测试却把动态最大版本放入 schema16 的 result、checkpoint、plan，26 个 preparation 测试在 setUp 提前失败；lookahead 10 个也因 schema19 plan/checkpoint 与 schema16 result 不符而失败。没有生产算法 bug 证据，生产拒绝正确。

把 synthetic JSON metadata 明确固定16；仅两个 grammar 测试类用可清理的 source-schema mock 模拟 schema16 来源，生产 validator 完整实跑，current source/registry/CLI 类不套用该 mock。嵌套手工 fixture 注册清理。新增具名正例、schema18/19 重标记拒绝、旧fixture对当前source拒绝、checkpoint 精确错误回归，保证负例不因 setUp 提前拒绝而假绿。

另外两个 mocked CLI fixture依赖真实 /tmp 容量；本环境 /tmp 为8.8GiB，S preflight要求10GiB预算+10GiB余量，故在抵达目标dispatch/ambient-method边界之前被拒绝。仅这两处模拟30GiB空闲，真实preflight仍执行；新增低于边界一字节拒绝、边界通过回归。未弱化预算或覆盖 validator。未删除原106个测试。

## 执行状态

初始完整文件：FAIL，106 tests，1 failure / 36 errors，原日志保留。中间两轮 FAIL 原日志也保留。最终 `final2-test-r5-benchmark.log`：**PASS 111/111**，无skip。相邻契约 current18 benchmark 21、archive boundary15、CI wiring5、source-version runner4 全PASS；最终共156个测试。运行命令是 workspace venv 的 `python -B -m unittest discover -s scripts/tests -p <对应文件> -v`。官方PyPI按原requirements-contract锁安装，记录安装日志，不改锁与系统权限。

读取 e5e23e 的 batch4整合说明；没有重跑全套集成，不把本批有限PASS覆盖整合剩余错误。本次没有 Go/PG/S/M/L性能执行、历史 evidence重写或benchmark成功宣称。其他并行任务的 descriptor、transaction inventory、中央TODO、锁与生产业务均不动。

GitHub API两项读取返回Forbidden（receipt含精确错误），停止后续API动作，不能读取新CI日志或创建draft PR；普通origin fetch成功，git推送另作独立核实。未merge/deploy。

## TODO（仅本批附录）

- PASS：synthetic schema16 fixture与具名回归；本文件与相邻契约完整执行。
- NOTRUN：新CI与完整集成、真实benchmark运行，不据本批结果升级产品资格。
- BLOCKED：draft PR创建，GitHub API Forbidden；已准备本目录PR-BODY.md供父任务恢复授权后使用。

详情、失败IDs、各轮计数见 receipt.json，payload字节哈希见 payload-sha256.json。
