package smtp_test

import (
	"bufio"
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/config"
	"tabmail/internal/ingest"
	"tabmail/internal/models"
	"tabmail/internal/policy"
	"tabmail/internal/resolver"
	smtpsrv "tabmail/internal/smtp"
	"tabmail/internal/store/fileobj"
	"tabmail/internal/testpg"
)

// The real protocol must acknowledge only AFTER the spool and receipt commit.
// All connections are local and recipients use reserved test domains.
func TestSMTPDurableAcceptanceAndDatabaseFailure(t *testing.T) {
	st, pool, _ := testpg.NewPostgres(t)
	ctx := context.Background()
	tenant := &models.Tenant{Name: "Spool", PlanID: uuid.MustParse("00000000-0000-0000-0000-000000000002")}
	if err := st.CreateTenant(ctx, tenant); err != nil {
		t.Fatal(err)
	}
	zone := &models.DomainZone{TenantID: tenant.ID, Domain: "spool.test", IsVerified: true, MXVerified: true}
	if err := st.CreateZone(ctx, zone); err != nil {
		t.Fatal(err)
	}
	box := &models.Mailbox{TenantID: tenant.ID, ZoneID: zone.ID, LocalPart: "alice", ResolvedDomain: zone.Domain, FullAddress: "alice@spool.test", AccessMode: models.AccessAPIKey}
	if err := st.CreateMailbox(ctx, box); err != nil {
		t.Fatal(err)
	}
	obj, err := fileobj.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	rv := resolver.New(st, policy.NamingFull, false)
	svc := ingest.NewService(st, obj, rv, nil, nil, models.SMTPPolicy{DefaultAccept: true, DefaultStore: true}, 24, nil, config.Ingest{Durable: true}, zerolog.Nop())
	addr := freeAddr(t)
	srv := smtpsrv.NewServer(config.SMTP{Addr: addr, Domain: "mx.spool.test", Timeout: 3 * time.Second, MaxMessageBytes: 1024 * 1024, MaxRecipients: 10}, svc, rv, zerolog.Nop())
	go func() { _ = srv.Start(ctx) }()
	waitTCP(t, addr)
	defer srv.Shutdown(ctx)
	send := func(want string) {
		t.Helper()
		conn, err := net.Dial("tcp", addr)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		reader := bufio.NewReader(conn)
		expectCode(t, reader, "220")
		for _, cmd := range []string{"HELO local.test", "MAIL FROM:<sender@remote.test>", "RCPT TO:<alice@spool.test>"} {
			sendLine(t, conn, cmd)
			expectCode(t, reader, "250")
		}
		sendLine(t, conn, "DATA")
		expectCode(t, reader, "354")
		for _, line := range []string{"Subject: wire", "", "persist before ack", "."} {
			sendLine(t, conn, line)
		}
		expectCode(t, reader, want)
	}
	send("250")
	jobs, total, err := st.ListIngestJobs(ctx, models.Page{Page: 1, PerPage: 10}, "", "", "")
	if err != nil || total != 1 {
		t.Fatalf("ack without receipt: %d %v", total, err)
	}
	targets, err := st.ListIngressTargets(ctx, jobs[0].ID)
	if err != nil || len(targets) != 1 {
		t.Fatal("ack without destinations")
	}
	if exists, err := obj.Exists(ctx, jobs[0].RawObjectKey); err != nil || !exists {
		t.Fatal("ack without raw file")
	}
	var count int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM messages`).Scan(&count); err != nil || count != 0 {
		t.Fatal("accept bypassed durable worker")
	}
	_, err = pool.Exec(ctx, `CREATE FUNCTION refuse_receipt() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected receipt commit failure'; END $$;
 CREATE TRIGGER receipt_fault BEFORE INSERT ON ingest_jobs FOR EACH ROW EXECUTE FUNCTION refuse_receipt()`)
	if err != nil {
		t.Fatal(err)
	}
	send("451")
	_, total, err = st.ListIngestJobs(ctx, models.Page{Page: 1, PerPage: 10}, "", "", "")
	if err != nil || total != 1 {
		t.Fatal("failed accept left a partial receipt")
	}
	// No raw-object cleanup follows ambiguous commit failure. Existing receipt is intact.
	if strings.HasPrefix(jobs[0].RawObjectKey, "sha256/") {
		t.Fatal("durable receipt used shared legacy object key")
	}
}
