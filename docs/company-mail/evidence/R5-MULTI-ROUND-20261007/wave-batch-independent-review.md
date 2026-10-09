# R5 多轮实施证据账本独立只读审核

日期：2026-10-07

审核者：原生 subagent `/root/compose_ownership`

结论：**ACCEPT，无阻塞发现。**

本文件记录已经完成的审核结果。此次落盘没有重复审核命令或测试，只新增本 JSON/Markdown 证据对；根仓库未修改，本次不增加 TODO 完成数。

## 固定源码与审核范围

- 被审核账本提交：`9232dee62cf209eb67addc339ae9b053555addc7`
- 本批产品整合：`e32564304c0c84aa80b1184e72129fb0316d989d`
- 本批统一基点：`4065c4909c8f21a401a9a1af6370fa3f72670b99`

直接审核对象为 `docs/company-mail/evidence/R5-MULTI-ROUND-20261007/` 下的 `wave-batch-summary.json`、`evidence-files.json`、`README.md`，以及 `docs/company-mail/R5-TODO.md`。同时读取了清单中的原始证据、对应 validation 原件和 Git 对象关系。

## 31 份原始证据

| 核对内容 | 结果 |
|---|---:|
| 清单文件数／唯一相对路径 | 31／31 |
| SHA-256 正确 | 31／31 |
| 字节长度正确 | 31／31 |
| 对应 validation 原件存在 | 31／31 |
| 与原件逐字节相同 | 31／31 |
| 差异 | 0 |

清单之外只有目录 README 和清单自身。没有改写 ANSI、空白或换行来改变证据字节。

## 10 个实施项的来源映射

以下十项均已完成映射核对。每项均满足：local tree = remote tree = recorded tested tree；remote commit 是对应 merge commit 的 parent；merge commit 是产品整合 `e325643` 的祖先。完整 commit/tree 值保存在同名 JSON。

| 子项 ID | PR | 作者提交 | 发布提交 | 合并提交 |
|---|---:|---|---|---|
| R5-P6-090-20261007-01 | #61 | `7a4629c79310` | `54dd44534e8f` | `0c402faa74ff` |
| R5-P6-040-20261007-01 | #63 | `1386b38c9e15` | `43e48bdd7bf2` | `a306da76360e` |
| R5-P6-050-20261007-01 | #67 | `013bd96e9351` | `ae35c883f235` | `58da53ea1c91` |
| R5-P9-080-20261007-01 | #65 | `8e976b634f36` | `8181f13905cc` | `7ea617f86bdc` |
| R5-P9-090-20261007-01 | #69 | `d170ef9a37f5` | `cd39089e983c` | `278ebc1aaa50` |
| R5-P9-100-20261007-01 | #71 | `6c73230d7f3c` | `639311885302` | `e32564304c0c` |
| R5-P8-110-20261007-01 | #64 | `042ad4835767` | `478eec7047d5` | `8d5fa6f5af72` |
| R5-P8-060-20261007-01 | #68 | `b504ba8e2f52` | `49facad80dab` | `1c160d445d9a` |
| R5-P5-060-20261007-01 | #70 | `14697c87d543` | `ec1e2b5c5536` | `ac1afc64e738` |
| R5-P11-080-20261007-01 | #62 | `36f5c1f6e430` | `e4e026724897` | `c4b7e1c2c442` |

十个实施项 ID 和十个 PR 均唯一，状态与映射一致；#66 仅为进度记录，没有计为实施项。十项对应的原父任务均存在且保持未勾选。

## 171 个父任务保持原状

与统一基点逐项比较，原父任务的顺序、ID 和勾选状态全部一致：

- 171 个父任务，171 个唯一 ID。
- 10 个已勾选，161 个未勾选。
- 本批限定实施项为 10/10 完成、剩余 0。
- 父任务仍为 10/171 完成、剩余 161；没有因子项完成而扣减父级余数。

## 固定 80 个产品回归场景

已从原始 Go JSONL 按 package/test 重新统计叶用例，并从前端 verbose 日志按文件及完整名称核对相同场景的候选结果。没有将重叠整包或关联测试数量相加。

| 产品实施项 | 固定场景 | 基线通过 | 基线失败 | 同一组候选通过 |
|---|---:|---:|---:|---:|
| SMTP IPv6 relay（#61） | 4 | 2 | 2 | 4 |
| SMTP greeting causes（#63） | 12 | 6 | 6 | 12 |
| Recipient failure causes（#67） | 8 | 1 | 7 | 8 |
| Recovery conflict reinspection（#65） | 7 | 3 | 4 | 7 |
| Session-scoped refresh（#69） | 6 | 0 | 6 | 6 |
| Recovery queue load states（#71） | 8 | 2 | 6 | 8 |
| SMTP and retention configuration bounds（#64） | 18 | 1 | 17 | 18 |
| Attachment source error classification（#68） | 10 | 2 | 8 | 10 |
| Attachment progress and cancellation（#70） | 7 | 1 | 6 | 7 |
| **九项产品合计** | **80** | **18** | **62** | **80** |

分线核对结果为 SMTP 24 场景（9 pass / 15 fail → 24 pass）、后端 35 场景（4 pass / 31 fail → 35 pass）、前端 21 场景（5 pass / 16 fail → 21 pass）。基线和候选均无跳过。

候选日志包含全部相同 package/名称的通过结果。前端三份冻结测试 SHA-256 与作者提交及当前整合文件相符。

CI 子项另记三条契约断言及实际 GitHub 执行验收，不计入上述 80 个产品场景。其账本记录为基线 1 pass / 2 fail、候选 3 pass。

## 五个产品子树相同

以下子树与相应作者最终源码及产品整合 `e325643` 的 Git tree 完全相同：

| 路径 | 作者最终提交 | 相同子树 |
|---|---|---|
| `web` | `6c73230d7f3c00ab640e2a8b7ce10b20bb88a2f4` | `24f095f234179d15ba692e003fc2f34709026817` |
| `internal/outbound` | `013bd96e93518dd453df7202bacf54f02a0609f1` | `b2cb34f28b2d2dd9557bb9b1bc0a48b44fc3a5cc` |
| `internal/config` | `14697c87d543ddcd215cf14f7a9685d3a07ddee8` | `5c1a85f307cc0beddd1b868446c8333ed39cc73c` |
| `internal/app/companymail` | `14697c87d543ddcd215cf14f7a9685d3a07ddee8` | `b469ccbee6cf128681f3405f6df227f15ae33676` |
| `internal/mailcontent` | `14697c87d543ddcd215cf14f7a9685d3a07ddee8` | `c3a72dd4f6fb2a1bd7eaa2d72f54becb44795126` |

产品整合到被审核账本提交 `9232dee` 之间只有文档和证据变化，没有修改上述产品字节。

## 源码归属与资格边界

保留的 CI 运行是 `37628534160`，head 为 `e4e0267248979faa974fab124d6cc2e271771876`，实际 checkout 为 `a8ae5ebff8fd63d6bafde2805ff1a9c49f91e191`。其 source gate、严格依赖审计和默认 Vitest 仍记录失败；后续 TypeScript、Vitest、lint、build 与证据上传实际执行。该次 Vitest 为 865 pass / 1 fail / 0 pending。

这份 CI 证据限定证明失败后继续执行和留存证据的目标，没有被表述为 `e325643` 的完整 CI 通过。账本 README 与 TODO 将最终组合验证保留为独立运行记录，没有用局部回归取得完整 PostgreSQL、真实浏览器、性能、CI 或发布资格。

**最终裁决：未发现计数、哈希、来源映射或跨源码资格宣称错误。此次审核及落盘均不增加 TODO 完成数。**
