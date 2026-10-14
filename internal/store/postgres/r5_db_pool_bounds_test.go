package postgres_test

import (
	"context"
	"fmt"
	"math"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"tabmail/internal/config"
	"tabmail/internal/store/postgres"
)

func TestR5DatabasePoolRejectsBeforeConnecting(t *testing.T) {
	for _, tc := range []struct {
		name string
		pool config.DB
		key  string
	}{
		{"zero maximum", config.DB{MaxOpenConns: 0, ConnMaxLifetime: time.Minute}, "TABMAIL_DB_MAXOPENCONNS"},
		{"negative maximum", config.DB{MaxOpenConns: -1, ConnMaxLifetime: time.Minute}, "TABMAIL_DB_MAXOPENCONNS"},
		{"overflow maximum", config.DB{MaxOpenConns: math.MaxInt32 + 1, ConnMaxLifetime: time.Minute}, "TABMAIL_DB_MAXOPENCONNS"},
		{"negative idle", config.DB{MaxOpenConns: 2, MaxIdleConns: -1, ConnMaxLifetime: time.Minute}, "TABMAIL_DB_MAXIDLECONNS"},
		{"idle exceeds maximum", config.DB{MaxOpenConns: 2, MaxIdleConns: 3, ConnMaxLifetime: time.Minute}, "TABMAIL_DB_MAXIDLECONNS"},
		{"zero lifetime", config.DB{MaxOpenConns: 2}, "TABMAIL_DB_CONNMAXLIFETIME"},
		{"negative lifetime", config.DB{MaxOpenConns: 2, ConnMaxLifetime: -time.Second}, "TABMAIL_DB_CONNMAXLIFETIME"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			var attempts atomic.Int64
			joined := make(chan struct{})
			go func() {
				defer close(joined)
				for {
					conn, err := listener.Accept()
					if err != nil {
						return
					}
					attempts.Add(1)
					_ = conn.Close()
				}
			}()
			t.Cleanup(func() { _ = listener.Close(); <-joined })
			tc.pool.DSN = fmt.Sprintf("postgres://fixture:private-pool-secret@%s/fixture?sslmode=disable", listener.Addr())
			ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
			defer cancel()
			defer func() {
				if recovered := recover(); recovered != nil {
					t.Errorf("invalid pool configuration panicked: %v", recovered)
				}
				if got := attempts.Load(); got != 0 {
					t.Errorf("invalid configuration opened %d network connections", got)
				}
			}()
			st, err := postgres.New(ctx, tc.pool)
			if st != nil {
				_ = st.Close()
				t.Fatal("invalid configuration created a store")
			}
			if err == nil || !strings.Contains(err.Error(), tc.key) {
				t.Fatalf("expected setting-specific %s error, got %v", tc.key, err)
			}
			if strings.Contains(err.Error(), "private-pool-secret") {
				t.Fatal("configuration error leaked DSN")
			}
		})
	}
}
