# R5 B01-E：附件完成的并发边界修复与最终回归

2026-09-28。**本批补齐 B01-D 的最终验证，并实际修复五类附件完成问题。** 新增八项真实 PostgreSQL 回归，最终全量后端、前端与工具检查通过。P0-070 尚有其他路径未完成，P0-080 草案未冻结，因此不新增整项勾选：仍为 **6/171，P0 6/12**。唯一进度源见 [R5-TODO](R5-TODO.md)。

## 1. 接续的是仓库真实版本

聊天上轮停在 `46711be9`，但本轮打开仓库时，已存在 B01-D 的生产修复 `0f9c3ce58518e93be4fec555a2adb9af46700f19` 和证据提交 `ea2328076b473e092b040ba97df778bd3c59fbd5`。本批以 **ea2328076b473e092b040ba97df778bd3c59fbd5** 为基线，新建 `test/company-mail-r5-b01e-upload-boundary`，保留原分支，不覆盖既有工作。

本轮实现提交：**`0d8cd6d1dfec27835f080e535703d053372d352b`**；实现树：`0a5f86bfb84944da94329e48ed8433e7367f0c20`。生产变更只在 `company_mail.go` 和 `mailbox_revision.go`；新增 `r5_attachment_finish_test.go`，并扩展必跑名单。B01-D 的 outbound.go 修复原样保留；Go 模块、前端业务、lockfile 和 13 个历史迁移均未修改。

先对继承的九项锁/并发测试连续执行三轮，**27/27 通过**。随后在本批最终源码上重跑全部已有及新增测试，不能拿这一继承基线通过代替新代码验收。

## 2. 五个问题：真实失败后再修复

测试直接调用生产 `FinishMailAttachment`，使用每例独立 PostgreSQL 数据库。控制事务只选择并发时序，不替换权限计算或实际写入。

| 标记 | 基线实际观测 | 修复后要求 |
|---|---|---|
| REMOVED | 完成命令等待附件行时，另一事务删除预留记录；旧命令仍返回成功 | 返回明确不可用错误，不能把零行更新当 ready |
| EXPIRED | 等待期间预留记录到期；旧命令仍写入 ready 和校验值 | 取得锁后按实际时间重验，到期冲突且保持原状态 |
| REPLAY | 两个不同校验值的完成命令均已读取 uploading，并等待同一行；之后都成功 | 只有一个完成成功，失败者不能覆盖成功者校验值 |
| REVOKED | 完成命令读过共享邮箱发送权后等待附件行；真实 SetWorkGrant 撤权已提交，旧完成仍成功 | 先取得邮箱授权锁；完成和撤权有明确顺序，后续无权完成拒绝 |
| CHECKSUM | 内部完成接口接受 64 个非十六进制字符 | 返回 BadRequest，不修改状态；合法 SHA-256 规范化为小写 |

这五条在未修复的 ea23280 上全部命中精确目标断言，Go 进程实际退出 1，无 skip。保留了[完整原始失败日志](evidence/R5-B01-E-FINISH-BEFORE.jsonl)和[当时测试源码](evidence/R5-B01-E-FINISH-BEFORE-SOURCE.go.txt)。首次 REPLAY 测试只数直接等待者，漏掉了经另一个 updater 间接等待的进程，该次超时未计作缺陷复现；改为查询实际递归等待链后，五个目标才全部成立。

CHECKSUM 是内部完成接口的数据完整性问题，不据此声称外部用户可以直接提交任意完成摘要；正常 UploadAttachment 的摘要仍由服务端计算。撤销后拒绝也不等于召回已经完成的请求或已发往外部 SMTP 的邮件。

## 3. 生产修复与锁边界

草稿保存和附件完成共用现有 postgres 包中的 `lockMailboxAuthorization`，不创建第二套权限引擎。完成命令按以下顺序执行：

```text
当前用户 SHARE 与身份重验
  → 读取附件所属邮箱 ID（只用于定位）
  → 同邮箱 SHARE
  → 重新计算当前 mailbox/grant 发送资格
  → 精确 tenant/user/mailbox/attachment 行 FOR UPDATE
  → 新语句检查 uploading 及 expires_at > clock_timestamp()
  → 条件更新 state/sha256，要求影响行数恰为 1
  → 提交
```

邮箱 SHARE 与现有 grant/policy/handover 的邮箱 revision 写入互斥；用户锁和邮箱锁保持到完成事务结束。附件查找与最终更新重复绑定当前主体、租户及邮箱，避免初始定位被当成最终授权。移除记录返回 NotFound；锁后状态或期限改变返回 Conflict；没有发送权返回 Forbidden。

先获取附件锁，再在新语句检查当前时间，防止开始时有效、等锁后已过期的事务继续使用旧时钟。新增 `TestR5AttachmentFinishUsesTimeAfterLockWait` 在控制事务**不修改任何行**的情况下，实际观察数据库时间越过预定截止，确认等待后的完成拒绝。不是靠改测试期望或只在 UPDATE 中写一个 now() 就宣布时间边界成立。

数据库语义参考为 PostgreSQL 16 官方[时间函数](https://www.postgresql.org/docs/16/functions-datetime.html)、[锁规则](https://www.postgresql.org/docs/16/explicit-locking.html)和[等待者函数](https://www.postgresql.org/docs/16/functions-info.html)。官方文档说明不替代本仓库真实竞争测试。

没有新增租户排他锁，没有把对象 Put/删除搬入此事务。现有 UploadAttachment 对 Finish 错误继续返回失败并保留恢复/回收所需对象，不因提交结果不明抢删对象。完整上传预算、reserve 竞争、HTTP 下载资格、正文读取和对象回调的其他问题仍在原任务范围中。

## 4. 八项完成回归及最终检查

八项新用例覆盖上述五个失败，加上合法大写摘要规范化与重复完成拒绝、等待期间主动取消并回滚、自然跨越真实截止。取消后还启动新完成命令，确认锁已释放且可以正常完成。

| 检查 | 最终实际结果 |
|---|---|
| Go build / vet | 通过 |
| 全量 Go race + 真实 PostgreSQL | **48 个项目包，566 pass / 0 fail / 1 明确 skip** |
| 后端必跑名单 | **31/31 通过**，含 B01-D 的全部 23 项和本轮新增 8 项 |
| 锁 / 并发 / 附件定向重跑 | **17 个不同测试各 3 次，51 pass / 0 fail / 0 skip** |
| 普通前端 Vitest | **108/108 通过，0 pending** |
| TypeScript / ESLint / Next 生产构建 | 通过 |
| Python / Node 验证工具测试 | **57/57、23/23 通过** |
| i18n / 客户端矩阵 / DTO | 118 生产文件、1325 键；125 客户端分支；16 共享模型、32 公司 DTO 通过 |
| 原 A01–A07 DB/HTTP 缺陷基线 | 七个精确目标失败再次复现，product_fixed=false；不混入普通通过数 |
| 源码 / 历史迁移 / 差异 | 实现文件逐字匹配被测副本；13 个历史迁移不变；diff --check 通过 |

唯一普通后端 skip 是 `TestR3BrowserJourney`；本轮没有运行 shipping-image 浏览器、远端 GitHub Actions、当前依赖安全审计、公网投递或 S/M 性能基线。普通前端 Vitest 不冒充本轮重新运行 A01/A02 原始编辑器与真实 API 的联动。缓存复用不称为重新 npm ci。

B01-D 的最终验证缺口现已补齐，但其 Docker 超时根因没有建立。本轮没有重启 Docker 或其他项目服务；本轮测试正常完成并不等于已经诊断、修好了宿主环境。一次初始环境诊断调用被工具安全检查拒绝，没有从未执行的诊断取得结论。后续获准的独立测试操作结果如上。P0-080 先前被拦截的协议校验脚本没有在本轮写入或执行，原协议草案保持未冻结。

## 5. 可追溯证据与提交

[机器证据](evidence/R5-B01-E.json)绑定 base、implementation commit/tree、四个运行时变更文件哈希、实际命令、原始输出哈希及清理记录。日志中 source_sha 是构建基线 ea23280；须结合覆盖文件哈希与实现提交解释，不声称该基线已经包含本轮新代码。

完整后端事件不仅留在易失临时目录，已压缩保存为 [R5-B01-E-BACKEND.jsonl.gz](evidence/R5-B01-E-BACKEND.jsonl.gz)。[三轮定向原始事件](evidence/R5-B01-E-FOCUSED.jsonl)、[前端逐用例结果](evidence/R5-B01-E-WEB-RESULTS.json)、原始失败测试源码与日志也已留存。所有数据来自合成测试，不含真实员工邮件；没有上传测试 JWT 或数据库凭据。

完整本机日志仍在 `/tmp/tabmail-r5-b01e-rni5zfqd/.validation/`，路径可能被操作系统清理；关键证据以仓库内文件为准。仅在核对本轮 session label 后删除独立 PostgreSQL 容器和 internal 网络，两个测试执行容器均已退出并移除，既有服务及缓存不变。

## 6. 任务范围与下一步

本次在已满足前置条件的 P0-070 中，根据实证前置修复同一完成函数的明确安全边界，记录为局部交付；不借此跳过完整 P5 上传/引用/故障验收或关闭 P5-050。没有通过拆任务、删验收标准来增加完成率。

070 仍需 reserve/普通内容读取、GC 引用、入站及原件对象回调的交叉锁/撤权验证；080 仍需完整可执行协议及跨层对照。090/110 依赖保持不变，G0 未关闭，不进入 P1。原 A01–A07 都不因本轮附件完成修复而关闭。

本地实现与证据提交完成后保持工作区干净；本轮没有推送、提 PR、合并 main、创建 Release、生产迁移或部署。
