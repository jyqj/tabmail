# R5 B01-L：原件下载错误传播与回复/转发权限重验

2026-09-29。**本批完成两个内容边界修复及相同测试的前后对照；未完成 B01-K 的 PostgreSQL 并发验收。全量无 DSN 的跳过仍由 backend 门禁拒绝，P0-070/080/G0 不关闭，原统计保持 6/171。**

## 1. 基线、范围与被测身份

开始时工作区干净，HEAD 为 `b67041876fc7268903ff7acdae417d4e9336f484`。本批分支 `fix/company-source-http-boundary-20260929`，最终被测 **Git tree** 为 `89a5e05efd6ddd2e71112d9f377a50b1faeec93b`，613 份源码快照文件逐一核对 SHA-256 一致。最终本地提交只追加本报告、任务/事务记录与执行证据；tree 不是 commit，提交身份以 Git 历史为准。

接手后检查到 `TABMAIL_TEST_DB_DSN` 未配置。未启动、连接或重新封装被限制的数据库动作；没有使用另一套数据库环境绕过既有安全拦截。B01-K 的六组数据库用例保留待验收。本批继续检查同一内容边界，修复可以用真实 HTTP 传输与确定性应用适配器直接复现的缺陷，而不是将无数据库运行标成数据库验收通过。

## 2. EML 下载不能把失败包装成成功

B01-K 的应用服务已经保护原件首批字节，但 `CompanyMailHandler.Source` 仍先设置下载响应头，再忽略 `io.Copy` 的错误。首读后的撤权/来源重验失败可能表现为空的 200；流中途读取失败也可能被客户端当成完整下载结束。

新增 `company_source.go`，保留原权限服务及统一应用错误映射：

- 首先读取一个有界的 32 KiB 块，通过应用服务的首批字节检查后才设置成功下载响应头。此前的 403/404/409/500 继续使用既有 JSON 错误格式；内部错误码仍是 `INTERNAL`，未创造新协议。
- 首次读取返回数据加非 EOF 错误时，不把这批不完整字节作为下载释放。合法空原件、单字节、data+EOF 和大原件保留原始字节及原下载头。
- 一旦响应已经开始，后续读取/客户端写入失败通过 `http.ErrAbortHandler` 中断传输，不在 EML 中追加 JSON，不把截断当作成功结束。仓库固定的 Chi Recoverer 会继续抛出该 sentinel；实际 HTTP/1.1 和 HTTP/2 客户端均已验证读取失败可见。
- preflight 和后续读取都限制连续 `(0,nil)` 最多 100 次；检查请求取消及非法 Reader 计数。该包装不提升 `WriterTo`，避免绕过读取边界。
- Source 仍由原 handler 的 defer 关闭。成功、拒绝、连接中断及客户端写入失败不引入长寿命数据库事务。

32 KiB 是预读块，不是整封邮件缓冲，也不是最大原件限制。正常原件下载仍最多三次既有 `GetWorkMessage` 检查，不按数据块增加数据库查询。取消检查发生在 Read 前后，不能强行中断一个完全不理会 context 且永久阻塞的对象 Reader；没有把本批表述为任意对象后端故障上界认证。已交付字节不可撤回。

## 3. 回复/转发跨解析及附件复制重验

原 Compose 先读取目标邮箱发送权，再解析来源。回复与 Reply-All 在解析期间目标资格改变后仍可能生成 payload；转发多个附件时，来源撤权/替换可能发生在前一个上传完成后，后续复制和最终 payload 仍继续。

本批让私有 `inboundEnvelope` 返回已经授权并解析的来源快照，既有 MIME 只解析一次。`sender` 复制已观察的邮箱信息并检查 context，避免适配器复用指针改变比较基准。Compose 在每个附件复制前、以及最终 payload 返回前，通过原来的源读取与目标发送端口重新检查：

1. 当前源邮件权限及原件键仍匹配原始观察。
2. 当前目标邮箱仍有发送资格，且地址未在处理中改变。
3. 请求未取消；查询失败不回退成成功草稿内容。

成功路径保留标题、Reply-To/去重规则、不携带 BCC、附件数量限制与 reserve → put → finish 协议。

**这不是跨对象事务或全有全无的批量转发。** 若首个附件已完成上传后才撤权，该 reservation 继续保留给既有留存/恢复机制；本批阻止下一次复制并拒绝最终 payload，不擅自删除可能已提交的对象，也不宣称撤回已完成的复制。

## 4. 相同测试的前后对照

将最终两份新测试文件覆盖到原始 `b670418…` 的 tracked-source 导出副本，其他基线源码不变：

| 运行 | 实际结果 |
|---|---|
| 原基线 + 最终相同测试 | 43 started，32 fail，11 pass，0 skip |
| 最终候选，定向第 1 次 | 43 pass，0 fail，0 skip；10 顶层必跑齐全 |
| 最终候选，定向第 2 次 | 43 pass，0 fail，0 skip；10 顶层必跑齐全 |
| 最终候选，定向第 3 次 | 43 pass，0 fail，0 skip；10 顶层必跑齐全 |

原基线的 32 个失败事件包含 25 个具名子场景、6 个父测试和 1 个独立测试，不是 32 个独立漏洞。三次候选共 129 个通过事件不是 129 个不同测试。

`company_source_test.go` 使用真实 companymail.Service、认证中间件与 HTTP handler，repo/对象流为合成适配器。流中断另通过本机 HTTP/1.1 与 TLS HTTP/2 服务执行，检查协商协议及客户端实际错误。它们不是 PostgreSQL 或真实对象存储验收。

`compose_boundary_test.go` 使用确定性的对象打开/上传完成回调触发权限变化，无任意 sleep；覆盖三种 compose 模式、解析期目标撤权/查询失败/地址改变、一或两个附件的源撤权/来源改变/目标撤权/取消，以及合法两附件转发对照。

早期一轮测试误写内部错误码为 `INTERNAL_ERROR`，导致修复候选仍有 2 个失败事件；核对既有 `respond.go` 后将测试纠正为 `INTERNAL`，没有修改生产错误协议。最终基线和候选已使用完全相同的纠正后测试重新运行，初始记录保留为测试开发历史。

## 5. 最终源码验收

| 项目 | 实际结果与边界 |
|---|---|
| Go build / vet，`-mod=readonly` | 均通过 |
| 新 `content-http-boundary` 套件 | 10 顶层必跑，43 事件，连续三次通过 |
| 完整 Go race，`./...`、180 秒包级预算、无 DSN | 845 started，686 pass，0 fail，159 skip |
| backend 必跑门禁 | 70 → 80 项；实际返回 exit 1，53 项未满足，不计为后端整体通过 |
| Python 验证工具测试 | 最终相同树 168 项通过 |
| 静态契约 | 16 shared Go/TS 模型、33 公司三方投影、67 操作绑定通过；61 JSON + 6 非 JSON |
| 源码一致性 | 613 文件哈希逐一核对；gofmt / diff check 通过 |
| 公共协议与依赖 | 未改 OpenAPI、Go module、npm lockfile、前端或历史数据库迁移 |

使用现有 Go 1.25.7 darwin/arm64、Python 3.12.11，独立 HOME/TMPDIR，`GOPROXY=off`、`GOTOOLCHAIN=local`，复用已有模块/构建缓存，不重新安装全局工具。测试 HTTP 服务由各自 httptest fixture 关闭；本批没有创建数据库或 SMTP worker。

定向复现命令（从对应源码快照运行，日志不可与另一轮拼接）：

```sh
# SOURCE_SHA is the exact frozen commit/tree being tested.
set +e
go test -mod=readonly -json -race -count=1 -timeout=120s \
  ./internal/api/handlers ./internal/app/companymail \
  -run '^TestCompany(SourceHTTP|ComposeRechecks|ForwardRechecks|ComposeBoundary)' > focused.jsonl
TEST_EXIT=$?
set -e
python3 -B scripts/check_go_test_evidence.py --suite content-http-boundary \
  --log focused.jsonl --exit-code "$TEST_EXIT" --source-sha "$SOURCE_SHA"
```

`TEST_EXIT` 必须保存实际 go test 退出码，`SOURCE_SHA` 必须标识对应冻结源码。完整 backend 仍执行 `./...`；定向成功不能替代缺 DSN 的数据库用例。

## 6. 未完成项与证据

B01-K 六组 PostgreSQL 并发/期限用例仍待真实前后对照；本批未重跑 PostgreSQL 的 80 响应 HTTP journey、shipping 浏览器、前端、DNS、SSE 回放、性能基准或依赖漏洞审计。真实本机 HTTP 传输测试不替代这些范围。

普通收件的逻辑期限/恢复规则未修改：P3-010/020 要求保护历史个人邮箱异常期限，并先完成可解释的映射；不能仅凭物理 GC 的保护谓词就新增读取过滤或批量隐藏旧邮件。profile/override 快照和其他跨资源锁关系仍按原清单推进。P0-070、P0-080、完整 P8-080 与 G0 没有提前勾选。

[机器结果](evidence/R5-B01-L-VALIDATION.json) 与[执行日志](evidence/R5-B01-L-LOGS.tar.gz) 保存最终来源映射、各次退出码、相同测试基线/候选和 backend 拒绝证据；初始失败保留，不覆盖 B01-K/AR07 的历史记录。归档不包含原始 HTTP 响应、源码 tar、运行环境配置或生产数据。

仅本地提交；无 push、PR、merge、release、deploy，无生产配置、员工邮件或生产数据库操作。
