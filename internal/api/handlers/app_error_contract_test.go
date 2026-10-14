package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rs/zerolog"

	appcore "tabmail/internal/app"
)

// TestRespondAppErrorContract pins the HTTP rendering of every app error kind:
// the status, machine code, JSON envelope and — for the conflict-with-data
// case — the exact position of the data payload must stay identical for every
// caller, whether the error comes from a domain constructor or a Failure.
func TestRespondAppErrorContract(t *testing.T) {
	hidden := errors.New("sensitive connection string")
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
		wantMsg    string
		wantData   map[string]any
	}{
		{
			name:       "bad request",
			err:        appcore.BadRequest("expected_revision is required"),
			wantStatus: http.StatusBadRequest,
			wantCode:   "BAD_REQUEST",
			wantMsg:    "expected_revision is required",
		},
		{
			name:       "forbidden",
			err:        appcore.Forbidden("not a member of this mailbox"),
			wantStatus: http.StatusForbidden,
			wantCode:   "FORBIDDEN",
			wantMsg:    "not a member of this mailbox",
		},
		{
			name:       "not found",
			err:        appcore.NotFound("submission not found"),
			wantStatus: http.StatusNotFound,
			wantCode:   "NOT_FOUND",
			wantMsg:    "submission not found",
		},
		{
			name:       "conflict",
			err:        appcore.Conflict("mailbox already exists"),
			wantStatus: http.StatusConflict,
			wantCode:   "CONFLICT",
			wantMsg:    "mailbox already exists",
		},
		{
			name:       "conflict with data carries revision",
			err:        appcore.ConflictWithData("draft revision changed; refresh the draft before retrying", map[string]any{"revision": 7}),
			wantStatus: http.StatusConflict,
			wantCode:   "CONFLICT",
			wantMsg:    "draft revision changed; refresh the draft before retrying",
			wantData:   map[string]any{"revision": float64(7)},
		},
		{
			name:       "quota exceeded",
			err:        appcore.QuotaExceeded("daily send quota reached"),
			wantStatus: http.StatusTooManyRequests,
			wantCode:   "QUOTA_EXCEEDED",
			wantMsg:    "daily send quota reached",
		},
		{
			name:       "internal never leaks the cause",
			err:        appcore.Internal(hidden),
			wantStatus: http.StatusInternalServerError,
			wantCode:   "INTERNAL",
			wantMsg:    "internal server error",
		},
		{
			name:       "unknown error never leaks",
			err:        hidden,
			wantStatus: http.StatusInternalServerError,
			wantCode:   "INTERNAL",
			wantMsg:    "internal server error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rr := httptest.NewRecorder()
			respondAppError(rr, zerolog.Nop(), tt.err)

			if rr.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rr.Code, tt.wantStatus)
			}
			var got envelope
			if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
				t.Fatalf("response is not valid JSON: %v", err)
			}
			if got.Error == nil {
				t.Fatalf("expected error object, got %s", rr.Body.String())
			}
			if got.Error.Code != tt.wantCode {
				t.Errorf("code = %q, want %q", got.Error.Code, tt.wantCode)
			}
			if got.Error.Message != tt.wantMsg {
				t.Errorf("message = %q, want %q", got.Error.Message, tt.wantMsg)
			}
			if tt.wantData == nil {
				if got.Data != nil {
					t.Errorf("data = %v, want none", got.Data)
				}
			} else {
				data, ok := got.Data.(map[string]any)
				if !ok {
					t.Fatalf("data = %#v, want object", got.Data)
				}
				for k, want := range tt.wantData {
					if data[k] != want {
						t.Errorf("data[%q] = %v, want %v", k, data[k], want)
					}
				}
			}
			if body := rr.Body.String(); strings.Contains(body, "sensitive connection string") {
				t.Errorf("response leaks internal cause: %s", body)
			}
		})
	}
}
