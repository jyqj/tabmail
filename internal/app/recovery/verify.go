// Package recovery verifies immutable ingress evidence before replay. It does
// not expose raw storage keys or bytes as an employee read capability.
package recovery

import (
	"context"
	"io"
	"tabmail/internal/company"
)

type Objects interface {
	Get(context.Context, string) (io.ReadCloser, error)
}

func Verify(ctx context.Context, objects Objects, v *company.RecoveryReceipt) bool {
	if objects == nil || v == nil || v.RawSize < 0 || v.RawSize > 25*1024*1024 || v.RawHash == "" {
		return false
	}
	r, e := objects.Get(ctx, v.RawKey)
	if e != nil || r == nil {
		return false
	}
	defer r.Close()
	raw, e := io.ReadAll(io.LimitReader(r, v.RawSize+1))
	return e == nil && int64(len(raw)) == v.RawSize && company.Hash(string(raw)) == v.RawHash
}
