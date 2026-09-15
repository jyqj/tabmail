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

# A shared mailbox without an owner/password uses the existing private API-key
# mode; grants are evaluated before that legacy access-mode fallback.
changefunc('internal/store/postgres/release_blockers_integration_test.go','func TestReleasePostgresSharedMailboxAndOutboundScope',lambda s:s.replace('AccessMode:models.AccessToken','AccessMode:models.AccessAPIKey'))
changefunc('internal/api/release_blockers_integration_test.go','func TestReleaseSharedOutboundReadIsNotRetry',lambda s:s.replace('AccessMode:models.AccessToken','AccessMode:models.AccessAPIKey'))
print('Migration, database constraints and isolation contracts updated')
