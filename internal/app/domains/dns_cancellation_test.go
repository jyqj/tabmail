package domainapp

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"golang.org/x/net/dns/dnsmessage"
	"tabmail/internal/models"
)

type dnsCancellationStore struct {
	*domainTestStore
	updates, identities, audits int
}

func (s *dnsCancellationStore) UpdateZone(context.Context, *models.DomainZone) error {
	s.updates++
	return nil
}
func (s *dnsCancellationStore) UpdateSendIdentitiesVerifiedByZone(context.Context, uuid.UUID, bool) error {
	s.identities++
	return nil
}
func (s *dnsCancellationStore) InsertAudit(context.Context, *models.AuditEntry) error {
	s.audits++
	return nil
}

func dnsCancellationFixture() (*dnsCancellationStore, *models.DomainZone, *fakeResolverInvalidator, *Service) {
	st := &dnsCancellationStore{domainTestStore: newDomainTestStore()}
	zone := &models.DomainZone{ID: uuid.New(), TenantID: uuid.New(), Domain: "cancel.example.invalid", TXTRecord: "tabmail-verify=fixture"}
	st.zones[zone.ID] = zone
	inv := &fakeResolverInvalidator{}
	return st, zone, inv, NewService(st, nil, "mx.example.invalid", inv, zerolog.Nop())
}

func TestDomainDNSCancellationStopsLaterQueriesAndWrites(t *testing.T) {
	for _, operation := range []string{"verify", "status"} {
		for _, stage := range []string{"before", "txt", "mx", "spf", "dmarc"} {
			t.Run(operation+"/"+stage, func(t *testing.T) {
				st, zone, inv, svc := dnsCancellationFixture()
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				if stage == "before" {
					cancel()
				}
				queries := []string{}
				txtCount := 0
				svc.SetResolvers(func(name string) ([]string, error) {
					txtCount++
					current := "txt"
					if txtCount == 2 {
						current = "spf"
					}
					if txtCount == 3 {
						current = "dmarc"
					}
					queries = append(queries, current)
					if stage == current {
						cancel()
					}
					return []string{zone.TXTRecord, "v=spf1 -all", "v=DMARC1; p=reject"}, nil
				}, func(string) ([]*net.MX, error) {
					queries = append(queries, "mx")
					if stage == "mx" {
						cancel()
					}
					return []*net.MX{{Host: "mx.example.invalid.", Pref: 10}}, nil
				})
				var err error
				if operation == "verify" {
					var result *models.DomainZone
					result, _, err = svc.TriggerVerify(ctx, adminActor(zone.TenantID), zone.ID)
					if result != nil {
						t.Error("cancelled verification returned a successful zone")
					}
				} else {
					var result *VerificationStatus
					result, err = svc.VerificationStatus(ctx, adminActor(zone.TenantID), zone.ID)
					if result != nil {
						t.Error("cancelled status returned a successful result")
					}
				}
				if !errors.Is(err, context.Canceled) {
					t.Errorf("request cancellation was lost: %v", err)
				}
				wantQueries := map[string]int{"before": 0, "txt": 1, "mx": 2, "spf": 3, "dmarc": 4}[stage]
				if len(queries) != wantQueries {
					t.Errorf("queries continued past cancellation: %v, want %d", queries, wantQueries)
				}
				if st.updates != 0 || st.identities != 0 || st.audits != 0 || len(inv.zonesCleared) != 0 {
					t.Errorf("cancelled DNS verification produced writes: zone=%d identity=%d audit=%d cache=%d", st.updates, st.identities, st.audits, len(inv.zonesCleared))
				}
			})
		}
	}
}

func TestDomainDNSCancellationPreservesSuccessfulVerification(t *testing.T) {
	st, zone, inv, svc := dnsCancellationFixture()
	svc.SetResolvers(func(string) ([]string, error) {
		return []string{zone.TXTRecord, "v=spf1 -all", "v=DMARC1; p=reject"}, nil
	}, func(string) ([]*net.MX, error) { return []*net.MX{{Host: "mx.example.invalid.", Pref: 10}}, nil })
	got, checks, err := svc.TriggerVerify(context.Background(), adminActor(zone.TenantID), zone.ID)
	if err != nil || got == nil || !got.IsVerified || !got.MXVerified || got.VerifiedAt == nil || checks.SPF.Status != "pass" || checks.DMARC.Status != "pass" {
		t.Fatalf("successful verification changed: got=%v checks=%+v err=%v", got, checks, err)
	}
	if st.updates != 1 || st.identities != 1 || st.audits != 1 || len(inv.zonesCleared) != 1 {
		t.Fatalf("successful effects changed: %+v", st)
	}
}

// Use the production resolver functions and real UDP packets. Only the named
// DNS question is left unanswered; cancellation must interrupt that lookup.
func TestDomainDNSRequestCancelsProductionResolver(t *testing.T) {
	for _, blocked := range []dnsmessage.Type{dnsmessage.TypeTXT, dnsmessage.TypeMX} {
		t.Run(blocked.String(), func(t *testing.T) {
			server, err := net.ListenPacket("udp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			requested := make(chan struct{})
			joined := make(chan struct{})
			var observed sync.Once
			go func() {
				defer close(joined)
				buf := make([]byte, 4096)
				for {
					n, addr, err := server.ReadFrom(buf)
					if err != nil {
						return
					}
					var p dnsmessage.Parser
					h, err := p.Start(buf[:n])
					if err != nil {
						continue
					}
					question, err := p.Question()
					if err != nil {
						continue
					}
					if question.Type == blocked {
						observed.Do(func() { close(requested) })
						continue
					}
					b := dnsmessage.NewBuilder(nil, dnsmessage.Header{ID: h.ID, Response: true, Authoritative: true, RecursionAvailable: true, RecursionDesired: h.RecursionDesired})
					if b.StartQuestions() != nil || b.Question(question) != nil || b.StartAnswers() != nil {
						continue
					}
					if question.Type == dnsmessage.TypeTXT {
						if b.TXTResource(dnsmessage.ResourceHeader{Name: question.Name, Type: dnsmessage.TypeTXT, Class: dnsmessage.ClassINET, TTL: 60}, dnsmessage.TXTResource{TXT: []string{"tabmail-verify=fixture"}}) != nil {
							continue
						}
					}
					answer, err := b.Finish()
					if err == nil {
						_, _ = server.WriteTo(answer, addr)
					}
				}
			}()
			priorResolver := net.DefaultResolver
			net.DefaultResolver = &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, network, server.LocalAddr().String())
			}}
			st, zone, inv, svc := dnsCancellationFixture()
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan error, 1)
			go func() { _, _, err := svc.TriggerVerify(ctx, adminActor(zone.TenantID), zone.ID); done <- err }()
			finished := false
			t.Cleanup(func() {
				cancel()
				_ = server.Close()
				<-joined
				if !finished {
					select {
					case <-done:
					case <-time.After(5 * time.Second):
						t.Error("DNS worker did not finish after fixture cleanup")
					}
				}
				net.DefaultResolver = priorResolver
			})
			select {
			case <-requested:
			case <-time.After(5 * time.Second):
				t.Fatal("production resolver did not issue the expected loopback query")
			}
			cancel()
			select {
			case err := <-done:
				finished = true
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("production resolver lost request cancellation: %v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("request cancellation did not interrupt the active DNS lookup")
			}
			if st.updates != 0 || st.identities != 0 || st.audits != 0 || len(inv.zonesCleared) != 0 {
				t.Fatalf("cancelled production lookup caused effects: %+v", st)
			}
		})
	}
}
