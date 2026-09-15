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
		if MessageExpiry(&Mailbox{OwnerUserID: &uid}, hours, now) != nil {
			t.Fatal("owner inherits temporary TTL")
		}
	}
	if MessageExpiry(&Mailbox{}, 0, now) != nil {
		t.Fatal("zero is not permanent")
	}
	expires := MessageExpiry(&Mailbox{}, 1, now)
	if expires == nil || !expires.Equal(now.Add(time.Hour)) {
		t.Fatal("temporary mailbox TTL changed")
	}
}
