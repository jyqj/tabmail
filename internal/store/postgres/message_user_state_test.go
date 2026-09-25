package postgres_test

import (
	"context"
	"testing"

	"tabmail/internal/authz"
	"tabmail/internal/models"
)

// Shared-mailbox read state is personal: marking a message seen or starring it
// writes the actor's own sparse row and never mutates the shared baseline other
// members read. Organizing (trash/archive/restore) remains a shared action
// gated by CanOrganize.
func TestSharedMailboxPersonalStateSeparation(t *testing.T) {
	f := seedCompany(t)
	ctx := context.Background()
	readerActor := authz.Actor{Type: authz.PrincipalUser, ID: f.employee.ID, TenantID: f.tenant.ID, Role: models.RoleUser}
	organizerActor := authz.Actor{Type: authz.PrincipalUser, ID: f.other.ID, TenantID: f.tenant.ID, Role: models.RoleUser}
	must(t, grantCurrent(f.st, ctx, f.a, models.MailboxGrant{MailboxID: f.shared.ID, UserID: f.employee.ID, CanRead: true}))
	must(t, grantCurrent(f.st, ctx, f.a, models.MailboxGrant{MailboxID: f.shared.ID, UserID: f.other.ID, CanRead: true, CanOrganize: true}))
	m := &models.Message{TenantID: f.tenant.ID, MailboxID: f.shared.ID, ZoneID: f.zone.ID, Sender: "sender@client.test", Recipients: []string{f.shared.FullAddress}, Subject: "shared item", RawObjectKey: "shared-item"}
	must(t, f.st.CreateMessage(ctx, m))

	// A read-only member marks the message read for themselves.
	must(t, f.st.MutateWorkMessage(ctx, readerActor, f.shared.ID, m.ID, "seen"))
	var baselineSeen bool
	must(t, f.pool.QueryRow(ctx, `SELECT seen FROM messages WHERE id=$1`, m.ID).Scan(&baselineSeen))
	if baselineSeen {
		t.Fatal("personal seen leaked into the shared baseline row")
	}
	var personalRows int
	must(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM message_user_states WHERE mailbox_id=$1 AND message_id=$2 AND user_id=$3`, f.shared.ID, m.ID, f.employee.ID).Scan(&personalRows))
	if personalRows != 1 {
		t.Fatalf("personal state row missing: %d", personalRows)
	}
	rows, _, e := f.st.ListWorkMessages(ctx, readerActor, f.shared.ID, "inbox", "", models.Page{})
	must(t, e)
	if len(rows) != 1 || !rows[0].Seen {
		t.Fatal("reader does not see own read state")
	}
	rows, _, e = f.st.ListWorkMessages(ctx, organizerActor, f.shared.ID, "inbox", "", models.Page{})
	must(t, e)
	if len(rows) != 1 || rows[0].Seen {
		t.Fatal("one member's read state overrode another member's view")
	}

	// Unseen and starred are personal toggles too.
	must(t, f.st.MutateWorkMessage(ctx, readerActor, f.shared.ID, m.ID, "unseen"))
	must(t, f.st.MutateWorkMessage(ctx, readerActor, f.shared.ID, m.ID, "starred"))
	rows, _, e = f.st.ListWorkMessages(ctx, readerActor, f.shared.ID, "inbox", "", models.Page{})
	must(t, e)
	if len(rows) != 1 || rows[0].Seen || !rows[0].Starred {
		t.Fatalf("personal toggle merge wrong: seen=%v starred=%v", rows[0].Seen, rows[0].Starred)
	}
	rows, _, e = f.st.ListWorkMessages(ctx, organizerActor, f.shared.ID, "inbox", "", models.Page{})
	must(t, e)
	if len(rows) != 1 || rows[0].Starred {
		t.Fatal("starring leaked across members")
	}

	// The personal override wins over a pre-existing shared baseline.
	_, e = f.pool.Exec(ctx, `UPDATE messages SET seen=true WHERE id=$1`, m.ID)
	must(t, e)
	rows, _, e = f.st.ListWorkMessages(ctx, readerActor, f.shared.ID, "inbox", "", models.Page{})
	must(t, e)
	if len(rows) != 1 || rows[0].Seen {
		t.Fatal("baseline seen should not override a personal unseen")
	}
	rows, _, e = f.st.ListWorkMessages(ctx, organizerActor, f.shared.ID, "inbox", "", models.Page{})
	must(t, e)
	if len(rows) != 1 || !rows[0].Seen {
		t.Fatal("member without a personal row must read the shared baseline")
	}
	_, e = f.pool.Exec(ctx, `UPDATE messages SET seen=false WHERE id=$1`, m.ID)
	must(t, e)

	// Read-only members still cannot organize.
	if e = f.st.MutateWorkMessage(ctx, readerActor, f.shared.ID, m.ID, "trash"); e == nil {
		t.Fatal("read-only member organized a shared mailbox")
	}
	if e = f.st.MutateWorkMessage(ctx, readerActor, f.shared.ID, m.ID, "archive"); e == nil {
		t.Fatal("read-only member archived a shared mailbox")
	}

	// Organizing is shared state: trash moves the message for everyone.
	must(t, f.st.MutateWorkMessage(ctx, organizerActor, f.shared.ID, m.ID, "trash"))
	for name, actor := range map[string]authz.Actor{"reader": readerActor, "organizer": organizerActor} {
		_, inboxTotal, e := f.st.ListWorkMessages(ctx, actor, f.shared.ID, "inbox", "", models.Page{})
		must(t, e)
		if inboxTotal != 0 {
			t.Fatalf("%s still sees the trashed message in inbox", name)
		}
		_, trashTotal, e := f.st.ListWorkMessages(ctx, actor, f.shared.ID, "trash", "", models.Page{})
		must(t, e)
		if trashTotal != 1 {
			t.Fatalf("%s cannot see shared trash state", name)
		}
	}
	must(t, f.st.MutateWorkMessage(ctx, organizerActor, f.shared.ID, m.ID, "restore"))
}

// Personal mailboxes keep their current behavior exactly: the owner's seen
// action still writes messages.seen (fast path), so both the workbench route
// and the legacy mailbox route stay consistent, and starring is additive.
func TestPersonalMailboxSeenBehaviorUnchanged(t *testing.T) {
	f := seedCompany(t)
	ctx := context.Background()
	m := &models.Message{TenantID: f.tenant.ID, MailboxID: f.personal.ID, ZoneID: f.zone.ID, Sender: "sender@client.test", Recipients: []string{f.personal.FullAddress}, Subject: "private item", RawObjectKey: "private-item"}
	must(t, f.st.CreateMessage(ctx, m))

	must(t, f.st.MutateWorkMessage(ctx, f.u, f.personal.ID, m.ID, "seen"))
	var baselineSeen bool
	must(t, f.pool.QueryRow(ctx, `SELECT seen FROM messages WHERE id=$1`, m.ID).Scan(&baselineSeen))
	if !baselineSeen {
		t.Fatal("personal mailbox fast path no longer updates messages.seen")
	}
	rows, _, e := f.st.ListWorkMessages(ctx, f.u, f.personal.ID, "inbox", "", models.Page{})
	must(t, e)
	if len(rows) != 1 || !rows[0].Seen || rows[0].Starred {
		t.Fatal("personal mailbox merged view wrong")
	}

	must(t, f.st.MutateWorkMessage(ctx, f.u, f.personal.ID, m.ID, "unseen"))
	must(t, f.pool.QueryRow(ctx, `SELECT seen FROM messages WHERE id=$1`, m.ID).Scan(&baselineSeen))
	if baselineSeen {
		t.Fatal("personal mailbox unseen lost")
	}
	must(t, f.st.MutateWorkMessage(ctx, f.u, f.personal.ID, m.ID, "starred"))
	rows, _, e = f.st.ListWorkMessages(ctx, f.u, f.personal.ID, "inbox", "", models.Page{})
	must(t, e)
	if len(rows) != 1 || rows[0].Seen || !rows[0].Starred {
		t.Fatal("personal mailbox starred merge wrong")
	}
}

// Personal state writes in a shared mailbox must keep SSE clients consistent:
// the trigger does not fire (no messages row changed), so the store writes the
// invalidation event itself.
func TestPersonalStateWritesStillEmitMailboxEvents(t *testing.T) {
	f := seedCompany(t)
	ctx := context.Background()
	must(t, grantCurrent(f.st, ctx, f.a, models.MailboxGrant{MailboxID: f.shared.ID, UserID: f.employee.ID, CanRead: true}))
	m := &models.Message{TenantID: f.tenant.ID, MailboxID: f.shared.ID, ZoneID: f.zone.ID, Sender: "sender@client.test", Recipients: []string{f.shared.FullAddress}, Subject: "event item", RawObjectKey: "event-item"}
	must(t, f.st.CreateMessage(ctx, m))
	readerActor := authz.Actor{Type: authz.PrincipalUser, ID: f.employee.ID, TenantID: f.tenant.ID, Role: models.RoleUser}
	must(t, f.st.MutateWorkMessage(ctx, readerActor, f.shared.ID, m.ID, "seen"))
	events, _, e := f.st.ListMailboxEvents(ctx, f.tenant.ID, f.shared.ID, 0, 100)
	must(t, e)
	found := false
	for _, v := range events {
		if v.Type == "changed" && v.MessageID != nil && *v.MessageID == m.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("personal seen did not emit a mailbox invalidation event")
	}
}
