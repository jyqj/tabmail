package messageapp

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"tabmail/internal/app"
	"tabmail/internal/models"
	"tabmail/internal/policy"
	"tabmail/internal/testutil"
)

func TestResolveMailboxOwnerBoundAPIKeyRequiresExplicitAccess(t *testing.T) {
	ctx := context.Background()
	_, svc, tenant, mailbox := seededMessageService(t, models.AccessAPIKey, nil)
	keyID := uuid.New()
	ownerID := uuid.New()

	viewer := Viewer{
		Tenant:        tenant,
		AuthMode:      AuthModeAPIKey,
		PrincipalType: AuthModeAPIKey,
		PrincipalID:   &keyID,
		OwnerUserID:   &ownerID,
	}

	_, err := svc.ResolveMailbox(ctx, mailbox.FullAddress, viewer)
	if err == nil {
		t.Fatal("owner-bound API key without tenant-wide access should be denied")
	}
	if appErr, ok := app.As(err); !ok || appErr.Kind != app.KindForbidden {
		t.Fatalf("expected forbidden app error, got %T %v", err, err)
	}

	integrationKey := Viewer{
		Tenant:        tenant,
		AuthMode:      AuthModeAPIKey,
		PrincipalType: AuthModeAPIKey,
		PrincipalID:   ptrUUID(uuid.New()),
		TenantWide:    true,
	}
	if _, err := svc.ResolveMailbox(ctx, mailbox.FullAddress, integrationKey); err != nil {
		t.Fatalf("ownerless integration API key should remain tenant-wide: %v", err)
	}
}

func TestResolveMailboxOwnerBoundAPIKeyOwnerFallbackAndAllowedZones(t *testing.T) {
	ctx := context.Background()
	ownerID := uuid.New()
	_, svc, tenant, mailbox := seededMessageService(t, models.AccessAPIKey, &ownerID)
	keyID := uuid.New()

	viewer := Viewer{
		Tenant:        tenant,
		AuthMode:      AuthModeAPIKey,
		PrincipalType: AuthModeAPIKey,
		PrincipalID:   &keyID,
		OwnerUserID:   &ownerID,
	}
	if _, err := svc.ResolveMailboxForWrite(ctx, mailbox.FullAddress, viewer); err != nil {
		t.Fatalf("owner-bound API key should inherit zone owner fallback: %v", err)
	}

	otherZoneID := uuid.New()
	viewer.AllowedZoneIDs = []uuid.UUID{otherZoneID}
	_, err := svc.ResolveMailbox(ctx, mailbox.FullAddress, viewer)
	if err == nil {
		t.Fatal("allowed_zone_ids should narrow owner-bound API key access")
	}
	if appErr, ok := app.As(err); !ok || appErr.Kind != app.KindForbidden {
		t.Fatalf("expected forbidden app error, got %T %v", err, err)
	}
}

func seededMessageService(t *testing.T, accessMode models.AccessMode, ownerUserID *uuid.UUID) (*testutil.FakeStore, *Service, *models.Tenant, *models.Mailbox) {
	t.Helper()
	st := testutil.NewFakeStore()
	tenant := &models.Tenant{ID: uuid.New(), Name: "tenant"}
	st.SeedTenant(tenant)
	_, svc, _, mailbox := seededMessageServiceWithStore(t, st, tenant, accessMode, ownerUserID)
	return st, svc, tenant, mailbox
}

func seededMessageServiceWithStore(t *testing.T, st *testutil.FakeStore, tenant *models.Tenant, accessMode models.AccessMode, ownerUserID *uuid.UUID) (*testutil.FakeStore, *Service, *models.Tenant, *models.Mailbox) {
	t.Helper()
	zoneID := uuid.New()
	domain := zoneID.String() + ".mail.test"
	zone := &models.DomainZone{
		ID:          zoneID,
		TenantID:    tenant.ID,
		OwnerUserID: ownerUserID,
		Domain:      domain,
		IsVerified:  true,
		MXVerified:  true,
		CreatedAt:   time.Now(),
	}
	st.SeedZone(zone)
	mailbox := &models.Mailbox{
		ID:             uuid.New(),
		TenantID:       tenant.ID,
		ZoneID:         zoneID,
		LocalPart:      "inbox",
		ResolvedDomain: domain,
		FullAddress:    "inbox@" + domain,
		AccessMode:     accessMode,
		CreatedAt:      time.Now(),
	}
	st.SeedMailbox(mailbox)
	svc := NewService(st, testutil.NewMemoryObjectStore(), nil, nil, policy.NamingFull, true, "mailbox-secret", zerolog.Nop())
	return st, svc, tenant, mailbox
}

func ptrUUID(id uuid.UUID) *uuid.UUID {
	return &id
}

func TestCursorRoundTrip(t *testing.T) {
	msg := &models.Message{ID: uuid.New(), ReceivedAt: time.Now().Truncate(time.Microsecond)}
	token := EncodeCursor(msg)
	if token == "" {
		t.Fatal("expected non-empty cursor token")
	}
	cursor, err := DecodeCursor(token)
	if err != nil {
		t.Fatal(err)
	}
	if !cursor.ReceivedAt.Equal(msg.ReceivedAt) || cursor.ID != msg.ID {
		t.Fatalf("cursor did not round-trip: %#v vs %#v", cursor, msg)
	}
}

func TestDecodeCursorRejectsMalformedTokens(t *testing.T) {
	for _, token := range []string{"", "!!!", "bm90LWEtY3Vyc29y", "MTIzNA"} {
		if _, err := DecodeCursor(token); err == nil {
			t.Fatalf("expected error for token %q", token)
		} else if appErr, ok := app.As(err); !ok || appErr.Kind != app.KindBadRequest {
			t.Fatalf("expected bad-request app error for token %q, got %v", token, err)
		}
	}
}

func TestListMessagesKeysetWalksAllPages(t *testing.T) {
	ctx := context.Background()
	st, svc, tenant, mailbox := seededMessageService(t, models.AccessPublic, nil)

	base := time.Now().Truncate(time.Microsecond)
	const totalMessages = 7
	for i := range totalMessages {
		if err := st.CreateMessage(ctx, &models.Message{
			TenantID:   tenant.ID,
			MailboxID:  mailbox.ID,
			ZoneID:     mailbox.ZoneID,
			Subject:    "m",
			ReceivedAt: base.Add(time.Duration(i) * time.Second),
			ExpiresAt:  base.Add(24 * time.Hour),
		}); err != nil {
			t.Fatal(err)
		}
	}
	viewer := Viewer{AuthMode: AuthModePublic}

	var seen []uuid.UUID
	var cursor *models.MessageCursor
	const pageSize = 3
	for page := 0; ; page++ {
		items, total, next, err := svc.ListMessagesKeyset(ctx, mailbox.FullAddress, viewer, cursor, pageSize)
		if err != nil {
			t.Fatal(err)
		}
		if total != totalMessages {
			t.Fatalf("expected total %d, got %d", totalMessages, total)
		}
		for _, m := range items {
			seen = append(seen, m.ID)
		}
		if next == "" {
			break
		}
		if len(items) != pageSize {
			t.Fatalf("expected a full page before a next cursor, got %d items", len(items))
		}
		cursor, err = DecodeCursor(next)
		if err != nil {
			t.Fatal(err)
		}
		if page > totalMessages {
			t.Fatal("cursor never terminated")
		}
	}

	if len(seen) != totalMessages {
		t.Fatalf("expected %d messages across pages, got %d", totalMessages, len(seen))
	}
	unique := map[uuid.UUID]struct{}{}
	for _, id := range seen {
		unique[id] = struct{}{}
	}
	if len(unique) != totalMessages {
		t.Fatalf("expected no duplicates across pages, got %d unique of %d", len(unique), len(seen))
	}
}
