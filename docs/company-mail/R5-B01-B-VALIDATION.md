# R5 B01-B：入口、迁移与七项缺陷基线

2026-09-28。本批完成 `R5-P0-020`、`R5-P0-060`，并推进但不关闭 `R5-P0-030`。总进度 **5/171**，P0 **5/12**；B01/G0仍未完成，不进入P1。唯一进度源是 [R5-TODO](R5-TODO.md)。

## 1. 源码归属与提交

开始版本：`99f1e21fe1feee13a4c9cc4c997b1a2c7d3fbfa9`，原分支 `fix/company-mail-r5-b01-validation`。本批独立分支 `test/company-mail-r5-b01-inventory`，保留原分支。核对/抓取后本地main与origin/main仍为 `d6d512172fb6b874c3283d9df3f4d56758684a13`。

实现提交：`f5a958958fe61c0c35f8c3ebde0ab80ea71f8f15`。本批新增加的是测试、清点工具、基线数据与CI/Makefile接线；既有 435 个业务/测试/依赖文件对照base Git对象一致，未修改生产Go、前端业务、依赖锁文件或历史13个迁移。只有新测试进入internal目录。

验证使用base的独立git archive再覆盖本批精确文件，测前核对501个文件的字节，测后将16个实现提交文件逐一对照被测副本。不能只拿base SHA声称新测试已在base提交中存在。完整哈希、实现树和原始命令保存在 [证据摘要](evidence/R5-B01-B.json)。

## 2. P0-020：所有入口与真实客户端调用

[R5-API-MATRIX](R5-API-MATRIX.md)逐行登记127条HTTP入口的主体、tenant/资源、读写资格、版本、审计、内容分类和源码链。包含外层health/ready、公司接口、旧平台接口、Key入口、普通回执与运维例外读取；不是只列Web导航可见页面。

Go AST测试保留继承middleware、路由条件与handler；缺失/新增/重复路由、守卫变化、条件变化和缺少边界说明都会失败。NewRouter返回外层HTTP包装器，因而这是明确的源码组合清点，不是假称已经chi.Walk了运行时返回值。health/ready先于认证处理，其他公司管理资格还需数据库层复核。

TypeScript AST清点122个实际请求调用点，展开为125个分支：118个全部映射后端注册，7个通用request/company/download/SSE转发单独记录。注释、curl文档字符串不是调用；条件路径与方法只有相同判断才配对。未发现当前Web调用的14条路由仍保留API-only/兼容核查说明，不能据此自动删除。

`node scripts/collect_api_calls.cjs --check`校验代码与客户端矩阵一致；三项新Go哨兵加入后端必跑名单，由11项增至14项。原有四个CI任务、全量回归、依赖审计和浏览器任务均保留。GitHub工作流代码接入不等于本轮已远端运行。

## 3. P0-060：真实数据库结构与迁移基线

[R5-MIGRATIONS](R5-MIGRATIONS.md)与机器快照来自本轮新建、测试后删除的PostgreSQL，不是从SQL文件关键词推测最终schema。

| 结构 | 本轮实测 |
|---|---:|
| Goose版本 | 13 |
| 表 / 列 | 50 / 499 |
| 约束 / 其中外键 | 177 / 75 |
| 索引 | 152 |
| 非内部业务触发器 / 函数 | 7 / 7 |

完整列定义、FK删除规则、全部索引及触发器函数体已保存。重复Migrate后catalog不变，最终提交对应采集器重新生成的catalog也与保存快照完全相同。现有00009→00013的员工资产升级回填测试本轮实际通过，不能冒充未来R5迁移已验证。

另记录：所有表当前未启用RLS，隔离依赖应用/SQL/复合FK；这是结构事实，不单独证明越权。草稿JSON附件引用、外部对象和队列/资产间来源关系不能由FK清单完整代替。历史Down并非全部拒绝：00001为no-op，00011会删除派生索引表，其余相关迁移显式拒绝降级。没有执行Down，也不重写历史文件来改变事实。

## 4. P0-030：七项实际目标失败，不是七项已修复

`internal/store/postgres/r5_audit_baseline_test.go`使用 `r5audit` build tag保存待修安全行为回归，正常CI不据此把已知缺陷变成允许行为。每条断言期待安全结果，`run_r5_audit_baseline.py`只接受完整七项具名目标断言失败；编译失败、DB不可用、skip、0用例、缺marker、重复marker都不能算复现。后续修复时适配新协议并将相关用例移入普通必跑套件。

| 项目 | 本轮实际观测 | 证据层 |
|---|---|---|
| A01 | 真实HTTP只改quota后，数据库原禁发/域名覆盖被清空 | DB + loopback HTTP |
| A02 | 先撤销can_send，再提交旧表单字段，数据库重新变为允许发送 | DB + loopback HTTP |
| A03 | 到期资产内容接口404，旧outbound接口仍返回正文 | DB + loopback HTTP |
| A04 | 冻结旧凭据已失效，但管理员交接预览409 already inactive | DB + loopback HTTP |
| A05 | 有限期限共享邮箱邮件trash→restore后expires_at被清除 | DB + loopback HTTP |
| A06 | 十个正常大小草稿保护前100附件，三轮sweep后第101孤儿仍未清理 | DB + 真实GC函数 |
| A07 | 清理job后正文资产仍存在，但资产中已无原BCC地址 | DB + 实际归档/清理 |

七个Go测试本身实际返回失败，进程exit=1；基线驱动核对全部目标失败后返回0，含义仅为 `baseline_reproduced`，机器证据明确 `product_fixed=false`。[原始审计JSONL](evidence/R5-B01-B-AUDIT.jsonl)保留在仓库，不只保留聊天结论。

A01/A02本轮没有运行原始编辑器组件，HTTP输入按已审计行为构造；因此030复选框仍不勾选。并未声称完成真实浏览器全链或已修复任何A01–A07。

## 5. 最终验证

| 检查 | 实际结果 |
|---|---|
| Go build / vet | 通过 |
| 全量Go race + 真实PG | 48个包；549 pass、0 fail、1明确skip |
| 后端必跑哨兵 | 14/14通过，无缺失 |
| 唯一skip | TestR3BrowserJourney，本轮未执行的shipping浏览器 |
| Python工具回归 | 52/52通过，0skip |
| Node AST / CLI / 客户端清点回归 | 22/22通过，0skip |
| i18n | 118生产文件、1325键，632字面键通过；30动态调用另报 |
| DTO字段检查 | 16共享模型、32公司DTO通过 |
| CI YAML和shell | 四个任务保留，20个shell块bash -n通过 |
| opt-in缺陷基线 | 七个目标失败精确复现；不加入普通549通过数 |
| 历史迁移/源码覆盖/差异 | 13个历史哈希未改、实现字节与被测副本匹配、diff --check通过 |

包数48而非上轮49并非少测业务包：对照两轮原始JSONL，上轮额外包含安装在前端依赖里的 `tabmail/web/node_modules/flatted/golang/pkg/flatted`。本轮后端副本未挂载前端node_modules；48个项目包全部参与，新增10个Go测试/子测试通过。

第一次路由清点为生成原始清单而运行，因尚未建立矩阵文件明确失败；清单补齐后完整重新验证通过。这是建档顺序，不算业务缺陷复现。前端Vitest/生产构建本轮未重跑，不能沿用上一轮108通过当作本轮新增组件证据。

## 6. 复现入口与环境

复用B01-A工具镜像：Go1.25.7、Node22.23.1、Python3.12.14、PostgreSQL16客户端；独立临时PG和内部网络，无宿主发布端口。普通后端最多4CPU/6GiB，Node工具最多2CPU/4GiB。测试SMTP worker未启动，HTTP仅loopback，无外发邮件。

```sh
# 仅在可丢弃测试环境中，准备隔离DSN及按lockfile安装的前端依赖。
go test -json -race -count=1 -timeout=180s ./...
python3 -B -m unittest discover -s scripts/tests -p 'test_*.py' -v
node --test scripts/tests/*.test.cjs
node scripts/collect_api_calls.cjs --check
# 输出目录必须不存在；成功只表示已复现基线缺陷。
python3 -B scripts/run_r5_audit_baseline.py --output-dir /tmp/new-r5-audit --source-sha f5a958958fe61c0c35f8c3ebde0ab80ea71f8f15
```

本机原始日志在 `/tmp/tabmail-r5-b01b-4yr0309a/.validation/`，可能被系统清理；仓库保存摘要、关键原始审计日志、schema和复现工具。最终日志哈希见证据JSON。已核对session label并删除本批临时PG容器和internal网络，既有服务、工具镜像、源码缓存未改动。

## 7. 下一批与未完成边界

下一组：补P0-030的A01/A02实际组件联动，推进依赖已满足的P0-070真实锁/事务地图，再进入080协议真值表及后续fixture/性能/契约门禁。G0尚未关闭，P1不提前启动。没有推送GitHub、提PR、合并main、创建Release、生产迁移或部署；没有检查真实员工邮件。
