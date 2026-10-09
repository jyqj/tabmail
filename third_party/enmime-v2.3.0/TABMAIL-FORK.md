# enmime v2.3.0 本地预算 fork

## 来源与差异

本目录完整保留 upstream Go module archive 的 213 个文件及原 MIT LICENSE。
它是 James Hillyerd 与 upstream contributors 的源码，不是 tabmail 原创实现。
身份、完整原件 SHA-256 manifest 及所有差异分别见 `PROVENANCE.json`、
`UPSTREAM-MANIFEST.json`、`UPSTREAM-DELTA.patch`。

仅 upstream `part.go` 被窄改；新增 `parse_budget.go` 与专用测试。
`boundary.go`、header/charset/transfer decoding、`envelope.go`、原测试与 fixtures
均未修改。现有 `ReadParts` / `ReadEnvelope` 传 nil guard，不读取 context 或 clock，
不额外解释 header。Parser / Part 结构与原公开 API 不变。

## 有界调用

`ReadEnvelopeBounded(ctx, reader, ParseLimits)` 在真实 root/child `&Part` 前计预算，
在实际 `setupHeaders` 后、正文 decode/递归前计原 attachment+inline 投影。
每次调用独占状态；ancestor reader 清除临时 EOF 后发现的新节点仍经过真实分配守卫。
拒绝是 sticky error，malformed-part recovery 不得吞掉。Envelope 投影与 HTML 转换
直接复用原 `EnvelopeFromPart`，不创建第二棵 MIME 树。

字节读取、reader close/取消中断由调用方负责。本 fork 不宣称完整 parser CPU/heap
成本已验收。节点预算计真实分配事件（包括可被 malformed recovery 丢弃的尝试）；
公开 bounded API 使用原 default parser，未开放额外 parser-option 行为。

## 替换与维护

根 `go.mod` 仅精确 replace v2.3.0；module path 保持原值，所有现有 import/type identity
不变。fork go.mod/go.sum 原样保留，不进行 tidy；root go.sum 不预设变更。
冷依赖图必须由独立审查与唯一 executor 确认，特别是 root MVS 与 standalone fork
的传递依赖版本可能不同，不能拿 standalone 局部 PASS 替代主模块证据。

升级必须完整重导 upstream、新建 provenance/manifest、复审全部实际分配及递归点、
核对投影条件，重新应用窄 patch，先独审再由唯一 executor 执行新 budget 和 legacy
兼容 selector。不要维护或恢复被 BLOCK 的独立 boundary-slice walker。

当前只 SOURCE，尚未执行 Go 测试、构建或冷环境验收。
