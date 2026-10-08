package testutil

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"tabmail/internal/models"
	"tabmail/internal/store"
	"testing"
)

func TestContinueDomainConflictFakeStore(t *testing.T) {
	for _, name := range []string{"different id same domain", "different tenant same domain", "same id seed overwrite", "different domain"} {
		t.Run(name, func(t *testing.T) {
			st := NewFakeStore()
			old := &models.DomainZone{ID: uuid.New(), TenantID: uuid.New(), Domain: "created.example"}
			if e := st.CreateZone(context.Background(), old); e != nil {
				t.Fatal(e)
			}
			candidate := *old
			candidate.ID = uuid.New()
			candidate.IsVerified = true
			wantConflict := true
			switch name {
			case "different tenant same domain":
				candidate.TenantID = uuid.New()
			case "same id seed overwrite":
				candidate.ID = old.ID
				wantConflict = false
			case "different domain":
				candidate.Domain = "other.example"
				wantConflict = false
			}
			var err error
			if name == "same id seed overwrite" {
				st.SeedZone(&candidate)
			} else {
				err = st.CreateZone(context.Background(), &candidate)
			}
			if errors.Is(err, store.ErrDomainAlreadyExists) != wantConflict {
				t.Errorf("error=%v want conflict=%v", err, wantConflict)
			}
			all, e := st.ListAllZones(context.Background())
			if e != nil {
				t.Fatal(e)
			}
			wantCount := 1
			if name == "different domain" {
				wantCount = 2
			}
			if len(all) != wantCount {
				t.Errorf("zones=%d want=%d", len(all), wantCount)
			}
			got, e := st.GetZone(context.Background(), old.ID)
			if e != nil || got == nil {
				t.Fatalf("original disappeared: %v %v", got, e)
			}
			wantVerified := name == "same id seed overwrite"
			if got.IsVerified != wantVerified {
				t.Errorf("existing row changed unexpectedly: %+v", got)
			}
		})
	}
}
