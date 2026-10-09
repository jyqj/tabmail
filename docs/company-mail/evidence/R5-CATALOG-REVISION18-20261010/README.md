# R5 catalog revision 18: NEXT-1010 source reconciliation proposal

Public product source: 177ee571b194836678cc9a473f178c655326ba7c; complete tree: 2d5d2c44680f6a6cb2b2d1166e2923806beafcc8 (PR #256).
Previous public catalog: ffb22b64698e4ff168a8d622018d3e169b3cdf07; previous product: 17be458fb9d36e135a95962638b68ce105c16518.

This is an author proposal. Qualification of the later committed maintenance candidate requires the three unchanged CLIs, the five original modules and the complete original source-version runner. Raw command output and exact source identities remain in the external execution packet. This packet makes no claim that the pending 50-case PostgreSQL workflow has passed.

Maintenance completes 0 implementation TODOs. Parent acceptance remains 10/171, with 161 remaining. runtime_verified, product_green and task_complete remain false.

## Original producer facts

- PostgreSQL files/functions: 65/407.
- SQL execution calls: 509.
- Direct write/write closure functions: 140/161.
- Migrations: 19; routes: 133; client branches: 137; finite closure files: 97, including 4 changed paths (the complete closure remains explicitly pinned).

## Reviewed source changes

RotateRefreshToken reads the complete authentication snapshot under its existing user SHARE lock and returns it only after successful Commit. Its userSelect concatenation changes lexical dynamic_sql_expression from false to true; family/user/token lock ordering is retained. ListCompanyAudit adds exactly five governed permission actions to the tenant-bounded count/page filter. Its current-administrator path locks the current user through companyReadTx and does not take the non-admin profile path.

ActivateEmployee adds a current sponsor FOR SHARE NOWAIT fence after required audit/outbox work, before the existing final domain fence and deadline check. Its two INSERTs classify only their own address constraints and preserve original causes. The new activation_errors.go helper is read-or-pure with no SQL, locks, callbacks or automatic retry. Prior manual reviews and optional source_review presence remain exactly archived.

The source change manifest explicitly includes third_party/go-smtp/conn.go and distinguishes paths outside the finite compatibility closure. The final NEXT1010 runner and workflow are protected as ordinary product-source files; original collectors, runners and revision17 utility retain their bytes.

## Historical preservation

All 37 old methods remain: 34 whole methods retain bytes and AST, two revision17 positive bodies run unchanged in the exact public historical clone, and one current positive has five checked pin/list substitutions. All five unapproved negative controls remain unchanged. Of 96 prior assignments only CURRENT_EVIDENCE, SOURCE_COMMIT and SOURCE_TREE advance; 93 values retain bytes and AST. Two new current positives bring the module to 39. New file/helper semantics and the one reviewed classification delta are tested explicitly without changing historical lock-reader/file-set assertions.

Original syntax producers only; SQL fragments, name-match callers and local operation order do not prove resolved dispatch, executed SQL or concurrency correctness. Separately bound EXEC product regressions retain their own source identities and limited qualification. This maintenance adds zero implementation TODOs and grants no runtime, release or parent acceptance.
