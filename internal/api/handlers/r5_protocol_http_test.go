package handlers

import (
	"encoding/json"
	"github.com/rs/zerolog"
	"net/http/httptest"
	"tabmail/internal/app"
	"testing"
)

// Execute the actual response helpers: reason is not a universal required
// field. Retry's exact sub-classification remains part of its own contract.
func TestR5ProtocolWireErrorShape(t *testing.T) {
	for _, c := range []struct {
		name, code, reason string
		status             int
		invoke             func(*httptest.ResponseRecorder)
	}{
		{"bad_request", "BAD_REQUEST", "", 400, func(w *httptest.ResponseRecorder) { errBadRequest(w, "invalid input") }},
		{"forbidden", "FORBIDDEN", "", 403, func(w *httptest.ResponseRecorder) { errForbidden(w, "denied") }},
		{"not_found", "NOT_FOUND", "", 404, func(w *httptest.ResponseRecorder) { errNotFound(w, "unavailable") }},
		{"conflict", "CONFLICT", "", 409, func(w *httptest.ResponseRecorder) { respondAppError(w, zerolog.Nop(), app.Conflict("stale")) }},
		{"internal", "INTERNAL", "", 500, func(w *httptest.ResponseRecorder) { errInternal(w) }},
		{"retry_uncertain", "CONFLICT", "delivery_uncertain", 409, func(w *httptest.ResponseRecorder) { errConflictReason(w, "uncertain", "delivery_uncertain") }},
		{"retry_changed", "CONFLICT", "state_changed", 409, func(w *httptest.ResponseRecorder) { errConflictReason(w, "changed", "state_changed") }},
	} {
		t.Run(c.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c.invoke(w)
			if w.Code != c.status {
				t.Fatal(w.Code)
			}
			var env struct {
				Error map[string]json.RawMessage `json:"error"`
			}
			if e := json.Unmarshal(w.Body.Bytes(), &env); e != nil {
				t.Fatal(e)
			}
			var code, msg string
			if e := json.Unmarshal(env.Error["code"], &code); e != nil {
				t.Fatal(e)
			}
			if e := json.Unmarshal(env.Error["message"], &msg); e != nil {
				t.Fatal(e)
			}
			if code != c.code || msg == "" {
				t.Fatal("missing code/message")
			}
			reason, present := env.Error["reason"]
			if c.reason == "" {
				if present {
					t.Fatal("reason unexpectedly present")
				}
			} else {
				var value string
				if e := json.Unmarshal(reason, &value); e != nil || value != c.reason {
					t.Fatal("wrong exact reason")
				}
			}
		})
	}
}
