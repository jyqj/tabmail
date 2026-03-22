package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"

	"tabmail/internal/models"
)

type envelope struct {
	Data  any     `json:"data,omitempty"`
	Error *apiErr `json:"error,omitempty"`
	Meta  *meta   `json:"meta,omitempty"`
}

type apiErr struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type meta struct {
	Total   int `json:"total"`
	Page    int `json:"page"`
	PerPage int `json:"per_page"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func ok(w http.ResponseWriter, data any) {
	writeJSON(w, http.StatusOK, envelope{Data: data})
}

func created(w http.ResponseWriter, data any) {
	writeJSON(w, http.StatusCreated, envelope{Data: data})
}

func okList(w http.ResponseWriter, data any, total, page, perPage int) {
	writeJSON(w, http.StatusOK, envelope{
		Data: data,
		Meta: &meta{Total: total, Page: page, PerPage: perPage},
	})
}

func noContent(w http.ResponseWriter) {
	w.WriteHeader(http.StatusNoContent)
}

func errBadRequest(w http.ResponseWriter, msg string) {
	writeJSON(w, http.StatusBadRequest, envelope{Error: &apiErr{Code: "BAD_REQUEST", Message: msg}})
}

func errNotFound(w http.ResponseWriter, msg string) {
	writeJSON(w, http.StatusNotFound, envelope{Error: &apiErr{Code: "NOT_FOUND", Message: msg}})
}

func errForbidden(w http.ResponseWriter, msg string) {
	writeJSON(w, http.StatusForbidden, envelope{Error: &apiErr{Code: "FORBIDDEN", Message: msg}})
}

func errInternal(w http.ResponseWriter) {
	writeJSON(w, http.StatusInternalServerError, envelope{Error: &apiErr{Code: "INTERNAL", Message: "internal server error"}})
}

func errConflict(w http.ResponseWriter, msg string) {
	writeJSON(w, http.StatusConflict, envelope{Error: &apiErr{Code: "CONFLICT", Message: msg}})
}

func pageFromReq(r *http.Request) models.Page {
	p := models.Page{Page: 1, PerPage: 30}
	if v, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil {
		p.Page = v
	}
	if v, err := strconv.Atoi(r.URL.Query().Get("per_page")); err == nil {
		p.PerPage = v
	}
	return p.Normalize()
}

func decodeBody(r *http.Request, dst any) error {
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}
