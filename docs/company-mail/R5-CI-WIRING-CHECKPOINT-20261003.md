# R5 CI 接线检查点

固定输入：[PR19](https://github.com/jyqj/tabmail/pull/19) head `5ae68c2d037603cd0151e1b9188f2df4db1e45a1`，base `integration/company-mail-r5-batch2-20261003@b4803fe6722ba05b83799459e88d4a014f546da2`。新 CI 分支从该 head 派生；draft PR 仍以指定二批 integration 为 base，包含 PR19 的既有三批内容。本次 delta 独占两份 workflow、小型接线测试和本检查点，不修改产品或中央 TODO。

`company-p0.yml` 和 `review-source.yml` 的 pull_request **目标分支**精确允许 `main`、`work/company-mail-r5-goal-20260930`、`integration/company-mail-r5-*`。覆盖当前 stacked R5 PR 的 integration base；无 paths 限制或 draft 条件。push 仍仅 main，不新增任意分支或 fork push。事件为普通 pull_request，不使用 pull_request_target、dispatch 或增权。contents:read、checkout persist-credentials:false 和 review-source 的 exact head archive 保持。

按当前 handoff，仅 backend 与 browser-journey 测试 job env 增加 `GODEBUG=asynctimerchan=0`。Go 继续来自原 `go-version-file: go.mod`（当前 1.25.7），没有改版本或依赖来源。四 job、20/15 分钟限制、PG16 测试服务、完整 `go test ./...`、checker、必跑/失败/skip 门、strict npm audit、Compose/build 检查和原失败证据上传完全保留。未改 production deploy、支付、secrets 或权限。

本地实际验证：`python3 -B -m unittest discover -s scripts/tests -p test_r5_ci_wiring.py -v` **4/4 PASS**；覆盖 PR base 正/负例、push/main 边界、只读 checkout/event、四 job/版本来源/PG16/timer 设置。PyYAML 6.0.3 BaseLoader 保留 GitHub `on` 键。另与固定输入逐份解析比较：撤回仅新增分支与两个 GODEBUG 后，完整 YAML 结构与原 workflow 相同；`git diff --check` PASS。

上述是接线静态验证，不是四 CI、wholePG、shipping 或 source-attestation 验收。运行状态以新 draft PR 的 exact head GitHub API 回执为准；没有 run 不能称测试通过，任何实际 CI 红保留并交父处理。尚未执行 merge、deploy、watchdog 或性能规模测试。中央 10/171 与既有失败/unknown 不变。
