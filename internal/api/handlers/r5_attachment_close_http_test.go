package handlers

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"tabmail/internal/app"
	"tabmail/internal/app/companymail"
	"tabmail/internal/authz"
	"tabmail/internal/company"
)

// Actual Auth -> download handler -> companymail service, with only the final
// repository and object reader controlled. No real PostgreSQL is claimed.
type attachmentCloseHTTPRepo struct {
	companymail.Repository
	tenant, owner, submission uuid.UUID
	attachment                company.Attachment
	reads                     int
}

func (r *attachmentCloseHTTPRepo) GetWorkAttachment(_ context.Context, a authz.Actor, id uuid.UUID) (*company.Attachment, error) {
	r.reads++
	if a.TenantID != r.tenant || a.ID != r.owner || id != r.attachment.ID {
		return nil, app.NotFound("attachment not found")
	}
	v := r.attachment
	return &v, nil
}
func (r *attachmentCloseHTTPRepo) GetSubmissionAttachment(ctx context.Context, a authz.Actor, id, attachment uuid.UUID) (*company.SubmissionAttachment, error) {
	if id != r.submission {
		return nil, app.NotFound("submission not found")
	}
	v, err := r.GetWorkAttachment(ctx, a, attachment)
	if err != nil {
		return nil, err
	}
	return &company.SubmissionAttachment{ID: v.ID, ObjectKey: v.ObjectKey, Filename: v.Filename, State: v.State, Size: v.Size, SHA256: v.SHA256}, nil
}

type attachmentCloseHTTPReader struct {
	*bytes.Reader
	err    error
	closes atomic.Int64
}

func (r *attachmentCloseHTTPReader) Close() error { r.closes.Add(1); return r.err }

type attachmentCloseHTTPObjects struct {
	companymail.ObjectStore
	reader io.ReadCloser
}

func (o *attachmentCloseHTTPObjects) Get(context.Context, string) (io.ReadCloser, error) {
	return o.reader, nil
}

func TestR5AttachmentOwnedCloseHTTP(t *testing.T) {
	for _, sent := range []bool{false, true} {
		endpoint := "attachment"
		if sent {
			endpoint = "submission-attachment"
		}
		for _, fault := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/close-fault-%t", endpoint, fault), func(t *testing.T) {
				f := newOutboundAccessFixture(t)
				raw := []byte("private-synthetic-attachment-body")
				repo := &attachmentCloseHTTPRepo{tenant: f.tenantID, owner: f.userA.ID, submission: uuid.New(), attachment: company.Attachment{ID: uuid.New(), ObjectKey: "synthetic/private-key", Filename: "fixture.txt", State: "ready", Size: int64(len(raw)), SHA256: fmt.Sprintf("%x", sha256.Sum256(raw))}}
				reader := &attachmentCloseHTTPReader{Reader: bytes.NewReader(raw)}
				if fault {
					reader.err = errors.New("private-synthetic-close-diagnostic")
				}
				svc := companymail.NewService(repo, &attachmentCloseHTTPObjects{reader: reader})
				h := NewCompanyMailHandler(nil, svc, nil, zerolog.Nop())
				params := map[string]string{"id": repo.attachment.ID.String()}
				path := "/api/v1/company/attachments/" + repo.attachment.ID.String()
				call := h.Attachment
				if sent {
					params = map[string]string{"id": repo.submission.String(), "aid": repo.attachment.ID.String()}
					path = "/api/v1/company/submissions/" + repo.submission.String() + "/attachments/" + repo.attachment.ID.String() + "/download"
					call = h.SubmissionAttachmentDownload
				}
				rr := doOutboundHandlerRequest(t, f.st, call, http.MethodGet, path, params, outboundUserHeaders(t, f.userA))
				if fault {
					if rr.Code != http.StatusInternalServerError || !strings.HasPrefix(rr.Header().Get("Content-Type"), "application/json") || rr.Header().Get("Content-Disposition") != "" || !strings.Contains(rr.Body.String(), "INTERNAL") || strings.Contains(rr.Body.String(), "private-synthetic") {
						t.Errorf("close failure released download or diagnostics: status=%d headers=%v body=%q", rr.Code, rr.Header(), rr.Body.String())
					}
					if repo.reads != 1 {
						t.Errorf("failed source crossed final release guard: reads=%d", repo.reads)
					}
				} else {
					if rr.Code != http.StatusOK || !bytes.Equal(rr.Body.Bytes(), raw) || rr.Header().Get("Content-Type") != "application/octet-stream" || !strings.Contains(rr.Header().Get("Content-Disposition"), "fixture.txt") || repo.reads != 2 {
						t.Errorf("normal download changed: status=%d headers=%v reads=%d", rr.Code, rr.Header(), repo.reads)
					}
				}
				if reader.closes.Load() != 1 {
					t.Errorf("reader closes=%d, want exactly one", reader.closes.Load())
				}
			})
		}
	}
}
