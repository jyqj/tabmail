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
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("fileobj: mkdir %s: %w", root, err)
	}
	return &FileStore{root: root}, nil
}

func (f *FileStore) Put(_ context.Context, key string, r io.Reader, _ int64) error {
	path := filepath.Join(f.root, key)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	out, err := os.Create(path)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, r)
	return err
}

func (f *FileStore) Get(_ context.Context, key string) (io.ReadCloser, error) {
	return os.Open(filepath.Join(f.root, key))
}

func (f *FileStore) Delete(_ context.Context, key string) error {
	return os.Remove(filepath.Join(f.root, key))
}
