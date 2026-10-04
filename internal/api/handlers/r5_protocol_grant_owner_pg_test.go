//go:build r5protocol

package handlers_test

import (
	"context"
	"errors"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"tabmail/internal/company"
	"tabmail/internal/models"
)

// These are two new owner probes, not the formal component/catalog producer.
func TestR5UIGrantOwnerLoopbackTwoFixtures(t *testing.T) {
	dsn, err := url.Parse(os.Getenv("TABMAIL_TEST_DB_DSN"))
	if err != nil || (dsn.Hostname() != "127.0.0.1" && dsn.Hostname() != "localhost" && dsn.Hostname() != "::1") {
		t.Fatal("explicit loopback test PostgreSQL required")
	}
	type identity struct {
		tenant           uuid.UUID
		server, database string
	}
	identities := make(chan identity, 2)
	t.Cleanup(func() {
		var got []identity
		for i := 0; i < 2; i++ {
			select {
			case v := <-identities:
				got = append(got, v)
			default:
				t.Error("fixture setup did not finish")
				return
			}
		}
		a, b := got[0], got[1]
		if a.tenant == b.tenant || a.server == b.server || a.database == b.database {
			t.Error("fixture identities overlap")
		}
	})
	for _, name := range []string{"owner_a", "owner_b"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := r5UISeed(t)
			identities <- identity{f.tenant.ID, f.server.URL, f.pool.Config().ConnConfig.Database}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			mb, err := f.st.GetWorkMailbox(ctx, f.actor, f.shared.ID)
			r5UIMust(t, err)
			r5UIMust(t, f.st.SetWorkGrant(ctx, f.actor, models.MailboxGrant{MailboxID: f.shared.ID, UserID: f.employee.ID, CanRead: true, CanSend: true}, mb.Revision))
			draft, err := f.st.SaveMailDraft(ctx, f.member, company.Draft{MailboxID: f.shared.ID, Payload: company.DraftPayload{Subject: "owner probe"}})
			r5UIMust(t, err)
			var owner r5UIBarrierOwner
			// Registered before the barrier, after seed: verifies LIFO join before
			// seed's server and pool teardown without relying on their timing.
			t.Cleanup(func() {
				if owner != nil {
					select {
					case <-owner.ownerDone():
					default:
						t.Error("fixture cleanup preceded barrier join")
					}
				}
			})
			owner = r5UIGrantBarrier(t, f, draft)
			token := r5UIToken(t, f.employee)
			draft.Payload.Subject = "first save under serialized authority"
			r5UICall(t, f, token, "PUT", "/api/v1/company/drafts/"+draft.ID.String(), draft, 200)
			r5UIMust(t, owner.ownerResult())
			r5UIMust(t, owner.ownerClose())
			rights, err := f.st.GetWorkMailbox(ctx, f.member, f.shared.ID)
			r5UIMust(t, err)
			if rights.CanSend {
				t.Fatal("revocation did not finish before owner acknowledgement")
			}
			draft.Revision++
			draft.Payload.Subject = "after completed revocation"
			r5UICall(t, f, token, "PUT", "/api/v1/company/drafts/"+draft.ID.String(), draft, 403)
			probe, err := f.pool.Begin(ctx)
			r5UIMust(t, err)
			defer probe.Rollback(context.Background())
			_, err = probe.Exec(ctx, `SELECT id FROM mail_drafts WHERE id=$1 FOR UPDATE NOWAIT`, draft.ID)
			r5UIMust(t, err)
			r5UIMust(t, probe.Rollback(ctx))
			r5UIGrantOwnerPGCancel(t, f, draft)
		})
	}
}

func r5UIGrantOwnerPGCancel(t *testing.T, f *r5UIFixture, draft *company.Draft) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	hold, err := f.pool.Begin(ctx)
	if err != nil {
		cancel()
		r5UIMust(t, err)
	}
	t.Cleanup(func() {
		cancel()
		if err := hold.Rollback(context.Background()); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			t.Errorf("probe transaction cleanup failed: %T", err)
		}
	})
	_, err = hold.Exec(ctx, `SELECT id FROM mail_drafts WHERE id=$1 FOR UPDATE`, draft.ID)
	r5UIMust(t, err)
	pid := int32(hold.Conn().PgConn().PID())
	ready := make(chan struct{})
	childExited := make(chan struct{})
	owner := r5UINewGrantOwner(ctx, cancel, func() error {
		err := hold.Rollback(context.Background())
		if errors.Is(err, pgx.ErrTxClosed) {
			return nil
		}
		return err
	}, func(o *r5UIGrantOwner) error {
		// Deliberately noncooperative child: cancellation alone cannot stop its
		// real PG wait. Transaction release and physical join are both required.
		o.startRevoker(func(context.Context) error {
			defer close(childExited)
			_, err := f.pool.Exec(context.Background(), `UPDATE mail_drafts SET updated_at=now() WHERE id=$1`, draft.ID)
			return err
		})
		close(ready)
		<-ctx.Done()
		return ctx.Err()
	})
	t.Cleanup(func() {
		// Cancellation is the explicitly expected outcome of this probe. Every
		// other failure still rejects it, and close must physically join the child.
		err := owner.ownerClose()
		if !errors.Is(err, context.Canceled) {
			t.Errorf("cancel probe result type=%T", err)
		}
		if owner.releaseErr != nil {
			t.Errorf("release failed: %T", owner.releaseErr)
		}
		if owner.revokerErr != nil && !errors.Is(owner.revokerErr, context.Canceled) {
			t.Errorf("unexpected revoker failure: %T", owner.revokerErr)
		}
	})
	<-ready
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		var blocked bool
		r5UIMust(t, f.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND $1::int=ANY(pg_blocking_pids(pid)))`, pid).Scan(&blocked))
		if blocked {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("child lock wait was not observed")
		case <-ticker.C:
		}
	}
	if err := owner.ownerClose(); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel probe result type=%T", err)
	}
	select {
	case <-childExited:
	default:
		t.Fatal("owner acknowledged before PG child exit")
	}
	probe, err := f.pool.Begin(context.Background())
	r5UIMust(t, err)
	defer probe.Rollback(context.Background())
	_, err = probe.Exec(context.Background(), `SELECT id FROM mail_drafts WHERE id=$1 FOR UPDATE NOWAIT`, draft.ID)
	r5UIMust(t, err)
	r5UIMust(t, probe.Rollback(context.Background()))
}
