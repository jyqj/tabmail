# CI 修绿进展（2026-10-13，未完成）

尚未合入 main，未发布、未部署；原父任务仍为 10/171。

## 已验证的有限修复

- 目录身份修复 #261 后，原 selected-binding mutation 用例在 Company run
  38405752547 通过；该运行的源码检查失败来自 revision19 对旧工具字节的固定要求。
  当前只承认精确已审 Git blob，不重写旧证据清单或放开任意修改。
- 依赖审计原 8 high 来自 `braces` 的同一递归深度漏洞
  [GHSA-vfj7-8cjw-p6xm](https://github.com/advisories/GHSA-vfj7-8cjw-p6xm)。
  采用精确 override `npm:@dieub/braces-depth-guard@3.0.3-pn.3`，不降级 Next.js、
  不关闭审计、不隐藏 devDependencies。该包是第三方维护派生版，不是上游官方修复。
- 已核对 npm tar SHA512 与 provenance subject，以及 10 个打包文件与源码
  `305a2e4bfe324bb53c336c1b03387ee1251c926f` 完全相同；检查深度守卫、
  原 stringify 行为与循环父链拒绝。上游派生库原套件 799 PASS；本项目实际
  micromatch 消费路径 14 PASS。原 3.0.3 的 4000 层 expand 实测栈溢出，
  修复后 101/4000 层有界拒绝，100 层与正常模式保持可用。
- `npm audit` 实测 8 high → 0；`npm audit signatures` 验证 752 个 registry
  签名和 153 个 attestations。审计零告警不替代上述代码及行为审查。
- lock 更新触发原完整 hash 准入拒绝（run 38411878719）；随后只增加精确新
  lock hash，保留官方 TypeScript 5.9.3 归档和原安装边界。旧 lock 测试绑定
  原历史字节，新增 alias/其他版本保持/未审字节拒绝测试。Linux 组合 9 PASS。

## PostgreSQL 尚未解决

Company job 115324787007 中 postgres 包在约 170 秒后耗尽 180 秒进程预算
（保留 10 秒 cleanup），142 个顶层用例已通过，后续大量用例无剩余 setup 预算。
首个失败在迁移版本 5 截止。它不能被记成所有未执行用例各自的业务失败。

`tools/ci/backend_shards.py` 是尚未接入正式 workflow 的候选：实际编译发现全量
默认测试，按顶层名称分区防止跨包同名重复；保留每个 Go 进程 180 秒、race、
count=1、原 mandatory/skip 规则，逐份调用原证据检查器，并验证集合恰好覆盖。
候选有 7 项合成控制测试通过，不能代替实际 PG 或最终 CI 验收。

## 证据与限制

本目录保留 npm 原审计和制品匹配记录。完整 CI 仍需精确最终源码实际执行。
旧失败不被本地专项成功抵销；目录、依赖和测试设施修复不关闭原父任务。

## PG 全覆盖实测与正式接线候选

Linux ARM64 / Go 1.25.7 / PostgreSQL 16 独立临时集群，源
`ce971c3412776200a733ad705f73483055c800e0`：发现并执行 1478 个顶层测试，
247 个 mandatory 全通过，11927 个唯一叶通过。唯一 skip 是原清单明确允许、
在独立 browser-journey job 执行的 `TestR3BrowserJourney`，未新增允许项。
原 v2 汇总器把这个既有允许项误列入动态必过集合而失败；原日志和失败保留，
修正为计划集合扣除原 allowed 集合后，对同一原始日志重新检查为 16/16 分片通过。
这是对该源原始执行的后验检查，不冒充最终接线源码已经重跑通过。

首次全覆盖发现两个旧夹具问题：refresh 已使用无别名的完整 userSelect，原观察器
仍搜索 `FOR SHARE OF u`；restore 夹具使用已过期 purge_after，当前契约应拒绝。
修复只调整查询观察器、restore-first 的有效期限和显式扫描 cutoff，保留真实等待、
源/索引/原件/计数/审计/回执断言，且不清除原 expires_at。相关 28 个叶全部通过。

完整 PG 现在显式启用原 owned 离职/交接探针，并使用其要求的独立 loopback
55447 端口。UI 依赖正常 npm ci 到私有外部目录，临时链接后精确清理；避免
node_modules 中 flatted 的无关 Go 文件混入 ./...，不删除依赖文件或过滤 Go 包。
候选 workflow 保持 4 jobs、backend 20 分钟、每个 Go 进程 180 秒和原全部门禁。
分片共用 660 秒内部预算并预留 60 秒清理，单独上传每次调用的原始日志与计划；
不把多进程日志拼成一次调用。原证据检查器、mandatory 清单和 skip 清单没有修改。

新增覆盖控制 8 项及 workflow/原制品边界控制 12 项通过。精确修复后完整 CI
仍需等待，不据本段合入 main。前一个公开源 `0d584522` 的 frontend、production-web、
browser-journey 与 catalog 已成功，backend 仍保留原全包 race 失败。

### 远端接线后验证

公开 `095b9518` 的 Company run 38415784552 / backend job 115359715004 已实际通过
新的完整 PG race 步骤。Catalog 原 inventory 通过；完整源码版本测试唯一失败为
`test_r5_ci_wiring.R5CIWiringTests.test_archive_guard_order_and_version_dispatch`：
它仍要求 workflow 中出现旧单进程命令，随后还禁止所有 unittest discovery。
现保留原 archive/build/vet/版本化分派约束，改为要求新的全量覆盖 runner；只允许
独立 tools/ci 控制套件使用 discovery，原源码套件仍走隔离版本化分派。组合17项通过。
该修改未删测试、未更改产品源码或原 PG mandatory/skip 清单，最终完整CI仍待验收。
