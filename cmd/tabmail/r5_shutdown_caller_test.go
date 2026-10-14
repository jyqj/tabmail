package main

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type r5CallerGate struct {
	ch   chan struct{}
	once sync.Once
}

func r5CallerNewGate(t *testing.T) *r5CallerGate {
	t.Helper()
	g := &r5CallerGate{ch: make(chan struct{})}
	t.Cleanup(g.release)
	return g
}
func (g *r5CallerGate) release() { g.once.Do(func() { close(g.ch) }) }
func r5CallerWait(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(3 * time.Second):
		t.Fatal("caller barrier did not complete")
	}
}
func r5CallerResult(t *testing.T, ch <-chan error) error {
	t.Helper()
	select {
	case err := <-ch:
		return err
	case <-time.After(3 * time.Second):
		t.Fatal("caller drain did not return")
		return nil
	}
}
func r5CallerCoordinator(t *testing.T, budget time.Duration) *shutdownCoordinator {
	t.Helper()
	c := newShutdownCoordinator(budget)
	t.Cleanup(c.release)
	return c
}
func r5CallerDrain(c *shutdownCoordinator) <-chan error {
	result := make(chan error, 1)
	go func() { result <- c.drain() }()
	return result
}
func r5CallerOwner(c *shutdownCoordinator, name string) *shutdownOwner {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, o := range c.owners {
		if o.name == name {
			return o
		}
	}
	return nil
}

func TestR5ShutdownCallerOneOriginAndSharedBudget(t *testing.T) {
	c := r5CallerCoordinator(t, 2*time.Second)
	contexts := make(chan context.Context, 3)
	for _, name := range []string{"http", "smtp", "outbound"} {
		c.goRun(name, func(ctx context.Context) { <-ctx.Done() }, func(ctx context.Context) error {
			contexts <- ctx
			return nil
		})
	}
	c.requestStop("first_server_error")
	original := c.stopCtx
	deadline, ok := original.Deadline()
	if !ok || original.Err() != nil || c.context().Err() == nil {
		t.Fatal("shutdown budget derived from cancelled run context or missing deadline")
	}
	c.requestStop("later_signal")
	if err := r5CallerResult(t, r5CallerDrain(c)); err != nil {
		t.Fatal(err)
	}
	if c.origin != "first_server_error" || c.stopCtx != original {
		t.Fatal("repeated stop replaced origin or budget")
	}
	for i := 0; i < 3; i++ {
		got := <-contexts
		d, ok := got.Deadline()
		if got != original || !ok || d != deadline {
			t.Fatal("component received a renewed shutdown budget")
		}
	}
}

func TestR5ShutdownCallerStopNilDoesNotJoinRun(t *testing.T) {
	c := r5CallerCoordinator(t, 2*time.Second)
	release := r5CallerNewGate(t)
	entered, stopReturned := make(chan struct{}), make(chan struct{})
	c.goRun("http", func(context.Context) { close(entered); <-release.ch }, func(context.Context) error {
		close(stopReturned)
		return nil
	})
	r5CallerWait(t, entered)
	result := r5CallerDrain(c)
	r5CallerWait(t, stopReturned)
	if c.dependenciesMayClose() {
		t.Fatal("stop nil authorized early dependency close")
	}
	select {
	case err := <-result:
		t.Fatalf("Run was not joined: %v", err)
	default:
	}
	release.release()
	if err := r5CallerResult(t, result); err != nil || !c.dependenciesMayClose() {
		t.Fatalf("actual completed join not admitted: %v", err)
	}
}

func TestR5ShutdownCallerTimeoutKeepsOwnerAndDependencies(t *testing.T) {
	c := r5CallerCoordinator(t, 20*time.Millisecond)
	release := r5CallerNewGate(t)
	entered := make(chan struct{})
	c.goRun("uncooperative-ingest", func(context.Context) { close(entered); <-release.ch }, nil)
	r5CallerWait(t, entered)
	err := r5CallerResult(t, r5CallerDrain(c))
	if !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "uncooperative-ingest") {
		t.Fatalf("missing named timeout: %v", err)
	}
	o := r5CallerOwner(c, "uncooperative-ingest")
	if shutdownClosed(o.bodyDone) || c.dependenciesMayClose() {
		t.Fatal("timeout detached owner or allowed dependency close")
	}
	if c.goRun("replacement", func(context.Context) {}, nil) {
		t.Fatal("shutdown admitted a replacement")
	}
	release.release()
	r5CallerWait(t, o.bodyDone)
	if next := c.drain(); next != err || c.dependenciesMayClose() {
		t.Fatal("failed process shutdown was relabelled graceful after the deadline")
	}
}

func TestR5ShutdownCallerPendingStartCannotProduceIdleJoin(t *testing.T) {
	c := r5CallerCoordinator(t, 2*time.Second)
	release := r5CallerNewGate(t)
	entered, startReturned, stopEntered := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var initialized atomic.Bool
	go func() {
		defer close(startReturned)
		c.start("outbound", func(ctx context.Context) {
			close(entered)
			<-release.ch
			// A delayed lazy Start receives the already-cancelled run context.
			if ctx.Err() == nil {
				t.Error("pending startup lost shutdown cancellation")
			}
			initialized.Store(true)
		}, func(context.Context) error {
			close(stopEntered)
			if !initialized.Load() {
				return errors.New("idle snapshot raced pending lazy initialization")
			}
			return nil
		})
	}()
	r5CallerWait(t, entered)
	result := r5CallerDrain(c)
	r5CallerWait(t, c.context().Done())
	select {
	case <-stopEntered:
		t.Fatal("stop snapshotted the worker before pending Start returned")
	default:
	}
	if c.dependenciesMayClose() {
		t.Fatal("pending startup was counted as drained")
	}
	release.release()
	r5CallerWait(t, startReturned)
	if err := r5CallerResult(t, result); err != nil {
		t.Fatal(err)
	}
	r5CallerWait(t, stopEntered)
}

func TestR5ShutdownCallerStartReturnIsNotWorkerJoin(t *testing.T) {
	c := r5CallerCoordinator(t, 20*time.Millisecond)
	release := r5CallerNewGate(t)
	entered := make(chan struct{})
	c.start("outbound", func(context.Context) {}, func(context.Context) error {
		close(entered)
		<-release.ch // Simulate a stop adapter that ignores its budget.
		return nil
	})
	result := r5CallerDrain(c)
	r5CallerWait(t, entered)
	err := r5CallerResult(t, result)
	o := r5CallerOwner(c, "outbound")
	if !errors.Is(err, context.DeadlineExceeded) || !shutdownClosed(o.bodyDone) || shutdownClosed(o.stopDone) || c.dependenciesMayClose() {
		t.Fatalf("Start return was treated as worker exit: %v", err)
	}
	release.release()
	r5CallerWait(t, o.stopDone)
}

func TestR5ShutdownCallerStopErrorCannotAuthorizeClose(t *testing.T) {
	c := r5CallerCoordinator(t, 2*time.Second)
	failure := errors.New("session drain failed")
	c.goRun("smtp", func(ctx context.Context) { <-ctx.Done() }, func(context.Context) error { return failure })
	err := r5CallerResult(t, r5CallerDrain(c))
	if !errors.Is(err, failure) || !strings.Contains(err.Error(), "smtp") || c.dependenciesMayClose() {
		t.Fatalf("lost named stop error: %v", err)
	}
}

func TestR5ShutdownCallerAllStopsBeginBeforeAnyJoin(t *testing.T) {
	c := r5CallerCoordinator(t, 2*time.Second)
	release := r5CallerNewGate(t)
	entered := make(chan struct{}, 3)
	for _, name := range []string{"http", "smtp", "outbound"} {
		c.goRun(name, func(ctx context.Context) { <-ctx.Done() }, func(context.Context) error {
			entered <- struct{}{}
			<-release.ch
			return nil
		})
	}
	result := r5CallerDrain(c)
	for i := 0; i < 3; i++ {
		r5CallerWait(t, entered)
	}
	if c.dependenciesMayClose() {
		t.Fatal("stops still running but dependencies admitted")
	}
	release.release()
	if err := r5CallerResult(t, result); err != nil {
		t.Fatal(err)
	}
}

func TestR5ShutdownCallerRegisteredUnscheduledRunIsOwned(t *testing.T) {
	c := r5CallerCoordinator(t, 20*time.Millisecond)
	// Exercise the exact registration boundary before goRun launches its body.
	o := c.register("not-yet-scheduled", false, nil)
	err := r5CallerResult(t, r5CallerDrain(c))
	if !errors.Is(err, context.DeadlineExceeded) || c.dependenciesMayClose() || shutdownClosed(o.bodyDone) {
		t.Fatalf("registered pending goroutine was omitted: %v", err)
	}
	c.finishBody(o)
}

func TestR5ShutdownCallerExpiredOriginIsNotRenewed(t *testing.T) {
	c := r5CallerCoordinator(t, -time.Second)
	o := c.register("blocked-start", true, func(context.Context) error { return nil })
	c.requestStop("startup_error")
	deadline, _ := c.stopCtx.Deadline()
	err := c.drain()
	actual, _ := c.stopCtx.Deadline()
	if !errors.Is(err, context.DeadlineExceeded) || actual != deadline || c.dependenciesMayClose() {
		t.Fatalf("expired origin received a new budget: %v", err)
	}
	c.finishBody(o)
	r5CallerWait(t, o.stopDone)
}

func TestR5ShutdownCallerLateJoinCannotCommitAfterDeadline(t *testing.T) {
	c := r5CallerCoordinator(t, -time.Second)
	o := c.register("late-owner", false, nil)
	c.requestStop("expired-origin")
	original := c.stopCtx
	deadline, _ := original.Deadline()
	// The owner really exits, but only after the shutdown budget has expired.
	// The first drain must fail even though no pending owner remains to expose it.
	c.finishBody(o)
	err := c.drain()
	if !errors.Is(err, context.DeadlineExceeded) || c.dependenciesMayClose() {
		t.Fatalf("late join committed a graceful shutdown: %v", err)
	}
	if next := c.drain(); next != err || c.dependenciesMayClose() {
		t.Fatalf("deadline failure was not sticky: first=%v next=%v", err, next)
	}
	actual, _ := c.stopCtx.Deadline()
	if c.stopCtx != original || actual != deadline {
		t.Fatal("late join replaced the original shutdown budget")
	}
}

// This source contract supplements (not replaces) runtime caller tests: it
// locks the real role wiring, not a test-only unused lifecycle instance.
func TestR5ShutdownCallerMainRoleWiring(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "main.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var mainFn *ast.FuncDecl
	for _, decl := range f.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Name.Name == "main" {
			mainFn = fn
		}
	}
	if mainFn == nil {
		t.Fatal("main entry missing")
	}
	calls := func(node ast.Node) map[string]int {
		found := map[string]int{}
		ast.Inspect(node, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) == 0 {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || (sel.Sel.Name != "goRun" && sel.Sel.Name != "start") {
				return true
			}
			if lit, ok := call.Args[0].(*ast.BasicLit); ok && lit.Kind == token.STRING {
				name, _ := strconv.Unquote(lit.Value)
				found[name]++
			}
			if sel.Sel.Name == "start" {
				stop, ok := call.Args[len(call.Args)-1].(*ast.SelectorExpr)
				if !ok || stop.Sel.Name != "StopContext" {
					t.Error("formal outbound entry is not wired to bounded StopContext")
				}
			}
			return true
		})
		return found
	}
	wantRoles := map[string]map[string]int{
		"all": {"retention": 1, "hooks": 1, "ingest": 1, "outbound": 1, "smtp": 1, "http": 1},
		"api": {"hooks": 1, "ingest": 1, "http": 1}, "smtp": {"smtp": 1},
		"worker": {"hooks": 1, "ingest": 1, "outbound": 1}, "retention": {"retention": 1},
	}
	seen := map[string]bool{}
	ast.Inspect(mainFn, func(n ast.Node) bool {
		clause, ok := n.(*ast.CaseClause)
		if !ok {
			return true
		}
		for _, expr := range clause.List {
			lit, ok := expr.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				continue
			}
			role, _ := strconv.Unquote(lit.Value)
			if want, ok := wantRoles[role]; ok {
				seen[role] = true
				if got := calls(clause); !reflect.DeepEqual(got, want) {
					t.Errorf("role %s owner wiring: got %v want %v", role, got, want)
				}
			}
		}
		return true
	})
	if len(seen) != len(wantRoles) {
		t.Fatalf("missing formal roles: %v", seen)
	}
	for _, name := range []string{"signal", "heartbeat", "mailindex"} {
		if calls(mainFn)[name] != 1 {
			t.Errorf("missing process owner %s", name)
		}
	}
	guards, failedExits := 0, 0
	ast.Inspect(mainFn, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
			if sel.Sel.Name == "dependenciesMayClose" {
				guards++
			}
			if owner, ok := sel.X.(*ast.Ident); ok && owner.Name == "os" && sel.Sel.Name == "Exit" && len(call.Args) == 1 {
				if code, ok := call.Args[0].(*ast.BasicLit); ok && code.Value == "1" {
					failedExits++
				}
			}
		}
		return true
	})
	if guards != 2 || failedExits != 1 {
		t.Fatalf("dependency guards / nonzero failed exit missing: %d / %d", guards, failedExits)
	}
}
