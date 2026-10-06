package outbound

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/smtp"
	"net/textproto"
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
	addr := fmt.Sprintf("%s:%d", cfg.RelayHost, cfg.RelayPort)

	conn, release, err := dialSMTPContext(ctx, addr)
	if err != nil {
		return fmt.Errorf("connect relay %s: %w", addr, err)
	}
	defer release()
	if strings.ToLower(cfg.RelayTLS) == "tls" {
		tlsConn := tls.Client(conn, tlsConf)
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			return fmt.Errorf("connect relay %s: %w", addr, smtpContextError(ctx, err))
		}
		conn = tlsConn
	}

	client, err := smtp.NewClient(conn, cfg.RelayHost)
	if err != nil {
		conn.Close()
		return fmt.Errorf("smtp client: %w", err)
	}
	defer client.Close()

	if strings.ToLower(cfg.RelayTLS) == "starttls" {
		if err := client.StartTLS(tlsConf); err != nil {
			return fmt.Errorf("starttls: %w", err)
		}
	}

	if cfg.RelayUser != "" {
		auth := smtp.PlainAuth("", cfg.RelayUser, cfg.RelayPass, cfg.RelayHost)
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("auth: %w", err)
		}
	}

	return sendSMTP(client, from, to, mime)
}

// DeliverDirect sends email by resolving MX records for each recipient domain.
// When requireTLS is true, delivery fails if STARTTLS is unavailable or negotiation fails,
// preventing MITM downgrade attacks.
func DeliverDirect(ctx context.Context, from string, to []string, mime []byte, requireTLS bool) error {
	return deliverDirectWith(ctx, from, to, mime, requireTLS, (&net.Resolver{}).LookupMX, deliverDirectMX)
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
			addr := fmt.Sprintf("%s:25", host)
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
	client, err := smtp.NewClient(conn, host)
	if err != nil {
		return fmt.Errorf("smtp client: %w", err)
	}
	defer func() {
		if client != nil {
			_ = client.Close()
		}
	}()
	if ok, _ := client.Extension("STARTTLS"); ok {
		if tlsErr := client.StartTLS(tlsConf); tlsErr != nil {
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
			client, err = smtp.NewClient(conn, host)
			if err != nil {
				return fmt.Errorf("smtp client after reconnect: %w", err)
			}
		}
	} else if requireTLS {
		return fmt.Errorf("STARTTLS required but not supported by %s", host)
	}
	return sendSMTP(client, from, to, mime)
}

func sendSMTP(client *smtp.Client, from string, to []string, mime []byte) error {
	if err := client.Mail(from); err != nil {
		return fmt.Errorf("MAIL FROM: %w", err)
	}
	for _, rcpt := range to {
		if err := client.Rcpt(rcpt); err != nil {
			return fmt.Errorf("RCPT TO %s: %w", rcpt, err)
		}
	}
	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("DATA: %w", err)
	}
	if _, err := w.Write(mime); err != nil {
		return fmt.Errorf("write data: %w", err)
	}
	if err := w.Close(); err != nil {
		var reply *textproto.Error
		if errors.As(err, &reply) {
			return fmt.Errorf("close data: %w", err)
		}
		return fmt.Errorf("%w: DATA final reply: %v", store.ErrOutboundUncertain, err)
	}
	// DATA final reply was successful; QUIT is merely connection cleanup.
	_ = client.Quit()
	return nil
}

func groupByDomain(addrs []string) map[string][]string {
	m := make(map[string][]string)
	for _, addr := range addrs {
		parts := strings.SplitN(addr, "@", 2)
		if len(parts) == 2 {
			m[parts[1]] = append(m[parts[1]], addr)
		}
	}
	return m
}

func lookupMXWithResolver(ctx context.Context, domain string, resolve func(context.Context, string) ([]*net.MX, error)) ([]string, error) {
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
