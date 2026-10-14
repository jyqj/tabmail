# R5 catalog revision 16: EXEC source reconciliation proposal

Product source: b6516f9ac873f4464894933b5c3a0e046bc9a4a5; complete tree: 4034b36faa89a421f2a21b525cf35e93da4a1670.

Previous public catalog: eae219951c6f9cbf17eb4062b69464c731807be7; previous product: 26a30e3c6963d553efdd8a8b33c095feb924c758.

This generated packet is an author proposal. The later committed maintenance candidate must pass the three unchanged CLIs, all five original modules with two new current positive tests, and the complete original source-version runner. No unobserved CI outcome is asserted here. Original source drift failures and all raw stdout/stderr remain in the proposal artifact and are separately source-bound.

Maintenance completes 0 implementation TODOs. Parent acceptance stays 10/171, remaining 161. runtime_verified, product_green and task_complete stay false.

## Original producer facts

- PostgreSQL files/functions: 64/405.
- SQL execution calls: 506.
- Direct write/write closure functions: 140/161.
- Migrations: 19; routes: 133; client branches: 136; finite closure files: 97.

## Manual source boundary

SaveMailTemplate adds the NUL name check before template validation, JSON or SQL; its accepted transaction and revision CAS paths remain. Four prior manual fields are archived exactly. The old entry had no source_review, and its archive does not invent one.

SeedSetting uses one INSERT ON CONFLICT(key) DO NOTHING statement, preserves all existing row fields on conflict, returns the inserted flag from RowsAffected, and propagates errors. It supplies no multi-key transaction or cache-consistency guarantee. Its exact new source review and derived syntax are independently pinned by the new current tests.

## Historical and rejection preservation

All 33 old test methods remain: 30 whole methods retain their bytes/AST, two revision-15 positive bodies run unchanged in the exact public historical clone, and one current positive has five explicitly checked current-pin/list changes. All five unapproved negative controls remain unchanged. Of 84 old assignments, only CURRENT_EVIDENCE, SOURCE_COMMIT and SOURCE_TREE advance; the other 81 values retain bytes and AST. Two new current methods bring this module to 35 and the five modules to the count actually reported by unittest. New maintenance source files have separate fixed Git blob, SHA256 and byte pins within the complete protected path set.

Original syntax producers only; SQL fragments, name-match callers and local operation order do not prove resolved dispatch, executed SQL or concurrency correctness. Separately bound EXEC product regressions retain their own source identities and limited qualification. This maintenance adds zero implementation TODOs and grants no runtime, release or parent acceptance.
