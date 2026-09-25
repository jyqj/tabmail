package postgres_test

import (
	"context"
	"github.com/google/uuid"
	"tabmail/internal/authz"
	"tabmail/internal/models"
	"tabmail/internal/store/postgres"
)

func grantCurrent(st *postgres.PgStore, ctx context.Context, a authz.Actor, g models.MailboxGrant) error {
	var revision int64 = 1
	if v, err := st.GetWorkMailbox(ctx, a, g.MailboxID); err == nil && v != nil {
		revision = v.Revision
	}
	return st.SetWorkGrant(ctx, a, g, revision)
}
func policyCurrent(st *postgres.PgStore, ctx context.Context, a authz.Actor, id uuid.UUID, policy *string) error {
	var revision int64 = 1
	if v, err := st.GetWorkMailbox(ctx, a, id); err == nil && v != nil {
		revision = v.Revision
	}
	return st.SetWorkMailboxSendPolicy(ctx, a, id, policy, revision)
}
