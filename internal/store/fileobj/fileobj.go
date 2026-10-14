package fileobj

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// FileStore stores raw .eml blobs on the local filesystem.
type FileStore struct {
	root string
}

func New(root string) (*FileStore, error) {
	// Resolve configuration once; a later process chdir must not select a new
	// object store. Existing directory permissions are intentionally untouched.
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(absolute, 0o700); err != nil {
		return nil, fmt.Errorf("fileobj: mkdir %s: %w", absolute, err)
	}
	return &FileStore{root: absolute}, nil
}

// Put publishes exactly size bytes; -1 explicitly denotes unknown length.
func (f *FileStore) Put(ctx context.Context, key string, r io.Reader, size int64) error {
	return f.putAtomic(ctx, key, r, size)
}

func (f *FileStore) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	root, err := f.openRoot(ctx, key)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	if _, err = regularObject(root, key); err != nil {
		return nil, err
	}
	file, err := root.Open(key)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err == nil && !info.Mode().IsRegular() {
		err = errNotRegularObject
	}
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	// The opened file owns its descriptor independently of root.
	return &contextFile{File: file, reader: contextReader{ctx: ctx, r: file}}, nil
}

func (f *FileStore) Delete(ctx context.Context, key string) error {
	root, err := f.openRoot(ctx, key)
	if err != nil {
		return err
	}
	defer root.Close()
	if _, err = regularObject(root, key); err != nil {
		if os.IsNotExist(err) {
			return ctx.Err()
		}
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	err = root.Remove(key)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func (f *FileStore) Exists(ctx context.Context, key string) (bool, error) {
	root, err := f.openRoot(ctx, key)
	if err != nil {
		return false, err
	}
	defer root.Close()
	_, err = regularObject(root, key)
	if cancelled := ctx.Err(); cancelled != nil {
		return false, cancelled
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return err == nil, err
}
