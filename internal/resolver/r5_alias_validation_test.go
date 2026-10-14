package resolver

import (
	"context"
	"testing"

	"tabmail/internal/policy"
)

func TestR5InvalidPlusTagCannotMaterializeMailbox(t *testing.T) {
	for _, local := range []string{"alice+bad..tag", "alice+bad@tag", "alice+bad tag"} {
		t.Run(local, func(t *testing.T) {
			st := seededResolverStore()
			rv := New(st, policy.NamingFull, true)
			res, err := rv.Resolve(context.Background(), local+"@sub.mail.test")
			if err == nil || res != nil {
				t.Errorf("malformed alias resolved: result=%#v, error=%v", res, err)
			}
			count, err := st.CountAllMailboxes(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if count != 0 {
				t.Fatalf("malformed plus tag created %d mailboxes", count)
			}
		})
	}
}

func TestR5ValidPlusTagsReuseOneMailbox(t *testing.T) {
	st := seededResolverStore()
	rv := New(st, policy.NamingFull, true)
	first, err := rv.Resolve(context.Background(), "alice+news.daily@sub.mail.test")
	if err != nil || first == nil || first.Mailbox == nil || !first.Created {
		t.Fatalf("valid first alias: %#v, %v", first, err)
	}
	second, err := rv.Resolve(context.Background(), "alice+updates@sub.mail.test")
	if err != nil || second == nil || second.Mailbox == nil || second.Created {
		t.Fatalf("valid subsequent alias: %#v, %v", second, err)
	}
	if first.Mailbox.ID != second.Mailbox.ID || second.Mailbox.FullAddress != "alice@sub.mail.test" {
		t.Fatalf("valid aliases did not reuse the canonical mailbox: %#v / %#v", first.Mailbox, second.Mailbox)
	}
}
