# R5 B01-G：前批验证收口与入站事务边界

2026-09-28。**B01-F 的附件预留修复已取得修复后完整验证，前批未提交文档与具名遗留资源已收口；本轮另增加三项入站真实数据库回归。** P0-070 尚有普通读取、索引及其他交叉路径，P0-080 仍未冻结，不新增整项勾选：**6/171，P0 6/12**。唯一任务入口为 [R5-TODO](R5-TODO.md)。

## 1. 真实起点与提交归属

开始 HEAD 为 `6ef485951f464d0e2c92413a5ec43feb0b14aa1e`，分支 `test/company-mail-r5-b01f-reservation-gc`。工作区有前批的 3 个已修改文档、6 个未跟踪报告/证据文件。先保存并核对全部 9 个文件的 SHA-256；新建 `test/company-mail-r5-b01g-validation-recovery`，没有 stash/reset 或覆盖既有成果。

- `30e10f85bf2e5fa361c4915d8c533297a038d167`：将前批 9 个文件原样提交。B01-F 报告中的“尚未提交/清理未完成”描述的是 F 结束时的历史状态；新的完成事实在本报告与活跃 TODO 登记，不改写原始失败证据。
- **`8ed18662dc7c1ea08525a42175fcd077717a09f0`**：本轮三个入站测试及必跑名单；树为 `05d3550aa9fb68cca8933bae8811f6e3f20a847a`。

**本轮没有新增生产行为修改。** F 的 ReserveMailAttachment 修复、D/E 的入队/草稿/附件完成修复均保留。13 个历史迁移、Go 模块、前端依赖锁文件逐字核对不变。main 与 fetch 后 origin/main 仍为 `d6d512172fb6b874c3283d9df3f4d56758684a13`。

## 2. 前批资源与验证阻塞如何收口

重新读取精确 ID 及 `tabmail.validation.session` 标签，而不是沿用旧状态：F 测试容器已经退出，实际 exit=137、OOMKilled=false；F PostgreSQL 已不存在，internal 网络已无 endpoint。正常删除已退出容器及空网络，并逐个确认这三个具名资源均不存在。没有由 exit=137 推断具体故障原因，没有重启全局 Docker、系统级 kill 或操作其他项目。

新测试使用同一工具镜像与 PostgreSQL 镜像 ID、同一 Go 1.25.7、真实新建数据库和 internal 网络，无宿主端口。不同点是将 git archive **复制到容器本地 /src**，并使用 **容器本地 GOCACHE=/var/cache/tabmail-go**；只读挂载已准备的 Go 模块缓存。GOPROXY=off、GOTOOLCHAIN=local、-mod=readonly 与校验保护保留，不重新解析依赖，不挂载工作区根目录或生产配置。

这套执行布局本轮正常完成，已写入 [testenv 说明](../../scripts/testenv/README.md)。**此前 Docker/链接超时的根因仍未建立，不能把本轮成功说成宿主故障已经诊断修复。**

首先在继承源码 `30e10f85` 上实测：新增 Reservation/Reference **10/10 通过**；完整后端 **576 通过、0 失败、1 明确 browser skip**，**41/41 必跑通过**；原 27 项定向各 3 次，**81/81 通过**。这些成绩终于补齐 F 的缺口，但没有被拿来替代本轮新测试所在提交的最终回归。

## 3. 新增三项入站回归：验证已有机制，不杜撰新修复

新增 `internal/store/postgres/r5_ingress_transaction_test.go`。每项复用 seedCompany/testpg 创建独立数据库，直接调用生产 CreateIngress、ClaimIngress、DeliverIngress，使用实际锁等待、数据库时钟和数据库最终状态。没有替换 SQL、租约判定或写入结果。

### 3.1 等待租户锁期间租约自然到期

`TestR5IngressLeaseExpiryAfterTenantWaitRollsBack`：合法领取后设置近端租约截止，控制事务只锁住租户行；实际 DeliverIngress 已取得 receipt/target 锁，并被观察到等待 tenant。确认事务开始于截止之前，再观察数据库时钟越过截止；控制器不修改任何业务行，最后释放租户锁。

实测旧 worker 返回 `ErrIngressClaim`、没有报告投递成功。独立检查消息、日配额、邮箱计数、目标进度、审计、outbox、触发器派生的索引任务均无半提交；接收回执继续保护原件引用。重新领取产生不同 token，新 worker 成功一次，同 token 重放不再次创建消息或增加额度。

这是对已存在提交前租约重验的实际验证，不是本轮新增租约机制；也不表示 SMTP 到最终收件端 exactly-once。

### 3.2 必要审计失败回滚已写入的状态

`TestR5IngressAuditFailureRollsBackCheckpointAndQuota`：仅在该临时数据库给 audit_log 增加测试约束，拒绝 message.received 审计。生产事务此前已经执行的配额、邮箱计数、消息、目标 checkpoint 及索引触发器写入必须全部回滚。测试要求实际 SQLSTATE=23514，环境故障或任意其他错误不能替代该故障。

实测所有对应数量归零、目标仍 pending/attempts=0。移除该约束后，原有效 claim 可以正常投递，数量与进度各增加一次。相较已有“messages 插入时失败”的回归，这一用例覆盖更晚的必要审计阶段。

### 3.3 后一个目标校验失败不能留下部分接收

`TestR5IngressAcceptanceRollsBackEarlierTarget`：第一个目标合法，第二个目标地址与固定邮箱身份不匹配；CreateIngress 返回 BadRequest，接收 job 与已插入的前一个目标都必须回滚。改正目标后重新提交，两份固定 mailbox ID 完整保留。

此项验证同一接收事务中后续目标失败的原子性，不声称运行了目标删除竞态、SMTP DATA 会话或真实对象 Put。原件持久化与进程崩溃仍由相应后续任务验证。

三项各自首次重复三轮 **9/9 通过**。已将三项加入既有 required_go_tests，名单由 41 增至 44，不另建成功判定标准。

## 4. 最终提交的独立完整回归

测试前将容器中的 **539 个文件逐一对照实现提交的 Git blob**；测试后再次逐一检查 SHA-256。最终证据的 source_sha 为真实实现 `8ed18662...`，不再只用父提交代指新代码。测试后仅新增报告、说明、进度与证据；这些文档不冒充已经包含在此前受测树中。

| 检查 | 最终实际结果 |
|---|---|
| Go build / vet | 均通过，分别保存退出码与日志 |
| 完整 Go race + 真实 PG | **48 个包，579 pass / 0 fail / 1 明确 skip** |
| 后端必跑名单 | **44/44 通过** |
| 锁/并发/附件/引用/入站定向 | **30 项各 3 次，90 pass / 0 fail / 0 skip** |
| Python 工具回归 | **57/57 通过** |
| DTO 字段检查 | **16 个共享模型、32 个公司 DTO 通过** |
| 原 A01–A07 DB/HTTP 基线 | 七个精确目标失败再次命中，驱动报告 product_fixed=false，单独保存 |
| 源码/历史迁移/差异 | 受测字节与实现提交匹配；13 个历史迁移不变；diff 检查通过 |

唯一普通后端 skip 是 `TestR3BrowserJourney`。**没有运行 shipping-image 浏览器、前端 Vitest/TS/lint/build、GitHub Actions、当前依赖安全审计、S/M 负载或 A01/A02 原始编辑器联动。** 没有把旧前端108通过数转记到本轮，没有将原七项缺陷的目标失败算入579通过数。

实际完整命令为 `go test -mod=readonly -p 4 -json -race -count=1 -timeout=180s ./...`；定向使用同一源码、`-count=3` 与 `^TestR5(Reservation|Reference|AttachmentFinish|LockMap|Concurrency|Ingress)`。每段 stdout/stderr/真实退出码在容器内留存，既有 check_go_test_evidence 校验完整后端生命周期，不只判断外层命令返回0。

## 5. 证据留存与清理

[机器证据](evidence/R5-B01-G.json)绑定继承实现、前批文档提交、本轮提交/树、源码对账、各层计数、运行参数与清理结果。完整最终 [后端事件](evidence/R5-B01-G-BACKEND.jsonl.gz)、[三轮定向事件](evidence/R5-B01-G-FOCUSED.jsonl.gz)、[原七项审计事件](evidence/R5-B01-G-AUDIT.jsonl)均已保存；不只依赖临时目录或摘要中的哈希。

本轮日志为合成测试数据；归档前检查未包含实际测试 DSN 密码、Bearer 凭据或 JWT。没有访问真实员工邮件。有效原始前批失败文件保持原样，新证据不会覆盖旧失败。

日志复制、计数和源码核对完成后，按精确 ID 和标签删除本轮 runner、PostgreSQL、空 internal 网络；本轮 PostgreSQL 自动创建的匿名数据卷也删除并确认不存在。F 的三个具名遗留资源与 G 新建资源均已收口；不作全局 Docker/历史匿名卷清理，也不把本轮清理扩大为整个宿主没有任何残留。

## 6. 本轮边界与下一批

B01-G 完成的是 **F 的修复后验收及文档/资源收口，加三项入站事务证明**。没有新增生产修改，也没有修改原验收条件来提高完成率。P0-070 尚需普通内容读取/派生缓存与撤权、截止时钟的边界，以及索引租约、其余隐式 FK/多资源写入交叉评审；本批只覆盖入站中已明确测试的三个场景。

下一组从普通读取和索引等待后的资格/租约窗口继续，不再重做已经验收的 Reserve/Finish/入队修复。P0-080 仍需可执行协议与跨层对照；先前被拦截的协议校验器本轮没有重试。090/110 的依赖保持原样，G0 不关闭、不进入 P1，A01–A07 的产品修复仍待相应阶段。

本轮仅本地提交；没有 push、PR、main 合并、正式标签/Release、生产迁移、外发邮件或部署。过程中两次调用参数错误（工作卡要求完整project ID、一个猜测的模型文件路径不存在）和一次文档末尾空行检查失败均有明确修正，不算产品缺陷；后续源码差异复核重新执行。
