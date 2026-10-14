package config

import (
	"math"
	"strconv"
	"strings"
	"testing"
)

func TestR5RetentionExpiryEnvironment(t *testing.T) {
	for _, hours := range []int{24, 3000000, 0, -1, -3000000, math.MaxInt32, math.MinInt32, int(^uint(0) >> 1), -int(^uint(0)>>1) - 1} {
		t.Run(strconv.Itoa(hours), func(t *testing.T) {
			r5ResourceEnvironment(t)
			const key = "TABMAIL_STORAGE_FALLBACKRETENTIONH"
			t.Setenv(key, strconv.Itoa(hours))
			cfg, err := Load()
			invalid := hours > 3000000 || hours < -3000000
			if invalid {
				if err == nil || cfg != nil || !strings.Contains(err.Error(), key) {
					t.Fatalf("invalid runtime retention must fail config load: %+v %v", cfg, err)
				}
			} else if err != nil || cfg == nil || cfg.Storage.FallbackRetentionH != hours {
				t.Fatalf("valid runtime retention changed: %+v %v", cfg, err)
			}
		})
	}
}
