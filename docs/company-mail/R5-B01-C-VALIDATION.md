# R5 B01-C：原始权限编辑器联动与事务锁基线

2026-09-28。完成 `R5-P0-030` 的剩余组件证据，推进但**不关闭** `R5-P0-070`。当前 **6/171**，P0 **6/12**；七项产品缺陷仍未修复，G0尚未完成。唯一进度源为 [R5-TODO](R5-TODO.md)。

## 1. 本批版本与改动边界

开始HEAD `97675a743f83287b85297af4e94c339ce5a83bba`，从 `test/company-mail-r5-b01-inventory` 建立本地分支 `test/company-mail-r5-b01-components-locks`，保留原分支。本轮实现提交：**`57a7492e401d13dfe9c03eba3787d9030b2f7c8b`**。本地main及开始fetch后的origin/main仍为 `d6d512172fb6b874c3283d9df3f4d56758684a13`；没有推送、提PR、合并或部署。

实现提交包含13个文件：原始编辑器的独立Vitest审计夹具、Go父进程联动测试、五项真实PG锁观察、已有基线驱动的components分层、必跑名单、扫描器对测试后缀的明确分类及反例，以及运行说明和事务图。未修改生产Go/前端业务实现、锁文件、历史迁移；323个既有生产/支持文件与base对象逐字比较一致，13个历史SQL哈希不变。不是再次拆业务目录或引入平行权限系统。

运行采用base的独立git archive，再覆盖本批精确文件。测试副本509个文件在运行前固定；最终11个非Markdown实现文件与被测副本及实现提交逐一核对。测试后的运行说明和地图术语修改属于单独审阅的文档，不假称旧快照已包含新文档。最终锁测试增加失败时的探针回滚后，重新执行完整后端回归。

机器证据见 [R5-B01-C.json](evidence/R5-B01-C.json)；其source_sha是运行时base，必须与implementation_commit及各覆盖文件SHA-256一起解释，不能只拿base提交宣称它已包含本批新测试。

## 2. P0-030：补齐原始编辑器 → HTTP → 数据库

新增 `web/features/company/permission-editors.r5audit.tsx`，通过 `web/vitest.r5audit.config.ts` 显式运行。Go的 `r5_component_baseline_test.go` 拥有临时数据库、原始API路由、loopback HTTP服务和短期测试凭据，再启动Node/Vitest。

实际执行保留原UsersPage、PermissionsPage、SidebarProvider、Base UI菜单/对话框、SWR、I18n、API客户端和session层。只注入宿主auth context的admin展示身份，并补jsdom缺少的布局观察器；fetch限制为测试服务origin但不伪造响应。Go API仍验证真正的签名JWT并访问真实PostgreSQL。没有使用手工拼出的保存请求替代组件点击，也没有mock权限写入或数据库结果。

### A01：只改额度，已有覆盖被清除

先通过真实API设置员工禁发和单域名allowlist，再在原用户表格打开Permissions，实际修改额度为25并点击Save Overrides。服务器观察到组件只提交 `{"daily_send_quota":25}`；独立数据库读取确认quota=25、can_send=true、域名限制数量变为0。组件中“保留原限制”的安全断言失败，Go再次核对请求与最终状态后输出A01目标标记。

这证明不相关编辑会恢复基础配置中的能力，不等于员工因此获得任何其他邮箱的send_as授权。

### A02：旧配置表单恢复已撤销能力

实际打开原权限配置编辑器后，另一次管理操作通过原API客户端撤销can_send并确认false。旧对话框只改描述再点Save，服务器观察到两个PATCH：先false，再由旧表单带回true；数据库描述改变且can_send重新为true。组件安全断言与Go独立读取同时确认A02。

这是两个先后发生的管理操作与一个真实陈旧表单，不声称运行了两个真实浏览器窗口。将来P1仍需两窗口、CAS、审计失败及完整shipping-browser验收。

### 复现不等于修复

组件两项测试实际exit=1；Go父测试只有在目标组件断言、实际HTTP写入和DB状态吻合时才产生对应目标失败。Python驱动返回0的含义仅为 `baseline_reproduced`，证据明确 `product_fixed=false`。环境故障、编译失败、缺provider、0用例或skip不会满足目标。

本轮在最终源码上还重新运行了原七项DB/HTTP基线，A01–A07的目标失败全部再次命中。结合新增C/H/D层，030“七项失败复现”任务可关闭；不关闭任何P1–P4产品修复任务。保留的 [组件原始报告与请求/DB对照](evidence/R5-B01-C-COMPONENTS.json)、[组件父进程JSONL](evidence/R5-B01-C-AUDIT.jsonl)可以重查，测试token未写入证据。

## 3. P0-070：24组事务链与五个锁观察

[R5-TRANSACTIONS](R5-TRANSACTIONS.md)登记companyTx/companyReadTx、成员管理、grant/policy、草稿/附件、入队及trigger、逐收件人结果、入站/恢复、刷新族、GC、对象回调、索引和旧权限写入的实际顺序、原子边界、授权检查窗口和待验证交叉关系。

新增五项 `TestR5LockMap*`：

| 实际观测 | 结论边界 |
|---|---|
| 持有tenant FOR UPDATE时，既有草稿CAS仍可提交 | 只证明这条不改父键的更新无显式租户锁，不扩大为所有写入都不等待 |
| 新草稿INSERT等待被锁住的父邮箱 | 实际FK是用户/邮箱，不是想当然的直接tenant FK |
| 草稿更新持有用户SHARE，冻结等待其完成 | 冻结后新的旧actor写入被拒绝；不是撤回已经完成的请求 |
| 刷新轮换先持有family advisory，再等用户SHARE，随后才锁旧token | probe验证了真实顺序，非源码字符串存在性检查 |
| 发送入队已持附件SHARE后才等待sender用户SHARE | 与离职transfer_owned路径的target user→attachment写入次序相反；不把观察通过当作无死锁证明 |

每项使用独立PG、实际等待者查询与NOWAIT/try-advisory探针，拥有截止时间及清理；没有靠固定sleep猜测操作已执行。最终全量日志中的 [锁观察原始片段](evidence/R5-B01-C-LOCKS.jsonl)已保留。

**070继续未勾选。** 当前已证入队一侧的顺序，离职另一侧的顺序由实际调用/SQL支持；但没有完成两条完整生产命令相互竞争的完整等待环试验，不能宣布已复现生产死锁，也不能宣布不存在死锁。还需补授权读取后等待资源时的grant撤销竞争、其它隐式FK与对象回调持锁边界。没有改写原070验收标准来把写图等同完成。

地图另外如实标出：旧原件保护/回收路径确实在DB事务内调用ensureObject/del，不能声称全项目都已把对象I/O移出事务；GC关于“SaveDraft/Finish首先拿tenant锁”的旧注释也不能作为实际锁保护的证明。本轮保留原件保护实现，不作无验证的搬移。

## 4. 最终检查与未执行层

| 检查 | 最终实际结果 |
|---|---|
| Go build / vet | 通过；探针清理修改后再次vet |
| 完整Go race + 真实PG | 48个项目包；**554通过、0失败、1明确skip** |
| 后端必跑名单 | **19/19通过**，较上轮增加5项锁观察 |
| 普通前端Vitest | **108/108通过，0pending** |
| TypeScript、ESLint、Next生产构建 | 全部通过；29个静态页面生成成功 |
| Python工具回归 | **57/57通过** |
| Node AST/CLI/客户端清点回归 | **23/23通过** |
| i18n / 客户端矩阵 / DTO | 118生产文件、1325语言键；125客户端分支；16共享模型/32公司DTO通过 |
| 七项原DB/HTTP审计 | 7个精确目标失败，驱动确认复现；不混入普通554通过 |
| 原始编辑器联动 | 2个精确目标失败，组件、HTTP及PG互相印证；不混入普通108通过 |
| 源码/迁移与diff | 被测文件哈希匹配实现提交；历史迁移未改；diff检查通过 |

唯一普通后端skip为本轮未运行的 `TestR3BrowserJourney`。jsdom组件联动、Next构建都不替代shipping-image真实浏览器。没有执行远端GitHub CI、最新依赖安全审计或S/M负载，没有公网发信、生产数据库操作和部署。依赖复用前一批隔离npm安装，package-lock逐字一致；不把缓存复用称为重新npm ci。

## 5. 中间失败与修正

首轮组件缺少真实SidebarProvider，按环境失败拒绝计数；增加真实provider后重跑。锁测试曾错误假设草稿直接引用tenant，两个等待断言因此未命中；实际catalog证明草稿/receipt分别引用用户与邮箱，测试改为验证确实存在的邮箱FK等待。TypeScript发现getByRole不支持exact选项，改为锚定name正则，并重新执行组件及完整前端检查。

另有一次i18n CLI报告NameError，文件哈希核对无变化；相同隔离命令重跑以及最终完整工具回归通过，原因尚未建立，不将它写成已修复业务缺陷。相关原始日志保留。最后源码核对曾把测试结束后扩展的testenv README误算为运行时代码变化；已在证据中分列文档审阅哈希，运行时代码仍要求精确匹配。

所有这些失败与本轮有意暴露的七项产品缺陷分别记录，没有通过删断言、忽略错误或模拟写入让门禁变绿。

## 6. 下一批

先继续P0-070的入队/离职完整竞争与grant撤销窗口验证，并据结果决定是否需明确提前安排锁序修复；P0-080的020/030/060前置现已满足，可独立落协议真值表。P0-090仍等待070，不能越依赖启动；G0和P1状态不变。当前6/171不是产品完成比例或发布认证。

本机完整日志位于本批临时源码副本的 `.validation/`，具体目录和哈希见机器证据；临时路径可能被系统清理，仓库已保存关键原始JSONL及组件对照。运行资源只清理本轮session所属数据库和internal网络，缓存与工具镜像保留；没有更改既有公司服务。
