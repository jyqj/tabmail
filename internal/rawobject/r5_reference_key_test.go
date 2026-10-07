package rawobject_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/google/uuid"
	"tabmail/internal/models"
	"tabmail/internal/rawobject"
	"tabmail/internal/testutil"
)

type referenceKeyProbe struct {
	*testutil.FakeStore
	messageCalls, jobCalls int
	metadataErr            error
}

func (p *referenceKeyProbe) CreateMessageWithQuota(ctx context.Context, m *models.Message, limit int, ensure func(context.Context) error) (bool, error) {
	p.messageCalls++
	if p.metadataErr != nil {
		return false, p.metadataErr
	}
	if m == nil {
		return false, errors.New("nil message reached metadata adapter")
	}
	return p.FakeStore.CreateMessageWithQuota(ctx, m, limit, ensure)
}

func (p *referenceKeyProbe) CreateIngestJob(ctx context.Context, job *models.IngestJob, ensure func(context.Context) error) error {
	p.jobCalls++
	if p.metadataErr != nil {
		return p.metadataErr
	}
	if job == nil {
		return errors.New("nil job reached metadata adapter")
	}
	return p.FakeStore.CreateIngestJob(ctx, job, ensure)
}

type referenceBlobProbe struct {
	*testutil.MemoryObjectStore
	puts, checks int
	existsErr    error
}

func (p *referenceBlobProbe) Put(ctx context.Context, key string, r io.Reader, size int64) error {
	p.puts++
	return p.MemoryObjectStore.Put(ctx, key, r, size)
}

func (p *referenceBlobProbe) Exists(ctx context.Context, key string) (bool, error) {
	p.checks++
	if p.existsErr != nil {
		return false, p.existsErr
	}
	return p.MemoryObjectStore.Exists(ctx, key)
}

func referenceBoundary(t *testing.T) (*rawobject.Store, *referenceKeyProbe, *referenceBlobProbe, uuid.UUID) {
	t.Helper()
	refs := &referenceKeyProbe{FakeStore: testutil.NewFakeStore()}
	box := uuid.New()
	refs.SeedMailbox(&models.Mailbox{ID: box, TenantID: uuid.New(), ZoneID: uuid.New()})
	blobs := &referenceBlobProbe{MemoryObjectStore: testutil.NewMemoryObjectStore()}
	return rawobject.NewStore(blobs, refs), refs, blobs, box
}

func storeReference(kind string, st *rawobject.Store, box uuid.UUID, raw []byte, key string, nilRow bool) (bool, error) {
	ctx := context.Background()
	if kind == "message" {
		var m *models.Message
		if !nilRow {
			m = &models.Message{MailboxID: box, RawObjectKey: key, Size: int64(len(raw))}
		}
		return st.StoreMessage(ctx, m, raw, 100)
	}
	var job *models.IngestJob
	if !nilRow {
		job = &models.IngestJob{RawObjectKey: key, Source: "smtp"}
	}
	err := st.StoreIngestJob(ctx, job, raw)
	return err == nil, err
}

func TestR5RawReferenceRejectsDifferentContentKey(t *testing.T) {
	raw := []byte("Subject: owned reference fixture\r\n\r\noriginal bytes")
	for _, tc := range []struct {
		name, key string
		nilRow    bool
	}{
		{name: "nil-row", nilRow: true},
		{name: "empty-key"},
		{name: "other-content", key: rawobject.Key([]byte("different content"))},
		{name: "leading-space", key: " " + rawobject.Key(raw)},
		{name: "leading-slash", key: "/" + rawobject.Key(raw)},
	} {
		for _, kind := range []string{"message", "ingest-job"} {
			t.Run(tc.name+"/"+kind, func(t *testing.T) {
				st, refs, blobs, box := referenceBoundary(t)
				created, err := storeReference(kind, st, box, raw, tc.key, tc.nilRow)
				if err == nil || created {
					t.Errorf("a reference to different bytes must be refused: created=%v, err=%v", created, err)
				}
				if refs.messageCalls != 0 || refs.jobCalls != 0 || blobs.puts != 0 || blobs.checks != 0 || blobs.Count() != 0 {
					t.Errorf("invalid reference reached storage: messages=%d jobs=%d puts=%d checks=%d objects=%d", refs.messageCalls, refs.jobCalls, blobs.puts, blobs.checks, blobs.Count())
				}
				messages, total, e := refs.ListMessages(context.Background(), box, models.Page{})
				if e != nil || total != 0 || len(messages) != 0 {
					t.Errorf("invalid reference committed a message: %d, %d, %v", len(messages), total, e)
				}
				jobs, total, e := refs.ListIngestJobs(context.Background(), models.Page{}, "", "", "")
				if e != nil || total != 0 || len(jobs) != 0 {
					t.Errorf("invalid reference committed a job: %d, %d, %v", len(jobs), total, e)
				}
			})
		}
	}
}

func TestR5RawReferencePreservesMatchingWritesAndDedup(t *testing.T) {
	for _, tc := range []struct {
		name        string
		raw         []byte
		preexisting bool
	}{
		{name: "missing-object-reput", raw: []byte("Subject: canonical\r\n\r\nbody")},
		{name: "already-present", raw: []byte("Subject: canonical\r\n\r\nbody"), preexisting: true},
		{name: "empty-content", raw: []byte{}},
	} {
		for _, kind := range []string{"message", "ingest-job"} {
			t.Run(tc.name+"/"+kind, func(t *testing.T) {
				st, refs, blobs, box := referenceBoundary(t)
				key := rawobject.Key(tc.raw)
				if tc.preexisting {
					if err := blobs.MemoryObjectStore.Put(context.Background(), key, bytes.NewReader(tc.raw), int64(len(tc.raw))); err != nil {
						t.Fatal(err)
					}
				}
				created, err := storeReference(kind, st, box, tc.raw, key, false)
				if err != nil || !created {
					t.Fatalf("matching reference failed: %v, %v", created, err)
				}
				wantPuts := 1
				if tc.preexisting {
					wantPuts = 0
				}
				if blobs.puts != wantPuts || refs.messageCalls+refs.jobCalls != 1 {
					t.Errorf("dedup/metadata protocol changed: puts=%d metadata=%d", blobs.puts, refs.messageCalls+refs.jobCalls)
				}
				r, err := blobs.Get(context.Background(), key)
				if err != nil {
					t.Fatalf("referenced object is absent: %v", err)
				}
				defer r.Close()
				got, err := io.ReadAll(r)
				if err != nil || !bytes.Equal(got, tc.raw) {
					t.Fatalf("referenced bytes changed: %q, %v", got, err)
				}
			})
		}
	}
}

func TestR5RawReferenceRetainsBackendErrorsAndQuota(t *testing.T) {
	for _, failure := range []string{"metadata", "object"} {
		for _, kind := range []string{"message", "ingest-job"} {
			t.Run(failure+"/"+kind, func(t *testing.T) {
				st, refs, blobs, box := referenceBoundary(t)
				cause := errors.New("owned " + failure + " failure")
				if failure == "metadata" {
					refs.metadataErr = cause
				} else {
					blobs.existsErr = cause
				}
				raw := []byte("original bytes")
				created, err := storeReference(kind, st, box, raw, rawobject.Key(raw), false)
				if created || !errors.Is(err, cause) || blobs.Count() != 0 {
					t.Fatalf("failed write changed error or published data: %v, %v, objects=%d", created, err, blobs.Count())
				}
			})
		}
	}
	t.Run("mailbox-quota", func(t *testing.T) {
		st, refs, _, box := referenceBoundary(t)
		if err := refs.CreateMessage(context.Background(), &models.Message{MailboxID: box}); err != nil {
			t.Fatal(err)
		}
		raw := []byte(strings.Repeat("q", 16))
		created, err := st.StoreMessage(context.Background(), &models.Message{MailboxID: box, RawObjectKey: rawobject.Key(raw)}, raw, 1)
		if err != nil || created {
			t.Fatalf("matching content must still obey mailbox quota: %v, %v", created, err)
		}
	})
}
