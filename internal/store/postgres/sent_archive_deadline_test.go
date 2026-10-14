package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestSentItemAvailableAt(t *testing.T) {
	now := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	past, future := now.Add(-time.Microsecond), now.Add(time.Microsecond)
	for _, tc := range []struct {
		name                    string
		expires, purge, mailbox *time.Time
		want                    bool
	}{
		{"permanent", nil, nil, nil, true},
		{"future", &future, &future, &future, true},
		{"item-past", &past, nil, nil, false},
		{"item-equal", &now, nil, nil, false},
		{"purge-past", nil, &past, nil, false},
		{"purge-equal", nil, &now, nil, false},
		{"mailbox-past", nil, nil, &past, false},
		{"mailbox-equal", nil, nil, &now, false},
		{"one-expired", &future, &past, &future, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := sentItemAvailableAt(now, tc.expires, tc.purge, tc.mailbox); got != tc.want {
				t.Fatalf("got %t want %t", got, tc.want)
			}
		})
	}
}

type sentClockTx struct {
	pgx.Tx // Other transaction operations must not be invoked by this guard.
	t      *testing.T
	now    time.Time
	err    error
}

func (x sentClockTx) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	if sql != "SELECT clock_timestamp()" || len(args) != 0 {
		x.t.Fatal("deadline guard did not query the current database clock")
	}
	return sentClockRow{x.now, x.err}
}

type sentClockRow struct {
	now time.Time
	err error
}

func (r sentClockRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	*dest[0].(*time.Time) = r.now
	return nil
}
func TestSentItemDeadlineUsesDatabaseClockAndPropagatesFailure(t *testing.T) {
	now := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	until := now.Add(time.Minute)
	if err := checkSentItemDeadline(context.Background(), sentClockTx{t: t, now: now}, &until, nil, nil); err != nil {
		t.Fatal("substituted host clock for database clock", err)
	}
	failure := errors.New("synthetic clock query failure")
	if err := checkSentItemDeadline(context.Background(), sentClockTx{t: t, err: failure}, nil, nil, nil); !errors.Is(err, failure) {
		t.Fatalf("database failure was swallowed: %v", err)
	}
}
