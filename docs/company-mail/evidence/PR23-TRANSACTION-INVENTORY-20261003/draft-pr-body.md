PR23 固定 aeed47f0e1998d9925acb680992b051463821d69 的 transaction manifest 落后于生产源码，导致 CI 37151630259 frontend job 111286597343 的 test_current_inventory_is_only_structure_evidence 在函数集合检查失败。

逐项核对45新增、2删除和33旧函数变更体，刷新当前AST、caller、62文件与19迁移的源hash及精确断言。明确区分自有事务、借用事务、session GC、外部回调、纯函数和usage包常量写；变更体仅source-only，历史runtime/acceptance records归档。保留原FAIL；producer/guard及生产业务/锁、benchmark/descriptor/中央TODO原字节未改。

Validation: inventory CLI PASS; 26 transaction unittest PASS，包括缺失/过时/错receiver、新纯函数、常量hash、callback与FK/trigger等拒绝反例。task_complete=false/runtime_verified=false。PG和完整集成/新CI NOTRUN；其他prepared errors/failures保持。

详见 docs/company-mail/R5-TRANSACTION-INVENTORY-PR23-20261003.md 与同名独立evidence目录。禁止merge/deploy；draft only。
