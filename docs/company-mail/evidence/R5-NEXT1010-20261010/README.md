# NEXT-1010 完成证据

本批 **10/10 实施子项已完成，剩余 0**；父级 **10/171 已验收，剩余 161**。完整行为、各轮数量与精确提交见 [完成记录](../../R5-NEXT-1010-20261010.md)。

## 完成与验收入口

- [机器完成清单](completion-summary.json)：三个实施 PR、十个 Issue、实际 merge/tree、验证数量及恢复边界。
- [第 1 轮 PostgreSQL](acceptance/round1-postgres.json)：原 19 个候选叶的公开比较收据。
- [第 2 轮 PostgreSQL](acceptance/round2-postgres.json)：同一完整产品 tree 的 30 个固定叶；refresh 两份完整原 Go JSONL 经长度和哈希校验。
- [第 3 轮 PostgreSQL](acceptance/round3-postgres.json)：最终精确产品源 50 个不同叶通过。
- [原最终 EXEC Go](acceptance/exec-go.json)：1857 个唯一叶的完整原流重解析。
- [原最终 EXEC Web](acceptance/exec-web.json)：229 个原断言及 TypeScript。
- [新的最终源前端执行](acceptance/native-frontend129.json)：实际再次执行的 129 个断言及完整非增量 TypeScript，前后 clean。
- [原目录完整门禁](acceptance/catalog.json)：875、106 和 3 个 CLI 的准确范围。
- [Root 独立 replay 复核](acceptance/root-replay-review.json)：11 个原始证据 frame 的字节、哈希与 Go 终态。
- [PR、分支与 Issue 终验](acceptance/final-branch-issue-audit.json)：实际 refs、merge parents/tree、10 个已关闭实施 Issue 和保留的历史工作。

## 原始材料归档

[完整证据压缩包](r5-next1010-evidence.tar.gz) 包含实际存在的原始日志、恢复的 GitHub API 记录、公开源码、读日志的验证脚本、交叉复核及新的前端运行记录。对应 [逐文件清单](manifest.json) 和 [归档校验结果](archive-receipt.json)。tar 内也包含 `ARCHIVE-MANIFEST.json`。

本次归档包含 **297 个现存证据文件**，压缩包 **3,037,821 bytes**；SHA256 为 `ffe6e22ed76ae7dc6c8d694085485d667f78dcd2b0f0d4d76a9356047f009381`。全部成员与源稳定性检查已实际通过。

每个文件都记录字节数和 SHA256；归档生成后重新读取全部 tar 成员逐一核验，并检查源文件集合与内容在打包期间没有变化。此本地产生的归档哈希与 GitHub 原 workflow artifact 的 API digest 是两类不同证据。

```bash
sha256sum r5-next1010-evidence.tar.gz
python3 -m json.tool archive-receipt.json
tar -tzf r5-next1010-evidence.tar.gz
```

压缩包内目录包括 `recovered/`、`backend/ci-r2-replay/`、`ci/round2-replay/`、`frontend/`、`final-docs-review/` 与 `tools/`。重新取得的 GitHub日志保留实际来源；新的恢复验收文件不冒充丢失前同名文件的原字节。部分 `transfer-parts` 是原日志传输分片，完整日志另行保存。

## 证据限制

旧本机原始流、部分原本地审查文件及旧 native bundle 在工作区重置中丢失，没有根据摘要重造。新的 129 前端断言是在公开最终源 `177ee571` 上真正再次执行。旧 PG 比较收据中的内部 JSONL 哈希仍标为原 runner 报告；新 refresh replay 明确恢复并校验了两份完整原 JSONL。

原 Docker 限流、临时 workflow 静态校验、诊断查询和旧完整 release CI 失败保留为失败。范围重叠的测试数量不相加。#56 保持 draft；父任务、生产崩溃恢复、外部邮件送达和完整发布条件仍按各自验收执行。
