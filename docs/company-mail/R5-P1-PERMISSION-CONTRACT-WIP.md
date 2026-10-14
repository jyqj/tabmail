# P1 权限编辑协议：当前原子补丁切片

状态：**WIP，非 G1 验收结论**。本文覆盖原子个人补丁、profile CAS/删除影响确认及 profile 分配命令的已冻结 HTTP 契约；运行时能力仅在所有正式接线完成后由 integration owner 切换。profile 交互及真实 DB 竞争仍须独立验收，不得依据本文件勾选 TODO。

## 读取与提交

- 原始编辑读取：`GET /api/v1/admin/users/{id}/permission-editor`。
- 原子补丁：`PATCH` 同一路径，返回 `{ "data": PermissionEditorSnapshot }`。
- 旧 `GET /api/v1/admin/users/{id}/permissions` 和 `GET /api/v1/auth/me/permissions` 仍为 effective 读取，不用于回填覆盖。
- 旧 PUT/DELETE 已由真实 handler 直接返回 `409 CONFLICT`，不再写入覆盖；要求升级使用新协议。OpenAPI 标记 deprecated，移除旧成功响应及旧请求体。

```json
{
  "expected_revision": {
    "user_id": "11111111-1111-4111-8111-111111111111",
    "tenant_id": "22222222-2222-4222-8222-222222222222",
    "user_revision": "7",
    "profile_id": null,
    "profile_revision": null
  },
  "patch": {"daily_send_quota": 0, "can_send": false}
}
```

`expected_revision` 必须绑定 URL 目标及当前租户；版本是正 int64 的规范十进制字符串，不是时间戳、覆盖行 ID 或 JSON number。profile ID 与版本必须同时为 null 或同时非 null。旧观察即使经历删除重建/重新分配后数值相同也不能重新有效。

## 字段意图与来源

| 输入 | 意图 |
|---|---|
| 字段省略 | 保留原始状态 |
| `null` | 明确恢复继承 |
| `false` / `0` | 显式覆盖；0 保留既有无限配额语义 |
| 空补丁 `{}` | 不清空覆盖、不消费版本；仍检查授权和 CAS |

`profile` 是原始分配对象或 null；`overrides` 是原始覆盖对象或 null（无覆盖行）。覆盖对象的标量字段必须显式保留 null/false/0，不能由 effective 相等关系反推是否继承。`field_sources` 包含 8 个标量字段及 `domain_access`，取值为 `override/profile/default`。

域名输入使用 `domain_access`：

- `inherit`：继承 profile/default；也可发送 `domain_access:null`。
- `all`：显式全部允许。
- `none`：显式全部拒绝。
- `list`：必须有非空、无重复、非零 UUID 的 `zone_ids`。

非 list 的 `zone_ids` 可省略、null 或 `[]`，不得包含 UUID。未知字段、重复 JSON key、非法 UUID、负数和非整数配额由严格解码/值对象拒绝。effective 的旧空 allowlist 不足以表达 none，消费者必须使用 `effective.domain_access_mode`（`all/list/none`）域名模式，不得猜测；该字段仅为兼容旧 payload 可省略，legacy 空值不输出。

## Profile CAS 与分配

| 方法 / 路径 | 协议 |
|---|---|
| PATCH `/api/v1/admin/permissions/{id}` | `expected_revision` 正十进制字符串 + 原 profile 字段；返回 `{data: PermissionProfile}` |
| GET `/api/v1/admin/permissions/{id}/deletion-preview` | 返回 `{data:{profile_id,profile_revision,members,changes}}` |
| DELETE `/api/v1/admin/permissions/{id}` | JSON body `{expected_revision,confirmed_members:[PermissionRevision]}`；成功204 |
| POST `/api/v1/admin/users/{id}/permission-editor/assignment` | `{expected_revision,profile_id,profile_revision,patch}` 四键必需；返回 editor snapshot |

删除预览 `members` 为所有受影响成员的复合观察；`changes` 每项含 `revision/before/after`，前后值都是有效权限。UI 必须展示影响并提交用户确认的精确成员集合；不能以空确认或 read-only 能力说明替代真实预览。任何 profile/member 版本或成员集合变化均应409，不能自动换版本重放。

分配命令的 `profile_id/profile_revision` 必须成对非null，或显式 null/null 取消分配；省略不是取消分配。目标新 profile 版本与旧 editor 观察一起检查，`patch` 同事务写入，不得先分配再单独改覆盖。

profile PATCH 与个人覆盖 null 不同：profile 的可选 pointer 字段 null/省略均保留现值；`false/0` 则替换；`allowed_zone_ids:null`/省略保留，`[]` 为旧 profile 全域语义。profile PATCH 缺版本409。DELETE 空 body400；有效 JSON 对象缺版本409。OpenAPI 的 required 描述合法请求，不表示非法输入一律400。

当前 schema 的个人 `assign_profile` 能力仍按 owner 最后冻结值维护，不能仅因路由存在就改成 true。profile 列表/写返回的可观察版本字段还须对齐实际模型，不能凭 timestamp 猜 CAS 版本。

## HTTP 结果与客户端动作

| 状态 / code | 动作 |
|---|---|
| 400 / BAD_REQUEST | 修正非法 ID、版本或字段意图，不提交猜测值 |
| 401 / UNAUTHORIZED | 恢复认证 |
| 403 / FORBIDDEN | 操作者、角色层级、租户或 Key 入口不允许 |
| 404 / NOT_FOUND | 目标不存在 |
| 409 / CONFLICT | 保留输入；重新读取并人工复核，不自动换版本重放 |
| 500 / INTERNAL | 不宣称成功；重新读取确认不确定状态 |

错误使用 `{ "error": { "code": "CONFLICT", "message": "…" } }`，可有 `reason`。权限编辑使用 Bearer 互动管理入口；API Key 不能调用编辑命令。事务失败不能留下半笔覆盖/版本/审计。

## 证据与范围

Evidence → Finding → Path：

- `internal/company/permission_editor.go` 与 `permission_editor_decode.go` → raw DTO、复合版本和字段意图 → `internal/api/openapi.yaml` 的 `PermissionEditor*` schema。
- `internal/store/postgres/permission_editor_patch.go` → 空补丁仍 CAS、原子覆盖写入 → 后续真实 PG/HTTP 验收；本文测试不能替代该验收。
- `internal/api/handlers/respond.go` 与 `app_error.go` → 统一 envelope/error code → editor 响应/错误 schema。

轻量复验（不启动 PG、不触碰百万邮件测试）：

```sh
python3 -m unittest discover -s scripts/tests -p test_permission_editor_contract.py -v
python3 -m unittest discover -s scripts/tests -p test_openapi_contract.py -v
```

新增测试直接读取当前 OpenAPI，检查路由/envelope/raw presence、字段意图、domain 模式、int64/版本配对和 schema 变异；使用聚焦 evaluator，不宣称完整 JSON Schema 引擎或 runtime 覆盖。后续 integration owner 必须验证真实 handler、严格解码、授权、profile/default/effective 同快照、profile CAS/删除预览与精确成员确认、分配复合版本、审计回滚与 ABA 竞争；这些未完成时 G1 保持未验收。
