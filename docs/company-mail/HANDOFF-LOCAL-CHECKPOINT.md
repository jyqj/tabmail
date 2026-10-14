# 本地收尾与可编辑源码交接

**这是用户授权的 GitHub WIP 检查点，不是发布、部署或验收通过。本地开发与测试在本次交接后停止。** 唯一任务清单仍为 [R5-TODO.md](R5-TODO.md)：本次实数为 **10 / 171**，原范围、复选框和父门禁均不改变。

## 1. 实际仓库与交付

- 本地仓库：`/Users/jin/Desktop/tabmail`。
- 分支及上游：`work/company-mail-r5-goal-20260930` / `origin/work/company-mail-r5-goal-20260930`。
- GitHub：[当前交接分支](https://github.com/jyqj/tabmail/tree/work/company-mail-r5-goal-20260930)。既有 `origin` 未修改，未 force/rebase。
- 恢复检查时，源码检查点 **`93005b644c1f39fa85072fe913156cf3d6c798e1`** 已提交且远端同 OID，工作树干净；该提交包含 887 个变更文件。没有再次提交或重复推送这批源码。本文件以其后单独文档提交补足交接，最终 OID 见 Git 历史及交付回执。
- 普通可编辑源码包括当前 schema19、API/main/shutdown/caller WIP、源码政策 v3，以及两套完整本地 fork：`third_party/enmime-v2.3.0`、`third_party/go-smtp`。两者的 MIT LICENSE、上游源码/测试、provenance/补丁均保留，不是归档占位或下载脚本。
- 两个精确 replace 分别为 `github.com/jhillyerd/enmime/v2 v2.3.0 => ./third_party/enmime-v2.3.0`、`github.com/emersion/go-smtp v0.24.0 => ./third_party/go-smtp`。fork 中 upstream 合成 MIME/TLS 夹具不代表私人邮件或生产凭据。

## 2. 已有证据的有效范围

以下是既有 immutable snapshot 的有限验收，**不是当前整个检查点的回归**；详情保存在 [最近中央报告](R5-USAGE19-C-OBSERVER-SHUTDOWN-ENCODING-STATUS-20261003.md)及其实际引用的公开安全附件。

| 层级 | 已交付事实 | 不可升级的边界 |
|---|---|---|
| 模板编码 | 8 top / 29 leaf / 34 PASS，非 race，source `51e578…` | package Elapsed 0.874s 不是 body/compile；wrapper 3.5408988s 为自身观测、origin epoch 未序列化，不补造外部时钟。没有 PG/HTTP publish/send 或 P5 父验收 |
| bounded workqueue | 24 top / 26 leaf / 28 race PASS，source `c0732a3f…` | package Elapsed 1.89s，wrapper 4.290512s；origin 虽序列化仍 self-report。只组件资格，旧 Stop 无界；随后 caller WIP 不继承组件运行证明 |
| usage19 | D1、D2 clean、D3/B/S 独立有限 actual PASS | 原 A FAILED、旧 D2 FAIL/上下文污染层保留；原内部回 base 的 PASS 不能转授。C0/V8 无完整 test terminal，C 仍 UNKNOWN；V9 observer 仅 SOURCE 候选 |
| 固定 enmime fork | V5 四目标 25 top / 154 leaf / 164 race PASS 的原 snapshot 有限资格 | 不完整 upstream 回归、Docker cold build、所有 CPU/heap 成本、P6 父项或当前双 fork 编译资格；旧 walker V2 FAIL/V3 结构 BLOCK 保留 |

## 3. 最新 SOURCE-only：继续工作前必须复审

- **main/caller**：main `d585…`、shutdown `57de…`、caller `420…`、API caller `38ef…` 的最新 SOURCE 已接入 API join，但尚未完成对应 SRC 续审与实际验收。不把旧报告的“caller 未接”当最新源码状态，也不把新接线当实际 drain 通过。
- **API background lifecycle**：10 文件、25 top 的 SOURCE 审通过，当前实际运行未验。独立构造的 Auth/Login owner 存在 foreign-lease 兼容限制；必须明确共享 owner。Revalidate fallback 的 released lease 不能作为 standalone 长期请求资格，具名残项仍需修复。
- **ROOT SMTP**：`8cc…` / `46d…`，15 leaf SOURCE PASS，actual 0。SMTP fork `7fff…` / `75be…` / `4c7a…`，14 top / 18 leaf SOURCE PASS，actual 0；根 `./...` 不会自动覆盖 nested module tests。
- **双 fork 源码政策 v3**：`scripts/r5_source_inventory.py` 当前声明 `r5_current_local_inputs_smtp_owner_forks_v3`、schema3、新 kind、`replacements[]`。4 文件仅 AST/diff 静态交付，**16 pure selectors 未运行**，需独立续审。只允许上述两个精确 module/version/path；未知第三 fork、替换路径/版本、nested、symlink、workspace 仍拒。原 v2 单 fork及 composed22 资格不转授 v3；原双 replace 的 SOURCE POLICY BLOCK / 0 tests 保留。
- v3 SOURCE capsule 原位置 `/tmp/r5-smtp-owner-source-policy-v3-20261003/`，合同 SHA256 `eaa7a9091f579c1a0403e8c352050bde986627b3473e343952d6bd80b1d591f4`。这个 private 路径不等于已在 Git 中传输了完整 capsule。
- 后续要求已准备的 **Go 1.25.7、`GODEBUG=asynctimerchan=0`**；当前双 fork root MVS/Go-selected inputs尚未观测，外部 cache 没有全字节 fresh 证明。SOURCE、GoList metadata、implicit build、cold build、runtime 必须各自登记。

## 4. 必须保持失败 / 未资格

1. **Whole backend / 四 CI**：原正式 package180 FAIL 不改绿。最新冻结18 `a331…` whole 有 WorkerRevocation/key-zone HTTP409 的失败 leaf及父项、104 top 未开始、19 whitelist PAUSE / 0 CONT；package timeout 不证明尾用例死锁。源码后续变化不能覆盖这些原 raw。
2. **百万 M**：原 source `852900…` / METHOD `d286…` / schema16，10800 秒失败；stored753380、ready753360、processing20、workload samples0，没有 result 或 Go test terminal、没有有效1M baseline。微诊断4000 PASS不解释753k深度主因，也不是 M retry / M通过。
3. **current19 Method/Catalog/SML**：尚未资格。C18 pure21 仅语法/CPU与RSS采样守卫资格；catalog15 pure只 synthetic，不 full semantic owned reference。主树19不得继承18/16参考或旧三S历史身份。v3 benchmark live/tool 在 METHOD 未迁移时明确拒绝，不能因 capture policy 支持双 fork而自动准许 S/M/L。
4. **PG phase R3**：B2/B5生命周期采样竞态、native后代覆盖仍缺；尚未正式 exact8 runtime，不以观察器方案推 whole180 改善。
5. **watchdog**：`scope_drained` 无置 true 路径、terminal buffer send缺真实 flush，serial正式 exact8仍 BLOCK / RUNTIME_DENIED。保留 **HARD1200**；开发 pure mocks不代表OS csops/census/hardkill/实际容纳安全性。
6. **Docker/shipping**：用户表示将启动 Docker，但未确认实际 ready；本次无 shipping 验收、无四 CI 全绿，未执行 Release、迁移或部署。

## 5. 下一环境的明确顺序

每个新作用域先确认独占 writer/真实源冻结，再由独立 reader 和唯一 executor 准入；以下顺序不是自动运行授权。

| 顺序 | 下一任务及验收依赖 |
|---|---|
| 1 | 新 SRC 独审双 fork policy v3 + 最新 main/API caller；处理 foreign lease / released fallback 残项，不能拿旧 owner审查或组件 PASS替代 |
| 2 | sole 运行新 pure16；实际新 capture / metadata绑定 root mod/sum、两完整 fork、embed、加载 helper与 Go-selected inputs；原 v2历史不回贴 |
| 3 | 根 MVS下 fork单次 race；验证 Go1.25.7 timer模式、root dependency graph与 new source identity；不只 nested module孤立图 |
| 4 | ROOT SMTP15 / API25 / caller23 top 27 leaf 的批准有限验收；保独立 Auth/Login、lease、seal/drain、晚到任务、错误传播与资源关闭顺序 |
| 5 | 真实 TCP+PG users/usage 锁及 SSE、SMTP ACK-loss、main role signal、lease dual-worker；证明完整 join后才关闭 PG/Redis，不把 mock/backend假夹具当 durable业务链 |
| 6 | 稳定 current19上的 whole CI与新 Method/Catalog/SML资格；先签新合同/完整 reference/tool门禁再批准 scale，旧S/M与 failed原件保持。Docker实际ready另确认 |

PGphase lifecycle、watchdog drain/flush、usage C未知资格分别保留具名工单，不聚合消失或预勾171父任务。

## 6. 安全交付与跨机边界

- Git交付普通源码与仓库中已有脱敏附件，不上传 `prod.env`、凭据、私人 URI、运行控制器/PG原日志、DB/object树、CPU/profile二进制、cache、`node_modules`、`.next` 或生成环境产物；原本地排除项保留原地，不删除。
- 本次源码提交后普通 Git工作树干净；ignored依赖/构建产物仍不在 Git，干净不等于所有私人原件均已传输。没有连接未知 Redis、停止系统服务或重跑产品测试。
- 私人 `/tmp` / `/private/tmp` 中原 raw、运行源树、PG/对象残留、外部 cache、签署材料及 frozen18完整 editable checkpoint **不随 Git自动转移**。只有仓库实际存在的 safe包/receipt可跨机使用；其 private-only依赖缺失应记 `unavailable / pending evidence`，不得补猜、复制聊天冒 raw或继承当前环境资格。
- Git HEAD是交付版本标识；报告里的 canonical closure SHA1可能不是 Git commit。旧 snapshot通过不覆盖当前 source，历史失败/unknown保持。
- 此次交接只增加本文件，不改原 TODO、历史证据、产品或测试。下一机器先设置项目索引并按需 `refresh_index`，禁止直接照旧 private运行目录继续派发。
