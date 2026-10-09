# 双 fork v3 独立 pure / SOURCE checkpoint — BLOCKED

固定 source `93005b644c1f39fa85072fe913156cf3d6c798e1`，handoff/base `29a388a3c7d6da03641abe79bb5c728b90b75273`。独立 worktree，不基于浮动 integration；二者间 go.mod/go.sum、fork、inventory/test 无差异。只新增 scripts/tests 测试与证据，不改生产、前端、manifest 政策或中央 TODO；10/171 保留。

## 实测结果与回父决定

- 现有 v3 pure16：16 PASS；历史 current v2 15 PASS、protocol source 7 PASS，仅各自纯域。
- 新增极小 pure5：3 个方法 PASS、2 个方法 FAIL（两 fork 各失败一次，共 4 failure subtests）。无 expectedFailure/skip 或放宽断言。`build/` 排除目录内部的未知 nested go.mod 与 symlink 未被拒绝；排除目录自身为 symlink 时会拒绝。`_walk` 在检查后代前剪枝，`_check_go_inputs` 因此看不到 nested。此为边界政策待裁决项，未擅自修改 EXCLUDED_DIRS 或扩大清单。
- 未编辑 handoff tree 的 SOURCE capture/validate 成功：1,028 文件；enmime 219、SMTP 29；源码闭包 `fa96623c4139236dc811108116f280ce12384c49`。`baseline-source.json` 明确是新增测试/本 checkpoint 之前的基线 receipt，不宣称最终提交的全树身份。其 snapshot_root 为本次隔离工作树；复用须重新 capture，不能改路径后冒充原 receipt。
- 实际 root MVS metadata：138 modules，恰好两个 module/version/path replace；根 mod/sum 字节未变。根选出的 x/text 为 v0.40.0，而 enmime 独立 go.mod 声明 v0.34.0，不能使用 nested 独立图代替根 MVS。
- 首次 Go-selected metadata 查询 exit1，VCS status exit128，输出 0 字节。另一次明确 `-buildvcs=false` 的诊断查询 exit0：630 package records；590 个唯一已选本地输入逐字节匹配 baseline source；60 条生成 testmain 记录另列。**21 个已选历史 evidence Go 文件不在 SOURCE 清单中**（具体 path/hash 见 review.json）。因此根 `./...` 的完整 SOURCE/selected 绑定 BLOCKED；不能用诊断 exit0 宣称准入。两个 fork 的已选实际源码在清单内。
- 双 MIT LICENSE、完整 regular-file fork inventory、upstream declared filename coverage、provenance、embed、根 mod/sum 均被 capture 绑定。upstream 修改文件 hash 可不同，清单不等于重验签名或 patch 正确性。private capsule unavailable/pending evidence，未访问或补造。

需要父/政策 owner 决定：如何在保留 artifact 排除的同时检查后代 nested/symlink；如何处理 `./...` 会选择的历史 evidence Go packages。此提交仅固定缺口，不推当前源码全绿。

## 实际命令

工作目录 `/workspace/r5-source-v3-pure`。Go `/workspace/tabmail-cloud/tools/go/bin/go` 实测 `go1.25.7 linux/amd64`。所有 Go 查询显式环境：

```sh
env GODEBUG=asynctimerchan=0 GOWORK=off GOENV=off GOTOOLCHAIN=local GOFLAGS= \
  GOPATH=/workspace/tabmail-cloud GOMODCACHE=/workspace/tabmail-cloud/gomod \
  GOCACHE=/workspace/r5-source-v3-evidence/gocache \
  /workspace/tabmail-cloud/tools/go/bin/go list -mod=readonly -m -json all
```

相同环境的首次 selected 命令：

```sh
go list -mod=readonly -deps -test -race -tags=r5protocol -json ./... github.com/jhillyerd/enmime/v2/... github.com/emersion/go-smtp/...
```

诊断命令仅另加 `-buildvcs=false`。`-test` 为 test-input metadata，未运行 Go test。capture 使用 protocol context：linux/amd64、cgo=1、race=true、tags=[[r5protocol]]、work=off、flags=''、all_local_variants_superset；调用脚本自身 capture/validate API。原 receipt 的 MVS/selected boolean 保持 false，实际查询证据单独登记。

```sh
python3 -m unittest discover -s scripts/tests -p test_r5_smtp_owner_source_inventory.py -v
python3 -m unittest discover -s scripts/tests -p test_r5_source_v3_boundary_negatives.py -v
python3 -m unittest discover -s scripts/tests -p test_r5_current_source_inventory.py -v
python3 -m unittest discover -s scripts/tests -p test_r5_protocol_source_inventory.py -v
```

原始日志、完整 root-MVS/diagnostic selected JSON gzip、基线 receipt、逐文件绑定与证据 hashes 均在同目录。只包含仓库源码/合成 fixture/metadata，不含真实邮件、controller/PG日志/DB/object。

## 独立资格账

SOURCE：capture 有限成功，但新增边界负例 FAIL。GoList：根 MVS 成功；原 selected VCS 失败；禁用 stamping 的诊断成功但覆盖绑定失败。implicit build：Go-list 可产生依赖/cgo/cache metadata，未使用 `-compiled`、未取得编译资格。cold build：NOT RUN。runtime：NOT RUN。external cache：复用既有 cache 并下载 test metadata，不是 fresh 全字节证明。timer：查询进程显式 GODEBUG pin，不是产品 runtime 行为验证。

没有 PG/watchdog/PGphase、fork race 实际执行、SMTP/HTTP、性能规模、Method19/SML、全后端或 CI/shipping 资格；无权限改动、merge/deploy。原失败/unknown保持。
