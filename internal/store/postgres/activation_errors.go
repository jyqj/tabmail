package postgres

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
	"tabmail/internal/app"
)

// Classify only the INSERT that owns this address constraint. An unrelated
// uniqueness or later mandatory audit failure must retain its original cause
// and must not tell an employee to change an otherwise available address.
func classifyActivationInsertError(err error, addressConstraint, duplicateMessage string) error {
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || pg == nil {
		return err
	}
	if pg.Code == "23505" && pg.ConstraintName == addressConstraint {
		return &app.Error{Kind: app.KindConflict, Message: duplicateMessage, Err: err}
	}
	if pg.Code == "40001" || pg.Code == "55P03" {
		return &app.Error{Kind: app.KindConflict, Message: "activation resources changed; try again", Err: err}
	}
	return err
}
