# R5 目标协议案例草案

2026-09-28，B01-D。依据 [R5-DESIGN](R5-DESIGN.md) 的既定目标，将内容、回执、留存、BCC 与离职语义整理为输入/输出案例。**这是尚未完成可执行核验的草案，不是生产接口行为清单，也不是已冻结的HTTP契约。P0-080保持未完成。** 实际入口和旧行为另见 [R5-API-MATRIX](R5-API-MATRIX.md)。

B01-D 时校验脚本未落地；本轮正常工具流程新增机器案例目录及命名测试参考驱动，新增部分共享输入 Go/组件消费者；HTTP 及其余案例仍缺，不能将结构检查当成协议验收。表中的状态码是目标响应类别；具体error.reason仍需与OpenAPI/Go/TS一起核验，不能将本表当成已经实现的字符串常量。

## 1. 通用规则和判定顺序

先验证当前主体及凭据，再验证具体租户/资源范围，再判断当前权利、生命周期和操作版本；数据库或资格查询失败不得回退为完整内容。管理身份不代替read，历史提交者不代替当前邮箱身份，缓存/任务/对象/索引存在不授予阅读资格。以下案例在未明确改变的轴上均假定其他前置条件成立；匿名与停用凭据优先拒绝。正常正文接口不因为运维人员的角色而跳过普通资格，恢复例外使用单独入口。

时间取服务端当前时间；等于截止即失效。收/发内容使用同一逻辑期限，物理清理进度不影响可见性。历史个人邮箱的异常期限必须按P3-020核查和保守迁移，不能直接套用新过滤进行批量删除。

## 2. 正文、回执与恢复

| 编号 | 输入或改变的条件 | 目标输出/拒绝 | 对应验收 |
|---|---|---|---|
| CT01 | 有效当前reader，原邮箱ID匹配，条目未到期 | 200，允许指定内容/附件；不扩大到别的邮箱 | AC-05、AC-28 |
| CT02 | 匿名或已撤销/冻结凭据 | 401，不打开对象 | AC-31 |
| CT03 | 公司admin或平台super_admin但没有该邮箱read | sent content 404，不凭角色放行；入站入口单独核对 | AC-05、AC-07 |
| CT04 | 历史发送者已撤去read | sent content 404；看得到提交回执也不能展开正文 | AC-05、AC-06 |
| CT05 | 邮箱地址被删除重建，ID不同 | 404，不把旧邮件转向新ID | AC-05 |
| CT06 | 同名资源属于另一租户 | 404，不能跨公司读取或泄露资源存在性 | AC-05 |
| CT07 | 当前域名范围不包含资源 | 403，不因grant或owner而放宽域名范围 | AC-05 |
| CT08 | 硬到期时刻小于或等于当前时刻 | 404，不打开正文/附件对象 | AC-05、AC-10 |
| CT09 | 回收站截止小于或等于当前时刻 | 404，物理记录仍在也不可读 | AC-05、AC-10 |
| CT10 | 权威存储查询失败 | 5xx、无正文，不回退为原始job JSON | AC-05、AC-06 |
| RC01 | 合法回执主体，内容已经失效，任务仍保留 | 回执可依政策200，但只有安全状态投影，view_content=false | AC-05、AC-06 |
| RC02 | 旧/outbound列表、详情、提交重放 | 与安全回执/同一内容资格适配，不再有独立正文旁路 | AC-05 |
| RC03 | 收件人结果包含accepted和permanent，后者恰好为BCC | 显示部分失败，隐藏BCC不能变为全部成功 | AC-06、AC-08 |
| RC04 | 任一收件人uncertain | 显示不确定，普通重试不可用；不能标为取消或成功 | AC-24 |
| RC05 | 全部收件人已被下一跳接受 | 表述为下一跳接受，不宣称最终送达 | AC-23、AC-24 |
| OP01 | 普通reader、公司admin或历史发送者要求例外读取 | 403，无恢复原件 | AC-07 |
| OP02 | 平台运维、选定公司、精确资源、有效理由、必要审计成功 | 允许受控恢复读取；无delivery token等执行凭据 | AC-07 |
| OP03 | 运维理由不足8字节或超过1000字节、仅空白 | 400，不执行例外读取；中文按UTF-8字节核验 | AC-07 |
| OP04 | 必要审计失败 | 5xx、无内容，不能先返回正文再补审计 | AC-07 |

普通回执不得包含正文、BCC、原件/附件对象键、token、原始headers或可回显正文的SMTP诊断。恢复读取也不应返回执行令牌。Key入口维持独立scope及当前属主边界，不因为它看得到某个任务就推导出互动公司管理资格；具体Key兼容映射由P0-110与P1-080逐项落实。

## 3. 留存和完整密送记录

| 编号 | 输入或改变的条件 | 目标输出/拒绝 | 对应验收 |
|---|---|---|---|
| RT01 | 有效共享邮箱条目，有限hard_expiry，trash后restore | 200，hard_expiry完全不变，只清删除/回收站标记 | AC-10 / A05 |
| RT02 | personal正常永久策略，hard_expiry为空 | restore/archive不会创建新的硬到期时间 | AC-11 |
| RT03 | hard_expiry或purge_after已达截止 | 恢复409，状态不变；读取按CT08/09拒绝 | AC-10 |
| RT04 | 只有organize而没有read | 403，不能操作或延长期限 | AC-10 |
| RT05 | archive/unarchive | 不暂停、清除或后移硬期限 | AC-10、AC-11 |
| RT06 | trash重复执行 | 不通过重复删除向后延长已有回收站截止 | AC-10 |
| RT07 | 草稿/已发送/队列/held原件仍引用对象 | 逻辑正文可失效，但受保护原件不因正文失效立即删掉 | AC-13、AC-14 |
| BC01 | 新提交包含结构化To/CC/BCC | 长期资产保存完整类别和附件；wire没有BCC头 | AC-08、AC-09 / A07 |
| BC02 | 旧job仍存在且tenant/asset精确匹配 | 允许从可靠结构化来源回填；重复回填不覆盖正确值 | AC-09 |
| BC03 | 历史来源已删除或无法确证 | 标明legacy_unknown；不得用空名单声称没有密送 | AC-09 |
| BC04 | 当前共享邮箱reader且条目存活 | 发件内容详情按同一read政策显示BCC，不另造平行权限 | AC-08 |
| BC05 | 普通列表、回执、SSE、webhook、metrics或网络MIME | 不输出BCC地址 | AC-06、AC-09 |
| BC06 | 删除投递job及recipient ledger后条目仍存活 | 完整发件内容、分类收件人和附件独立保留 | AC-08 / A07 |
| GC01 | 前100个候选仍受保护，第101个过期孤儿可删除 | 有界清理最终能够处理孤儿，前100个仍受保护 | AC-12 / A06 |
| GC02 | sealed draft或held原件持有引用 | 普通清理不得删除；创建tombstone也不自动到期 | AC-13、AC-14 |

RT03的操作拒绝与CT08的读取不可见并不矛盾：前者是写命令冲突，后者是不再提供内容。实现时必须一起覆盖实际收件、已发送、附件、原件与兼容回执入口。

## 4. 冻结、离职和权限编辑

| 编号 | 输入或改变的条件 | 目标输出/拒绝 | 对应验收 |
|---|---|---|---|
| LF01 | 目标已冻结、尚未资产处置，当前有权管理员及有效接收人 | 可预览/执行，全过程不重新启用旧账号 | AC-15 / A04 |
| LF02 | 同一有效生命周期的同一已执行plan重放 | 返回原回执，不重复转移/取消/审计 | AC-16 |
| LF03 | 同一次离职已有另一plan完成 | 409，不执行第二次处置 | AC-16 |
| LF04 | 相同数量但资产ID/revision或资格已变 | 409，重新预览，不以数量相同放行旧指纹 | AC-17 |
| LF05 | 接收人已冻结/跨公司，或试图自己交接/越管理层级 | 按参数或资格拒绝，无副作用；具体400/403/404边界待契约核对 | AC-04、AC-17 |
| LF06 | 重新启用/再次入职后的旧生命周期plan或旧凭据 | 不复活旧plan、grant或token；旧plan409 | AC-18、AC-31 |
| LF07 | 审计或中间写入失败 | 原子回滚资产、用户及计划状态，不能留下半交接 | AC-18 |
| PE01 | 个人覆盖禁发/限制域名，仅调整quota | 只改额度，其他原始覆盖保持不变 | AC-01 / A01 |
| PE02 | 未提交字段 / 显式null / false / 0 / [] | 分别表示保持/恢复继承/具体输入；原始覆盖不从effective反推 | AC-03 |
| PE03 | 旧profile表单修改描述携带旧安全字段 | revision不匹配409，不恢复刚撤销权限 | AC-02 / A02 |
| PE04 | 删除重建覆盖或重新分配profile后提交旧revision | 409，持久版本不能回到初始值 | AC-03 |
| PE05 | 授权校验后等待草稿行，管理员撤去邮箱发送grant | 二者有明确顺序：保存先完成或新权限先阻止；撤权完成后旧快照不能继续提交 | AC-13、AC-31 |

## 5. 尚缺的交付

本表尚未做到机器可执行、输入模式完整、Go/HTTP/组件适配器逐项对照；部分拒绝码边界仍明确待核对。因此080未完成，不能据此启动依赖它的110实现。后续须保留这些缺口，并把每项具体案例绑定到相同输入/预期数据与真实测试；不能仅验证Markdown行数就宣布协议验收通过。P1-P4原七项缺陷仍由各阶段修复验收关闭。


## 6. B01-V 机器案例子包（仍未冻结）

- [R5-PROTOCOL-CASES.json](evidence/R5-PROTOCOL-CASES.json) 编码本表全部 **46** 个案例的结构化输入、预期 HTTP 类别、语义拒绝类别、AC/A01–A07、必需证据层和未解决项。`semantic_code` 是目标分类，不是已经落实的 `error.reason`。
- `scripts/check_r5_protocol.py` 核对精确案例集合及真实 Go 测试符号，按 `unit/db/http/components` 执行命名参考测试，要求每个测试实际 run/pass、包成功，拒绝 skip/missing/失败。记录源提交、dirty 路径、案例与适配器源哈希和原始 Go JSONL。
- 19 个案例绑定现有真实测试参考。第二轮 RC03/04/05 与 OP03 已增加共享输入真实 Go 消费者，RC03/04/05 增加实际渲染组件消费者；其余 42 个案例仍没有共享消费者，所有案例仍缺完整必需层。参考测试 PASS 只能证明该测试的原断言，不能证明此 JSON 的全部目标、精确理由码或各层一致。组件基线观察器 PASS 表示成功复现旧缺陷，不是安全协议已实现。
- CT03/CT04 的 sent-content 错误码从草案 403 明确修正为 404，与 `CONTENT-BOUNDARIES.md` 及 `TestSubmissionAuthorLosesContentButKeepsReceiptAfterReadRevocation` 的当前断言一致。入站或域名拒绝码需单独绑定入口；不通过放宽为任意 403/404 集合隐去矛盾。

```sh
# 仅结构检查：成功不代表协议已冻结
python3 scripts/check_r5_protocol.py
python3 -m unittest discover -s scripts/tests -p 'test_r5_protocol.py'
# 实际 Go 单元参考；fresh output-dir 防止覆盖旧证据
python3 scripts/check_r5_protocol.py --run unit --output-dir /tmp/tabmail-r5-protocol-unit
# 只使用隔离测试 DB，沿用已有 fixture；不读取 .env、不连接生产
TABMAIL_TEST_DB_DSN='<disposable-test-dsn>' python3 scripts/check_r5_protocol.py --run http --output-dir /tmp/tabmail-r5-protocol-http
```

P0-080 仍未完成：要为所有案例接入共同输入、精确入口/理由码与当前 Go/HTTP/组件断言，且不能用基线缺陷观察器替代安全目标回归。P0-110/G0 等依赖仍按 TODO 原门禁执行。


### 第二轮共享输入消费者

RC03/04/05 的 JSON 输入现在包括真实 job state、分类地址与逐目标状态、in-flight 标记、敏感字段。Go 测试直接调用生产 `delivery.DeriveSubmissionStatus`、`submissions.RedactOutboundJobView`、`FilterRecipientsForJobView` 与 `OutboundCapabilities`，不按案例编号编写第二套政策。能力计算的隔离存储是 FakeStore，仅证明应用策略，**不证明数据库授权或 HTTP 原子边界**。

组件测试读取相同 JSON 及 Go 真实输出（带案例 SHA-256），将其送给真实 `SubmissionPane`；只注入 API 运输结果，不把目标状态/按钮值伪造成服务端输出。验证部分接受、下一跳接受、不确定提醒、重试按钮、BCC与敏感字段不展示。该证据为 jsdom 组件，不是 shipping 浏览器或实际 HTTP。

OP03 新增 11 个输入变体，涵盖空白、trim、7/8 字节、中文 6/9/12 字节及 1000/1001 字节。目标包含无效拒绝与有效边界控制；消费者调用生产 `credentials.AuditReason`，验证真实错误 sentinel 与持久化规范值。恢复 HTTP 的精确 reason 仍未冻结。

```sh
python3 scripts/check_r5_protocol.py --run shared-unit --output-dir /tmp/tabmail-r5-protocol-shared-unit
python3 scripts/check_r5_protocol.py --run shared-components --output-dir /tmp/tabmail-r5-protocol-shared-components
```

共享执行报告逐例列出已验证层和 `missing_required_layers`；报告及返回码成功仅表示所声明的共享纯策略/组件子包通过，`task_complete` 仍为 false。
