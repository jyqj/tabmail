from patchutil import *

# One consistent lock order: user -> family -> token, including global logout.
p='internal/store/postgres/refresh_tokens.go'
replace(p,'var family uuid.UUID\n err:=tx.QueryRow(ctx,`SELECT family_id FROM refresh_tokens WHERE token_hash=$1`,hash).Scan(&family)',r'''var family, user uuid.UUID
 err:=tx.QueryRow(ctx,`SELECT family_id,user_id FROM refresh_tokens WHERE token_hash=$1`,hash).Scan(&family,&user)''')
replace(p,'if _,err=tx.Exec(ctx,`SELECT pg_advisory_xact_lock(hashtextextended($1,0))`,"refresh-family:"+family.String());',r'''var locked uuid.UUID
 if err=tx.QueryRow(ctx,`SELECT id FROM users WHERE id=$1 FOR SHARE`,user).Scan(&locked);errors.Is(err,pgx.ErrNoRows){return nil,nil}else if err!=nil{return nil,err}
 if _,err=tx.Exec(ctx,`SELECT pg_advisory_xact_lock(hashtextextended($1,0))`,"refresh-family:"+family.String());''')
s=read(p).replace('// Every rotation, replay and logout locks the family BEFORE the token row.','// Every rotation, replay and logout locks the user, then family, then token.')
s=s.replace('// Rotation holds its token row before the user row. Avoid lock-order cycles\n // by invalidating with a single statement; PostgreSQL detects any deadlock\n // and the caller must fail closed rather than claiming logout succeeded.','// Rotations hold a shared user lock first, so this exclusive lock waits\n // for in-flight rotations and then revokes all their committed descendants.')
Path(p).write_text(s)
# Preserve existing ability to promote ordinary users to company admin; assigning
# platform power remains super-admin-only and targets are guarded independently.
p='internal/store/postgres/members.go'
replace(p,'if u.Role != old.Role && role != models.RoleSuperAdmin { return authz.ErrForbidden("only super admin can change member roles") }','if u.Role == models.RoleSuperAdmin && role != models.RoleSuperAdmin { return authz.ErrForbidden("only super admin can assign super_admin role") }')
p='internal/testutil/fake_store_members_refresh.go'
replace(p,'if u.Role!=old.Role && s.users[a.ID].Role!=models.RoleSuperAdmin{return authz.ErrForbidden("only super admin can change member roles")}','if u.Role==models.RoleSuperAdmin && s.users[a.ID].Role!=models.RoleSuperAdmin{return authz.ErrForbidden("only super admin can assign super_admin role")}')
replace(p,'delete(s.users,id)',r'''for keyID,key:=range s.apiKeys {if key.OwnerUserID!=nil && *key.OwnerUserID==id {delete(s.apiKeys,keyID);for raw,resolved:=range s.apiRaw{if resolved.KeyID==keyID{delete(s.apiRaw,raw)}}}}
 for pair,g:=range s.mailboxGrants {if g.UserID==id{delete(s.mailboxGrants,pair)}}
 delete(s.users,id)''')
# Never silently downgrade a live session family migration.
p='internal/store/postgres/migrations/00003_refresh_token_family.sql'
replace(p,'ALTER TABLE refresh_tokens DROP COLUMN family_id;',r'''-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'refresh family downgrade requires coordinated session invalidation and backup restore'; END $$;
-- +goose StatementEnd''')

write('scripts/verify_production_config.py',r'''
#!/usr/bin/env python3
"""Validate company Compose defaults and, with --build, actual standalone rewrites.
Uses only disposable configuration; never starts the mail backend or sends mail.
"""
import argparse
import json
import os
from pathlib import Path
import subprocess
import tempfile

parser=argparse.ArgumentParser()
parser.add_argument('--build',action='store_true')
args=parser.parse_args()
values={
 'TABMAIL_MAILBOX_TOKEN_SECRET':'ci-mailbox-not-a-production-secret',
 'TABMAIL_JWT_SECRET':'ci-jwt-different-not-a-production-secret',
 'TABMAIL_DB_DSN':'postgres://ci:ci@postgres:5432/ci?sslmode=disable',
 'TABMAIL_REDIS_ADDR':'redis:6379','TABMAIL_REDIS_PASSWORD':'ci-disposable-password',
 'TABMAIL_SMTP_DOMAIN':'mail.example.invalid',
 'TABMAIL_HTTP_ALLOWED_ORIGINS':'https://mail.example.invalid',
 'TABMAIL_HTTP_TRUSTED_PROXIES':'127.0.0.1/32',
 'TABMAIL_OUTBOUND_RELAY_HOST':'smtp.example.invalid',
 'POSTGRES_USER':'ci','POSTGRES_PASSWORD':'ci-disposable-password','POSTGRES_DB':'ci',
}
env={k:v for k,v in os.environ.items() if not k.startswith(('TABMAIL_','POSTGRES_'))}
with tempfile.TemporaryDirectory() as tmp:
 p=Path(tmp)/'compose.env';p.write_text(''.join(k+'='+v+'\n' for k,v in values.items()))
 command=['docker','compose','--env-file',str(p),'-f','docker-compose.prod.yml']
 config=json.loads(subprocess.check_output(command+['config','--format','json'],env=env,text=True))
 services=config['services']
 assert services['web']['build']['args']['INTERNAL_API_URL']=='http://tabmail-api:8080'
 for name in ['tabmail-api','tabmail-smtp','tabmail-worker','tabmail-retention']:
  settings=services[name]['environment']
  assert settings['TABMAIL_OPEN_REGISTRATION']=='false',(name,'registration')
  assert settings['TABMAIL_OUTBOUND_ENABLED']=='true',(name,'outbound')
  assert settings['TABMAIL_OUTBOUND_RELAY_HOST']=='smtp.example.invalid'
  assert settings['TABMAIL_OUTBOUND_RELAY_TLS']=='starttls'
 print('Company Compose defaults and build-time API address: PASS',flush=True)
 if args.build:
  subprocess.run(command+['build','web'],env=env,check=True)
  check="const fs=require('fs'); const routes=fs.readFileSync('.next/routes-manifest.json','utf8'); if(!routes.includes('http://tabmail-api:8080')||routes.includes('http://tabmail:8080')) throw new Error('wrong baked-in API destination'); console.log('Built standalone API rewrites: PASS');"
  subprocess.run(['docker','run','--rm','--entrypoint','node','tabmail-prod-web','-e',check],env=env,check=True)
''')
write('docs/company-mail/RELEASE-BLOCKERS-R2.md',r'''
# Release blockers round 2

Base: `6434118298387cb85fa473a4e6d78ff35c14102e` (PR #10).

## Scope and guarantees

A: member management checks the selected tenant and current target role. Company administrators manage ordinary users, not peer admins or super-admins. A tenant-row lock serializes guarded mutations, preventing two concurrent removals of the last two active administrators. Every admin edit uses the same transaction and an updated_at precondition: display-name-only edits cannot restore stale privileges. Owned mailboxes must be transferred before user deletion. Successful guarded mutations include a transactional audit record.

B: outbound lists, details, retry responses and attempt logs apply content policy. The immutable submitter or a current sender-mailbox owner/read-grantee can read content. Administrator titles alone only grant operational metadata visibility. Delivery fencing tokens are never serialized. Redaction also removes BCC from envelope recipients and clears SMTP text which could echo hidden addresses. Metadata preserves explicit To/Cc, subject, domain progress, time, SMTP code and a generic error indicator. Shared mailbox readers can list and inspect history, but read permission does not permit retrying another sender's task. API-key scopes and zone restrictions remain in force. Identity-only submissions without a mailbox have no mailbox-grant content path. No new break-glass endpoint is introduced.

C: unspecified mailbox access defaults to token and requires a password. Effective user ownership defaults within the selected tenant; a platform operator impersonating another tenant cannot become a cross-tenant owner. Explicit assignment requires an active ordinary company member and management authority. Public compatibility remains explicit, particularly for ownerless integration callers; an owned personal mailbox remains protected regardless of its legacy access_mode. Mailbox lists include owner/read-grant relationships inside, never outside, tenant and zone boundaries. Zero-hour retention remains permanent and is accepted by the UI.

D: migration 00003 adds independent families to existing refresh tokens. Rotation locks user -> family -> token, atomically revokes the old token and inserts a child. No JWT or replacement cookie is returned before commit. Replaying a consumed token revokes that family, not other sessions. Concurrent duplicate refresh is deliberately strict: one request can succeed, the duplicate returns 401 and invalidates the successful child's refresh family. Clients must single-flight refresh; there is no grace window. Logout revokes the presented family, including a concurrent child; logout without a presented token remains explicit all-session logout. Revocation failures are not reported as successful logout. Issued access JWTs are not retroactively revoked by family rotation; this change governs refresh capability. Replay tombstones persist while a family has a live descendant. Downgrade requires coordinated invalidation/restore.

E: production Compose passes the correct API destination at web build time, closes public registration by default, and exposes outbound relay configuration. Empty relay host fails startup in enabled relay mode. Replace example relay settings before use. Existing database-backed open_registration values are not overwritten by environment seeding; inspect and close an existing open setting during upgrade. The deployment verification script checks resolved Compose configuration and the actual routes manifest inside the built web image, without starting SMTP or sending mail.

## Deliberately deferred

Cross-process realtime delivery; ingress recovery console; per-recipient outbound ledger; send-submission idempotency; employee invitations; template governance; outbound break-glass UI/API; public Internet delivery and production login acceptance. None is claimed complete by this PR.

## Release procedure

Back up PostgreSQL and raw mail jointly. Apply Goose migrations in a controlled upgrade. Existing tokens are assigned independent families. Preserve the P0 rule against mixed old/new writers. Set and verify effective company settings and relay credentials; validate the web image and run a real deployment login smoke test before routing company traffic. CI validation is not a deployment or a mail-deliverability test.

Actual verification results and Actions run identifiers are recorded in `RELEASE-BLOCKERS-R2-VALIDATION.md`.
''')
