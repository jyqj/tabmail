//go:build r5benchmark

package postgres

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"tabmail/internal/api"
	"tabmail/internal/api/middleware"
	"tabmail/internal/authn"
	"tabmail/internal/authz"
	"tabmail/internal/company"
	"tabmail/internal/config"
	"tabmail/internal/mailcontent"
	"tabmail/internal/models"
	"tabmail/internal/outbound"
	"tabmail/internal/policy"
	"tabmail/internal/rawobject"
	"tabmail/internal/store/fileobj"
	"tabmail/internal/testutil"
)

const r5BenchmarkSeed int64 = 3893945

var r5BenchmarkScales = map[string][3]int{"S": {100, 500, 100000}, "M": {1000, 5000, 1000000}, "L": {1000, 5000, 10000000}}

type r5SQLCounter struct {
	count   atomic.Int64
	mu      sync.Mutex
	capture bool
	sql     string
	args    []any
}

func (c *r5SQLCounter) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	c.count.Add(1)
	c.mu.Lock()
	if c.capture && c.sql == "" && strings.Contains(strings.ToUpper(strings.TrimSpace(data.SQL)), "SELECT") && strings.Contains(strings.ToLower(data.SQL), "messages") {
		c.sql = data.SQL
		c.args = append([]any(nil), data.Args...)
	}
	c.mu.Unlock()
	return ctx
}
func (c *r5SQLCounter) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

type r5BenchCompany struct {
	tenant           *models.Tenant
	zone             *models.DomainZone
	admin            *models.User
	actor            authz.Actor
	users            []*models.User
	personal, shared []*models.Mailbox
}
type r5BenchState struct {
	store      *PgStore
	pool       *pgxpool.Pool
	objects    *fileobj.FileStore
	counter    *r5SQLCounter
	companies  [2]r5BenchCompany
	messages   []*models.Message
	objectRoot string
	ctx        context.Context
}

func r5BenchID(kind string, n int) uuid.UUID {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte(fmt.Sprintf("r5:%d:%s:%d", r5BenchmarkSeed, kind, n)))
}
func r5BenchSizeBlock(rng *rand.Rand) []int {
	sizes := make([]int, 0, 1000)
	for _, bucket := range []struct{ size, count int }{{4096, 800}, {32768, 180}, {262144, 19}, {2097152, 1}} {
		for n := 0; n < bucket.count; n++ {
			sizes = append(sizes, bucket.size)
		}
	}
	rng.Shuffle(len(sizes), func(i, j int) { sizes[i], sizes[j] = sizes[j], sizes[i] })
	return sizes
}
func r5BenchLife(n int) string {
	switch n % 20 {
	case 16, 17:
		return "archived"
	case 18:
		return "trash_expired"
	case 19:
		return "hard_expired"
	default:
		return "inbox"
	}
}

func r5BenchCheck(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func r5BenchOwned(t *testing.T, ctx context.Context) *r5BenchState {
	t.Helper()
	dsn := os.Getenv("TABMAIL_TEST_DB_DSN")
	if dsn == "" {
		t.Fatal("benchmark requires explicit owned DSN")
	}
	admin, err := pgxpool.New(ctx, dsn)
	r5BenchCheck(t, err)
	name := "tm_benchmark_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	_, err = admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize())
	r5BenchCheck(t, err)
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), "DROP DATABASE "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)")
		admin.Close()
	})
	parsed, err := url.Parse(dsn)
	r5BenchCheck(t, err)
	parsed.Path = "/" + name
	cfg, err := pgxpool.ParseConfig(parsed.String())
	r5BenchCheck(t, err)
	cfg.MaxConns = 24
	counter := &r5SQLCounter{}
	cfg.ConnConfig.Tracer = counter
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	r5BenchCheck(t, err)
	t.Cleanup(pool.Close)
	r5BenchCheck(t, Migrate(ctx, cfg.ConnConfig))
	r5BenchCheck(t, pool.Ping(ctx))
	objectRoot := t.TempDir()
	objects, err := fileobj.New(objectRoot)
	r5BenchCheck(t, err)
	return &r5BenchState{store: &PgStore{pool: pool}, pool: pool, objects: objects, counter: counter, objectRoot: objectRoot, ctx: ctx}
}
func (s *r5BenchState) seed(t *testing.T, size [3]int) {
	t.Helper()
	ctx := s.ctx
	plan := &models.Plan{ID: r5BenchID("plan", 0), Name: "Owned benchmark unlimited plan", MaxDomains: 10, MaxMailboxesPerDomain: 100000, MaxMessagesPerMailbox: 20000000, MaxMessageBytes: 25 << 20, DailyQuota: 20000000, RPMLimit: 10000000}
	r5BenchCheck(t, s.store.CreatePlan(ctx, plan))
	globalUser := 0
	for ci, count := range []int{size[0] * 80 / 100, size[0] * 20 / 100} {
		c := r5BenchCompany{}
		domain := fmt.Sprintf("bench-%d.test", ci)
		c.tenant = &models.Tenant{ID: r5BenchID("tenant", ci), Name: domain, PlanID: plan.ID}
		r5BenchCheck(t, s.store.CreateTenant(ctx, c.tenant))
		c.admin = &models.User{ID: r5BenchID("admin", ci), TenantID: c.tenant.ID, Email: "admin@" + domain, Role: models.RoleAdmin, IsActive: true, PasswordHash: "synthetic-not-password"}
		r5BenchCheck(t, s.store.CreateUser(ctx, c.admin))
		c.actor = authz.Actor{Type: authz.PrincipalUser, ID: c.admin.ID, TenantID: c.tenant.ID, Role: models.RoleAdmin, IsAdmin: true}
		c.zone = &models.DomainZone{ID: r5BenchID("zone", ci), TenantID: c.tenant.ID, Domain: domain, IsVerified: true, MXVerified: true}
		r5BenchCheck(t, s.store.CreateZone(ctx, c.zone))
		_, err := s.store.ConfigureCompany(ctx, c.actor, company.Settings{Name: domain, PrimaryZoneID: c.zone.ID})
		r5BenchCheck(t, err)
		for n := 0; n < count; n++ {
			local := fmt.Sprintf("employee-%d", globalUser)
			email := local + "@contact.test"
			hash := company.Hash("benchmark-invite-" + local)
			_, err = s.store.InviteEmployee(ctx, c.actor, company.InvitationInput{Email: email, LocalPart: local, DisplayName: local}, hash)
			r5BenchCheck(t, err)
			r5BenchCheck(t, s.store.ActivateEmployee(ctx, hash, "synthetic-not-password"))
			user, e := s.store.GetUserByEmail(ctx, email)
			r5BenchCheck(t, e)
			if user == nil {
				t.Fatal("real employee activation missing")
			}
			c.users = append(c.users, user)
			box, e := s.store.GetMailboxByAddress(ctx, local+"@"+domain)
			r5BenchCheck(t, e)
			if box == nil {
				t.Fatal("real personal mailbox missing")
			}
			c.personal = append(c.personal, box)
			globalUser++
		}
		for n := 0; n < count*4; n++ {
			box, e := s.store.CreateWorkMailbox(ctx, c.actor, company.MailboxInput{LocalPart: fmt.Sprintf("shared-%d", n), Kind: "shared"})
			r5BenchCheck(t, e)
			c.shared = append(c.shared, box)
			for grant := 0; grant < 4; grant++ {
				u := c.users[(n+grant)%len(c.users)]
				current, e := s.store.GetWorkMailbox(ctx, c.actor, box.ID)
				r5BenchCheck(t, e)
				r5BenchCheck(t, s.store.SetWorkGrant(ctx, c.actor, models.MailboxGrant{TenantID: c.tenant.ID, MailboxID: box.ID, UserID: u.ID, CanRead: true, CanOrganize: grant == 1, CanSend: grant == 2}, current.Revision))
			}
		}
		s.companies[ci] = c
	}
	rng := rand.New(rand.NewSource(r5BenchmarkSeed))
	rawStore := rawobject.NewStore(s.objects, s.store)
	var sizeBlock []int
	for n := 0; n < size[2]; n++ {
		ci := 0
		if (n/20)%5 == 4 {
			ci = 1
		}
		c := s.companies[ci]
		slot := (n / 5) % (len(c.personal) + len(c.shared))
		var box *models.Mailbox
		if slot < len(c.personal) {
			box = c.personal[slot]
		} else {
			box = c.shared[slot-len(c.personal)]
		}
		if r5BenchLife(n) == "hard_expired" {
			box = c.shared[(n/20)%len(c.shared)]
		}
		if n%1000 == 0 {
			sizeBlock = r5BenchSizeBlock(rng)
		}
		target := sizeBlock[n%1000]
		header := fmt.Sprintf("From: source@synthetic.test\r\nTo: %s\r\nSubject: Benchmark %d\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nneedle-%d ", box.FullAddress, n, n)
		raw := append([]byte(header), bytes.Repeat([]byte("x"), max(0, target-len(header)))...)
		message := &models.Message{ID: r5BenchID("message", n), TenantID: c.tenant.ID, MailboxID: box.ID, ZoneID: c.zone.ID, Sender: "source@synthetic.test", Recipients: []string{box.FullAddress}, Subject: fmt.Sprintf("Benchmark %d", n), Size: int64(len(raw)), RawObjectKey: rawobject.Key(raw), HeadersJSON: json.RawMessage(`{}`)}
		created, e := rawStore.StoreMessage(ctx, message, raw, 20000000)
		r5BenchCheck(t, e)
		if !created {
			t.Fatal("original scale message not actually created")
		}
		// Controlled fixture lifecycle input, not duplicated business query logic.
		switch n % 20 {
		case 16, 17:
			_, e = s.pool.Exec(ctx, `UPDATE messages SET archived_at=clock_timestamp() WHERE id=$1`, message.ID)
		case 18:
			_, e = s.pool.Exec(ctx, `UPDATE messages SET deleted_at=clock_timestamp()-interval '2 hours',purge_after=clock_timestamp()-interval '1 hour' WHERE id=$1`, message.ID)
		case 19:
			if box.Kind == "shared" {
				_, e = s.pool.Exec(ctx, `UPDATE messages SET expires_at=clock_timestamp()-interval '1 hour' WHERE id=$1`, message.ID)
			}
		}
		r5BenchCheck(t, e)
		s.messages = append(s.messages, message)
	}
	parser := mailcontent.New(s.objects)
	for {
		jobs, e := s.store.ClaimMailIndexJobs(ctx, 100)
		r5BenchCheck(t, e)
		if len(jobs) == 0 {
			break
		}
		for _, job := range jobs {
			doc, e := parser.Document(ctx, job.MessageID, job.SourceKey)
			r5BenchCheck(t, e)
			r5BenchCheck(t, s.store.CompleteMailIndexJob(ctx, job, *doc))
		}
	}
}

type r5BenchMeasurement struct {
	Workload, CacheMode             string
	Samples                         int
	SQLCount                        int64
	P50, P95, P99                   float64
	AllocBytes, RSSBytes, DiskBytes uint64
}

func (m r5BenchMeasurement) wire() map[string]any {
	return map[string]any{"workload": m.Workload, "cache_mode": m.CacheMode, "samples": m.Samples, "sql_count": m.SQLCount, "p50_ms": m.P50, "p95_ms": m.P95, "p99_ms": m.P99, "alloc_bytes": m.AllocBytes, "rss_bytes": m.RSSBytes, "disk_bytes": m.DiskBytes}
}
func r5BenchDisk(root string) (uint64, error) {
	var size uint64
	err := filepath.Walk(root, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			size += uint64(info.Size())
		}
		return nil
	})
	return size, err
}
func r5BenchRSS() (uint64, error) {
	var usage syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &usage); err != nil {
		return 0, err
	}
	v := uint64(usage.Maxrss)
	if runtime.GOOS == "linux" {
		v *= 1024
	}
	if v == 0 {
		return 0, fmt.Errorf("process RSS observation unavailable")
	}
	return v, nil
}

func r5BenchHTTP(ctx context.Context, client *http.Client, url, token, method string, body any, key string, want int) error {
	raw, e := json.Marshal(body)
	if e != nil {
		return e
	}
	request, e := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(raw))
	if e != nil {
		return e
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	if key != "" {
		request.Header.Set("Idempotency-Key", key)
	}
	response, e := client.Do(request)
	if e != nil {
		return e
	}
	defer response.Body.Close()
	_, e = io.Copy(io.Discard, io.LimitReader(response.Body, 4<<20))
	if e != nil {
		return e
	}
	if response.StatusCode != want {
		return fmt.Errorf("shipping benchmark HTTP status=%d expected=%d; private response discarded", response.StatusCode, want)
	}
	return nil
}
func (s *r5BenchState) measure(t *testing.T, workload, cacheMode string, operation func(int) error) r5BenchMeasurement {
	t.Helper()
	var memBefore, memAfter runtime.MemStats
	runtime.ReadMemStats(&memBefore)
	beforeSQL := s.counter.count.Load()
	times := make([]float64, 200)
	var wg sync.WaitGroup
	errs := make(chan error, 200)
	for worker := 0; worker < 20; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for sample := worker; sample < 200; sample += 20 {
				start := time.Now()
				err := operation(sample)
				times[sample] = float64(time.Since(start)) / float64(time.Millisecond)
				if err != nil {
					errs <- err
				}
			}
		}(worker)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	runtime.ReadMemStats(&memAfter)
	sort.Float64s(times)
	sql := s.counter.count.Load() - beforeSQL
	if sql <= 0 {
		t.Fatal("SQL tracer observed no workload queries")
	}
	var pgBytes int64
	r5BenchCheck(t, s.pool.QueryRow(s.ctx, `SELECT pg_database_size(current_database())`).Scan(&pgBytes))
	objectBytes, err := r5BenchDisk(s.objectRoot)
	r5BenchCheck(t, err)
	rss, err := r5BenchRSS()
	r5BenchCheck(t, err)
	return r5BenchMeasurement{workload, cacheMode, len(times), sql, times[99], times[189], times[197], memAfter.TotalAlloc - memBefore.TotalAlloc, rss, objectBytes + uint64(pgBytes)}
}

func TestR5BenchmarkGeneratorContract(t *testing.T) {
	if r5BenchmarkScales["S"] != [3]int{100, 500, 100000} || r5BenchmarkScales["M"] != [3]int{1000, 5000, 1000000} || r5BenchmarkScales["L"][2] != 10000000 {
		t.Fatal("original-scale generator contract changed")
	}
	a, b := rand.New(rand.NewSource(r5BenchmarkSeed)), rand.New(rand.NewSource(r5BenchmarkSeed))
	for n := 0; n < 1000; n++ {
		if r5BenchSizeBlock(a)[0] != r5BenchSizeBlock(b)[0] {
			t.Fatal("seed determinism changed")
		}
	}
	// Pure validation only: not an S/M/L run, SQL calibration, or PG proof.
}

func r5BenchRoundTrip(ctx context.Context, client *http.Client, url, token, method string, body any, key string) (int, []byte, error) {
	raw, e := json.Marshal(body)
	if e != nil {
		return 0, nil, e
	}
	request, e := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(raw))
	if e != nil {
		return 0, nil, e
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	if key != "" {
		request.Header.Set("Idempotency-Key", key)
	}
	response, e := client.Do(request)
	if e != nil {
		return 0, nil, e
	}
	defer response.Body.Close()
	value, e := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	return response.StatusCode, value, e
}

var r5BenchResourceScope = map[string]string{
	"sql":    "application_and_worker_pool_not_server_trigger_sql",
	"memory": "go_alloc_and_process_high_water_rss_not_pg_redis_system",
	"disk":   "owned_raw_objects_plus_owned_pg_database_bytes",
	"gc":     "raw_reference_read_only_not_sweep_throughput",
	"cache":  "app_reset_and_reused_no_os_db_cold_claim",
}

func (s *r5BenchState) observations(t *testing.T, size [3]int) (map[string]any, string) {
	t.Helper()
	ctx := s.ctx
	count := func(query string) int { var n int; r5BenchCheck(t, s.pool.QueryRow(ctx, query).Scan(&n)); return n }
	distribution := map[string]any{
		"employees":          count(`SELECT count(*) FROM users WHERE role='user'`),
		"mailboxes":          count(`SELECT count(*) FROM mailboxes`),
		"messages":           count(`SELECT count(*) FROM messages`),
		"index_ready_count":  count(`SELECT count(*) FROM mail_documents`),
		"personal_mailboxes": count(`SELECT count(*) FROM mailboxes WHERE kind='personal'`),
		"shared_mailboxes":   count(`SELECT count(*) FROM mailboxes WHERE kind='shared'`),
		"shared_grant_rows":  count(`SELECT count(*) FROM mailbox_grants g JOIN mailboxes b ON b.id=g.mailbox_id WHERE b.kind='shared'`),
	}
	var lo, hi int
	r5BenchCheck(t, s.pool.QueryRow(ctx, `SELECT min(n),max(n) FROM (SELECT b.id,count(DISTINCT g.user_id)::int n FROM mailboxes b LEFT JOIN mailbox_grants g ON g.mailbox_id=b.id WHERE b.kind='shared' GROUP BY b.id) x`).Scan(&lo, &hi))
	distribution["min_distinct_shared_grantees"], distribution["max_distinct_shared_grantees"] = lo, hi
	life := map[string]int{"inbox": 0, "archived": 0, "trash_expired": 0, "hard_expired": 0}
	rows, e := s.pool.Query(ctx, `SELECT CASE WHEN deleted_at IS NOT NULL THEN 'trash_expired' WHEN expires_at IS NOT NULL AND expires_at<clock_timestamp() THEN 'hard_expired' WHEN archived_at IS NOT NULL THEN 'archived' ELSE 'inbox' END,count(*)::int FROM messages GROUP BY 1`)
	r5BenchCheck(t, e)
	for rows.Next() {
		var k string
		var n int
		r5BenchCheck(t, rows.Scan(&k, &n))
		life[k] = n
	}
	r5BenchCheck(t, rows.Err())
	rows.Close()
	distribution["lifecycle_counts"] = life
	sizes := map[string]int{"4096": 0, "32768": 0, "262144": 0, "2097152": 0}
	rows, e = s.pool.Query(ctx, `SELECT size::text,count(*)::int FROM messages GROUP BY size`)
	r5BenchCheck(t, e)
	for rows.Next() {
		var k string
		var n int
		r5BenchCheck(t, rows.Scan(&k, &n))
		sizes[k] = n
	}
	r5BenchCheck(t, rows.Err())
	rows.Close()
	distribution["mime_size_counts"] = sizes
	tenants := map[string]int{}
	rows, e = s.pool.Query(ctx, `SELECT t.name,count(*)::int FROM messages m JOIN tenants t ON t.id=m.tenant_id GROUP BY t.name`)
	r5BenchCheck(t, e)
	for rows.Next() {
		var k string
		var n int
		r5BenchCheck(t, rows.Scan(&k, &n))
		tenants[k] = n
	}
	r5BenchCheck(t, rows.Err())
	rows.Close()
	distribution["tenant_message_counts"] = tenants
	expected := map[string]any{"employees": size[0], "mailboxes": size[1], "messages": size[2], "index_ready_count": size[2], "personal_mailboxes": size[0], "shared_mailboxes": size[0] * 4, "shared_grant_rows": size[0] * 16, "min_distinct_shared_grantees": 4, "max_distinct_shared_grantees": 4,
		"lifecycle_counts": map[string]int{"inbox": size[2] * 80 / 100, "archived": size[2] * 10 / 100, "trash_expired": size[2] * 5 / 100, "hard_expired": size[2] * 5 / 100},
		"mime_size_counts": map[string]int{"4096": size[2] * 800 / 1000, "32768": size[2] * 180 / 1000, "262144": size[2] * 19 / 1000, "2097152": size[2] / 1000}, "tenant_message_counts": map[string]int{"bench-0.test": size[2] * 80 / 100, "bench-1.test": size[2] * 20 / 100}}
	a, _ := json.Marshal(distribution)
	b, _ := json.Marshal(expected)
	if !bytes.Equal(a, b) {
		t.Fatalf("actual dataset contract mismatch: observed=%s expected=%s", a, b)
	}
	// Stream actual logical identities/ACL/raw source keys/sizes/lifecycle. Random
	// row IDs and wall-clock timestamps are not parameter hashes or dataset bytes.
	h := sha256.New()
	streams := []string{
		`SELECT jsonb_build_array(t.name,b.full_address,m.subject,m.size,m.raw_object_key,m.archived_at IS NOT NULL,m.deleted_at IS NOT NULL,m.expires_at IS NOT NULL AND m.expires_at<clock_timestamp())::text FROM messages m JOIN mailboxes b ON b.id=m.mailbox_id JOIN tenants t ON t.id=m.tenant_id ORDER BY m.id`,
		`SELECT jsonb_build_array(b.full_address,u.email,g.can_read,g.can_organize,g.can_send)::text FROM mailbox_grants g JOIN mailboxes b ON b.id=g.mailbox_id JOIN users u ON u.id=g.user_id ORDER BY b.full_address,u.email`,
		`SELECT jsonb_build_array(t.name,u.email,u.role,u.is_active)::text FROM users u JOIN tenants t ON t.id=u.tenant_id ORDER BY t.name,u.email`,
		`SELECT jsonb_build_array(t.name,b.full_address,b.kind,u.email)::text FROM mailboxes b JOIN tenants t ON t.id=b.tenant_id LEFT JOIN users u ON u.id=b.owner_user_id ORDER BY b.full_address`,
	}
	for i, q := range streams {
		fmt.Fprintf(h, "stream:%d\n", i)
		r, e := s.pool.Query(ctx, q)
		r5BenchCheck(t, e)
		for r.Next() {
			var value string
			r5BenchCheck(t, r.Scan(&value))
			_, e = h.Write([]byte(value + "\n"))
			r5BenchCheck(t, e)
		}
		r5BenchCheck(t, r.Err())
		r.Close()
	}
	return distribution, hex.EncodeToString(h.Sum(nil))
}

func (s *r5BenchState) capturedPlan(t *testing.T, workload, scale, source string, schema int64, indexed int, operation func(int) error) map[string]any {
	t.Helper()
	s.counter.mu.Lock()
	s.counter.sql = ""
	s.counter.args = nil
	s.counter.capture = true
	s.counter.mu.Unlock()
	r5BenchCheck(t, operation(0))
	s.counter.mu.Lock()
	q, args := s.counter.sql, append([]any(nil), s.counter.args...)
	s.counter.capture = false
	s.counter.mu.Unlock()
	if q == "" {
		t.Fatal("shipping request produced no capturable message query")
	}
	var raw json.RawMessage
	r5BenchCheck(t, s.pool.QueryRow(s.ctx, "EXPLAIN (FORMAT JSON) "+q, args...).Scan(&raw))
	var plan []map[string]any
	r5BenchCheck(t, json.Unmarshal(raw, &plan))
	if len(plan) == 0 {
		t.Fatal("actual EXPLAIN plan missing")
	}
	qb := sha256.Sum256([]byte(q))
	ab, e := json.Marshal(args)
	r5BenchCheck(t, e)
	ah := sha256.Sum256(ab)
	return map[string]any{"origin": "captured_shipping_query_same_parameters", "workload": workload, "scale": scale, "source_sha": source, "schema_version": schema, "index_ready_count": indexed, "query_sha256": hex.EncodeToString(qb[:]), "parameters_sha256": hex.EncodeToString(ah[:]), "plan": plan}
}

func r5BenchParameterFingerprint(size [3]int) string {
	b, _ := json.Marshal(map[string]any{"seed": r5BenchmarkSeed, "population": size})
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func TestR5BenchmarkOriginalScale(t *testing.T) {
	scale := os.Getenv("TABMAIL_R5_BENCHMARK_SCALE")
	size, ok := r5BenchmarkScales[scale]
	if !ok || scale == "L" {
		t.Fatal("explicit approved S/M original-scale execution required; L not requested")
	}
	output, source := os.Getenv("TABMAIL_R5_BENCHMARK_OUTPUT"), os.Getenv("TABMAIL_R5_BENCHMARK_SOURCE_SHA")
	if output == "" || len(source) != 40 {
		t.Fatal("fresh private output and source identity required")
	}
	budget := 30 * time.Minute
	if scale == "M" {
		budget = 3 * time.Hour
	}
	ctx, cancel := context.WithTimeout(context.Background(), budget)
	defer cancel()
	state := r5BenchOwned(t, ctx)
	state.seed(t, size)
	distribution, actualFingerprint := state.observations(t, size)
	var indexed, employees, mailboxes, messages int
	var schema int64
	r5BenchCheck(t, state.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM users WHERE role='user'),(SELECT count(*) FROM mailboxes),(SELECT count(*) FROM messages),(SELECT count(*) FROM mail_documents),(SELECT max(version_id) FROM goose_db_version WHERE is_applied)`).Scan(&employees, &mailboxes, &messages, &indexed, &schema))
	if [3]int{employees, mailboxes, messages} != size || indexed != size[2] {
		t.Fatal("actual original scale/index-ready population mismatch")
	}
	state.counter.count.Store(0)
	for n := 0; n < 20; n++ {
		var value int
		r5BenchCheck(t, state.pool.QueryRow(ctx, `SELECT 1`).Scan(&value))
	}
	calibration := state.counter.count.Load()
	if calibration != 20 {
		t.Fatal("actual SQL tracer calibration mismatch")
	}
	c := state.companies[0]
	reader := c.users[0]
	personal, shared := c.personal[0], c.shared[0]
	var personalMessage, sharedMessage, foreignMessage *models.Message
	for _, message := range state.messages {
		if message.MailboxID == personal.ID && personalMessage == nil {
			personalMessage = message
		}
		if message.MailboxID == shared.ID && sharedMessage == nil {
			sharedMessage = message
		}
		if message.TenantID == state.companies[1].tenant.ID && foreignMessage == nil {
			foreignMessage = message
		}
	}
	if personalMessage == nil || sharedMessage == nil || foreignMessage == nil {
		t.Fatal("actual ACL/source safety sample missing")
	}
	measurements := []map[string]any{}
	var sqlPlans []map[string]any
	security := []string{}
	for _, workload := range []string{"inbox_list", "indexed_search", "indexed_message", "permission_mailboxes", "draft_save", "submit_and_loopback_worker", "gc_reference_safety"} {
		secret := uuid.NewString()
		token, e := authn.IssueAccessToken(secret, reader)
		r5BenchCheck(t, e)
		mr := miniredis.RunT(t)
		rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
		relay, e := testutil.StartR5SMTP(ctx, testutil.R5SMTPBehavior{})
		r5BenchCheck(t, e)
		cfg := relay.Config()
		cfg.MaxRetries = 3
		cfg.BatchSize = 20
		cfg.PollInterval = 20 * time.Millisecond
		cfg.RetryDelay = time.Hour
		svc := outbound.NewService(cfg, state.store, state.store, zerolog.Nop())
		svc.SetObjectStore(state.objects)
		router := api.NewRouter(api.RouterConfig{Store: state.store, CompanyRepository: state.store, ObjectStore: state.objects, RawObjects: rawobject.NewStore(state.objects, state.store), JWTSecret: secret, MailboxTokenSecret: uuid.NewString(), PublicTenantID: "00000000-0000-0000-0000-000000000001", NamingMode: policy.NamingFull, CompanyOnly: true, HTTP: config.HTTP{}, RateLimiter: middleware.NewRateLimiter(rdb, state.store, 10000000, nil), OutboundService: svc, Logger: zerolog.Nop(), Readiness: state.store.Readiness})
		server := httptest.NewServer(router)
		client := server.Client()
		// Actual safe semantics; do not replace a denied path with an authorized proxy.
		foreignPath := server.URL + "/api/v1/company/mailboxes/" + foreignMessage.MailboxID.String() + "/messages/" + foreignMessage.ID.String() + "/source"
		r5BenchCheck(t, r5BenchHTTP(ctx, client, foreignPath, token, "GET", nil, "", 404))
		current, e := state.store.GetWorkMailbox(ctx, c.actor, shared.ID)
		r5BenchCheck(t, e)
		r5BenchCheck(t, state.store.SetWorkGrant(ctx, c.actor, models.MailboxGrant{TenantID: c.tenant.ID, MailboxID: shared.ID, UserID: reader.ID}, current.Revision))
		revokedPath := server.URL + "/api/v1/company/mailboxes/" + shared.ID.String() + "/messages/" + sharedMessage.ID.String() + "/source"
		r5BenchCheck(t, r5BenchHTTP(ctx, client, revokedPath, token, "GET", nil, "", 404))
		current, e = state.store.GetWorkMailbox(ctx, c.actor, shared.ID)
		r5BenchCheck(t, e)
		r5BenchCheck(t, state.store.SetWorkGrant(ctx, c.actor, models.MailboxGrant{TenantID: c.tenant.ID, MailboxID: shared.ID, UserID: reader.ID, CanRead: true}, current.Revision))
		security = append(security, workload+":foreign_and_revoked_current_source_denied")
		if workload == "submit_and_loopback_worker" {
			svc.StartWorker(ctx)
		}
		for _, cache := range []string{"app_cold_db_os_unspecified", "app_hot_db_os_unspecified"} {
			if cache == "app_cold_db_os_unspecified" {
				mr.FlushAll()
			}
			operation := func(sample int) error {
				switch workload {
				case "inbox_list":
					return r5BenchHTTP(ctx, client, server.URL+"/api/v1/company/mailboxes/"+personal.ID.String()+"/messages?folder=inbox", token, "GET", nil, "", 200)
				case "indexed_search":
					return r5BenchHTTP(ctx, client, server.URL+"/api/v1/company/mailboxes/"+shared.ID.String()+"/messages?folder=inbox&q=needle", token, "GET", nil, "", 200)
				case "indexed_message":
					return r5BenchHTTP(ctx, client, server.URL+"/api/v1/company/mailboxes/"+personal.ID.String()+"/messages/"+personalMessage.ID.String(), token, "GET", nil, "", 200)
				case "permission_mailboxes":
					return r5BenchHTTP(ctx, client, server.URL+"/api/v1/company/mailboxes", token, "GET", nil, "", 200)
				case "gc_reference_safety":
					n, e := state.store.CountRawObjectReferences(ctx, personalMessage.RawObjectKey)
					if e != nil {
						return e
					}
					if n < 1 {
						return fmt.Errorf("actual protected raw reference disappeared")
					}
					return nil
				case "draft_save", "submit_and_loopback_worker":
					draft := company.Draft{MailboxID: personal.ID, Payload: company.DraftPayload{To: []string{"loopback@synthetic.test"}, Subject: "Benchmark operation", TextBody: "Synthetic benchmark operation"}}
					status, raw, e := r5BenchRoundTrip(ctx, client, server.URL+"/api/v1/company/drafts", token, "POST", draft, "")
					if e != nil {
						return e
					}
					if status != 200 {
						return fmt.Errorf("real draft creation status=%d", status)
					}
					var response struct{ Data company.Draft }
					if e = json.Unmarshal(raw, &response); e != nil {
						return e
					}
					if workload == "draft_save" {
						return r5BenchHTTP(ctx, client, server.URL+"/api/v1/company/drafts/"+response.Data.ID.String()+fmt.Sprintf("?revision=%d", response.Data.Revision), token, "DELETE", nil, "", 200)
					}
					key := fmt.Sprintf("benchmark-%s-%d", cache, sample)
					status, raw, e = r5BenchRoundTrip(ctx, client, server.URL+"/api/v1/company/drafts/"+response.Data.ID.String()+"/submit", token, "POST", map[string]int{"expected_revision": response.Data.Revision}, key)
					if e != nil {
						return e
					}
					if status != 201 {
						return fmt.Errorf("actual enqueue status=%d", status)
					}
					var result struct{ Data models.OutboundJob }
					if e = json.Unmarshal(raw, &result); e != nil {
						return e
					}
					ticker := time.NewTicker(10 * time.Millisecond)
					defer ticker.Stop()
					for {
						job, e := state.store.GetOutboundJob(ctx, result.Data.ID)
						if e != nil {
							return e
						}
						if job != nil && job.State == models.OutboundSent {
							return nil
						}
						select {
						case <-ctx.Done():
							return ctx.Err()
						case <-ticker.C:
						}
					}
				}
				return fmt.Errorf("unknown workload")
			}
			if workload == "inbox_list" && len(sqlPlans) == 0 {
				sqlPlans = append(sqlPlans, state.capturedPlan(t, workload, scale, source, schema, indexed, operation))
			}
			measurements = append(measurements, state.measure(t, workload, cache, operation).wire())
		}
		svc.Stop()
		server.Close()
		_ = relay.Close()
		_ = rdb.Close()
		mr.Close()
	}
	datasetPath := filepath.Join("..", "..", "..", "docs", "company-mail", "evidence", "R5-BENCHMARK-DATASETS.json")
	contract, e := os.ReadFile(datasetPath)
	r5BenchCheck(t, e)
	hash := sha256.Sum256(contract)
	result := map[string]any{"scale": scale, "employees": employees, "mailboxes": mailboxes, "messages": messages, "seed": r5BenchmarkSeed, "concurrency": 20, "pool_size": 24, "schema_version": schema, "source_sha": source, "dataset_sha256": hex.EncodeToString(hash[:]), "dataset_fingerprint": actualFingerprint, "parameter_fingerprint": r5BenchParameterFingerprint(size), "actual_distribution": distribution, "sql_plans": sqlPlans, "resource_scope": r5BenchResourceScope, "system_memory_observation": "not_measured", "index_ready_count": indexed, "sql_tracer_calibration_count": calibration, "sql_tracer_calibration_expected": 20, "sql_count_scope": "actual application and worker pool queries, includes transaction/control queries; no SQL text or params recorded", "hardware": map[string]any{"os": runtime.GOOS, "arch": runtime.GOARCH, "cpu": runtime.NumCPU()}, "safety_assertions": security, "safety_assertions_passed": true, "measurements": measurements, "cache_boundary": "app lifecycle/Redis fresh versus reused; DB/OS cache is explicitly unspecified", "task_complete": false, "product_green": false}
	b, e := json.MarshalIndent(result, "", "  ")
	r5BenchCheck(t, e)
	file, e := os.OpenFile(output, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	r5BenchCheck(t, e)
	_, e = file.Write(append(b, '\n'))
	r5BenchCheck(t, e)
	r5BenchCheck(t, file.Close())
}

// This tiny real-PG measurement validates the instrument only. It creates no
// benchmark population and cannot be recorded as original-scale S/M evidence.
func TestR5BenchmarkSQLTracerCalibration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	state := r5BenchOwned(t, ctx)
	state.counter.count.Store(0)
	for n := 0; n < 20; n++ {
		var value int
		r5BenchCheck(t, state.pool.QueryRow(ctx, `SELECT 1`).Scan(&value))
		if value != 1 {
			t.Fatal("actual PostgreSQL calibration value differs")
		}
	}
	if state.counter.count.Load() != 20 {
		t.Fatal("actual tracer count differs from 20 queries")
	}
	t.Log("TOOL_ONLY_SQL_CALIBRATION actual_queries=20 no_S_M_L_population_created")
}

// This real small dataset verifies generator, observed bytes/ACL/indexes and
// shipping plan capture. It is not an original-scale S/M/L performance run.
func TestR5BenchmarkToolOnlyDatasetCalibration(t *testing.T) {
	output, source := os.Getenv("TABMAIL_R5_BENCHMARK_OUTPUT"), os.Getenv("TABMAIL_R5_BENCHMARK_SOURCE_SHA")
	if output == "" || len(source) != 40 {
		t.Fatal("fresh private output and immutable source identity required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	state := r5BenchOwned(t, ctx)
	size := [3]int{20, 100, 1000}
	state.seed(t, size)
	distribution, fingerprint := state.observations(t, size)
	var schema int64
	r5BenchCheck(t, state.pool.QueryRow(ctx, `SELECT max(version_id) FROM goose_db_version WHERE is_applied`).Scan(&schema))
	c := state.companies[0]
	reader := c.users[0]
	personal := c.personal[0]
	secret := uuid.NewString()
	token, e := authn.IssueAccessToken(secret, reader)
	r5BenchCheck(t, e)
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()
	router := api.NewRouter(api.RouterConfig{Store: state.store, CompanyRepository: state.store, ObjectStore: state.objects, RawObjects: rawobject.NewStore(state.objects, state.store), JWTSecret: secret, MailboxTokenSecret: uuid.NewString(), PublicTenantID: "00000000-0000-0000-0000-000000000001", NamingMode: policy.NamingFull, CompanyOnly: true, HTTP: config.HTTP{}, RateLimiter: middleware.NewRateLimiter(rdb, state.store, 10000000, nil), Logger: zerolog.Nop(), Readiness: state.store.Readiness})
	server := httptest.NewServer(router)
	defer server.Close()
	operation := func(int) error {
		return r5BenchHTTP(ctx, server.Client(), server.URL+"/api/v1/company/mailboxes/"+personal.ID.String()+"/messages?folder=inbox", token, "GET", nil, "", 200)
	}
	plan := state.capturedPlan(t, "inbox_list", "tool_only", source, schema, 1000, operation)
	objectBytes, e := r5BenchDisk(state.objectRoot)
	r5BenchCheck(t, e)
	rss, e := r5BenchRSS()
	r5BenchCheck(t, e)
	var pgBytes int64
	r5BenchCheck(t, state.pool.QueryRow(ctx, `SELECT pg_database_size(current_database())`).Scan(&pgBytes))
	contract, e := os.ReadFile(filepath.Join("..", "..", "..", "docs", "company-mail", "evidence", "R5-BENCHMARK-DATASETS.json"))
	r5BenchCheck(t, e)
	hash := sha256.Sum256(contract)
	result := map[string]any{"mode": "TOOL_ONLY_DATASET_CALIBRATION", "scale": "tool_only", "S_M_L_executed": false, "employees": 20, "mailboxes": 100, "messages": 1000, "seed": r5BenchmarkSeed, "source_sha": source, "schema_version": schema, "dataset_sha256": hex.EncodeToString(hash[:]), "actual_distribution": distribution, "dataset_fingerprint": fingerprint, "parameter_fingerprint": r5BenchParameterFingerprint(size), "sql_plans": []map[string]any{plan}, "resource_scope": r5BenchResourceScope, "system_memory_observation": "not_measured", "rss_bytes": rss, "disk_bytes": objectBytes + uint64(pgBytes), "task_complete": false, "product_green": false}
	b, e := json.MarshalIndent(result, "", "  ")
	r5BenchCheck(t, e)
	f, e := os.OpenFile(output, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	r5BenchCheck(t, e)
	_, e = f.Write(append(b, '\n'))
	r5BenchCheck(t, e)
	r5BenchCheck(t, f.Close())
	t.Log("TOOL_ONLY_DATASET_CALIBRATION actual20/100/1000 distributions/streamSHA/index/ACL/shippingplan observed; noS_M_Lbaseline")
}
