# R5 普通回执 / inspection 测试门禁修复

基于首批 integration `eb366eecb6413f5aa025afb4ee63bd1eea7fdf7d`，其组合被测 source 为 `4016720586be817e5f1edcf0e45ef7d0cccc605a`。仅修改 `scripts/tests/test_outbound_inspection_contract.py` 与本专属 checkpoint/证据；生产 OpenAPI、DTO、handler、store、依赖锁及中央 TODO 未修改。

## 合同与旧失败

R5-DESIGN §5.2/§5.3、当前 `OutboundReceipt` DTO、普通 receipt tests 与正式 operation 表明 `OutboundJob` / `Submission` 为最小 metadata receipt 别名。旧断言直接取 `properties` 的 KeyError 在修改前再次复现：[before.stderr](evidence/R5-OUTBOUND-INSPECTION-GATE-20261003/before.stderr)。旧基线的“不把普通回执扩成 inspection”意图保留；其 raw job 字段清单不能作为当前允许清单。`CONTENT-BOUNDARIES.md` 的“现有回执仍含主题、收件人”属于旧阶段描述，与当前 R5 投影不一致，本块只记录，不改共享文档。`CompanyRecipient` 旧字段/状态基线继续单独检查，但不将其视为普通回执依赖。

专属 helper 只展开明确的本地 component 引用，检查普通引用闭包的对象关闭和禁用字段；未知 component、非本地/错误 namespace/子路径引用、循环、ref sibling 以及未知 schema keyword 均明确失败。普通两别名与 canonical receipt 一致；闭包四种 DTO 字段/required 对照当前 Go tags。保留 inspection 独占 route、superadmin/reason/audit、header allowlist 及清理后的诊断断言。

三项独立新负例覆盖所有普通对象禁用字段污染/重新开放、两别名或嵌套属性指向 inspection/legacy recipient、别名单独扩字段，以及未知引用/直接和间接循环/unsupported keyword。负例在未改 schema 正控后深复制 mutation，不能通过放宽生产 schema 或 catch KeyError 获得绿。

## 本次实际验证

[完整回执与命令](evidence/R5-OUTBOUND-INSPECTION-GATE-20261003/receipt.json)，[十 suite raw](evidence/R5-OUTBOUND-INSPECTION-GATE-20261003/contracts.stderr)，[原锁依赖 freeze](evidence/R5-OUTBOUND-INSPECTION-GATE-20261003/deps-freeze.txt)。临时 venv 原样安装 `scripts/requirements-contract.txt`，包含 date-time validator，无版本替换或跳过依赖。

- 原 integration 十个 pure suite 全部重跑：227 tests，226 PASS、1 明确 fresh typed fixture SKIP、0 FAIL/ERROR，6.186s。旧 217 数量因 HTTP setUpClass 缺依赖漏执行 7 项，本次另新增 3 项；没有删旧测试。inspection 本次 13 PASS，HTTP 45 PASS。
- 随后现有 Go exporter 新执行 1 PASS，产生 21 个自有 synthetic typed projection/shipping envelope fixture；普通 receipt suite 再跑 8 PASS、0 skip，fresh wire test 实际执行。[Go raw](evidence/R5-OUTBOUND-INSPECTION-GATE-20261003/typed-go.stdout)、[Python raw](evidence/R5-OUTBOUND-INSPECTION-GATE-20261003/typed-python.stderr)。两轮结果分开，不称首轮 227 零 skip。Go package 0.005s 不是独立编译时间；使用现有缓存与 readonly module，非 race。
- `git diff --check` 通过。无 PG/真实邮箱/公网 SMTP、whole backend、browser、四 CI、merge/deploy。

## 父残项映射

`RES-CONTRACT-OUTBOUND-ALIAS-STALE-01` 本次提供门禁修复与新实际证据，交父审查/整合；本块不自行修改中央 TODO 或关闭父门。`RES-CONTRACT-CURRENT-ENTRY-01` 的 generic gate/完整 caller/PGHTTP 授权等依赖不由本测试覆盖。旧 integration 的 217 首轮依赖错误、KeyError 及仅 HTTP45 补跑事实保留：[原 receipt](evidence/R5-INTEGRATION-20261003/receipt.json)。**10/171 保持**；本块完成即停。
