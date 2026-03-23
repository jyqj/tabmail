package smtp

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	gosmtp "github.com/emersion/go-smtp"
	"github.com/google/uuid"
	"github.com/jhillyerd/enmime/v2"
	"github.com/rs/zerolog"

	"tabmail/internal/config"
	"tabmail/internal/hooks"
	"tabmail/internal/metrics"
	"tabmail/internal/models"
	"tabmail/internal/policy"
	"tabmail/internal/realtime"
	"tabmail/internal/resolver"
	"tabmail/internal/store"
)

// Server wraps go-smtp with TabMail's domain resolution and storage.
type Server struct {
	inner  *gosmtp.Server
	cfg    config.SMTP
	logger zerolog.Logger
}

func NewServer(
	cfg config.SMTP,
	fallbackRetentionH int,
	st store.Store,
	obj store.ObjectStore,
	res *resolver.Resolver,
	hub *realtime.Hub,
	dispatcher *hooks.Dispatcher,
	defaultPolicy models.SMTPPolicy,
	logger zerolog.Logger,
) *Server {
	be := &backend{
		cfg:                cfg,
		defaultPolicy:      defaultPolicy,
		store:              st,
		obj:                obj,
		resolver:           res,
		hub:                hub,
		dispatcher:         dispatcher,
		fallbackRetentionH: fallbackRetentionH,
		logger:             logger.With().Str("component", "smtp").Logger(),
	}

	s := gosmtp.NewServer(be)
	s.Addr = cfg.Addr
	s.Domain = cfg.Domain
	s.ReadTimeout = cfg.Timeout
	s.WriteTimeout = cfg.Timeout
	s.MaxMessageBytes = int64(cfg.MaxMessageBytes)
	s.MaxRecipients = cfg.MaxRecipients
	s.AllowInsecureAuth = true
	if cfg.TLSEnabled && cfg.TLSCert != "" && cfg.TLSKey != "" {
		cert, err := tls.LoadX509KeyPair(cfg.TLSCert, cfg.TLSKey)
		if err != nil {
			logger.Error().Err(err).Msg("loading TLS cert, disabling STARTTLS")
		} else {
			s.TLSConfig = &tls.Config{Certificates: []tls.Certificate{cert}}
			s.AllowInsecureAuth = false
		}
	}

	return &Server{inner: s, cfg: cfg, logger: logger}
}

func (s *Server) Start(_ context.Context) error {
	s.logger.Info().Str("addr", s.cfg.Addr).Msg("SMTP server starting")
	if s.cfg.ForceTLS && s.inner.TLSConfig != nil {
		ln, err := net.Listen("tcp", s.cfg.Addr)
		if err != nil {
			return err
		}
		return s.inner.Serve(tls.NewListener(ln, s.inner.TLSConfig))
	}
	return s.inner.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.inner.Close()
}

// --- go-smtp Backend ---

type backend struct {
	cfg                config.SMTP
	defaultPolicy      models.SMTPPolicy
	store              store.Store
	obj                store.ObjectStore
	resolver           *resolver.Resolver
	hub                *realtime.Hub
	dispatcher         *hooks.Dispatcher
	fallbackRetentionH int
	logger             zerolog.Logger
	policyMu           sync.RWMutex
	policyCache        *models.SMTPPolicy
	policyExpiresAt    time.Time
}

func (b *backend) NewSession(c *gosmtp.Conn) (gosmtp.Session, error) {
	metrics.SMTPSessionOpened()
	return &session{
		backend: b,
		logger:  b.logger.With().Str("remote", c.Hostname()).Logger(),
	}, nil
}

// --- go-smtp Session ---

type session struct {
	backend    *backend
	logger     zerolog.Logger
	from       string
	recipients []string
	rcptChecks map[string]*resolver.Result
}

func (s *session) AuthPlain(_ string, _ string) error {
	return nil
}

func (s *session) Mail(from string, _ *gosmtp.MailOptions) error {
	s.from = sanitizeAddr(from)
	pol, err := s.currentPolicy()
	if err != nil {
		return smtpErr(451, "temporary policy lookup failure")
	}
	if policy.ShouldRejectOrigin(s.from, pol.RejectOriginDomains) {
		return smtpErr(550, "sender domain rejected by policy")
	}
	return nil
}

func (s *session) Rcpt(to string, _ *gosmtp.RcptOptions) error {
	addr := sanitizeAddr(to)
	_, domain, err := policy.NormalizeAddressParts(addr, s.backend.resolver.StripPlus())
	if err != nil {
		metrics.SMTPRecipientRejected()
		return smtpErr(550, "invalid recipient")
	}
	pol, err := s.currentPolicy()
	if err != nil {
		return smtpErr(451, "temporary policy lookup failure")
	}
	if !policy.ShouldAcceptDomain(domain, pol.DefaultAccept, pol.AcceptDomains, pol.RejectDomains) {
		metrics.SMTPRecipientRejected()
		metrics.MailboxRecipientRejected(addr)
		return smtpErr(550, "recipient domain rejected by policy")
	}
	res, err := s.backend.resolver.Check(context.Background(), addr)
	if err != nil {
		s.logger.Warn().Err(err).Str("rcpt", addr).Msg("recipient validation failed")
		return smtpErr(451, "temporary recipient validation failure")
	}
	if res == nil || res.Zone == nil {
		metrics.SMTPRecipientRejected()
		return smtpErr(550, "unknown recipient domain")
	}
	if !res.Zone.IsVerified || !res.Zone.MXVerified {
		metrics.SMTPRecipientRejected()
		metrics.TenantRecipientRejected(res.Zone.TenantID.String())
		return smtpErr(550, "domain is not verified")
	}
	if res.Mailbox == nil && (res.Route == nil || !res.Route.AutoCreateMailbox) {
		metrics.SMTPRecipientRejected()
		metrics.TenantRecipientRejected(res.Zone.TenantID.String())
		return smtpErr(550, "recipient not provisioned")
	}
	metrics.SMTPRecipientAccepted()
	metrics.TenantRecipientAccepted(res.Zone.TenantID.String())
	metrics.MailboxRecipientAccepted(addr)
	s.recipients = append(s.recipients, addr)
	if s.rcptChecks == nil {
		s.rcptChecks = make(map[string]*resolver.Result)
	}
	s.rcptChecks[addr] = res
	return nil
}

func (s *session) Data(r io.Reader) error {
	raw, err := io.ReadAll(r)
	if err != nil {
		s.logger.Err(err).Msg("reading DATA")
		return err
	}
	metrics.SMTPBytesReceived(int64(len(raw)))

	env, err := enmime.ReadEnvelope(bytes.NewReader(raw))
	if err != nil {
		s.logger.Warn().Err(err).Msg("parsing MIME envelope (storing raw only)")
	}

	ctx := context.Background()
	if len(s.recipients) == 0 {
		metrics.SMTPMessageRejected()
		return smtpErr(554, "no valid recipients")
	}
	now := time.Now()
	successes := 0
	tenantDaily := map[uuid.UUID]int{}
	mailboxCounts := map[uuid.UUID]int{}
	tenantConfigs := map[uuid.UUID]*models.EffectiveConfig{}
	pol, err := s.currentPolicy()
	if err != nil {
		s.logger.Warn().Err(err).Msg("load smtp policy")
		metrics.SMTPMessageRejected()
		return smtpErr(451, "temporary policy lookup failure")
	}

	subject := ""
	var headersJSON json.RawMessage
	if env != nil {
		subject = env.GetHeader("Subject")
		hm := make(map[string]string)
		for _, key := range []string{"From", "To", "Cc", "Date", "Message-Id", "Reply-To", "Content-Type"} {
			if v := env.GetHeader(key); v != "" {
				hm[key] = v
			}
		}
		headersJSON, _ = json.Marshal(hm)
	}

	objKey := ""

	for _, rcpt := range s.recipients {
		addr := sanitizeAddr(rcpt)
		var result *resolver.Result
		if checked, ok := s.rcptChecks[addr]; ok && checked != nil && checked.Mailbox != nil {
			result = checked
		} else {
			result, err = s.backend.resolver.Resolve(ctx, addr)
			if err != nil {
				metrics.MailboxRecipientRejected(addr)
				s.logger.Warn().Err(err).Str("rcpt", addr).Msg("resolve failed")
				continue
			}
		}
		if result == nil || result.Mailbox == nil || result.Zone == nil {
			metrics.MailboxRecipientRejected(addr)
			s.logger.Debug().Str("rcpt", addr).Msg("no matching zone/route, rejecting recipient")
			continue
		}
		if !result.Zone.IsVerified || !result.Zone.MXVerified {
			metrics.MailboxRecipientRejected(addr)
			s.logger.Warn().Str("rcpt", addr).Msg("recipient zone is not verified")
			continue
		}
		if result.Created {
			s.logger.Info().Str("address", addr).Msg("auto-created mailbox")
		}

		mb := result.Mailbox
		if !policy.ShouldStoreDomain(mb.ResolvedDomain, pol.DefaultStore, pol.StoreDomains, pol.DiscardDomains) {
			s.logger.Info().Str("mailbox", mb.FullAddress).Msg("message accepted but discarded by store policy")
			continue
		}
		cfg, ok := tenantConfigs[mb.TenantID]
		if !ok {
			cfg, err = s.backend.store.EffectiveConfig(ctx, mb.TenantID)
			if err != nil || cfg == nil {
				s.logger.Warn().Err(err).Str("mailbox", mb.FullAddress).Msg("load tenant config")
				continue
			}
			tenantConfigs[mb.TenantID] = cfg
		}
		if cfg.MaxMessageBytes > 0 && len(raw) > cfg.MaxMessageBytes {
			s.logger.Warn().
				Str("mailbox", mb.FullAddress).
				Int("limit", cfg.MaxMessageBytes).
				Int("size", len(raw)).
				Msg("tenant max message bytes exceeded")
			continue
		}
		if _, ok := mailboxCounts[mb.ID]; !ok {
			count, err := s.backend.store.CountMessages(ctx, mb.ID)
			if err != nil {
				s.logger.Warn().Err(err).Str("mailbox", mb.FullAddress).Msg("count mailbox messages")
				continue
			}
			mailboxCounts[mb.ID] = count
		}
		if cfg.MaxMessagesPerMailbox > 0 && mailboxCounts[mb.ID] >= cfg.MaxMessagesPerMailbox {
			s.logger.Warn().
				Str("mailbox", mb.FullAddress).
				Int("limit", cfg.MaxMessagesPerMailbox).
				Msg("mailbox message quota exceeded")
			continue
		}
		if _, ok := tenantDaily[mb.TenantID]; !ok {
			count, err := s.backend.store.CountTenantMessagesSince(ctx, mb.TenantID, now.UTC().Truncate(24*time.Hour))
			if err != nil {
				s.logger.Warn().Err(err).Str("tenant", mb.TenantID.String()).Msg("count tenant daily quota")
				continue
			}
			tenantDaily[mb.TenantID] = count
		}
		if cfg.DailyQuota > 0 && tenantDaily[mb.TenantID] >= cfg.DailyQuota {
			s.logger.Warn().
				Str("tenant", mb.TenantID.String()).
				Int("limit", cfg.DailyQuota).
				Msg("tenant daily quota exceeded")
			continue
		}

		retH := resolveRetention(s.backend.store, ctx, result, s.backend.fallbackRetentionH)
		if objKey == "" {
			objKey = objectKeyForRaw(raw)
			exists, err := s.backend.obj.Exists(ctx, objKey)
			if err != nil {
				s.logger.Warn().Err(err).Str("key", objKey).Msg("checking raw .eml existence")
				continue
			}
			if !exists {
				if err := s.backend.obj.Put(ctx, objKey, bytes.NewReader(raw), int64(len(raw))); err != nil {
					s.logger.Err(err).Str("key", objKey).Msg("storing raw .eml")
					objKey = ""
					continue
				}
			}
		}

		msg := &models.Message{
			TenantID:     mb.TenantID,
			MailboxID:    mb.ID,
			ZoneID:       mb.ZoneID,
			Sender:       s.from,
			Recipients:   []string{addr},
			Subject:      subject,
			Size:         int64(len(raw)),
			RawObjectKey: objKey,
			HeadersJSON:  headersJSON,
			ExpiresAt:    now.Add(time.Duration(retH) * time.Hour),
		}
		if err := s.backend.store.CreateMessage(ctx, msg); err != nil {
			metrics.SMTPDeliveryFailed(mb.TenantID.String(), mb.FullAddress)
			s.logger.Err(err).Str("mailbox", mb.FullAddress).Msg("storing message metadata")
			continue
		}
		metrics.SMTPDeliverySucceeded(mb.TenantID.String(), mb.FullAddress)
		if s.backend.hub != nil {
			s.backend.hub.Publish(realtime.Event{
				Type:      realtime.EventMessage,
				Mailbox:   mb.FullAddress,
				MessageID: msg.ID.String(),
				Sender:    s.from,
				Subject:   subject,
				Size:      int64(len(raw)),
			})
		}
		if s.backend.dispatcher != nil {
			s.backend.dispatcher.Publish(hooks.Event{
				Type:       "message.received",
				Mailbox:    mb.FullAddress,
				MessageID:  msg.ID.String(),
				TenantID:   mb.TenantID.String(),
				Sender:     s.from,
				Recipients: []string{addr},
				Subject:    subject,
			})
		}
		mailboxCounts[mb.ID]++
		tenantDaily[mb.TenantID]++
		successes++
		s.logger.Info().
			Str("from", s.from).
			Str("to", addr).
			Str("subject", subject).
			Int64("size", int64(len(raw))).
			Msg("message delivered")
	}
	if successes == 0 {
		metrics.SMTPMessageRejected()
		return smtpErr(554, "message rejected for all recipients")
	}
	metrics.SMTPMessageAccepted()
	return nil
}

func (s *session) Reset() {
	s.from = ""
	s.recipients = nil
	s.rcptChecks = nil
}

func (s *session) Logout() error {
	metrics.SMTPSessionClosed()
	return nil
}

func (s *session) currentPolicy() (*models.SMTPPolicy, error) {
	return s.backend.currentPolicy(context.Background())
}

// resolveRetention applies the 4-level priority:
//
//	mailbox override → route override → tenant effective config → 24h fallback
func resolveRetention(st store.Store, ctx context.Context, res *resolver.Result, fallback int) int {
	if res.Mailbox.RetentionHoursOverride != nil {
		return *res.Mailbox.RetentionHoursOverride
	}
	if res.Route != nil && res.Route.RetentionHoursOverride != nil {
		return *res.Route.RetentionHoursOverride
	}
	cfg, err := st.EffectiveConfig(ctx, res.Mailbox.TenantID)
	if err == nil && cfg != nil {
		return cfg.RetentionHours
	}
	if fallback > 0 {
		return fallback
	}
	return 24
}

func sanitizeAddr(addr string) string {
	addr = strings.TrimSpace(addr)
	addr = strings.TrimPrefix(addr, "<")
	addr = strings.TrimSuffix(addr, ">")
	return strings.ToLower(addr)
}

func smtpErr(code int, msg string) error {
	return &gosmtp.SMTPError{
		Code:    code,
		Message: msg,
	}
}

func objectKeyForRaw(raw []byte) string {
	sum := sha256.Sum256(raw)
	hexSum := hex.EncodeToString(sum[:])
	return fmt.Sprintf("sha256/%s/%s.eml", hexSum[:2], hexSum)
}

func (b *backend) currentPolicy(ctx context.Context) (*models.SMTPPolicy, error) {
	b.policyMu.RLock()
	if b.policyCache != nil && time.Now().Before(b.policyExpiresAt) {
		cp := *b.policyCache
		b.policyMu.RUnlock()
		return &cp, nil
	}
	b.policyMu.RUnlock()

	b.policyMu.Lock()
	defer b.policyMu.Unlock()
	if b.policyCache != nil && time.Now().Before(b.policyExpiresAt) {
		cp := *b.policyCache
		return &cp, nil
	}

	pol, err := b.store.GetSMTPPolicy(ctx)
	if err != nil {
		return nil, err
	}
	if pol == nil {
		cp := b.defaultPolicy
		pol = &cp
	}

	cp := *pol
	b.policyCache = &cp
	b.policyExpiresAt = time.Now().Add(2 * time.Second)
	return &cp, nil
}
