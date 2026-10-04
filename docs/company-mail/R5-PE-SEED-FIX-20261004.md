# PE component fixture setup repair — 2026-10-04

This repairs setup only for PE01/default, PE02/omitted/null/false/0/[], and PE04/default. No formal component case was dispatched or qualified. The original single formal run remains rejected: source `f875672dea2b68fdec9e0b986f4863724da5d7ba`, 53.781s, 17 pass / 9 fail / 17 missing / zero qualified. Its unchanged report and old failures remain at [formal evidence](R5-FORMAL-COMPONENTS-F875-20261004.md), retained by the evidence-only parent `46de5010b7dc79b841c52d6ff7245d070a846a76`.

The setup previously attempted legacy `PUT /api/v1/admin/users/{id}/permissions` expecting 200. The shipping handler deliberately returns 409. The replacement reads `GET /api/v1/admin/users/{id}/permission-editor`, then submits `PATCH` to that same path with the complete observed `expected_revision` and this raw patch:

```json
{"can_send":false,"daily_send_quota":19,"domain_access":{"mode":"list","zone_ids":["<owned fixture zone ID>"]}}
```

The revision binds user ID, tenant ID, decimal-string persistent user revision, and paired nullable profile ID/profile revision. It is read from the shipping snapshot, never synthesized from row identity, timestamps, or effective permissions. The patch is a JSON map so omitted fields stay absent; null means inheritance, false and zero are explicit inputs, and domain access uses the supported mode/zone list rather than legacy `allowed_zone_ids` input. A nonempty list establishes the original intended restriction. Current empty patches retain state and revision; stale observations must receive 409. Storage triggers own revision advancement.

The helper independently GETs the raw editor again and verifies false, quota 19, exactly the owned zone, explicit list mode, override field sources, and a changed revision before allowing child dispatch. No case input, expected property, marker, acceptance mapping, status assertion, production handler, TSX, Go module/replaces, lock, or formal budget was changed.

## Focused regression evidence

`TestR5PermissionSeedSetup` reads the unchanged original catalog and invokes the actual setup for all seven variants. Each creates an owned disposable PostgreSQL database and the shipping router over HTTP with an issued administrator JWT. GET/PATCH/GET must all return 200. An independent observer connection reads raw override columns and `users.permission_revision`, verifying they match the fixture and raw HTTP snapshot.

`TestR5PermissionSeedLegacy409AndCAS` checks authenticated legacy PUT and DELETE both return 409/CONFLICT and leave stored user/override/audit state unchanged. It checks old CAS rejection, current empty-patch preservation, explicit inheritance reset followed by restoration of the same raw values, and rejection of the earlier revision after restoration. This is a same-value revision probe, not a claim of formal PE04 coverage or physical override-row delete/recreation coverage.

Final focused command (Go 1.25.7, original two replaces, readonly modules):

```sh
TABMAIL_TEST_DB_DSN='<owned fresh local cluster admin DSN>' go test -tags=r5protocol ./internal/api/handlers -run '^TestR5PermissionSeed(Setup|Legacy409AndCAS)$' -count=1 -timeout=120s -v
```

Result: PASS, package 1.686s; seven setup subtests and the separate legacy/CAS guard passed, zero skipped. A preceding focused iteration passed in 1.569s before adding the same-value restoration check. Neither command is a formal 43/26/46 rerun or business qualification. No response contract mocks, actual email/accounts/data, wholePG180, audit batch, default675 or sharedDB import were used. The real router's existing fixture transport recorder forwards actual handler responses; it fabricates no responses.

Evidence: [final raw output](evidence/R5-PE-SEED-PROBE-20261004/setup.raw.log), SHA256 `b80a3b0c4adf0b9f8337bde4841e8c512b9135b85c08ee8650ba704c799b16da`; [independent cleanup counts](evidence/R5-PE-SEED-PROBE-20261004/cleanup.raw.log) report zero `tm_test_%` databases and zero associated backend connections before successful owned PG shutdown. The cluster was freshly initialized on a separate port; the existing workspace database was not used.

Original catalog SHA256 remains `f46d58c45dbf66e48eb1e379fd019aa5c02dc153f004548a69752f45dc52b1b7`. Formal evidence, catalog, production files, Go module files and web lock remain byte-identical to their supplied parents.

## Remaining hunks for the root owner

`web/components/company/r5-protocol-shared-components.test.tsx` remains unchanged. Exact original-source locations requiring a separate approved adapter repair:

- Line 19 imports legacy `setUserPermissionOverride` and `deleteUserPermissionOverride` and the effective-only `getUserPermission` reader.
- Lines 291 and 308 read effective-only before/after values; these cannot prove raw override intent or establish the compound revision observation.
- Line 298 (PE04) calls legacy DELETE then PUT; both deliberately return 409, so this cannot establish a successful revoke/recreate event against a stale form.
- Lines 295–305 select controls and the legacy `Save Overrides` button; the shipping editor's controls/intent must be reconciled without changing the seven original variants or expected properties.
- Lines 306–307 wait for and select a legacy PUT to `/permissions`; current saves use PATCH `/permission-editor`.
- Lines 309–316 inspect legacy top-level mutation fields, `allowed_zone_ids`, and effective state; current intent is under `body.patch`, uses `domain_access`, and requires compound CAS plus independent raw/persistent readback. Existing PE01/PE02/PE04 target markers and acceptance must remain intact.

The compatibility helpers themselves still send legacy commands (`web/lib/api/permissions.ts:51–61`); leave their deliberately rejected contract intact and use the existing versioned editor helpers for the adapter migration. No shared TSX or production permission-handler changes were made here. PE03 and LF work belong to their respective owners. Central10/171 and all other catalog layers remain open. No merge, deploy, or closure is claimed.
