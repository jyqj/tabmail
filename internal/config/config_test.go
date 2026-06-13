package config

import "testing"

func TestValidateRejectsPlaceholderSecrets(t *testing.T) {
	cfg := &Root{
		Role:               "all",
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
		MailboxTokenSecret: "mailbox-token-secret-123456",
		DB:                 DB{DSN: "postgres://user:pass@db:5432/tabmail?sslmode=disable"},
		Redis:              Redis{Addr: "redis:6379"},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("unexpected validate error: %v", err)
	}
	if cfg.Outbound.DKIMFailPolicy != DKIMFailClosed {
		t.Fatalf("expected DKIM fail policy to default to %q, got %q", DKIMFailClosed, cfg.Outbound.DKIMFailPolicy)
	}
}

func TestValidateNormalizesAndRejectsDKIMFailPolicy(t *testing.T) {
	cfg := &Root{
		Role:               "worker",
		ObjectStore:        "fs",
		DataDir:            "/data",
		MailboxTokenSecret: "mailbox-token-secret-123456",
		DB:                 DB{DSN: "postgres://user:pass@db:5432/tabmail?sslmode=disable"},
		Redis:              Redis{Addr: "redis:6379"},
		Outbound:           Outbound{DKIMFailPolicy: " FAIL_OPEN "},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("unexpected validate error: %v", err)
	}
	if cfg.Outbound.DKIMFailPolicy != DKIMFailOpen {
		t.Fatalf("expected DKIM fail policy to normalize to %q, got %q", DKIMFailOpen, cfg.Outbound.DKIMFailPolicy)
	}

	cfg.Outbound.DKIMFailPolicy = "deliver_anyway"
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected invalid DKIM fail policy to be rejected")
	}
}

func TestValidateRequiresS3FieldsWhenEnabled(t *testing.T) {
	cfg := &Root{
		Role:               "all",
		ObjectStore:        "s3",
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
