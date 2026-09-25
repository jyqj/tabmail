// Company transports share small decoding helpers, never one another's state.
package handlers

import (
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"net/http"
	"tabmail/internal/api/middleware"
	"tabmail/internal/authz"
)

func companyID(w http.ResponseWriter, r *http.Request, name string) (uuid.UUID, bool) {
	id, e := uuid.Parse(chi.URLParam(r, name))
	if e != nil {
		errBadRequest(w, "invalid "+name)
		return uuid.Nil, false
	}
	return id, true
}
func companyBody[T any](w http.ResponseWriter, r *http.Request) (T, bool) {
	var v T
	r.Body = http.MaxBytesReader(w, r.Body, 2*1024*1024)
	if e := decodeBody(r, &v); e != nil {
		errBadRequest(w, "invalid request body")
		return v, false
	}
	return v, true
}
func companyActor(r *http.Request) authz.Actor { return middleware.ActorFromContext(r.Context()) }
