package postgres_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"golang.org/x/crypto/bcrypt"
	"tabmail/internal/authn"
	"tabmail/internal/config"
	"tabmail/internal/models"
	"tabmail/internal/outbound"
	"tabmail/internal/testutil"
)

// This opt-in test uses the real production Next build, a real PostgreSQL
// database, the real API and a loopback SMTP server. It is a separate CI job;
// normal unit tests do not download browsers or open the fixed frontend port.
func TestR3BrowserJourney(t *testing.T) {
	if os.Getenv("TABMAIL_BROWSER_E2E") != "1" {
		t.Skip("run in the browser-journey CI job")
	}
	f := seedCompany(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	const password = "browser-only-password-123"
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	must(t, err)
	for _, u := range []*models.User{f.admin, f.employee, f.other} {
		_, err = f.pool.Exec(ctx, `UPDATE users SET password_hash=$2 WHERE id=$1`, u.ID, string(hash))
		must(t, err)
	}
	obj := testutil.NewMemoryObjectStore()
	raw := []byte("From: Client <client@recipient.test>\r\nTo: employee@company.test\r\nSubject: Browser welcome\r\nMessage-ID: <browser-welcome@recipient.test>\r\n\r\nPrivate browser journey body.\r\n")
	must(t, obj.Put(ctx, "browser.eml", bytes.NewReader(raw), int64(len(raw))))
	m := &models.Message{TenantID: f.tenant.ID, MailboxID: f.personal.ID, ZoneID: f.zone.ID, Sender: "client@recipient.test", Recipients: []string{f.personal.FullAddress}, Subject: "Browser welcome", RawObjectKey: "browser.eml", Size: int64(len(raw))}
	must(t, f.st.CreateMessage(ctx, m))
	smtp := newLocalSMTP(t)
	svc := outbound.NewService(config.Outbound{Enabled: true, Mode: "relay", RelayHost: "127.0.0.1", RelayPort: smtp.ln.Addr().(*net.TCPAddr).Port, RelayTLS: "none", PollInterval: 10 * time.Millisecond, RetryDelay: time.Millisecond, MaxRetries: 3}, f.st, zerolog.Nop())
	svc.SetObjectStore(obj)
	svc.StartWorker(ctx)
	defer svc.Stop()
	var refreshes atomic.Int64
	real := companyRouter(t, f, obj, svc)
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/auth/refresh" {
			refreshes.Add(1)
		}
		real.ServeHTTP(w, r)
	})
	srv := httptest.NewUnstartedServer(h)
	listener, err := net.Listen("tcp", "127.0.0.1:18080")
	must(t, err)
	srv.Listener = listener
	srv.Start()
	defer srv.Close()
	claims := authn.AccessClaims{UserID: f.other.ID, TenantID: f.tenant.ID, Role: f.other.Role, Email: f.other.Email, SessionVersion: f.other.SessionVersion, IssuedAt: time.Now().Add(-time.Hour).Unix(), Exp: time.Now().Add(-time.Minute).Unix()}
	b, err := json.Marshal(claims)
	must(t, err)
	payload := base64.RawURLEncoding.EncodeToString(b)
	mac := hmac.New(sha256.New, []byte(companyTestJWT))
	_, err = mac.Write([]byte(payload))
	must(t, err)
	expired := payload + "." + hex.EncodeToString(mac.Sum(nil))
	fixture, err := json.Marshal(map[string]string{"employee": f.employee.Email, "other": f.other.Email, "admin": f.admin.Email, "password": password, "expired_token": expired, "personal_mailbox": f.personal.ID.String()})
	must(t, err)
	script, err := filepath.Abs("../../../scripts/browser_company.cjs")
	must(t, err)
	cmd := exec.CommandContext(ctx, "node", script)
	for _, env := range os.Environ() {
		if !strings.HasPrefix(env, "LD_LIBRARY_PATH=") {
			cmd.Env = append(cmd.Env, env)
		}
	}
	cmd.Env = append(cmd.Env, "TABMAIL_BROWSER_FIXTURE="+string(fixture))
	output, err := cmd.CombinedOutput()
	t.Log(string(output))
	must(t, err)
	if refreshes.Load() != 1 {
		t.Fatalf("cross-tab refresh network requests=%d, want 1", refreshes.Load())
	}
	// A UI-created invitation may only provision an ordinary employee.
	invited, err := f.st.GetUserByEmail(ctx, "browser-new@contact.test")
	must(t, err)
	if invited == nil || invited.Role != models.RoleUser {
		t.Fatal("UI invitation did not provision an employee")
	}
	smtp.mu.Lock()
	n := len(smtp.messages["client@recipient.test"])
	smtp.mu.Unlock()
	if n != 1 {
		t.Fatalf("browser send count=%d", n)
	}
	cancel()
}
