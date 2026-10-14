# R5 B01-W：当前配额、POST回放与接收目标资格

2026-09-30。三路agent独占测试与交叉复审，主线程持有生产接线、manifest、集成验收。**本批修复已验收，不等于完整070/080/G0；原父统计仍6/171。** 用户目标仍为完整171项，不缩减为本批子集。

## 范围与源码

基线 `cb2e9e915e1b3f76b4e117ef7483848aff44dd0e`；工作分支 `work/company-mail-r5-goal-20260930`。真实被测Git tree `95881fb77cb15d05c0f002e92ca79940dd7a3077`，697文件逐一SHA-256验证；验收前后当前源码与冻结快照无漂移。独立临时index生成tree，未改主index；tree不是commit。后续仅补本报告、证据和文档入口，commit以Git回执为准。

只使用专属新PG16.13集群，0700 Unix socket/禁TCP；Go1.25.7只读离线缓存、Node22.23.2、Python3.12.11固定依赖。复用上一轮已验证的独立工具，不复用上一轮DB或测试成绩；中断后先核实PID96099及实际命令，再继续同集群。没有生产配置/数据、真实邮件、推送、PR、迁移或部署。

## Evidence → Finding → Path

| 证据 | 已确认事实 | 正式链与修复 |
|---|---|---|
| E01 baseline-final-v2 | 等tenant后继续用早期quota：31→1、0→有限仍201；1→0错误429；覆盖新增/删除及双请求也可错判 | Router→SubmitAuthorized→fenced enqueue callback；同reader重新读取当前user/role/profile并返回quota，0保留unlimited；互动admin无限额、owned admin Key仍profile；保留明确trusted内部自定义预算 |
| E02 同基线consumed POST | JWT版本撤销/冻结再启用后返回200受限回执：body失败关闭没有替代当前主体准入 | 正式三个POST回放分支统一ReplayReceiptView：当前send:write回执snapshot+原SubmitActor/tenant/job ID绑定→当前sent投影；身份拒绝403，内容失效仍安全200，不套用未来发送CanSend/From/template政策 |
| E03 同基线retry/worker | address/zone/tenant/删重建或域验证变化、审计等待自然expiry后仍排队/投递 | retry排序并锁固定target；retry/worker共享精确zone+mailbox tuple行保护和receive资格，不加入read/send权。审计后DB期限重验；worker一次materialized时钟同时检查lease和target截止，失败全回滚 |
| E04 真实loopback HTTP | 目标row/zone忙时原版200；原版row实际等待后释放，非把timeout当红灯 | Retry只将55P03/40001映射409/CONFLICT，其他错误保留，不吞40P01。Inspect必要审计允许，重试状态/audit/outbox无半提交，held原件引用保留 |

原版同最终测试46事件21pass/25fail；首次夹具42703/23503/23514分别来自错误SQL列、不合法跨tenant tuple、空Key scopes，已纠正并重跑，原日志保留但不计产品缺陷。第一版43/43通过后再补真实HTTPbusy与同一时钟lease/目标约束；最终统计以冻结树为准。

## 实际验证

- 新PG：17顶层、每轮46事件全部通过，连续三轮；严格必跑门禁无skip/missing。
- 全量 `go test -mod=readonly -json -race -count=1 -timeout=180s ./cmd/... ./internal/...`：1320pass/0fail/1独立browser skip，50项目包，197必跑齐全。
- 又执行字面 `./...` build/vet/race：同1320pass/0fail/1skip、197必跑；51包包含npm安装目录中的一个无测试第三方Go包，不把它当新产品覆盖。PG包该轮176.262秒，接近原180秒预算，未放宽预算，后续需记录/优化实际测试装配成本而非删用例。
- HTTP/PG：80响应/65操作/66成功变体；两个实时DNS操作排除。
- Python196、Node23、Vitest118、tsc/ESLint/Next production build通过；契约16共享/33投影/67操作通过；i18n1325键/30动态调用明确不认证。
- 缺DSN/0用例/browser-disabled原负例门禁均拒绝。
- 新quota helper与replay单元通过，明确为端口/FakeStore证据，不代替PG锁竞争。

复现先按 `scripts/testenv/README.md` 准备新独立服务与child PATH，设置可丢弃测试DSN：

```sh
go test -mod=readonly -json -race -count=1 -timeout=180s \
  -run '^TestR5(EnqueueQuota|RecoveryRetry|IngressDeliveryRejectsTarget|IngressDeliveryOrdersZone|RecoveryInspectRetains|RecoveryReconcileDoesNot|Submission)' ./internal/store/postgres
go test -mod=readonly -json -race -count=1 -timeout=180s ./...
python3 -B scripts/check_http_contract.py --output-dir "$FRESH_EVIDENCE_DIR" --source-sha "$FROZEN_TREE"
```

原Goose迁移/公开DTO/OpenAPI/npm lockfile未修改。新enqueue内部callback返回当前reservation，不与retry无新配额的validator混用；当前HTTP政策标记只用于DB UTC日和时间，不改trusted自定义计数窗。跨午夜有限quota最后发现日变化则409整笔回滚，可保持原key重试；真实等待跨UTC午夜未执行，仅历史午夜±1微秒计数边界与代码路径证明，不冒称全部时间竞争实证。

## 未完成与下一主线

1. 不再重复本批已闭合子包；把070做成有限的全入口事务/FK/trigger/等待清单。补开通、模板发布、SaveParsedMessage、缓存/索引状态、生命周期、完整队列状态、Key/旧写入与对象回调映射及具体未覆盖交叉关系。SaveParsedMessage晚tenant FK仅静态候选，未确认死锁。
2. 080仍缺42共享案例和HTTP/DB消费者、未定操作和层适用性。冻结实际error.code；message验证语义；reason按真实入口absent/optional/exact，而非要求全部增加reason（真实retry有可选reason字段，不能统一删除）。
3. G0原文允许绑定AC/后续任务的精确目标政策红灯，不要求先把P1–P7全绿；但不能用结构清单、未接线案例、timeout、夹具错误或skip关闭P0。070/080之后推进090夹具目录、100实际S/M基准、110兼容地图与120评审。
4. outer/inside全credential撤销时序、不同intent并发、Key scope原子读取尚未由本批逐分支证明；当前唯一公开公司POST拒Key，不能伪造已退休的Key提交入口。缺稿回放原revision缺少持久来源，留P5固定命令身份协议；未暗增新规则或迁移。
5. Subject/To/CC沿既有回执协议，不等于完整P2最小投影；普通收件期限、GC、公网DNS/SSE、shipping浏览器、规模性能、依赖审计和冷升级仍待后续阶段。

专属PG已stop0且PID文件消失，临时工具/快照/日志保留项目外。机器结果及归档见 `evidence/R5-B01-W-VALIDATION.json`、`evidence/R5-B01-W-LOGS.tar.gz`；归档不含responses.json、DSN、生产配置、源码tar或数据库目录。无push/PR/merge/release/deploy，原父复选框不改变。
