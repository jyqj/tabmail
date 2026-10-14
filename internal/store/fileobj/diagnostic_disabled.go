//go:build !r5benchmark

package fileobj

import "context"

// Normal production builds do not inspect ctx or read a clock. The compiler
// can inline this constant nil result; original filesystem operations remain.
func beginFileDiagnostic(context.Context, string) func(error) { return nil }
