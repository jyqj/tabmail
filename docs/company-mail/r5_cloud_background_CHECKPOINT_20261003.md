# R5 cloud background：真实 Router synthetic HTTP 限定验证

状态：本块测试与 checkpoint 完成，等待父集成；中央 TODO 不修改，10/171 不变。

固定起点 `jyqj/tabmail work/company-mail-r5-goal-20260930@93005b644c1f39fa85072fe913156cf3d6c798e1`。fetch 后建立独立分支 `r5-cloud-background-20261003`，未在旧 R4/main 开发。测试源码提交 `ca77a8a1936dbc1ae3a3f28c12d5a1812119fd1b`，tree `a5a4f7f185a706fdba2a03b840644feebda36950`。交付只新增 `internal/api/r5_cloud_background_http_test.go` 与本 checkpoint/具名前缀证据；无生产、router、main、DTO、store、迁移、依赖、CI 或中央 TODO 修改。

## 父任务映射（不能据本块关闭）

| 父任务 | 本块证据 | 仍待父集成/验收 |
|---|---|---|
| RES-SHUTDOWN-FORMAL-CALLERS | shipping Router.ServeHTTP 的请求/child join port：关闭 admission 不撤销已准入 live lease，timeout 不释放真实任务 | main 正式调用链、有界停机预算、错误传播、依赖关闭顺序与各角色真实 join |
| R5-P8-020 | 已有 router 实例 owner 真实装配，Login/Auth/SSE 共用生命周期 | 全部启动依赖与生产角色装配 |
| R5-P10-110 | synthetic API admission/drain 子范围 | SMTP/outbound/index/retention/readiness/heartbeat、重启与 claimed 任务证据；依赖任务未闭 |
| R5-P0-050 | 每次实际 run/pass/fail/skip/unfinished 与 source/commands 可查 | 仅本块名单，不完整发布门禁 |

## 有用发现与实际范围

- 没有确认 owner 生产缺陷，无 owner 重造或修复。现有 `Go` 同步注册、live lease 验证、StopContext 保留 owner 与 RestoreRequestLease 在本范围符合契约。
- `TestR5CloudBackgroundHTTPAdmittedLateChild/key,login`：真实 API-key ResolveAPIKey / Login GetUserByEmail 屏障；先过 admission 后关闭 admission，并让 StopContext 用已过期原 deadline 返回。随后放行 handler，实际 TouchAPIKey/TouchUserLogin child 仍可注册；HTTP 返回且请求取消后 child context 仍有效，Done 仍未关闭，直到 task 实际返回才 drain。同一路由闭 admission 后 health/ready/auth/SSE 均 503，身份 store 计数无新增；旧已释放 nested lease 503。
- `TestR5CloudBackgroundHTTPSSERevalidationLease`：在真实 router 的初始 Auth GetUser 屏障关闭 admission，再放行，真实 monitor SSE handler 的首次 revalidation 仍执行 uncached GetUser 并到达 journal read。由此取得的已恢复 stream context 在 admission 关闭后还能连续两次通过同一 shipping RevalidateRequest；每次确实读取 store。foreign router 拒绝这个 live lease。Flush 屏障维持真实 SSE body；返回后 revalidation 与旧 handshake context 均被拒绝，且不触达 GetUser；晚 drain 不抹掉原 timeout。
- Auth 包住完整下游 handler，所以初始 handshake lease 在 SSE 返回前仍 live，可以做 nested entry。第一版 fixture 把它错判为 released，收到正确 200 后失败；只修 fixture 期待，未调整产品行为。此次失败及精确源码 snapshot 保留，不能称为生产 red-green。

所有 store、账号、token、域名均为自有 synthetic（`.test`）；无真实邮件、生产 DB、外部 SMTP、凭据修改或现有服务访问。HTTP 使用真实 Router.ServeHTTP 的 httptest request/response；不是 loopback TCP 证据。进一步两次 revalidation 显式调用 shipping revalidator，未等待/加速五秒 ticker，也不声称后续周期 poll 的全链覆盖。

## 执行与结果

使用现有官方 Go 1.25.7 linux/amd64、go.mod/go.sum、本地正常 module cache，`-mod=readonly`；未替换依赖。未 source 环境启动脚本或接入其 DB/Redis。最终命令：

```sh
env PATH=/workspace/tabmail-cloud/tools/go/bin:$PATH \
  GOMODCACHE=/workspace/tabmail-cloud/gomod \
  GOCACHE=/workspace/tabmail-cloud/gocache \
  GOPATH=/workspace/tabmail-cloud/gopath \
  GOFLAGS=-mod=readonly GOMAXPROCS=2 \
  go test -race -p 1 -count=1 -json \
  ./internal/api ./internal/api/lifecycle ./internal/api/middleware ./internal/api/handlers \
  -run '^TestR5(CloudBackgroundHTTP|APIBackgroundShutdown)'
```

| run | 源码 | 顶层 / nodes started | pass / fail / skip / unfinished | exit / 资格 |
|---|---|---|---|---|
| candidate | 固定 base + 首版新测试，编译期间 gofmt、未捕获精确字节 | 2 / 4 | 4 / 0 / 0 / 0 | 0，探索结果，不用于精确 source 资格 |
| fixture-fail | 固定 base + test blob `9174ff191b61ab7372bafd8a7fb4b410e6d4373a`；完整 snapshot 留证 | 27 / 31 | 30 / 1 / 0 / 0 | 1，fixture 误判 live handshake |
| final | 上述 `ca77a8a` / `a5a4f7f` 的全部测试/生产源码 | 27 / 31 | 31 / 0 / 0 / 0 | 0，仅本范围合格 |

最终四个 package 全 pass：api 7 top/9 nodes，lifecycle 8/8，middleware 8/8，handlers 4/6。新测试为 2 top/4 nodes；其余为原有限定背景回归，不借 8 lifecycle green 宣称真实 router 外的装配覆盖。之前进度消息的“29 顶层”计数错误；解析 go-test JSON 后确认为 **27 顶层**。没有0用例或 skip 计通过。

首个裸 `gofmt` 命令 PATH 中不存在，随后使用现有官方 toolchain 的完整路径格式化；这不是 Go 测试失败。最终 run 在格式化、fixture 修正后执行，期间源码不变。源码提交只提交该已测文件；后续提交仅保存本 checkpoint/证据，无额外 Go 变化。

证据目录：[r5_cloud_background_results.json](evidence/r5_cloud_background_20261003/r5_cloud_background_results.json)，含每次 source、完整命令、计数与测试 action 事件文件。事件保留 start/run/pass/fail/skip 和 fixture 的相关错误行，不复制无关原始输出；失败 snapshot 可按 Git blob 重验。`git diff --check` 通过。

未运行大 PG、性能或完整项目门禁；不证明 PG touch durability、main shutdown、外部依赖生命周期、实际慢 SMTP、浏览器、Docker、shipping/CI、完整权限矩阵或171父任务完成。父集成后再回填中央 TODO；禁止 merge/force-push/deploy。
