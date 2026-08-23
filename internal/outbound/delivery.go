package outbound

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"strings"
	"time"

	"tabmail/internal/config"
)

// DeliverRelay sends email through a configured SMTP relay.
func DeliverRelay(ctx context.Context, cfg config.Outbound, from string, to []string, mime []byte) error {
	addr := fmt.Sprintf("%s:%d", cfg.RelayHost, cfg.RelayPort)

	var conn net.Conn
	var err error
	dialer := &net.Dialer{Timeout: 30 * time.Second}

	switch strings.ToLower(cfg.RelayTLS) {
	case "tls":
		tlsConf := &tls.Config{ServerName: cfg.RelayHost}
		conn, err = tls.DialWithDialer(dialer, "tcp", addr, tlsConf)
	default:
		conn, err = dialer.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("connect relay %s: %w", addr, err)
	}

	client, err := smtp.NewClient(conn, cfg.RelayHost)
	if err != nil {
		conn.Close()
		return fmt.Errorf("smtp client: %w", err)
	}
	defer client.Close()

	if strings.ToLower(cfg.RelayTLS) == "starttls" {
		tlsConf := &tls.Config{ServerName: cfg.RelayHost}
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
	byDomain := groupByDomain(to)
	var lastErr error
	for domain, rcpts := range byDomain {
		mxs, err := lookupMX(ctx, domain)
		if err != nil {
			lastErr = fmt.Errorf("mx lookup %s: %w", domain, err)
			continue
		}
		delivered := false
		for _, mx := range mxs {
			host := strings.TrimSuffix(mx, ".")
			addr := fmt.Sprintf("%s:25", host)
			conn, err := net.DialTimeout("tcp", addr, 30*time.Second)
			if err != nil {
				continue
			}
			client, err := smtp.NewClient(conn, mx)
			if err != nil {
				conn.Close()
				continue
			}

			if ok, _ := client.Extension("STARTTLS"); ok {
				tlsConf := &tls.Config{ServerName: host}
				if tlsErr := client.StartTLS(tlsConf); tlsErr != nil {
					client.Close()
					if requireTLS {
						lastErr = fmt.Errorf("STARTTLS required but negotiation failed for %s: %w", host, tlsErr)
						continue
					}
					conn2, err2 := net.DialTimeout("tcp", addr, 30*time.Second)
					if err2 != nil {
						lastErr = fmt.Errorf("reconnect to %s after TLS failure: %w", host, err2)
						continue
					}
					client, err = smtp.NewClient(conn2, mx)
					if err != nil {
						conn2.Close()
						continue
					}
				}
			} else if requireTLS {
				client.Close()
				lastErr = fmt.Errorf("STARTTLS required but not supported by %s", host)
				continue
			}

			if err := sendSMTP(client, from, rcpts, mime); err != nil {
				client.Close()
				lastErr = err
				continue
			}
			client.Close()
			delivered = true
			break
		}
		if !delivered && lastErr == nil {
			lastErr = fmt.Errorf("all MX hosts failed for %s", domain)
		}
	}
	return lastErr
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
		return fmt.Errorf("close data: %w", err)
	}
	return client.Quit()
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

func lookupMX(ctx context.Context, domain string) ([]string, error) {
	resolver := &net.Resolver{}
	records, err := resolver.LookupMX(ctx, domain)
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return []string{domain}, nil
	}
	hosts := make([]string, len(records))
	for i, mx := range records {
		hosts[i] = mx.Host
	}
	return hosts, nil
}
