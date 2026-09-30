# AB兼容后验：P0-110清点与协调方案验收

基底`1a14ce506a06763991f9fa2f9950eb1afae17bdf`，产品被测e2d744文件。**本批只后验工具／文档，不重跑或冒新checker整树full成绩**；原产品20门禁0、1417/0/1、229必跑证据保留。

## 原条款逐项裁决

| 条款 | 证据／边界 |
|---|---|
| 依赖020／050／080 | 在本批actual验证之前已验收 |
| OpenAPI／Go／TS／真实响应差异清点 | 127真实GoAST route／127 NodeAST分支；122 OpenAPI／57 DTO绑定。safe actual wire193状态／aspect、75路由；52明确未观测，5 schema缺项保留 |
| 旧客户端／API Key使用者 | 当前115有shipped webcaller、12无；实际middleware／scope／客户端源码明确，外部SDK／Key用量未知，不编无使用者，不读取生产usage |
| 协调发布批次 | P5模板／草稿、P6恢复／收发、P7索引事件语义按原TODO核对，拒绝ID存在但阶段错误的旧方案 |
| route→schema→client→test表与字段检查局限 | 明确inline request schema及opaque／null／动态payload／无DTO／登记不等执行等边界 |
| 每项breaking拒旧写方案＋同批升级条件 | 权限CAS/遗漏保留；BCC安全投影与可信迁移；retention截止语义；offboard plan/revision；template/draft/source token；recovery revision；索引/events/cursor；各批绑定真实客户端及原任务。都是实施条件，不说本批未来政策已生效 |

**110清点交付与协调方案验收，TODO8/171、P0 8/12；070/G0未关闭，未来产品任务不减范围。**

## 后验实际运行

integration operator从最终四SHA独立执行strict checker `--wire-evidence-root`，实际重哈希29安全artifact、finalmanifest、tree/privatecommit、spec/case SHA并exact canonicalroute join；13反例方法、现contract drift、NodeAPI inventory均exit0。193观察不是127全部runtime通过；target red保原marker层，无未知错误洗绿。

原DNStruth错标的staticgreen/raw不覆盖；正确排除仅GET verification与POST verify，POST domains create实际覆盖。后修checker／tests／地图与被测e2d差异单独列于[机器证据](evidence/R5-COMPATIBILITY-POST-VALIDATION.json)，[后验原始日志](evidence/R5-COMPATIBILITY-POST-LOGS.tar.gz)八成员逐hash。
