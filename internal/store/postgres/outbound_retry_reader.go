package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"tabmail/internal/app"
	"tabmail/internal/authz"
	"tabmail/internal/company"
	"tabmail/internal/models"
	"tabmail/internal/store"
	"time"
)

// Authorization-only adapter. Every read uses tx, never PgStore.pool. NOWAIT
// dependencies cannot deadlock with legacy deletes using the opposite order.
// No cached result is reused across the job-row wait.
type outboundRetryReader struct {
	store     *PgStore
	tx        pgx.Tx
	tenant    uuid.UUID
	deadlines []time.Time
}

var _ store.OutboundRetryReader = (*outboundRetryReader)(nil)

func (r *outboundRetryReader) deadline(v *time.Time) {
	if v != nil {
		r.deadlines = append(r.deadlines, *v)
	}
}
func (r *outboundRetryReader) GetUser(ctx context.Context, id uuid.UUID) (*models.User, error) {
	return scanUser(r.tx.QueryRow(ctx, userSelect+` WHERE id=$1 FOR SHARE NOWAIT`, id))
}
func (r *outboundRetryReader) EffectivePermission(ctx context.Context, id uuid.UUID) (*models.EffectivePermission, error) {
	u, e := r.GetUser(ctx, id)
	if e != nil {
		return nil, e
	}
	if u == nil {
		return nil, authz.ErrForbidden("retry user unavailable")
	}
	return effectivePermissionSnapshot(ctx, r.tx, id)
}
func (r *outboundRetryReader) GetAPIKey(ctx context.Context, id uuid.UUID) (*models.TenantAPIKey, error) {
	k := &models.TenantAPIKey{}
	var raw json.RawMessage
	e := r.tx.QueryRow(ctx, `SELECT id,tenant_id,owner_user_id,scopes,allowed_zone_ids,expires_at FROM tenant_api_keys WHERE id=$1 AND tenant_id=$2 FOR SHARE NOWAIT`, id, r.tenant).Scan(&k.ID, &k.TenantID, &k.OwnerUserID, &raw, &k.AllowedZoneIDs, &k.ExpiresAt)
	if errors.Is(e, pgx.ErrNoRows) {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	if e = json.Unmarshal(raw, &k.Scopes); e != nil {
		return nil, e
	}
	r.deadline(k.ExpiresAt)
	return k, nil
}
func (r *outboundRetryReader) GetZone(ctx context.Context, id uuid.UUID) (*models.DomainZone, error) {
	return scanZone(r.tx.QueryRow(ctx, zoneSelect+` WHERE id=$1 AND tenant_id=$2 FOR SHARE NOWAIT`, id, r.tenant))
}
func (r *outboundRetryReader) GetMailboxGrant(ctx context.Context, tenant, mailbox, user uuid.UUID) (*models.MailboxGrant, error) {
	if tenant != r.tenant {
		return nil, nil
	}
	g := &models.MailboxGrant{}
	e := r.tx.QueryRow(ctx, `SELECT tenant_id,mailbox_id,user_id,can_read,can_organize,can_send,template_only FROM mailbox_grants WHERE tenant_id=$1 AND mailbox_id=$2 AND user_id=$3 FOR SHARE NOWAIT`, tenant, mailbox, user).Scan(&g.TenantID, &g.MailboxID, &g.UserID, &g.CanRead, &g.CanOrganize, &g.CanSend, &g.TemplateOnly)
	if errors.Is(e, pgx.ErrNoRows) {
		return nil, nil
	}
	return g, e
}
func (r *outboundRetryReader) ForTenant(tenant uuid.UUID) store.TenantScoped {
	return retryTenantView{r, tenant}
}

type retryTenantView struct {
	r      *outboundRetryReader
	tenant uuid.UUID
}

func (v retryTenantView) GetMailbox(ctx context.Context, id uuid.UUID) (*models.Mailbox, error) {
	if v.tenant != v.r.tenant {
		return nil, nil
	}
	m, e := v.r.store.scanMailbox(v.r.tx.QueryRow(ctx, mailboxSelect+` WHERE m.id=$1 AND m.tenant_id=$2 FOR SHARE OF m NOWAIT`, id, v.tenant))
	if m != nil {
		v.r.deadline(m.ExpiresAt)
	}
	return m, e
}
func (v retryTenantView) GetMailboxByAddress(ctx context.Context, address string) (*models.Mailbox, error) {
	if v.tenant != v.r.tenant {
		return nil, nil
	}
	m, e := v.r.store.scanMailbox(v.r.tx.QueryRow(ctx, mailboxSelect+` WHERE m.full_address=$1 AND m.tenant_id=$2 FOR SHARE OF m NOWAIT`, address, v.tenant))
	if m != nil {
		v.r.deadline(m.ExpiresAt)
	}
	return m, e
}
func (v retryTenantView) GetMessage(context.Context, uuid.UUID) (*models.Message, error) {
	return nil, app.Forbidden("message reads are outside retry authorization")
}
func (r *outboundRetryReader) FindSendIdentityForAddress(ctx context.Context, tenant uuid.UUID, address string) (*models.SendIdentity, error) {
	if tenant != r.tenant {
		return nil, nil
	}
	return findSendIdentityForAddress(ctx, r.tx, tenant, address, ` FOR SHARE NOWAIT`)
}
func (r *outboundRetryReader) TemplateForSend(ctx context.Context, tenant uuid.UUID, user, key *uuid.UUID, mailbox, version uuid.UUID) (*company.TemplateVersion, string, string, error) {
	if tenant != r.tenant || user == nil {
		return nil, "", "", authz.ErrForbidden("template sender unavailable")
	}
	// Parent/version share protection fences retirement/revocation. Protect the
	// exact granted row too: a direct delete cannot invalidate an authorization
	// already accepted by this transaction. Missing grants still fail closed.
	var id uuid.UUID
	e := r.tx.QueryRow(ctx, `SELECT v.id FROM mail_template_versions v JOIN mail_templates t ON t.id=v.template_id AND t.tenant_id=v.tenant_id WHERE v.id=$1 AND v.tenant_id=$2 FOR SHARE OF t,v NOWAIT`, version, tenant).Scan(&id)
	if errors.Is(e, pgx.ErrNoRows) {
		return nil, "", "", authz.ErrForbidden("published template unavailable")
	}
	if e != nil {
		return nil, "", "", e
	}
	var granted uuid.UUID
	e = r.tx.QueryRow(ctx, `SELECT g.template_id FROM mail_template_grants g JOIN mail_template_versions v ON v.template_id=g.template_id AND v.tenant_id=g.tenant_id WHERE v.id=$1 AND g.tenant_id=$2 AND g.user_id=$3 AND g.mailbox_id=$4 FOR SHARE OF g NOWAIT`, version, tenant, *user, mailbox).Scan(&granted)
	if errors.Is(e, pgx.ErrNoRows) {
		return nil, "", "", authz.ErrForbidden("published template unavailable or not granted")
	}
	if e != nil {
		return nil, "", "", e
	}
	a, e := currentMemberActor(ctx, r.tx, authz.Actor{Type: authz.PrincipalUser, ID: *user, TenantID: tenant}, tenant)
	if e != nil {
		return nil, "", "", e
	}
	if !a.IsTenantAdmin() {
		a.Permission, e = r.EffectivePermission(ctx, *user)
		if e != nil {
			return nil, "", "", e
		}
	}
	var out company.TemplateVersion
	var employee, name string
	e = r.store.loadTemplateForSendTx(ctx, r.tx, a, tenant, user, key, mailbox, version, &out, &employee, &name)
	if e != nil {
		return nil, "", "", e
	}
	return &out, employee, name, nil
}
