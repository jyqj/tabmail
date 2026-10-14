//go:build r5benchmark

package fileobj

import (
	"context"
	"time"
)

type fileDiagnosticKey struct{}

// R5DiagnosticObserver receives only fixed stage names and call boundaries.
// No object key, path, bytes, credentials or syscall arguments are supplied.
// Elapsed includes goroutine rescheduling; it is not device I/O latency.
type R5DiagnosticObserver interface {
	ObserveFileOperation(context.Context, string, time.Time, time.Time, bool)
}

func WithR5DiagnosticObserver(ctx context.Context, observer R5DiagnosticObserver) context.Context {
	return context.WithValue(ctx, fileDiagnosticKey{}, observer)
}

func beginFileDiagnostic(ctx context.Context, stage string) func(error) {
	observer, _ := ctx.Value(fileDiagnosticKey{}).(R5DiagnosticObserver)
	if observer == nil {
		return nil
	}
	start := time.Now()
	return func(err error) { observer.ObserveFileOperation(ctx, stage, start, time.Now(), err != nil) }
}
