# Asynchronous editor-opening ownership (bounded)

## Source and finding

- Baseline: published draft PR #48 `d1c7102507fe1cf050298b108096b3508662cd8f`; its verified local equivalent is `963d409b9e6eecbe9b01c72b4182b90531eda564`, tree `d337c998ce63b5273cd8a1e516fa679a7a39ea18`.
- Reviewed production candidate: `8dcb77b1b831dcba3a60f01afcb92196ed4f1459`, tree `77122ca6974ce892863b642875fee1997ead4179`.
- `DraftFolder` awaited its Continue editing GET before calling the workspace's unconditional editor setter. `MessagePane` did the same for reply, reply-all and forward preparation.
- Their local action guard did not disable the workspace's header Compose button. A user could start preparation, open a new editor, and type while the old request remained pending. Its eventual response installed a different editor key, unmounting the newer editor and discarding input inside the two-second autosave delay.
- A committed folder/mailbox/message/search/page navigation, including away and back, also did not retire the pending callback. It could open old content over the newer view. This concerns editor takeover, not a fabricated URL rewrite or a cross-session authorization claim.
- This is a concrete instance of R5-P9-050's existing requirement to preserve unsaved content, separate from the prior send-completion, recipient-reload and DraftWriter fixes.

## Bounded ownership contract

1. An asynchronous opening captures the current committed view token and installs a new opening-intent token synchronously in the initiating action, before awaiting its request.
2. The result may install an editor only while both tokens still belong to it. Committed semantic navigation, session changes or workspace unmount retire its view token. A new opening intent permanently supersedes older intents, even if the new request fails.
3. Header Compose supersedes outstanding preparation immediately, before the new editor commits. Closing that newer editor does not revive an old request.
4. Unrelated query fields, query ordering and ordinary revalidation do not revoke an otherwise-current opening. Current failures remain visible; superseded failures do not report into the newer view.
5. No transport cancellation or server rollback is claimed. In particular, already-dispatched forward preparation may have created attachment reservations. This change performs no compensating DELETE and adds no submit.

Only `web/features/mail/workspace.tsx` and the callback chain in `components/draft-folder.tsx`, `components/message-pane.tsx` and `components/received-folder.tsx` change in production. Children pass the pending promise immediately; the workspace owns the completion decision. New authority refs are assigned only from layout-effect commit/cleanup or explicit opening actions. PR48's existing send-completion ownership, request-layer session fences, authorization checks and cache refresh paths remain unchanged.

## Reproduction and verification

The real-consumer harness retains production Workspace, Compose, DraftWriter, SWR, session and request transport. Only Next location, fetch responses and toast delivery are substituted. All mailboxes, messages, drafts and sessions are synthetic.

Minimal red: defer the existing draft GET; click Continue editing; click header Compose; change Subject; resolve the GET before the two-second autosave; assert that the same input element and unsaved value survive, with no draft write. Baseline instead replaces the input with the old draft. The same proof covers Reply, Reply all and Forward.

- Original frozen harness: **16 failed / 12 passed**, then **28/28 passed** against the production candidate.
- The first harness typecheck rejected four unsupported Testing Library `exact` options; lint found an unused import. Original source/logs remain preserved. Only those options and the unused import were removed, with assertions unchanged. The final normalized harness was rerun against an isolated baseline: **16 failed / 12 passed** again.
- Final normalized harness SHA256: `27b5275487018be7e33cdb66485e76aa227f0b0781bb9137342490c37fd7fcea`.
- Related regression selection: **14 files / 219 passed**, including the new 28 tests, PR48 navigation, received/sent actions, page identity consumers, existing Compose lifecycle, recipient reload, DraftWriter, session and compose-identity checks.
- Frontend-only selection: **54 files / 760 passed**. The existing `components/company/r5-protocol.test.tsx` and `r5-external-batch-probe.test.tsx` are explicitly excluded; the existing shared-protocol fixture suite remains excluded by its unchanged config. This is not full-default/Go/PostgreSQL qualification.
- Final `tsc --noEmit --incremental false`: **passed**. Full lint: **exit 0**, with the same four preexisting warnings; no new product warning.
- Independent review found **no blocking production issue**. Its separately frozen original 43 and pre-candidate supplemental seven cases produced **34 failed / 16 passed** on baseline, then **50/50 passed** on the unchanged candidate. Normalized typecheck copies removed only the same unsupported role-query option; both snapshots were rerun with identical results.
- Unchanged historical independent send/navigation controls were **82/82** on baseline and **81/82** on candidate. The one incompatible old oracle intentionally expected a pending draft GET to exploit this exact bug and replace a newer editor. That original oracle/failure is retained; it is not reported as passing. Four separately labeled post-inspection controls pass, covering legitimate remount replacement, preservation of a current dispatched submit, and speculative-versus-committed navigation.
- Independent qualified frontend sweep: **60 files / 852 passed / 1 explicitly skipped historical bug-dependent oracle**. This is distinct from the author's unskipped 760-test frontend-only run. Independent focused production lint and tracked-source plus normalized 50-case harness typecheck pass; historical imported review harnesses are excluded from that typecheck, not their runtime checks.
- The reviewer's initial broad baseline sweep retained **799 passed plus two environment-dependent suite failures**: missing explicit PG fixture stopped before a request; the environment's Go command did not support `test`. These suites were excluded explicitly from the qualified frontend selection. No backend/PG success is claimed.
- Counts overlap and must not be added.

## Limits

No build, actual browser, backend, PostgreSQL, real mailbox, credentials, live mail or deployment was exercised. No dependencies, lockfile, workflow, backend contracts, route registrations or historical evidence changed. This report does not promote earlier frontend or formal receipts to new-source runtime qualification. Central completion remains **10/171**; R5-P9-050, its dependencies and all prior formal/whole-PG/audit gates remain open. Publication and exact-head CI verification are separate from the local checks; no merge or deployment is authorized.
