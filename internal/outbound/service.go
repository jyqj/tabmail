package outbound

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"tabmail/internal/config"
	"tabmail/internal/models"
	"tabmail/internal/store"
)

// Service manages outbound email submission and background delivery.
type Service struct {
	cfg    config.Outbound
	store  store.Store
	logger zerolog.Logger
	stopCh chan struct{}
	wg     sync.WaitGroup
}

// NewService creates a new outbound service.
func NewService(cfg config.Outbound, st store.Store, logger zerolog.Logger) *Service {
	return &Service{
		cfg:    cfg,
		store:  st,
		logger: logger.With().Str("component", "outbound").Logger(),
		stopCh: make(chan struct{}),
	}
}

// SendRequest is the validated input for submitting an outbound email.
type SendRequest struct {
	TenantID uuid.UUID
	UserID   *uuid.UUID
	APIKeyID *uuid.UUID
	ZoneID   uuid.UUID
	From     string
	To       []string
	CC       []string
	BCC      []string
	Subject  string
	TextBody string
	HTMLBody string
	Headers  map[string]string
}

// Submit enqueues an outbound email job after validation.
func (s *Service) Submit(ctx context.Context, req SendRequest) (*models.OutboundJob, error) {
	if !s.cfg.Enabled {
		return nil, fmt.Errorf("outbound sending is disabled")
	}

	// Merge all recipients.
	allRcpt := make([]string, 0, len(req.To)+len(req.CC)+len(req.BCC))
	allRcpt = append(allRcpt, req.To...)
	allRcpt = append(allRcpt, req.CC...)
	allRcpt = append(allRcpt, req.BCC...)

	if len(allRcpt) == 0 {
		return nil, fmt.Errorf("at least one recipient required")
	}
	if len(allRcpt) > 50 {
		return nil, fmt.Errorf("too many recipients (max 50)")
	}
	if req.Subject == "" {
		return nil, fmt.Errorf("subject is required")
	}
	if len(req.Subject) > 998 {
		return nil, fmt.Errorf("subject too long (max 998 chars)")
	}
	if req.TextBody == "" && req.HTMLBody == "" {
		return nil, fmt.Errorf("text_body or html_body required")
	}

	// Build Message-ID header.
	msgID := fmt.Sprintf("<%s@%s>", uuid.New().String(), extractDomain(req.From))

	// Marshal custom headers.
	var headersJSON []byte
	if len(req.Headers) > 0 {
		var err error
		headersJSON, err = json.Marshal(req.Headers)
		if err != nil {
			return nil, fmt.Errorf("invalid headers: %w", err)
		}
	}

	now := time.Now().UTC()
	job := &models.OutboundJob{
		ID:              uuid.New(),
		TenantID:        req.TenantID,
		UserID:          req.UserID,
		APIKeyID:        req.APIKeyID,
		MailFrom:        req.From,
		RcptTo:          allRcpt,
		Subject:         req.Subject,
		TextBody:        req.TextBody,
		HTMLBody:        req.HTMLBody,
		HeadersJSON:     headersJSON,
		ZoneID:          req.ZoneID,
		State:           models.OutboundPending,
		MaxAttempts:     s.cfg.MaxRetries,
		MessageIDHeader: msgID,
		CreatedAt:       now,
		UpdatedAt:       now,
		NextAttemptAt:   now,
	}

	if err := s.store.CreateOutboundJob(ctx, job); err != nil {
		return nil, fmt.Errorf("enqueue outbound job: %w", err)
	}

	s.logger.Info().
		Str("job_id", job.ID.String()).
		Str("from", job.MailFrom).
		Int("rcpt_count", len(allRcpt)).
		Msg("outbound job enqueued")

	return job, nil
}

// StartWorker begins the background delivery worker loop.
func (s *Service) StartWorker(ctx context.Context) {
	if !s.cfg.Enabled {
		s.logger.Info().Msg("outbound disabled, worker not started")
		return
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.logger.Info().Msg("outbound worker started")
		ticker := time.NewTicker(s.cfg.PollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-s.stopCh:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.processJobs(ctx)
			}
		}
	}()
}

func (s *Service) processJobs(ctx context.Context) {
	jobs, err := s.store.ClaimOutboundJobs(ctx, time.Now().UTC(), s.cfg.BatchSize)
	if err != nil {
		s.logger.Error().Err(err).Msg("claiming outbound jobs")
		return
	}
	for _, job := range jobs {
		s.deliverJob(ctx, job)
	}
}

func (s *Service) deliverJob(ctx context.Context, job *models.OutboundJob) {
	log := s.logger.With().Str("job_id", job.ID.String()).Logger()

	// Build MIME message.
	mime, err := BuildMIME(job)
	if err != nil {
		log.Error().Err(err).Msg("building MIME")
		s.failOrRetry(ctx, job, fmt.Sprintf("mime build: %s", err))
		return
	}

	// Deliver via configured mode.
	var deliverErr error
	switch s.cfg.Mode {
	case "direct":
		deliverErr = DeliverDirect(ctx, job.MailFrom, job.RcptTo, mime)
	default:
		deliverErr = DeliverRelay(ctx, s.cfg, job.MailFrom, job.RcptTo, mime)
	}

	if deliverErr != nil {
		log.Warn().Err(deliverErr).Int("attempt", job.Attempts+1).Msg("delivery failed")
		s.failOrRetry(ctx, job, deliverErr.Error())
		return
	}

	if err := s.store.MarkOutboundJobSent(ctx, job.ID, 250, "OK", job.MessageIDHeader); err != nil {
		log.Error().Err(err).Msg("marking job sent")
	}
	log.Info().Msg("outbound delivered")
}

func (s *Service) failOrRetry(ctx context.Context, job *models.OutboundJob, errMsg string) {
	attempt := job.Attempts + 1
	if attempt >= job.MaxAttempts {
		if err := s.store.MarkOutboundJobFailed(ctx, job.ID, errMsg, true); err != nil {
			s.logger.Error().Err(err).Str("job_id", job.ID.String()).Msg("marking job dead")
		}
		return
	}
	delay := s.cfg.RetryDelay * time.Duration(1<<uint(attempt))
	next := time.Now().UTC().Add(delay)
	if err := s.store.MarkOutboundJobRetry(ctx, job.ID, errMsg, next); err != nil {
		s.logger.Error().Err(err).Str("job_id", job.ID.String()).Msg("marking job retry")
	}
}

// Shutdown gracefully stops the worker.
func (s *Service) Shutdown() {
	close(s.stopCh)
	s.wg.Wait()
}

func extractDomain(addr string) string {
	for i := len(addr) - 1; i >= 0; i-- {
		if addr[i] == '@' {
			return addr[i+1:]
		}
	}
	return "localhost"
}
