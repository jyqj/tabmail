package handlers

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/models"
)

type adminInvitationInputStore struct {
	userAdminStore
	lookups, writes int
	lookupEmail     string
	invitation      *models.AdminInvitation
	existing        *models.User
	lookupErr       error
	writeErr        error
}

func (s *adminInvitationInputStore) GetUserByEmail(_ context.Context, email string) (*models.User, error) {
	s.lookups++
	s.lookupEmail = email
	return s.existing, s.lookupErr
}

func (s *adminInvitationInputStore) CreateAdminInvitation(_ context.Context, invitation *models.AdminInvitation) error {
	s.writes++
	if s.writeErr != nil {
		return s.writeErr
	}
	invitation.ID = uuid.New()
	copy := *invitation
	s.invitation = &copy
	return nil
}

type adminInvitationInputReader struct {
	io.Reader
	read, closed, chunk int
}

func (r *adminInvitationInputReader) Read(p []byte) (int, error) {
	if r.chunk > 0 && len(p) > r.chunk {
		p = p[:r.chunk]
	}
	n, err := r.Reader.Read(p)
	r.read += n
	return n, err
}
func (r *adminInvitationInputReader) Close() error { r.closed++; return nil }

func adminInvitationInputRequest(t *testing.T, st *adminInvitationInputStore, body string, streamed bool) (*httptest.ResponseRecorder, *adminInvitationInputReader) {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/admin/invite", strings.NewReader(body))
	reader := &adminInvitationInputReader{Reader: r.Body}
	if streamed {
		r.ContentLength = -1
		r.TransferEncoding = []string{"chunked"}
		reader.chunk = 17
	}
	r.Body = reader
	w := httptest.NewRecorder()
	NewUserAdminHandler(st, zerolog.Nop()).InviteAdmin(w, r)
	if reader.closed != 1 {
		t.Fatalf("invitation request body closed %d times", reader.closed)
	}
	return w, reader
}

func adminInvitationInputJSON(t *testing.T, email string) string {
	t.Helper()
	raw, err := json.Marshal(map[string]string{"email": email})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestR5AdminInvitationRejectsInvalidEmailBeforePersistence(t *testing.T) {
	for _, tc := range []struct{ name, email string }{
		{"empty", " \t\r\n"},
		{"missing_domain", "employee"},
		{"missing_local", "@example.test"},
		{"empty_domain", "employee@"},
		{"address_list", "one@example.test,two@example.test"},
		{"display_name", "Employee <employee@example.test>"},
		{"bracketed", "<employee@example.test>"},
		{"newline", "employee\r\n@example.test"},
		{"nul", "employee\x00@example.test"},
		{"ascii_256", strings.Repeat("a", 243) + "@example.test"},
		{"unicode_256", strings.Repeat("中", 243) + "@example.test"},
		{"normalized_256", " \t" + strings.Repeat("A", 243) + "@EXAMPLE.TEST\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := &adminInvitationInputStore{}
			w, _ := adminInvitationInputRequest(t, st, adminInvitationInputJSON(t, tc.email), false)
			var response envelope
			if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if w.Code != http.StatusBadRequest || response.Error == nil || response.Error.Code != "BAD_REQUEST" || st.lookups != 0 || st.writes != 0 || st.invitation != nil {
				t.Fatalf("invalid invitation email crossed persistence: status=%d lookups=%d writes=%d", w.Code, st.lookups, st.writes)
			}
		})
	}
}

func TestR5AdminInvitationBodyHasFiniteBudget(t *testing.T) {
	valid := adminInvitationInputJSON(t, "employee@example.test")
	for _, streamed := range []bool{false, true} {
		mode := "known_length"
		if streamed {
			mode = "streamed"
		}
		for _, tc := range []struct {
			name, body string
			valid      bool
		}{
			{"at_limit", valid + strings.Repeat(" ", maxAuthBodyBytes-len(valid)), true},
			{"one_over", valid + strings.Repeat(" ", maxAuthBodyBytes+1-len(valid)), false},
			{"large_trailing_whitespace", valid + strings.Repeat(" ", 3*maxAuthBodyBytes), false},
			{"oversized_email", adminInvitationInputJSON(t, strings.Repeat("a", 3*maxAuthBodyBytes)+"@example.test"), false},
			{"unknown_field", `{"email":"employee@example.test","role":"super_admin"}`, false},
			{"trailing_document", valid + ` {}`, false},
			{"malformed", `{"email":"employee@example.test"`, false},
			{"whole_null", `null`, false},
		} {
			t.Run(mode+"/"+tc.name, func(t *testing.T) {
				st := &adminInvitationInputStore{}
				w, reader := adminInvitationInputRequest(t, st, tc.body, streamed)
				if reader.read > maxAuthBodyBytes+1 {
					t.Errorf("read %d bytes, exceeds finite credential body budget %d", reader.read, maxAuthBodyBytes+1)
				}
				if tc.valid {
					if w.Code != http.StatusCreated || st.lookups != 1 || st.writes != 1 || st.invitation == nil {
						t.Fatalf("valid at-limit body rejected: status=%d lookups=%d writes=%d", w.Code, st.lookups, st.writes)
					}
				} else if w.Code != http.StatusBadRequest || st.lookups != 0 || st.writes != 0 || st.invitation != nil {
					t.Fatalf("invalid body crossed invitation persistence: status=%d lookups=%d writes=%d", w.Code, st.lookups, st.writes)
				}
			})
		}
	}
}

func TestR5AdminInvitationValidIdentityAndCredential(t *testing.T) {
	for _, tc := range []struct{ name, email, normalized string }{
		{"ordinary", "employee@example.test", "employee@example.test"},
		{"normalized", " \tEMPLOYEE@EXAMPLE.TEST\r\n", "employee@example.test"},
		{"unicode", "员工@example.test", "员工@example.test"},
		{"quoted_comma", `"sales,west"@example.test`, `"sales,west"@example.test`},
		{"quoted_at", `"sales@west"@example.test`, `"sales@west"@example.test`},
		{"text_255", strings.Repeat("a", 242) + "@example.test", strings.Repeat("a", 242) + "@example.test"},
		{"unicode_255", strings.Repeat("中", 242) + "@example.test", strings.Repeat("中", 242) + "@example.test"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := &adminInvitationInputStore{}
			before := time.Now()
			w, _ := adminInvitationInputRequest(t, st, adminInvitationInputJSON(t, tc.email), false)
			var response struct {
				Data struct {
					ID         uuid.UUID `json:"id"`
					Email      string    `json:"email"`
					InviteCode string    `json:"invite_code"`
					ExpiresAt  time.Time `json:"expires_at"`
				} `json:"data"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if w.Code != http.StatusCreated || st.lookups != 1 || st.writes != 1 || st.invitation == nil || st.lookupEmail != tc.normalized || st.invitation.Email != tc.normalized || response.Data.Email != tc.normalized || response.Data.ID != st.invitation.ID {
				t.Fatalf("valid invitation identity changed: status=%d lookups=%d writes=%d", w.Code, st.lookups, st.writes)
			}
			decoded, err := hex.DecodeString(response.Data.InviteCode)
			if err != nil || len(decoded) != 32 || response.Data.InviteCode != st.invitation.InviteCode || !response.Data.ExpiresAt.Equal(st.invitation.ExpiresAt) || response.Data.ExpiresAt.Before(before.Add(72*time.Hour)) || response.Data.ExpiresAt.After(time.Now().Add(72*time.Hour)) || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("invitation credential/expiry/no-store response changed")
			}
		})
	}
}

func TestR5AdminInvitationKeepsStoreFailureOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name                            string
		existing                        *models.User
		lookupErr, writeErr             error
		status, wantLookups, wantWrites int
	}{
		{name: "duplicate", existing: &models.User{ID: uuid.New()}, status: http.StatusConflict, wantLookups: 1},
		{name: "lookup", lookupErr: errors.New("synthetic lookup error"), status: http.StatusInternalServerError, wantLookups: 1},
		{name: "write", writeErr: errors.New("synthetic write error"), status: http.StatusInternalServerError, wantLookups: 1, wantWrites: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := &adminInvitationInputStore{existing: tc.existing, lookupErr: tc.lookupErr, writeErr: tc.writeErr}
			w, _ := adminInvitationInputRequest(t, st, adminInvitationInputJSON(t, "employee@example.test"), false)
			if w.Code != tc.status || st.lookups != tc.wantLookups || st.writes != tc.wantWrites || st.invitation != nil || strings.Contains(w.Body.String(), "invite_code") {
				t.Fatalf("failure returned wrong outcome: status=%d lookups=%d writes=%d", w.Code, st.lookups, st.writes)
			}
		})
	}
}
