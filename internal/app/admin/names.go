package adminapp

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"tabmail/internal/app"
)

const (
	planNameMaxCharacters   = 64
	tenantNameMaxCharacters = 255
)

// Match the existing PostgreSQL VARCHAR character limits before persistence.
// Preserve meaningful names verbatim, including Unicode and surrounding space.
func validateAdminName(name string, maxCharacters int) error {
	if strings.TrimSpace(name) == "" {
		return app.BadRequest("name is required")
	}
	if strings.ContainsRune(name, 0) {
		return app.BadRequest("name must not contain NUL")
	}
	if utf8.RuneCountInString(name) > maxCharacters {
		return app.BadRequest(fmt.Sprintf("name must not exceed %d characters", maxCharacters))
	}
	return nil
}
