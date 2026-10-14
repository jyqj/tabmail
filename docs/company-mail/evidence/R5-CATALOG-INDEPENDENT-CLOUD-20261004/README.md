# Independent cloud catalog review — 2026-10-04

**Accept the static reconciliation at exact draft PR28 head `4540fb91ba742443dcf0a872b80261e58af7b3ab` against `3f34c31ed51a721b318a76512805e4a14dc292c3`.** No blocking finding or pin laundering was found. This verdict grants no runtime, dependency, source-binding, product or task qualification. Central progress remains **10/171**.

The review fetched both exact Git objects into the provisioned cloud checkout and inspected the complete ten-path delta, repository instructions (`web/AGENTS.md` governs untouched web work; no applicable root/scripts instruction or repository skill), `docs/VERSIONING.md`, transaction PR23 review, compatibility contract, original validators/collectors, reconciliation report, historical snapshots, source-change chain, verification evidence and LF successor-policy evidence. The reviewed source is the exact requested head, not the later evidence commit containing this report.

## Catalog and test conclusions

Both explicit inventory revisions are 2 and identify the exact base source. Each revision-1 snapshot matches the complete original catalog bytes read directly from that base Git object, as well as its declared SHA256. Current schema, historical runtime/wire evidence, classifications, migration definitions, reviewed file types and qualification limitations remain intact.

The transaction delta has 19 affected entries, 11 syntax changes, 16 caller-list changes and one PostgreSQL file hash change. Only `offboardingSubjectsTx` changes its body; the other syntax changes are locations. The new policy matches the already reviewed `23228449254bc0312171640a637e359187169192`: reject caller-self successor; scan current successor activity and role under the existing tenant-scoped SHARE fence; require existing `authz.CanManageTenantMember`; clarify the caller/execute-plan lock comments. No new policy hierarchy, lock or transaction is introduced by this catalog change. The independent script checks every affected entry ID/field against the declared reconciliation, and rejects any unrelated top-level catalog change.

Compatibility changes only the two declared test-source hashes and additive revision metadata. All eight source-chain rows were checked against exact parent/current Git bytes, review-document hashes at the pinned base, ancestry and continuity. Every blob in the complete intervening path history is accounted for; shared-test integration merge `1150633a4948c873349743658fad2661b08f1240` restates the accepted `6138113c1896cb810db567605154b5023f0340de` blob. The component chain includes external candidate v1/v2, closed RC receipt assertions, owned fixture join/batch executor, versioned PE seed and socket/SSE transport. It is not reduced to the latest seed change. The shared-test delta retains RC02 original submission identity/one-job checks while forbidding expired subject/body/header/BCC, and uses the explicit historical unknown BC03 asset instead of incorrectly treating a current COMPLETE snapshot as unknown. These stored test hashes describe declared source bytes, not a complete runtime compiler closure or renewed receipt.

The narrow PR23 test change retains all 78 unique historical reviews and their original body hashes against the byte-preserved revision-1 inventory. It separately checks current LF review/hash/base/trace, and continues to require unchanged reviews/body hashes for other retained functions plus unchanged classification/evidence levels. No test was deleted or skipped; old snapshots still reject actual current facts.

## Fresh execution

Existing Go1.25.7 Linux/amd64, Node24.19.0 and TypeScript5.9.3; independent cache `/tmp/R5-CATALOG-INDEPENDENT-CLOUD-20261004/cache`, existing module cache `/workspace/tabmail-cloud/gomod`, GOPROXY off. No dependency update or installation.

```sh
export R5_TEST_GO=/workspace/tabmail-cloud/tools/go/bin/go
export R5_TEST_CACHE=/tmp/R5-CATALOG-INDEPENDENT-CLOUD-20261004/cache
export R5_TEST_MODULECACHE=/workspace/tabmail-cloud/gomod GOPROXY=off
export PYTHONDONTWRITEBYTECODE=1 PYTHONPATH=scripts:scripts/tests
python3 -B -m unittest -v test_r5_transactions test_r5_compatibility test_r5_catalog_reconciliation
python3 -B scripts/check_r5_transactions.py
python3 -B scripts/check_r5_compatibility.py
python3 -B docs/company-mail/evidence/R5-CATALOG-INDEPENDENT-CLOUD-20261004/review.py
```

**51/51 focused author tests passed**, zero skips/errors/failures, 43.332 seconds. This includes freshly run actual route-source baseline/mutation, removal of successor-self rejection, file-only comment, closure bytes, fabricated route/SQL facts and inherited historical anti-laundering controls. Both original CLIs passed: transaction 62 files / 395 functions / 498 SQL calls / 134 direct writes / 154 candidate write closures / 19 migrations; compatibility 132 routes / 134 client branches / 94 declared closure files. Runtime/task/product flags remain false and current wire remains required.

The independently authored [review.py](review.py) additionally extracts fresh actual AST/routes/clients and runs five controls in disposable copies: successor role-policy bypass, file-only bytes, extra AST SQL operation, changed route fact and changed actual component-closure bytes. All reject using original validators. Original copied source first reproduces the fresh AST exactly. [review.json](review.json) records safe verdict, exact objects, paths, snapshot hashes, full history and control results; [verification.json](verification.json) records fresh log hashes and command results. Private logs remain under the owned mode700 temporary prefix.

No production/catalog/shared TODO/source-binding v3 edits were made. No formal default, sharedDB, HTTP, component or wholePG runtime; no PostgreSQL/service start, source capture/preparation, actual mail, GitHub CLI/API action, CI-read retry, merge or deployment. Only evidence and the standalone source-review script are delivered. Ordinary commit/push is authorized; parent owns creation of the new PR through the GitHub connector.

## Proposed central TODO text for parent integration

- Independent cloud review accepts exact PR28 head `4540fb91ba742443dcf0a872b80261e58af7b3ab` vs `3f34c31ed51a721b318a76512805e4a14dc292c3` for static revision-2 catalog reconciliation only. Revision-1 bytes, LF successor policy, complete two-test-source hash history and historical PR23 reviews verified. Fresh 51/51 checks, both original source CLIs and five additional independent mutation negatives pass. No new blocker or pin laundering found.
- Central **10/171 unchanged**. Original default/setup/component/shared-wire/wholePG/audit failures and missing external/source/runtime qualification remain open. No historical receipt transfers to this source; later formal-run authorization and parent integration remain separate.
