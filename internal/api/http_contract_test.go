package api_test

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"tabmail/internal/api"
	"tabmail/internal/api/middleware"
	"tabmail/internal/authz"
	"tabmail/internal/company"
	"tabmail/internal/config"
	"tabmail/internal/models"
	"tabmail/internal/outbound"
	"tabmail/internal/policy"
	"tabmail/internal/store"
	"tabmail/internal/testpg"
	"tabmail/internal/testutil"
)

// These are responses captured from the real router over loopback HTTP, not
// fabricated JSON examples. PostgreSQL is the production adapter; object bytes
// and Redis are disposable local test adapters. No SMTP worker is started.
type contractHTTPResponse struct {
	ID      string            `json:"id"`
	Method  string            `json:"method"`
	Path    string            `json:"path"`
	Route   string            `json:"route"`
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers"`
	Body    []byte            `json:"body_base64"`
}

type contractRoute struct {
	Method  string `json:"method"`
	Path    string `json:"path"`
	Handler string `json:"handler"`
}

func contractData[T any](t *testing.T, r contractHTTPResponse) T {
	t.Helper()
	var envelope struct {
		Data T `json:"data"`
	}
	if err := json.Unmarshal(r.Body, &envelope); err != nil {
		t.Fatal(err)
	}
	return envelope.Data
}

func TestCompanyHTTPContract(t *testing.T) {
	st, pool, _ := testpg.NewPostgres(t)
	ctx := context.Background()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	tenant := &models.Tenant{Name: "HTTP Contract Co", PlanID: uuid.MustParse("00000000-0000-0000-0000-000000000002")}
	must(st.CreateTenant(ctx, tenant))
	admin := &models.User{TenantID: tenant.ID, Email: "http-admin@contact.test", DisplayName: "Contract Admin", Role: models.RoleSuperAdmin, IsActive: true, PasswordHash: "synthetic-non-login-hash"}
	must(st.CreateUser(ctx, admin))
	actor := authz.Actor{Type: authz.PrincipalUser, ID: admin.ID, TenantID: tenant.ID, Role: models.RoleSuperAdmin, IsSuperAdmin: true}
	zone := &models.DomainZone{TenantID: tenant.ID, Domain: "contract.test", IsVerified: true, MXVerified: true}
	must(st.CreateZone(ctx, zone))
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	objects := testutil.NewMemoryObjectStore()
	out := outbound.NewService(config.Outbound{Enabled: true, Mode: "relay"}, st, st, zerolog.Nop())
	out.SetObjectStore(objects)
	router := api.NewRouter(api.RouterConfig{Store: st, ObjectStore: objects, JWTSecret: "jwt-test-secret", MailboxTokenSecret: "mailbox-secret", PublicTenantID: publicTenantID, NamingMode: policy.NamingFull, StripPlus: true, HTTP: config.HTTP{CookieSecure: true}, RateLimiter: middleware.NewRateLimiter(rdb, st, 10000, nil), CompanyRepository: st, OutboundService: out, Logger: zerolog.Nop()})
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)
	client := server.Client()
	client.Timeout = 10 * time.Second

	inventory, err := os.ReadFile("../../docs/company-mail/evidence/R5-API-MATRIX.json")
	must(err)
	var routes []contractRoute
	must(json.Unmarshal(inventory, &routes))
	sort.SliceStable(routes, func(i, j int) bool { return strings.Count(routes[i].Path, "{") < strings.Count(routes[j].Path, "{") })
	match := func(method, path string) string {
		path = strings.Split(path, "?")[0]
		for _, route := range routes {
			if route.Method != method || !strings.HasPrefix(route.Path, "/api/v1/company/") {
				continue
			}
			a, b := strings.Split(route.Path, "/"), strings.Split(path, "/")
			if len(a) != len(b) {
				continue
			}
			ok := true
			for i := range a {
				if a[i] != b[i] && !(strings.HasPrefix(a[i], "{") && b[i] != "") {
					ok = false
					break
				}
			}
			if ok {
				return route.Path
			}
		}
		t.Fatalf("unregistered contract request %s %s", method, path)
		return ""
	}
	var captures []contractHTTPResponse
	seen := map[string]bool{}
	rawCall := func(id, method, path, ct string, body []byte, user *models.User, want int) contractHTTPResponse {
		t.Helper()
		if seen[id] {
			t.Fatalf("duplicate capture id %q", id)
		}
		seen[id] = true
		req, e := http.NewRequestWithContext(ctx, method, server.URL+path, bytes.NewReader(body))
		must(e)
		if user != nil {
			req.Header.Set("Authorization", "Bearer "+issueAccessTokenForExistingUser(t, user))
		}
		if ct != "" {
			req.Header.Set("Content-Type", ct)
		}
		if strings.HasSuffix(path, "/submit") {
			req.Header.Set("Idempotency-Key", "http-contract-submit-once")
		}
		res, e := client.Do(req)
		must(e)
		var data []byte
		if strings.HasPrefix(res.Header.Get("Content-Type"), "text/event-stream") {
			reader := bufio.NewReader(res.Body)
			for len(data) < 65536 {
				line, readErr := reader.ReadBytes('\n')
				data = append(data, line...)
				if readErr != nil {
					must(readErr)
				}
				if bytes.HasSuffix(data, []byte("\n\n")) {
					break
				}
			}
		} else {
			data, e = io.ReadAll(io.LimitReader(res.Body, 2*1024*1024+1))
			must(e)
		}
		must(res.Body.Close())
		if len(data) > 2*1024*1024 {
			t.Fatal("oversized synthetic response")
		}
		if res.StatusCode != want {
			t.Fatalf("%s: status=%d want=%d body=%s", id, res.StatusCode, want, data)
		}
		headers := map[string]string{}
		for _, key := range []string{"Content-Type", "Cache-Control", "Content-Disposition", "X-Content-Type-Options", "X-Accel-Buffering"} {
			if v := res.Header.Get(key); v != "" {
				headers[key] = v
			}
		}
		r := contractHTTPResponse{id, method, path, match(method, path), res.StatusCode, headers, data}
		captures = append(captures, r)
		return r
	}
	call := func(id, method, path string, body any, user *models.User, want int) contractHTTPResponse {
		t.Helper()
		var data []byte
		if body != nil {
			var e error
			data, e = json.Marshal(body)
			must(e)
		}
		return rawCall(id, method, path, "application/json", data, user, want)
	}
	const base = "/api/v1/company"
	call("settings.unconfigured", "GET", base+"/settings", nil, admin, 200)
	call("invitations.empty", "GET", base+"/invitations", nil, admin, 200)
	call("settings.configure", "PUT", base+"/settings", map[string]any{"name": tenant.Name, "primary_zone_id": zone.ID}, admin, 200)
	call("domains.list", "GET", base+"/domains", nil, admin, 200)
	domain := contractData[company.Domain](t, call("domains.create", "POST", base+"/domains", map[string]string{"domain": "remove.contract.test"}, admin, 201))
	call("domains.delete", "DELETE", base+"/domains/"+domain.ID.String(), nil, admin, 200)

	invite := contractData[struct {
		Invitation company.Invitation `json:"invitation"`
		Token      string             `json:"activation_token"`
	}](t,
		call("invitations.create", "POST", base+"/invitations", map[string]string{"email": "http-worker@contact.test", "local_part": "worker", "display_name": "Worker"}, admin, 200))
	call("invitations.activate", "POST", base+"/activate", map[string]string{"token": invite.Token, "password": "synthetic-password-for-fixture"}, nil, 200)
	worker, err := st.GetUserByEmail(ctx, "http-worker@contact.test")
	must(err)
	if worker == nil {
		t.Fatal("activation did not create a worker")
	}
	mb, err := st.GetMailboxByAddress(ctx, "worker@contract.test")
	must(err)
	if mb == nil {
		t.Fatal("activation did not provision mailbox")
	}
	mbp := base + "/mailboxes/" + mb.ID.String()
	second := contractData[struct {
		Invitation company.Invitation `json:"invitation"`
	}](t,
		call("invitations.second", "POST", base+"/invitations", map[string]string{"email": "revoke@contact.test", "local_part": "revoke", "display_name": "Revoke"}, admin, 200))
	call("invitations.revoke", "DELETE", base+"/invitations/"+second.Invitation.ID.String(), nil, admin, 200)
	call("invitations.list", "GET", base+"/invitations", nil, admin, 200)
	call("mailboxes.list", "GET", base+"/mailboxes", nil, worker, 200)
	shared := contractData[models.Mailbox](t, call("mailboxes.create", "POST", base+"/mailboxes", map[string]any{"local_part": "shared", "kind": "shared"}, admin, 200))
	revision := func(id uuid.UUID) int64 {
		t.Helper()
		a, e := st.GetWorkMailbox(ctx, actor, id)
		must(e)
		return a.Revision
	}
	call("grants.empty", "GET", mbp+"/grants", nil, admin, 200)
	call("grants.update", "PUT", mbp+"/grants", map[string]any{"revision": revision(mb.ID), "user_id": admin.ID, "can_read": true, "can_organize": false, "can_send": false, "template_only": false}, admin, 200)
	call("grants.list", "GET", mbp+"/grants", nil, admin, 200)
	oldRevision := revision(mb.ID)
	call("policy.update", "PUT", mbp+"/send-policy", map[string]any{"revision": oldRevision, "send_policy": "free"}, admin, 200)
	call("policy.conflict", "PUT", mbp+"/send-policy", map[string]any{"revision": oldRevision, "send_policy": "disabled"}, admin, 409)
	call("access.explain", "GET", mbp+"/access/"+worker.ID.String(), nil, admin, 200)

	call("templates.empty", "GET", base+"/templates", nil, admin, 200)
	templateBody := map[string]any{"subject": "Fixture notice", "text_body": "Synthetic text"}
	template := contractData[company.Template](t, call("templates.create", "POST", base+"/templates", map[string]any{"name": "Fixture template", "draft": templateBody}, admin, 200))
	tp := base + "/templates/" + template.ID.String()
	call("templates.versions.empty", "GET", tp+"/versions", nil, admin, 200)
	template = contractData[company.Template](t, call("templates.update", "PUT", tp, map[string]any{"name": "Updated fixture", "draft": templateBody, "revision": template.Revision}, admin, 200))
	version := contractData[company.TemplateVersion](t, call("templates.publish", "POST", tp+"/publish", map[string]int{"revision": template.Revision}, admin, 200))
	call("templates.grant", "PUT", tp+"/grants", map[string]any{"mailbox_id": mb.ID, "user_id": worker.ID, "enabled": true}, admin, 200)
	call("templates.grants", "GET", tp+"/grants", nil, admin, 200)
	call("templates.versions", "GET", tp+"/versions", nil, admin, 200)
	call("templates.usable", "GET", mbp+"/templates", nil, worker, 200)
	call("templates.preview", "POST", base+"/templates/preview", map[string]any{"mailbox_id": mb.ID, "template_version_id": version.ID, "vars": map[string]string{}}, worker, 200)

	call("drafts.empty", "GET", base+"/drafts", nil, worker, 200)
	call("messages.empty", "GET", mbp+"/messages", nil, worker, 200)
	var upload bytes.Buffer
	form := multipart.NewWriter(&upload)
	part, err := form.CreateFormFile("file", "fixture.txt")
	must(err)
	_, err = part.Write([]byte("upload-fixture-bytes"))
	must(err)
	must(form.Close())
	attachment := contractData[company.Attachment](t, rawCall("attachments.upload", "POST", mbp+"/attachments", form.FormDataContentType(), upload.Bytes(), worker, 200))
	download := call("attachments.download", "GET", base+"/attachments/"+attachment.ID.String(), nil, worker, 200)
	if string(download.Body) != "upload-fixture-bytes" {
		t.Fatal("upload bytes changed")
	}
	draft := contractData[company.Draft](t, call("draft.incomplete", "POST", base+"/drafts", map[string]any{"mailbox_id": mb.ID}, worker, 200))
	dp := base + "/drafts/" + draft.ID.String()
	call("draft.get", "GET", dp, nil, worker, 200)
	call("draft.list", "GET", base+"/drafts", nil, worker, 200)
	payload := map[string]any{"to": []string{"recipient@client.test"}, "subject": "Synthetic mail", "text_body": "Only synthetic message content", "attachment_ids": []uuid.UUID{attachment.ID}}
	draft = contractData[company.Draft](t, call("draft.update", "PUT", dp, map[string]any{"mailbox_id": mb.ID, "revision": draft.Revision, "payload": payload}, worker, 200))
	job := contractData[models.OutboundJob](t, call("submit.created", "POST", dp+"/submit", map[string]int{"expected_revision": draft.Revision}, worker, 201))
	call("submit.replayed", "POST", dp+"/submit", map[string]int{"expected_revision": draft.Revision}, worker, 200)
	sp := base + "/submissions/" + job.ID.String()
	call("submissions.list", "GET", base+"/submissions", nil, worker, 200)
	call("submissions.get", "GET", sp, nil, worker, 200)
	call("submissions.content", "GET", sp+"/content", nil, worker, 200)
	call("submissions.attachments", "GET", sp+"/attachments", nil, worker, 200)
	download = call("submissions.download", "GET", sp+"/attachments/"+attachment.ID.String()+"/download", nil, worker, 200)
	if string(download.Body) != "upload-fixture-bytes" {
		t.Fatal("pinned download changed")
	}
	call("recipients.list", "GET", base+"/outbound/"+job.ID.String()+"/recipients", nil, worker, 200)
	archived := contractData[[]company.ArchivedMail](t, call("archive.list", "GET", mbp+"/sent", nil, worker, 200))
	if len(archived) != 1 {
		t.Fatalf("sent asset count=%d", len(archived))
	}
	call("archive.change", "POST", mbp+"/sent/"+archived[0].ID.String()+"/actions", map[string]any{"action": "archive", "revision": archived[0].Revision}, worker, 200)
	otherDraft := contractData[company.Draft](t, call("draft.second", "POST", base+"/drafts", map[string]any{"mailbox_id": mb.ID}, worker, 200))
	call("draft.delete", "DELETE", base+"/drafts/"+otherDraft.ID.String()+"?revision=1", nil, worker, 200)

	raw := []byte("From: sender@client.test\r\nTo: worker@contract.test\r\nSubject: Synthetic incoming mail\r\nMessage-ID: <http-contract@client.test>\r\nMIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=contract-part\r\n\r\n--contract-part\r\nContent-Type: text/plain\r\n\r\nSynthetic incoming body\r\n--contract-part\r\nContent-Type: text/plain\r\nContent-Disposition: attachment; filename=hello.txt\r\nContent-Transfer-Encoding: base64\r\n\r\naGVsbG8=\r\n--contract-part--\r\n")
	must(objects.Put(ctx, "http-contract-source", bytes.NewReader(raw), int64(len(raw))))
	message := &models.Message{TenantID: tenant.ID, MailboxID: mb.ID, ZoneID: zone.ID, Sender: "sender@client.test", Recipients: []string{mb.FullAddress}, Subject: "Synthetic incoming mail", RawObjectKey: "http-contract-source", Size: int64(len(raw))}
	must(st.CreateMessage(ctx, message))
	mp := mbp + "/messages/" + message.ID.String()
	call("messages.list", "GET", mbp+"/messages", nil, worker, 200)
	call("message.detail", "GET", mp, nil, worker, 200)
	call("message.conversation", "GET", mp+"/conversation", nil, worker, 200)
	parts := contractData[[]company.ParsedAttachment](t, call("message.attachments", "GET", mp+"/attachments", nil, worker, 200))
	if len(parts) != 1 {
		t.Fatalf("MIME attachment count=%d", len(parts))
	}
	download = call("message.attachment.index", "GET", mp+"/attachments/0", nil, worker, 200)
	if string(download.Body) != "hello" {
		t.Fatal("MIME attachment bytes changed")
	}
	download = call("message.attachment.id", "GET", mp+"/parts/"+parts[0].ID, nil, worker, 200)
	if string(download.Body) != "hello" {
		t.Fatal("stable-ID MIME attachment bytes changed")
	}
	source := call("message.source", "GET", mp+"/source", nil, worker, 200)
	if !bytes.Equal(source.Body, raw) {
		t.Fatal("original source changed")
	}
	call("message.compose", "POST", mp+"/compose", map[string]any{"mode": "reply", "from_mailbox_id": mb.ID}, worker, 200)
	call("message.action", "POST", mp+"/actions", map[string]string{"action": "archive"}, worker, 200)
	call("events.ready", "GET", mbp+"/events", nil, worker, 200)
	call("index.status", "GET", mbp+"/index-status", nil, worker, 200)
	call("index.retry", "POST", base+"/index/retry", map[string]string{"reason": "Synthetic derived-index verification"}, admin, 200)

	// A held, never-networked receipt exercises recovery against real persisted
	// identities and original-byte integrity, without contacting public SMTP.
	jid := uuid.New()
	ingress := &models.IngestJob{ID: jid, Source: "smtp", RemoteIP: "127.0.0.1", MailFrom: "sender@client.test", Recipients: []string{mb.FullAddress}, RawObjectKey: "ingress-" + jid.String() + ".eml", Metadata: json.RawMessage(`{}`)}
	must(objects.Put(ctx, ingress.RawObjectKey, bytes.NewReader(raw), int64(len(raw))))
	sum := sha256.Sum256(raw)
	must(st.CreateIngress(ctx, ingress, []store.IngressTarget{{MailboxID: mb.ID, TenantID: tenant.ID, ZoneID: zone.ID, Address: mb.FullAddress}}, hex.EncodeToString(sum[:]), int64(len(raw))))
	_, err = pool.Exec(ctx, `UPDATE ingest_jobs SET state='dead' WHERE id=$1`, jid)
	must(err)
	call("recovery.list", "GET", base+"/recovery", nil, admin, 200)
	inspected := contractData[struct {
		Receipt company.RecoveryReceipt `json:"receipt"`
		Valid   bool                    `json:"original_valid"`
	}](t, call("recovery.inspect", "POST", base+"/recovery/"+jid.String()+"/inspect", map[string]string{"reason": "Synthetic original integrity check"}, admin, 200))
	if !inspected.Valid {
		t.Fatal("synthetic original failed integrity")
	}
	call("recovery.retry", "POST", base+"/recovery/"+jid.String()+"/retry", map[string]any{"updated_at": inspected.Receipt.UpdatedAt, "targets": []uuid.UUID{mb.ID}, "reason": "Synthetic scoped recovery verification"}, admin, 200)
	uncertain := &models.OutboundJob{TenantID: tenant.ID, ZoneID: zone.ID, UserID: &worker.ID, SenderUserID: &worker.ID, SenderMailboxID: &mb.ID, MailFrom: mb.FullAddress, To: []string{"uncertain@client.test"}, RcptTo: []string{"uncertain@client.test"}, Subject: "Synthetic uncertain fixture", State: models.OutboundFailed}
	must(st.CreateOutboundJob(ctx, uncertain))
	_, err = pool.Exec(ctx, `UPDATE outbound_jobs SET in_flight_domain='rcpt:uncertain@client.test',updated_at=clock_timestamp() WHERE id=$1`, uncertain.ID)
	must(err)
	_, err = pool.Exec(ctx, `UPDATE outbound_recipients SET state='uncertain' WHERE job_id=$1`, uncertain.ID)
	must(err)
	uncertain, err = st.GetOutboundJob(ctx, uncertain.ID)
	must(err)
	up := base + "/outbound/" + uncertain.ID.String()
	call("outbound.inspect", "POST", up+"/inspect", map[string]string{"reason": "Synthetic operator evidence inspection"}, admin, 200)
	call("outbound.reconcile", "POST", up+"/reconcile", map[string]any{"updated_at": uncertain.UpdatedAt, "results": []map[string]string{{"address": "uncertain@client.test", "state": "accepted"}}, "reason": "Synthetic receipt confirms fixture acceptance"}, admin, 200)

	other := &models.User{TenantID: tenant.ID, Email: "departing@contact.test", DisplayName: "Departing", Role: models.RoleUser, IsActive: true, PasswordHash: "synthetic-non-login-hash"}
	must(st.CreateUser(ctx, other))
	personal := contractData[models.Mailbox](t, call("mailboxes.personal", "POST", base+"/mailboxes", map[string]any{"local_part": "handover", "kind": "personal", "owner_user_id": other.ID}, admin, 200))
	call("mailboxes.handover", "POST", base+"/mailboxes/"+personal.ID.String()+"/handover", map[string]any{"revision": revision(personal.ID), "owner_user_id": worker.ID, "reason": "Synthetic ownership transfer verification"}, admin, 200)
	// Construct legacy data only in the disposable DB to exercise conversion.
	_, err = pool.Exec(ctx, `UPDATE mailboxes SET mailbox_kind='legacy',password_hash='synthetic-unused-legacy-hash' WHERE id=$1`, shared.ID)
	must(err)
	call("mailboxes.convert", "POST", base+"/mailboxes/"+shared.ID.String()+"/convert-shared", map[string]any{"revision": revision(shared.ID), "reason": "Synthetic legacy conversion verification"}, admin, 200)
	plan := contractData[company.OffboardingPlan](t, call("offboard.preview", "POST", base+"/employees/"+other.ID.String()+"/offboard/preview", map[string]any{"successor_user_id": worker.ID, "reason": "Synthetic departure process verification"}, admin, 200))
	call("offboard.execute", "POST", base+"/employees/"+other.ID.String()+"/offboard", map[string]any{"plan_id": plan.ID}, admin, 200)
	var templateRevision int
	must(pool.QueryRow(ctx, `SELECT revision FROM mail_templates WHERE id=$1`, template.ID).Scan(&templateRevision))
	call("templates.revoke", "POST", tp+"/versions/1/revoke", map[string]int{"revision": templateRevision}, admin, 200)
	must(pool.QueryRow(ctx, `SELECT revision FROM mail_templates WHERE id=$1`, template.ID).Scan(&templateRevision))
	call("templates.retire", "POST", tp+"/retire", map[string]any{"revision": templateRevision, "retired": true}, admin, 200)
	call("overview.get", "GET", base+"/overview", nil, admin, 200)
	call("audit.list", "GET", base+"/audit", nil, admin, 200)
	// The administrative stream is a distinct route from mailbox events. Read
	// its real ready frame through the existing bounded SSE capture branch.
	adminReady := call("company.events.ready", "GET", base+"/events", nil, admin, 200)
	if strings.TrimSpace(strings.Split(adminReady.Headers["Content-Type"], ";")[0]) != "text/event-stream" || adminReady.Headers["X-Accel-Buffering"] != "no" {
		t.Fatal("company events did not return the non-buffered SSE media contract")
	}
	cache := map[string]bool{}
	for _, directive := range strings.Split(adminReady.Headers["Cache-Control"], ",") {
		cache[strings.ToLower(strings.TrimSpace(directive))] = true
	}
	if !cache["private"] || !cache["no-store"] || !cache["no-transform"] || cache["public"] {
		t.Fatal("company events lost private non-storable stream headers")
	}
	const readyPrefix = "event: ready\ndata: "
	if !bytes.HasPrefix(adminReady.Body, []byte(readyPrefix)) || !bytes.HasSuffix(adminReady.Body, []byte("\n\n")) {
		t.Fatal("company events capture is not a complete ready frame")
	}
	var readyScope struct {
		TenantID uuid.UUID `json:"tenant_id"`
	}
	readyJSON := adminReady.Body[len(readyPrefix) : len(adminReady.Body)-2]
	readyDecoder := json.NewDecoder(bytes.NewReader(readyJSON))
	readyDecoder.DisallowUnknownFields()
	must(readyDecoder.Decode(&readyScope))
	if readyScope.TenantID != tenant.ID {
		t.Fatal("company events ready frame has the wrong tenant scope")
	}
	if e := readyDecoder.Decode(new(any)); e != io.EOF {
		t.Fatal("company events ready frame contains trailing data")
	}

	call("error.unauthenticated", "GET", base+"/submissions", nil, nil, 401)
	call("error.forbidden", "GET", base+"/domains", nil, worker, 403)
	call("error.bad-request", "GET", base+"/drafts/not-a-uuid", nil, worker, 400)
	call("error.not-found", "GET", base+"/submissions/"+uuid.NewString()+"/content", nil, worker, 404)
	// Fault injection is deliberately last and only affects this test database.
	_, err = pool.Exec(ctx, `DROP TABLE company_settings`)
	must(err)
	call("error.internal", "GET", base+"/settings", nil, admin, 500)

	// The two live-DNS operations are not covered by this loopback-only journey.
	// All other company operations must execute a successful HTTP response.
	successes := map[string]bool{}
	for _, c := range captures {
		if c.Status >= 200 && c.Status < 300 {
			successes[c.Method+" "+c.Route] = true
		}
	}
	for _, route := range routes {
		if !strings.HasPrefix(route.Path, base+"/") || route.Handler == "c.Domains.Verify" || route.Handler == "c.Domains.Verification" {
			continue
		}
		if !successes[route.Method+" "+route.Path] {
			t.Errorf("missing successful HTTP operation: %s %s", route.Method, route.Path)
		}
	}
	if t.Failed() {
		return
	}
	if path := os.Getenv("TABMAIL_HTTP_CONTRACT_CAPTURE"); path != "" {
		spec, e := os.ReadFile("openapi.yaml")
		must(e)
		hash := sha256.Sum256(spec)
		data, e := json.MarshalIndent(struct {
			Version    int                    `json:"version"`
			Producer   string                 `json:"producer"`
			SpecSHA256 string                 `json:"spec_sha256"`
			Cases      []contractHTTPResponse `json:"cases"`
		}{1, "TestCompanyHTTPContract", hex.EncodeToString(hash[:]), captures}, "", "  ")
		must(e)
		f, e := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		must(e)
		_, e = f.Write(append(data, '\n'))
		closeErr := f.Close()
		must(e)
		must(closeErr)
	}
	t.Logf("HTTP_CONTRACT: %d captured responses, %d successful operations, live DNS operations excluded=2", len(captures), len(successes))
}
