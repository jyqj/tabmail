package handlers

import (
	"net/http"
	"testing"
)

func TestAdvanceRegistrationEmailIndependentAddrSpecWhitespace(t *testing.T) {
	for _, email := range []string{
		"person@ example.test", "person@\texample.test", "person@ \texample.test",
		`"ops@team"@ example.test`, "\"ops team\"@\texample.test",
	} {
		t.Run(email, func(t *testing.T) {
			st := &registrationTextStore{}
			w := registrationTextRequest(t, newAuthBodyLimitHandler(t, st), email, "Synthetic person")
			if w.Code != http.StatusBadRequest || st.lookups != 0 || st.tenantCalls != 0 || st.userCalls != 0 || st.tokenCalls != 0 || len(w.Result().Cookies()) != 0 {
				t.Fatalf("noncanonical domain whitespace created account state: status=%d lookup=%d tenant=%d user=%d token=%d", w.Code, st.lookups, st.tenantCalls, st.userCalls, st.tokenCalls)
			}
		})
	}
	for _, email := range []string{`"ops team"@example.test`, `"ops@team"@example.test`, `"ops\" team"@example.test`} {
		t.Run(email, func(t *testing.T) {
			st := &registrationTextStore{}
			w := registrationTextRequest(t, newAuthBodyLimitHandler(t, st), email, "Synthetic person")
			if w.Code != http.StatusCreated || st.user == nil || st.user.Email != email {
				t.Fatalf("quoted local-part data changed: status=%d user=%#v", w.Code, st.user)
			}
		})
	}
}
