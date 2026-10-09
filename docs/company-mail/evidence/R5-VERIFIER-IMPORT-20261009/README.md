# API verifier 的 source-runner 导入补修

本维护处理 ADVANCE-20261009 最终 CI 揭示的验证测试导入问题，**计 0 个新增实施 TODO**。三轮 #197–#206 仍为 **10/10 已实际完成、剩余 0**，原父任务仍为 **10/171 已验收、161 未验收**。发布门槛保持。

## 原始 CI 与定位依据

核验对象为 [PR #210](https://github.com/jyqj/tabmail/pull/210) 的公开 head `ad6b39451b6a4206519e998bccf889f1308ad130`、[Company run 37833287011](https://github.com/jyqj/tabmail/actions/runs/37833287011)、frontend job `113503840648`。原 artifact `11573874798` 的 ZIP 为 76,420 bytes，SHA256 `aab0af1550efcf770b37045d3e274d42d2fd17b8392e2a2eb0eaa4ad3f0c5bed`，下载后的原字节与 GitHub API digest 一致。

原 ZIP 只含 `frontend-source-sha.txt` 与 `frontend-vitest.json`。记录的实际 checkout 是 `28e017cf31d65d4b554cc5c31711f7f4656aa416`；其完整 tree `19dbe626627213b474bbaa54f41740976a3e6def` 与公开 ad6、实际合并 `4730c648b1c31c34588d585256ff17f417a86b3a` 相同，root 已读取 commit API 独立核验。原 job 日志在 `run()` 的 `ids = discover(current)` 处抛出 `ValueError: test discovery import failure`，随后 exit 1。此位置早于 preparation 和 current/frozen 分派，并位于结构化 preparation 失败报告的捕获范围之外，因此没有 `frontend-source-version-tests.json`。CI 没有给出具体 loader.errors 或 discovered/executed/missing/skip 统计；不能将任何本地结果充作该 CI 的统计。

原全量 Vitest JSON 为 **2039 总计、2038 PASS、1 FAIL、0 pending、0 TODO**；唯一失败为 `r5-external-batch-probe.test.tsx` 的 `explicit private fixture required`。它属于 Vitest，和上述 source-version 的发现失败分开记录。[本 run 最终 job/step 原 API 回执](ci210-jobs-final.json) 显示：Company 整体 FAILURE；production-web/browser-journey SUCCESS；frontend 的 source-version、严格依赖审计、Vitest 失败；backend 的 PG race、validation tools、protocol 步骤失败。目录/TypeScript source validation、tsc、lint/build，以及 backend 的 route/client、transaction、HTTP contract 等步骤按回执各自记录，不合并为整体通过。

## 精确复现与修复

作者使用独立普通 clone、真实 Go 1.25.7 和原 CLI 的 `sys.path` 形状，只做测试发现与导入，不执行测试方法。固定 fdea 源正常导入；新增 API verifier 之后的 26a、ad6 和实际 4730 均出现唯一 `_FailedTest.test_r5_api_key_issuance`，具体异常为新测试第 7 行 `ModuleNotFoundError: No module named 'scripts'`。这是本地对同一公开源码和入口的独立复现；CI 原日志本身只披露较泛的 discovery failure。

原因是脚本入口的路径含 invocation `scripts` 和 clone 的 `scripts/tests`，不自然包含 `from scripts import ...` 所需的仓库根。修复通过该测试自身的 `__file__` 定位同一源码目录中的 `run_r5_api_key_issuance.py`，再用 `importlib` 加载。只改变导入前缀，整个原 class、`events`、`verify`、三个测试方法及文件尾部保持逐字相同；原 runner、workflow、Go/Web 产品、PG 实测用例及其判定均保持。

| 验证范围 | 精确源码 | 实际结果 |
| --- | --- | --- |
| 作者修复前发现 | `4730c648` / tree `19dbe626` | 819 个发现项含一个失败占位，唯一 API verifier 导入失败；测试方法实际执行 0 |
| 作者修复后发现 | `c6f64e16f4432e5c4f58cf4ad58431e141d307f4` / tree `e574f842350c2d483280a56593eb0d66f0cf3ea2` | 821 个不同发现项，0 导入错误；测试方法实际执行 0 |
| 作者原三个 verifier 方法 | 同一个 c6f64e1 | 3 PASS，0 FAIL/ERROR/SKIP/expected failure/unexpected success，前后源码 clean 且身份相同 |
| 目录守卫作者 | `e0de3cc0fa42a582e9ea1f8469571b0094493de5` / tree `f3c8519ad94e09d0c0bf93e76fafe91d308a953c` | 当前 revision15 原 protection 方法 1 PASS，前后源码 clean 且身份相同 |

**821 是发现数量，不是 821 个测试通过。** 原三个方法验证的是合成的执行回执判定，不是重新执行真实 PostgreSQL。原 PG18、603 Go race、181 UI 和 catalog 五模块 100 的结果继续保留各自原来的来源，不迁移到本补修源码。

## 目录保护仍为精确字节校验

API verifier 测试位于 revision15 当时冻结的 174 项保护 manifest。该 manifest 及其所有原 hashes/回执均保持，仍描述冻结产品源。当前保护循环仅对这个精确路径接受一个经过审查的变换：要求原完整导入前缀位于文件开头且出现一次，将其替换为已固定的新导入前缀，再比较**整个文件**。三个原方法、helpers 和任何其他字节的变动仍会被拒绝；没有将该文件加入可变忽略表。其他 173 项仍与原源码逐字相同。

守卫单独提交 `729bbe7ba10ed52ab50ff10a5021fe4e2a706585` 只修改当前 revision15 的保护方法。root 组合为 `a5b720d1db6d1932fced4877912cca86dd86117f`，完整 tree 与作者组合相同，均为 `f3c8519ad94e09d0c0bf93e76fafe91d308a953c`。原 33 个目录方法集合、120 个历史文件、72 个旧 REVISION 常量、五个原负控、四个目录 JSON、collectors、runner 与 CI 保持。包含本说明和证据的最终提交，其公开 head/tree、实际 merge 和新 CI 状态由 [PR #56](https://github.com/jyqj/tabmail/pull/56) 的后续维护回执登记。

## 原始证据与独立审查

- [作者与旧 CI 完整原始包](author-original-evidence.zip)：32 个成员，210,209 bytes，SHA256 `0ee9410315bdee365610b5eb2e2ff2221cfcd95fff7b61c31d915b37a57fdf3b`。root 逐成员核对原文件字节和索引中的 hashes 后打包；原 CI ZIP、完整 job log、原 traceback、各个 discovery 和三方法执行的失败/成功回执全部保留。
- [作者原始索引](author-summary.json)：原字节 SHA256 `1be66c45ef2a086aeed7cfe62eb4daa9e92a26d4000d7df59fc0905f6d982f62`。
- [frontend 独立审查回执](independent-review.json) 与 [完整独审原包](source-discovery-independent-review.zip)：在精确 root 组合 a5b720d1 / tree f3c8519 上直接调用未改的原 runner.discover/partition，821 个不同 ID、817 current/4 frozen、0 导入错误；发现未执行这些测试。随后原三个 verifier 方法真实 3 PASS，当前保护方法真实 1 PASS。将原 leaf_count 断言唯一字节 18→19 后，同一保护方法在整文件比较处实际 1 FAIL；finally 恢复原字节，最终 HEAD/tree/clean 与开始相同。最终 **ACCEPT**。ZIP 26 成员、85,342 bytes，SHA256 `3f85831d53570db3c125e311d91b0a783897182e6f986d37a376a92d7e73bd77`；原独审摘要 SHA256 `7b2e45aed2170255d6149ac21325448aa43756bdc70c9a33f4d6148fea214b7c`。root 打开原回执与日志、核对 ID 集与原包全部成员字节；没有重复运行后再叠加数字。
- [守卫作者原始两次启动包](guard-author-original-evidence.zip)：7 成员、3,278 bytes，SHA256 `d24f14b3fedd808513b90c36b54733c8a69dda9fca69903adcc5f92b0b352c03`。作者首次启动守卫漏了原先必需的 PYTHONPATH，目标方法未执行；该次 import ERROR 原样保留，未计为产品反例或成功执行。frontend 首次外部 probe 的相对报告路径在 chdir 后不可写，原输出也保留在其独审包中，正式重跑只修正外部报告路径。

本次仅解决已证实的入口导入回归，并保持目录的确定性保护。完整原必跑 PG、独立私有 fixture、严格依赖审计、有效 M/G0 与原父项验收继续开放；#56 保持 draft，main 不随此整合维护推进。
