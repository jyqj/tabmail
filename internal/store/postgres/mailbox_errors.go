package postgres

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
	"tabmail/internal/app"
)

// Only the address constraint on this INSERT describes a user-resolvable
// provisioning conflict. Do not classify by localized diagnostic text or hide
// unrelated uniqueness, foreign-key, transport or audit failures.
func classifyWorkMailboxCreateError(err error) error {
	var pgError *pgconn.PgError
	if errors.As(err, &pgError) && pgError != nil && pgError.Code == "23505" && pgError.ConstraintName == "mailboxes_full_address_key" {
		return &app.Error{Kind: app.KindConflict, Message: "mailbox address already exists", Err: err}
	}
	return err
}
