package s3obj

import (
	"context"
	"fmt"
	"io"
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

func New(cfg config.S3) (*Store, error) {
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
	ctx := context.Background()
	exists, err := client.BucketExists(ctx, strings.TrimSpace(cfg.Bucket))
	if err != nil {
		return nil, fmt.Errorf("s3obj: bucket exists check: %w", err)
	}
	if !exists {
		return nil, fmt.Errorf("s3obj: bucket %q does not exist", cfg.Bucket)
	}
	return &Store{client: client, bucket: strings.TrimSpace(cfg.Bucket)}, nil
}

func (s *Store) Put(ctx context.Context, key string, r io.Reader, size int64) error {
	_, err := s.client.PutObject(ctx, s.bucket, normalizeKey(key), r, size, minio.PutObjectOptions{
		ContentType: "message/rfc822",
	})
	if err != nil {
		return fmt.Errorf("s3obj: put %s: %w", key, err)
	}
	return nil
}

func (s *Store) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	obj, err := s.client.GetObject(ctx, s.bucket, normalizeKey(key), minio.GetObjectOptions{})
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
	err := s.client.RemoveObject(ctx, s.bucket, normalizeKey(key), minio.RemoveObjectOptions{})
	if err == nil {
		return nil
	}
	resp := minio.ToErrorResponse(err)
	if resp.Code == "NoSuchKey" || resp.Code == "NoSuchObject" || resp.Code == "NoSuchBucket" {
		return nil
	}
	return fmt.Errorf("s3obj: delete %s: %w", key, err)
}

func (s *Store) Exists(ctx context.Context, key string) (bool, error) {
	_, err := s.client.StatObject(ctx, s.bucket, normalizeKey(key), minio.StatObjectOptions{})
	if err == nil {
		return true, nil
	}
	resp := minio.ToErrorResponse(err)
	if resp.Code == "NoSuchKey" || resp.Code == "NoSuchObject" || resp.Code == "NoSuchBucket" {
		return false, nil
	}
	return false, fmt.Errorf("s3obj: stat %s: %w", key, err)
}

func normalizeKey(key string) string {
	return strings.TrimPrefix(strings.TrimSpace(key), "/")
}

func bucketLookup(forcePathStyle bool) minio.BucketLookupType {
	if forcePathStyle {
		return minio.BucketLookupPath
	}
	return minio.BucketLookupAuto
}
