# R5 source inventory excluded metadata repair

固定独立基线 `8de4809516a39ad43b0e17afa288bcedab8b40a7`（`r5-source-v3-pure-isolated-20261003`）；生产 R5 source `93005b644c1f39fa85072fe913156cf3d6c798e1`。此分支依赖该双 fork SOURCE v3 / 独立缺陷证据提交，不依赖浮动 integration。仅修复 inventory 边界及该边界专属测试、checkpoint；中央 TODO、Go、mod/sum、fork、前端和生产合同未改。

## 边界与政策

现有 v3 已规定拒绝 symlink/未知 nested module，完整 regular-file fork 清单同时明确排除 artifact 目录。现在仅在两个精确 local replacements 的排除子树做 names / no-follow stat 类型检查，使用 directory FD 和 O_NOFOLLOW 逐层下降。不解析 nested go.mod，不打开 regular files，不读取任何 artifact / secret 正文，不将这些内容或文件名加入 SOURCE。普通多层 artifact / node_modules / cache 仍排除；symlink（文件、目录、悬空）、go.mod、特殊/未知类型和无法读取的 metadata 一律拒绝。遍历采用流式迭代，每个 fork 每轮全部排除子树共限 4096 条目（含排除根）、32 层（含排除根），超限失败，绝不截断成功。capture 两次路径核查和 validate recapture 均执行检查。

沿用 schema3 的理由是落实已有拒绝边界，未扩大可准入的输入或改为收集 artifact。receipt 增加明确的 metadata inspection/拒绝规则/预算；既有 inventory_implementation_sha256 严格绑定实际加载 helper 和 snapshot helper 字节。旧 v3 receipt 无法静默继承资格：receipt 内容、helper hash 与 recapture 均需精确一致，必须重新 capture。v2 历史单 fork 域保持原实现。实现不构成敌对并发文件系统的原子快照证明；metadata 消失/无权限、stat 后目录替换为 link 均 fail closed。

## 回归证据

- before-pure5.raw.log：改动前原 pure5 实际 FAIL，2 个方法 / 4 个 fork subcase failures；未改成 expectedFailure 或 skip。原独立 checkpoint 的 raw FAIL 保留原样。
- test_r5_smtp_owner_source_inventory.raw.log：原 pure16 全 PASS。
- test_r5_current_source_inventory.raw.log：历史 current v2 15 PASS。
- test_r5_protocol_source_inventory.raw.log：协议 source 7 PASS。
- test_r5_source_v3_boundary_negatives.raw.log：原新增 pure5 全 PASS，原 4 failure subcases 转绿。
- test_r5_source_excluded_metadata.raw.log：新增 5 方法 PASS；覆盖所有 EXCLUDED_DIRS 的普通 artifact/合成不可读 .env.secret、两 fork 多层 nested/link/FIFO/socket、未知/不可读 metadata、目录替换 link、实际预算边界与超限。subprocess 执行实际 SOURCE CLI capture/validate，验证 receipt pin、loaded helper hash 和 snapshot helper drift，未只测私有 helper。

以上共 48 方法 PASS，只运行 Python pure SOURCE/小型自有 temp fixtures，没有 Go/build/runtime/PG/邮件/SMTP/watchdog。下面的实际仓库 CLI 同样只有 SOURCE capture/validate，结果和 helper hash 在 cli-summary.raw.log；完整新 receipt 在 cli-capture.raw.log（.log 是 scripts artifact 排除域，避免 receipt 自引用）。它不是原独立基线 receipt，snapshot_root 固定实际工作树，复用必须重新 capture。

```sh
python3 -m unittest discover -s scripts/tests -p test_r5_smtp_owner_source_inventory.py -v
python3 -m unittest discover -s scripts/tests -p test_r5_current_source_inventory.py -v
python3 -m unittest discover -s scripts/tests -p test_r5_protocol_source_inventory.py -v
python3 -m unittest discover -s scripts/tests -p test_r5_source_v3_boundary_negatives.py -v
python3 -m unittest discover -s scripts/tests -p test_r5_source_excluded_metadata.py -v
python3 scripts/r5_source_inventory.py capture --root /workspace/tabmail --purpose protocol \
  --policy r5_current_local_inputs_smtp_owner_forks_v3 \
  --build-context '{"goos":"linux","goarch":"amd64","cgo_enabled":1,"build_tag_sets":[["r5protocol"]],"race":true,"go_work":"off","go_flags":"","selection":"all_local_variants_superset"}'
python3 scripts/r5_source_inventory.py validate --root /workspace/tabmail --purpose protocol \
  --policy r5_current_local_inputs_smtp_owner_forks_v3 \
  --receipt scripts/tests/checkpoints/r5_source_excluded_metadata_20261003/cli-capture.raw.log \
  --receipt-sha256 <actual receipt byte hash from cli-summary.raw.log>
```

## 仍 BLOCKED 的独立范围

原 root ./... 选出的 21 个历史 evidence Go 文件遗漏未修，原 MVS/selected 证据及其 failure/unknown 保持原样；完整 SOURCE/Go-selected 绑定仍 BLOCKED。effective_root_MVS_observed、go_selected_inputs_observed、external_module_cache_verified 保持 false。不声称 Method19/SML、whole backend、运行时、性能、产品全绿或 shipping 准入。没有 merge/deploy；完成此缺陷即停。
