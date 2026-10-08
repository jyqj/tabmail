package s3obj

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"tabmail/internal/config"
)

// Store implements store.ObjectStore on top of an S3-compatible backend.
type Store struct {
	client *minio.Client
	bucket string
}

// New preserves the standalone constructor's background-context behavior.
// Process startup should use NewWithContext so shutdown cancels its check.
func New(cfg config.S3) (*Store, error) {
	return NewWithContext(context.Background(), cfg)
}

// NewWithContext initializes the store for the caller's startup lifetime.
func NewWithContext(ctx context.Context, cfg config.S3) (*Store, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(cfg.Endpoint) == "" {
		return nil, fmt.Errorf("s3obj: endpoint is required")
	}
	if strings.TrimSpace(cfg.Bucket) == "" {
		return nil, fmt.Errorf("s3obj: bucket is required")
	}
	client, err := minio.New(strings.TrimSpace(cfg.Endpoint), &minio.Options{
		Creds:        credentials.NewStaticV4(strings.TrimSpace(cfg.AccessKey), strings.TrimSpace(cfg.SecretKey), ""),
		Secure:       cfg.UseTLS,
		Region:       strings.TrimSpace(cfg.Region),
		BucketLookup: bucketLookup(cfg.ForcePathStyle),
	})
	if err != nil {
		return nil, fmt.Errorf("s3obj: create client: %w", err)
	}
	exists, err := client.BucketExists(ctx, strings.TrimSpace(cfg.Bucket))
	if err != nil {
		return nil, fmt.Errorf("s3obj: bucket exists check: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !exists {
		return nil, fmt.Errorf("s3obj: bucket %q does not exist", cfg.Bucket)
	}
	return &Store{client: client, bucket: strings.TrimSpace(cfg.Bucket)}, nil
}

func (s *Store) Put(ctx context.Context, key string, r io.Reader, size int64) error {
	if err := validateKey(ctx, key); err != nil {
		return err
	}
	_, err := s.client.PutObject(ctx, s.bucket, key, r, size, minio.PutObjectOptions{
		ContentType: "message/rfc822",
	})
	if err != nil {
		return fmt.Errorf("s3obj: put %s: %w", key, err)
	}
	return nil
}

func (s *Store) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	if err := validateKey(ctx, key); err != nil {
		return nil, err
	}
	obj, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("s3obj: get %s: %w", key, err)
	}
	if _, err := obj.Stat(); err != nil {
		_ = obj.Close()
		return nil, fmt.Errorf("s3obj: stat %s: %w", key, err)
	}
	return obj, nil
}

func (s *Store) Delete(ctx context.Context, key string) error {
	if err := validateKey(ctx, key); err != nil {
		return err
	}
	err := s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{})
	if err == nil {
		return nil
	}
	resp := minio.ToErrorResponse(err)
	if resp.Code == "NoSuchKey" || resp.Code == "NoSuchObject" {
		return nil
	}
	return fmt.Errorf("s3obj: delete %s: %w", key, err)
}

func (s *Store) Exists(ctx context.Context, key string) (bool, error) {
	if err := validateKey(ctx, key); err != nil {
		return false, err
	}
	_, err := s.client.StatObject(ctx, s.bucket, key, minio.StatObjectOptions{})
	if err == nil {
		return true, nil
	}
	resp := minio.ToErrorResponse(err)
	if resp.Code == "NoSuchKey" || resp.Code == "NoSuchObject" {
		return false, nil
	}
	return false, fmt.Errorf("s3obj: stat %s: %w", key, err)
}

// Metadata reference locks and counts use the exact stored key. Do not turn a
// different spelling into another object's read, overwrite or delete request.
// Keep canonical legacy names, Unicode and URL metacharacters byte-for-byte.
func validateKey(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !fs.ValidPath(key) || key == "." || strings.TrimSpace(key) != key || strings.ContainsAny(key, "\\\x00") {
		return fmt.Errorf("s3obj: object key must be a canonical relative path")
	}
	return nil
}

func bucketLookup(forcePathStyle bool) minio.BucketLookupType {
	if forcePathStyle {
		return minio.BucketLookupPath
	}
	return minio.BucketLookupAuto
}
