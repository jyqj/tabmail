# R5 archive 独立固定 checkpoint review

本 review 的原始反例固定在 `0c1f4f6f4cda1d39952df1fe9eeda87d74f56eb9`；随后仅核对用户给定最终 `66eb5e819170d3508edb07db8ab1e0014555ff8c` 的 delta。base 是 `41b015c30c66b3ba58a3c1395e8559ebcd27a65f`，设计是 `05b4bb886164d0d85353a542fbb2881f104089b4`。未自动跟随分支。所有新增内容仅为独立 tests/docs/evidence；作者生产源码、历史文件、中央 TODO 均未修改。

## 先报告重要事实

1. **0c1 有条件实际越界读取已复现。** `root_fd` 在祖先 symlink 检查与绝对路径 `os.open(root)` 间，存在可控窗口；自己的临时 parent 被换成 symlink 后，`boundary.read(root, 'own.txt')` 实际返回 root 外自编 sentinel。`O_NOFOLLOW` 只保护最终 root 组件，未保护该次 pathname lookup 的祖先。另一个反例在 `_module_document → checked_path(...).read_text()` 的检查后换 leaf 为自有外部 go.mod，实际 stream inode 对应外部文件，完整 `boundary.check` 仍通过六 module 检查。外部 module 多了独有注释，因此不是仅有相同字节的假读。
2. **66eb 的相关 delta 已通过独立反例核对。** root 改为从 `/` 起逐级 descriptor/no-follow 打开并比对身份；`boundary.check` 和 v4 复用的 v3 body reader 改为 `boundary.read`。同样的 ancestor symlink 插入现在拒绝，未获得外部 body；完整 v4 在禁止全部 root 下 pathname body-read 的 hook 下仍能 capture。leaf 换为自有外部 symlink 时，`/proc/self/fd/<O_PATH handle>` 实际重开原对象，随后 closing identity 拒绝。
3. **新 archive selected v2 的独立 cold/warm 验证通过。** 两指定 head、两个 context 都实际 capture/validate；Go1.25.7，独立正常 Git clone，开始/结束均 clean。每个 head 的首次 default capture 从空 module cache 和空 build cache 开始；race-r5protocol 使用另一空 build cache，但 module cache 已由 default hydration 准备。根 `go build -mod=readonly ./...`、`go vet -mod=readonly ./...` 均 exit 0。没有运行生产邮件服务或 PostgreSQL。
4. **旧 v1 冷 bug 未修。** 冻结 base、空 module/build cache 下 capture 成功，随后 validate 同一回执失败；暖缓存重新 capture/validate 成功。原始 MVS 的 57 项 `Dir` 字段 hydration 前后变化，原命令 stdout SHA256 改变，规范化 MVS 不包含这些字段。旧四 actual-root 暖缓存测试通过只是兼容性，不是冷 bug 修复；没有 cherry-pick `209d` 或替换旧算法。
5. **全量版本 runner 仍失败。** dispatch union/disjoint 完整，实际启动 union 不完整。expectedFailures 的红灯/报告 delta 已独立实测。新 32 个测试 ID 在最终 runner 中全部实际启动且无失败、错误、skip 或 expected failure；这不使全量绿灯。

反例都通过自编 hook 插入一个确定性调度窗口，真实文件、真实 syscalls、真实 body/descriptor 身份保持。可达条件为执行期间有进程能修改对应自有 fixture 的 ancestor/leaf；没有证明 CI 上存在不受信任并发写入者，也没有访问系统私密或其他人的文件。finite repeated observation、前后哈希和上述反例都不是全局 atomic proof。

## 固定内容、拓扑与生产覆盖

独立使用 base Git blobs 核对 registry 的 36 项 path/size/SHA256，恰好 21 项 Go。registry SHA256 固定为 `8e04081e1f2b9d9b82b854954b56a1ac71047646f8768d1b7a7ff89af223bb15`；修改 production_roots 的自授权 registry 仍拒绝。最终先验 hash 检查在 JSON parse 前；权威来自固定 bytes，而非 selected metadata。

Git tree 比对另证实：base 的全部 **528 个既有 evidence 文件**在两个 head 上 blob/mode/path 均未改变；**765 个 cmd/internal/third_party 输入、root go.mod/go.sum 和旧 selected helper**也未改变。六 module 为 root、两精确 fork、三精确 archive marker；marker 与历史域分离。未知 module、新 root、ignored Go、隐藏生产 module、空的新 archive-family 目录、VCS symlink 均有拒绝实证。

default 与 race-r5protocol 的 selected_local 数量分别 **589 / 592**；36 archive_static 与 selected_local 交集为空。21 个历史 Go 在 archive_static，经原 hash 绑定，未作为 root 执行输入。root `./...` 与显式 `./cmd/... ./internal/...` 的 package identity 相等；fork 使用独立 explicit pattern。66eb 另将 **64 个全部变体目录**逐目录实际 Go metadata 与静态 `production_go` 的完整集合核对。

最终额外的真实自编 clone fixtures：

| fixture | 实际结果 |
| --- | --- |
| 没有任何 importer 的 standalone 包 | default selected 含文件，根 build exit 0 |
| test-only 包 | selected TestGoFiles 含文件，variant record 存在 |
| r5fixtures-only 包、Windows-only 包 | root default 不执行它们，explicit variant metadata 含 IgnoredGoFiles；完整静态域涵盖，selected_context=false |
| standalone 改 undefinedIndependentReviewSentinel | 根 build exit 1，stderr 含精确 sentinel |
| 删除第一 archive marker | 根 build exit 1，stderr 恢复相应历史 archive 路径 |

未执行其他平台/tag 的编译。静态覆盖和 IgnoredGoFiles 覆盖不能赋予这些变体运行资格。

## 读取边界、预算与 identity

独立负例验证 canonical path、own symlink、FIFO、单次 body 超过 MAX_BYTES、ignored subtree 的全局 entry/depth 预算拒绝；topology 在禁止 `boundary.read` 的 hook 下仍枚举合法 raw/cache 名称，未开正文。MAX_BYTES 是每次 body-read 的上限，entry/depth 是 topology 的观测界限，不是瞬时一致快照或总读取量/内存承诺。

完整 v4 禁止路径 body-read 的观测覆盖 `_module_binding`、manifest/protocol bytes、files hashes 和 `_check_go_inputs`。旧 v2/v3 保持旧策略入口；六种互异 source schema/policy 组合与 selected-v1/selected-v2 双向交叉均在 capture 前拒绝。receipt 来源由固定源码与实际命令绑定，metadata path 不作为可任意读取正文的 capability。

66eb 每次正式 capture 记录 11 条 observation；本 review 额外保留全部 12 次实际 subprocess stdout/stderr，包括 unbound hydration。四次 capture/validate 的 **20 对正式 repeated commands 原始 stdout 全等**，并独立解压验 SHA256。0c 的 16 对也全等。Go-env 的原始 GOGCCFLAGS 临时前缀差异只在 helper 规定的 canonical hash 域归一化；没有把 raw env 全等当作要求。

66eb default cold 与 warm attestation identity 相等，整份 receipt 只在 hydration_diagnostics 不同，包含真实下载 stderr。该字段明确 unbound、排除于 identity；原始准备输出在独立 evidence 中保留。注入一次 exit 7 hydration，实际错误对象保留 role、exit、stdout 和 stderr。0c 的 helper 丢弃 hydration 诊断，但本 review 的 raw recorder 仍保留了那次执行；不能倒称旧 helper 自身已经保留。

## 版本调度与剩余 blocker

| 独立运行 | discovered | current requested / started | frozen started | failure events | error events | skips | expected failures | current 未启动 |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 0c 初始环境 | 640 | 636 / 601 | 4 | 2 | 64 | 1 | 未报告 | 35 |
| 66eb 原锁 Python venv | 644 | 640 / 612 | 4 | 2 | 40 | 1 | 0 | 28 |

两个运行的 requested testIDs union 精确等于 discovered、两组 disjoint；actual started disjoint，但 union 分别缺 35 / 28。这里 errors 是 unittest 的事件数，不能与失败测试 ID、setUpClass 未启动数混为一谈。合成 expectedFailure 在 0c child 返回 0 且未报告，在 66eb child 返回 1 且报告精确 ID。0c 的“未报告”不能伪称为 0。

最终两个 failure 是原 `test_runner_default_overrides_ambient_optin_and_selected_source_must_match`、`test_valid_excluded_files_never_read_or_enter_source`，保留 source/descriptor 原测试与错误。全量 source AST/兼容性并未完成：fresh clone 无相对 `web/node_modules/typescript` 导致 compatibility 的 9 个 ID 未启动；Go AST producer 尝试默认 `/home/agent/.cache/go-build` 写入，被只读文件系统拒绝，transaction 的 19 个 ID 未启动。已停止该对应动作，未改安全设置、未 escalation、未换路重跑。原锁依赖使 date-time validator 在本运行可用，解释其余计数与作者报告不同。

作者随 66eb 带的 summary 记录 current 624 / 640、2 fail / 64 error / 1 skip / 16 未执行。它指向 `2f0ba47e200c2f6364db3f32adff0f1f53988b20` 的执行来源，应按其原 evidence 解读；本 review 不将其改写为自己的 66eb 实测。作者 PG timeout/SMTP fork panic 不在本次执行中重跑，也没有因此被擦除或认定解决。Method19、SML、wholePG、wholeCI、外部依赖字节/native ABI/runtime 均未获资格升级。

## 证据与复现

新增脚本都以 `review_` 命名，避免改变作者自动发现的 `test_*.py` 调度全集：

- `scripts/tests/review_archive_checkpoint0c1f4f6.py`：固定 0c 的 8 项独立观察，包含三个已知反例。PASS 表示反例成功重现，不是漏洞 gate 通过。
- `scripts/tests/review_archive_delta66eb.py`：最终 delta 的 9 项独立观察。`REVIEW_DELTA_ROOT` 必须是 final 正常 Git checkout，`R5_TEST_GO` 必须指向 Go1.25.7。
- `scripts/tests/review_archive_actual_capture.py`：`--root --output --go --modules`；fresh output/cache/modules 路径，记录 actual raw subprocess、cold/warm receipts、root build/vet。
- `scripts/tests/review_archive_live_fixtures.py`：`--source --output --go --modules`，只修改自己创建的临时 clone。
- `scripts/tests/review_archive_legacy_cold.py`：冻结 base 的 cold/warm 旧 v1 实证，必须使用空的自己 cache/modules 路径。

独立 evidence 位于 `docs/company-mail/evidence/R5-ARCHIVE-INDEPENDENT-REVIEW-20261003/`。`observations.tar.gz` 包含指定 head 的原始 stdout/stderr gzip、receipts、完整 runner raw logs/reports、fixture summary、旧 v1 MVS 差异；`payload-sha256.json` 给出解包后每个文件 SHA256。`summary.json`、`runner-counts.json`、`git-integrity.json`、`raw-observation-checks.json` 可直接阅读。归档不包含缓存、依赖正文、私密服务环境或作者未提交工作。

有界结论：66eb 上本次 archive/static/selected/build 目标通过；原 0c 反例与旧 v1 cold bug 有明确保留；全量 runner 失败和上述未执行项仍是 blocker。没有 merge、deploy、wholeCI 绿灯或 atomicity 结论。
