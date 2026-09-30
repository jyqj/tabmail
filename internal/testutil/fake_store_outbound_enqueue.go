package testutil

import (
	"context"
	"tabmail/internal/app"
	"tabmail/internal/models"
	"tabmail/internal/store"
	"time"
)

// The fake models policy/update atomicity only. PostgreSQL regressions prove
// row ordering, required audit, draft consumption and database deadlines.
func (s *FakeStore) CreateOutboundJobAuthorized(ctx context.Context, job *models.OutboundJob, quota store.OutboundQuotaReservation, draft *store.DraftConsumption, validate store.OutboundRetryValidator) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if job == nil || validate == nil {
		return false, app.Forbidden("enqueue validation unavailable")
	}
	if draft != nil {
		return false, app.BadRequest("draft submission unavailable")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	reader := &fakeRetryReader{s: s, tenant: job.TenantID}
	if err := validate(ctx, reader, job); err != nil {
		return false, err
	}
	if q := quota.UserDaily; q != nil && q.Limit > 0 && s.countOutboundSinceLocked(job.TenantID, q.UserID, q.Since) >= q.Limit {
		return false, store.ErrOutboundDailyQuotaExceeded
	}
	if q := quota.SendAsDaily; q != nil && q.Limit > 0 && s.countOutboundByIdentitySinceLocked(job.TenantID, q.PrincipalType, q.PrincipalID, q.IdentityID, q.Since) >= q.Limit {
		return false, store.ErrSendAsDailyQuotaExceeded
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	for _, deadline := range reader.deadlines {
		if !deadline.After(time.Now()) {
			return false, app.Forbidden("enqueue credential or mailbox expired")
		}
	}
	s.createOutboundJobLocked(job)
	return false, nil
}
