package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"tabmail/internal/app"
	"tabmail/internal/authz"
	"tabmail/internal/models"
	"tabmail/internal/store"
)

var _ store.OutboundReceiptReader = (*PgStore)(nil)

func (s *PgStore) GetOutboundReceipt(ctx context.Context, a authz.Actor, id uuid.UUID, scope string) (*store.OutboundReceipt, error) {
	if scope != "send:read" && scope != "send:write" {
		return nil, app.Forbidden("invalid outbound receipt scope")
	}
	var out []store.OutboundReceipt
	err := s.outboundPrincipalTx(ctx, a, []string{scope}, func(tx pgx.Tx, current authz.Actor, key *models.TenantAPIKey) error {
		var e error
		out, _, e = readOutboundReceipts(ctx, tx, a, current, key, &id, models.Page{Page: 1, PerPage: 1})
		return e
	})
	if err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, nil
	}
	return &out[0], nil
}

func (s *PgStore) ListOutboundReceipts(ctx context.Context, a authz.Actor, page models.Page) ([]store.OutboundReceipt, int, error) {
	var out []store.OutboundReceipt
	var total int
	err := s.outboundPrincipalTx(ctx, a, []string{"send:read"}, func(tx pgx.Tx, current authz.Actor, key *models.TenantAPIKey) error {
		var e error
		out, total, e = readOutboundReceipts(ctx, tx, a, current, key, nil, page.Normalize())
		return e
	})
	if err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

// The count, selected IDs, content decision and selected rows are ONE SQL
// statement snapshot. Materialize only visibility metadata, not every body.
// This also gives beyond-end pages a real total and a stable (time,id) order.
func readOutboundReceipts(ctx context.Context, tx pgx.Tx, observed, current authz.Actor, key *models.TenantAPIKey, id *uuid.UUID, page models.Page) ([]store.OutboundReceipt, int, error) {
	contentActor := current
	if current.Type == authz.PrincipalAPIKey && current.OwnerUserID != nil {
		contentActor = authz.Actor{Type: authz.PrincipalUser, ID: *current.OwnerUserID, TenantID: current.TenantID, Permission: current.Permission}
	}
	contentWhere, args := submissionContentScope(contentActor, 1)
	bind := func(v any) string { args = append(args, v); return fmt.Sprintf("$%d", len(args)) }
	filter := []string{"j.tenant_id=$1"}
	if id != nil {
		filter = append(filter, "j.id="+bind(*id))
	}
	// Current permissions and the original credential envelope can only narrow
	// one another; an old request must not gain newly expanded credential scope.
	for _, permission := range []*models.EffectivePermission{observed.Permission, current.Permission} {
		if permission != nil && len(permission.AllowedZoneIDs) > 0 {
			filter = append(filter, "j.zone_id=ANY("+bind(permission.AllowedZoneIDs)+")")
		}
	}
	if key != nil && len(key.AllowedZoneIDs) > 0 {
		filter = append(filter, "j.zone_id=ANY("+bind(key.AllowedZoneIDs)+")")
	}
	owner := "FALSE"
	switch current.Type {
	case authz.PrincipalUser:
		if current.IsTenantAdmin() {
			owner = "TRUE"
		} else {
			owner = "j.user_id=" + bind(current.ID)
		}
	case authz.PrincipalAPIKey:
		owner = "j.api_key_id=" + bind(current.ID)
	}
	content := `EXISTS(SELECT 1` + sentContentFrom + ` WHERE ` + contentWhere + ` AND s.id=j.id AND s.tenant_id=j.tenant_id AND s.zone_id=j.zone_id AND s.sender_mailbox_id=j.sender_mailbox_id)`
	limit, offset := bind(page.PerPage), bind(page.Offset())
	sql := `WITH visible AS MATERIALIZED (
 SELECT j.id,j.created_at FROM outbound_jobs j WHERE ` + strings.Join(filter, " AND ") + ` AND (` + owner + ` OR ` + content + `)
 ), page AS (SELECT * FROM visible ORDER BY created_at DESC,id DESC LIMIT ` + limit + ` OFFSET ` + offset + `), totals AS (SELECT count(*) AS total FROM visible)
 SELECT j.*,` + content + ` AS content_allowed,totals.total FROM totals
 LEFT JOIN page p ON TRUE LEFT JOIN (` + outboundJobSelect + `) j ON j.id=p.id
 ORDER BY p.created_at DESC,p.id DESC`
	rows, err := tx.Query(ctx, sql, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []store.OutboundReceipt{}
	total := 0
	for rows.Next() {
		values, e := rows.Values()
		if e != nil {
			return nil, 0, e
		}
		if values[0] == nil {
			n, ok := values[len(values)-1].(int64)
			if !ok || int64(int(n)) != n {
				return nil, 0, fmt.Errorf("invalid outbound receipt total")
			}
			total = int(n)
			continue
		}
		contentAllowed := false
		job, e := scanOutboundJob(outboundReceiptRow{Row: rows, content: &contentAllowed, total: &total})
		if e != nil {
			return nil, 0, e
		}
		out = append(out, store.OutboundReceipt{Job: job, ContentAllowed: contentAllowed})
	}
	if err = rows.Err(); err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

type outboundReceiptRow struct {
	pgx.Row
	content *bool
	total   *int
}

func (r outboundReceiptRow) Scan(dest ...any) error {
	return r.Row.Scan(append(dest, r.content, r.total)...)
}
