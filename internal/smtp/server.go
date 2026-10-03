package smtp

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"

	"github.com/emersion/go-sasl"
	gosmtp "github.com/emersion/go-smtp"
	"github.com/rs/zerolog"

	"tabmail/internal/config"
	"tabmail/internal/ingest"
	"tabmail/internal/metrics"
	"tabmail/internal/policy"
	"tabmail/internal/resolver"
)

// Server wraps go-smtp with TabMail's domain resolution and storage.
type Server struct {
	initErr error
	inner   *gosmtp.Server
	backend *backend
	cfg     config.SMTP
	logger  zerolog.Logger

	// A Server is single-use: go-smtp's done channel cannot be reopened. In
	// particular, Shutdown before Start must seal admission, not start a new
	// generation when a previously scheduled Start finally runs.
	mu           sync.Mutex
	started      bool
	stopping     bool
	ready        chan struct{}
	serveDone    chan struct{}
	drained      chan struct{}
	listener     *limitedListener
	listen       func(context.Context, string, string) (net.Listener, error)
	cancelListen context.CancelFunc
	cancelWork   context.CancelFunc
	drainErr     error
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
	var initErr error
	if cfg.ForceTLS && !cfg.TLSEnabled {
		initErr = fmt.Errorf("implicit TLS requires TLS configuration")
	}
	if cfg.TLSEnabled {
		cert, err := tls.LoadX509KeyPair(cfg.TLSCert, cfg.TLSKey)
		if err != nil {
			initErr = fmt.Errorf("loading SMTP TLS certificate: %w", err)
		} else {
			s.TLSConfig = &tls.Config{Certificates: []tls.Certificate{cert}}
			s.AllowInsecureAuth = false
		}
	}

	return &Server{
		inner: s, backend: be, cfg: cfg, logger: logger, initErr: initErr,
		ready: make(chan struct{}), serveDone: make(chan struct{}), drained: make(chan struct{}),
		listen: (&net.ListenConfig{}).Listen,
	}
}

// ErrServerStarted reports a second Start on the same server instance.
var ErrServerStarted = errors.New("smtp: server already started")

func (s *Server) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.stopping {
		s.mu.Unlock()
		return gosmtp.ErrServerClosed
	}
	if s.started {
		s.mu.Unlock()
		return ErrServerStarted
	}
	s.started = true
	listenCtx, cancelListen := context.WithCancel(ctx)
	// Process cancellation seals admission, but in-flight DB operations retain
	// their own lifetime until the shutdown caller's absolute deadline. Never
	// derive that lifetime from an already-cancelled process context.
	workCtx, cancelWork := context.WithCancel(context.WithoutCancel(ctx))
	s.cancelListen, s.cancelWork = cancelListen, cancelWork
	s.backend.ctx = workCtx
	s.mu.Unlock()

	stopWatching := context.AfterFunc(ctx, s.beginStop)
	defer stopWatching()
	defer cancelListen()
	defer func() {
		close(s.serveDone)
		s.beginStop()
	}()
	if err := ctx.Err(); err != nil {
		close(s.ready)
		return err
	}
	if s.initErr != nil {
		close(s.ready)
		return s.initErr
	}
	s.logger.Info().Str("addr", s.cfg.Addr).Msg("SMTP server starting")
	ln, err := s.listen(listenCtx, "tcp", s.cfg.Addr)
	if err != nil {
		close(s.ready)
		return err
	}
	owned := s.wrapListener(ln)
	s.mu.Lock()
	s.listener = owned
	if s.stopping {
		owned.sealAdmission()
	}
	close(s.ready)
	s.mu.Unlock()
	var serving net.Listener = owned
	if s.cfg.ForceTLS && s.inner.TLSConfig != nil {
		serving = tls.NewListener(owned, s.inner.TLSConfig)
	}
	err = s.inner.Serve(serving)
	s.mu.Lock()
	stopping := s.stopping
	s.mu.Unlock()
	if stopping && errors.Is(err, net.ErrClosed) {
		return nil
	}
	return err
}

// wrapListener owns every raw socket, even without a connection limit. Close
// only interrupts transport; the single retained inner.Shutdown call below is
// the sole completion witness, including the local fork's DATA tails and Logout.
func (s *Server) wrapListener(ln net.Listener) *limitedListener {
	l := &limitedListener{Listener: ln, done: make(chan struct{}), conns: make(map[*limitedConn]struct{})}
	if s.cfg.MaxConnections > 0 {
		s.logger.Info().Int("max_connections", s.cfg.MaxConnections).Msg("SMTP connection limit enabled")
		l.sem = make(chan struct{}, s.cfg.MaxConnections)
	}
	return l
}

type limitedListener struct {
	net.Listener
	sem       chan struct{}
	done      chan struct{}
	mu        sync.Mutex
	closed    bool
	conns     map[*limitedConn]struct{}
	closeOnce sync.Once
	closeErr  error
	connErr   error
}

func (l *limitedListener) Accept() (net.Conn, error) {
	if l.sem != nil {
		select {
		case l.sem <- struct{}{}:
		case <-l.done:
			return nil, net.ErrClosed
		}
	}
	release := func() {
		if l.sem != nil {
			<-l.sem
		}
	}
	select {
	case <-l.done:
		release()
		return nil, net.ErrClosed
	default:
	}
	conn, err := l.Listener.Accept()
	if err != nil {
		release()
		return nil, err
	}
	c := &limitedConn{Conn: conn, owner: l}
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		// A successful underlying Accept may race Close. Do not hand the
		// socket to go-smtp, and keep its actual close in the Serve lifetime.
		closeErr := conn.Close()
		l.mu.Lock()
		if l.connErr == nil && closeErr != nil && !errors.Is(closeErr, net.ErrClosed) {
			l.connErr = closeErr
		}
		l.mu.Unlock()
		release()
		return nil, net.ErrClosed
	}
	l.conns[c] = struct{}{}
	l.mu.Unlock()
	return c, nil
}

func (l *limitedListener) sealAdmission() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.closed {
		l.closed = true
		close(l.done) // Also wakes Accept blocked on a full connection limit.
	}
}

func (l *limitedListener) Close() error {
	l.closeOnce.Do(func() {
		l.sealAdmission()
		l.closeErr = l.Listener.Close()
	})
	return l.closeErr
}

func (l *limitedListener) closeConnections() {
	l.mu.Lock()
	conns := make([]*limitedConn, 0, len(l.conns))
	for c := range l.conns {
		conns = append(conns, c)
	}
	l.mu.Unlock()
	for _, c := range conns {
		_ = c.Close()
	}
}

type limitedConn struct {
	net.Conn
	owner *limitedListener
	once  sync.Once
	err   error
}

func (c *limitedConn) Close() error {
	c.once.Do(func() {
		c.err = c.Conn.Close()
		c.owner.mu.Lock()
		delete(c.owner.conns, c)
		if c.owner.connErr == nil && c.err != nil && !errors.Is(c.err, net.ErrClosed) {
			c.owner.connErr = c.err
		}
		c.owner.mu.Unlock()
		if c.owner.sem != nil {
			<-c.owner.sem
		}
	})
	return c.err
}

// beginStop seals admission synchronously, including a Start which has not run
// yet. Exactly one retained owner does the potentially blocking close/join work.
func (s *Server) beginStop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopping {
		return
	}
	s.stopping = true
	if s.listener != nil {
		s.listener.sealAdmission()
	}
	if !s.started {
		close(s.ready)
		close(s.serveDone)
	}
	if s.cancelListen != nil {
		s.cancelListen()
	}
	go s.drain()
}

func (s *Server) drain() {
	<-s.ready
	s.mu.Lock()
	ln := s.listener
	s.mu.Unlock()
	var err error
	if ln != nil {
		err = ln.Close()
		ln.closeConnections()
	}
	// The module replacement in third_party/go-smtp extends v0.24.0: it
	// registers BDAT ownership before launch, captures transaction state, and
	// joins the result/pipe-close tail before Logout. Retain exactly one
	// Shutdown owner through all of that work. Neither Close nor a second
	// inner.Shutdown returning ErrServerClosed is a completion witness.
	// Waiting for Serve here also explicitly closes our outer admission phase.
	// The backend fence remains a separate business-operation owner, not a
	// substitute for the fork's creation-to-tail goroutine ownership.
	<-s.serveDone
	innerErr := s.inner.Shutdown(context.Background())
	s.backend.sealAndWait()
	if errors.Is(err, net.ErrClosed) {
		err = nil
	}
	if errors.Is(innerErr, net.ErrClosed) {
		innerErr = nil
	}
	err = errors.Join(err, innerErr)
	if ln != nil {
		ln.mu.Lock()
		err = errors.Join(err, ln.connErr)
		ln.mu.Unlock()
	}
	s.mu.Lock()
	if s.cancelWork != nil {
		s.cancelWork()
	}
	s.drainErr = err
	close(s.drained)
	s.mu.Unlock()
}

// Shutdown returns nil only after every owned connection handler has returned.
// On deadline it cancels cooperative backend work and returns an error, retaining
// the real owner for a subsequent Shutdown to join; it never reports a fake drain.
// Start and Shutdown are single-use/idempotent respectively. A fresh listener
// generation requires a new Server, not reuse of go-smtp's closed internal state.
func (s *Server) Shutdown(ctx context.Context) error {
	s.beginStop()
	s.mu.Lock()
	cancel := s.cancelWork
	s.mu.Unlock()
	if cancel == nil {
		cancel = func() {}
	}
	stopCancel := context.AfterFunc(ctx, cancel)
	defer stopCancel()
	select {
	case <-s.drained:
		return s.drainErr
	default:
	}
	select {
	case <-s.drained:
		return s.drainErr
	case <-ctx.Done():
		cancel()
		return fmt.Errorf("SMTP shutdown incomplete: %w", ctx.Err())
	}
}

// --- go-smtp Backend ---

type backend struct {
	workMu     sync.Mutex
	workClosed bool
	workWG     sync.WaitGroup
	ctx        context.Context
	cfg        config.SMTP
	ingest     *ingest.Service
	resolver   *resolver.Resolver
	logger     zerolog.Logger
}

// A BDAT goroutine may outlive go-smtp's connection WaitGroup. The mutex makes
// admission and sealing atomic; a late scheduled call cannot add work after Wait.
func (b *backend) beginWork() bool {
	b.workMu.Lock()
	defer b.workMu.Unlock()
	if b.workClosed {
		return false
	}
	b.workWG.Add(1)
	return true
}

func (b *backend) sealAndWait() {
	b.workMu.Lock()
	b.workClosed = true
	b.workMu.Unlock()
	b.workWG.Wait()
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
	mu         sync.Mutex // BDAT Data may overlap the connection goroutine's Reset.
	backend    *backend
	logger     zerolog.Logger
	from       string
	recipients []string
	// results caches RCPT-phase resolver.Results keyed by policy.SanitizeAddr(rcpt) so
	// DATA can hand them to ingest.Accept via WithResolved and skip a redundant
	// Resolve per recipient. Only Reusable entries (Mailbox present, not just
	// Created) are stored; auto-create results are left for deliver to resolve.
	// BDAT uses a separate Data goroutine; mu protects envelope snapshots from
	// Reset on the connection goroutine.
	results map[string]*resolver.Result
}

// workContext is fixed before Serve admits a session. The fallback supports
// standalone backend construction; production always supplies the owned context.
func (s *session) workContext() context.Context {
	if s.backend.ctx != nil {
		return s.backend.ctx
	}
	return context.Background()
}

func (s *session) AuthPlain(_ string, _ string) error {
	return gosmtp.ErrAuthUnsupported
}

func (s *session) AuthMechanisms() []string {
	return nil
}

func (s *session) Auth(_ string) (sasl.Server, error) {
	return nil, gosmtp.ErrAuthUnsupported
}

func (s *session) Mail(from string, opts *gosmtp.MailOptions) error {
	if !s.backend.beginWork() {
		return smtpErr(451, "SMTP server shutting down")
	}
	defer s.backend.workWG.Done()
	s.mu.Lock()
	defer s.mu.Unlock()

	if opts != nil && opts.Auth != nil {
		return gosmtp.ErrAuthUnsupported
	}
	s.from = policy.SanitizeAddr(from)
	pol, err := s.backend.ingest.CurrentPolicy(s.workContext())
	if err != nil {
		return smtpErr(451, "temporary policy lookup failure")
	}
	if policy.ShouldRejectOrigin(s.from, pol.RejectOriginDomains) {
		return smtpErr(550, "sender domain rejected by policy")
	}
	return nil
}

func (s *session) Rcpt(to string, _ *gosmtp.RcptOptions) error {
	if !s.backend.beginWork() {
		return smtpErr(451, "SMTP server shutting down")
	}
	defer s.backend.workWG.Done()
	s.mu.Lock()
	defer s.mu.Unlock()

	addr := policy.SanitizeAddr(to)
	_, domain, err := policy.NormalizeAddressParts(addr, s.backend.resolver.StripPlus())
	if err != nil {
		metrics.SMTPRecipientRejected()
		return smtpErr(550, "invalid recipient")
	}
	pol, err := s.backend.ingest.CurrentPolicy(s.workContext())
	if err != nil {
		return smtpErr(451, "temporary policy lookup failure")
	}
	if !policy.ShouldAcceptDomain(domain, pol.DefaultAccept, pol.AcceptDomains, pol.RejectDomains) {
		metrics.SMTPRecipientRejected()
		metrics.MailboxRecipientRejected(addr)
		return smtpErr(550, "recipient domain rejected by policy")
	}
	res, err := s.backend.resolver.Check(s.workContext(), addr)
	if err != nil {
		s.logger.Warn().Err(err).Str("rcpt", addr).Msg("recipient validation failed")
		return smtpErr(451, "temporary recipient validation failure")
	}
	if res == nil || res.Zone == nil {
		metrics.SMTPRecipientRejected()
		return smtpErr(550, "unknown recipient domain")
	}
	if !res.Zone.CanReceiveMessage() {
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
	// Cache Reusable results for DATA-phase reuse. Check never sets Created, so
	// the Mailbox!=nil gate alone matches Reusable here; the helper is used for
	// symmetry with deliver's reuse condition and to keep the contract explicit.
	if res.Reusable() {
		if s.results == nil {
			s.results = map[string]*resolver.Result{}
		}
		s.results[addr] = res
	}
	return nil
}

func (s *session) Data(r io.Reader) error {
	if !s.backend.beginWork() {
		return smtpErr(451, "SMTP server shutting down")
	}
	defer s.backend.workWG.Done()

	raw, err := io.ReadAll(r)
	if err != nil {
		s.logger.Err(err).Msg("reading DATA")
		return err
	}
	metrics.SMTPBytesReceived(int64(len(raw)))

	ctx := s.workContext()
	s.mu.Lock()
	from := s.from
	recipients := append([]string(nil), s.recipients...)
	results := make(map[string]*resolver.Result, len(s.results))
	for addr, result := range s.results {
		results[addr] = result
	}
	s.mu.Unlock()
	if len(recipients) == 0 {
		metrics.SMTPMessageRejected()
		return smtpErr(554, "no valid recipients")
	}
	// Hand the RCPT-phase results to deliver via WithResolved. recipients are
	// already policy.SanitizeAddr-normalized (set in Rcpt), and WithResolved
	// re-runs policy.SanitizeAddr on the key, so the map lookup aligns
	// byte-for-byte.
	opts := make([]ingest.AcceptOption, 0, len(results))
	for addr, r := range results {
		opts = append(opts, ingest.WithResolved(addr, r))
	}
	res, err := s.backend.ingest.Accept(ctx, ingest.Envelope{
		Source:     "smtp",
		MailFrom:   from,
		Recipients: recipients,
	}, raw, opts...)
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
	s.mu.Lock()
	defer s.mu.Unlock()
	s.from = ""
	s.recipients = nil
	s.results = nil
}

func (s *session) Logout() error {
	metrics.SMTPSessionClosed()
	return nil
}

func smtpErr(code int, msg string) error {
	return &gosmtp.SMTPError{
		Code:    code,
		Message: msg,
	}
}
