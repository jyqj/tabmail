package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"tabmail/internal/api/middleware"
	"tabmail/internal/models"
)

type suppressionBodyReader struct {
	io.Reader
	readBytes int
	readCalls int
}

func (r *suppressionBodyReader) Read(p []byte) (int, error) {
	r.readCalls++
	n, err := r.Reader.Read(p)
	r.readBytes += n
	return n, err
}

func (r *suppressionBodyReader) Close() error { return nil }

type suppressionBodyReadFailure struct{}

func (suppressionBodyReadFailure) Read([]byte) (int, error) {
	return 0, errors.New("PRIVATE suppression body read failure")
}

func TestDeleteSuppressionRequiresOneBoundedJSONDocument(t *testing.T) {
	const wireLimit = 64 * 1024
	const valid = `{"reason":"reviewed cleanup"}`
	for _, tc := range []struct {
		name    string
		body    string
		allowed bool
		readErr bool
	}{
		{"ordinary", valid, true, false},
		{"exact_wire_limit", valid + strings.Repeat(" ", wireLimit-len(valid)), true, false},
		{"unknown_field", `{"reason":"reviewed cleanup","force":true}`, false, false},
		{"unknown_nested_field", `{"reason":"reviewed cleanup","extra":{"action":"delete"}}`, false, false},
		{"second_object", valid + ` {"reason":"another reason"}`, false, false},
		{"second_null", valid + ` null`, false, false},
		{"trailing_garbage", valid + ` trailing`, false, false},
		{"oversized_leading_whitespace", strings.Repeat(" ", wireLimit) + valid, false, false},
		{"oversized_trailing_whitespace", valid + strings.Repeat(" ", wireLimit+1-len(valid)), false, false},
		{"oversized_trimmed_reason", `{"reason":"` + strings.Repeat(" ", wireLimit) + `reviewed cleanup"}`, false, false},
		{"truncated_object", `{"reason":"reviewed cleanup"`, false, false},
		{"array", `[{"reason":"reviewed cleanup"}]`, false, false},
		{"wrong_reason_type", `{"reason":123}`, false, false},
		{"null_document", `null`, false, false},
		{"trailing_read_failure", valid, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newOutboundAccessFixture(t)
			id := uuid.New()
			const address = "suppressed@fixture.test"
			if err := f.st.AddSuppression(context.Background(), &models.SuppressionEntry{
				ID: id, TenantID: f.tenantID, Address: address, Reason: "hard_bounce", CreatedAt: time.Now(),
			}); err != nil {
				t.Fatal(err)
			}
			var input io.Reader = strings.NewReader(tc.body)
			if tc.readErr {
				input = io.MultiReader(input, suppressionBodyReadFailure{})
			}
			body := &suppressionBodyReader{Reader: input}
			rr := performSuppressionDelete(t, f, id, f.tenantAdmin, body)
			wantStatus := http.StatusBadRequest
			if tc.allowed {
				wantStatus = http.StatusNoContent
			}
			if rr.Code != wantStatus {
				t.Errorf("HTTP %d, want %d: %s", rr.Code, wantStatus, rr.Body.String())
			}
			if body.readBytes > wireLimit+1 {
				t.Errorf("body consumed %d bytes, limit must stop at %d", body.readBytes, wireLimit+1)
			}
			if strings.Contains(rr.Body.String(), "PRIVATE") {
				t.Error("response exposed request reader failure")
			}
			remaining, err := f.st.IsSuppressed(context.Background(), f.tenantID, address)
			if err != nil || remaining == tc.allowed {
				t.Errorf("suppression retained=%v err=%v, request allowed=%v", remaining, err, tc.allowed)
			}
			audits, err := f.st.ListAuditEntries(context.Background(), 10)
			if err != nil {
				t.Fatal(err)
			}
			if !tc.allowed {
				if len(audits) != 0 {
					t.Errorf("invalid body wrote %d audit rows", len(audits))
				}
				return
			}
			if len(audits) != 1 || audits[0].Action != "suppression.delete" || audits[0].ResourceID == nil || *audits[0].ResourceID != id || audits[0].TenantID == nil || *audits[0].TenantID != f.tenantID {
				t.Fatalf("accepted deletion lost audit identity: %+v", audits)
			}
			var details struct{ Reason, Address string }
			if err := json.Unmarshal(audits[0].Details, &details); err != nil || details.Reason != "reviewed cleanup" || details.Address != address {
				t.Fatalf("accepted deletion audit details=%+v err=%v", details, err)
			}
		})
	}
}

func TestDeleteSuppressionPreservesReasonByteBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, reason string
		allowed      bool
	}{
		{"minimum", strings.Repeat("a", 8), true},
		{"maximum", strings.Repeat("a", 1000), true},
		{"trimmed", " \n\t" + strings.Repeat("a", 8) + "\u3000", true},
		{"too_short", strings.Repeat("a", 7), false},
		{"too_long", strings.Repeat("a", 1001), false},
		{"unicode_byte_limit", strings.Repeat("界", 334), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newOutboundAccessFixture(t)
			id := uuid.New()
			if err := f.st.AddSuppression(context.Background(), &models.SuppressionEntry{
				ID: id, TenantID: f.tenantID, Address: "bounded-reason@fixture.test", Reason: "hard_bounce", CreatedAt: time.Now(),
			}); err != nil {
				t.Fatal(err)
			}
			raw, err := json.Marshal(map[string]string{"reason": tc.reason})
			if err != nil {
				t.Fatal(err)
			}
			rr := performSuppressionDelete(t, f, id, f.tenantAdmin, &suppressionBodyReader{Reader: strings.NewReader(string(raw))})
			wantStatus := http.StatusBadRequest
			if tc.allowed {
				wantStatus = http.StatusNoContent
			}
			if rr.Code != wantStatus {
				t.Fatalf("HTTP %d, want %d: %s", rr.Code, wantStatus, rr.Body.String())
			}
			audits, err := f.st.ListAuditEntries(context.Background(), 10)
			if err != nil {
				t.Fatal(err)
			}
			if !tc.allowed {
				if len(audits) != 0 {
					t.Fatalf("invalid reason wrote %d audit rows", len(audits))
				}
				return
			}
			if len(audits) != 1 {
				t.Fatalf("accepted reason wrote %d audit rows", len(audits))
			}
			var details struct{ Reason string }
			if err := json.Unmarshal(audits[0].Details, &details); err != nil || details.Reason != strings.TrimSpace(tc.reason) {
				t.Fatalf("normalized audit reason=%q err=%v", details.Reason, err)
			}
		})
	}
}

func TestDeleteSuppressionRejectsEmployeeBeforeBodyRead(t *testing.T) {
	f := newOutboundAccessFixture(t)
	body := &suppressionBodyReader{Reader: suppressionBodyReadFailure{}}
	rr := performSuppressionDelete(t, f, uuid.New(), f.userA, body)
	if rr.Code != http.StatusForbidden || body.readCalls != 0 {
		t.Fatalf("employee request HTTP=%d body reads=%d", rr.Code, body.readCalls)
	}
	audits, err := f.st.ListAuditEntries(context.Background(), 10)
	if err != nil || len(audits) != 0 {
		t.Fatalf("employee request audit rows=%d err=%v", len(audits), err)
	}
}

func performSuppressionDelete(t *testing.T, f outboundAccessFixture, id uuid.UUID, user *models.User, body io.ReadCloser) *httptest.ResponseRecorder {
	t.Helper()
	h := NewOutboundHandler(nil, f.st, zerolog.Nop())
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/suppression/"+id.String(), body)
	// The limiter must enforce streamed bodies without trusting Content-Length.
	req.ContentLength = -1
	for name, value := range outboundUserHeaders(t, user) {
		req.Header.Set(name, value)
	}
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", id.String())
	req = req.WithContext(withRouteContext(req, rctx))
	rr := httptest.NewRecorder()
	middleware.Auth(f.st, outboundTestJWTSecret, publicTenantIDForTests)(http.HandlerFunc(h.DeleteSuppression)).ServeHTTP(rr, req)
	return rr
}
