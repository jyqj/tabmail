# R5 B01-X：有限事务目录、共享协议与三项真实边界修复

2026-09-30至10-01。完整目标仍是R5-TODO全部171项；本批继续P0-070/080，不以子包替代完整目标。**父进度仍6/171，070/080/G0尚未通过完整验收。**

## 源码与范围

基线 `3893945`，工作分支 `work/company-mail-r5-goal-20260930`。真实被测Git tree `d7a1610a34debfd38fa1ada28363010f57f33622`，710文件逐一SHA-256验证；运行时程序/测试与冻结快照一致；后续文档及目录事实校正不沿旧hash冒称整棵当前树已重新执行。独立临时index不污染主index；tree不是commit。协议driver需要Git上下文，私有scratch commit `bb76ccb1cf6928a900c371fb2f723e0d417ddbfb` 的tree精确等于上述被测tree，不能冒称工作分支已提交。随后仅增加本报告/证据、文档入口及配套目录人工事实校正；原被测tree与日志不改写。

三路agent独占事务目录、缓存/改密回归、协议案例与消费者；主线程持有生产修改、manifest、Make/CI、PG和全量集成。独立新PG16.13仅0700 Unix socket、禁TCP；不读取生产配置/数据，不对真实邮箱投递，不发布或部署。

## Evidence → Finding → Path

| 证据 | 已确认或排除 | 修复/真实路径 |
|---|---|---|
| E01 cache原版17事件9pass/8fail | catalog证明mail_documents仅FK→messages，缓存与完整ingress worker无祖父tenant环；普通admin读必须显式grant。三个物理delete与缓存在正/反向完整命令中均40P01，victim回滚验证 | SaveParsedMessage保留actor/profile→M SHARE；用精确source message SHARE NOWAIT替代未保护的EXISTS，打断messages DELETE→M计数反序，不给热缓存新增T锁。三轮各17pass |
| E02 password原版1pass/2fail | 正式ChangePasswordAtomic与Guarded冻结形成40P01；audit故障原有回滚控制通过 | 定位tenant不是授信；T KEY SHARE先于U UPDATE，UPDATE再次核tenant+expected hash+active，密码/session/refresh/audit仍同commit。三轮各3pass |
| E03 queue原版5fail | 合法NULL last_error扫描string失败；真实outbox已processing、无fanout/POST，不是BCC红灯或endpoint错误 | 六处读取COALESCE空诊断，不改schema/writer/state/lease。四种默认PG场景三轮各5pass；完整真实outbox→delivery→loopback POST→delivered ACK也通过 |
| E04 协议真实输入 | 46例真实Go/PG/HTTP消费者，29个精确已知目标leaf红、无非目标错误；控制器通过不等于product green | 共用JSON→实际命令/状态/返回码；原JSONL与exit1保留。r5protocol build tag独立基线，普通backend不吞目标红也不以skip伪绿。未来P1–P7同断言继续闭环 |

## 新交付与证据级别

- `R5-TRANSACTION-COVERAGE.md/json`：49生产PG文件、350函数、445 SQL调用、126字面直接写候选、141名称闭包写候选、14迁移Up定义。66重要事务逐函数手工顺序断言；其余保留精确local operations/helper/branch trace。名字匹配caller只是候选，不是假动态dispatch证明。
- Go AST工具与Python校验/19反例验证新增/删除/动态SQL/caller/FK/source漂移会拒绝。结构PASS仅证明目录未漏未漂移，不证明全部锁图无死锁；既有与本批动态关系逐档标注，其余风险/后续任务保留。
- 全46协议明确实际error.code/message、per-entry reason absent/optional/exact。保留typed retry的真实reason，不再以统一reason=null伪造额外阻塞。层适用性/不可构造organize-only/后台无HTTP公开说明，不绕约束构造假状态。
- shared-db报告17个components层未整合；其中RC03/04/05已有同CASE SHA的真实Go production-policy→SubmissionPane组件scoped证据，14例仍缺共享组件消费者。该3例不是DB/HTTP→UI完整旅程；BC02/03实际schema和现存HTTP产物证明缺快照/unknown，不声称运行不存在的正式回填命令。未来迁移/backfill完整写入证明属于后续真实实现，父080仍需跨层审核。

## 验收结果

- 完整默认Go/race原180秒：1353pass/0fail/1独立browser skip；207必跑齐全。cmd/internal共51包（含新增CLI）；字面./...含npm目录一个无测试包共52，同结果，PG包139.473秒。
- build/vet两种范围通过；HTTP/PG80响应/65操作/66成功变体，两个公网DNS操作排除。
- Python230、Node23、Vitest118、tsc/lint/Next production build通过；契约16共享/33投影/67操作；事务目录检查通过，原负例缺DSN/0test/browser-disabled仍失败关闭。
- 独立协议baseline：46case精确runtime path均执行，29已知目标leaf红；原过程exit1，controller exit0且product_green=false，绝不加到默认Go通过数。

## 原失败与修正记录

初始cache admin没授读grant而未到INSERT属于夹具错误；已通过正式SetWorkGrant授read，角色不授正文。协议多个scratch快照保留CT05非法DELETE FK、RT02 owner grant、RT04不可构造状态、LF错误404路径等非目标失败。RT07最初personal异常expires前提纠正为真实purge截止；源GetWorkMessage仍不筛该生命周期，SQL状态+精确同资源返回后标限定目标红，继续验证引用保护。

SSE ResponseRecorder的WriteString绕过Write取消钩子导致真正120秒timeout；覆写WriteString并要求真实ready/event后cancel，加入反例，不增预算。webhook匹配/ACK夹具分别按真实event metadata与delivered状态纠正；仍无POST后的单case诊断揭示生产NULL scan，再由主线程修复并独立PG红绿。panic/timeout/parent额外错误/未知variant/compile/skip永不能被目标marker接受。单条BC05诊断命令记录使用GOSUMDB=off，不作为正式依赖校验结论；最终冻结验收保持原依赖与只读模块并独立重跑全部路径，诊断原回执不改写。

Make和现有CI已接入事务漂移与独立协议baseline；CI上传结果不改变product_green，未宣称远端CI实际执行。未来正式发布门禁必须要求后续全部安全目标绿色，不能把精确基线红当发布成功。

## 复现与剩余任务

先按scripts/testenv/README.md配置全新可丢弃PG、Go/Python/Node child PATH：

```sh
go test -mod=readonly -json -race -count=1 -timeout=180s -run '^TestR5(Cache|Password|QueueNullable)' ./internal/store/postgres
python3 -B scripts/check_r5_transactions.py
python3 -B scripts/check_r5_protocol.py --run shared-db --output-dir "$FRESH_EVIDENCE_DIR"
go test -mod=readonly -json -race -count=1 -timeout=180s ./...
```

070仍需目录具体交叉关系审查（旧内部DeleteUser、Activate邀请/域、模板/队列/对象回调等），静态候选不能确认漏洞或全图绿。080需整合3个已有policy→组件scoped证据、补14个共享组件消费者及future能力适用性最终审查；不能以46注册或baseline controller绿色勾父项。随后090夹具目录/100真实S/M/110兼容地图/120 G0原依赖保持；所有P1–P11、shipping浏览器/DNS/SSE重连/规模性能/依赖审计/冷升级未由本批替代。

本批新PG已stop0且PID文件消失；临时工具、快照和日志保留项目外。正式摘要/归档在evidence/R5-B01-X-VALIDATION.json和R5-B01-X-LOGS.tar.gz，不含responses.json、DSN、生产配置、源码tar或DB目录。无push/PR/merge/release/deploy，TODO唯一进度入口不另建平行清单。

## 集成独立事实复核

- ActivateEmployee实际普通locator SELECT→T UPDATE→invitation FOR UPDATE；配套目录旧反序断言已校正，无生产改动，剩余期限/重复token/当前资格风险不涂绿。
- final-vitest.json中RC03/04/05全部passed；beforeAll真实调用Go生产delivery policy并核CASE SHA，渲染shipping SubmissionPane。shared-db报告仍保留原17缺components记录；附加3例scoped层证据不冒充DB/HTTP→UI端到端，14例缺消费者。
- 唯一integration operator复算两个backend JSONL、207必跑门禁、协议精确29目标红/无extra errors、710源manifest与179归档SHA及成员安全；PG stop0/PID文件消失，TODO171复选框仍6且逐项未改。原raw与原失败均不可变；只本地集成，不push/发布/部署。
