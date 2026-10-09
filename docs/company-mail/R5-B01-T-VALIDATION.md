# R5 B01-T：旧outbound回执的当前身份与整页读取边界

2026-09-30。B01-T完成旧回执详情、列表、尝试记录及公司收件人回执的当前资格保护，并修复Key记录使用IP后的inet解码故障。原版同最终测试31事件24fail/7pass；47组PG/173事件连续三轮通过，完整backend1117pass/0fail/1 browser skip、149必跑齐全，HTTP80响应通过。不提前关闭P0-070/080、完整P2或G0。

## 1. 起点与实际复现

起点commit `be460d2c28fe74e83033a64e1561597b9bcef886`，原工作区clean；工作分支 `fix/company-outbound-receipt-snapshot-20260930`。唯一活跃清单仍为R5-TODO.md。

最终被测源码tree `aed2d31f28e96f366c3f5b2cef04e712ec50377f`，661份版本文件逐一核验哈希。验收后只更新本文、TODO、事务图与非敏感证据，不继续修改被测程序；精确commit以最终Git回执为准。

S保护了正文，但原 `AccessibleOutboundJob` 的owner/admin判断仍依赖请求开始时的Actor；旧列表把同一个Actor翻译成OwnerListScope后直接查询。真实PgStore对照复现：已停用普通用户/管理员、已降级管理员、已收紧域名的用户，以及过期/改属主/撤读scope的Key，可能继续取得历史回执。不存在的用户伪带旧admin标记和未知principal类型也不能作为有效身份。

真实生产Router夹具进一步复现：有效Key认证后在outbound_jobs或outbound_attempts表锁上等待，跨过截止时间仍取得200回执/尝试结果。另一列表等待场景中，用户停用已先提交，列表仍从旧Actor返回Subject等元数据。这里是已认证请求跨数据库等待的窗口，不声称新请求可以绕过JWT或X-API-Key中间件；新请求对停用用户仍应401。

原详情对共享读者会检查live sent content，而旧列表只检查当前邮箱read，导致条目过期后同一共享读者列表能发现详情返回404的任务。本批统一采用详情已经执行的较严边界，历史提交者/当前管理员的回执维度保留。

## 2. 原位收敛

新增 `store.OutboundReceiptReader` 的两个角色方法和内部 `OutboundReceipt{Job,ContentAllowed}`。该结构不是公开DTO，原始Job只能由submissions执行已有RedactOutboundJobView后输出。

`outboundPrincipalTx` 从S的Key内容事务提取共同身份保护：用户复用companyReadTx/当前角色及profile快照；owned Key保持owner user SHARE → 当前key SHARE NOWAIT → owner profile SHARE NOWAIT；ownerless Key仅保护Key并保留其自己的回执维度。当前角色与scope在数据库内重载，不采信Actor的admin标记。Key身份和scope纯函数与S内容检查复用，不新增第二份规范化语义。

Key读回执要求send:read；重试入口的回执准入单独要求send:write，不能要求写Key同时拥有GET权限。收件人/详情最后检查使用read，原S的提交响应内容投影可继续接受read或write。Key已变属主、停用属主、无当前scope或截止到期均不返回结果。截止在实际数据查询结束后按数据库clock_timestamp再次核对，连空页也不能在失效凭据下报成功。

`readOutboundReceipts` 用一条SQL取得可见ID、总数、页面数据和内容布尔值，全部在同一语句的数据快照内。visibility CTE仅物化ID/created_at，不为整个租户复制所有正文；仅选中页才连接完整Job行。count和超出末页的空结果由同条语句返回，排序为created_at DESC,id DESC。当前权限、原请求域名范围与Key当前域名约束只做收紧。

可见范围统一为独立tenant/zone约束 AND（当前owner/admin回执权 OR 原sentContentFrom/submissionContentScope当前内容权）。共享列表因此不再枚举详情拒绝的已过期内容；ownerless Key无正文权，但合法的自己集成回执仍存在。admin只是既有回执范围，不变成正文read。

## 3. HTTP和错误边界

GET详情直接使用同时带可见性和内容决定的记录；列表直接使用整页记录后做纯投影，删除handler内逐行I/O及旧OwnerListScope构造。尝试记录先做准入，读取attempts后再做最终current receipt+content查询，任何错误都不输出已经收集的结果。公司recipient投影也在ledger读取后做最后的回执资格检查。

重试的初始准入改成send:write专用入口，但本批没有修改ValidateJobAuthorization→RequeueOutboundJob之间的最终写入窗口。已有重试动作、发送状态机、SMTP网络和出站worker未被重写。原始ListOutboundJobsScoped作为内部低层端口仍存在，HTTP消费者已退出；其旧接口测试不能冒称为新回执权威实现。

明确允许的变更：身份/角色/Key失效后的详情隐藏为404，列表可能403；共享读者不再列出详情无权访问的过期条目；未分类存储失败保持脱敏500；profile/Key竞争409沿用既有映射。公开JSON字段、OpenAPI、迁移和依赖均不改变。

### 同批发现：Key记录使用IP后元数据无法读取

新增write-only Key重试正例在原版和第一候选都返回500。逐阶段诊断确认不是取消认证或测试环境问题：`GetAPIKey` 在读取非空 `last_used_ip` 时报告 `cannot scan inet (OID 869) in binary format into **string`。正常X-API-Key认证会记录该IP，因此重试的ValidateJobAuthorization重新读取Key就可能触发错误，两个Key列表入口也使用同样的错误投影。

现在三个元数据读取入口复用 `apiKeyMetadataSelect`，用 `host(last_used_ip)` 投影为原公开模型要求的地址字符串，保留NULL；不改数据库类型、公开字段或凭证哈希。PostgreSQL 16 [网络地址函数](https://www.postgresql.org/docs/16/functions-net.html)定义host返回不带网络掩码的IP文本。原版/候选对照分别测试未使用、IPv4、IPv6，以及Get、租户列表和属主列表三个入口；重试正例明确先TouchAPIKey再调用现有发送验证和真实HTTP POST，不能靠未记录IP的空夹具取得绿灯。

## 4. 回归与证据口径

八组新PostgreSQL顶层回归（含Key使用地址）覆盖当前身份矩阵、Key在详情/列表/尝试等待中到期、列表与用户停用的实际排序、详情/列表共享范围、ownerless Key受限回执、write-only Key仍可进入既有重试动作、跨租户/存储故障，以及同时间戳稳定分页和超出末页的total。

两个submissions单测验证50行页面只调用一次授权页面端口、不再次调用逐行ContentAllowed、原始模型不被修改、空列表编码及失败时不返回半页，并固定GET/read与retry/write不同scope。Fake只验证内存模型/接线；数据快照与锁等待由真实PG负责。

原始基线使用同一份最终PG测试覆盖起点源码，正例与反例分别保存。最初一次夹具因未使用的import编译失败，未计为行为基线；修正后才接受有实际run/fail事件的对照。第一候选定向167pass/2fail中的两个失败为write-only-retry子用例及父节点；真实inet解码缺陷修复后，单独Key地址与重试组7事件全部通过。原失败与诊断记录保留，最终重新冻结源码并完整执行。测试不通过任意sleep假定调度；使用pg_blocking_pids、真实数据库截止和通道。只有合成数据，HTTP使用生产Router和PgStore、测试对象/Redis适配器，不启动邮件投递worker。

## 5. 最终验收

| 验证 | 实际结果 |
|---|---|
| 原版加相同最终八组PG测试 | 31事件：24fail、7pass、0skip；包含父节点，不是24个独立漏洞。 |
| 最终PG定向，含O/P/Q/R/S | 每轮173pass，连续三轮共519；47必跑齐全、0fail/skip。 |
| Go build / vet | 最终树readonly modules通过。 |
| Python / 静态契约 | 168测试通过；16共享模型、33三方投影、67公司操作绑定通过。 |
| 全量Go/PostgreSQL race | 1118started、1117pass、0fail、1skip；原180秒包级预算未放宽。 |
| 完整backend门禁 | 149必跑齐全，0缺失；唯一允许skip为独立TestR3BrowserJourney。 |
| 实际HTTP/PostgreSQL契约 | 80响应、65成功操作、66必需成功变体通过；两个实时DNS操作明确排除。 |
| 缺DSN负例 | Go exit0、15子测试skip/父节点pass；门禁exit1，拒绝未执行的必跑案例。 |

首次完整候选因上述真实inet缺陷在retry正例失败，原Job保持exit1记录；修复API Key三个元数据读取入口后重新冻结并从原版对照、三轮PG到完整backend/HTTP全部执行。不是仅重跑失败子测试后把旧成绩贴到新源码。最终验证Job `wc_job_26jNj8Uwk7LdTaAL` exit0，源码及日志哈希分别核验完成。

## 6. 边界、兼容与后续

同一SQL说明的是数据库行的语句快照；clock_timestamp仍在实际评估时检查到期，不宣称整个网络响应或所有分页请求在同一时刻线性化。用户、Key、profile锁保护当前来源；不扩大到任意直接维护SQL、所有JWT session版本变化或整个下载过程。触发撤权之前已经放行的字节不可追回。

本批避免逐行权限网络往返，并只物化visibility元数据，但精确count仍要扫描匹配范围，未取得S/M/L、吞吐或P95无回归证明。profile/Key使用NOWAIT，可能因管理或Key last-used更新产生409；不吞掉冲突或自动重试。

剩余重点为RetryAuthority/ValidateJobAuthorization到最终requeue的原子性、入队最终资格、旧POST重放及回执协议最小字段、普通收件历史期限映射、其他FK/GC、多资源批量锁、P0-080真值表、P1编辑CAS/ABA及发布验收。未执行前端/完整browser、真实DNS/SSE重连、真实S3/断电或远端CI。父任务仍6/171；仅本地提交，不push/PR/merge/release/deploy。

环境沿用Go1.25.7、PG16.13和已有Python3.12虚拟环境；child PATH明确前置Python3.12，创建本批0700临时目录和Unix socket、禁TCP/host认证拒绝；不读取生产DSN、环境文件或员工邮件。原始HTTP响应仅私有0600留存，发布归档排除responses.json、完整服务日志和运行凭据。

实际版本Go1.25.7 darwin/arm64、PostgreSQL/psql/pg_dump16.13、Python3.12.11。首次候选集群PID69179已停止，诊断与最终验收重启后的PID25605也停止exit0，PID文件消失。仅保留本批私有日志与源码，不清理其他会话资源。

[机器结果](evidence/R5-B01-T-VALIDATION.json)及[执行证据](evidence/R5-B01-T-LOGS.tar.gz)共123成员、213248字节，SHA-256 `8309c238e5fe6dabf7504f1033c887b0b643ab5d1266e33b524779139f9d0eac`。成员逐一比对MANIFEST，Go日志/必跑清单/OpenAPI/HTTP采集哈希核验通过，原始responses.json未发布。
