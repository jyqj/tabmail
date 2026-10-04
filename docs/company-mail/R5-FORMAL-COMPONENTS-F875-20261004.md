# Formal components run at f875672 — 2026-10-04

**BATCH_REJECTED. One formal batch started; no rerun.** All staged children remain unqualified.

Fixed source: `f875672dea2b68fdec9e0b986f4863724da5d7ba`. Batch contract SHA256: `17a3d0bedf8b1f0e897a740f1956d1ab26d876919e0540432f88df03c9be1d29`.
Safe report SHA256: `7dbcc94223f82606af4a646d79fb8231e677102ff71a33c7b96402c893d3c896`.

Unchanged Go1.25.7, both local replaces, official-lock lifecycle-enabled npm ci, Go120/batch180/case75/max4. Fresh dedicated clean real checkout, external dependencies and original runtime-v2/archive/default/race observations. No historical receipt inheritance.

43 required terminals = 40 Go + 3 Python. Handler scope is 17 IDs / 26 variants; catalog has 46 IDs. Observed 17 pass, 9 fail, 17 missing, zero skipped. Zero terminals or case layers qualified. Credentials (11), delivery (3) and direct Python (3) did not run after handler acknowledgement rejection. No recorded child timeout/tail; peak4.

Seven PE01/PE02/PE04 fixture variants failed before child dispatch: the fixture expects permission PUT 200 but the current legacy handler unconditionally returns 409. PE03 fails the exact second-PATCH count assertion at TSX line 268 (one observed versus two expected); deeper UI cause remains unknown. LF01 reaches declared target marker R5_PROTOCOL_UI_TARGET_LF01_FROZEN at TSX line 210. Go owner acknowledgement is rejected; passing children receive no qualification.

Full preflight passed. Mandatory in-batch terminal postcheck was withheld because exact owner acknowledgement was absent. A separately labelled complete post-run original source/dependency validation passed in 6.146s; it cannot substitute for the missing in-batch barrier.

All 21 registered process roots were directly reaped, process groups joined and pipes closed. RPC channel joined, pending roots zero, cleanup errors zero, lease released and token absent. Independent owned-cluster query found zero fixture databases and zero fixture backend connections before successful PG shutdown. Process cleanup does not fabricate fixture-owner acknowledgement. OS cleanup tails have no hard 180-second return guarantee.

Elapsed: preparation 49.306s; compile-only warmup 47.639s (zero tests); formal 53.781s; handler Go parent 44.930s. Event-window and launch observations are recorded in report.json; exact phase attribution beyond those observations remains unknown.

One prestart CLI environment rejection occurred before output/lease/test creation (unbound GOTOOLCHAIN). Owned launch setup was corrected; this is not a second formal batch. A separate diagnostic postvalidation initially rejected inherited NODE_PATH before inventory reads, then passed with the prepared clean environment. Both errors and raw evidence are retained privately, with safe hashes.

Raw logs, fixture packets, reports, manifests and response bodies remain under the private evidence root outside source/dependencies. Only aggregate status, terminal identities, layer matrix and hashes are published.

Central10/171, wholePG180, audit and other catalog layers remain open. default675/sharedDB89e7 excluded; no actual email/user data, test/assertion/budget/marker changes, merge or deploy.

## Catalog-derived terminal matrix

| Terminal | Outcome | Qualified |
| --- | --- | --- |
| `TestR5ProtocolAuditReasonSharedCases/OP03/eight` | missing | false |
| `TestR5ProtocolAuditReasonSharedCases/OP03/empty` | missing | false |
| `TestR5ProtocolAuditReasonSharedCases/OP03/maximum` | missing | false |
| `TestR5ProtocolAuditReasonSharedCases/OP03/over_maximum` | missing | false |
| `TestR5ProtocolAuditReasonSharedCases/OP03/seven` | missing | false |
| `TestR5ProtocolAuditReasonSharedCases/OP03/trim_maximum` | missing | false |
| `TestR5ProtocolAuditReasonSharedCases/OP03/trim_seven` | missing | false |
| `TestR5ProtocolAuditReasonSharedCases/OP03/utf8_nine` | missing | false |
| `TestR5ProtocolAuditReasonSharedCases/OP03/utf8_six` | missing | false |
| `TestR5ProtocolAuditReasonSharedCases/OP03/utf8_twelve` | missing | false |
| `TestR5ProtocolAuditReasonSharedCases/OP03/whitespace` | missing | false |
| `TestR5ProtocolComponentObservations/LF01/default` | fail | false |
| `TestR5ProtocolComponentObservations/LF02/default` | pass | false |
| `TestR5ProtocolComponentObservations/LF03/default` | pass | false |
| `TestR5ProtocolComponentObservations/LF04/default` | pass | false |
| `TestR5ProtocolComponentObservations/LF05/foreign_tenant` | pass | false |
| `TestR5ProtocolComponentObservations/LF05/frozen` | pass | false |
| `TestR5ProtocolComponentObservations/LF05/higher_role` | pass | false |
| `TestR5ProtocolComponentObservations/LF05/self` | pass | false |
| `TestR5ProtocolComponentObservations/LF06/default` | pass | false |
| `TestR5ProtocolComponentObservations/LF07/default` | pass | false |
| `TestR5ProtocolComponentObservations/PE01/default` | fail | false |
| `TestR5ProtocolComponentObservations/PE02/0` | fail | false |
| `TestR5ProtocolComponentObservations/PE02/[]` | fail | false |
| `TestR5ProtocolComponentObservations/PE02/false` | fail | false |
| `TestR5ProtocolComponentObservations/PE02/null` | fail | false |
| `TestR5ProtocolComponentObservations/PE02/omitted` | fail | false |
| `TestR5ProtocolComponentObservations/PE03/default` | fail | false |
| `TestR5ProtocolComponentObservations/PE04/default` | fail | false |
| `TestR5ProtocolComponentObservations/PE05/default` | pass | false |
| `TestR5ProtocolComponentObservations/RC01/default` | pass | false |
| `TestR5ProtocolComponentObservations/RC02/legacy_outbound_detail` | pass | false |
| `TestR5ProtocolComponentObservations/RC02/legacy_outbound_list` | pass | false |
| `TestR5ProtocolComponentObservations/RC02/submit_replay` | pass | false |
| `TestR5ProtocolComponentObservations/RC03/default` | pass | false |
| `TestR5ProtocolComponentObservations/RC04/default` | pass | false |
| `TestR5ProtocolComponentObservations/RC05/default` | pass | false |
| `TestR5ProtocolDeliverySharedCases/RC03` | missing | false |
| `TestR5ProtocolDeliverySharedCases/RC04` | missing | false |
| `TestR5ProtocolDeliverySharedCases/RC05` | missing | false |
| `R5 shared receipt RC03` | missing | false |
| `R5 shared receipt RC04` | missing | false |
| `R5 shared receipt RC05` | missing | false |

## Per-case layer matrix

Every required layer remains unqualified because the whole batch was rejected. Unit observations are separate from catalog-required DB/HTTP/components layers.

| Case | Scope layers | Observed pass/fail/missing | Required layers still missing |
| --- | --- | --- | --- |
| CT01 | outside scope | 0/0/0 | db, http |
| CT02 | outside scope | 0/0/0 | db, http |
| CT03 | outside scope | 0/0/0 | db, http |
| CT04 | outside scope | 0/0/0 | db, http |
| CT05 | outside scope | 0/0/0 | db, http |
| CT06 | outside scope | 0/0/0 | db, http |
| CT07 | outside scope | 0/0/0 | db, http |
| CT08 | outside scope | 0/0/0 | db, http |
| CT09 | outside scope | 0/0/0 | db, http |
| CT10 | outside scope | 0/0/0 | db, http |
| RC01 | components | 1/0/0 | db, http, components |
| RC02 | components | 3/0/0 | db, http, components |
| RC03 | components, unit | 1/0/2 | db, http, components |
| RC04 | components, unit | 1/0/2 | db, http, components |
| RC05 | components, unit | 1/0/2 | db, http, components |
| OP01 | outside scope | 0/0/0 | db, http |
| OP02 | outside scope | 0/0/0 | db, http |
| OP03 | unit | 0/0/11 | db, http |
| OP04 | outside scope | 0/0/0 | db, http |
| RT01 | outside scope | 0/0/0 | db, http |
| RT02 | outside scope | 0/0/0 | db, http |
| RT03 | outside scope | 0/0/0 | db, http |
| RT04 | outside scope | 0/0/0 | db, http |
| RT05 | outside scope | 0/0/0 | db, http |
| RT06 | outside scope | 0/0/0 | db, http |
| RT07 | outside scope | 0/0/0 | db |
| BC01 | outside scope | 0/0/0 | db, http |
| BC02 | outside scope | 0/0/0 | db, http |
| BC03 | outside scope | 0/0/0 | db, http |
| BC04 | outside scope | 0/0/0 | db, http |
| BC05 | outside scope | 0/0/0 | db, http |
| BC06 | outside scope | 0/0/0 | db, http |
| GC01 | outside scope | 0/0/0 | db |
| GC02 | outside scope | 0/0/0 | db |
| LF01 | components | 0/1/0 | db, http, components |
| LF02 | components | 1/0/0 | db, http, components |
| LF03 | components | 1/0/0 | db, http, components |
| LF04 | components | 1/0/0 | db, http, components |
| LF05 | components | 4/0/0 | db, http, components |
| LF06 | components | 1/0/0 | db, http, components |
| LF07 | components | 1/0/0 | db, http, components |
| PE01 | components | 0/1/0 | db, http, components |
| PE02 | components | 0/5/0 | db, http, components |
| PE03 | components | 0/1/0 | db, http, components |
| PE04 | components | 0/1/0 | db, http, components |
| PE05 | components | 1/0/0 | db, http, components |
