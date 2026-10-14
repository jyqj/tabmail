package models

import (
	"github.com/google/uuid"
	"testing"
	"time"
)

func TestP0PermanentAndTemporaryMessageExpiry(t *testing.T) {
	now := time.Now()
	uid := uuid.New()
	for _, hours := range []int{0, 1, 72} {
		expiry, err := MessageExpiry(&Mailbox{OwnerUserID: &uid}, hours, now)
		if err != nil {
			t.Fatal(err)
		}
		if expiry != nil {
			t.Fatal("owner inherits temporary TTL")
		}
	}
	expiry, err := MessageExpiry(&Mailbox{}, 0, now)
	if err != nil {
		t.Fatal(err)
	}
	if expiry != nil {
		t.Fatal("zero is not permanent")
	}
	expires, err := MessageExpiry(&Mailbox{}, 1, now)
	if err != nil {
		t.Fatal(err)
	}
	if expires == nil || !expires.Equal(now.Add(time.Hour)) {
		t.Fatal("temporary mailbox TTL changed")
	}
}
