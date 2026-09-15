#!/usr/bin/env python3
from finalize import ROOT,edit

# v3 is now the latest migration; the P0 historical-conflict case still stops
# safely at v1. Preserve that existing safety assertion rather than skipping it.
edit('internal/store/postgres/company_integration_test.go','want := 2','want := 3')

# Last-admin and refresh protocols require fresh snapshots after waiting on a
# lock. Pin READ COMMITTED rather than inheriting an operator's session default.
for path in ['internal/store/postgres/member_guard.go','internal/store/postgres/refresh_rotation.go']:
 p=ROOT/path;s=p.read_text()
 assert 's.pool.Begin(ctx)' in s
 p.write_text(s.replace('s.pool.Begin(ctx)','s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})'))
print('Migration fixture and isolation contracts updated')
