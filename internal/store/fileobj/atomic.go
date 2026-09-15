package fileobj

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Publish only a complete, fsynced file. An interrupted writer must not truncate
// an existing message or acknowledge a partial spool file as durable SMTP DATA.
func (f *FileStore) putAtomic(ctx context.Context, key string, r io.Reader) error {
	if !filepath.IsLocal(key) {
		return fmt.Errorf("object key must be a local relative path")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	root, err := filepath.Abs(f.root)
	if err != nil {
		return err
	}
	dest := filepath.Join(root, key)
	dir := filepath.Dir(dest)
	if err = os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	out, err := os.CreateTemp(dir, ".ingress-*")
	if err != nil {
		return err
	}
	defer os.Remove(out.Name())
	closed := false
	defer func() {
		if !closed {
			_ = out.Close()
		}
	}()
	if _, err = io.Copy(out, contextReader{ctx: ctx, r: r}); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = out.Sync(); err != nil {
		return err
	}
	err = out.Close()
	closed = true
	if err != nil {
		return err
	}
	if err = os.Rename(out.Name(), dest); err != nil {
		return err
	}
	// Sync newly created ancestor directories as well as the rename's directory.
	for current := dir; ; current = filepath.Dir(current) {
		d, err := os.Open(current)
		if err != nil {
			return err
		}
		syncErr := d.Sync()
		closeErr := d.Close()
		if syncErr != nil {
			return syncErr
		}
		if closeErr != nil {
			return closeErr
		}
		if current == root {
			break
		}
	}
	return nil
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r contextReader) Read(b []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(b)
}
