package main

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"strconv"
	"testing"
	"time"

	"tabmail/internal/api/lifecycle"
)

type r5APICallerOwner struct {
	close func()
	stop  func(context.Context) error
}

func (o r5APICallerOwner) CloseAdmission()                       { o.close() }
func (o r5APICallerOwner) StopContext(ctx context.Context) error { return o.stop(ctx) }

func TestR5APIBackgroundShutdownCallerOrderContextAndErrors(t *testing.T) {
	httpFailure, ownerFailure := errors.New("HTTP shutdown failure"), errors.New("API join failure")
	for _, tc := range []struct {
		name              string
		httpErr, ownerErr error
	}{
		{"success", nil, nil},
		{"http_failure", httpFailure, nil},
		{"owner_failure", nil, ownerFailure},
		{"both_failures", httpFailure, ownerFailure},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(time.Hour))
			defer cancel()
			deadline, _ := ctx.Deadline()
			var order []string
			check := func(got context.Context) {
				t.Helper()
				d, ok := got.Deadline()
				if got != ctx || !ok || d != deadline {
					t.Error("callback renewed or replaced the original shutdown context")
				}
			}
			owner := r5APICallerOwner{
				close: func() { order = append(order, "close-admission") },
				stop: func(got context.Context) error {
					check(got)
					order = append(order, "join-api")
					return tc.ownerErr
				},
			}
			err := stopHTTPAndAPI(ctx, func(got context.Context) error {
				check(got)
				order = append(order, "stop-http")
				return tc.httpErr
			}, owner)
			if !reflect.DeepEqual(order, []string{"close-admission", "stop-http", "join-api"}) {
				t.Fatalf("shutdown order: %v", order)
			}
			for _, cause := range []error{tc.httpErr, tc.ownerErr} {
				if cause != nil && !errors.Is(err, cause) {
					t.Fatalf("lost error identity %v: %v", cause, err)
				}
			}
			if tc.httpErr == nil && tc.ownerErr == nil && err != nil {
				t.Fatal(err)
			}
		})
	}
}

func r5APICallerPendingTask(t *testing.T, owner *lifecycle.Owner, gate *r5CallerGate) {
	t.Helper()
	ctx, release, err := owner.Enter(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	entered := make(chan struct{})
	if err := owner.Go(ctx, func() { close(entered); <-gate.ch }); err != nil {
		t.Fatal(err)
	}
	r5CallerWait(t, entered)
}

func TestR5APIBackgroundShutdownCallerRealOwnerBlocksDependencyClose(t *testing.T) {
	c := r5CallerCoordinator(t, 2*time.Second)
	owner := lifecycle.New()
	gate := r5CallerNewGate(t)
	r5APICallerPendingTask(t, owner, gate)
	httpStopped := make(chan struct{})
	c.goRun("http", func(ctx context.Context) { <-ctx.Done() }, func(ctx context.Context) error {
		return stopHTTPAndAPI(ctx, func(context.Context) error { close(httpStopped); return nil }, owner)
	})
	result := r5CallerDrain(c)
	r5CallerWait(t, httpStopped)
	if shutdownClosed(owner.Done()) || c.dependenciesMayClose() {
		t.Fatal("HTTP completion lost a real API background owner")
	}
	if _, release, err := owner.Enter(context.Background()); !errors.Is(err, lifecycle.ErrStopping) {
		if release != nil {
			release()
		}
		t.Fatalf("new API request admitted during shutdown: %v", err)
	}
	select {
	case err := <-result:
		t.Fatalf("drained while API task was blocked: %v", err)
	default:
	}
	gate.release()
	if err := r5CallerResult(t, result); err != nil || !c.dependenciesMayClose() {
		t.Fatalf("real API join not admitted: %v", err)
	}
	r5CallerWait(t, owner.Done())
}

func TestR5APIBackgroundShutdownCallerHTTPErrorStillJoinsOwner(t *testing.T) {
	c := r5CallerCoordinator(t, 2*time.Second)
	owner := lifecycle.New()
	gate := r5CallerNewGate(t)
	r5APICallerPendingTask(t, owner, gate)
	failure := errors.New("HTTP shutdown failed")
	httpStopped := make(chan struct{})
	c.goRun("http", func(ctx context.Context) { <-ctx.Done() }, func(ctx context.Context) error {
		return stopHTTPAndAPI(ctx, func(context.Context) error { close(httpStopped); return failure }, owner)
	})
	result := r5CallerDrain(c)
	r5CallerWait(t, httpStopped)
	select {
	case err := <-result:
		t.Fatalf("HTTP error skipped real API join: %v", err)
	default:
	}
	if c.dependenciesMayClose() || shutdownClosed(owner.Done()) {
		t.Fatal("HTTP error released API ownership")
	}
	gate.release()
	err := r5CallerResult(t, result)
	if !errors.Is(err, failure) || c.dependenciesMayClose() {
		t.Fatalf("HTTP failure was erased after actual API join: %v", err)
	}
	r5CallerWait(t, owner.Done())
}

func TestR5APIBackgroundShutdownCallerTimeoutIsNotDrain(t *testing.T) {
	c := r5CallerCoordinator(t, 20*time.Millisecond)
	owner := lifecycle.New()
	gate := r5CallerNewGate(t)
	r5APICallerPendingTask(t, owner, gate)
	c.goRun("http", func(ctx context.Context) { <-ctx.Done() }, func(ctx context.Context) error {
		return stopHTTPAndAPI(ctx, func(context.Context) error { return nil }, owner)
	})
	err := r5CallerResult(t, r5CallerDrain(c))
	if !errors.Is(err, context.DeadlineExceeded) || c.dependenciesMayClose() || shutdownClosed(owner.Done()) {
		t.Fatalf("API timeout was reported as complete: %v", err)
	}
	gate.release()
	r5CallerWait(t, owner.Done())
	r5CallerWait(t, r5CallerOwner(c, "http").stopDone)
	if next := c.drain(); next != err || c.dependenciesMayClose() {
		t.Fatal("late API completion relabelled the failed process shutdown")
	}
}

func TestR5APIBackgroundShutdownCallerExhaustedContextReachesOwner(t *testing.T) {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(time.Hour))
	defer cancel()
	owner := lifecycle.New()
	// HTTP consumes/cancels the original budget. The subsequent real Owner must
	// see it, even though its own Done is already closed and no task remains.
	err := stopHTTPAndAPI(ctx, func(got context.Context) error {
		if got != ctx {
			t.Fatal("HTTP received a different context")
		}
		cancel()
		return context.Canceled
	}, owner)
	if !errors.Is(err, context.Canceled) || !shutdownClosed(owner.Done()) {
		t.Fatalf("API owner received a fresh budget: %v", err)
	}
}

func TestR5APIBackgroundShutdownCallerAdmittedRequestCanSpawnDuringHTTPDrain(t *testing.T) {
	c := r5CallerCoordinator(t, 2*time.Second)
	owner := lifecycle.New()
	request, releaseRequest, err := owner.Enter(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(releaseRequest)
	gate := r5CallerNewGate(t)
	spawned := make(chan struct{})
	c.goRun("http", func(ctx context.Context) { <-ctx.Done() }, func(ctx context.Context) error {
		return stopHTTPAndAPI(ctx, func(context.Context) error {
			// Admission is closed, but this request was already admitted.
			err := owner.Go(request, func() { close(spawned); <-gate.ch })
			releaseRequest()
			return err
		}, owner)
	})
	result := r5CallerDrain(c)
	r5CallerWait(t, spawned)
	if c.dependenciesMayClose() || shutdownClosed(owner.Done()) {
		t.Fatal("child admitted during HTTP drain was not joined")
	}
	gate.release()
	if err := r5CallerResult(t, result); err != nil {
		t.Fatal(err)
	}
}

// This locks both real role callbacks to their locally constructed Router. It
// is source wiring evidence, not a real role/signal/TCP/PG process acceptance.
func TestR5APIBackgroundShutdownCallerBothFormalRolesWired(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "main.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]int{}
	ast.Inspect(f, func(n ast.Node) bool {
		clause, ok := n.(*ast.CaseClause)
		if !ok {
			return true
		}
		var role string
		for _, expr := range clause.List {
			if lit, ok := expr.(*ast.BasicLit); ok && lit.Kind == token.STRING {
				value, _ := strconv.Unquote(lit.Value)
				if value == "all" || value == "api" {
					role = value
				}
			}
		}
		if role == "" {
			return true
		}
		newRouter, httpHandler := false, false
		for _, stmt := range clause.Body {
			if assign, ok := stmt.(*ast.AssignStmt); ok && len(assign.Lhs) == 1 && len(assign.Rhs) == 1 {
				id, _ := assign.Lhs[0].(*ast.Ident)
				call, _ := assign.Rhs[0].(*ast.CallExpr)
				if id == nil || call == nil {
					continue
				}
				if sel, ok := call.Fun.(*ast.SelectorExpr); ok && id.Name == "handler" && sel.Sel.Name == "NewRouter" {
					if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "api" {
						newRouter = true
					}
				}
				if fn, ok := call.Fun.(*ast.Ident); ok && fn.Name == "newHTTPServer" && id.Name == "httpSrv" && len(call.Args) == 2 {
					if arg, ok := call.Args[1].(*ast.Ident); ok && arg.Name == "handler" {
						httpHandler = true
					}
				}
			}
		}
		ast.Inspect(clause, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) != 3 {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "goRun" {
				return true
			}
			name, ok := call.Args[0].(*ast.BasicLit)
			if !ok || name.Value != `"http"` {
				return true
			}
			callback, ok := call.Args[2].(*ast.FuncLit)
			if !ok || len(callback.Body.List) != 1 || len(callback.Type.Params.List) != 1 || len(callback.Type.Params.List[0].Names) != 1 {
				t.Errorf("role %s has no bounded API join callback", role)
				return true
			}
			ret, ok := callback.Body.List[0].(*ast.ReturnStmt)
			if !ok || len(ret.Results) != 1 {
				t.Errorf("role %s callback does not return join error", role)
				return true
			}
			join, ok := ret.Results[0].(*ast.CallExpr)
			if !ok || len(join.Args) != 3 {
				t.Errorf("role %s callback is not the HTTP/API join", role)
				return true
			}
			fn, _ := join.Fun.(*ast.Ident)
			ctx, _ := join.Args[0].(*ast.Ident)
			server, _ := join.Args[1].(*ast.SelectorExpr)
			owner, _ := join.Args[2].(*ast.Ident)
			if fn == nil || fn.Name != "stopHTTPAndAPI" || ctx == nil || ctx.Name != callback.Type.Params.List[0].Names[0].Name || server == nil || server.Sel.Name != "Shutdown" || owner == nil || owner.Name != "handler" {
				t.Errorf("role %s changed owner, deadline, or shutdown callback", role)
				return true
			}
			if target, ok := server.X.(*ast.Ident); !ok || target.Name != "httpSrv" || !newRouter || !httpHandler {
				t.Errorf("role %s does not join its actual HTTP server and API Router", role)
				return true
			}
			seen[role]++
			return true
		})
		return true
	})
	if !reflect.DeepEqual(seen, map[string]int{"all": 1, "api": 1}) {
		t.Fatalf("formal role callbacks: %v", seen)
	}
}
