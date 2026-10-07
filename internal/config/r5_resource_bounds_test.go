package config

import (
	"os"
	"strings"
	"testing"
	"time"
)

// Exercise Load with only synthetic configuration. Preserve the caller's
// environment through testing cleanup, without depending on local .env files.
func r5ResourceEnvironment(t *testing.T) {
	t.Helper()
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(key, "TABMAIL_") {
			t.Setenv(key, "")
			if err := os.Unsetenv(key); err != nil {
				t.Fatal(err)
			}
		}
	}
	t.Setenv("TABMAIL_MAILBOX_TOKEN_SECRET", "synthetic-mailbox-secret-resource-bounds")
	t.Setenv("TABMAIL_JWT_SECRET", "synthetic-distinct-jwt-secret-resource-bounds")
}

func TestR5ResourceConfigDefaultsAndPositiveBoundaries(t *testing.T) {
	r5ResourceEnvironment(t)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SMTP.MaxRecipients != 200 || cfg.SMTP.MaxMessageBytes != 25*1024*1024 || cfg.SMTP.Timeout != 300*time.Second || cfg.SMTP.MaxConnections != 100 || cfg.Storage.RetentionScanInterval != time.Minute || cfg.Storage.RetentionBatchSize != 1000 {
		t.Fatal("resource defaults changed")
	}
	// A documented zero value for connections remains a valid explicit opt-out.
	for key, value := range map[string]string{
		"TABMAIL_SMTP_MAXRECIPIENTS": "1", "TABMAIL_SMTP_MAXMESSAGEBYTES": "1",
		"TABMAIL_SMTP_TIMEOUT": "1ns", "TABMAIL_SMTP_MAX_CONNECTIONS": "0",
		"TABMAIL_STORAGE_RETENTIONSCANINTERVAL": "1ns", "TABMAIL_STORAGE_RETENTIONBATCHSIZE": "1",
	} {
		t.Setenv(key, value)
	}
	cfg, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SMTP.MaxRecipients != 1 || cfg.SMTP.MaxMessageBytes != 1 || cfg.SMTP.Timeout != time.Nanosecond || cfg.SMTP.MaxConnections != 0 || cfg.Storage.RetentionScanInterval != time.Nanosecond || cfg.Storage.RetentionBatchSize != 1 {
		t.Fatal("valid environment overrides were ignored")
	}
}

func TestR5ResourceConfigRejectsUnsafeEnvironment(t *testing.T) {
	r5ResourceEnvironment(t)
	for _, tc := range []struct {
		key    string
		values []string
	}{
		{"TABMAIL_SMTP_MAXRECIPIENTS", []string{"0", "-1"}},
		{"TABMAIL_SMTP_MAXMESSAGEBYTES", []string{"0", "-1"}},
		{"TABMAIL_SMTP_TIMEOUT", []string{"0s", "-1s"}},
		{"TABMAIL_SMTP_MAX_CONNECTIONS", []string{"-1"}},
		{"TABMAIL_STORAGE_RETENTIONSCANINTERVAL", []string{"0s", "-1s"}},
		{"TABMAIL_STORAGE_RETENTIONBATCHSIZE", []string{"0", "-1"}},
	} {
		for _, value := range tc.values {
			t.Run(tc.key+"="+value, func(t *testing.T) {
				t.Setenv(tc.key, value)
				cfg, err := Load()
				if err == nil || cfg != nil {
					t.Fatalf("unsafe override accepted: %s=%s", tc.key, value)
				}
				if !strings.Contains(err.Error(), tc.key) {
					t.Fatalf("error does not identify the invalid setting: %v", err)
				}
			})
		}
	}
}

func TestR5ResourceConfigValidateRejectsUnsafeValues(t *testing.T) {
	r5ResourceEnvironment(t)
	for _, tc := range []struct {
		key    string
		mutate func(*Root)
	}{
		{"TABMAIL_SMTP_MAXRECIPIENTS", func(c *Root) { c.SMTP.MaxRecipients = 0 }},
		{"TABMAIL_SMTP_MAXMESSAGEBYTES", func(c *Root) { c.SMTP.MaxMessageBytes = -1 }},
		{"TABMAIL_SMTP_TIMEOUT", func(c *Root) { c.SMTP.Timeout = 0 }},
		{"TABMAIL_SMTP_MAX_CONNECTIONS", func(c *Root) { c.SMTP.MaxConnections = -1 }},
		{"TABMAIL_STORAGE_RETENTIONSCANINTERVAL", func(c *Root) { c.Storage.RetentionScanInterval = 0 }},
		{"TABMAIL_STORAGE_RETENTIONBATCHSIZE", func(c *Root) { c.Storage.RetentionBatchSize = -1 }},
	} {
		t.Run(tc.key, func(t *testing.T) {
			cfg, err := Load()
			if err != nil {
				t.Fatal(err)
			}
			tc.mutate(cfg)
			if err = cfg.Validate(); err == nil || !strings.Contains(err.Error(), tc.key) {
				t.Fatalf("Validate failed to identify %s: %v", tc.key, err)
			}
		})
	}
}
