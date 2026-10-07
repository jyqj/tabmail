package handlers

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"tabmail/internal/mailcontent"
)

func TestR5InboundAttachmentHTTPSourceFailureIsNotMissing(t *testing.T) {
	deep := "Content-Type: text/plain\r\n\r\nbody"
	for level := 0; level < mailcontent.MaxMIMEDepth; level++ {
		boundary := fmt.Sprintf("part%d", level)
		deep = fmt.Sprintf("Content-Type: multipart/mixed; boundary=%s\r\n\r\n--%s\r\n%s\r\n--%s--\r\n", boundary, boundary, deep, boundary)
	}
	for _, tc := range []struct {
		name   string
		raw    string
		err    error
		status int
		code   string
	}{
		{"storage_failure", "", errors.New("private object path and backend failure"), 500, "INTERNAL"},
		{"malformed_source", "Subject broken no colon\r\n\r\nbody", io.EOF, 500, "INTERNAL"},
		{"parse_depth_limit", deep, io.EOF, 500, "INTERNAL"},
		{"missing_part", "Subject: valid\r\n\r\nbody", io.EOF, 404, "NOT_FOUND"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, fixture, repo, _ := sourceHTTPFixture(t, []sourceHTTPStep{{data: []byte(tc.raw), err: tc.err}})
			part := strings.Repeat("0", 64)
			path := "/api/v1/company/mailboxes/" + repo.message.MailboxID.String() + "/messages/" + repo.message.ID.String() + "/parts/" + part
			rr := doOutboundHandlerRequest(t, fixture.st, h.InboundAttachmentByID, http.MethodGet, path,
				map[string]string{"id": repo.message.MailboxID.String(), "message": repo.message.ID.String(), "attachment": part}, outboundUserHeaders(t, fixture.userA))
			if rr.Code != tc.status || !strings.Contains(rr.Body.String(), `"code":"`+tc.code+`"`) {
				t.Fatalf("source failure misclassified: status=%d body=%s", rr.Code, rr.Body.String())
			}
			if strings.Contains(rr.Body.String(), "private object") || strings.Contains(rr.Body.String(), "backend failure") {
				t.Fatal("internal storage details leaked into HTTP response")
			}
		})
	}
}
