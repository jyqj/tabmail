package testutil

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/google/uuid"
	"tabmail/internal/models"
)

func (s *FakeStore) CreateMessage(_ context.Context, m *models.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *m
	if cp.ID == uuid.Nil {
		cp.ID = uuid.New()
	}
	if cp.ReceivedAt.IsZero() {
		cp.ReceivedAt = time.Now()
	}
	s.messages[cp.ID] = &cp
	if mb, ok := s.mailboxes[cp.MailboxID]; ok {
		mb.MessageCount++
	}
	m.ID = cp.ID
	m.ReceivedAt = cp.ReceivedAt
	return nil
}

func (s *FakeStore) CreateMessageWithQuota(ctx context.Context, m *models.Message, maxMessages int, ensureObject func(context.Context) error) (bool, error) {
	if m.RawObjectKey != "" && ensureObject != nil {
		if err := ensureObject(ctx); err != nil {
			return false, err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	mb, ok := s.mailboxes[m.MailboxID]
	if !ok {
		return false, errors.New("mailbox not found")
	}
	if maxMessages > 0 && int(mb.MessageCount) >= maxMessages {
		return false, nil
	}

	cp := *m
	if cp.ID == uuid.Nil {
		cp.ID = uuid.New()
	}
	if cp.ReceivedAt.IsZero() {
		cp.ReceivedAt = time.Now()
	}
	s.messages[cp.ID] = &cp
	mb.MessageCount++
	m.ID = cp.ID
	m.ReceivedAt = cp.ReceivedAt
	return true, nil
}

func (s *FakeStore) GetMessage(_ context.Context, id uuid.UUID) (*models.Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if m, ok := s.messages[id]; ok {
		cp := *m
		return &cp, nil
	}
	return nil, nil
}

func (v *fakeTenantView) GetMessage(_ context.Context, id uuid.UUID) (*models.Message, error) {
	v.store.mu.Lock()
	defer v.store.mu.Unlock()
	if m, ok := v.store.messages[id]; ok && m.TenantID == v.tenantID {
		cp := *m
		return &cp, nil
	}
	return nil, nil
}

func (s *FakeStore) ListMessages(_ context.Context, mailboxID uuid.UUID, pg models.Page) ([]*models.Message, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var list []*models.Message
	for _, m := range s.messages {
		if m.MailboxID == mailboxID {
			cp := *m
			list = append(list, &cp)
		}
	}
	sort.Slice(list, func(i, j int) bool { return list[i].ReceivedAt.After(list[j].ReceivedAt) })
	return paginateMessages(list, pg), len(list), nil
}

func (s *FakeStore) MarkSeen(_ context.Context, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if m, ok := s.messages[id]; ok {
		m.Seen = true
	}
	return nil
}

func (s *FakeStore) DeleteMessage(_ context.Context, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if msg, ok := s.messages[id]; ok {
		if mb, exists := s.mailboxes[msg.MailboxID]; exists && mb.MessageCount > 0 {
			mb.MessageCount--
		}
		delete(s.messages, id)
	}
	return nil
}

func (s *FakeStore) PurgeMailbox(_ context.Context, mailboxID uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, m := range s.messages {
		if m.MailboxID == mailboxID {
			delete(s.messages, id)
		}
	}
	if mb, ok := s.mailboxes[mailboxID]; ok {
		mb.MessageCount = 0
	}
	return nil
}

func (s *FakeStore) CountMessages(_ context.Context, mailboxID uuid.UUID) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if mb, ok := s.mailboxes[mailboxID]; ok {
		return int(mb.MessageCount), nil
	}
	return 0, nil
}

func (s *FakeStore) CountMessagesByObjectKey(_ context.Context, objectKey string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, m := range s.messages {
		if m.RawObjectKey == objectKey {
			n++
		}
	}
	return n, nil
}

func (s *FakeStore) CountRawObjectReferences(_ context.Context, objectKey string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, m := range s.messages {
		if m.RawObjectKey == objectKey {
			n++
		}
	}
	for _, job := range s.ingestJobs {
		if job.RawObjectKey == objectKey && (job.State == "pending" || job.State == "retry" || job.State == "processing") {
			n++
		}
	}
	return n, nil
}

func (s *FakeStore) ReleaseRawObjectIfUnreferenced(ctx context.Context, key string, del func(context.Context) error) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, m := range s.messages {
		if m.RawObjectKey == key {
			n++
		}
	}
	for _, job := range s.ingestJobs {
		if job.RawObjectKey == key && (job.State == "pending" || job.State == "retry" || job.State == "processing") {
			n++
		}
	}
	if n > 0 {
		return false, nil
	}
	if del != nil {
		if err := del(ctx); err != nil {
			return false, err
		}
	}
	return true, nil
}

func (s *FakeStore) EnqueueOrphanRetry(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.orphanRetries == nil {
		s.orphanRetries = map[string]int{}
	}
	s.orphanRetries[key]++
	return nil
}

func (s *FakeStore) ListPendingOrphanRetries(_ context.Context, limit int) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if limit <= 0 {
		return nil, nil
	}
	out := make([]string, 0, len(s.orphanRetries))
	for key, attempts := range s.orphanRetries {
		if attempts < 10 {
			out = append(out, key)
		}
	}
	sort.Strings(out)
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (s *FakeStore) ClearOrphanRetry(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.orphanRetries, key)
	return nil
}

func (s *FakeStore) ReapExhaustedOrphanRetries(_ context.Context) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	dropped := 0
	for key, attempts := range s.orphanRetries {
		if attempts >= 10 {
			delete(s.orphanRetries, key)
			dropped++
		}
	}
	return dropped, nil
}

// SeedOrphanRetry sets the retry attempt count for a key (test helper).
func (s *FakeStore) SeedOrphanRetry(key string, attempts int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.orphanRetries == nil {
		s.orphanRetries = map[string]int{}
	}
	s.orphanRetries[key] = attempts
}

// OrphanRetryAttempts returns the retry attempt count for a key, or -1 if the
// key is not queued (test helper).
func (s *FakeStore) OrphanRetryAttempts(key string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if a, ok := s.orphanRetries[key]; ok {
		return a
	}
	return -1
}

func (s *FakeStore) CountTenantMessagesSince(_ context.Context, tenantID uuid.UUID, since time.Time) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, m := range s.messages {
		if m.TenantID == tenantID && !m.ReceivedAt.Before(since) {
			n++
		}
	}
	return n, nil
}

func (s *FakeStore) CountAllMessages(_ context.Context) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.messages), nil
}

func (s *FakeStore) DeleteExpiredMessages(_ context.Context, before time.Time, limit int) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	expired := make([]*models.Message, 0, len(s.messages))
	for _, m := range s.messages {
		if m.ExpiresAt.Before(before) {
			cp := *m
			expired = append(expired, &cp)
		}
	}
	sort.Slice(expired, func(i, j int) bool {
		if expired[i].ExpiresAt.Equal(expired[j].ExpiresAt) {
			return expired[i].ID.String() < expired[j].ID.String()
		}
		return expired[i].ExpiresAt.Before(expired[j].ExpiresAt)
	})

	n := 0
	for _, m := range expired {
		if n >= limit {
			break
		}
		if mb, ok := s.mailboxes[m.MailboxID]; ok && mb.MessageCount > 0 {
			mb.MessageCount--
		}
		delete(s.messages, m.ID)
		n++
	}
	return n, nil
}

func (s *FakeStore) ListExpiredObjectKeys(_ context.Context, before time.Time, limit int) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	expired := make([]*models.Message, 0, len(s.messages))
	for _, m := range s.messages {
		if m.ExpiresAt.Before(before) && m.RawObjectKey != "" {
			cp := *m
			expired = append(expired, &cp)
		}
	}
	sort.Slice(expired, func(i, j int) bool {
		if expired[i].ExpiresAt.Equal(expired[j].ExpiresAt) {
			return expired[i].ID.String() < expired[j].ID.String()
		}
		return expired[i].ExpiresAt.Before(expired[j].ExpiresAt)
	})

	var out []string
	for _, m := range expired {
		if len(out) >= limit {
			break
		}
		out = append(out, m.RawObjectKey)
	}
	return out, nil
}

func (s *FakeStore) DeleteExpiredMessagesReturningKeys(_ context.Context, before time.Time, limit int) (int, []string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var expired []*models.Message
	for _, m := range s.messages {
		if m.ExpiresAt.Before(before) {
			cp := *m
			expired = append(expired, &cp)
		}
	}
	sort.Slice(expired, func(i, j int) bool {
		if expired[i].ExpiresAt.Equal(expired[j].ExpiresAt) {
			return expired[i].ID.String() < expired[j].ID.String()
		}
		return expired[i].ExpiresAt.Before(expired[j].ExpiresAt)
	})
	if len(expired) > limit {
		expired = expired[:limit]
	}

	var keys []string
	for _, m := range expired {
		if mb, ok := s.mailboxes[m.MailboxID]; ok && mb.MessageCount > 0 {
			mb.MessageCount--
		}
		delete(s.messages, m.ID)
		if m.RawObjectKey != "" {
			keys = append(keys, m.RawObjectKey)
		}
	}
	return len(expired), keys, nil
}

func paginateMessages(list []*models.Message, pg models.Page) []*models.Message {
	pg = pg.Normalize()
	start := pg.Offset()
	if start >= len(list) {
		return []*models.Message{}
	}
	end := start + pg.PerPage
	if end > len(list) {
		end = len(list)
	}
	return list[start:end]
}
