package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"tabmail/internal/models"
)

// Exercise the actual HTTP pagination parser, authenticated handler and store
// pagination consumer. The in-memory fixture does not qualify PostgreSQL.
func TestSuppressionPaginationDoesNotWrapOversizedPages(t *testing.T) {
	f := newOutboundAccessFixture(t)
	h := NewOutboundHandler(nil, f.st, zerolog.Nop())
	var ids []uuid.UUID
	for i := range 3 {
		id := uuid.New()
		ids = append(ids, id)
		if err := f.st.AddSuppression(context.Background(), &models.SuppressionEntry{
			ID: id, TenantID: f.tenantID, Address: fmt.Sprintf("blocked-%d@fixture.test", i),
			Reason: "hard_bounce", CreatedAt: time.Unix(100-int64(i), 0),
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.st.AddSuppression(context.Background(), &models.SuppressionEntry{
		ID: uuid.New(), TenantID: f.otherTenantID, Address: "foreign@fixture.test",
		Reason: "hard_bounce", CreatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name               string
		page, size         int
		wantPage, wantSize int
		wantIDs            []uuid.UUID
	}{
		{"first", 1, 2, 1, 2, ids[:2]},
		{"second", 2, 2, 2, 2, ids[2:]},
		{"ordinary_empty", 3, 2, 3, 2, nil},
		{"defaults", 0, 0, 1, 30, ids},
		{"negative_page", math.MinInt, 2, 1, 2, ids[:2]},
		{"size_limit", 1, math.MaxInt, 1, 100, ids},
		{"first_overflow", math.MaxInt/100 + 2, 100, math.MaxInt/100 + 2, 100, nil},
		{"largest_page", math.MaxInt, 100, math.MaxInt, 100, nil},
		{"zero_wrap", math.MaxInt/2 + 2, 4, math.MaxInt/2 + 2, 4, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Record a baseline panic as this case's failure so other frozen
			// paging controls still execute; the candidate must never panic.
			defer func() {
				if cause := recover(); cause != nil {
					t.Fatalf("paginated HTTP request panicked: %v", cause)
				}
			}()
			path := fmt.Sprintf("/api/v1/suppression?page=%d&per_page=%d", tc.page, tc.size)
			rr := doOutboundHandlerRequest(t, f.st, h.ListSuppressions, http.MethodGet, path, nil, outboundUserHeaders(t, f.tenantAdmin))
			if rr.Code != http.StatusOK {
				t.Fatalf("HTTP %d: %s", rr.Code, rr.Body.String())
			}
			var result struct {
				Data []models.SuppressionEntry `json:"data"`
				Meta meta                      `json:"meta"`
			}
			if err := json.Unmarshal(rr.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if result.Meta != (meta{Total: 3, Page: tc.wantPage, PerPage: tc.wantSize}) {
				t.Fatalf("pagination metadata changed: %+v", result.Meta)
			}
			var got []uuid.UUID
			for _, row := range result.Data {
				if row.TenantID != f.tenantID {
					t.Fatal("foreign tenant escaped list scope")
				}
				got = append(got, row.ID)
			}
			if !reflect.DeepEqual(got, tc.wantIDs) {
				t.Fatalf("page returned IDs %v, want %v", got, tc.wantIDs)
			}
		})
	}
}
