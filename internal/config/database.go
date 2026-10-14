package config

import (
	"fmt"
	"math"
)

// Validate checks the pool limits before pgx's int32 conversion or any network
// activity. Load supplies defaults; explicitly invalid values are not defaults.
// MaxIdleConns is the existing configuration name for pgx's minimum pool size.
func (c DB) Validate() error {
	if c.MaxOpenConns <= 0 || int64(c.MaxOpenConns) > math.MaxInt32 {
		return fmt.Errorf("config: TABMAIL_DB_MAXOPENCONNS must be between 1 and %d", math.MaxInt32)
	}
	if c.MaxIdleConns < 0 || c.MaxIdleConns > c.MaxOpenConns {
		return fmt.Errorf("config: TABMAIL_DB_MAXIDLECONNS must be between 0 and TABMAIL_DB_MAXOPENCONNS")
	}
	if c.ConnMaxLifetime <= 0 {
		return fmt.Errorf("config: TABMAIL_DB_CONNMAXLIFETIME must be positive")
	}
	return nil
}
