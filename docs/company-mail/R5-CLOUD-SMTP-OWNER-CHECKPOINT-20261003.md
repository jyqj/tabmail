# R5 cloud SMTP owner checkpoint — 2026-10-03

独占工单：RES-SMTP-CONTEXT-SESSION-JOIN 的新增 loopback 测试；生产代码只读。实施起点为 `93005b644c1f39fa85072fe913156cf3d6c798e1`，独立分支 `r5-cloud-smtp-owner-20261003`。云默认旧 R4 未用于开发。仓库仅发现 `web/AGENTS.md`（已读，本块无 web 写入）；已读 VERSIONING、R5-TODO、fork provenance 及独审报告。用户指定固定 R5 起点和 PR base 优先于文档的默认 main 流程。

## Findings and bounded acceptance

固定源码已有 context cancellation、connection admission/owner、BDAT creation-to-tail join、Logout join，未依据旧 TODO 重开发。`go list -mod=readonly -m -json github.com/emersion/go-smtp` 确认 v0.24.0 使用起点已有 `./third_party/go-smtp`；上游 identity 为既存 UPSTREAM-MANIFEST。未更改依赖、fork生产实现或历史 source-only 报告。新增测试不包含在旧 LOCAL-SOURCE-MANIFEST/LOCAL-PATCH 中；本批新测试 hashes 见独立 receipt，旧清单仅描述其原版本。

实际 Go 1.25.7/linux amd64、GOMAXPROCS=2、readonly/p1/race/count1/90s：

| final scope | top | leaf | test nodes passed | fail | skip |
|---|---:|---:|---:|---:|---:|
| internal/smtp `^TestR5Cloud` | 3 | 6 | 9 | 0 | 0 |
| fixed fork `^TestOwnerR5Cloud` | 1 | 1 | 1 | 0 | 0 |

根用例使用真实 `127.0.0.1:0` TCP、合成 Session、channel 屏障，无 Sleep；DATA/BDAT 各检查部分正文慢读被 transport close 中断、deadline 取消 work context、未释放 Data 时无 Logout/drained；观察最终250后断线仍等待 Logout 尾部；成功决定在最终回复之前断线，则 Data/Logout 两个门分别阻止 drained。最后 bounded Shutdown 成功才检查真正 drained。fork 用例用 synctest/内存 pipe，验证第二次 Shutdown/Close 的 ErrServerClosed 不是首个 retained join 完成证明，Logout gate 阻塞至释放。

本范围未产生生产缺陷反例。非合作 Data/Logout 在 deadline 后继续持有 owner 是诚实 incomplete，不应改期待以制造假 drain。收到 250 的用例只表示合成 backend nil 和客户端协议观察；回复未观察的用例只表示合成成功决定，不能认定业务持久化、uncertain 分类或重试安全。

[receipt](evidence/R5-CLOUD-SMTP-OWNER-20261003/receipt.json) 保留固定 base/tree、最终文件 pins、每次 commands/exit/start/pass/fail/skip 与本块 synthetic JSONL/stderr。初次根 run 为2top/4leaf/6nodes P，随后新增回复未观察场景并收紧 join watchdog，最终3top/6leaf/9nodes独立重跑；初次测试 bytes 未独立冻结；从记录的原始写入/最终差异重建 initial-test-source.go.txt 供审阅，不冒称独立pre-run attestation，明示不计最终 acceptance。fork未改测试再复跑。日志只有本块合成数据，不含真实邮件/DB/凭据或无关日志。

## Parent task mapping and not run

- `R5-P10-110` / RES-SMTP-CONTEXT-SESSION-JOIN：仅 SMTP wrapper/fork 协议 owner 的 bounded acceptance；main/formal callers 的 deadline/error传播、真实 PG/Redis 在未drained时不得关闭仍 NOT_RUN，属父整合。测试没有把伪造的 dependency close 函数当正式装配验收。
- `R5-P6-040`：此处验证入站 wrapper context cancellation，不覆盖出站 relay/direct/TLS/MX/QUIT，父任务保持未完成。
- `R5-P6-050`、`R5-P6-100/110/150/160`：持久账本、接受原件/事务、lease/retry/uncertain、PG/object故障集成与G6均 NOT_RUN。
- 生产 Mail/Rcpt/Data 的完整业务 context链、TLS/LMTP、完整 fork upstream/root SMTP旧集、whole CI、正式 shutdown、OS containment、性能/容量及公网均 NOT_RUN。新 race测试不是完整SMTP可靠性结论。

中央 TODO 仍10/171，本 checkpoint 不勾父任务；由父集成后回填。仅测试和此独立证据/checkpoint写集；未改main/router/共享DTO/store/迁移/CI/依赖，未 merge/forcepush/deploy。
