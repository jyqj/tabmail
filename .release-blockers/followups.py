#!/usr/bin/env python3
from finalize import ROOT,edit,changefunc

# v3 is now the latest migration; the P0 historical-conflict case still stops
# safely at v1. Preserve the existing safety assertions, including restarts.
edit('internal/store/postgres/company_integration_test.go','want := 2','want := 3')
edit('internal/store/postgres/migrate.go','version_id NOT IN (0,1,2)','version_id NOT IN (0,1,2,3)')

# Last-admin and refresh protocols require fresh snapshots after waiting on a
# lock. Pin READ COMMITTED rather than inheriting an operator's session default.
for path in ['internal/store/postgres/member_guard.go','internal/store/postgres/refresh_rotation.go']:
 p=ROOT/path;s=p.read_text()
 assert 's.pool.Begin(ctx)' in s
 p.write_text(s.replace('s.pool.Begin(ctx)','s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})'))
changefunc('internal/store/postgres/users.go','func(s *PgStore)RevokeUserRefreshTokens',lambda s:s.replace('s.pool.Begin(ctx)','s.pool.BeginTx(ctx,pgx.TxOptions{IsoLevel:pgx.ReadCommitted})'))

# A shared mailbox without an owner/password uses the existing private API-key
# mode; grants are evaluated before that legacy access-mode fallback.
changefunc('internal/store/postgres/release_blockers_integration_test.go','func TestReleasePostgresSharedMailboxAndOutboundScope',lambda s:s.replace('AccessMode:models.AccessToken','AccessMode:models.AccessAPIKey'))
changefunc('internal/api/release_blockers_integration_test.go','func TestReleaseSharedOutboundReadIsNotRetry',lambda s:s.replace('AccessMode:models.AccessToken','AccessMode:models.AccessAPIKey'))

# A nullable raw value distinguishes omitted profile from explicit JSON null.
# A pointer to RawMessage loses that distinction during decoding.
changefunc('internal/api/handlers/users_admin.go','func (h *UserAdminHandler) UpdateUserByAdmin',lambda s:s.replace('*json.RawMessage','json.RawMessage').replace('string(*req.PermissionProfileID)','string(req.PermissionProfileID)').replace('json.Unmarshal(*req.PermissionProfileID,','json.Unmarshal(req.PermissionProfileID,'))

# Keep all shared outbound fields checked, not merely the old 16 DTOs.
edit('web/lib/types.ts','export interface MailboxCreateInput {','export interface MailboxCreateInput {\n  owner_user_id?: string;')
edit('web/lib/types.ts','  content_redacted?: boolean;','  content_redacted: boolean;\n  to?: string[];\n  cc?: string[];\n  bcc?: string[];')
edit('scripts/check_contract_drift.py','SHARED_TYPES = [','SHARED_TYPES = [\n    "OutboundJob",')

# The mailbox creation schema is inline, not a named component. Restrict the
# change to this operation, preserving temporary-domain and route defaults.
p=ROOT/'internal/api/openapi.yaml';s=p.read_text();a=s.index('  /api/v1/mailboxes:\n');b=s.index('\n  /api/',a+1);block=s[a:b]
assert 'default: public' in block
block=block.replace('default: public','default: token')
block=block.replace('                password: { type: string }','                owner_user_id: { type: string, format: uuid, description: "Active employee in this company; only administrators may assign another employee. Omitted ownership defaults to the current same-company user." }\n                password: { type: string, description: "Required for ownerless token mailboxes; owned mailboxes may use the owner session instead." }')
block=block.replace('retention_hours_override: { type: integer, nullable: true, example: 24 }','retention_hours_override: { type: integer, minimum: 0, nullable: true, example: 0, description: "0 means permanent retention; omit to inherit. Owner mailboxes remain permanent." }')
s=s[:a]+block+s[b:]
a=s.index('    OutboundJob:\n');b=s.index('\n    OutboundAttempt:',a) if '\n    OutboundAttempt:' in s[a:] else s.index('\n    ',s.index('\n      ',a)+1)
# Only append nonexisting content properties to the known schema header.
marker='    OutboundJob:\n      type: object\n      properties:\n'
assert marker in s
s=s.replace(marker,marker+'''        to: { type: array, items: { type: string } }
        cc: { type: array, items: { type: string } }
        bcc: { type: array, items: { type: string }, description: "Omitted when content_redacted is true." }
        text_body: { type: string, description: "Omitted for metadata-only viewers." }
        html_body: { type: string, description: "Omitted for metadata-only viewers." }
        headers: { type: object, additionalProperties: { type: string }, description: "Omitted for metadata-only viewers." }
''',1)
p.write_text(s)

# A caller's read grant must not bypass a globally disabled send capability.
changefunc('internal/api/handlers/outbound_view.go','func(h *OutboundHandler)authorizeOutboundRetry',lambda s:s.replace('actor:=middleware.ActorFromContext(ctx)','actor:=middleware.ActorFromContext(ctx)\n if !actor.IsTenantAdmin()&&actor.Permission!=nil&&!actor.Permission.CanSend{return authz.ErrForbidden("sending not allowed")}'))
print('Migration, database constraints, isolation and API contracts updated')
