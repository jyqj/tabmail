package outbound

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"tabmail/internal/config"
	"tabmail/internal/models"
	"tabmail/internal/store"
	"tabmail/internal/testutil"
)

func TestSubmitReservesUserDailyQuotaWithJobCreation(t *testing.T) {
	ctx := context.Background()
	st := testutil.NewFakeStore()
	svc := NewService(config.Outbound{Enabled: true, MaxRetries: 3}, st, zerolog.Nop())
	tenantID := uuid.New()
	userID := uuid.New()
	req := quotaTestSendRequest(tenantID, userID)
	req.Quota.UserDaily = &store.OutboundUserDailyQuota{
		UserID: &userID,
		Since:  time.Now().Add(-time.Hour),
		Limit:  1,
	}

	if _, err := svc.Submit(ctx, req); err != nil {
		t.Fatalf("first submit should reserve quota and enqueue: %v", err)
	}
	if _, err := svc.Submit(ctx, req); !errors.Is(err, store.ErrOutboundDailyQuotaExceeded) {
		t.Fatalf("second submit expected user quota error, got %v", err)
	}
	_, total, err := st.ListOutboundJobs(ctx, tenantID, models.Page{Page: 1, PerPage: 10})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 {
		t.Fatalf("quota failure must not enqueue another job, got total=%d", total)
	}
}

func TestSubmitReservesSendAsDailyQuotaWithJobCreation(t *testing.T) {
	ctx := context.Background()
	st := testutil.NewFakeStore()
	svc := NewService(config.Outbound{Enabled: true, MaxRetries: 3}, st, zerolog.Nop())
	tenantID := uuid.New()
	userID := uuid.New()
	identity := &models.SendIdentity{
		ID:           uuid.New(),
		TenantID:     tenantID,
		ZoneID:       uuid.New(),
		Address:      "sender@example.test",
		IdentityType: models.SendIdentityExact,
		Verified:     true,
	}
	if err := st.CreateSendIdentity(ctx, identity); err != nil {
		t.Fatal(err)
	}
	req := quotaTestSendRequest(tenantID, userID)
	req.Quota.SendAsDaily = &store.OutboundSendAsDailyQuota{
		PrincipalType: "user",
		PrincipalID:   userID,
		IdentityID:    identity.ID,
		Since:         time.Now().Add(-time.Hour),
		Limit:         1,
	}

	if _, err := svc.Submit(ctx, req); err != nil {
		t.Fatalf("first submit should reserve send-as quota and enqueue: %v", err)
	}
	if _, err := svc.Submit(ctx, req); !errors.Is(err, store.ErrSendAsDailyQuotaExceeded) {
		t.Fatalf("second submit expected send-as quota error, got %v", err)
	}
	_, total, err := st.ListOutboundJobs(ctx, tenantID, models.Page{Page: 1, PerPage: 10})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 {
		t.Fatalf("send-as quota failure must not enqueue another job, got total=%d", total)
	}
}

func quotaTestSendRequest(tenantID, userID uuid.UUID) SendRequest {
	return SendRequest{
		TenantID: tenantID,
		UserID:   &userID,
		ZoneID:   uuid.New(),
		From:     "sender@example.test",
		To:       []string{"recipient@example.net"},
		Subject:  "quota test",
		TextBody: "hello",
	}
}
