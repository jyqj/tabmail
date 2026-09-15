# Company-mail release blockers A–E

Baseline: `6434118298387cb85fa473a4e6d78ff35c14102e` (PR #10).
This round closes the five explicitly scoped release blockers. It is not a
claim that the deferred corporate-mail roadmap or a live deployment is complete.

## Authorization and persistence contracts

**A — member hierarchy.** Tenant administrators may update or delete ordinary
employees only, never peer administrators or super administrators. A super
administrator must select the target tenant. Handlers and transactional stores
both check hierarchy. The store reads current actor/target roles, serializes
changes on the tenant row before locking the target, and rejects removal of the
last active administrator. All administrative updates are field patches, not
stale whole-user overwrites. Explicit `permission_profile_id: null` clears the
profile; an omitted field preserves it. Member audit writes commit or roll back
with the mutation. Deletion of an employee owning mailboxes is rejected: disable
the employee and resolve mailbox ownership/lifecycle first.

**B — outbound content.** Administrative visibility does not authorize reading
bodies, BCC, custom headers, or raw protocol diagnostics. Content is visible only
to the recorded sender or a current owner/read grantee of the original sender
mailbox, with tenant and zone restrictions preserved. A verified-send-identity
job without a mailbox is readable in full only by its recorded user sender.
Ownerless integration keys have no implicit employee-content privilege.
`delivery_token` is never serialized for anyone. Redacted views copy the stored
job, set `content_redacted: true`, remove bodies/BCC/headers, rebuild `rcpt_to`
from visible To/CC, and replace error/SMTP-response text that could echo BCC.
Attempt diagnostics use the same rule. Status, subject, visible recipients,
domains, times and SMTP codes remain operational metadata. A read grant permits
shared history, not retrying mail: retry separately requires sending authority.
Super administrators have no automatic content bypass in these endpoints.

**C — private mailbox defaults.** Creation defaults to `token`. Same-tenant
interactive/user-owned principals default to themselves as owner. Administrators
may explicitly assign an active same-tenant employee (`role=user`); other
principals may assign only themselves. An ownerless integration key remains
ownerless when no owner is supplied. Account-owned mailboxes use account/grant
authorization and need no second mailbox password. An ownerless `token` mailbox
still requires its own password. Explicit legacy `public` remains supported;
an owner never loses personal-mailbox content protection by selecting it.
Current read grants and ownership now make shared/assigned mailboxes and
outbound history discoverable without escaping tenant or zone filters.
The web form defaults to private, accepts integer retention `0` (permanent), and
rejects negative/fractional retention. OpenAPI and TypeScript match the request.

**D — refresh families.** Atomic consume-and-insert rotation, active-user checks,
and family replay revocation happen within a PostgreSQL transaction. A stable
family advisory lock also serializes descendant rotations, logout and cleanup.
Only one concurrent use of an old token can succeed; subsequent replay revokes
that family, not unrelated sessions. Logout revokes the supplied cookie's
family, including a concurrently rotated child. Explicit all-session logout
retains user-wide revocation. Errors never report successful logout or issue
new credentials. Expired ancestors remain as replay evidence until the entire
family is expired. Already issued short-lived access JWTs are not a family-wide
access-token revocation mechanism; these semantics concern refresh tokens.

## Migrations and deployment

Apply migrations using the normal application startup path. Do not edit released
migrations 00001/00002, run archived binaries against the new schema, or roll
back only some roles:

- `00003_refresh_token_family.sql`: existing refresh sessions each become their
  own family (`family_id=id`); add the family index.
- `00004_owned_mailbox_private.sql`: allow account-owned private mailboxes without
  a separate password, keep credentials mandatory for ownerless private boxes,
  and make the database default private. The migration-version guard now derives
  supported versions from the binary's embedded migrations, including restarts.

Coordinate the API, SMTP, worker and retention rollout. Snapshot the database
before upgrading. Downgrade migrations deliberately reject unsafe live rollback;
use a coordinated restore if rollback is required.

Production Compose passes the web API origin at **build time** and runtime:
`http://tabmail-api:8080`. Rebuild the web image. Registration defaults off and
outbound relay defaults enabled; a real relay host must be configured before
starting. Provide real secrets, allowed origins/proxies, relay credentials/TLS,
and the intended sender domain/DKIM setup. `.env.example` is not a production
secret file. Existing stored system settings take precedence over first-start
seeds: explicitly disable stored `open_registration` on existing installations.
The authenticated Redis healthcheck now receives its password through
`REDISCLI_AUTH` instead of relying on an unset shell variable.

## Verification map

Six baseline-compatible API regressions run unchanged against the baseline and
must fail there: member peer protection, private creation, outbound redaction,
never serializing delivery tokens, refresh fail-closed and logout fail-closed.
They must all pass on the repaired tree. Additional tests cover cookie rotation,
family isolation, read-versus-retry, explicit/implicit owners, grant revocation,
profile-null patch semantics and owner-deletion safety.

Real PostgreSQL tests (not only fakes) exercise concurrent final-two-admin
removal by disable/demotion/deletion, audit rollback, token insert/revoke failure
rollback, replay/logout versus descendant rotation, cleanup replay evidence,
shared list filtering, the private-mailbox constraint, migrations and restart.

The read-only GitHub workflow runs build, full `go test -race`, vet, shared DTO
contracts, frontend types/Vitest/lint/build, Compose expansion, and an actual
production web image build. It checks the image's `.next/routes-manifest.json`
for `tabmail-api:8080` and rejects the obsolete `tabmail:8080` destination.
No workflow deploys the application or sends live email.

## Explicitly deferred

Cross-process event distribution, inbound recovery-center UI, per-recipient
outbound ledger, business idempotency, employee invitation/provisioning workflow,
advanced administrator-template governance and an outbound break-glass audit UI
remain subsequent roadmap work. Existing administrator template/send-as
capability is retained; this round does not pretend to implement that P1 work.
A production login and real relay send/receive smoke test require the deployment
operator's environment and remain deployment acceptance checks.
