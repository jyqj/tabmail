package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// These tests run the real registration handler, password hashing and token
// response against counted persistence ports. They do not attest PostgreSQL
// atomicity or external email ownership/deliverability.
func TestAdvanceRegistrationEmailRejectsBeforeAccountEffects(t *testing.T) {
	for _, tc := range []struct{ name, email string }{
		{"missing-at", "not-an-email"},
		{"missing-local", "@example.test"},
		{"missing-domain", "person@"},
		{"multiple-at", "person@example@test"},
		{"local-leading-dot", ".person@example.test"},
		{"local-trailing-dot", "person.@example.test"},
		{"local-double-dot", "per..son@example.test"},
		{"domain-double-dot", "person@example..test"},
		{"two-addresses", "one@example.test,two@example.test"},
		{"semicolon-list", "one@example.test;two@example.test"},
		{"display-name", "Person <person@example.test>"},
		{"quoted-display-name", `"Person" <person@example.test>`},
		{"angle-address", "<person@example.test>"},
		{"trailing-comment", "person@example.test (Person)"},
		{"leading-comment", "(Person)person@example.test"},
		{"address-group", "people:person@example.test;"},
		{"unclosed-quote", `"person@example.test`},
		{"newline", "person@example.test\nX-Injected: value"},
		{"crlf-quoted-local", "\"per\r\nson\"@example.test"},
		{"nul", "per\x00son@example.test"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := &registrationTextStore{}
			w := registrationTextRequest(t, newAuthBodyLimitHandler(t, st), tc.email, "Person")
			var got envelope
			if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if w.Code != http.StatusBadRequest || got.Error == nil || got.Error.Code != "BAD_REQUEST" {
				t.Errorf("invalid account email status=%d body=%s", w.Code, w.Body.String())
			}
			if st.lookups != 0 || st.tenantCalls != 0 || st.userCalls != 0 || st.tokenCalls != 0 || len(w.Result().Cookies()) != 0 {
				t.Errorf("invalid account email reached persistence: lookups=%d tenants=%d users=%d tokens=%d cookies=%d", st.lookups, st.tenantCalls, st.userCalls, st.tokenCalls, len(w.Result().Cookies()))
			}
		})
	}
}

func TestAdvanceRegistrationEmailPreservesSingleMailbox(t *testing.T) {
	for _, tc := range []struct{ name, email, want string }{
		{"ordinary", "person@example.test", "person@example.test"},
		{"normalized", " \tPERSON@Example.TEST\n", "person@example.test"},
		{"plus-tag", "person+tag@example.test", "person+tag@example.test"},
		{"quoted-comma", `"ops,team"@example.test`, `"ops,team"@example.test`},
		{"quoted-at", `"ops@team"@example.test`, `"ops@team"@example.test`},
		{"quoted-space", `"ops team"@example.test`, `"ops team"@example.test`},
		{"escaped-quote", `"ops\"team"@example.test`, `"ops\"team"@example.test`},
		{"redundant-quotes", `"person"@example.test`, `"person"@example.test`},
		{"unicode", "用户@例子.测试", "用户@例子.测试"},
		{"address-literal", "person@[127.0.0.1]", "person@[127.0.0.1]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := &registrationTextStore{}
			w := registrationTextRequest(t, newAuthBodyLimitHandler(t, st), tc.email, "Person")
			if w.Code != http.StatusCreated || st.lookups != 1 || st.tenantCalls != 1 || st.userCalls != 1 || st.tokenCalls != 1 || st.user == nil || st.tenant == nil || st.token == nil {
				t.Fatalf("single mailbox rejected: status=%d calls=%d/%d/%d/%d", w.Code, st.lookups, st.tenantCalls, st.userCalls, st.tokenCalls)
			}
			if st.lookupEmail != tc.want || st.user.Email != tc.want || st.tenant.Name != tc.want || len(w.Result().Cookies()) != 1 {
				t.Fatal("existing normalized identity or successful session response changed")
			}
		})
	}
}

func TestAdvanceRegistrationEmailKeepsLegacyLoginLookup(t *testing.T) {
	st := &authBodyLimitStore{}
	h := newAuthBodyLimitHandler(t, st)
	r := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"email":"Legacy-Identifier","password":"synthetic-password"}`))
	w := httptest.NewRecorder()
	h.Login(w, r)
	if w.Code != http.StatusUnauthorized || st.lookups != 1 || st.lastEmail != "legacy-identifier" || len(w.Result().Cookies()) != 0 {
		t.Fatal("registration validation changed the legacy login contract")
	}
}
