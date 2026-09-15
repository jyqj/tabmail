package postgres

import (
	"strings"
	"testing"
)

func TestP0BaselineDoesNotDropHistoricalGrants(t *testing.T) {
	b, err := migrationsFS.ReadFile("migrations/00001_baseline.sql")
	if err != nil {
		t.Fatal(err)
	}
	up := strings.Split(string(b), "-- +goose Down")[0]
	for _, table := range []string{"send_as_grants", "mailbox_grants", "zone_grants"} {
		if strings.Contains(up, "DROP TABLE IF EXISTS "+table) {
			t.Errorf("baseline destroys historical %s", table)
		}
	}
}
