package outbound

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/smtp"
	"net/textproto"
	"strconv"
	"strings"
	"tabmail/internal/store"

	"tabmail/internal/config"
)

// DeliverRelay sends email through a configured SMTP relay.
func DeliverRelay(ctx context.Context, cfg config.Outbound, from string, to []string, mime []byte) error {
	return deliverRelayTLS(ctx, cfg, from, to, mime, &tls.Config{ServerName: cfg.RelayHost})
}

// deliverRelayTLS is the relay adapter's transport path. Its caller owns the
// trust policy; the exported entry point always uses normal system trust.
func deliverRelayTLS(ctx context.Context, cfg config.Outbound, from string, to []string, mime []byte, tlsConf *tls.Config) (err error) {
	defer func() { err = smtpContextError(ctx, err) }()
	addr := net.JoinHostPort(cfg.RelayHost, strconv.Itoa(cfg.RelayPort))

	conn, release, err := dialSMTPContext(ctx, addr)
	if err != nil {
		return fmt.Errorf("connect relay %s: %w", addr, err)
	}
	defer release()
	responseBudget := conn.(*smtpResponseConn)
	if strings.ToLower(cfg.RelayTLS) == "tls" {
		tlsConn := tls.Client(conn, tlsConf)
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			return fmt.Errorf("connect relay %s: %w", addr, smtpContextError(ctx, err))
		}
		conn = tlsConn
	}

	client, err := smtp.NewClient(conn, cfg.RelayHost)
	err = responseBudget.responseError(err)
	if err != nil {
		conn.Close()
		return fmt.Errorf("smtp client: %w", err)
	}
	defer client.Close()
	// Guard the greeting before Mail/Auth/StartTLS can run their implicit
	// hello followed by another command after a truncated positive reply.
	if err := responseBudget.responseError(client.Hello("localhost")); err != nil {
		return fmt.Errorf("smtp greeting: %w", err)
	}

	if strings.ToLower(cfg.RelayTLS) == "starttls" {
		if err := responseBudget.responseError(client.StartTLS(tlsConf)); err != nil {
			return fmt.Errorf("starttls: %w", err)
		}
	}

	if cfg.RelayUser != "" {
		auth := smtp.PlainAuth("", cfg.RelayUser, cfg.RelayPass, cfg.RelayHost)
		if err := responseBudget.responseError(client.Auth(auth)); err != nil {
			return fmt.Errorf("auth: %w", err)
		}
	}

	return sendSMTP(client, from, to, mime)
}

// DeliverDirect resolves recipient MX records, or connects directly to a valid
// SMTP address literal without DNS (RFC 5321 section 4.1.3).
// When requireTLS is true, delivery fails if STARTTLS is unavailable or negotiation fails,
// preventing MITM downgrade attacks.
func DeliverDirect(ctx context.Context, from string, to []string, mime []byte, requireTLS bool) error {
	return deliverDirectWith(ctx, from, to, mime, requireTLS, smtpMXLookup(&net.Resolver{}), deliverDirectMX)
}

type smtpDNSResolver interface {
	LookupMX(context.Context, string) ([]*net.MX, error)
	LookupIPAddr(context.Context, string) ([]net.IPAddr, error)
}

// Keep both DNS record types bound to the same per-delivery resolver.
func smtpMXLookup(resolver smtpDNSResolver) func(context.Context, string) ([]*net.MX, error) {
	return func(ctx context.Context, domain string) ([]*net.MX, error) {
		mxs, err := resolver.LookupMX(ctx, domain)
		if err == nil {
			return mxs, nil
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		var dnsErr *net.DNSError
		if !errors.As(err, &dnsErr) || !dnsErr.IsNotFound || dnsErr.Temporary() {
			return nil, err
		}
		// Go reports both NXDOMAIN and an empty MX answer as IsNotFound.
		// RFC 5321's implicit MX needs a usable address for the domain; never
		// turn an unproven not-found result into a delivery attempt.
		addresses, addressErr := resolver.LookupIPAddr(ctx, domain)
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		if addressErr != nil {
			return nil, fmt.Errorf("implicit MX address lookup: %w", addressErr)
		}
		for _, address := range addresses {
			if address.IP.To16() != nil {
				// The existing empty-answer path uses the domain as its MX.
				return nil, nil
			}
		}
		return nil, err
	}
}

// deliverDirectWith keeps DNS normalization and direct-delivery orchestration
// shared by the production entry point and offline tests. Dependencies are
// per-call so independent deliveries never share mutable test hooks.
func deliverDirectWith(ctx context.Context, from string, to []string, mime []byte, requireTLS bool,
	resolve func(context.Context, string) ([]*net.MX, error),
	session func(context.Context, string, string, string, []string, []byte, bool) error,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	for _, address := range to {
		// Keep the established DNS route contract (including absolute domain
		// names) unchanged; malformed literals must not become DNS targets.
		if !strings.ContainsAny(address, "[]") {
			continue
		}
		if _, err := ParseRecipientAddress(address); err != nil {
			return &textproto.Error{Code: 553, Msg: "5.1.3 Invalid recipient address"}
		}
	}
	byDomain := groupByDomain(to)
	var failures []error
	for domain, rcpts := range byDomain {
		if err := ctx.Err(); err != nil {
			return err
		}
		var lastErr error
		mxs, err := lookupMXWithResolver(ctx, domain, resolve)
		if err != nil {
			failures = append(failures, fmt.Errorf("mx lookup %s: %w", domain, err))
			continue
		}
		delivered := false
		for _, mx := range mxs {
			host := strings.TrimSuffix(mx, ".")
			addr := net.JoinHostPort(host, "25")
			err := session(ctx, host, addr, from, rcpts, mime, requireTLS)
			if err != nil {
				if errors.Is(err, store.ErrOutboundUncertain) || ctx.Err() != nil {
					return err
				}
				lastErr = err
				continue
			}
			delivered = true
			break
		}
		if !delivered {
			if lastErr == nil {
				lastErr = fmt.Errorf("all MX hosts failed for %s", domain)
			}
			failures = append(failures, lastErr)
		}
	}
	return errors.Join(failures...)
}

// deliverDirectMX is the production per-MX session, including opportunistic
// plaintext reconnect when the caller explicitly allows that existing policy.
func deliverDirectMX(ctx context.Context, host, addr, from string, to []string, mime []byte, requireTLS bool) error {
	return deliverDirectMXTLS(ctx, host, addr, from, to, mime, requireTLS, &tls.Config{ServerName: host})
}

func deliverDirectMXTLS(ctx context.Context, host, addr, from string, to []string, mime []byte, requireTLS bool, tlsConf *tls.Config) (err error) {
	defer func() { err = smtpContextError(ctx, err) }()
	conn, release, err := dialSMTPContext(ctx, addr)
	if err != nil {
		return err
	}
	defer func() { release() }()
	responseBudget := conn.(*smtpResponseConn)
	client, err := smtp.NewClient(conn, host)
	err = responseBudget.responseError(err)
	if err != nil {
		return fmt.Errorf("smtp client: %w", err)
	}
	defer func() {
		if client != nil {
			_ = client.Close()
		}
	}()
	// Extension suppresses hello errors. Preserve a failed greeting before
	// deciding whether the server omitted STARTTLS; use net/smtp's default name
	// and keep its existing EHLO-to-HELO fallback.
	if err := responseBudget.responseError(client.Hello("localhost")); err != nil {
		return fmt.Errorf("smtp greeting: %w", err)
	}
	if ok, _ := client.Extension("STARTTLS"); ok {
		if tlsErr := responseBudget.responseError(client.StartTLS(tlsConf)); tlsErr != nil {
			_ = client.Close()
			release()
			// A canceled handshake is not a reason to reconnect or try another MX.
			if ctx.Err() != nil {
				return smtpContextError(ctx, tlsErr)
			}
			if requireTLS {
				return fmt.Errorf("STARTTLS required but negotiation failed for %s: %w", host, tlsErr)
			}
			conn, release, err = dialSMTPContext(ctx, addr)
			// Keep cleanup callable even when no replacement socket was allocated.
			if err != nil {
				release = func() {}
				return fmt.Errorf("reconnect to %s after TLS failure: %w", host, err)
			}
			responseBudget = conn.(*smtpResponseConn)
			client, err = smtp.NewClient(conn, host)
			err = responseBudget.responseError(err)
			if err != nil {
				return fmt.Errorf("smtp client after reconnect: %w", err)
			}
			if err := responseBudget.responseError(client.Hello("localhost")); err != nil {
				return fmt.Errorf("smtp greeting after reconnect: %w", err)
			}
		}
	} else if requireTLS {
		return fmt.Errorf("STARTTLS required but not supported by %s", host)
	}
	return sendSMTP(client, from, to, mime)
}

func sendSMTP(client *smtp.Client, from string, to []string, mime []byte) error {
	reader, responseError := guardSMTPReplyReader(client.Text.Reader.R)
	client.Text.Reader.R = reader
	// Hello and any STARTTLS negotiation have already completed in the
	// adapters. Check the current capabilities before sending any envelope.
	if requiresSMTPUTF8(from, to, mime) {
		if ok, _ := client.Extension("SMTPUTF8"); !ok {
			return errors.New("smtp: server does not support SMTPUTF8 required by envelope or headers")
		}
	}
	if err := responseError(client.Mail(from)); err != nil {
		return fmt.Errorf("MAIL FROM: %w", err)
	}
	for _, rcpt := range to {
		if err := responseError(client.Rcpt(rcpt)); err != nil {
			return fmt.Errorf("RCPT TO %s: %w", rcpt, err)
		}
	}
	w, err := client.Data()
	err = responseError(err)
	if err != nil {
		return fmt.Errorf("DATA: %w", err)
	}
	if _, err := w.Write(mime); err != nil {
		return fmt.Errorf("write data: %w", err)
	}
	if err := responseError(w.Close()); err != nil {
		var reply *textproto.Error
		// net/smtp expects exactly 250 here. textproto.Error also represents
		// unexpected positive, intermediate, or invalid reply codes; only a
		// 4xx/5xx completion proves rejection and permits ordinary failure
		// handling. Other final replies must not trigger another delivery.
		if errors.As(err, &reply) && reply.Code >= 400 && reply.Code < 600 {
			return fmt.Errorf("close data: %w", err)
		}
		return fmt.Errorf("%w: DATA final reply: %w", store.ErrOutboundUncertain, err)
	}
	// DATA final reply was successful; QUIT is merely connection cleanup.
	_ = client.Quit()
	return nil
}

func requiresSMTPUTF8(from string, to []string, message []byte) bool {
	containsNonASCII := func(value string) bool {
		for i := 0; i < len(value); i++ {
			if value[i] >= 0x80 {
				return true
			}
		}
		return false
	}
	if containsNonASCII(from) {
		return true
	}
	for _, address := range to {
		if containsNonASCII(address) {
			return true
		}
	}
	// Build emits ASCII MIME part headers. Inspect its top-level headers,
	// stopping at the empty line so UTF-8 body content remains compatible.
	// Encoded words and encoded attachment names are ASCII on the wire.
	for len(message) > 0 {
		line, rest, _ := bytes.Cut(message, []byte("\n"))
		if len(line) == 0 || (len(line) == 1 && line[0] == '\r') {
			break
		}
		if containsNonASCII(string(line)) {
			return true
		}
		message = rest
	}
	return false
}

func groupByDomain(addrs []string) map[string][]string {
	m := make(map[string][]string)
	for _, addr := range addrs {
		// Quoted local parts may contain @. The final @ separates the domain
		// in the canonical addr-spec stored by the submission boundary.
		if at := strings.LastIndexByte(addr, '@'); at >= 0 {
			domain := addr[at+1:]
			m[domain] = append(m[domain], addr)
		}
	}
	return m
}

func lookupMXWithResolver(ctx context.Context, domain string, resolve func(context.Context, string) ([]*net.MX, error)) ([]string, error) {
	if host, literal, err := addressLiteralHost(domain); literal {
		if err != nil {
			return nil, &textproto.Error{Code: 553, Msg: "5.1.3 Invalid recipient address literal"}
		}
		return []string{host}, nil
	}
	records, err := resolve(ctx, domain)
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return []string{domain}, nil
	}
	// RFC 7505: a sole preference-0 root exchange explicitly declines mail.
	// Keep the SMTP rejection in the error chain for the existing recipient
	// classifier, and never turn this route into an empty-host connection.
	if len(records) == 1 && records[0].Pref == 0 && records[0].Host == "." {
		return nil, &textproto.Error{Code: 556, Msg: "5.1.10 Recipient domain does not accept mail (null MX)"}
	}
	// A root exchange in any other answer is malformed. Conservatively leave
	// it retryable without trying another MX or falling back to the domain.
	for _, mx := range records {
		if mx.Host == "." {
			return nil, fmt.Errorf("invalid MX answer: root exchange must be the sole preference-0 record")
		}
	}
	hosts := make([]string, len(records))
	for i, mx := range records {
		hosts[i] = mx.Host
	}
	return hosts, nil
}
