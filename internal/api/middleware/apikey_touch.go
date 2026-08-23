package middleware

import (
	"context"
	"net"
	"sync"
	"time"

	"github.com/google/uuid"
)

const apiKeyTouchInterval = time.Minute

var apiKeyLastTouch sync.Map // uuid.UUID -> time.Time

func clientIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return remoteAddr
	}
	return host
}

func touchAPIKeyAsync(st authStore, keyID uuid.UUID, remoteAddr string) {
	now := time.Now()
	if last, ok := apiKeyLastTouch.Load(keyID); ok {
		if now.Sub(last.(time.Time)) < apiKeyTouchInterval {
			return
		}
	}
	apiKeyLastTouch.Store(keyID, now)

	ip := clientIP(remoteAddr)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = st.TouchAPIKey(ctx, keyID, ip)
	}()
}
