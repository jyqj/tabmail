//go:build r5fixtures

package testpg_test

import (
	"bytes"
	"context"
	"github.com/google/uuid"
	"io"
	"mime/multipart"
	"net/http"
	"tabmail/internal/company"
	"tabmail/internal/testpg"
	"tabmail/internal/testutil"
	"testing"
)

// Raw multipart is sent through the real listener; JSON Request cannot stand
// in for a file upload. This local helper does not modify the leased shared one.
func r5Multipart(t *testing.T, f *testpg.R5HTTPFixture, ctx context.Context, payload []byte) (int, []byte) {
	t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	file, err := form.CreateFormFile("file", "owned-fixture.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = file.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err = form.Close(); err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/company/mailboxes/" + f.Companies[0].Personal["sender"].ID.String() + "/attachments"
	request, err := http.NewRequestWithContext(ctx, "POST", f.Server.URL+path, &body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", form.FormDataContentType())
	request.Header.Set("Authorization", "Bearer "+f.JWT(0, "sender"))
	response, err := f.Server.Client().Do(request)
	if err != nil {
		t.Fatal("actual multipart transport failed", err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if err != nil {
		t.Fatal(err)
	}
	return response.StatusCode, raw
}

func TestR5FixtureHTTPObjectPutAndFinishFailurePreserveReservation(t *testing.T) {
	f := testpg.NewR5HTTPFixture(t)
	ctx := r5HTTPContext(t)
	c := f.Companies[0]
	payload := []byte("PRIVATE_SYNTHETIC_UPLOAD")
	if err := f.Objects.FailNext("put", testutil.ErrR5ObjectFault); err != nil {
		t.Fatal(err)
	}
	status, body := r5Multipart(t, f, ctx, payload)
	r5RequireStatus(t, status, 500)
	if bytes.Contains(body, payload) {
		t.Fatal("actual failed upload exposed private bytes")
	}
	var id uuid.UUID
	var key, state string
	var count int
	if err := f.Pool.QueryRow(ctx, `SELECT count(*) FROM mail_attachments WHERE tenant_id=$1 AND user_id=$2`, c.Tenant.ID, c.Users["sender"].ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatal("Put failure did not preserve exactly its owned reservation")
	}
	if err := f.Pool.QueryRow(ctx, `SELECT id,object_key,state FROM mail_attachments WHERE tenant_id=$1 AND user_id=$2 ORDER BY created_at DESC,id DESC LIMIT 1`, c.Tenant.ID, c.Users["sender"].ID).Scan(&id, &key, &state); err != nil {
		t.Fatal(err)
	}
	exists, err := f.Objects.Exists(ctx, key)
	if err != nil || exists || state != "uploading" {
		t.Fatal("failed Put invented object or ready row", err)
	}
	if _, err = f.Pool.Exec(ctx, `CREATE FUNCTION r5_fixture_finish_fault() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.state='ready' THEN RAISE EXCEPTION 'owned fixture attachment finish fault'; END IF; RETURN NEW; END $$; CREATE TRIGGER r5_fixture_finish_fault BEFORE UPDATE OF state ON mail_attachments FOR EACH ROW EXECUTE FUNCTION r5_fixture_finish_fault()`); err != nil {
		t.Fatal(err)
	}
	status, _ = r5Multipart(t, f, ctx, payload)
	r5RequireStatus(t, status, 500)
	if err = f.Pool.QueryRow(ctx, `SELECT id,object_key,state FROM mail_attachments WHERE tenant_id=$1 AND user_id=$2 ORDER BY created_at DESC,id DESC LIMIT 1`, c.Tenant.ID, c.Users["sender"].ID).Scan(&id, &key, &state); err != nil {
		t.Fatal(err)
	}
	exists, err = f.Objects.Exists(ctx, key)
	if err != nil || !exists || state != "uploading" {
		t.Fatal("ambiguous finish violated actual reservation/object retention contract", err)
	}
	if _, err = f.Pool.Exec(ctx, `DROP TRIGGER r5_fixture_finish_fault ON mail_attachments; DROP FUNCTION r5_fixture_finish_fault()`); err != nil {
		t.Fatal(err)
	}
	status, body = r5Multipart(t, f, ctx, payload)
	r5RequireStatus(t, status, 200)
	ready := r5Data[company.Attachment](t, body)
	if ready.State != "ready" || ready.Size != int64(len(payload)) {
		t.Fatal("normal upload did not restore actual ready metadata")
	}
}

func TestR5FixtureHTTPObjectShortReadIntegrityAndStableMetadata(t *testing.T) {
	f := testpg.NewR5HTTPFixture(t)
	ctx := r5HTTPContext(t)
	payload := []byte("PRIVATE_SYNTHETIC_SHORT_READ_CONTENT")
	status, body := r5Multipart(t, f, ctx, payload)
	r5RequireStatus(t, status, 200)
	attachment := r5Data[company.Attachment](t, body)
	var before, after []byte
	if err := f.Pool.QueryRow(ctx, `SELECT to_jsonb(a) FROM mail_attachments a WHERE id=$1`, attachment.ID).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if err := f.Objects.ShortNextGet(3); err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequestWithContext(ctx, "GET", f.Server.URL+"/api/v1/company/attachments/"+attachment.ID.String(), nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+f.JWT(0, "sender"))
	response, err := f.Server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	value, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	_ = response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	r5RequireStatus(t, response.StatusCode, 500)
	if response.Header.Get("Content-Disposition") != "" || bytes.Contains(value, payload[:3]) {
		t.Fatal("failed integrity exposed download header/private prefix")
	}
	if err = f.Pool.QueryRow(ctx, `SELECT to_jsonb(a) FROM mail_attachments a WHERE id=$1`, attachment.ID).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("short object read changed durable attachment metadata")
	}
	status, body = f.Request(t, ctx, f.JWT(0, "sender"), "GET", "/api/v1/company/attachments/"+attachment.ID.String(), nil, "")
	r5RequireStatus(t, status, 200)
	if !bytes.Equal(body, payload) {
		t.Fatal("normal retry did not read the actual full object")
	}
}
