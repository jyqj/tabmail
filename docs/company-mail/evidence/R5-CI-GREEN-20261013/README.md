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
