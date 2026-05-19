package handlers

import (
	"net/http"

	"github.com/google/uuid"

	"tabmail/internal/api/middleware"
)

func actorFromRequest(r *http.Request) string {
	if user := middleware.UserFromCtx(r.Context()); user != nil {
		return "user:" + user.ID.String()
	}
	if t := middleware.TenantFromCtx(r.Context()); t != nil && t.ID != uuid.Nil {
		return t.ID.String()
	}
	return "public"
}
