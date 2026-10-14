# Independent slice 4 batch review — scoped acceptance

Decision: **ACCEPT** the batch implementation at `7fea1edeee6bcad3e77edab767997b8757873db1`, delivered at exact head `5ebf159302520507829b93c5502b94b0f768f94e`, against frozen base `03f2f19b5e01f0f033405e7a84cde03794d491ca`. This accepts the batch source and guarded synthetic controls only. No physical runtime, formal or lifecycle qualification is claimed. Central **10/171** and historical **F52 failures** remain unchanged.

Review branch: `review/r5-slice4-batch-independent-20261004`; isolated worktree: `/workspace/tabmail-review`. Read repository `docs/VERSIONING.md`, `docs/company-mail/R5-TODO.md`, `web/AGENTS.md`, the [author report](R5-BATCH-V2-SLICE4-20261004.md), preparation independent evidence, runtime author/independent reports, the [combined interface inventory](R5-CONSUMER-SLICES23-INTEGRATION-20261004.md), and migration design at `42f49538f7bceb6eb8e698d716c1418f85343413:docs/company-mail/R5-V3-CONSUMER-MIGRATION-DESIGN-20261004.md`. No root/scripts/workspace AGENTS.md applies; workspace `.agents` and `.codex` are empty. No web edits invoke the Next.js guide. The user's explicit frozen-base instruction selects this isolated branch over the ordinary main convention.

## Findings

No blocking defect found within the trusted-controller, quiescent-path boundary.

- Omitted selection remains batch schema/policy 1 with runtime schema/policy 2. Explicit integer selection 3 requires batch schema/policy 2 and runtime schema/policy 3. Bool, string, float, missing explicit API selector, unsupported and mixed selectors reject at capture/load/validation/constructor boundaries. CLI omission stays 2 even with inherited environment selection 3. Old batch schema bytes are unchanged. The schema documents structure; Python semantic admission supplies the stricter exact-integer and transitive-pin checks.
- V3 capture authenticates serialized manifest bytes through the frozen `runtime.load_pinned` before catalog/Go observation. The manifest returned by that loader must equal the supplied object. Outer contract byte authentication precedes strict parsing and admission. Its authenticated runtime reference delegates bundle and complete envelope verification to the accepted adapter. Canonical `runtime_sha256` is equality evidence; recapture does not create authority.
- Independent challenges reseal seven envelope fields at each of all twelve positions in both receipt slots, recompute receipt byte and observation pins, replace the bundle, and update the candidate manifest's bundle pin and canonical runtime self-hash. All **168** reject against the unchanged expected manifest byte pin, with no mocked producer, process observation or catalog dispatch. Correctly byte-pinned but incompatible source/run/version/bundle references also reject. Duplicate/nonfinite/malformed JSON, equal JSON with changed serialized bytes, schema/policy cross-products and promotion/field/budget mutations reject.
- Validation invokes full frozen runtime validation before catalog recapture. Constructor environments replace inherited selector and v3 manifest/path pins with authenticated values. The killable worker receives explicit selection and its existing contract pin; its finish uses the same abort event and deadline. Timeout, failed exit, tail, cancellation and exhausted remaining deadline reject. Capture/load/validate/run CLI plumbing passes explicit selection consistently.
- Receipt construction selects schema/policy 1 for v1 and 2 for v2, adding integer selector 3 only for v2. Independent execution of only the extracted receipt-construction/publication AST checks write/chmod/replace failures: no successful publication flag is set. The unchanged `Batch.run` barrier, with `_run` fully mocked, invalidates all staged qualified children after a synthetic finalization failure and preserves selector/policy. Unpublished failures propagate; single-use rejection remains intact. No `_run`, lease, server, drain/reap or physical lifecycle body was executed.

## Fresh evidence and preservation

[Independent source](evidence/R5-BATCH-V2-SLICE4-INDEPENDENT-20261004/independent_checks.py), [stdout](evidence/R5-BATCH-V2-SLICE4-INDEPENDENT-20261004/independent.stdout), and [full log](evidence/R5-BATCH-V2-SLICE4-INDEPENDENT-20261004/independent.stderr): **11 test methods passed**, zero failures/errors, **168 resealed envelope/bundle mutations**, **zero OS-process audit events**. Guard installation precedes consumer imports. AST fixture loading removes all author test methods; assertions are independently written. Fixture setup uses inherited synthetic runtime plumbing and mocked controller returns, so it is not independent physical provenance. Receipt AST extraction and the mocked return barrier are synthetic failure checks, not lifecycle qualification. An initial CLI mock omitted the required result status; that harness error was corrected before the final passing run.

[Author replay stdout](evidence/R5-BATCH-V2-SLICE4-INDEPENDENT-20261004/author.stdout) and [log](evidence/R5-BATCH-V2-SLICE4-INDEPENDENT-20261004/author.stderr): **14 test methods passed**, zero failures/errors, **zero OS-process audit events**. Author envelope subtests are separate from the independent 168 count.

[Fresh static checker](evidence/R5-BATCH-V2-SLICE4-INDEPENDENT-20261004/static_review.py) reproduces the author's AST preservation assertions in this new evidence directory, then independently verifies the exact three-path implementation delta and byte equality of every delivery path. [Static results](evidence/R5-BATCH-V2-SLICE4-INDEPENDENT-20261004/static-results.json) and [full mode/blob manifest](evidence/R5-BATCH-V2-SLICE4-INDEPENDENT-20261004/preserved-tree.json) prove **2,259 pre-existing paths unchanged**; only the batch helper changes among existing paths. Seventeen definitions remain source-byte identical, including lease, owned process/drain/reap, RPC, ack, cleanup, primary-error handling, cancellation and finalization. Inventory worker AST changes only selector argv; `_run` AST changes only receipt version/policy/selector. V1 contract constructor remains AST-equivalent.

Worker4, Go120/process180/case75, RPC65536, catalogs/argv/context, remaining deadline and staged-result semantics are preserved. Frozen runtime, adapters/helpers, old schemas, Go/bridge, check_r5_protocol, registry/closure/inventory, discovery, locks and TODO bytes remain unchanged. All candidate delivery evidence remains unchanged. Read-only Git metadata processes used by static inspection and orchestration interpreter starts are outside the guarded zero-process claims.

Commands:

```sh
python3 -B docs/company-mail/evidence/R5-BATCH-V2-SLICE4-INDEPENDENT-20261004/independent_checks.py
python3 -B scripts/tests/r5_external_batch_v2_checks.py
python3 -B docs/company-mail/evidence/R5-BATCH-V2-SLICE4-INDEPENDENT-20261004/static_review.py
git diff --check
```

[Review identities](evidence/R5-BATCH-V2-SLICE4-INDEPENDENT-20261004/review.json) pin the exact reviewed source, design/report bytes, new checker sources and complete logs. No actual producer, process/runtime, Go, Node, PostgreSQL, mail, service, formal or lifecycle execution occurred. No candidate, bridge, frozen runtime or registry edits; no PR, merge, deploy or force push.

## Remaining gates

No blocker remains for this bounded batch-source acceptance. Separate bridge/check_r5_protocol selector review, closure integration and explicitly authorized physical qualification remain pending. Synthetic mocks cannot establish physical capture provenance, atomic executable attestation, hostile concurrent mutation protection or physical lease/kill/drain/reap/ack behavior. Direct API objects and caller-supplied expected pins remain trusted controller inputs; changing those expected pins changes the authority boundary.
