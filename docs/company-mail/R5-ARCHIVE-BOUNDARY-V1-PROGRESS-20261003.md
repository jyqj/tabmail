# R5 档案编译边界 A：实现与真实验收记录

2026-10-03。实现基于 PR21 `41b015c30c66b3ba58a3c1395e8559ebcd27a65f`，设计锚点 `05b4bb886164d0d85353a542fbb2881f104089b4`。源码验收锚点见同目录证据包 `summary.json` 的 `implementation_commit`；随后仅追加本记录和执行证据。独立分支 `feat/r5-archive-boundary-v1`。没有编辑中央 R5-TODO，没有合入同批其他实现、merge 或 deploy。

## 已实现的有限边界

仅在 `R5-MIME-PREPARSE-SOURCE`、`R5-MIME-PREPARSE-SOURCE-V2`、`R5-MIME-PREPARSE-SOURCE-V3` 三个精确目录增加无依赖 `go.mod`，marker 完整字节、module 值和 SHA256 固定。全仓恰好六个 module（root、两 fork、三个档案）。root go.mod/go.sum 和两 fork replace 不变。

`r5-archive-boundary-v1.json` 从 baseline Git blobs 生成，共保护 36 个历史文件（21 Go）；registry 原字节 hash 固定在 guard 中，修改、重复 key、伪造 hash、traversal 均失败。36 文件均保持 bytes/path；另逐一核实 baseline 全部 528 个既有 evidence 文件 bytes/path 不变。marker 是新控制输入，不加入旧 capsule/receipt。

新 guard 从 `/` 逐级通过 no-follow、identity-matched directory descriptors 取得 root；全仓遍历包括 ignore/excluded 目录，module 不剪枝，仅排除 root `.git` 元数据。拒绝 symlink、FIFO/device、metadata 不可用、未知 module、go.sum、workspace/vendor、未分类 Go/native 输入、生产根外 Go、excluded 生产 Go、档案新文件/目录。检查预算为全仓 100000 entries、64 depth、单受控读取 64 MiB；旧 fork excluded metadata 的 4096/32 限制保留。正文从已确认 regular 的 O_PATH object descriptor reopen，避免 leaf 被替换为 FIFO/device 后取得其 I/O；closing metadata/path identity 再核对。

inventory 新 v4 `r5_current_local_inputs_archive_boundary_v4` / schema4，旧 v2/v3 dispatcher 和默认身份保持；新增 reader 只在 v4 使用，旧公开调用的拒绝规则不放宽。v4 绑定 registry、三个 marker、新 guard/v2/runner/tests/CI bytes，36 历史文件独立归入 archive_static。

selected 新独立文件 `r5_selected_source_binding_v2.py`，policy `r5_root_selected_local_archive_attestation_v2` / schema2。没有修改旧 `r5_selected_source_binding.py`。新 v2 不依赖旧 helper；采用独立稳定观测：env → 非身份 hydration → selected → MVS → coverage → 重复 selected/MVS/coverage，正式重复输出要求 raw bytes 相同。hydration 的实际 argv、exit、stdout hash、stderr 保存在独立、不参与恒定 identity 的 diagnostics；失败异常保留真实 command/stdout/stderr，CLI 输出失败记录而不盖成功章。新 schema2 不是另 session 的 stable successor 身份。

v2 固定 Linux/amd64、cgo=1 的 default 与 race+r5protocol 两 contexts。保持根 `./...` 与两 fork patterns，比较 root 与 explicit `./cmd/... ./internal/...` 的生产 package/module/Dir/test variants/Go fields；同时显式查询 filesystem 找出的每个生产目录，selected/ignored Go 文件并集必须等于独立全变体 superset。所有本地 selected packages 与 files/fields/hash 均记录；archive package 进入 metadata 立即拒绝，不后处理删除。其他 OS/ARCH/tag 是静态覆盖，未声称实际编译或运行。

## 新旧测试调度

CI Build 前调用 guard，原 root build/race/vet argv 不缩小。backend 的裸 unittest discovery 改为 version runner；frontend npm ci 后同样使用该 runner。runner 总是在独立正常 Git clean clone 的当前 HEAD 做 discovery/当前测试，四个旧 selected v1 actual-root ID 在 baseline 的独立正常 Git checkout 执行；stdout/stderr 保留，两组请求/加载/实际开始 ID、source SHA、exit、failure/error/skip/expected-failure/unexpected-success/missing 均报告。遗漏、重复、错误 baseline、子进程失败、skip、expectedFailure、缺 fresh v2 actual-root 都不能全绿。

按父级最终决定，没有合入 `209d2f5…`，没有可选 stable successor 调度或其新 suite。本分支旧 helper 字节完全原样。旧四项 PASS 是配置的**暖模块缓存**上的历史兼容性结果：当前组先执行且 seed cache 已 hydration；不证明旧 v1 首次冷 roundtrip 被修复，该已知历史缺陷仍保留。另 session 的修复分支/证据独立保存。

## 实跑结果与证据

证据：`docs/company-mail/evidence/R5-ARCHIVE-BOUNDARY-V1-20261003/summary.json`、`payload-sha256.json`、`execution-evidence.tar.gz`。tar 中保存每组完整 source/实际 test IDs/命令/日志、两个 v2 receipts、原 root/fork race logs 和真实性 gate。具体数量/完整 SHA 以 summary 为准。

- guard 15 项通过；含 36 文件逐个 byte/delete/rename、marker 删除/值/指令、module 位于 docs/生产/fork/build/档案/node_modules、档案新增、registry forgery、symlink ancestors、FIFO、leaf replacement、metadata-only 不读取 raw/cache 正文、descriptor second-stat replacement、预算/不可读 metadata、未知生产根/native/ignored Go、生产 import/embed/generate 指向档案等实际 fixtures。
- 新 v2 actual-root 在正常 clean Git clone 执行：default、race+r5protocol、真实 capture/validate；四类控制/档案/生产文件在真实 metadata capture 中途修改，均拒绝；错误身份/metadata 不能成为文件读取能力；unsupported context 拒绝。hydration failure 负例保留真实 dispatch 的 role/exit/partial raw streams。
- 新生产无 import 者的独立 fixture 实际进入 root go list 并 root build exit0；替换为 `undefinedBoundarySentinel` 后 root build exit非零；移除 marker 后 root build 恢复历史缺符号错误。fixtures 均在独立 checkout，不污染生产。
- fresh 空模块/cache 的 default capture→立即 validate 和随后 race+r5protocol capture→立即 validate 均稳定通过；各覆盖 64 个生产目录。default 602 metadata packages / 589 selected local files；race+r5protocol 604 / 592。正式 raw 观测重复一致；冷准备的下载 stderr 保留在独立 hydration diagnostics。
- 源码锚点的 `go build -mod=readonly ./...` 与 `go vet ./...` exit0。Go1.25.7，GODEBUG=asynctimerchan=0，GOWORK/GOENV off、GOFLAGS 空、GOTOOLCHAIN local。真实 root/explicit 生产集合相等，21 历史 Go 不被 root 选择，36 hashes 不变。
- 全量版本调度整体 **FAILED**，未 expected-skip 旧失败。当前组的 2 failures / 64 errors / 1 skip / 16 因 setup errors 未执行的 ID 与独立 baseline 全量运行逐项相同；所有新增项通过。dispatch union 覆盖全部 discovery IDs 且无交集；实际执行 `no_missing=false`（16 个既有未执行）、`no_overlap=true`，报告没有把 setup 未执行算成通过。expectedFailure 负例实际运行并使 child exit1；测试报告列出具体 expected-failure ID。
- 真实 npm ci 另在普通 Git clone 成功（本地 Node24.19.0/npm11.9.0，CI Node22，不能称 Node22 完整 CI）。其 ignored flatted Go 实际进入 `go list ./...`；dirty guard exit1（先观测到 `.bin/acorn` symlink），另独立 flatted 无 symlink fixture 也明确以 unknown Go 拒绝。第三方文件没有删/移/allowlist。dirty caller 的 version runner 从 clean clone 调度，失败/未执行集合与 baseline 相同，没有裸 discover 或隐藏旧测试。

## 真实运行仍红；没有扩张资格

原 `go test -json -race -count=1 -timeout=180s ./...` 已在本次独占 `/tmp/r5-archive-pg` PostgreSQL17 集群、loopback 55439 上执行，未连原 5432/真实业务。source 为 `0c1f4f6f4cda1d39952df1fe9eeda87d74f56eb9`，与最终源码的 cmd/internal/两 fork/root mod/sum bytes 完全相同；不把该历史执行改记为最终 Git HEAD。结果 exit1：架构 route inventory 失败、`TestR5KeyUsageAuthorityAdmissionIsolation/usage_held_allows_retry` 失败，postgres 包 180 秒 timeout，未完成项由原真实性 gate 拒绝。原 JSONL 与 failed gate 保留；临时 PG 已正常停止。

两 fork 的真实 combined race 命令 exit1；SMTP `TestOwnerBDATPanicTailIncludedInShutdown` 出现 `panic: close of closed channel`，日志保留，没有修 fork 或改为通过。Method19、SML、shipping、全 CI、tagged protocol runtime 和 benchmark 未取得资格；schema19/既有 workload/receipt 没改。曾在其他 Go build/test 同时修改 cache 状态时捕获一次 raw metadata drift 拒绝，随后独立 cache 的稳定 cold/warm capture 通过；未放宽 raw equality。

## 阻塞与交付

锁定 Python 依赖安装因 `/home/agent/.local/lib` 只读失败，停止该安装，没有换身份/路径；缺格式校验 dependency 与既有 schema/结构 evidence 漂移造成的 baseline 失败保留。GitHub GraphQL PR 查询返回 Forbidden，停止对应 API，没有切换 token/connector；代码正常 git push 可用，draft PR 仍待授权可用的父级流程创建。建议 stacked base `ci/company-mail-r5-wiring-20261003`，此分支精确来自 PR21 head。draft 文案在新证据目录。

当前工具 catalog 未暴露 `cloud_threads.send_message`，搜索无结果，无法发送父级要求的中途 tool 消息；源码 checkpoints 已正常 commit/push，最终回复提供 fixed head。没有权限请求循环或绕过权限。
