package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/models"
)

// The lookup sentinel stops valid inputs before hashing or persistence. Invalid
// inputs must never reach storage, including the previously accepted 8-byte
// registration password and bcrypt's over-limit input.
type credentialLookupProbe struct {
	authStore
	lookups int
}

func (s *credentialLookupProbe) GetUserByEmail(context.Context, string) (*models.User, error) {
	s.lookups++
	return nil, errors.New("test lookup sentinel")
}

func TestRegistrationPasswordBytePolicy(t *testing.T) {
	for _, tc := range []struct {
		name, password string
		valid          bool
	}{
		{"legacy-eight", "12345678", false}, {"eleven", strings.Repeat("a", 11), false},
		{"twelve", strings.Repeat("a", 12), true}, {"maximum", strings.Repeat("a", 72), true},
		{"seventy-three", strings.Repeat("a", 73), false},
		{"unicode-at-maximum", strings.Repeat("中", 24), true},
		{"unicode-over-maximum", strings.Repeat("中", 25), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := &credentialLookupProbe{}
			h := NewAuthHandler(st, "test-only-secret", uuid.New(), true, nil, false, zerolog.Nop())
			body, err := json.Marshal(map[string]string{"email": "policy@example.test", "password": tc.password})
			if err != nil {
				t.Fatal(err)
			}
			r := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", strings.NewReader(string(body)))
			r.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			h.Register(w, r)
			wantStatus, wantLookups := http.StatusBadRequest, 0
			if tc.valid {
				wantStatus, wantLookups = http.StatusInternalServerError, 1
			}
			if w.Code != wantStatus || st.lookups != wantLookups {
				t.Fatalf("CREDENTIAL_POLICY_DRIFT: status=%d lookups=%d, want %d/%d", w.Code, st.lookups, wantStatus, wantLookups)
			}
		})
	}
}
