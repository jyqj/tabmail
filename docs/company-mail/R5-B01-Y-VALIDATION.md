# R5 B01-Y：实际时间／域资格与消息维护锁边界、真实组件基线

2026-10-01。基线 `777eaa83`（B01-X）。**父进度仍6/171，P0-070/080/G0未关闭；目标全部171项没有缩减。** 根线程仅分派，唯一integration operator持有共享生产、统一PG、源码冻结、全量和本地集成；测试agent持独占写集。

## 1. 精确源码与范围

最终被测Git tree `9dfd3d3bee057c77cffd62edea2543f88872ed53`，720个源码／工具／已有文档文件逐一SHA-256复核无漂移。私有临时验收commit `a779bf96b2fa26fee6e79b6cb5eb9570540174c3`只为driver提供Git上下文，其tree精确相等，不冒称用户分支提交。随后新增本报告／归档和更新README、TODO、主事务图属于文档交付，不把当前整树冒称重新执行。

明确排除下一批Z两个新文件：`r5_restore_retention_snapshot_test.go`（64fc23c）、`r5_template_version_order_test.go`（781a41b）。它们没有纳入Y frozen tree、默认full或本地提交；不能沿Y成绩声称Z通过。

生产仅 `company_members.go` 的Activate及 `company_mail.go` 的消息状态边界。`messages.go`、迁移、Go/npm锁文件、生命周期政策均未改。本批没有生产配置／员工邮件访问、外部发信、push／PR／merge／release／deploy。

## 2. Evidence → Finding → Path

| 证据 | 实际裁决 | 修复／范围 |
|---|---|---|
| activation最终同测试14事件：原6pass8fail，候选三轮14pass | tenant/invitation/audit真实等待越期限仍旧成功；域verified/mx撤验已正式提交后旧成功；真实域writer持非key锁时旧无final fence | 保留普通定位tenant→T UPDATE→绑定原tenant/invitation；锁后及audit/outbox后DB clock重验原expires；final原primary/zone/domain verified+mx SHARE NOWAIT，late400/409完整rollback |
| maintenance最终10事件：原4pass6fail，候选三轮10pass | work Trash、organize、owner seen三个正式port对retention均真实40P01；逆序及bulk旧tombstone控制绿 | messageMutation合法action后精确源NOKEY UPDATE NOWAIT，55→409/不存在404；bulk MATERIALIZED仅锁原active subset并复核predicate；原COALESCE／30day／计数及receipt政策不变 |
| personal最终5事件：原2pass3fail，候选三轮5pass | non-owner首次seen/star sparse INSERT晚FK source锁与M形成真实环，victim及派生／计数／receipt回滚验证 | UID/owner fastpath后、UPSERT前精确源KEY SHARE NOWAIT，兼容非key组织写；保原seen/star维度和actor/T/M/current grant spine |
| 真实Go-owned HTTP/PG→shipping组件 | 16case／23精确variant全部执行，12产品安全目标红、11限定scope绿；每packet CASE hash／child exit／HTTP trace／PG state复核 | 精确adapter目标独立于原DB scalar marker，各红映射原P1/P2/P4任务；不为080基线提前偷改未来政策，不以目标红当产品绿 |

失败事件含parent rollup，不能将8／6／3直接称作独立漏洞数。首次0test是`_windows_test.go`误被GOOS过滤；keyword DSN与既有testpg URL换库helper不兼容是环境失败，两者保留而不当产品红。

## 3. 首次统一验收失败与精确纠正

首棵tree `c04c2ab0...`全部原raw留存：

- 两个旧audited message夹具仍持source并等writer排队，新NOWAIT应立即409，12／15秒错误等待累积导致PG包原180秒超时。只改测试为**每resource/action专属audit advisory屏障**，不同message仍可并行；父先actor／freeze／审计失败rollback断言和预算没有放宽。完整29个TestR5Audited事件通过。等待由source前移到audit后，不能冒称与历史40P01同一阶段的红绿复现。
- 当前Vitest JSON省略可选`numRuntimeErrorTestSuites`，packet validator失败关闭误拒23个packet。最小兼容缺字段；显式NULL／bool／string／非0、unhandled errors、未知marker、缺实际HTTP/PG、错hash／variant／exit等反例仍拒。driver＋packet局部60测试，不混为全Python总数。
- Turbopack拒绝测试snapshot的node_modules越root符号链接。用APFS物理clone依赖到**全新隔离snapshot**后clean Next构建；不删除或使用用户旧.next缓存。
- UI首批未知失败是raw Store Draft的NULL地址被错误作为HTTP DTO、不存在单mailbox GET路径、ISO RegExp未转义`+`。全部用真实HTTP DTO／实际mailboxes列表／精确文本纠正，不手写安全响应、不吞404、不改生产渲染。

## 4. 最终fresh统一结果

19个step全部exit0，同一frozen tree、一次默认full、原预算：

- 字面 `go test -json -race -count=1 -timeout=180s ./...`：**1382pass／0fail／1明确browser skip**，52包；**218必跑全部通过**，PG包158.954秒。build／vet通过。
- Python **247**、Node **23**、默认Vitest **118**；clean Next production build、tsc、eslint通过。独立opt-in组件不偷并默认118绿数。
- shared-db真实 **46case／29精确目标leaf红**，controller0／product_green=false。
- shared-components真实 **16组件case／23packet**：12目标红／11scope pass、errors=[]。报告17 scoped case还含OP03 unit，不称17个组件。
- 两报告适用层union仅 **RC02.components**仍缺；BC02/03虽然DB／HTTP观察已执行，未来正式可靠来源回填／重复不覆盖／unknown迁移写入未执行，层集合不替代语义验收。
- HTTP／PG **80响应／65操作／66成功变体**，两公网DNS操作排除；缺DSN／0test／browser-disabled负例仍拒，契约16／33／67，事务目录49文件／350函数／451词法SQL call site通过；官方GOSUMDB下`go mod verify`确认全部modules。

## 5. 交付、范围与续接

[机器摘要](evidence/R5-B01-Y-VALIDATION.json)、[603成员原始日志归档](evidence/R5-B01-Y-LOGS.tar.gz)保留基线、candidate、首次full失败与最终fresh结果。白名单不含responses.json、DSN、私有fixture／token、源码快照tar、数据库或node_modules；归档SHA见机器摘要。

专属PG16.13 PID34679、0700 Unix socket／无TCP，Y所有子进程已terminal。**PG没有停止**：唯一operator按授权串行继续Z租约，不虚写stop0／PID消失；整个目标停止或阶段资源释放时再真实停库记录。

070逐要求矩阵在配套事务目录section10／JSON：source inventory、已证关系、actual caller及未证层分列；多source batch busy／已有sparse conflict／其他cascade与object callback／dispatch／GC推进边界不因本批绿外推。080仍缺RC02精确组件consumer／兼容入口裁决及BC02/03未来写入适用性。090–120/G0与P1–P11依赖保持。

Z已记录实际restore-first stale-retention候选删除恢复源并返回key的红；Y不改删除政策，下一步用Z最终8leaf与模板5leaf同冻结原messages正式baseline，再按实证修复／排除。不能借Y完整默认绿宣称Z、070、080或全部171完成。
