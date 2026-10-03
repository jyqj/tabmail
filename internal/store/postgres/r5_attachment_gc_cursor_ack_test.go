package postgres_test

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"tabmail/internal/config"
	"tabmail/internal/store/postgres"
)

// The only fault injected here is loss of the UPDATE acknowledgement on a real
// PostgreSQL wire. PostgreSQL must first expose the committed cursor to an
// independent observer; then the proxy discards CommandComplete/ReadyForQuery.
// This is not an UPDATE rejection relabelled as an uncertain commit.
func r5GCCursorAckProxy(t *testing.T, ctx context.Context, f *companyFixture, dsn string, want uuid.UUID) (string, <-chan error) {
	t.Helper()
	cfg, err := pgx.ParseConfig(dsn)
	must(t, err)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	must(t, err)
	injected := make(chan error, 1)
	var fired atomic.Bool
	var mu sync.Mutex
	connections := map[net.Conn]bool{}
	var wg sync.WaitGroup
	track := func(c net.Conn) {
		mu.Lock()
		connections[c] = true
		mu.Unlock()
	}
	closeConn := func(c net.Conn) {
		c.Close()
		mu.Lock()
		delete(connections, c)
		mu.Unlock()
	}
	t.Cleanup(func() {
		listener.Close()
		mu.Lock()
		for c := range connections {
			c.Close()
		}
		mu.Unlock()
		wg.Wait()
	})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			client, err := listener.Accept()
			if err != nil {
				return
			}
			track(client)
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer closeConn(client)
				network, address := "tcp", net.JoinHostPort(cfg.Host, strconv.Itoa(int(cfg.Port)))
				if strings.HasPrefix(cfg.Host, "/") {
					network, address = "unix", cfg.Host+"/.s.PGSQL."+strconv.Itoa(int(cfg.Port))
				}
				upstream, err := (&net.Dialer{}).DialContext(ctx, network, address)
				if err != nil {
					return
				}
				track(upstream)
				defer closeConn(upstream)
				deadline, _ := ctx.Deadline()
				client.SetDeadline(deadline)
				upstream.SetDeadline(deadline)
				// The disposable local frontend uses clear PG framing, while the
				// real upstream keeps its configured TLS verification, if any.
				if cfg.TLSConfig != nil {
					ssl := make([]byte, 8)
					binary.BigEndian.PutUint32(ssl, 8)
					binary.BigEndian.PutUint32(ssl[4:], 80877103)
					if _, err = upstream.Write(ssl); err != nil {
						return
					}
					var reply [1]byte
					if _, err = io.ReadFull(upstream, reply[:]); err != nil {
						return
					}
					if reply[0] == 'S' {
						secured := tls.Client(upstream, cfg.TLSConfig.Clone())
						if err = secured.HandshakeContext(ctx); err != nil {
							return
						}
						upstream = secured
					} else if reply[0] != 'N' || len(cfg.Fallbacks) == 0 {
						return
					}
				}
				var size [4]byte
				if _, err = io.ReadFull(client, size[:]); err != nil {
					return
				}
				n := int(binary.BigEndian.Uint32(size[:]))
				if n < 8 || n > 1<<20 {
					return
				}
				startup := make([]byte, n)
				copy(startup, size[:])
				if _, err = io.ReadFull(client, startup[4:]); err != nil {
					return
				}
				if _, err = upstream.Write(startup); err != nil {
					return
				}
				var armed atomic.Bool
				frontendDone := make(chan struct{})
				go func() {
					defer close(frontendDone)
					for {
						packet, err := r5GCReadPacket(client)
						if err != nil {
							upstream.Close()
							return
						}
						if (packet[0] == 'P' || packet[0] == 'Q') && bytes.Contains(packet[5:], []byte("UPDATE company_attachment_gc_cursor SET last_tenant=")) {
							armed.Store(true)
						}
						if _, err = upstream.Write(packet); err != nil {
							return
						}
					}
				}()
				defer func() { client.Close(); upstream.Close(); <-frontendDone }()
				for {
					packet, err := r5GCReadPacket(upstream)
					if err != nil {
						return
					}
					if packet[0] == 'C' && bytes.HasPrefix(packet[5:], []byte("UPDATE 1")) && armed.Load() && fired.CompareAndSwap(false, true) {
						tick := time.NewTicker(5 * time.Millisecond)
						defer tick.Stop()
						for {
							var committed bool
							err = f.pool.QueryRow(ctx, `SELECT COALESCE(last_tenant=$1,FALSE) FROM company_attachment_gc_cursor WHERE singleton`, want).Scan(&committed)
							if err != nil || committed {
								injected <- err
								return // Discard the acknowledgement, close BOTH wire ends.
							}
							select {
							case <-ctx.Done():
								injected <- ctx.Err()
								return
							case <-tick.C:
							}
						}
					}
					if _, err = client.Write(packet); err != nil {
						return
					}
				}
			}()
		}
	}()
	u, err := url.Parse(dsn)
	must(t, err)
	u.Host = listener.Addr().String()
	q := u.Query()
	q.Set("sslmode", "disable")
	q.Set("host", "127.0.0.1")
	_, port, err := net.SplitHostPort(listener.Addr().String())
	must(t, err)
	q.Set("port", port)
	u.RawQuery = q.Encode()
	return u.String(), injected
}

func r5GCReadPacket(r io.Reader) ([]byte, error) {
	var header [5]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return nil, err
	}
	n := int(binary.BigEndian.Uint32(header[1:]))
	if n < 4 || n > 16<<20 {
		return nil, fmt.Errorf("invalid PostgreSQL fixture packet length %d", n)
	}
	packet := make([]byte, n+1)
	copy(packet, header[:])
	_, err := io.ReadFull(r, packet[5:])
	return packet, err
}

func TestR5AttachmentGCTenantCommittedCursorLostAcknowledgement(t *testing.T) {
	fs, as, dsn := r5GCTenantFixture(t, 21)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	r5GCTenantExpireAll(t, fs[0], ctx)
	proxyDSN, injected := r5GCCursorAckProxy(t, ctx, fs[0], dsn, fs[0].tenant.ID)
	st, err := postgres.New(ctx, config.DB{DSN: proxyDSN, MaxOpenConns: 1, MaxIdleConns: 0, ConnMaxLifetime: time.Minute})
	must(t, err)
	defer st.Close()
	if err = st.SweepCompanyMetadata(ctx); err == nil {
		t.Fatal("lost cursor acknowledgement reported success")
	}
	select {
	case err := <-injected:
		must(t, err)
	case <-ctx.Done():
		t.Fatal("did not observe committed cursor before dropping acknowledgement")
	}
	r5GCTenantCursor(t, fs[0], ctx, fs[0].tenant.ID)
	for i := range fs {
		r5ReferenceCounts(t, fs[i], ctx, as[i], 1, 0)
	}
	r5GCTenantNoSession(t, fs[0], ctx)
	// Committed progress is authoritative even though the caller got an error.
	// No speculative GC ran for tenant 1; a fresh scanner moves to 2..21.
	must(t, fs[0].st.SweepCompanyMetadata(ctx))
	for i := 1; i < 21; i++ {
		r5ReferenceCounts(t, fs[i], ctx, as[i], 0, 1)
	}
	r5ReferenceCounts(t, fs[0], ctx, as[0], 1, 0)
	must(t, fs[0].st.SweepCompanyMetadata(ctx))
	r5ReferenceCounts(t, fs[0], ctx, as[0], 0, 1)
}
