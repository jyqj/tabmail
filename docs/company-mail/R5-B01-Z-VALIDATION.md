# R5 B01-Z：实际删除行资格、共享转换与最终数据库关系

2026-10-01。基线354dd71e，完整171目标不缩减；父仍6/171，070/080/G0未勾。唯一integration operator管理统一PG、生产和全量，root只分派；测试／只读review持独占写集。

## 精确源码

最终被测tree `1ec0abde9887cf7f7609dfb5dcd5e3ad68f9d77b`、726文件逐一SHA复核。独立Git上下文commit的tree相等，不冒称用户提交。此scope仅当前Z三test（restore64fc、template781a、convert d8fb）、最小生产、库存／226必跑；tenant9c679和开发中RC02/080新client／组件／producer均明确排除，没有沿Y绿色或未来源码成绩。

## Evidence → Finding → Path

| 原source同最终test | 实際关系 | 最小修复／裁决 |
|---|---|---|
| restore10事件8pass2fail（唯一leaf红＋parent）→三轮10pass | restore先成功清deleted/purge/expires；retention已筛旧eligible且等source后只按id删除，错误返回committed key | DELETE实际target m重复完整原eligibility，EPQ按等待后m再筛；RETURNING实际m mailbox/key。strict<／原ORDER+LIMIT／owner/shared永久保护／计数／commit后receipt不改 |
| template7事件全部绿（5leaf＋2parent） | Publish T UPDATE与Revoke T KEY SHARE先互斥，完整两方向/CAS/immutable/auditfailure/idempotent成立 | 排除仅凭template/version词法倒序假称40P01；不改模板生产，也不外推其它caller |
| convert6事件3pass3fail（2leaf红＋parent）→三轮6pass | 转换成功清expiry后旧candidate stale删；反方向conversion真正40P01且victim完整回滚 | 原T/adminactor/M/revision与五转换字段保留；active source MATERIALIZED按id NO KEY UPDATE NOWAIT，只locked subset＋重复qualification清expiry，55→原409同Tx回滚；与actual-row资格修复共同验证 |

失败rollup不当独立漏洞数。各snapshot原source、最终test SHA、原失败与候选日志均保留；restore修复后的绿不能冒称convert旧source红已验证。

## Fresh统一验收

同immutable程序一次默认full，原180秒：**1405pass／0fail／1明确browser skip**，52包，**226必跑全部pass**，PG166.044秒。19step全部exit0：build/vet、库存、Python247、Node23、默认Vitest118、cleanNext／tsc／lint、HTTP80/65/66、negative execution、契约16/33/67、官方modules验证。

独立DB协议46case／29target红、组件16case23variant／12target红11scope pass，两controller通过但product_green=false；不是默认1405绿数，也不是新AA源码成绩。RC02精确legacy UI／BC02–03正式future backfill写入适用性继续欠缺。

## 最终actual catalog与有限映射

新空fixture经正式PgStore.New/Migrate、PG16.13实际pg_constraint/getdef及user triggers导出：**75 FK／7 enabled user triggers**，goose0和1–14已应用；源Up的77 REFERENCES声明不是final75，也不是生产catalog。

[最终catalog](evidence/R5-TRANSACTION-DB-CATALOG.json)记录validation/deferrable、delete/update动作、trigger/function body及350entry保守literal／name-closure links。关联表trigger名单不等operation实际fire：Convert改expires不触发UPDATE OF rawkey索引；Publish INSERT版本不触发immutable BEFORE UPDATE。TX21对象回调＋rawkey advisory即使DML/FK映射空也有effect。没有把conservative closure当dynamic dispatch。

临时export工具首次用了自造0 lifetime而非正式default300s，pool获取失败属于工具配置；改标准配置后官方Migrate和dump实际成功。原失败保留，full程序没有为export改动。

## 交付与剩余范围

[摘要](evidence/R5-B01-Z-VALIDATION.json)、[175成员raw归档](evidence/R5-B01-Z-LOGS.tar.gz)SHA可复算；无responses、DSN、私有fixture/token、源码tar、DB或node_modules。所有Z进程已terminal；唯一PG34679继续授权后续tenant／AA租约，**没有假称stop0/PID消失**。无push/PR/merge/release/deploy。

070有限matrix仍要actual caller/config/state-owner与明确未证关系审查，不要求350函数每条动态但也不以本子集绿全部关闭：DeleteMailbox内部未连formal route／拒company，不虚造公司删除；CreateMessageWithQuota是Durable=false legacyfallback，公司强Durabletrue；DeleteTenant正式superadmin current约束行为待9c679实际控制，23503或条件skip不等任务PASS。其它callback提交不明、GC公平性、批多source等按原任务保留。新080按原条款推进，不免RC02层、不伪未来writes。TODO仍原171／6位，不另造平行任务源。
