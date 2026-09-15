package postgres_test

import (
 "context"
 "os"
 "testing"
 "testing/fstest"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/stdlib"
 "github.com/pressly/goose/v3"
 "tabmail/internal/store/postgres"
 "tabmail/internal/testpg"
)

func TestReleaseRefreshFamilyMigrationBackfillAndRestart(t *testing.T) {
 _,pool,dsn:=testpg.NewPostgres(t)
 ctx:=context.Background()
 _,err:=pool.Exec(ctx,`DROP SCHEMA public CASCADE; CREATE SCHEMA public`)
 must(t,err)
 cfg,err:=pgx.ParseConfig(dsn);must(t,err)
 db:=stdlib.OpenDB(*cfg);defer db.Close()
 fs:=fstest.MapFS{}
 for _,name:=range []string{"00001_baseline.sql","00002_mailbox_grants.sql"}{
  data,err:=os.ReadFile("migrations/"+name);must(t,err);fs[name]=&fstest.MapFile{Data:data}
 }
 provider,err:=goose.NewProvider(goose.DialectPostgres,db,fs);must(t,err)
 _,err=provider.Up(ctx);must(t,err)
 user:=uuid.New();first,second:=uuid.New(),uuid.New()
 _,err=pool.Exec(ctx,`INSERT INTO users(id,tenant_id,email,password_hash,role,is_active) VALUES($1,'00000000-0000-0000-0000-000000000001','migration@example.test','test','user',true)`,user);must(t,err)
 _,err=pool.Exec(ctx,`INSERT INTO refresh_tokens(id,user_id,token_hash,expires_at) VALUES($1,$3,'migration-token-one',now()+interval '1 day'),($2,$3,'migration-token-two',now()+interval '1 day')`,first,second,user);must(t,err)
 must(t,postgres.Migrate(ctx,cfg))
 must(t,postgres.Migrate(ctx,cfg))
 var count int
 must(t,pool.QueryRow(ctx,`SELECT count(*) FROM refresh_tokens WHERE id IN ($1,$2) AND family_id=id AND revoked_at IS NULL`,first,second).Scan(&count))
 if count!=2{t.Fatalf("existing sessions not preserved as independent families: %d",count)}
 var version int
 must(t,pool.QueryRow(ctx,`SELECT max(version_id) FROM goose_db_version WHERE is_applied`).Scan(&version))
 if version!=3{t.Fatalf("version=%d",version)}
}
