package testutil

import (
	"context"
	"github.com/google/uuid"
	"strings"
	"tabmail/internal/app"
	"tabmail/internal/authz"
	"tabmail/internal/company"
	"tabmail/internal/delivery"
	"tabmail/internal/models"
	"tabmail/internal/store"
	"time"
)

// In-memory transaction fixture: validation and update share the existing mutex.
// PG tests, not this fake, prove row locks, audit rollback and database time.
func (s *FakeStore) RequeueOutboundJobAuthorized(ctx context.Context, a authz.Actor, observed *models.OutboundJob, validate store.OutboundRetryValidator) (*models.OutboundJob, error) {
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	if validate == nil || observed == nil || observed.TenantID != a.TenantID {
		return nil, app.Forbidden("retry validation unavailable")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	j := s.outboundJobs[observed.ID]
	if j == nil || j.TenantID != a.TenantID || j.State != models.OutboundDead && j.State != models.OutboundFailed {
		return nil, store.ErrOutboundNotRetryable
	}
	if j.InFlightDomain != "" {
		return nil, store.ErrOutboundUncertain
	}
	reader := &fakeRetryReader{s: s, tenant: a.TenantID}
	if e := validate(ctx, reader, cloneOutboundJob(j)); e != nil {
		return nil, e
	}
	for _, r := range s.outboundRecipients[j.ID] {
		if r.State == delivery.Uncertain {
			return nil, store.ErrOutboundUncertain
		}
	}
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	for _, deadline := range reader.deadlines {
		if !deadline.After(time.Now()) {
			return nil, app.Forbidden("retry credential or mailbox expired")
		}
	}
	if e := s.requeueOutboundJobLocked(j.ID); e != nil {
		return nil, e
	}
	return cloneOutboundJob(s.outboundJobs[j.ID]), nil
}

type fakeRetryReader struct {
	s         *FakeStore
	tenant    uuid.UUID
	deadlines []time.Time
}

var _ store.OutboundRetryReader = (*fakeRetryReader)(nil)

func (r *fakeRetryReader) deadline(t *time.Time) {
	if t != nil {
		r.deadlines = append(r.deadlines, *t)
	}
}
func (r *fakeRetryReader) GetUser(_ context.Context, id uuid.UUID) (*models.User, error) {
	v := r.s.users[id]
	if v == nil {
		return nil, nil
	}
	cp := *v
	return &cp, nil
}
func (r *fakeRetryReader) GetAPIKey(_ context.Context, id uuid.UUID) (*models.TenantAPIKey, error) {
	v := r.s.apiKeys[id]
	if v == nil || v.TenantID != r.tenant {
		return nil, nil
	}
	cp := *v
	r.deadline(cp.ExpiresAt)
	return &cp, nil
}
func (r *fakeRetryReader) GetZone(_ context.Context, id uuid.UUID) (*models.DomainZone, error) {
	v := r.s.zones[id]
	if v == nil || v.TenantID != r.tenant {
		return nil, nil
	}
	cp := *v
	return &cp, nil
}
func (r *fakeRetryReader) EffectivePermission(ctx context.Context, id uuid.UUID) (*models.EffectivePermission, error) {
	return r.s.EffectivePermission(ctx, id)
}
func (r *fakeRetryReader) GetMailboxGrant(_ context.Context, tenant, mailbox, user uuid.UUID) (*models.MailboxGrant, error) {
	v := r.s.mailboxGrants[[2]uuid.UUID{mailbox, user}]
	if v == nil || tenant != r.tenant || v.TenantID != tenant {
		return nil, nil
	}
	cp := *v
	return &cp, nil
}
func (r *fakeRetryReader) ForTenant(tenant uuid.UUID) store.TenantScoped {
	return fakeRetryTenant{r, tenant}
}

type fakeRetryTenant struct {
	r      *fakeRetryReader
	tenant uuid.UUID
}

func (v fakeRetryTenant) GetMailbox(_ context.Context, id uuid.UUID) (*models.Mailbox, error) {
	m := v.r.s.mailboxes[id]
	if m == nil || m.TenantID != v.tenant || v.tenant != v.r.tenant {
		return nil, nil
	}
	cp := *m
	v.r.deadline(cp.ExpiresAt)
	return &cp, nil
}
func (v fakeRetryTenant) GetMailboxByAddress(ctx context.Context, address string) (*models.Mailbox, error) {
	for _, m := range v.r.s.mailboxes {
		if m.FullAddress == address && m.TenantID == v.tenant {
			return v.GetMailbox(ctx, m.ID)
		}
	}
	return nil, nil
}
func (v fakeRetryTenant) GetMessage(context.Context, uuid.UUID) (*models.Message, error) {
	return nil, app.Forbidden("message reads are outside retry authorization")
}
func (r *fakeRetryReader) FindSendIdentityForAddress(_ context.Context, tenant uuid.UUID, address string) (*models.SendIdentity, error) {
	if tenant != r.tenant {
		return nil, nil
	}
	for _, v := range r.s.sendIdentities {
		if v.TenantID == tenant && v.Address == address && v.IdentityType == models.SendIdentityExact {
			cp := *v
			return &cp, nil
		}
	}
	if i := strings.LastIndex(address, "@"); i >= 0 {
		for _, v := range r.s.sendIdentities {
			if v.TenantID == tenant && v.Address == "*@"+address[i+1:] && v.IdentityType == models.SendIdentityDomainWildcard {
				cp := *v
				return &cp, nil
			}
		}
	}
	return nil, nil
}
func (r *fakeRetryReader) TemplateForSend(ctx context.Context, tenant uuid.UUID, user, key *uuid.UUID, mailbox, version uuid.UUID) (*company.TemplateVersion, string, string, error) {
	return (DeniedTemplateGovernance{}).TemplateForSend(ctx, tenant, user, key, mailbox, version)
}
