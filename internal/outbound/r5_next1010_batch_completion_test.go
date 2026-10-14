package outbound

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/models"
)

type next1010OutboundMarkFailure struct{ *next1010OutboundBatchStore }

func (*next1010OutboundMarkFailure) MarkOutboundJobSent(context.Context, uuid.UUID, *uuid.UUID, int, string, string) error {
	return errors.New("synthetic job completion unavailable")
}

func TestNext1010OutboundBatchCompletionFailure(t *testing.T) {
	st, original, jobs, adapter := next1010OutboundBatchFixture(t, 3, 3)
	svc := NewService(original.cfg, &next1010OutboundMarkFailure{st}, nopGovernance{}, zerolog.Nop())
	svc.adapter = adapter
	svc.ensureWorker().ProcessBatch(context.Background())
	_, trace := st.snapshot()
	states := next1010OutboundStates(t, st, jobs)
	if !reflect.DeepEqual(trace, []string{"claim", "deliver"}) || states[models.OutboundRetry] != 1 || states[models.OutboundPending] != 2 {
		t.Errorf("completion failure advanced the batch: trace=%v states=%v", trace, states)
	}
}

func TestNext1010OutboundBatchSuccessThenFailure(t *testing.T) {
	st, svc, jobs, adapter := next1010OutboundBatchFixture(t, 3, 3)
	calls := 0
	adapter.deliver = func(context.Context) error {
		calls++
		if calls == 2 {
			return errors.New("second delivery temporarily failed")
		}
		return nil
	}
	svc.ensureWorker().ProcessBatch(context.Background())
	_, trace := st.snapshot()
	states := next1010OutboundStates(t, st, jobs)
	if calls != 2 || !reflect.DeepEqual(trace, []string{"claim", "deliver", "sent", "claim", "deliver"}) || states[models.OutboundSent] != 1 || states[models.OutboundRetry] != 1 || states[models.OutboundPending] != 1 {
		t.Errorf("poll failed to stop after its first failure: calls=%d trace=%v states=%v", calls, trace, states)
	}
}
