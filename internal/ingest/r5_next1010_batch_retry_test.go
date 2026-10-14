package ingest

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestNext1010IngestBatchPreservesFailurePollBoundary(t *testing.T) {
	st, svc := next1010IngestBatchFixture(t, 3, 3)
	calls := 0
	st.deliver = func(context.Context) error {
		calls++
		if calls == 1 {
			return errors.New("temporary delivery transaction failure")
		}
		return nil
	}
	svc.ProcessBatch(context.Background())
	trace := st.snapshot()
	states := next1010IngestStates(t, st, 3)
	if calls != 1 || !reflect.DeepEqual(trace, []string{"claim", "deliver", "finish"}) || states["retry"] != 1 || states["pending"] != 2 {
		t.Errorf("failed receipt did not end its poll: calls=%d trace=%v states=%v", calls, trace, states)
	}
}
