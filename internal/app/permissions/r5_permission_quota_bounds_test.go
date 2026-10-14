package permissions

import (
	"context"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"
	"tabmail/internal/app"
	"tabmail/internal/authz"
	"tabmail/internal/company"
)

// Use the production command decoders and service together. Only the final
// persistence port is a spy: malformed quotas must not reach either writer.
// This is not a PostgreSQL transaction or HTTP middleware acceptance test.
type quotaBoundaryPort struct {
	Store
	EditorStore
	ProfileCASStore
	writes int
	patch  company.PermissionPatch
	result *company.PermissionEditorSnapshot
}

func (s *quotaBoundaryPort) PatchPermissionEditor(_ context.Context, _ authz.Actor, _ uuid.UUID, cmd company.PermissionEditorCommand) (*company.PermissionEditorSnapshot, error) {
	s.writes++
	s.patch = cmd.Patch
	return s.result, nil
}

func (s *quotaBoundaryPort) AssignPermissionEditor(_ context.Context, _ authz.Actor, _ uuid.UUID, cmd company.PermissionAssignmentCommand) (*company.PermissionEditorSnapshot, error) {
	s.writes++
	s.patch = cmd.Patch
	return s.result, nil
}

var quotaBoundaryFields = []struct {
	name string
	get  func(*company.PermissionPatch) *company.PermissionField[int]
}{
	{"daily_send_quota", func(p *company.PermissionPatch) *company.PermissionField[int] { return &p.DailySendQuota }},
	{"daily_receive_quota", func(p *company.PermissionPatch) *company.PermissionField[int] { return &p.DailyReceiveQuota }},
	{"max_mailboxes", func(p *company.PermissionPatch) *company.PermissionField[int] { return &p.MaxMailboxes }},
	{"max_domains", func(p *company.PermissionPatch) *company.PermissionField[int] { return &p.MaxDomains }},
}

func quotaBoundaryFixture() (*Service, *quotaBoundaryPort, authz.Actor, company.PermissionRevision) {
	tenant, user := uuid.New(), uuid.New()
	port := &quotaBoundaryPort{result: &company.PermissionEditorSnapshot{UserID: user, TenantID: tenant}}
	actor := authz.Actor{Type: authz.PrincipalUser, ID: uuid.New(), TenantID: tenant, IsAdmin: true}
	return New(port), port, actor, company.PermissionRevision{UserID: user, TenantID: tenant, UserRevision: "7"}
}

func TestR5PermissionQuotaWireBounds(t *testing.T) {
	for _, command := range []string{"patch", "assignment"} {
		for _, field := range quotaBoundaryFields {
			for _, tc := range []struct {
				name, raw string
				valid     bool
				want      company.PermissionField[int]
			}{
				{"above_postgres_integer", "2147483648", false, company.PermissionField[int]{}},
				{"maximum_go_integer", "9223372036854775807", false, company.PermissionField[int]{}},
				{"negative", "-1", false, company.PermissionField[int]{}},
				{"maximum_postgres_integer", "2147483647", true, company.PermissionField[int]{Present: true, Value: 2147483647}},
				{"zero", "0", true, company.PermissionField[int]{Present: true}},
				{"null", "null", true, company.PermissionField[int]{Present: true, Inherit: true}},
				{"omitted", "", true, company.PermissionField[int]{}},
			} {
				t.Run(command+"/"+field.name+"/"+tc.name, func(t *testing.T) {
					svc, port, actor, revision := quotaBoundaryFixture()
					patch := `"can_send":false,"domain_access":{"mode":"none","zone_ids":[]}`
					if tc.raw != "" {
						patch += fmt.Sprintf(",%q:%s", field.name, tc.raw)
					}
					body := fmt.Sprintf(`{"expected_revision":{"user_id":%q,"tenant_id":%q,"user_revision":"7","profile_id":null,"profile_revision":null},"patch":{%s}`, revision.UserID.String(), revision.TenantID.String(), patch)
					if command == "assignment" {
						body += `,"profile_id":null,"profile_revision":null`
					}
					body += "}"
					var out *company.PermissionEditorSnapshot
					var err error
					if command == "patch" {
						var cmd company.PermissionEditorCommand
						cmd, err = company.DecodePermissionEditorCommand(strings.NewReader(body))
						if err == nil {
							out, err = svc.PatchEditor(context.Background(), actor, &revision.TenantID, revision.UserID, cmd)
						}
					} else {
						var cmd company.PermissionAssignmentCommand
						cmd, err = company.DecodePermissionAssignmentCommand(strings.NewReader(body))
						if err == nil {
							out, err = svc.AssignEditor(context.Background(), actor, &revision.TenantID, revision.UserID, cmd)
						}
					}
					if !tc.valid {
						if err == nil || out != nil || port.writes != 0 {
							t.Fatalf("invalid quota reached persistence: err=%v out=%v writes=%d", err, out, port.writes)
						}
						return
					}
					if err != nil || out != port.result || port.writes != 1 || *field.get(&port.patch) != tc.want {
						t.Fatalf("valid quota intent changed: err=%v writes=%d field=%+v want=%+v", err, port.writes, *field.get(&port.patch), tc.want)
					}
					if !port.patch.CanSend.Present || port.patch.CanSend.Inherit || port.patch.CanSend.Value || port.patch.DomainAccess.Value.Mode != "none" {
						t.Fatal("quota update changed explicit send/domain denial")
					}
				})
			}
		}
	}
}

func TestR5PermissionQuotaServiceRejectsOverflowBeforePort(t *testing.T) {
	for _, command := range []string{"patch", "assignment"} {
		for _, field := range quotaBoundaryFields {
			for _, value := range []int64{2147483648, 9223372036854775807} {
				// A 32-bit caller cannot construct these typed int values; the wire
				// cases above still verify that its JSON decoder rejects them.
				if int64(int(value)) != value {
					continue
				}
				t.Run(command+"/"+field.name+"/"+strconv.FormatInt(value, 10), func(t *testing.T) {
					svc, port, actor, revision := quotaBoundaryFixture()
					patch := company.PermissionPatch{}
					*field.get(&patch) = company.PermissionField[int]{Present: true, Value: int(value)}
					before := patch
					var out *company.PermissionEditorSnapshot
					var err error
					if command == "patch" {
						out, err = svc.PatchEditor(context.Background(), actor, &revision.TenantID, revision.UserID, company.PermissionEditorCommand{ExpectedRevision: revision, Patch: patch})
					} else {
						out, err = svc.AssignEditor(context.Background(), actor, &revision.TenantID, revision.UserID, company.PermissionAssignmentCommand{ExpectedRevision: revision, Patch: patch})
					}
					classified, ok := app.As(err)
					if !ok || classified.Kind != app.KindBadRequest || out != nil || port.writes != 0 {
						t.Fatalf("overflow was not rejected before persistence: err=%v out=%v writes=%d", err, out, port.writes)
					}
					if !reflect.DeepEqual(patch, before) {
						t.Fatal("rejecting quota overflow mutated caller intent")
					}
				})
			}
		}
	}
}
