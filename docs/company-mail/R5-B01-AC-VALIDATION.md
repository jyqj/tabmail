# B01-AC：当前凭证准入与scope迁移实证

被测tree `9a5418ccdd4882c1a078816fbdd138317ea78407`／753文件；57940最终同次20门禁0，默认1431pass／0fail／1独立browser skip、52包、234必跑齐全。Python263、Node23、Vitest118、clean Next／tsc／lint均0，冻结源无漂移。新成绩不是旧AB绿。

## Evidence → Fix → bounded proof

- 原0fba合法Key POST实际23514，current scope CHECK缺suppressionread/manage，权限race当时未执行。正式新增15仅扩两已被app/API支持scope，不改历史1..14；fresh／真实旧14 GooseUp→15／旧keybytes／原12+新2双ownership／emptyunknown23514／重复两次均实测。真实Down14因新scope拒绝，版本15／CHECK／keys全回滚保留。
- 权限基线明确原0fba+schema15+同6202，正常5绿，role／active／SV／Key删除四实际stale204红。候选仅四Actor文件差异，三轮各11pass：role／active等待Guarded后current403；Pwd与Key是持U／exactKey行fence到commit，source先提交再credentialwriter合法串行，**不是四场景都撤权后拒绝**。Key trace明确DELETE，不借异步Touch PID。
- 正式HTTP改明确Actor Authorized port：TKEY→JWT当前U SHARE／active／原SV0／角色，或Key owneractive（不继承JWT SV／admin）→exactKey SHARE NOWAIT／current management scope／identity；与S／required audit／最终Key DBclock同Tx。audit五身份字段绑定verifiedprincipal，不当credential。旧Audited只trusted兼容；Fake不证明PG并发，audit期间Key到期分支未动态覆盖。

## 失败与最终回归分层

原8376 full16/20：三旧schema14断言和inventory误混未冻结AD caller；早期只报告先见两版本错误，完整扫描补正第三。37aa full18/20仅残留第三v14断言。三处改固定15，历史14升级控制与backfill index／receipt各1、conflict1、catalog repeat／hash强断言完全保留。Caller inventory从明确snapshot重抽，未放宽checker。目标实际0后最终9a54 full20/20。

初down-labeled runner误匹配authority测试原log保留、不当Down证明；精确migration-down-final才是Down控制。所有失败raw见[机器记录](evidence/R5-B01-AC-VALIDATION.json)与[脱敏归档](evidence/R5-B01-AC-LOGS.tar.gz)，不覆盖、不洗目标红。

## 边界与资源

fresh官方New/Migrate15真实catalog75FK／7user trigger／34CHECK（非生产）；352函数／456SQL执行调用仍是结构及bounded proof，不全图PASS。共享DB46／32精确目标红，17真实组件／26包=12target+14scope pass，OP03额外unit不算第18组件；controller0非产品绿。

完成仍8/171。070其他有限profile FK／event批次关系待AE／AF实际证据，既有domain资格与plan期限按原准入时点记录，不自创最终commit政策；090依赖070未过，不勾G0或全部任务。AD14／AE／AF所有新增文件排本批source／回归／提交。

全部测试进程terminal；隔离PG34679继续只用于授权下一队列，未假报停止。提交相对被测tree只后验文档／报告／产物变化，产品／测试相同。
