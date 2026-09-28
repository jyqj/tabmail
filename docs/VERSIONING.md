# TabMail 版本与分支管理

## 1. 已核验的源码基线

核验日期：2026-09-28。下表是不可变的 **R4 业务源码检查点**，不是随主线自动变化的“当前版本号”；后续文档、CI 和业务提交可以继续推进 `main`。

| 项目 | 基线 |
|---|---|
| 仓库 | `jyqj/tabmail` |
| 集成主线 | `main` |
| 源码检查点标签 | `baseline/company-mail-r4-20260928`，annotated tag |
| 完整 commit | `507fa292653ee585ba99ce323a29eb4135fccc24` |
| Git tree | `fb11a2e062a80d2c8cc9014076906163ccf70d23` |
| 受 Git 管理的文件 | 474 个；不含运行时数据、依赖和本地配置 |
| 数据库迁移 | Goose `00001`–`00013`；最高为 `00013_draft_creation_receipts.sql` |
| Go 声明 | `go.mod`：`1.25.7` |
| 前端声明 | `web/package.json`：Next.js `16.3.3`、React `19.2.4`、Vitest `4.1.11`；依赖树以 lockfile 为准 |
| 发布状态 | 本次核验时没有 GitHub Release；检查点不等于正式发布或生产验收 |

[主线 CI 36112428211](https://github.com/jyqj/tabmail/actions/runs/36112428211) 在 **2026-09-25** 针对以上 commit 完成：`backend`、`frontend`、`production-web`、`browser-journey` 四项均为 success。本次管理核验读取了实际 run/job 状态；这不是 2026-09-28 重新运行的业务测试，更不代表未来依赖告警、生产容量或公网投递已经验收。后续提交必须使用自己的 CI 结果。

## 2. 主线、历史分支与文档的关系

PR #13 已于 2026-09-18 合入主线；PR #14 与 #15 已于 2026-09-25 合入主线。#15 的历史 head 是 `74fc1da2ed8ae15ec2290b32a73362d51224f64f`，其中包含 #14 的 `5a99281940e54a44ad6a02612c0fd33aaac01d1d`。后者是 `507fa292` 的祖先，没有只存在于该分支的提交。

本次整理前没有 open PR。GitHub 上残留的 `fix/company-mail-safety-contracts` 已确认合并、确认精确 head 后以 SHA lease 保护删除；本地已清理远端不存在的 `origin/refactor/company-mail-architecture`。删除分支引用不删除主线中的提交或 PR 讨论。旧 PR 正文里的“尚未合并”是提交时的历史说明，应读取 GitHub 实际 merged 状态，不能据此重复合并。

文档入口按以下含义使用：

| 文档 | 含义 |
|---|---|
| [README](../README.md) | 产品入口与当前部署注意事项 |
| [R5-TODO](company-mail/R5-TODO.md) | 下一完整优化版本的唯一活跃任务清单；规划已建档，实现初始为 0/171 |
| [R5-DESIGN](company-mail/R5-DESIGN.md) | R5 目标架构、协议、边界与迁移原则；不是当前已实现能力 |
| [ARCHITECTURE-R4](company-mail/ARCHITECTURE-R4.md) | R4 数据所有权、接口兼容变更和协调升级要求 |
| [SAFETY-CONTRACTS](company-mail/SAFETY-CONTRACTS.md) | #14 的域名资产保护、授权 revision 和前后端操作契约 |
| [CONTENT-BOUNDARIES](company-mail/CONTENT-BOUNDARIES.md) | #13 的内容访问与应用边界 |
| [RELEASE-R3](company-mail/RELEASE-R3.md) | R3 的基础运行手册；R4 兼容与迁移要求优先 |
| [原始 ROADMAP](company-mail/ROADMAP.md) | P0→P1→P2 的历史设计；不是当前未完成任务清单 |
| P0 VALIDATION / R2 release-blockers | 对应旧基线的历史证据；不能冒充新提交的验收结果 |

基线目录边界：`cmd/tabmail` 装配进程；`internal/api` 是 HTTP 接口；`internal/app` 是用例服务；`internal/store/postgres` 保存事务和迁移；`web` 是前端；`scripts`、`deploy` 和 `.github/workflows` 承载检查与运维。版本整理不改这些业务实现，也不宣称完成了新的全量代码安全审计。

2026-09-28 在 `d6d512172fb6b874c3283d9df3f4d56758684a13` 上建立 R5 规划，按 12 阶段分批实施。R5 是工程迭代代号，不是 `v5.0.0` 或新的源码检查点标签；创建待办不改变 R4 的不可变基线，不提升前端私有包版本，不代表发布、部署或业务测试通过。R5 的实际完成情况以活跃清单中的任务和对应证据为准，旧 ROADMAP 继续保留作历史追溯。

## 3. 版本标识的唯一含义

部署与故障回溯始终记录 **完整 Git commit SHA**；tag 是可读检查点，不能替代 SHA、镜像 digest 和数据库迁移版本。

- `main`：持续集成分支，不等于“已上线”或“正式稳定版”。
- `baseline/<阶段>-YYYYMMDD`：已核对的源码检查点；说明必须记录 commit、tree、验证时间和边界，不创建同名正式 Release。
- `archive/<名称>`：历史实现的只读参考；不得整包并回主线、复用其迁移体系或把其旧测试成绩挪给当前源码。
- `vMAJOR.MINOR.PATCH[-rc.N]`：仅用于有明确发布说明、兼容范围和验收记录的版本；不因一次整理自动提升版本号。

既有 `v1.0.0` 指向 `67110d61d4e9f7b0c1f7c01cb4d34ae8fda3eb34`，是历史标签，不代表现在的公司邮箱 R4。两个既有 `archive/*` 标签原样保留；已有标签不得移动、覆写或复用。前端是 `private: true` 的内部包，其 `0.1.0` 不是整个产品的正式版本；本轮不为表面一致去改 package/lockfile 或再引入一个会漂移的 VERSION 文件。

`make build` 使用 `git describe --tags --always --dirty` 生成版本字符串，因此可能包含 `baseline/` 前缀；不要直接把它当 Docker image tag。现有 Dockerfile 不注入相同版本字段，部署仍须独立记录 commit 与镜像 digest，不能据包名或容器名称猜测实际源码。

## 4. 分支与合并约定

从刚同步的 `origin/main` 新建 `feat/`、`fix/`、`refactor/` 或 `chore/` 分支，一个分支只承担一批可验收改动。默认通过 PR 回主线；不得强推主线、把未提交内容混入版本整理，或用旧 CI 代替目标 head/merge tree 的检查。确需堆叠 PR 时，正文列出依赖关系与精确 SHA，顺序整合后重新检查目标树。

GitHub 已启用 `delete_branch_on_merge=true`：只有合并成功才自动删除已完成的 feature branch，**不会自动合并 PR**。手动清理前必须核对 open PR、精确 head 和主线包含关系；有独有提交的分支保留待审。Squash 后不能只凭祖先关系判断丢失，必须核对 PR、实际差异与源码树。本次已合并旧分支是直接祖先，不存在这一歧义。

本轮未改仓库可见性、协作者、合并方式或主线保护规则；`main` 在核验时未受分支保护，以上 PR/检查约定不是已由服务器强制执行的规则。后续启用 required checks / ruleset 时应先确定检查名与维护者流程，不虚称已启用。

当前 CI 面向 `main` 的 push 与 PR；不再保留已经结束的 R2 / #14 分支名作为触发目标。保留完整后端、前端、生产镜像、浏览器和 PR 精确源码归档检查，不为文档整理关闭测试或放宽失败门槛。

## 5. 本地安全同步

在仓库根目录执行；工作区不干净或本地主线分叉时停止，不自动 stash、reset 或丢弃文件：

```sh
set -eu
test -z "$(git status --porcelain)"
git fetch --prune --tags origin
git switch main
git pull --ff-only origin main
git status --short --branch
git rev-list --left-right --count main...origin/main
```

完成后最后一行应为 `0 0`。本次仅对当前仓库设置 `pull.ff=only` 和 `fetch.prune=true`，不更改全局 Git 配置。需要查看历史检查点时，先保持工作区干净，再使用独立 worktree 或 detached checkout；这不是数据库回滚指令。

版本整理前的引用与仓库设置快照保存在本地 `.git/tabmail-version-management/20260928/before.json`，不上传 GitHub，不包含凭据或运行时邮件数据。这是引用清单，不是完整源码/数据库/对象存储备份。

## 6. 正式发布仍需独立验收

正式 Release 必须明确完整 commit、不可变 tag、镜像 digest、对应 CI、Goose 版本、依赖审计时间，以及接口兼容变化。R4 包含草稿分页、离职 plan_id、授权与发送策略 revision 等变更，不能在未说明兼容性的情况下当成旧版的无感补丁。

迁移须遵循 R4 手册：联合备份数据库和对象存储，在恢复副本验证 `pg_trgm`、回填与索引负载，停止旧 API/SMTP/worker/retention 写入，协调升级所有角色和前端。不得自动运行破坏性 Down 或让新旧写入端混跑。公网收发、DNS/TLS、容量和恢复目标另行验收；本次版本管理没有部署、启动服务、迁移数据库或向外部地址发送邮件。
