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

### 资源隔离修正（取代上文单 runner 四 worker 接线）

`4489453a` 的 catalog 完整门禁、frontend、production-web 和 browser-journey
成功；backend 在共享 runner 重跑中触及分片进程/660秒总预算，随后到达原20分钟
job超时。此失败保留，未靠重新跑同一源挑绿，也未提高180秒或20分钟上限。

现将 PG 放到 4 个独立 matrix runner，各自串行执行固定的4个分片；另设汇总 job，
重新从实际源码编译发现全量测试，并逐一用原检查器验证4份 artifact中的全部原始
日志、来源、命令、哈希、唯一覆盖和 mandatory。缺失/多余/错源/篡改/超时任一项
均失败。独立 backend 源码/协议检查不依赖 PG 成败，继续产生完整证据。

这是明确的 CI 拓扑调整：从4个 job定义变为6个 job定义（含4路matrix，共9个实际
job），不是保持原4 jobs的声明。每个 backend相关job仍20分钟；每个Go进程180秒，
每个分区仍660秒内预算。只运行分片中实际有测试的包；全部包仍在独立全量编译
发现阶段检查，所有1478测试必须完整且唯一覆盖，不跳过失败测试或改变skip规则。

矩阵负控10项、接线/制品/模板门禁控制24项、当前目录两项通过。最终远端矩阵CI
尚待实际运行，不能由这些合成/局部结果宣布完整修绿。

### 前端构建隔离

`ca789c0d` 的4个PG matrix分区、独立全覆盖汇总、backend完整源码/协议、catalog、
production-web和browser-journey全部成功。汇总job 115391882344 在实际merge
`f53d4b483a5a2afdf488fb27d9a84558f8fb9557` / tree
`a10cbfdd661f709d52a3b416e624ba7099b07fd2` 独立验证1478顶层、247 mandatory。
frontend的源码、完整默认测试、类型、lint、严格audit步骤成功，但最后npm build被原
15分钟job时限取消；不能登记该workflow成功。

当前把原npm build移入独立frontend-build job：同一checkout/lock、原npm ci和
npm run build，仍15分钟，失败仍使完整workflow失败。原frontend保留全部验证与
原证据上传；没有删构建、增大预算或用Docker构建结果顶替它。拓扑现为7个job定义，
矩阵展开10个实际job。接线/前端独立失败/原制品/模板控制27项、当前目录两项通过；
最终远端整轮尚待验收。
