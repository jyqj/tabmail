# R5 B01-V：多轮集成的新入队、恢复审计与会话边界

2026-09-30。新入队当前授权及末端期限、恢复运维审计父键、JWT版本携带已取得真实原版/修复后对照；协议共享输入完成四例局部接线。**完整P0-070/080/G0与R5仍未完成，父任务维持6/171。**

## 范围与源码身份

用户目标为多轮multiagent完成唯一R5-TODO全部任务。本批是该目标的第一组推进，不缩减171项范围。只修改当前项目，不读取生产配置或员工邮件，不公网发信，不推送、创建PR、发布或部署。主线程掌握共享端口、生产接线、TODO、manifest和全量集成；agent分别独占enqueue测试、恢复/JWT测试与协议消费者。

基线 `c1b4deb17e02225804e30536e369a9905ce2cb94`；工作分支 `work/company-mail-r5-goal-20260930`。实际被测Git tree `f6acecfd2657f7550f6c6424f2f4cffe36003a0f`，687份文件逐一SHA-256校验，当前源码与冻结快照无漂移。tree通过单独临时index生成，未污染主index，不能称为commit。验收后仅追加报告、TODO、事务图和README入口；最终commit以Git回执为准。

## Evidence → Finding → Path

| 证据 | 发现及边界 | 正式路径与处理 |
|---|---|---|
| E01：`baseline-final.jsonl`，52事件8pass/44fail | 新入队在父键等待前已做外层校验，但撤权先提交后仍可生成job/asset/pin/audit并消费草稿；Key删除原版为23503，不冒称成功入队 | SubmitAuthorized→SubmitWithReplay→AtomicOutboundEnqueue；同步复用请求者与durable sender策略，tenant SHARE→依赖SHARE/NOWAIT→附件/quota→INSERT/audit/草稿→DB期限→Commit |
| E02：同基线recovery三种完整命令 | 原版恢复与冻结真实产生40P01，非超时或错误映射假红 | InspectRecoveryReceipt/RetryRecoveryReceipt/ReconcileOutbound先取tenant KEY SHARE再重载actor；job NOWAIT保留，独立命令不改为租户排他串行 |
| E03：JWT基线与同测试最终三轮 | middleware已验sv后，在途改密/会话撤销/冻结再启用仍返回200；可信内部actor、API Key和JWT版本0必须区分 | Actor.SessionVersion *int64由JWT上下文携带，RefreshMemberActor在最终user fence验证；HTTP新提交将Principal传入最终callback，不把sv写入durable job |
| E04：candidate1 | 29pass/2fail，模板grant可在INSERT等待时先撤销；首次候选并未通过 | 复用事务reader增加精确grant行SHARE/NOWAIT，保留既有管理员也需grant政策 |
| E05：Go/组件共享JSON测试 | RC03/04/05、OP03使用真实生产策略；其余42例和HTTP/DB层仍缺消费者 | 46案例目录→delivery/submissions/credentials→实际SubmissionPane；JSON哈希绑定Go观察值，不从expected伪造返回 |

## 最终门禁

| 验证 | 实际结果 |
|---|---|
| 新PG并发三轮 | 每轮52pass/0fail/0skip；9个顶层必跑全部通过 |
| 全量Go/PostgreSQL race，原180秒预算 | 1232pass/0fail/1明确browser skip；50包、171必跑齐全 |
| Go build/vet | 通过，readonly模块和离线缓存 |
| HTTP/PostgreSQL | 80响应、65操作、66成功变体通过；2个实时DNS操作排除 |
| Python/Node | 196/23通过；125 API调用分支、7 transport forwarder通过 |
| 前端 | Vitest118通过；tsc/ESLint/Next production build通过 |
| 契约/i18n | 16共享模型、33三方投影、67操作；i18n1325键、30动态调用明确保留 |
| 缺执行负例 | 缺DSN、0用例、browser-disabled均被原证据门禁拒绝 |

以上从同一冻结快照实际运行，不沿用B01-U成绩。Vitest JSON内部套件计数包含describe，不等于测试文件数。新跨语言组件测试需要Go，frontend CI已按go.mod设置Go；未声称远端CI已执行。

## 验证装配与失败保留

Docker当前不可用。一次性PG16.13源码从官方FTP下载，核对官方sha256后在项目外编译；新集群只有0700 Unix socket、禁TCP、host reject。使用项目Go1.25.7缓存、Node22.23.2及独立Python3.12.11 venv和固定requirements。未改变全局工具或既有服务。

首次PG配置因默认SDK27与linker不匹配失败；仅子进程选SDK15.4后重试。增加OpenSSL配置后的旧对象发生重复符号，clean重编后成功；原失败日志保留在临时目录。最终全量原预算未放宽。第一次全量编译发现store.Store聚合遗漏新窄端口，修复接口后全量编译通过，未放宽生产授权。

首次JWT候选回执已拒绝404，但测试误期403；按既有存在性隐藏契约修为精确404，草稿仍精确403，原版200红证据重新跑同最终测试保留。最初把64位manifest哈希传给只接受40位Git身份的三个证据CLI被明确拒绝；生成真实冻结tree后重跑通过，不截断哈希伪造身份。

复现命令（先按 `scripts/testenv/README.md` 准备独立PG/Go/Python/Node并设置测试DSN）：

```sh
go test -mod=readonly -json -race -count=1 -timeout=180s \
  -run '^TestR5(Enqueue|RecoveryAudit|SessionVersionInFlight)' ./internal/store/postgres
go test -mod=readonly -json -race -count=1 -timeout=180s ./cmd/... ./internal/...
python3 -B scripts/check_http_contract.py --output-dir "$FRESH_EVIDENCE_DIR" --source-sha "$FROZEN_TREE"
python3 -B -m unittest discover -s scripts/tests -p 'test_*.py'
(cd web && npm test && npx tsc --noEmit && npm run lint && npm run build)
```

## 明确未完成与下一批

- 当前profile quota变更：最终读取权限，但计数仍使用请求早期捕获的quota.Limit；只降低配额/0→有限仍须真实并发红绿，不以CanSend撤权测试代替。
- Service/事务/缺稿的幂等回放早返回尚需当前principal与内容投影验收；不新增发送不代表可以返回过时内容。
- JWT真实Router本轮只覆盖receipt/draft-save；新enqueue/retry等全入口sv竞争还须独立测试，内部actor兼容不意味着允许HTTP丢版本。
- recovery父键修复不替代目标mailbox/zone/期限与审计等待的一致资格；其他FK/GC、普通收件历史期限继续待验。
- P0-080仍有42案例、HTTP/DB、字面reason及完整跨层真值表缺口；不能据四例标记冻结。
- 090/100/110/120及P1–P11按原依赖推进。shipping浏览器、SSE跨实例/重连、公网DNS、规模性能、当前依赖漏洞审计和生产升级仍未由本批验证。

本批PG已正常stop0，PID文件消失。临时源码、工具和日志保留在项目外，不删他人资源。机器摘要及非敏感日志位于 `evidence/R5-B01-V-VALIDATION.json`、`evidence/R5-B01-V-LOGS.tar.gz`；归档不含原始responses.json、DSN、生产数据、源码tar或PG目录。
