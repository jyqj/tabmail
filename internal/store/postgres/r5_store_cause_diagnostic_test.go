//go:build r5benchmark

package postgres

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
	"time"
)

// Entry is a fresh 1000/5000/4000 diagnosis, not original M/1M admission or an
// automatic retry. The unchanged consumer indexes every new message through
// real Claim20/Parser4/Complete and keeps durable pending/active at most20.
func TestR5MStoreCauseDiagnostic(t *testing.T) { r5RunStoreDiagnosticCause(t, true, true) }

func (d *r5StoreDiagnostic) ObserveFileOperation(ctx context.Context, stage string, start, end time.Time, failed bool) {
	var e error
	if failed {
		e = errors.New("observed filesystem operation failed")
	}
	d.recordInterval(ctx, "fs/"+stage, start, end, e)
}
func r5CauseValidate(spans map[string]*r5DiagnosticSpan) error {
	base := map[string]*r5DiagnosticSpan{}
	for k, v := range spans {
		if !strings.HasPrefix(k, "fs/") {
			base[k] = v
		}
	}
	if e := r5DiagnosticValidate(base); e != nil {
		return e
	}
	expected := map[string]int{"fs/copy_and_declared_length_validation": 4000, "fs/file_sync": 4000, "fs/file_close": 4000, "fs/rename": 4000, "fs/dir_sync_leaf": 4000, "fs/dir_sync_parent": 4000, "fs/dir_sync_root": 4000, "fs/dir_close": 12000}
	if len(spans) != len(base)+len(expected) {
		return errors.New("unknown/missing exact FS stage")
	}
	for k, n := range expected {
		v := spans[k]
		if v == nil || v.Calls != n || v.Errors != 0 || v.ElapsedSum <= 0 || math.IsNaN(v.ElapsedSum) || math.IsInf(v.ElapsedSum, 0) || math.IsNaN(v.MinMS) || math.IsInf(v.MinMS, 0) || math.IsNaN(v.MaxMS) || math.IsInf(v.MaxMS, 0) || v.MinMS < 0 || v.MaxMS < v.MinMS || math.IsNaN(v.MaxGapMS) || math.IsInf(v.MaxGapMS, 0) || v.MaxGapMS < 0 || v.MaxGapMS > 100 {
			return errors.New("unknown/incomplete FS cause evidence: " + k)
		}
	}
	return nil
}
func TestR5MCauseExactInterval(t *testing.T) {
	d := newR5StoreDiagnostic(time.Now(), "", "", nil)
	d.causeEnabled = true
	ctx := d.scopeForPureCause(context.Background())
	start := time.Unix(1700000000, 0)
	d.ObserveFileOperation(ctx, "file_sync", start, start.Add(2*time.Millisecond), false)
	d.ObserveFileOperation(ctx, "file_sync", start, start.Add(3*time.Millisecond), true)
	v := d.spans["fs/file_sync"]
	if v == nil || v.Calls != 2 || v.Errors != 1 || v.ElapsedSum != 0.005 || v.MaxMS != 3 || v.MinMS != 2 || d.observerNanos.Load() < 0 {
		t.Fatal("operation interval/failure/overhead observation changed")
	}
	if r5CauseValidate(d.spans) == nil {
		t.Fatal("partial FS/SQL cause evidence accepted")
	}
}
func (d *r5StoreDiagnostic) scopeForPureCause(ctx context.Context) context.Context {
	return context.WithValue(ctx, r5DiagnosticScopeKey{}, r5DiagnosticScope{ordinal: 0, size: 4096, mailbox: "opaque"})
}

func (d *r5StoreDiagnostic) preserveCauseFailure() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return r5DiagnosticJSON(d.dir+"/cause-failure.json", map[string]any{"mode": "DIAGNOSTIC_M_SHAPED_STORE_CAUSE_01_NOT_M_BASELINE", "status": "failed_partial_observations_not_admission", "source_sha": d.source, "population": []int{1000, 5000, 4000}, "call_spans_partial": d.spans, "preparation_partial_not_final_SQL_ready": d.state.preparation, "sampler_queries": d.samplerQueries, "store_state_samples_partial": d.waits, "failure_cause_count": len(d.failures), "execution_original_budget_seconds": 180, "outer_original_budget_seconds": 300, "profile_artifacts_retained_in_private_output": true, "business_bodies_credentials_paths_not_archived": true, "task_complete": false, "product_green": false})
}
