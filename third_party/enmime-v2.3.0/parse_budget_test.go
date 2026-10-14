package enmime

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func forkLimits() ParseLimits {
	return ParseLimits{MaxDepth: 32, MaxNodes: 1024, MaxParts: 512, MaxBoundaryBytes: 4090}
}
func forkWide(valid int, lastHeader string) []byte {
	var raw strings.Builder
	raw.WriteString("Content-Type: multipart/mixed; boundary=B\r\n\r\n")
	for i := 0; i < valid; i++ {
		raw.WriteString("--B\r\nContent-Type: text/plain\r\n\r\nbody\r\n")
	}
	if lastHeader != "" {
		fmt.Fprintf(&raw, "--B\r\n%s\r\n\r\nlast\r\n", lastHeader)
	}
	raw.WriteString("--B--\r\n")
	return []byte(raw.String())
}
func forkNested(depth int) []byte {
	raw := "Content-Type: text/plain\r\n\r\nbody"
	for level := depth - 1; level > 0; level-- {
		boundary := fmt.Sprintf("N%d", level)
		raw = fmt.Sprintf("Content-Type: multipart/mixed; boundary=%s\r\n\r\n--%s\r\n%s\r\n--%s--\r\n", boundary, boundary, raw, boundary)
	}
	return []byte(raw)
}
func forkAncestorEOF(siblings int, attachment bool) []byte {
	var raw strings.Builder
	raw.WriteString("Content-Type: multipart/mixed; boundary=A\r\n\r\n--A\r\nContent-Type: multipart/mixed; boundary=B\r\n\r\n--B\r\nContent-Type: text/plain\r\n\r\nx--A--\r\nignored\r\n")
	for i := 0; i < siblings; i++ {
		raw.WriteString("--B\r\nContent-Type: text/plain\r\n")
		if attachment {
			raw.WriteString("Content-Disposition: attachment; filename=real.txt\r\n")
		}
		raw.WriteString("\r\nreal attachment\r\n")
	}
	raw.WriteString("--B--\r\n--A--\r\n")
	return []byte(raw.String())
}
func forkTreeNodes(env *Envelope) int {
	return len(env.Root.DepthMatchAll(func(*Part) bool { return true }))
}
func TestTabmailBudgetAncestorTemporaryEOF(t *testing.T) {
	for _, tc := range []struct {
		name       string
		siblings   int
		attachment bool
		wantNodes  int
		wantParts  int
		want       error
	}{
		{"small_review_counterexample", 1, true, 4, 1, nil},
		{"nodes_exact", 1021, false, 1024, 0, nil},
		{"nodes_plus_one", 1022, false, 1025, 0, ErrParseNodeLimit},
		{"parts_exact", 512, true, 515, 512, nil},
		{"parts_plus_one", 513, true, 516, 513, ErrParsePartLimit},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := forkAncestorEOF(tc.siblings, tc.attachment)
			original, err := ReadEnvelope(bytes.NewReader(raw))
			if err != nil {
				t.Fatal(err)
			}
			if forkTreeNodes(original) != tc.wantNodes || len(original.Attachments)+len(original.Inlines) != tc.wantParts {
				t.Fatalf("original nodes/parts %d/%d want %d/%d", forkTreeNodes(original), len(original.Attachments)+len(original.Inlines), tc.wantNodes, tc.wantParts)
			}
			bounded, err := ReadEnvelopeBounded(context.Background(), bytes.NewReader(raw), forkLimits())
			if !errors.Is(err, tc.want) {
				t.Fatalf("error %v want %v", err, tc.want)
			}
			if tc.want == nil {
				if !reflect.DeepEqual(bounded, original) {
					t.Fatal("real reader recovery changed")
				}
			} else if bounded != nil {
				t.Fatal("refused envelope exposed")
			}
		})
	}
}
func TestTabmailBudgetAllocationBeforeHeader(t *testing.T) {
	// Root + 1023 valid children exhaust 1024 allocations. Node 1025 has a
	// malformed first header: the node refusal must occur BEFORE setupHeaders
	// gets a chance to reject that header. inspect the same per-call guard.
	raw := forkWide(1023, "Subject broken no colon")
	if _, err := ReadEnvelope(bytes.NewReader(raw)); err == nil {
		t.Fatal("malformed last header precondition not met")
	}
	budget, err := newParseBudget(context.Background(), forkLimits())
	if err != nil {
		t.Fatal(err)
	}
	root, err := defaultParser.readParts(bytes.NewReader(raw), budget)
	if !errors.Is(err, ErrParseNodeLimit) || root != nil || budget.nodes != 1024 {
		t.Fatalf("before-header refusal err=%v root=%v nodes=%d", err, root, budget.nodes)
	}
	if !errors.Is(budget.err, ErrParseNodeLimit) {
		t.Fatal("refusal not sticky")
	}
}
func TestTabmailBudgetProjectionBeforeDecode(t *testing.T) {
	var raw strings.Builder
	raw.WriteString("Content-Type: multipart/mixed; boundary=B\r\n\r\n")
	for i := 0; i < 512; i++ {
		raw.WriteString("--B\r\nContent-Type: text/plain\r\nContent-Disposition: attachment\r\n\r\ncontent\r\n")
	}
	// One base64 digit is corrupt. The existing decoding error-policy hook
	// witnesses body decoding if reached; header prefetch alone cannot call it.
	raw.WriteString("--B\r\nContent-Type: text/plain\r\nContent-Disposition: attachment\r\nContent-Transfer-Encoding: base64\r\n\r\nZ\r\n--B--\r\n")
	decodeErrors := 0
	p := NewParser(SetReadPartErrorPolicy(func(*Part, error) bool { decodeErrors++; return false }))
	if _, err := p.ReadEnvelope(strings.NewReader(raw.String())); err != nil {
		t.Fatal(err)
	}
	if decodeErrors != 1 {
		t.Fatalf("offending body decode witness %d, want 1", decodeErrors)
	}
	decodeErrors = 0
	budget, err := newParseBudget(context.Background(), forkLimits())
	if err != nil {
		t.Fatal(err)
	}
	_, err = p.readParts(strings.NewReader(raw.String()), budget)
	if !errors.Is(err, ErrParsePartLimit) || budget.parts != 512 || budget.nodes != 514 || decodeErrors != 0 {
		t.Fatalf("before-decode projection event err=%v parts=%d nodes=%d offending-decodes=%d", err, budget.parts, budget.nodes, decodeErrors)
	}
}
func TestTabmailBudgetDepthAndContextBeforeAllocation(t *testing.T) {
	for _, depth := range []int{32, 33} {
		t.Run(fmt.Sprint(depth), func(t *testing.T) {
			budget, err := newParseBudget(context.Background(), forkLimits())
			if err != nil {
				t.Fatal(err)
			}
			root, err := defaultParser.readParts(bytes.NewReader(forkNested(depth)), budget)
			if depth == 32 {
				if err != nil || root == nil || budget.nodes != 32 {
					t.Fatalf("exact depth %v nodes %d", err, budget.nodes)
				}
			} else if !errors.Is(err, ErrParseDepthLimit) || budget.nodes != 32 {
				t.Fatalf("depth +1 allocated: %v nodes %d", err, budget.nodes)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	budget, err := newParseBudget(ctx, forkLimits())
	if err != nil {
		t.Fatal(err)
	}
	if root, err := defaultParser.readParts(nil, budget); !errors.Is(err, context.Canceled) || root != nil || budget.nodes != 0 {
		t.Fatalf("canceled root allocated/read: %v nodes %d", err, budget.nodes)
	}
}
func TestTabmailBudgetMalformedRecoveryCannotSwallowRefusal(t *testing.T) {
	p := NewParser(SkipMalformedParts(true))
	limits := forkLimits()
	limits.MaxNodes = 3
	budget, err := newParseBudget(context.Background(), limits)
	if err != nil {
		t.Fatal(err)
	}
	root, err := p.readParts(bytes.NewReader(forkAncestorEOF(2, false)), budget)
	if !errors.Is(err, ErrParseNodeLimit) || root != nil || budget.nodes != 3 {
		t.Fatalf("recovery swallowed budget: %v nodes %d", err, budget.nodes)
	}
}
func TestTabmailBudgetLegacyNilGuardAndBoundedAgreement(t *testing.T) {
	var guard *parseBudget
	if guard.admitNode(0) != nil || guard.admitProjection(nil, false) != nil || guard.admitBoundary(strings.Repeat("x", 5000)) != nil {
		t.Fatal("nil guard is not a no-op")
	}
	for _, raw := range [][]byte{[]byte("Subject: plain\r\n\r\nbody"), forkWide(3, ""), forkNested(8), forkAncestorEOF(1, true)} {
		original, err := ReadEnvelope(bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		bounded, err := ReadEnvelopeBounded(context.Background(), bytes.NewReader(raw), forkLimits())
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(original, bounded) {
			t.Fatal("legacy/default pipeline changed")
		}
	}
}

// Each first Read occurs after ReadEnvelopeBounded has created its private
// budget, admitted/allocated the root, and kept that budget on its live stack.
// Block both first reads until the test observes two simultaneously-live calls.
type tabmailBudgetRendezvousReader struct {
	reader  io.Reader
	ctx     context.Context
	first   bool
	id      int
	entered chan<- int
	release <-chan struct{}
	active  *atomic.Int32
}

func (r *tabmailBudgetRendezvousReader) Read(buf []byte) (int, error) {
	if !r.first {
		r.first = true
		r.active.Add(1)
		r.entered <- r.id
		select {
		case <-r.ctx.Done():
			r.active.Add(-1)
			return 0, r.ctx.Err()
		case <-r.release:
			r.active.Add(-1)
		}
	}
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(buf)
}

func TestTabmailBudgetPerCallIsolation(t *testing.T) {
	for _, mode := range []string{"refusal_and_success", "cancellation_and_success"} {
		t.Run(mode, func(t *testing.T) {
			// Two parser calls maximum, sequential cases, cooperative readers,
			// a real deadline, and a cleanup deadline even when an assertion fails.
			deadline := time.Now().Add(5 * time.Second)
			if outer, ok := t.Deadline(); ok && outer.Add(-time.Second).Before(deadline) {
				deadline = outer.Add(-time.Second)
			}
			ctx, cancel := context.WithDeadline(context.Background(), deadline)
			failedCtx, cancelFailed := context.WithCancel(ctx)
			entered := make(chan int, 2)
			release := make(chan struct{})
			var releaseOnce sync.Once
			open := func() { releaseOnce.Do(func() { close(release) }) }
			var active, completed atomic.Int32
			done := make(chan struct{})
			started := false
			type result struct {
				id  int
				env *Envelope
				err error
			}
			results := make(chan result, 2)
			defer func() {
				cancelFailed()
				cancel()
				open()
				if !started {
					return
				}
				cleanup := time.NewTimer(time.Second)
				defer cleanup.Stop()
				select {
				case <-done:
				case <-cleanup.C:
					t.Error("bounded parser calls did not finish cleanup")
				}
			}()
			raw := forkWide(1, "")
			baseline, err := ReadEnvelope(bytes.NewReader(raw))
			if err != nil {
				t.Fatal(err)
			}
			started = true
			for id := 0; id < 2; id++ {
				callCtx := ctx
				limits := forkLimits()
				if id == 0 {
					callCtx = failedCtx
					if mode == "refusal_and_success" {
						limits.MaxNodes = 1
					}
				}
				r := &tabmailBudgetRendezvousReader{reader: bytes.NewReader(raw), ctx: callCtx, id: id, entered: entered, release: release, active: &active}
				go func(id int, callCtx context.Context, limits ParseLimits, r io.Reader) {
					defer func() {
						if completed.Add(1) == 2 {
							close(done)
						}
					}()
					env, err := ReadEnvelopeBounded(callCtx, r, limits)
					results <- result{id, env, err}
				}(id, callCtx, limits, r)
			}
			seen := map[int]bool{}
			for len(seen) < 2 {
				select {
				case id := <-entered:
					if seen[id] {
						t.Fatal("duplicate rendezvous entry")
					}
					seen[id] = true
				case <-ctx.Done():
					t.Fatal("two live budgets did not rendezvous before deadline")
				}
			}
			if active.Load() != 2 {
				t.Fatalf("overlap witness=%d, want 2", active.Load())
			}
			t.Log("overlap witness: two bounded calls/root budgets simultaneously waiting in first Read")
			got := map[int]result{}
			if mode == "cancellation_and_success" {
				cancelFailed()
				select {
				case r := <-results:
					if r.id != 0 || !errors.Is(r.err, context.Canceled) || r.env != nil {
						t.Fatalf("canceled call result: %+v", r)
					}
					got[r.id] = r
				case <-ctx.Done():
					t.Fatal("canceled call did not finish before releasing successful peer")
				}
				if active.Load() != 1 {
					t.Fatalf("successful peer not independently waiting: active=%d", active.Load())
				}
			}
			open()
			for len(got) < 2 {
				select {
				case r := <-results:
					if _, exists := got[r.id]; exists {
						t.Fatal("duplicate bounded result")
					}
					got[r.id] = r
				case <-ctx.Done():
					t.Fatal("bounded calls did not finish before deadline")
				}
			}
			want := ErrParseNodeLimit
			if mode == "cancellation_and_success" {
				want = context.Canceled
			}
			if !errors.Is(got[0].err, want) || got[0].env != nil {
				t.Fatalf("failed call result: %+v", got[0])
			}
			if got[1].err != nil || !reflect.DeepEqual(got[1].env, baseline) {
				t.Fatalf("independent successful peer changed: %v", got[1].err)
			}
		})
	}
}
