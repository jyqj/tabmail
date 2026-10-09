package postgres

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/puddle/v2"
	"tabmail/internal/app"
	"tabmail/internal/authz"
	"tabmail/internal/company"
)

// A closed real pool makes entry into the SQL boundary observable without a
// server: valid names reach ErrClosedPool, invalid names must fail first. This
// is a validation-order test, not evidence of a PostgreSQL transaction run.
func TestR5TemplateNULNameBeforeSQL(t *testing.T) {
	pool, err := pgxpool.New(t.Context(), "postgres://fixture@127.0.0.1:1/unused?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	pool.Close()
	s := &PgStore{pool: pool}
	for _, mode := range []string{"create", "update"} {
		for _, tc := range []struct {
			name, value string
			invalid     bool
		}{
			{"nul_middle", "before\x00after", true},
			{"nul_end", "before\x00", true},
			{"blank", " \t\r\n ", true},
			{"oversized", strings.Repeat("a", 121), true},
			{"ascii", "  valid name  ", false},
			{"unicode", "中文😀", false},
		} {
			t.Run(mode+"/"+tc.name, func(t *testing.T) {
				v := company.Template{Name: tc.value, Draft: company.TemplateDraft{Subject: "Subject", TextBody: "Body"}}
				if mode == "update" {
					v.ID, v.Revision = uuid.New(), 3
				}
				_, err := s.SaveMailTemplate(t.Context(), authz.Actor{}, v)
				if tc.invalid {
					e, ok := app.As(err)
					if !ok || e.Kind != app.KindBadRequest {
						t.Fatalf("invalid template name reached storage: %v", err)
					}
				} else if !errors.Is(err, puddle.ErrClosedPool) {
					t.Fatalf("legal name did not reach the SQL boundary: %v", err)
				}
			})
		}
	}
}
