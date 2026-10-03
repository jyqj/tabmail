> 当前第四批SOURCE资格以 [batch4回执](R5-INTEGRATION-BATCH4-20261003.md) 为准，freeze `aeed47f0e1998d9925acb680992b051463821d69`。原下文保留第三批历史范围，不能转授第四批wholeCI/runtime。第四批仍blocked：prepared full版本FAIL、root PG/audit FAIL；10/171保持。

# R5 当前 SOURCE 资格边界 — 第三批整合

本轮固定 base 为二批 `b4803fe6722ba05b83799459e88d4a014f546da2`，新分支 `integration/company-mail-r5-batch3-20261003` stack 于二批；二批又依赖首批 integration，未改 R5 工作分支。组合源码冻结点 `c808fbe1699804d2f7f2c4de08f31975950e3f36`，tree `2ed953a6d9e51c892e45b7fad311ca45a47dcd9d`。本文件和本轮证据是当前解释；历史 snapshot、失败、skip 与回执保持原字节。

仅整合 selected-attestation 输入 head `529f54782f0cfc90ed2501c253723a4fd88497e0`（production freeze `9d53d54`，基于 `64cd118`，保留 `8de4809` 实际失败证据）和 descriptor 输入 head `cb523295afc132869310d36d8de4d1fde25f32f4`（实现 `fe7a751`）。三方合并同一 inventory script，selected helper 与输入 head 字节一致，`_check_excluded_metadata` 函数与 descriptor 输入字节一致，没有整文件覆盖。980 个前端/API/SMTP/fork/依赖/workflow 等已跟踪输入与二批 base 字节相同，详 [pins](evidence/R5-INTEGRATION-BATCH3-20261003/pins.log)。

## 实际组合验收

- 原 independent metadata oracle 从 `30c25c57402a92f7126cb25ad77813fec811e470` 取出，SHA256 `e9d8c6f413754452227cb0df249f98ce571107088af0384a57d12f264d26bcca` 与本组合 oracle 一致，实跑 **9/9 PASS**，含原 regular-to-symlink API/CLI capture/validate 8 子例。
- Descriptor suite **12 方法 PASS**（继承原 9 方法，加 96 个有限 API/CLI fault 子例、metadata handle/no-read 与 budget 检查）；source inventory **38 PASS**；v3 boundary negatives **5 PASS**。旧 excluded suite **4 PASS**；唯一 chmod(0) positive 方法按本任务禁止权限变更没有运行，原测试未改；其旧 name-only open guard 也不接受新 O_PATH 元数据句柄，不能称该旧全套绿。
- Selected suite 在原 `/workspace/tabmail` 为 **8 个 negative 方法 PASS / actual-root setup ERROR**：现存未跟踪 `web/node_modules/flatted/golang/pkg/flatted/flatted.go` 被拒为 unclassified。未移动或忽略该输入，实际失败日志保留。独立 standalone checkout `/workspace/tabmail-batch3-selected-checkout`，独立 `.git`、相同冻结提交、无已安装 node_modules，实跑 **12/12 PASS**；另 fresh capture 与 fresh recapture validation PASS。这是明确不同的 checkout 环境，不抹掉原环境拒绝。
- Ordinary/inspection/alias pure contract 首轮 **64 方法：63 PASS / 1 合法 fresh-fixture SKIP**；原 typed exporter 用 Go1.25.7、实际 `GODEBUG=asynctimerchan=0`、原 mod/sum 与 readonly，产生 21 个 synthetic wire fixture 后完整复跑 **64/64 PASS**。首轮日志保留；未重跑二批 UI 全套，也不转授本轮 UI/HTTP/PG 资格。

## 当前清单与身份

[本轮 attestation](evidence/R5-INTEGRATION-BATCH3-20261003/attestation.log) 是 canonical JSON；[capture summary](evidence/R5-INTEGRATION-BATCH3-20261003/capture-summary.json) 与 [组合回执](evidence/R5-INTEGRATION-BATCH3-20261003/receipt.json) 记录实际范围。新独立身份 `r5_root_selected_local_attestation_v1` 不兼容旧 v2/v3；历史 v2/v3 只保其原 directory-superset 资格。

父裁决方案 A：保完整 `./...`、两 fork 原 pattern 和实际 **21 个静态 evidence Go 输入**，逐项由 helper 的固定 `STATIC_EVIDENCE` 授权；没有挪走、忽略或按 metadata 扩大读取 authority。实际输出 **630 package records / 613 unique selected local inputs / 138 root-MVS modules / 60 generated testmain records**；613 与旧 611 的差异来自二批增加的 API/SMTP 测试，数量由真实 metadata 决定。完整 selected path/Go 字段/body hash、script hash、root identities、flags、MVS 与命令 stdout hashes 均绑定并在捕获前后稳定匹配。

真实工具 `/workspace/tabmail-cloud/tools/go/bin/go` 为 Go1.25.7 linux/amd64，CGO=1、GOAMD64=v1、race=true、tags=r5protocol、GOWORK=off、GOENV=off、GOTOOLCHAIN=local、GOFLAGS=''、GODEBUG=asynctimerchan=0。只执行固定 `go env -json`、`go list -mod=readonly -m -json all` 和 `go list -mod=readonly -deps -test -race -tags=r5protocol -json ./... github.com/jhillyerd/enmime/v2/... github.com/emersion/go-smtp/...`；不增加 `-buildvcs=false`。原 mod/sum、root-MVS 两 replace：enmime/v2 v2.3.0 与 go-smtp v0.24.0 本地 fork 保持。

Descriptor 的 regular artifact 仅用 Linux `O_PATH|O_NOFOLLOW` 类型/device/inode 句柄，不读 artifact 正文；目录通过独立 identity 匹配的 no-follow directory FD 枚举，unsupported primitive/platform fail closed。每 fork/pass 4096 entry、32 depth、12 exclusions 保持。有限观察点能拒绝原 race，不能证明原子敌对 FS snapshot、最后观察之后的变更或两观察点间已恢复的瞬态变化。

上游 selected receipt 的 `overall=blocked` 与 `excluded_metadata_boundary=blocked_pending_separate_helper_race_fix` 是冻结实现中的保守标签，本轮不改 payload 语义或重写历史 receipt。实际 descriptor 组合验收已通过；该标签不是本轮失败断言，也不是可升级整体资格的票据。Generated testmain、external modulecache、toolchain/native/compiler 未验证字节各自 **unknown**；本轮静态本地绑定不授 Method19、Catalog/SML、百万 M、wholePG、四 CI、shipping 或 runtime。

**10/171 未变化，父任务未关闭。** 旧 wholeFAIL/skip、百万 M FAILED、私有原件缺证与被拒 watchdog 保留。无真实邮箱/PG/Redis/外 SMTP、watchdog runtime、秘密/权限修改、merge、forcepush 或 deploy。交付状态按本轮实际 Git push、连接 GitHub PR API 与固定 head CI 读取分别记录；任何拒绝都不换身份/remote/路线重试。

交付实况：新分支首次普通 push 成功，固定 `504b59731adb219c41ed685cd2029759f3c116d1` ls-remote 确认；连接 GitHub API 首次创建 [draft PR #19](https://github.com/jyqj/tabmail/pull/19)，base 精确二批 b4803fe。该 head CI 读取成功但 0 workflow runs / 0 statuses / 0 check runs，原 main-only workflow 未变，四 CI 未验。该 docs/evidence head fresh validation 与冻结 attestation 匹配；后续只本交付状态 docs-only commit/push，不改变冻结源码资格。
