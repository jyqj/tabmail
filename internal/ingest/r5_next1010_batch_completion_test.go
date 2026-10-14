package ingest

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"tabmail/internal/config"
	"tabmail/internal/models"
	"tabmail/internal/policy"
	"tabmail/internal/resolver"
	"tabmail/internal/store"
)

type next1010IngestMarkFailure struct{ *next1010IngestBatchStore }

func (*next1010IngestMarkFailure) FinishIngress(context.Context, *store.IngressClaim, int, time.Time) error {
	return errors.New("synthetic receipt completion unavailable")
}

func TestNext1010IngestBatchCompletionFailure(t *testing.T) {
	st, original := next1010IngestBatchFixture(t, 3, 3)
	failing := &next1010IngestMarkFailure{st}
	svc := NewService(failing, original.obj, resolver.New(failing, policy.NamingFull, true), nil, nil,
		models.SMTPPolicy{DefaultAccept: true, DefaultStore: true}, 24, nil,
		config.Ingest{Durable: true, BatchSize: 3, PollInterval: time.Hour, MaxRetries: 5}, zerolog.Nop())
	svc.ProcessBatch(context.Background())
	trace := st.snapshot()
	states := next1010IngestStates(t, st, 3)
	if !reflect.DeepEqual(trace, []string{"claim", "deliver"}) || states["processing"] != 1 || states["pending"] != 2 {
		t.Errorf("completion failure advanced the batch: trace=%v states=%v", trace, states)
	}
}

func TestNext1010IngestBatchSuccessThenFailure(t *testing.T) {
	st, svc := next1010IngestBatchFixture(t, 3, 3)
	calls := 0
	st.deliver = func(context.Context) error {
		calls++
		if calls == 2 {
			return errors.New("second delivery transaction temporarily failed")
		}
		return nil
	}
	svc.ProcessBatch(context.Background())
	trace := st.snapshot()
	states := next1010IngestStates(t, st, 3)
	if calls != 2 || !reflect.DeepEqual(trace, []string{"claim", "deliver", "finish", "claim", "deliver", "finish"}) || states["done"] != 1 || states["retry"] != 1 || states["pending"] != 1 {
		t.Errorf("poll failed to stop after its first failure: calls=%d trace=%v states=%v", calls, trace, states)
	}
}
