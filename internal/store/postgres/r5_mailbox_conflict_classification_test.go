package postgres

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"tabmail/internal/app"
)

func TestR5MailboxConflictClassification(t *testing.T) {
	for _, name := range []string{"nil", "address", "localized", "wrapped", "joined", "primary_key", "unrelated_unique", "missing_constraint", "foreign_key", "text_only", "canceled", "deadline", "ordinary"} {
		t.Run(name, func(t *testing.T) {
			var original error
			want := false
			switch name {
			case "nil":
			case "text_only":
				original = errors.New("duplicate unique mailboxes_full_address_key 23505")
			case "canceled":
				original = fmt.Errorf("duplicate: %w", context.Canceled)
			case "deadline":
				original = fmt.Errorf("unique: %w", context.DeadlineExceeded)
			case "ordinary":
				original = errors.New("connection unavailable")
			default:
				pg := &pgconn.PgError{Code: "23505", ConstraintName: "mailboxes_full_address_key", Message: "opaque storage diagnostic"}
				original, want = pg, true
				switch name {
				case "localized":
					pg.Message = "邮箱已存在"
				case "wrapped":
					original = fmt.Errorf("outer: %w", pg)
				case "joined":
					original = errors.Join(errors.New("annotation"), pg)
				case "primary_key":
					pg.ConstraintName = "mailboxes_pkey"
					want = false
				case "unrelated_unique":
					pg.ConstraintName = "other_unique"
					want = false
				case "missing_constraint":
					pg.ConstraintName = ""
					want = false
				case "foreign_key":
					pg.Code = "23503"
					want = false
				}
			}
			got := classifyWorkMailboxCreateError(original)
			var ae *app.Error
			conflict := errors.As(got, &ae) && ae.Kind == app.KindConflict
			if conflict != want {
				t.Errorf("classification=%v want conflict=%v", got, want)
			}
			if original == nil {
				if got != nil {
					t.Errorf("nil changed: %v", got)
				}
				return
			}
			if !errors.Is(got, original) {
				t.Error("original cause lost")
			}
			if !want && got != original {
				t.Error("unrelated error rewritten")
			}
		})
	}
}
