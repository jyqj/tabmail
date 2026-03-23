package config

import "testing"

func TestValidateRejectsPlaceholderSecrets(t *testing.T) {
	cfg := &Root{
		Role:               "all",
		AdminKey:           "change-this-admin-key",
		MailboxTokenSecret: "change-this-mailbox-token-secret",
		DB:                 DB{DSN: "postgres://user:pass@db:5432/tabmail?sslmode=disable"},
		Redis:              Redis{Addr: "redis:6379"},
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected placeholder secrets to be rejected")
	}
}

func TestValidateAcceptsProductionLikeConfig(t *testing.T) {
	cfg := &Root{
		Role:               "worker",
		ObjectStore:        "fs",
		DataDir:            "/data",
		AdminKey:           "super-admin-key-123",
		MailboxTokenSecret: "mailbox-token-secret-123456",
		DB:                 DB{DSN: "postgres://user:pass@db:5432/tabmail?sslmode=disable"},
		Redis:              Redis{Addr: "redis:6379"},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("unexpected validate error: %v", err)
	}
}

func TestValidateRequiresS3FieldsWhenEnabled(t *testing.T) {
	cfg := &Root{
		Role:               "all",
		ObjectStore:        "s3",
		AdminKey:           "super-admin-key-123",
		MailboxTokenSecret: "mailbox-token-secret-123456",
		DB:                 DB{DSN: "postgres://user:pass@db:5432/tabmail?sslmode=disable"},
		Redis:              Redis{Addr: "redis:6379"},
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected missing s3 config to be rejected")
	}
}

func TestValidateAcceptsS3Config(t *testing.T) {
	cfg := &Root{
		Role:               "all",
		ObjectStore:        "s3",
		AdminKey:           "super-admin-key-123",
		MailboxTokenSecret: "mailbox-token-secret-123456",
		DB:                 DB{DSN: "postgres://user:pass@db:5432/tabmail?sslmode=disable"},
		Redis:              Redis{Addr: "redis:6379"},
		S3: S3{
			Endpoint:  "minio:9000",
			Bucket:    "tabmail",
			AccessKey: "minioadmin",
			SecretKey: "minioadminsecret",
			UseTLS:    false,
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("unexpected validate error for s3 config: %v", err)
	}
}
