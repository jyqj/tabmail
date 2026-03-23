package smtp

import (
	"context"
	"crypto/tls"
	"io"
	"net"
	"strings"

	gosmtp "github.com/emersion/go-smtp"
	"github.com/rs/zerolog"

	"tabmail/internal/config"
	"tabmail/internal/ingest"
	"tabmail/internal/metrics"
	"tabmail/internal/models"
	"tabmail/internal/policy"
	"tabmail/internal/resolver"
)

// Server wraps go-smtp with TabMail's domain resolution and storage.
type Server struct {
	inner  *gosmtp.Server
	cfg    config.SMTP
	logger zerolog.Logger
}

func NewServer(
	cfg config.SMTP,
	ingestSvc *ingest.Service,
	res *resolver.Resolver,
	logger zerolog.Logger,
) *Server {
	be := &backend{
		cfg:      cfg,
		ingest:   ingestSvc,
		resolver: res,
		logger:   logger.With().Str("component", "smtp").Logger(),
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
	cfg      config.SMTP
	ingest   *ingest.Service
	resolver *resolver.Resolver
	logger   zerolog.Logger
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

	ctx := context.Background()
	if len(s.recipients) == 0 {
		metrics.SMTPMessageRejected()
		return smtpErr(554, "no valid recipients")
	}
	res, err := s.backend.ingest.Accept(ctx, ingest.Envelope{
		Source:     "smtp",
		MailFrom:   s.from,
		Recipients: append([]string(nil), s.recipients...),
	}, raw, s.rcptChecks)
	if err != nil {
		s.logger.Warn().Err(err).Msg("ingest accept failed")
		metrics.SMTPMessageRejected()
		return smtpErr(451, "temporary ingest failure")
	}
	if res.Queued {
		metrics.SMTPMessageAccepted()
		return nil
	}
	if res.Delivered == 0 {
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

func smtpErr(code int, msg string) error {
	return &gosmtp.SMTPError{
		Code:    code,
		Message: msg,
	}
}

func sanitizeAddr(addr string) string {
	addr = strings.TrimSpace(addr)
	addr = strings.TrimPrefix(addr, "<")
	addr = strings.TrimSuffix(addr, ">")
	return strings.ToLower(addr)
}

func (b *backend) currentPolicy(ctx context.Context) (*models.SMTPPolicy, error) {
	return b.ingest.CurrentPolicy(ctx)
}
