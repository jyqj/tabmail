package fileobj

import (
	"context"
	"fmt"
	"github.com/google/uuid"
	"io"
	"os"
	"path/filepath"
)

// Publish only a complete, fsynced file. An interrupted writer must not truncate
// an existing message or acknowledge a partial spool file as durable SMTP DATA.
func (f *FileStore) putAtomic(ctx context.Context, key string, r io.Reader, size int64) error {
	if r == nil || size < -1 {
		return fmt.Errorf("fileobj: invalid reader or declared size")
	}
	root, err := f.openRoot(ctx, key)
	if err != nil {
		return err
	}
	defer root.Close()
	if _, err = regularObject(root, key); err != nil && !os.IsNotExist(err) {
		return err
	}
	dir := filepath.Dir(key)
	if err = root.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	parent, err := root.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer parent.Close()
	// Keep the creating directory open for cleanup if its path is renamed.
	name := ".ingress-" + uuid.NewString()
	out, err := parent.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer parent.Remove(name)
	closed := false
	defer func() {
		if !closed {
			_ = out.Close()
		}
	}()
	reader := &contextReader{ctx: ctx, r: r}
	if size < 0 {
		_, err = io.Copy(out, reader)
	} else {
		var written int64
		written, err = io.Copy(out, &io.LimitedReader{R: reader, N: size})
		if err == nil && written != size {
			err = io.ErrUnexpectedEOF
		}
		if err == nil {
			// Probe one excess byte without size+1 overflow or unbounded draining.
			var probe [1]byte
			n, readErr := reader.Read(probe[:])
			if n != 0 {
				err = fmt.Errorf("fileobj: source exceeds declared size")
			} else if readErr != io.EOF {
				err = readErr
			}
		}
	}
	if err != nil {
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
	if err = ctx.Err(); err != nil {
		return err
	}
	// Resolve both names through the original root, not an unchecked absolute
	// path. A swapped parent symlink cannot redirect publication elsewhere.
	if err = root.Rename(filepath.Join(dir, name), key); err != nil {
		return err
	}
	// Sync newly created ancestor directories as well as the rename's directory.
	for current := dir; ; current = filepath.Dir(current) {
		d, err := root.Open(current)
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
		if current == "." {
			break
		}
	}
	return nil
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
	eof bool
}

func (r *contextReader) Read(b []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	if len(b) == 0 {
		return 0, nil
	}
	if r.eof {
		return 0, io.EOF
	}
	for empty := 0; empty < 100; empty++ {
		n, err := r.r.Read(b)
		if cancelled := r.ctx.Err(); cancelled != nil {
			clear(b)
			return 0, cancelled
		}
		if n < 0 || n > len(b) {
			clear(b)
			return 0, io.ErrUnexpectedEOF
		}
		if err == io.EOF {
			r.eof = true
		}
		if n > 0 || err != nil {
			return n, err
		}
	}
	return 0, io.ErrNoProgress
}
