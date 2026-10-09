package fileobj

import (
	"context"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

var errNotRegularObject = errors.New("fileobj: object is not a regular file")

// Keys are canonical relative slash paths. Lexical validation preserves object
// identity; os.Root also prevents escapes through symlinks while resolving paths.
// The configured root and its ownership are trusted configuration, not a sandbox
// against privileged local users, hard links or filesystem mounts.
func (f *FileStore) openRoot(ctx context.Context, key string) (*os.Root, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !filepath.IsLocal(key) || key == "." || path.Clean(key) != key || strings.ContainsAny(key, "\\\x00") {
		return nil, errors.New("fileobj: object key must be a canonical relative path")
	}
	root, err := os.OpenRoot(f.root)
	if err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		_ = root.Close()
		return nil, err
	}
	return root, nil
}

func regularObject(root *os.Root, key string) (os.FileInfo, error) {
	info, err := root.Lstat(key)
	if err != nil {
		return nil, err
	}
	// Blob keys identify files, never directories or leaf symlink aliases.
	if !info.Mode().IsRegular() {
		return nil, errNotRegularObject
	}
	return info, nil
}

type contextFile struct {
	*os.File
	reader contextReader
}

func (f *contextFile) Read(b []byte) (int, error) { return f.reader.Read(b) }

// Do not promote File.WriteTo: io.Copy must also pass through context checks.
func (f *contextFile) WriteTo(w io.Writer) (int64, error) {
	return io.Copy(w, struct{ io.Reader }{f})
}
