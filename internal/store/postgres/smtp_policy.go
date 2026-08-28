package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"tabmail/internal/models"
)

func (s *PgStore) GetSMTPPolicy(ctx context.Context) (*models.SMTPPolicy, error) {
	p := &models.SMTPPolicy{}
	err := s.db(ctx).QueryRow(ctx, `
		SELECT default_accept,accept_domains,reject_domains,default_store,store_domains,discard_domains,reject_origin_domains,updated_at
		FROM smtp_policies WHERE id=TRUE`).
		Scan(&p.DefaultAccept, &p.AcceptDomains, &p.RejectDomains, &p.DefaultStore, &p.StoreDomains, &p.DiscardDomains, &p.RejectOriginDomains, &p.UpdatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return p, err
}

func (s *PgStore) UpsertSMTPPolicy(ctx context.Context, p *models.SMTPPolicy) error {
	p.UpdatedAt = time.Now().UTC()
	accept := nonNil(p.AcceptDomains)
	reject := nonNil(p.RejectDomains)
	store := nonNil(p.StoreDomains)
	discard := nonNil(p.DiscardDomains)
	rejectOrigin := nonNil(p.RejectOriginDomains)
	_, err := s.db(ctx).Exec(ctx, `
		INSERT INTO smtp_policies (id,default_accept,accept_domains,reject_domains,default_store,store_domains,discard_domains,reject_origin_domains,updated_at)
		VALUES (TRUE,$1,$2,$3,$4,$5,$6,$7,$8)
		ON CONFLICT (id) DO UPDATE SET
			default_accept=EXCLUDED.default_accept,
			accept_domains=EXCLUDED.accept_domains,
			reject_domains=EXCLUDED.reject_domains,
			default_store=EXCLUDED.default_store,
			store_domains=EXCLUDED.store_domains,
			discard_domains=EXCLUDED.discard_domains,
			reject_origin_domains=EXCLUDED.reject_origin_domains,
			updated_at=EXCLUDED.updated_at`,
		p.DefaultAccept, accept, reject, p.DefaultStore, store, discard, rejectOrigin, p.UpdatedAt)
	return err
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
