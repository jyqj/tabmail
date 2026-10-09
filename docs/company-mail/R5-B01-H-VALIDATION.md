# R5 B01-H：索引租约候选重构与普通读取回归

2026-09-28。**状态：候选代码与10项测试已提交，运行验收阻塞；不是已复现并验证通过的修复。** P0-070/P0-080均未关闭，进度仍为 **6/171，P0 6/12**。唯一活跃进度见 [R5-TODO](R5-TODO.md)。

## 1. 版本与范围

开始HEAD为 `5e4b9df22041ac0bfda570901d114166101067b6`，工作区干净。新建本地分支 `fix/company-mail-r5-b01h-index-fencing`，保留之前分支。fetch后main/origin/main仍为 `d6d512172fb6b874c3283d9df3f4d56758684a13`。

候选实现提交为 **`01937522ebaa299d1c816085ef1678d5c2e62a03`**，tree为 `3cf3d7ac738b14161961d2057d7aa1d5c6e2817e`。提交消息明确为wip，等待PostgreSQL验证。生产行为改动仅在 `internal/store/postgres/mail_content.go` 的索引完成/失败路径；新增两份测试，扩充既有必跑名单。未修改普通读取的生产授权/期限规则，没有重做Reserve/Finish/入队修复，没有修改13个历史迁移、Go模块或前端依赖。

## 2. 源码支持的风险，不是本轮实测缺陷

原CompleteMailIndexJob先以 `lease_until>now()` 更新任务为ready并清除租约，再调用saveParsed；原FailMailIndexJob同样以 `now()` 判定。由此提出三个待验证场景：完成等待job行后过期、失败等待job行后过期、任务更新后等待document行再过期。原代码没有覆盖这些等待后的新时间检查。

PostgreSQL 16官方[时间函数](https://www.postgresql.org/docs/16/functions-datetime.html)明确区分事务开始时间与clock_timestamp实际时间；[行锁与一致次序](https://www.postgresql.org/docs/16/explicit-locking.html)说明锁模式和次序要求。这些资料与源码支持风险推断，**不替代本仓库的真实竞争复现**。本轮没有取得目标失败日志，不登记三个已确认漏洞。

迁移00011的source更新会先处理message，再处理document/job子记录。原完成路径可能在已经持有job时才遇到document外键对message的等待，因此另加入source-before-job的顺序探针；尚未运行，不能宣称已经复现40P01。

## 3. 候选实现

复用原postgres包和原事务，不引入平行索引引擎。完成路径调整为：

```text
核对输入的message/source
  → 精确tenant/message的source行FOR SHARE，并比较当前source key
  → 精确tenant/message/source/token/processing的job行FOR UPDATE
  → 新语句用数据库clock_timestamp重验租约
  → 原saveParsed写入document
  → 最后的条件UPDATE再次检查当前租约，再标记ready/清除token
  → 原事务提交
```

source使用SHARE而非KEY SHARE，以同时约束非主键raw_object_key的替换。document写入失败，或最后租约判断失败，均沿原事务回滚路径退出。最终条件UPDATE是提交决策检查点，不承诺数据库提交确认返回时的物理时刻仍在租期内。

失败路径改为明确事务：先锁精确job，再以新语句检查实际时间，最后条件更新pending/failed及下一次重试时间。仍然只保存固定的粗粒度诊断，不将调用方reason中的正文写入状态；不获取source/document锁，也不做对象或网络I/O。

这是依据源码与数据库语义形成的**修复候选**，未满足项目要求的基线红灯→候选绿灯闭环。应保持在本地WIP分支，不合并或部署；若实际基线行为与推断不符，须修正判断而不是制造目标失败。

## 4. 新增测试代码：执行数为0

| 测试 | 待验证的断言 |
|---|---|
| IndexCompleteRechecksAfterJobWait | 只持有job行，让租约自然过期；旧完成被拒且状态/正文不变，新token可恢复 |
| IndexFailRechecksAfterJobWait | 同样等待后，旧失败不能修改任务状态或诊断 |
| IndexCompleteRechecksAfterDocumentWait | 完成等待document后过期，派生正文及job必须一起回滚 |
| IndexSourceLockPrecedesJob | 等待message时job仍可被NOWAIT探针取得；不将此探针称作完整死锁复现 |
| IndexDocumentFailureRollsBackCompletion | document约束失败必须精确为23514，任务/正文不留下部分成功 |
| IndexCancelledCompletionReleasesLocks | 取消等待传播context错误，回滚后同一有效job能再次完成 |
| IndexRejectsChangedSourceAndStaleToken | 原件标识改变后，旧结果/旧失败不能覆盖新来源 |
| ReadCachedContentRejectsNewRequestsAfterRevoke | 保留真实PG缓存与内存原件，撤去read后新正文/EML/附件读取在对象Get前拒绝 |
| ReadAdminAndWrongMailboxDoNotOpenObject | 管理身份、错误邮箱ID和错租户不能打开原件 |
| ReadWaitingIdentityReloadRejectsFrozenActor | 真实用户重载等锁期间active被改变，不能继续信任旧actor |

完整名称带 `TestR5` 前缀，代码在 `r5_index_boundary_test.go` 和 `r5_read_boundary_test.go`。7项索引、3项读取，共10项。数据库操作设计为真实PgStore；读取测试中的对象适配器是内存实现并计数，**不是S3/文件系统耐久性或真实HTTP/浏览器测试**。冻结场景仅修改隔离fixture的active字段，不冒充完整管理员冻结命令。

普通读取测试只覆盖新请求/尚未完成身份重载的请求；不要求召回已经传给浏览器的字节。到期可见性、列表/统计一致性、跨语句授权快照、持续连接以及全部HTTP入口仍需原P0/P2/P3/P7任务，不由这三个用例替代。

## 5. 实际检查与执行阻塞

| 检查 | 本轮实际状态 |
|---|---|
| 宿主Python验证工具回归 | **57/57通过**；完整输出在机器静态记录中 |
| DTO字段检查 | **16共享模型、32公司DTO通过**；不是HTTP类型/响应验收 |
| Go语法解析 | 独立沙箱Go 1.23.2的parser解析266份Go源码通过；**仅语法，不是类型检查、Go 1.25.7编译或测试运行** |
| Go格式 | 三个改动Go文件使用同一沙箱gofmt输出，并按前后SHA-256核对回写；未改语义 |
| 新增真实PG/应用读取测试 | **0项执行，没有基线失败或修复后通过成绩** |
| 全量Go build/vet/race | 未完成；独立沙箱go test在编译前拒绝go.mod最低Go 1.25.7要求 |
| 必跑名单 | 从44扩充到**54项，均为执行要求，不是54项已通过** |
| 前端、shipping浏览器、GitHub CI、依赖审计、S/M性能 | 本轮未运行，不沿用G批579/44/90成绩 |
| 历史迁移、既有上传/入队修复、模块与lockfile | 逐字对照基线不变；git diff --check通过 |

Mac上新建隔离Docker环境的调用被工具安全检查在执行前拦截，没有把同一创建操作换入口重试。独立沙箱仅有Go 1.23.2，未发现PostgreSQL；公开依赖端点DNS/下载失败，Go 1.25.7下载也未成功。本轮没有在Mac创建新runner/PG/network，也没有全局重启Docker或更改生产服务。

源码通过Git archive和四文件工作快照导出用于独立静态检查，未包含未跟踪配置、生产邮件或数据库凭据。Mac tar附带的AppleDouble元数据最初触发语法错误，已按非源码元数据排除后重跑；实际266个Go源码解析通过，不能把该文件表示问题算成产品缺陷。生成的两个临时导出tar已按精确哈希核对后从仓库工作区删除。

## 6. 证据与下一步

[机器静态记录](evidence/R5-B01-H-STATIC.json)保存Python/DTO命令及真实退出码、候选文件哈希、注册数量和运行阻塞。候选提交/树与后续文档提交分别登记；没有伪造go-test JSONL，没有将函数名或静态解析结果当作真实执行。

下一批先在获准、版本正确的独立环境对两个源码版本分别验证：基线 `5e4b9df...` 加本批测试，以及候选 `01937522...`。时间/顺序场景必须确认失败来源，候选全部10项必须真正执行；再运行完整54项必跑所在的 `go test -json -race -count=1 ./...` 和原有证据门禁。重复定向测试是补充，不能代替全量。若环境仍未恢复，保留阻塞，不继续增加未经运行的生产修改。

P0-070仍未收口，P0-080保持独立未冻结状态；之前被拦截的协议校验器本轮未重试。A01–A07本轮未重跑、未修复，090/110依赖及G0/P1状态不变。没有push、PR、main合并、正式Release、生产迁移、外发邮件或部署。
