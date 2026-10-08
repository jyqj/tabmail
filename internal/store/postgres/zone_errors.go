package postgres

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
	"tabmail/internal/store"
)

// classifyZoneCreateError maps only the domain-name constraint, not unrelated
// unique indexes or error text. Preserve the complete adapter cause chain.
func classifyZoneCreateError(err error) error {
	var pgError *pgconn.PgError
	if errors.As(err, &pgError) && pgError != nil && pgError.Code == "23505" && pgError.ConstraintName == "domain_zones_domain_key" {
		return errors.Join(store.ErrDomainAlreadyExists, err)
	}
	return err
}
