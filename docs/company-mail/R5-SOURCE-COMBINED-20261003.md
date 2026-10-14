# R5 source-only 两完整分支独立组合

Base 为 batch5 `4aec142bcffd53c6ab91f2263436aaf980833fab`，独立分支 `integration/r5-source-combined-20261003`，实际测试源为 `a9012118b4fdab630bc0afea349df3759e9efc66`。完整 no-ff 合入 compatibility `006ff693` → `ee3308fd` 与 runner preparation `753e8f94` → `054492fd` → `92c04049`，五条提交均为组合祖先。没有只合末尾文档；作者 initial/final、rawlogs、历史 FAIL 保持原字节。仓库无 `.agents/skills`，workspace `.agents` 为空；已读 `web/AGENTS.md` 与 testenv 执行说明。

## 实际复验

| 阶段 | 结果 | 资格边界 |
|---|---|---|
| 首轮默认 runner | FAIL / exit 1，675 discovered、0 dispatched | 首次 Go hydration 补齐测试依赖，default capture 命令 stdout 哈希发生变化，严格 preparation 比较拒绝；原始报告和四 capture 保留 |
| 原默认 runner，hydration 后重跑 | PASS / exit 0；675/675 started | current 671 + 原 frozen-v1 4，0 failure/error/skip，完整 partition、无漏无重；未用 wrapper、未改 runner 或断言 |
| 真实 Go 选择 | PASS | default 与 race-r5protocol 的完整 selection identity 前后相等；selected local、MVS、package records、coverage 等原域保持 |
| 同 source 固定二进制 fixture | PASS | official `go tool test2json` 实际执行 parent proc FD 固定 inode；build/executed SHA256 相等，21 fixtures；Python typed receipt 测试实际 PASS |
| 缺 fixture 反例 | 预期 FAIL / exit 1 | 真实 assertion 要求 fresh same-source build/run evidence，未 skip、未伪造 fixture |
| 相关 contract/protocol/compatibility | PASS | 223/223；contract CLI 16 shared models、36 company DTO、68 company operations；client AST 13/13 |
| compatibility source CLI | PASS | 132 routes、134 client branches、94 source files；`task_complete=false`、`product_green=false`、current wire required |
| scoped static/格式 | PASS | 七个改变的 Python 文件 AST 与内存 compile；scripts diff whitespace exit 0；本轮无 Go 源变化 |

首轮失败不是测试通过，也不声称 host 根因完全解决。比较日志显示 selected-local/MVS/package records 未变，而 hydration 后命令 stdout 哈希变动；重跑保持原严格比较并获得逐项实际执行。另一次未带 Go 环境的 standalone compatibility CLI invocation failure 原样保留，正确配置后的新结果另存。

原 runnerprep 669/669 与两个 compatibility ERROR 是其原源真实结果；本组合新增六个 compatibility 回归，675 项全部执行。两 ERROR 通过明确 schema2 current-source inventory 与冻结历史 wire/source 分离解决。历史 safejoin 只验证历史 metadata；当前源码拒绝旧 spec/case 或给旧 wire 重签。source green 不授予 HTTP/PG/旧 client upgrade、依赖审计或父任务完成资格。

实际执行二进制 SHA256：`99bdf08aa4c61fe7e43d94226d5a67465cbb383932f8f547e4b9ce530262a97f`。Run SHA256：`40a16ea926467f2cfa94b575ced86d51e833e5b64cec06a4d28bf718ca1b6a21`。fixture 只保留哈希与个数，不新增发布 body、binary、npm tree、source clone 或 secret。完整实际 ID 分区和安全 selected-source metadata 用 deterministic gzip 保留，未改原日志。

## 保留与未过范围

aeed `aeed47f0e1998d9925acb680992b051463821d69` 的 1,051 个非 docs/test 基线文件逐路径比较：1,012 个产品/runtime/config 文件全字节相同；其余工具文件仅三项已授权作者改动（compatibility checker、versioned runner、testenv README），新 preparer 是新增文件。不得将这个明确的工具例外写成 1,051 文件全部原字节不变。36 old-history 文件全 hash 相同。相对 batch5，旧 evidence 只有获准 schema2 `R5-COMPATIBILITY-GATES.json` 变化；锁、两 replace、workflow/PG180/audit gate 不变。

完整 diff whitespace exit 2 来自作者保留的 `R5-COMPATIBILITY-CURRENT-20261003/push.raw.txt` 四处远端原输出尾空格，未清洗历史证据。源码范围 exit 0，新维护文档另验。

新 HTTP/sharedDB `ac5db2ee` 仅作为另分支旁证，没有合入，也没有据此 admit current wire。根任务的 current actual-wire failure（含 stale subject oracle）仍待修；没有把私密 subject 恢复进普通 receipt。sharedDB RC02/BC03 与 components 外部 dependency 准备仍属其他 owner，不合未知 head。

本地 whole PG180、full Web/type/lint/build、browser、真实邮件服务均 NOTRUN；原 backend PG180 与完整 dependency audit 门禁保持。**10/171 不变，没有关闭 R5 父项，没有 merge/deploy/force。** 后续交付 HEAD 为安全 evidence/TODO docs-only 维护，不把它的未观察 CI 写成已通过。

普通 origin push exit 0；`ls-remote` 精确确认请求头 `7c8eba4526568943f682deded1b26f73c2cf7079`。新组合唯一一次 `gh pr create --draft`（base=batch5）实际 exit 1：`Post "https://api.github.com/graphql": Forbidden`。PR 未创建，立即停止 PR/API 动作，没有换身份、connector、remote 或 route，没有代重试旧 PR。该分支 push 不匹配原 main-only push trigger；没有 PR，所以本任务新 CI 未观察/未启动，不借旧 head 成绩宣称 green。原 PG180/audit gate 不变。随后只提交拒绝结果和普通 Git push，最终交付 SHA 用外部终态回执精确确认。

在 replay 已结束、证据 commit 已冻结后，root 新批准 `89e7af39aa53408225b284548bffe39ce467aa2d`（test source `6138113c1896cb810db567605154b5023f0340de`）完整 RC02/BC03 分支。按 root 的“若已冻结跑完/发布，先交当前 head，不扩大”指示留待下一组合，未合入本轮。其 46 shared-DB verified 只属另分支 scoped PASS；本轮没有复跑旧泄露 subject oracle 或改 caseJSON/gate。

证据入口：[summary](evidence/R5-SOURCE-COMBINED-20261003/summary.json)、[preservation](evidence/R5-SOURCE-COMBINED-20261003/preservation.json)、[完整默认报告](evidence/R5-SOURCE-COMBINED-20261003/versioned-hydrated.json.gz)、[首轮拒绝](evidence/R5-SOURCE-COMBINED-20261003/versioned.json.gz)、[payload hashes](evidence/R5-SOURCE-COMBINED-20261003/payload-sha256.json)。
