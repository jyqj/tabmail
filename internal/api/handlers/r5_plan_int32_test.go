package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/rs/zerolog"
	"tabmail/internal/api/middleware"
	"tabmail/internal/app"
	"tabmail/internal/models"
	"tabmail/internal/testutil"
)

var continuePlanFields = []string{"max_domains", "max_mailboxes_per_domain", "max_messages_per_mailbox", "max_message_bytes", "retention_hours", "rpm_limit", "daily_quota"}

// These final persistence ports use pgx's actual INT4 codec, matching the seven
// existing PostgreSQL columns. They are not a live PostgreSQL transaction test.
type continueInt32Store struct {
	*testutil.FakeStore
	writes, audits int
	persisted      map[string]*int
	failure        error
}

func continueInt32Values(t any) map[string]*int {
	raw, _ := json.Marshal(t)
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(raw, &fields)
	out := map[string]*int{}
	for _, key := range continuePlanFields {
		var n *int
		_ = json.Unmarshal(fields[key], &n)
		out[key] = n
	}
	return out
}
func (s *continueInt32Store) write(value any) error {
	s.writes++
	if s.failure != nil {
		return s.failure
	}
	fields := continueInt32Values(value)
	codec := pgtype.NewMap()
	for key, n := range fields {
		if n != nil {
			if _, err := codec.Encode(pgtype.Int4OID, pgtype.BinaryFormatCode, int64(*n), nil); err != nil {
				return fmt.Errorf("%s: %w", key, err)
			}
		}
	}
	s.persisted = fields
	return nil
}
func (s *continueInt32Store) CreatePlan(_ context.Context, p *models.Plan) error { return s.write(p) }
func (s *continueInt32Store) UpdatePlan(_ context.Context, p *models.Plan) error { return s.write(p) }
func (s *continueInt32Store) UpsertOverride(_ context.Context, p *models.TenantOverride) error {
	return s.write(p)
}
func (s *continueInt32Store) InsertAudit(_ context.Context, _ *models.AuditEntry) error {
	s.audits++
	return nil
}

func continueInt32Call(t *testing.T, mode, operation string, body []byte, fail error) (*continueInt32Store, int, string, error) {
	t.Helper()
	f := newOutboundAccessFixture(t)
	st := &continueInt32Store{FakeStore: f.st, failure: fail}
	h := NewAdminHandler(st, nil, models.SMTPPolicy{}, nil, nil, zerolog.Nop())
	planID := uuid.New()
	if mode == "service" {
		var e error
		if operation == "override" {
			var v models.TenantOverride
			if err := json.Unmarshal(body, &v); err != nil {
				t.Fatal(err)
			}
			_, e = h.service.UpdateTenantOverride(context.Background(), f.tenantID, v, "synthetic-admin")
		} else {
			var v models.Plan
			if err := json.Unmarshal(body, &v); err != nil {
				t.Fatal(err)
			}
			v.ID = planID
			if operation == "create" {
				_, e = h.service.CreatePlan(context.Background(), &v, "synthetic-admin")
			} else {
				_, e = h.service.UpdatePlan(context.Background(), &v, "synthetic-admin")
			}
		}
		status := http.StatusOK
		if operation == "create" {
			status = http.StatusCreated
		}
		if e != nil {
			status = http.StatusInternalServerError
			if a, ok := app.As(e); ok && a.Kind == app.KindBadRequest {
				status = http.StatusBadRequest
			}
		}
		return st, status, fmt.Sprint(e), e
	}
	method, path, call := http.MethodPost, "/api/v1/admin/plans", h.CreatePlan
	if operation == "update" {
		method, path, call = http.MethodPatch, "/api/v1/admin/plans/"+planID.String(), h.UpdatePlan
	}
	if operation == "override" {
		method, path, call = http.MethodPatch, "/api/v1/admin/tenants/"+f.tenantID.String(), h.UpdateTenantOverride
	}
	r := httptest.NewRequest(method, path, bytes.NewReader(body))
	for k, v := range outboundUserHeaders(t, f.platformAdmin) {
		r.Header.Set(k, v)
	}
	route := chi.NewRouteContext()
	if operation == "override" {
		route.URLParams.Add("id", f.tenantID.String())
	} else {
		route.URLParams.Add("id", planID.String())
	}
	r = r.WithContext(withRouteContext(r, route))
	state := middleware.NewAuthState(nil)
	w := httptest.NewRecorder()
	middleware.Auth(st, outboundTestJWTSecret, publicTenantIDForTests, state)(middleware.RequireSuperAdmin(http.HandlerFunc(call))).ServeHTTP(w, r)
	if err := state.StopContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	return st, w.Code, w.Body.String(), nil
}
func continueInt32Body(t *testing.T, values map[string]any) []byte {
	t.Helper()
	body := map[string]any{"name": "synthetic-plan"}
	for k, v := range values {
		body[k] = v
	}
	raw, e := json.Marshal(body)
	if e != nil {
		t.Fatal(e)
	}
	return raw
}
func continueInt32OperationBody(t *testing.T, operation string, values map[string]any) []byte {
	if operation != "override" {
		return continueInt32Body(t, values)
	}
	raw, e := json.Marshal(values)
	if e != nil {
		t.Fatal(e)
	}
	return raw
}

func TestContinuePlanInt32RejectsBeforePersistence(t *testing.T) {
	for _, mode := range []string{"service", "http"} {
		for _, operation := range []string{"create", "update", "override"} {
			for _, field := range continuePlanFields {
				for _, n := range []int64{int64(math.MinInt32) - 1, int64(math.MaxInt32) + 1} {
					t.Run(fmt.Sprintf("%s/%s/%s/%d", mode, operation, field, n), func(t *testing.T) {
						body := continueInt32OperationBody(t, operation, map[string]any{field: n})
						st, status, response, _ := continueInt32Call(t, mode, operation, body, nil)
						if status != http.StatusBadRequest || !strings.Contains(response, field) {
							t.Errorf("status=%d response=%s; want field-specific 400", status, response)
						}
						if st.writes != 0 || st.audits != 0 || st.persisted != nil {
							t.Errorf("invalid value reached persistence: writes=%d audits=%d values=%v", st.writes, st.audits, st.persisted)
						}
					})
				}
			}
		}
	}
}
func TestContinuePlanInt32PreservesValues(t *testing.T) {
	for _, mode := range []string{"service", "http"} {
		for _, operation := range []string{"create", "update", "override"} {
			for _, tc := range []struct {
				name         string
				n, retention int
			}{
				{"minimum", math.MinInt32, -1}, {"maximum", math.MaxInt32, 3000000}, {"zero", 0, 0}, {"negative", -1, -1}, {"ordinary", 37, 37}, {"negative-retention", 37, -3000000},
			} {
				t.Run(mode+"/"+operation+"/"+tc.name, func(t *testing.T) {
					values := map[string]any{}
					for _, field := range continuePlanFields {
						values[field] = tc.n
					}
					values["retention_hours"] = tc.retention
					st, status, response, _ := continueInt32Call(t, mode, operation, continueInt32OperationBody(t, operation, values), nil)
					want := http.StatusOK
					if operation == "create" {
						want = http.StatusCreated
					}
					if status != want || st.writes != 1 || st.audits != 1 {
						t.Fatalf("status=%d body=%s writes=%d audits=%d", status, response, st.writes, st.audits)
					}
					for field, n := range values {
						if st.persisted[field] == nil || *st.persisted[field] != n.(int) {
							t.Errorf("changed %s: got %v want %d", field, st.persisted[field], n)
						}
					}
				})
			}
		}
	}
}
func TestContinuePlanInt32KeepsInheritanceAndFailure(t *testing.T) {
	for _, mode := range []string{"service", "http"} {
		for _, body := range []string{`{}`, `{"max_domains":null,"max_mailboxes_per_domain":null,"max_messages_per_mailbox":null,"max_message_bytes":null,"retention_hours":null,"rpm_limit":null,"daily_quota":null}`, `{"max_domains":0,"daily_quota":-1}`} {
			t.Run(mode+"/inherit/"+body, func(t *testing.T) {
				st, status, response, _ := continueInt32Call(t, mode, "override", []byte(body), nil)
				if status != http.StatusOK || st.writes != 1 || st.audits != 1 || !reflect.DeepEqual(st.persisted, continueInt32Values(json.RawMessage(body))) {
					t.Fatalf("inheritance changed: status=%d body=%s values=%v", status, response, st.persisted)
				}
			})
		}
		for _, operation := range []string{"create", "update", "override"} {
			t.Run(mode+"/failure/"+operation, func(t *testing.T) {
				cause := errors.New("synthetic write failure")
				st, status, response, e := continueInt32Call(t, mode, operation, continueInt32OperationBody(t, operation, map[string]any{"daily_quota": 3}), cause)
				if status != http.StatusInternalServerError || st.writes != 1 || st.audits != 0 || st.persisted != nil {
					t.Fatalf("failure changed: status=%d body=%s writes=%d audits=%d", status, response, st.writes, st.audits)
				}
				if mode == "service" && !errors.Is(e, cause) {
					t.Fatalf("lost cause: %v", e)
				}
			})
		}
	}
}
func TestContinuePlanInt32KeepsJSONDecoder(t *testing.T) {
	for _, operation := range []string{"create", "update", "override"} {
		for _, value := range []string{`1.5`, `"4"`, `9223372036854775808`, `[]`} {
			t.Run(operation+"/"+value, func(t *testing.T) {
				body := `{"max_domains":` + value + `}`
				if operation != "override" {
					body = `{"name":"synthetic-plan","max_domains":` + value + `}`
				}
				st, status, response, _ := continueInt32Call(t, "http", operation, []byte(body), nil)
				if status != http.StatusBadRequest || st.writes != 0 || st.audits != 0 {
					t.Fatalf("decoder changed: status=%d body=%s writes=%d audits=%d", status, response, st.writes, st.audits)
				}
			})
		}
	}
}
