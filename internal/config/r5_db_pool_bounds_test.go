package config

import (
	"math"
	"strings"
	"testing"
	"time"
)

// These boundary cases exercise the public configuration entry points. They
// intentionally do not construct a database connection or depend on a DSN.
func TestR5DatabasePoolConfigBoundaries(t *testing.T) {
	r5ResourceEnvironment(t)
	for _, tc := range []struct {
		name  string
		key   string
		value string
	}{
		{"zero maximum", "TABMAIL_DB_MAXOPENCONNS", "0"},
		{"negative maximum", "TABMAIL_DB_MAXOPENCONNS", "-1"},
		{"int32 maximum overflow", "TABMAIL_DB_MAXOPENCONNS", "2147483648"},
		{"wrapped positive maximum", "TABMAIL_DB_MAXOPENCONNS", "4294967321"},
		{"negative idle minimum", "TABMAIL_DB_MAXIDLECONNS", "-1"},
		{"idle minimum exceeds maximum", "TABMAIL_DB_MAXIDLECONNS", "26"},
		{"int32 idle overflow", "TABMAIL_DB_MAXIDLECONNS", "4294967301"},
		{"zero lifetime", "TABMAIL_DB_CONNMAXLIFETIME", "0s"},
		{"negative lifetime", "TABMAIL_DB_CONNMAXLIFETIME", "-1s"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(tc.key, tc.value)
			cfg, err := Load()
			if err == nil || cfg != nil {
				t.Fatalf("unsafe database setting accepted: %s=%s", tc.key, tc.value)
			}
			if !strings.Contains(err.Error(), tc.key) {
				t.Fatalf("error must identify %s without exposing a DSN: %v", tc.key, err)
			}
		})
	}
}

func TestR5DatabasePoolDirectValidation(t *testing.T) {
	r5ResourceEnvironment(t)
	for _, tc := range []struct {
		name string
		pool DB
		key  string
	}{
		{"zero maximum", DB{MaxOpenConns: 0, ConnMaxLifetime: time.Minute}, "TABMAIL_DB_MAXOPENCONNS"},
		{"negative maximum", DB{MaxOpenConns: -1, ConnMaxLifetime: time.Minute}, "TABMAIL_DB_MAXOPENCONNS"},
		{"overflow maximum", DB{MaxOpenConns: math.MaxInt32 + 1, ConnMaxLifetime: time.Minute}, "TABMAIL_DB_MAXOPENCONNS"},
		{"negative idle", DB{MaxOpenConns: 2, MaxIdleConns: -1, ConnMaxLifetime: time.Minute}, "TABMAIL_DB_MAXIDLECONNS"},
		{"oversized idle", DB{MaxOpenConns: 2, MaxIdleConns: 3, ConnMaxLifetime: time.Minute}, "TABMAIL_DB_MAXIDLECONNS"},
		{"zero lifetime", DB{MaxOpenConns: 2}, "TABMAIL_DB_CONNMAXLIFETIME"},
		{"negative lifetime", DB{MaxOpenConns: 2, ConnMaxLifetime: -time.Nanosecond}, "TABMAIL_DB_CONNMAXLIFETIME"},
		{"zero idle allowed", DB{MaxOpenConns: 1, ConnMaxLifetime: time.Nanosecond}, ""},
		{"idle equals maximum", DB{MaxOpenConns: 2, MaxIdleConns: 2, ConnMaxLifetime: time.Minute}, ""},
		{"int32 maximum boundary", DB{MaxOpenConns: math.MaxInt32, MaxIdleConns: math.MaxInt32, ConnMaxLifetime: time.Minute}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := Load()
			if err != nil {
				t.Fatal(err)
			}
			tc.pool.DSN = cfg.DB.DSN
			cfg.DB = tc.pool
			err = cfg.Validate()
			if tc.key == "" {
				if err != nil {
					t.Fatalf("legitimate pool rejected: %v", err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.key) {
				t.Fatalf("expected a setting-specific error for %s, got %v", tc.key, err)
			}
		})
	}
	t.Run("environment defaults preserved", func(t *testing.T) {
		cfg, err := Load()
		if err != nil {
			t.Fatal(err)
		}
		if cfg.DB.MaxOpenConns != 25 || cfg.DB.MaxIdleConns != 5 || cfg.DB.ConnMaxLifetime != 300*time.Second {
			t.Fatal("database defaults changed")
		}
	})
}
