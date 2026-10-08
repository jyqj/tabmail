package models

import (
	"encoding/json"
	"math"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestR5RetentionExpiryExactFiniteHours(t *testing.T) {
	location, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name  string
		at    time.Time
		hours int
	}{
		{"ordinary", time.Date(2026, 10, 8, 12, 34, 56, 123456789, time.UTC), 24},
		{"duration-first-overflow", time.Date(2026, 10, 8, 12, 34, 56, 123456789, time.UTC), 2562048},
		{"three-million", time.Date(2026, 10, 8, 12, 34, 56, 123456789, time.UTC), 3000000},
		{"negative-three-million", time.Date(2026, 10, 8, 12, 34, 56, 123456789, time.UTC), -3000000},
		{"negative-hour", time.Date(2026, 10, 8, 12, 34, 56, 123456789, time.UTC), -1},
		{"spring-DST", time.Date(2026, 3, 7, 12, 34, 56, 123456789, location), 48},
		{"fall-DST", time.Date(2026, 11, 2, 12, 34, 56, 123456789, location), -48},
		{"lower-bound", time.Date(0, 1, 1, 1, 0, 0, 123456789, time.UTC), -1},
		{"upper-bound", time.Date(9999, 12, 31, 22, 59, 59, 999999999, time.UTC), 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := MessageExpiry(nil, tc.hours, tc.at)
			// Oracle uses epoch seconds, never a duration that could overflow in
			// the same way as the original implementation.
			want := time.Unix(tc.at.Unix()+int64(tc.hours)*3600, int64(tc.at.Nanosecond())).UTC()
			if err != nil || got == nil || !got.Equal(want) || got.Nanosecond() != tc.at.Nanosecond() {
				t.Fatalf("finite hours changed: got=%v err=%v want=%v", got, err, want)
			}
			encoded, err := json.Marshal(&Message{ExpiresAt: got})
			if err != nil {
				t.Fatalf("expiry cannot be returned by the existing message wire: %v", err)
			}
			var decoded Message
			if err := json.Unmarshal(encoded, &decoded); err != nil || decoded.ExpiresAt == nil || !decoded.ExpiresAt.Equal(*got) {
				t.Fatalf("JSON round trip failed: %s %v", encoded, err)
			}
			// Actual pgx binary codec only, not a PostgreSQL execution. Its
			// existing microsecond precision differs from the model's nanoseconds.
			codec := pgtype.NewMap()
			pg, err := codec.Encode(pgtype.TimestamptzOID, pgtype.BinaryFormatCode, *got, nil)
			if err != nil {
				t.Fatal(err)
			}
			var stored time.Time
			if err := codec.Scan(pgtype.TimestamptzOID, pgtype.BinaryFormatCode, pg, &stored); err != nil || !stored.Equal(got.Truncate(time.Microsecond)) {
				t.Fatalf("pgx finite codec failed: %v %v", stored, err)
			}
		})
	}
}

func TestR5RetentionExpiryRejectsUnrepresentableFiniteDates(t *testing.T) {
	at := time.Date(2026, 10, 8, 12, 34, 56, 999999999, time.UTC)
	for _, tc := range []struct {
		name  string
		at    time.Time
		hours int
	}{
		{"PG-INT-max", at, math.MaxInt32}, {"PG-INT-min", at, math.MinInt32},
		{"native-int-max", at, int(^uint(0) >> 1)}, {"native-int-min", at, -int(^uint(0)>>1) - 1},
		{"past-lower-bound", time.Date(0, 1, 1, 0, 59, 59, 999999999, time.UTC), -1},
		{"past-upper-bound", time.Date(9999, 12, 31, 23, 0, 0, 0, time.UTC), 1},
		{"invalid-origin-lower", time.Date(-1, 12, 31, 0, 0, 0, 0, time.UTC), 1},
		{"invalid-origin-upper", time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC), -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := MessageExpiry(nil, tc.hours, tc.at)
			if err == nil || got != nil {
				t.Fatalf("unsafe finite expiry accepted: hours=%d result=%v err=%v", tc.hours, got, err)
			}
		})
	}
}

func TestR5RetentionExpiryPermanentPolicies(t *testing.T) {
	owner := uuid.New()
	hours := math.MaxInt32
	for _, tc := range []struct {
		name  string
		mb    *Mailbox
		hours int
	}{
		{"zero", nil, 0}, {"owned", &Mailbox{OwnerUserID: &owner}, hours},
		{"owned-explicit", &Mailbox{OwnerUserID: &owner, RetentionHoursOverride: &hours}, hours},
		{"shared-inherited", &Mailbox{Kind: "shared"}, hours},
		{"shared-zero", &Mailbox{Kind: "shared", RetentionHoursOverride: new(int)}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := MessageExpiry(tc.mb, tc.hours, time.Now())
			if err != nil || got != nil {
				t.Fatalf("permanent policy changed: %v %v", got, err)
			}
		})
	}
	for _, h := range []int{3000000, -1} {
		t.Run("shared-finite-"+strconv.Itoa(h), func(t *testing.T) {
			now := time.Now()
			got, err := MessageExpiry(&Mailbox{Kind: "shared", RetentionHoursOverride: &h}, h, now)
			want := time.Unix(now.Unix()+int64(h)*3600, int64(now.Nanosecond()))
			if err != nil || got == nil || !got.Equal(want) {
				t.Fatalf("explicit shared policy changed: %v %v", got, err)
			}
		})
	}
}
