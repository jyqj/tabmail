# Independent classifier-contract review: REJECT

Reviewed exact candidate `0c9adb17559e347ed99b3661d789b80ba59762aa`
against frozen base `412f875985854527f5e7d74040f18954c3a352bf` and
previous candidate `116d44a2a9c0b5d7ac6a09187bf3fae22c1a1e50`.
Read `R5-SELECTED-CONSUMER-CLASSIFIER-CONTRACT-20261004.md`; approved design
remains `42f49538f7bceb6eb8e698d716c1418f85343413`.
The prior eleven gaps are fixed, and isolated classifier reuse is pure under the
reviewed dependencies. One new generated-exception cross-field gap blocks acceptance.
Interface remains unfrozen.

## Blocking finding

**P2: an in-source non-Go file can borrow the generated testmain exception.**
At `scripts/r5_selected_binding_consumer.py:288–300`, a package import path ending
in `.test` is sufficient to infer `Name='main'`. The non-Go exception checks only
GoFiles and an absolute path. It does not require that path to be outside the source
root, contained in the observed cache, have the generated `-d` suffix, or be linked
to a generated classification record before passing through the classifier.

Independent case `generated-exception-borrowed-local-non-Go` supplies:

* Local row `directory="internal", import_path="tabmail/internal.test"`, with
  `GoFiles=["/independent/controller/source/internal/borrowed.txt"]`.
* The file is included in source hashes and selected_local with GoFiles, and both
  root/explicit package coverage lists are updated consistently.
* `generated_testmain=[]`; the file is outside GOCACHE and has no `-d` suffix.
* All source/binding/observation seals and receipt/bundle byte pins are republished
  by the independent synthetic publisher so contract validation is reached.

Expected **reject**; actual **accept**, returning verified receipts. This contradicts
the candidate report's explicit claim: “Arbitrary local non-Go files or other
first-four fields cannot borrow that generated exception.” The original trusted
producer's classifier also uses Name to exempt generated-main GoFields from the
ordinary suffix check. For paths inside source, it enters local classification and
never reaches its cache containment/GoFiles/`-d` checks. Reusing that function on
reconstructed rows does not make it a complete supplied-receipt contract validator:
the adapter itself manufactures Name from a suffix that is weaker than the required
linked cache-domain exception. This is not a claim that the helper classifier's
source changed, or that synthetic rows reconstruct original stdout.

Require the exceptional non-.go GoFiles input to be the separately represented
cache-based generated testmain occurrence before allowing the exception or
synthesizing a generated Name. Preserve legitimate cache testmain success and
ordinary .go/local/native/embed paths; do not edit helper projection, exclusions,
source registry or runtime wiring. The pure gate must reject this case even with
newly supplied synthetic pins. Fixed-original-pin counterfeits still reject first;
this finding does not bypass the trusted pin or establish malicious controller access.

## Independent controls and isolation

[New independent program](evidence/R5-CLASSIFIER-INDEPENDENT-20261004/adversarial_checks.py)
imports no author fixture/check module. It hash-verifies and executes the preserved
independent completed fixture from `d0811178`, then authors new mutations and valid
controls. [Results](evidence/R5-CLASSIFIER-INDEPENDENT-20261004/adversarial-results.json):
**43 controls: 42 expected results, one unexpected acceptance**; three additional
fixed-original-pin counterfeits reject. The program exits 1 on the unexpected
acceptance; [stderr](evidence/R5-CLASSIFIER-INDEPENDENT-20261004/adversarial-stderr.txt)
preserves the failing final expectation assertion. Zero OS-process events.

New controls cover native attribution/field/path/qualification, missing/duplicate/
reordered occurrences across two packages selecting the same file, valid generated
cache output and wrong package/path/field/cache/suffix/count, all four Go fields and
native routing in external/toolchain domains, valid nonlocal records plus wrong
extension/local attribution/count/path/qualification/module version/GOROOT. Valid
positives ensure those gates do not simply reject every classified record.

During each new verify call, open/read/stat/lstat/scandir/listdir/readlink, getenv,
capture/validate/Retention are forbidden. The original classifier global dictionary's
keys and every value identity remain unchanged before/after every operation; the
imported module set also remains unchanged. Outside the mock scope the global
identities are checked again. The private FunctionType uses the original code object
and a fresh copied globals dict with only an in-memory base_markers callable substituted.
Inspection confirms the classifier's remaining dependencies use lexical Path
operations. No global patch, import, filesystem or runtime action is performed by
verification. Test-only mocks are scoped and restored. Helper import-time source/schema
reads occur before read mocks, as previously documented.

## Preserved review evidence and results

The two prior independent programs are extracted byte-for-byte from
`d0811178bc68760280c44a68cfd7f7bc0790c9b9` into a separate temporary layout;
a scripts symlink selects this exact checkout. Source hashes are checked by the
replay wrapper. No fixture/case/expectation changes:

* Corrected candidate: **114 prior + 18 delta = 132/132 expected results**.
* Previous candidate: 114 pass; delta retains exactly its five unexpected acceptances.
* Original candidate: original 114 controls still reproduce exactly six schema gaps.
* Author checks rerun unchanged: **41 pass**, zero OS-process events.

Compared replay case names, order and expectations against immutable prior evidence:
all 132 match. Original fixed-pin counterfeits and projection/stderr/selector
assertions remain active. Prior reports and evidence are untouched at
[054d479](https://github.com/jyqj/tabmail/blob/054d47924e041117afb88f4d8e74217a89bd2464/docs/company-mail/R5-ADAPTER-INDEPENDENT-REVIEW-20261004.md)
and
[d081117](https://github.com/jyqj/tabmail/blob/d0811178bc68760280c44a68cfd7f7bc0790c9b9/docs/company-mail/R5-ADAPTER-DELTA-INDEPENDENT-REVIEW-20261004.md).

Commands from this review worktree:

```text
PYTHONDONTWRITEBYTECODE=1 python3 docs/company-mail/evidence/R5-CLASSIFIER-INDEPENDENT-20261004/adversarial_checks.py
PYTHONDONTWRITEBYTECODE=1 python3 scripts/tests/r5_selected_binding_consumer_checks.py
PYTHONDONTWRITEBYTECODE=1 python3 scripts/tests/r5_selected_binding_consumer_checks.py --replay-delta /tmp/r5-third-run/docs/company-mail/evidence/R5-ADAPTER-DELTA-INDEPENDENT-20261004 --adapter /tmp/r5-second-rejected-consumer.py --expected-gaps 5
PYTHONDONTWRITEBYTECODE=1 python3 scripts/tests/r5_selected_binding_consumer_checks.py --replay-delta /tmp/r5-third-run/docs/company-mail/evidence/R5-ADAPTER-DELTA-INDEPENDENT-20261004 --expected-gaps 0
PYTHONDONTWRITEBYTECODE=1 python3 scripts/tests/r5_selected_binding_consumer_checks.py --replay-review /tmp/r5-independent-controls.py --adapter /tmp/r5-rejected-consumer.py --expected-gaps 6
```

Temporary baseline adapter files are exact git-show extractions of the two rejected
heads; original reviewer source is from 054d479. Git/evidence bookkeeping and fixture
materialization occur outside program audit guards. No guarded program launches
another OS process. Synthetic fixtures/pins are independent contract probes, not
real captures, live closure evidence or fresh runtime qualification.

## Exact identities and scope

[Identity manifest](evidence/R5-CLASSIFIER-INDEPENDENT-20261004/identities.json) pins
all code/test/results bytes. Candidate adapter SHA256:
`6d87b0b172a25871fdc1c766cf4e135c1ebf075d7886514faf2259f9cde4c6e3`.
Candidate author checks SHA256:
`71db298f70aded89e13eae597ce5dc9315f49bebc8b6978fa8228f9db515e6a6`.
All **2,208** frozen-base tracked paths retain exact Git blob identities, including
existing v1/v2/v3 helper/schema bytes and discovery. Trust inputs remain explicit
caller/controller arguments, with no candidate/environment derivation. Pin-first,
exact twelve descriptors, version/slot/phase/schema compatibility and default-v2/
explicit integer-3/lazy selection checks remain preserved. Projection exclusively
uses the existing helper; retained stderr stays bound with no broader exclusion.

Applicable instructions rechecked: repository has only web/AGENTS.md, read; no web
edits. No applicable root/scripts/test skill files; workspace .agents/.codex empty.
This review adds only its separate report/evidence. No product edits, producer or
source capture, runtime/preparation/Go bridge/registry/closure/discovery changes,
formal suite, PG/mail/service, PR/merge/deploy/force. Central **10/171** and old F52
remain unchanged. Request correction and fresh exact-head independent review before
interface freeze.
