package outbound

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"tabmail/internal/models"
)

// A very short configured retry delay exposes a same-poll retry, without
// sleeping or moving any queue timestamp. Preserve the old one-claim worker's
// boundary: failure ends this poll even though successful jobs may now continue.
func TestNext1010OutboundBatchPreservesFailurePollBoundary(t *testing.T) {
	st, svc, jobs, adapter := next1010OutboundBatchFixture(t, 3, 3)
	svc.cfg.RetryDelay = time.Nanosecond
	calls := 0
	adapter.deliver = func(context.Context) error {
		calls++
		if calls == 1 {
			return errors.New("temporary delivery failure")
		}
		return nil
	}
	svc.ensureWorker().ProcessBatch(context.Background())
	_, trace := st.snapshot()
	states := next1010OutboundStates(t, st, jobs)
	if calls != 1 || !reflect.DeepEqual(trace, []string{"claim", "deliver"}) || states[models.OutboundRetry] != 1 || states[models.OutboundPending] != 2 {
		t.Errorf("one failed poll was retried or advanced: calls=%d trace=%v states=%v", calls, trace, states)
	}
}
