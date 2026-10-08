package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestR5ProxyChainCanonicalIdentity(t *testing.T) {
	for _, tc := range []struct {
		name, peer, want string
		forwarded, real  []string
	}{
		{name: "direct peer ignores forwarding", peer: "198.51.100.7:4132", forwarded: []string{"203.0.113.8"}, real: []string{"192.0.2.8"}, want: "198.51.100.7"},
		{name: "trusted single hop", peer: "10.0.0.9:4132", forwarded: []string{"198.51.100.7"}, want: "198.51.100.7"},
		{name: "untrusted prefix is not the client", peer: "10.0.0.9:4132", forwarded: []string{"203.0.113.8, 198.51.100.7"}, want: "198.51.100.7"},
		{name: "trusted suffix is stripped", peer: "10.0.0.9:4132", forwarded: []string{"203.0.113.8, 198.51.100.7, 10.1.0.3"}, want: "198.51.100.7"},
		{name: "chain overrides conflicting real IP", peer: "10.0.0.9:4132", forwarded: []string{"203.0.113.8, 198.51.100.7"}, real: []string{"192.0.2.8"}, want: "198.51.100.7"},
		{name: "multiple header lines retain hop order", peer: "10.0.0.9:4132", forwarded: []string{"203.0.113.8", "198.51.100.7, 10.1.0.3"}, want: "198.51.100.7"},
		{name: "invalid nearest hop fails to peer", peer: "10.0.0.9:4132", forwarded: []string{"198.51.100.7, not-an-ip"}, real: []string{"203.0.113.8"}, want: "10.0.0.9"},
		{name: "invalid trusted suffix fails to peer", peer: "10.0.0.9:4132", forwarded: []string{"198.51.100.7, not-an-ip, 10.1.0.3"}, want: "10.0.0.9"},
		{name: "invalid untrusted prefix stays irrelevant", peer: "10.0.0.9:4132", forwarded: []string{"not-an-ip, 198.51.100.7"}, want: "198.51.100.7"},
		{name: "empty nearest hop fails to peer", peer: "10.0.0.9:4132", forwarded: []string{"198.51.100.7,"}, want: "10.0.0.9"},
		{name: "empty chain fails to peer", peer: "10.0.0.9:4132", forwarded: []string{""}, real: []string{"198.51.100.7"}, want: "10.0.0.9"},
		{name: "port is not an IP header", peer: "10.0.0.9:4132", forwarded: []string{"198.51.100.7:443"}, want: "10.0.0.9"},
		{name: "scoped IPv6 is not a forwarded client", peer: "10.0.0.9:4132", forwarded: []string{"fe80::7%eth0"}, want: "10.0.0.9"},
		{name: "IPv6 chain canonicalized", peer: "[fd00::9]:4132", forwarded: []string{"203.0.113.8, 2001:0db8:0:0::7, fd00::3"}, want: "2001:db8::7"},
		{name: "IPv4 mapped chain canonicalized", peer: "10.0.0.9:4132", forwarded: []string{"::ffff:198.51.100.7"}, want: "198.51.100.7"},
		{name: "direct IPv6 canonicalized", peer: "[2001:0db8:0:0::7]:4132", want: "2001:db8::7"},
		{name: "direct mapped IPv4 canonicalized", peer: "[::ffff:198.51.100.7]:4132", want: "198.51.100.7"},
		{name: "legacy real IP without chain", peer: "10.0.0.9:4132", real: []string{" 198.51.100.7 "}, want: "198.51.100.7"},
		{name: "legacy real IPv6 canonicalized", peer: "10.0.0.9:4132", real: []string{"2001:0db8:0:0::7"}, want: "2001:db8::7"},
		{name: "multiple real IP values rejected", peer: "10.0.0.9:4132", real: []string{"198.51.100.7", "203.0.113.8"}, want: "10.0.0.9"},
		{name: "comma real IP rejected", peer: "10.0.0.9:4132", real: []string{"198.51.100.7, 203.0.113.8"}, want: "10.0.0.9"},
		{name: "invalid real IP rejected", peer: "10.0.0.9:4132", real: []string{"not-an-ip"}, want: "10.0.0.9"},
		{name: "missing peer never trusts headers", peer: "", forwarded: []string{"198.51.100.7"}, real: []string{"203.0.113.8"}, want: "unknown"},
		{name: "invalid peer never trusts headers", peer: "broken:peer", real: []string{"203.0.113.8"}, want: "unknown"},
		{name: "portless peer has no proxy trust", peer: "10.0.0.9", forwarded: []string{"198.51.100.7"}, real: []string{"203.0.113.8"}, want: "10.0.0.9"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/", nil)
			request.RemoteAddr = tc.peer
			for _, value := range tc.forwarded {
				request.Header.Add("X-Forwarded-For", value)
			}
			for _, value := range tc.real {
				request.Header.Add("X-Real-IP", value)
			}
			limiter := NewRateLimiter(nil, nil, 1, []string{"10.0.0.0/8", "fd00::/8"})
			if got := limiter.realIP(request); got != tc.want {
				t.Fatalf("client identity = %q; want %q", got, tc.want)
			}
		})
	}
}

func TestR5ProxyChainCannotRotatePublicRateLimitIdentity(t *testing.T) {
	for _, tc := range []struct {
		name, peer, client string
		forwarded          [][]string
		real               []string
	}{
		{name: "spoofed prefix and conflicting real IP", peer: "10.0.0.9:4132", client: "198.51.100.7", forwarded: [][]string{{"203.0.113.1, 198.51.100.7"}, {"203.0.113.2, 198.51.100.7"}, {"not-an-ip, 198.51.100.7"}}, real: []string{"192.0.2.1", "192.0.2.2", "192.0.2.3"}},
		{name: "multiple lines cannot hide nearest client", peer: "10.0.0.9:4132", client: "198.51.100.7", forwarded: [][]string{{"203.0.113.1", "198.51.100.7, 10.0.0.3"}, {"203.0.113.2", "198.51.100.7, 10.0.0.3"}, {"203.0.113.3", "198.51.100.7, 10.0.0.3"}}},
		{name: "IPv6 spellings share quota", peer: "10.0.0.9:4132", client: "2001:db8::7", forwarded: [][]string{{"2001:0db8:0:0:0:0:0:7"}, {"2001:db8::7"}, {"2001:0db8::0007"}}},
		{name: "IPv4 and mapped address share quota", peer: "10.0.0.9:4132", client: "198.51.100.7", forwarded: [][]string{{"198.51.100.7"}, {"::ffff:198.51.100.7"}, {"::ffff:c633:6407"}}},
		{name: "invalid headers stay on peer bucket", peer: "10.0.0.9:4132", client: "10.0.0.9", forwarded: [][]string{{"invented-one"}, {"invented-two"}, {"invented-three"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server, client := windowRedis(t)
			delivered := 0
			handler := NewRateLimiter(client, nil, 1, []string{"10.0.0.0/8"}).Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				delivered++
				w.WriteHeader(http.StatusNoContent)
			}))
			for i, values := range tc.forwarded {
				request := httptest.NewRequest(http.MethodGet, "/api/v1/auth/login", nil)
				request.RemoteAddr = tc.peer
				for _, value := range values {
					request.Header.Add("X-Forwarded-For", value)
				}
				if len(tc.real) != 0 {
					request.Header.Set("X-Real-IP", tc.real[i])
				}
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, request)
				want := http.StatusTooManyRequests
				if i == 0 {
					want = http.StatusNoContent
				}
				if response.Code != want {
					t.Errorf("request %d status = %d; want %d", i+1, response.Code, want)
				}
				if i > 0 && response.Header().Get("Retry-After") != "60" {
					t.Errorf("limited request %d lacks retry budget", i+1)
				}
			}
			if delivered != 1 {
				t.Errorf("public handler admitted %d requests from one client; want 1", delivered)
			}
			keys := server.Keys()
			if len(keys) != 1 || keys[0] != "rate:ip:"+tc.client {
				t.Errorf("unexpected rate buckets: %v", keys)
			}
		})
	}
}
