//go:build r5benchmark

package postgres

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"testing/synctest"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"tabmail/internal/api"
	"tabmail/internal/api/middleware"
	"tabmail/internal/app"
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
const r5BenchLatencyClock = "wall_elapsed_cross_checked_monotonic_gap_le_100ms"
const r5BenchParserJoinReserve = 31 * time.Second
const r5BenchParserJoinPolicy = "uncancelled_document_wait_with_31s_absolute_wall_admission_guard"

var r5BenchmarkScales = map[string][3]int{"S": {100, 500, 100000}, "M": {1000, 5000, 1000000}, "L": {1000, 5000, 10000000}}

var r5MessageQuery = regexp.MustCompile(`(?i)\b(?:FROM|JOIN)\s+messages\b`)

type r5SQLCounter struct {
	count     atomic.Int64
	mu        sync.Mutex
	capture   bool
	sql       string
	args      []any
	claimSQL  string
	claimArgs []any
}

func (c *r5SQLCounter) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	c.count.Add(1)
	c.mu.Lock()
	if c.capture && c.sql == "" && strings.Contains(strings.ToUpper(strings.TrimSpace(data.SQL)), "SELECT") && r5MessageQuery.MatchString(data.SQL) {
		c.sql = data.SQL
		c.args = append([]any(nil), data.Args...)
	}
	if strings.HasPrefix(strings.TrimSpace(data.SQL), "WITH picked AS(SELECT message_id FROM mail_index_jobs ") && strings.Contains(data.SQL, "lease_token=gen_random_uuid()") {
		c.claimSQL, c.claimArgs = data.SQL, append([]any(nil), data.Args...)
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
	store                  *PgStore
	pool                   *pgxpool.Pool
	objects                *fileobj.FileStore
	counter                *r5SQLCounter
	companies              [2]r5BenchCompany
	messages               []*models.Message
	objectRoot             string
	ctx                    context.Context
	preparation            r5BenchPreparation
	preparationCheckpoints []map[string]any
	fingerprintStreams     []map[string]any
	diagnostic             *r5StoreDiagnostic
	preparationMethod      string
	inputScheduler         *r5Lookahead
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
	return r5BenchOwnedDiagnostic(t, ctx, nil)
}

func r5BenchOwnedDiagnostic(t *testing.T, ctx context.Context, diagnostic *r5StoreDiagnostic) *r5BenchState {
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
		if diagnostic == nil {
			_, _ = admin.Exec(context.Background(), "DROP DATABASE "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)")
		} else {
			cleanupCtx, cleanupCancel := context.WithDeadline(context.Background(), time.Unix(0, diagnostic.totalStarted.UnixNano()+int64(r5StoreDiagnosticTotalBudget)))
			defer cleanupCancel()
			if _, err := admin.Exec(cleanupCtx, "DROP DATABASE IF EXISTS "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)"); err != nil {
				t.Errorf("diagnostic final bounded database cleanup: %v", err)
			}
		}
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
	if diagnostic != nil {
		diagnostic.counter = counter
		cfg.ConnConfig.Tracer = diagnostic
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	r5BenchCheck(t, err)
	t.Cleanup(pool.Close)
	r5BenchCheck(t, Migrate(ctx, cfg.ConnConfig))
	r5BenchCheck(t, pool.Ping(ctx))
	objectRoot := t.TempDir()
	objects, err := fileobj.New(objectRoot)
	r5BenchCheck(t, err)
	state := &r5BenchState{store: &PgStore{pool: pool}, pool: pool, objects: objects, counter: counter, objectRoot: objectRoot, ctx: ctx, diagnostic: diagnostic}
	if diagnostic != nil {
		diagnostic.admin, diagnostic.state, diagnostic.database = admin, state, name
	}
	return state
}

// Fixture-only bounded scheduler. The single producer owns RNG and ordinal
// assignment; errors cancel and join all workers before returning to testing.T.
func r5BenchPrepare(ctx context.Context, produce func(context.Context, func(func(context.Context) error) bool) error) error {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	jobs := make(chan func(context.Context) error, 20)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var failures []error
	fail := func(err error) {
		if err == nil {
			return
		}
		mu.Lock()
		failures = append(failures, err)
		mu.Unlock()
		cancel()
	}
	for worker := 0; worker < 20; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-runCtx.Done():
					return
				case job, ok := <-jobs:
					if !ok {
						return
					}
					if err := job(runCtx); err != nil {
						fail(err)
						return
					}
				}
			}
		}()
	}
	enqueue := func(job func(context.Context) error) bool {
		select {
		case <-runCtx.Done():
			return false
		case jobs <- job:
			return true
		}
	}
	err := produce(runCtx, enqueue)
	if err != nil {
		fail(err)
	}
	close(jobs)
	wg.Wait()
	mu.Lock()
	defer mu.Unlock()
	if len(failures) != 0 {
		return errors.Join(failures...)
	}
	return runCtx.Err()
}

// This is fixture scheduling, not a replacement index worker or claim SQL. A
// window is fully stored before it is claimed, and every leased batch is joined
// before claiming again. Thus ready rows accumulate, but unindexed input and
// total active writer/parser tasks are each bounded by 20. No leased prefetch.
type r5BenchPreparation struct {
	Method                             string  `json:"method"`
	Phase                              string  `json:"phase"`
	JoinedInflight                     int     `json:"joined_inflight"`
	ClaimQuerySHA256                   string  `json:"claim_query_sha256"`
	ClaimParametersSHA256              string  `json:"claim_parameters_sha256"`
	ParserJoinPolicy                   string  `json:"parser_join_policy"`
	ParserJoinReserveSeconds           int     `json:"parser_join_reserve_seconds"`
	ParserJoinedInflight               int     `json:"parser_joined_inflight"`
	ParserJoinedCalls                  int     `json:"parser_joined_calls"`
	ParserCanceledJoinedCalls          int     `json:"parser_canceled_joined_calls"`
	ParserJoinedCallWallSecondsSum     float64 `json:"parser_joined_call_wall_seconds_sum"`
	ParserMinStartRemainingWallSeconds float64 `json:"parser_min_start_remaining_wall_seconds"`
	ParserOriginalDeadlineUnixSeconds  float64 `json:"parser_original_deadline_unix_seconds"`
	ParserQuiescent                    bool    `json:"parser_quiescent"`
	ActiveBudget                       int     `json:"active_budget"`
	PendingUpperBound                  int     `json:"pending_upper_bound"`
	MaxActive                          int     `json:"max_active"`
	MaxWindowMessages                  int     `json:"max_window_messages"`
	Written                            int     `json:"joined_stored_messages"`
	Completed                          int     `json:"joined_completed_jobs"`
	ClaimCalls                         int     `json:"claim_calls"`
	MaxClaimBatch                      int     `json:"max_claim_batch"`
	LastClaimBatch                     int     `json:"last_claim_batch"`
	LastClaimReadyHeapDepth            int     `json:"joined_ready_before_last_claim"`
	LastClaimWallSeconds               float64 `json:"last_claim_wall_seconds"`
	ProducerDone                       bool    `json:"producer_done"`
	FinalEmptyClaim                    bool    `json:"final_empty_claim"`
	SQLReady                           int     `json:"sql_ready_count"`
	CompanyWallSeconds                 float64 `json:"company_wall_seconds"`
	StoreWallSeconds                   float64 `json:"store_wall_seconds"`
	ClaimWallSeconds                   float64 `json:"claim_wall_seconds"`
	ParseCompleteWallSeconds           float64 `json:"parse_complete_wall_seconds"`
	WallSeconds                        float64 `json:"pipeline_wall_seconds"`
	MaxWallMonotonicGapMS              float64 `json:"max_wall_monotonic_gap_ms"`
}

type r5BenchParserJoins struct {
	active, joined, canceled         atomic.Int64
	wallNanos                        atomic.Int64
	minRemainingNanos, deadlineNanos atomic.Int64
}

func (j *r5BenchParserJoins) observe(stats *r5BenchPreparation) {
	stats.ParserJoinPolicy, stats.ParserJoinReserveSeconds = r5BenchParserJoinPolicy, int(r5BenchParserJoinReserve/time.Second)
	stats.ParserJoinedInflight, stats.ParserJoinedCalls, stats.ParserCanceledJoinedCalls = int(j.active.Load()), int(j.joined.Load()), int(j.canceled.Load())
	stats.ParserJoinedCallWallSecondsSum = float64(j.wallNanos.Load()) / float64(time.Second)
	stats.ParserMinStartRemainingWallSeconds = float64(j.minRemainingNanos.Load()) / float64(time.Second)
	stats.ParserOriginalDeadlineUnixSeconds = float64(j.deadlineNanos.Load()) / float64(time.Second)
	stats.ParserQuiescent = stats.ParserJoinedInflight == 0
}

// The fixture owns every waiter on this shared Parser. Keep each waiter alive
// until the production singleflight finishes: a canceled Document waiter alone
// does not join Parser.load's WithoutCancel background flight. Its unchanged
// 30s timeout (including gate4 waiting) must fit inside the ORIGINAL wall budget.
// ObjectReader.Get must honor context and Close must unblock Read, as required
// by the production Parser. Host-suspend overruns remain failures, not an extra
// drain allowance. A canceled/late waiter never proceeds to Complete.
func r5BenchDocumentJoined(ctx context.Context, parser *mailcontent.Parser, joins *r5BenchParserJoins, id uuid.UUID, key string) (*company.ParsedMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	deadline, ok := ctx.Deadline()
	remaining := deadline.UnixNano() - time.Now().UnixNano()
	if !ok || remaining <= int64(r5BenchParserJoinReserve) {
		return nil, fmt.Errorf("fixture parser admission requires original absolute wall remaining>31s: %w", context.DeadlineExceeded)
	}
	if !joins.deadlineNanos.CompareAndSwap(0, deadline.UnixNano()) && joins.deadlineNanos.Load() != deadline.UnixNano() {
		return nil, errors.New("fixture parser changed its original absolute deadline")
	}
	for old := joins.minRemainingNanos.Load(); old == 0 || remaining < old; old = joins.minRemainingNanos.Load() {
		if joins.minRemainingNanos.CompareAndSwap(old, remaining) {
			break
		}
	}
	start := time.Now()
	joins.active.Add(1)
	defer func() {
		joins.wallNanos.Add(time.Now().UnixNano() - start.UnixNano())
		joins.joined.Add(1)
		if ctx.Err() != nil {
			joins.canceled.Add(1)
		}
		joins.active.Add(-1)
	}()
	doc, err := parser.Document(context.WithoutCancel(ctx), id, key)
	err = errors.Join(err, ctx.Err())
	if time.Now().UnixNano() >= deadline.UnixNano() {
		err = errors.Join(err, context.DeadlineExceeded)
	}
	if err != nil {
		return nil, err
	}
	return doc, nil
}

func r5BenchPipeline(ctx context.Context, expected int,
	produce func(context.Context, func(func(context.Context) error) bool) error,
	claim func(context.Context, int) ([]company.MailIndexJob, error),
	complete func(context.Context, company.MailIndexJob) error,
	ready func(context.Context) (int, error),
	progress func(r5BenchPreparation) error,
) (stats r5BenchPreparation, err error) {
	stats.Method, stats.ActiveBudget, stats.PendingUpperBound = "bounded_store_claim_complete_window_v1", 20, 20
	started := time.Now()
	var active, maxActive atomic.Int32
	defer func() {
		stats.MaxActive = int(maxActive.Load())
		stats.JoinedInflight = int(active.Load())
		stats.WallSeconds = float64(time.Now().UnixNano()-started.UnixNano()) / float64(time.Second)
	}()
	if expected <= 0 {
		return stats, fmt.Errorf("pipeline requires positive expected population")
	}
	track := func(job func(context.Context) error) func(context.Context) error {
		return func(ctx context.Context) error {
			n := active.Add(1)
			defer active.Add(-1)
			for old := maxActive.Load(); n > old; old = maxActive.Load() {
				if maxActive.CompareAndSwap(old, n) {
					break
				}
			}
			return job(ctx)
		}
	}
	timed := func(start time.Time, seconds *float64, operationErr error) error {
		end := time.Now()
		elapsed, gap, clockErr := r5BenchLatency(start, end, end.Sub(start))
		*seconds += elapsed / 1000
		stats.MaxWallMonotonicGapMS = max(stats.MaxWallMonotonicGapMS, gap)
		return errors.Join(operationErr, clockErr)
	}
	claimBatch := func() ([]company.MailIndexJob, error) {
		start := time.Now()
		jobs, e := claim(ctx, 20) // Legal production limit, never requested100/clamp5.
		stats.ClaimCalls++
		stats.MaxClaimBatch = max(stats.MaxClaimBatch, len(jobs))
		before := stats.ClaimWallSeconds
		e = timed(start, &stats.ClaimWallSeconds, e)
		stats.LastClaimWallSeconds = stats.ClaimWallSeconds - before
		stats.LastClaimBatch, stats.LastClaimReadyHeapDepth = len(jobs), stats.Completed
		return jobs, e
	}
	report := func(phase string) error {
		stats.Phase, stats.MaxActive = phase, int(maxActive.Load())
		stats.JoinedInflight = int(active.Load())
		stats.WallSeconds = float64(time.Now().UnixNano()-started.UnixNano()) / float64(time.Second)
		if progress != nil {
			return progress(stats)
		}
		return nil
	}
	window := make([]func(context.Context) error, 0, 20)
	flush := func() error {
		if len(window) == 0 {
			return nil
		}
		stats.MaxWindowMessages = max(stats.MaxWindowMessages, len(window))
		start := time.Now()
		e := r5BenchPrepare(ctx, func(runCtx context.Context, enqueue func(func(context.Context) error) bool) error {
			for _, job := range window {
				if !enqueue(track(job)) {
					return runCtx.Err()
				}
			}
			return nil
		})
		if e = timed(start, &stats.StoreWallSeconds, e); e != nil {
			return e
		}
		stats.Written += len(window)
		window = window[:0]
		if e = report("before_claim"); e != nil {
			return e
		}
		for stats.Completed < stats.Written {
			jobs, e := claimBatch()
			if e != nil {
				return e
			}
			if len(jobs) == 0 || len(jobs) > min(20, stats.Written-stats.Completed) {
				return fmt.Errorf("pipeline claim cannot finish early or exceed outstanding window: claimed=%d written=%d completed=%d", len(jobs), stats.Written, stats.Completed)
			}
			start = time.Now()
			e = r5BenchPrepare(ctx, func(runCtx context.Context, enqueue func(func(context.Context) error) bool) error {
				for _, job := range jobs {
					if !enqueue(track(func(ctx context.Context) error { return complete(ctx, job) })) {
						return runCtx.Err()
					}
				}
				return nil
			})
			if e = timed(start, &stats.ParseCompleteWallSeconds, e); e != nil {
				return e
			}
			stats.Completed += len(jobs)
			if e = report("batch_joined"); e != nil {
				return e
			}
		}
		return nil
	}
	var enqueueErr error
	generated := 0
	e := produce(ctx, func(job func(context.Context) error) bool {
		if enqueueErr != nil || ctx.Err() != nil {
			return false
		}
		generated++
		if generated > expected {
			enqueueErr = fmt.Errorf("producer exceeded expected population")
			return false
		}
		window = append(window, job)
		if len(window) == 20 {
			enqueueErr = flush()
		}
		return enqueueErr == nil
	})
	if e = errors.Join(e, enqueueErr, ctx.Err()); e != nil {
		return stats, e // All active work was joined inside flush, even on error.
	}
	if generated != expected {
		return stats, fmt.Errorf("producer stopped short: generated=%d expected=%d", generated, expected)
	}
	stats.ProducerDone = true
	if e = flush(); e != nil {
		return stats, e
	}
	jobs, e := claimBatch()
	if e != nil {
		return stats, e
	}
	if len(jobs) != 0 || active.Load() != 0 || stats.Completed != expected {
		return stats, fmt.Errorf("pipeline terminal claim/inflight/population mismatch")
	}
	stats.FinalEmptyClaim = true
	stats.SQLReady, e = ready(ctx)
	if e != nil {
		return stats, e
	}
	if stats.SQLReady != expected {
		return stats, fmt.Errorf("pipeline durable source-bound ready=%d expected=%d", stats.SQLReady, expected)
	}
	if e = report("terminal"); e != nil {
		return stats, e
	}
	return stats, nil
}

func (s *r5BenchState) seed(t *testing.T, size [3]int) {
	t.Helper()
	companyStarted := time.Now()
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
		// Legal benchmark fixture allowance covers 400 measured sends; keep all
		// enforcement enabled and preserve ordinary employee infrastructure rights.
		profile := &models.PermissionProfile{ID: r5BenchID("profile", ci), TenantID: &c.tenant.ID, Name: "Benchmark employee", Description: "Synthetic 400-sample delivery allowance", CanSend: true, DailySendQuota: 1000, MaxMailboxes: 1}
		r5BenchCheck(t, s.store.CreatePermissionProfile(ctx, profile))
		for n := 0; n < count; n++ {
			local := fmt.Sprintf("employee-%d", globalUser)
			email := local + "@contact.test"
			hash := company.Hash("benchmark-invite-" + local)
			_, err = s.store.InviteEmployee(ctx, c.actor, company.InvitationInput{Email: email, LocalPart: local, DisplayName: local, PermissionProfileID: &profile.ID}, hash)
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
	companyEnded := time.Now()
	companyMS, companyGap, err := r5BenchLatency(companyStarted, companyEnded, companyEnded.Sub(companyStarted))
	r5BenchCheck(t, err)
	rng := rand.New(rand.NewSource(r5BenchmarkSeed))
	var blob rawobject.BlobStore = s.objects
	var refs rawobject.ReferenceStore = s.store
	if s.diagnostic != nil {
		blob = &r5DiagnosticBlob{BlobStore: blob, diagnostic: s.diagnostic}
		refs = &r5DiagnosticRefs{ReferenceStore: refs, diagnostic: s.diagnostic}
	}
	rawStore := rawobject.NewStore(blob, refs)
	parser := mailcontent.New(s.objects) // One unchanged production four-parse gate.
	parserJoins := &r5BenchParserJoins{}
	var sizeBlock []int
	s.messages = make([]*models.Message, size[2])
	stats, err := r5BenchPipeline(ctx, size[2], func(runCtx context.Context, enqueue func(func(context.Context) error) bool) error {
		var lookahead *r5Lookahead
		if s.selectedPreparation() == r5PreparationLookahead || s.diagnostic != nil && s.diagnostic.lookaheadEnabled {
			lookahead = newR5Lookahead(runCtx, enqueue)
			s.inputScheduler = lookahead
			if s.diagnostic != nil {
				s.diagnostic.lookahead = lookahead
			}
		}
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
			s.messages[n] = message
			storeJob := func(workerCtx context.Context) error {
				if s.diagnostic != nil {
					workerCtx = s.diagnostic.scope(workerCtx, n, message.Size, message.MailboxID)
				}
				storeStarted := time.Time{}
				if s.diagnostic != nil {
					storeStarted = time.Now()
				}
				created, e := rawStore.StoreMessage(workerCtx, message, raw, 20000000)
				if s.diagnostic != nil {
					s.diagnostic.record(workerCtx, "store", storeStarted, e)
				}
				if e != nil {
					return e
				}
				if !created {
					return fmt.Errorf("original scale message %d not actually created", n)
				}
				// Same owned fixture lifecycle inputs, never replacement business queries.
				lifecycleStarted := time.Time{}
				if s.diagnostic != nil && n%20 >= 16 {
					lifecycleStarted = time.Now()
				}
				switch n % 20 {
				case 16, 17:
					_, e = s.pool.Exec(workerCtx, `UPDATE messages SET archived_at=clock_timestamp() WHERE id=$1`, message.ID)
				case 18:
					_, e = s.pool.Exec(workerCtx, `UPDATE messages SET deleted_at=clock_timestamp()-interval '2 hours',purge_after=clock_timestamp()-interval '1 hour' WHERE id=$1`, message.ID)
				case 19:
					if box.Kind != "shared" {
						return fmt.Errorf("hard-expiry ordinal assigned nonshared mailbox")
					}
					_, e = s.pool.Exec(workerCtx, `UPDATE messages SET expires_at=clock_timestamp()-interval '1 hour' WHERE id=$1`, message.ID)
				}
				if !lifecycleStarted.IsZero() {
					s.diagnostic.record(workerCtx, "lifecycle/"+r5BenchLife(n), lifecycleStarted, e)
				}
				return e
			}
			accepted := false
			if lookahead != nil {
				accepted = lookahead.addMapped(n, box.ID, r5GeneratedMailboxOrdinal(n, size[0]), storeJob)
			} else {
				accepted = enqueue(storeJob)
			}
			if !accepted {
				return runCtx.Err()
			}
		}
		if lookahead != nil {
			return lookahead.finish()
		}
		return nil
	}, s.store.ClaimMailIndexJobs, func(ctx context.Context, job company.MailIndexJob) error {
		doc, e := r5BenchDocumentJoined(ctx, parser, parserJoins, job.MessageID, job.SourceKey)
		if e != nil {
			return e
		}
		return s.store.CompleteMailIndexJob(ctx, job, *doc)
	}, s.indexReadyCount, func(stats r5BenchPreparation) error {
		stats.Method = s.selectedPreparation()
		parserJoins.observe(&stats)
		stats.CompanyWallSeconds = companyMS / 1000
		stats.MaxWallMonotonicGapMS = max(stats.MaxWallMonotonicGapMS, companyGap)
		depth := stats.Completed
		if stats.Phase == "batch_joined" {
			depth = stats.LastClaimReadyHeapDepth
		}
		if !stats.FinalEmptyClaim && depth != 20 && depth != 1000 && depth != 10000 && depth != 100000 && (depth == 0 || depth%250000 != 0) && depth != size[2]-20 {
			return nil
		}
		queryDigest, parameterDigest, e := s.preparationClaimDigests()
		if e != nil {
			return e
		}
		stats.ClaimQuerySHA256, stats.ClaimParametersSHA256 = queryDigest, parameterDigest
		if stats.Phase == "batch_joined" {
			if len(s.preparationCheckpoints) != 0 {
				checkpoint := s.preparationCheckpoints[len(s.preparationCheckpoints)-1]
				if before, ok := checkpoint["preparation"].(r5BenchPreparation); ok && before.Phase == "before_claim" && before.Completed == depth {
					checkpoint["actual_claim_joined"] = stats
				}
			}
			b, e := json.Marshal(stats)
			if e == nil {
				t.Logf("PREPARATION_ACTUAL_CLAIM_JOINED %s", b)
			}
			return e
		}
		checkpoint, e := s.preparationCheckpoint(stats)
		if e != nil {
			return e
		}
		s.preparationCheckpoints = append(s.preparationCheckpoints, checkpoint)
		b, e := json.Marshal(checkpoint)
		if e == nil {
			t.Logf("PREPARATION_CHECKPOINT %s", b)
		}
		return e
	})
	stats.Method = s.selectedPreparation()
	if stats.ClaimCalls != 0 {
		var digestErr error
		stats.ClaimQuerySHA256, stats.ClaimParametersSHA256, digestErr = s.preparationClaimDigests()
		err = errors.Join(err, digestErr)
	}
	stats.CompanyWallSeconds = companyMS / 1000
	stats.MaxWallMonotonicGapMS = max(stats.MaxWallMonotonicGapMS, companyGap)
	parserJoins.observe(&stats)
	if err == nil && len(s.preparationCheckpoints) != 0 {
		// One authoritative post-diagnostic terminal snapshot. Include the final
		// observation/EXPLAIN cost in pipeline wall, without numerical echo drift.
		s.preparationCheckpoints[len(s.preparationCheckpoints)-1]["preparation"] = stats
	}
	s.preparation = stats
	b, marshalErr := json.Marshal(stats)
	if marshalErr == nil {
		t.Logf("PREPARATION_TERMINAL %s", b)
	}
	r5BenchCheck(t, errors.Join(err, marshalErr))
}

type r5BenchMeasurement struct {
	Workload, CacheMode             string
	Samples                         int
	SQLCount                        int64
	P50, P95, P99                   float64
	AllocBytes, RSSBytes, DiskBytes uint64
	MaxWallMonotonicGapMS           float64
}

func (m r5BenchMeasurement) wire() map[string]any {
	return map[string]any{"workload": m.Workload, "cache_mode": m.CacheMode, "samples": m.Samples, "sql_count": m.SQLCount, "p50_ms": m.P50, "p95_ms": m.P95, "p99_ms": m.P99, "alloc_bytes": m.AllocBytes, "rss_bytes": m.RSSBytes, "disk_bytes": m.DiskBytes, "max_wall_monotonic_gap_ms": m.MaxWallMonotonicGapMS}
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

// UnixNano explicitly excludes time.Time's hidden monotonic component.
func r5BenchLatency(start, end time.Time, monotonic time.Duration) (float64, float64, error) {
	wall := end.UnixNano() - start.UnixNano()
	if wall < 0 || monotonic < 0 {
		return 0, 0, fmt.Errorf("benchmark clock moved backwards: wall_ns=%d monotonic_ns=%d", wall, monotonic)
	}
	gap := wall - int64(monotonic)
	if gap < 0 {
		gap = -gap
	}
	wallMS, gapMS := float64(wall)/float64(time.Millisecond), float64(gap)/float64(time.Millisecond)
	if gap > int64(100*time.Millisecond) {
		return wallMS, gapMS, fmt.Errorf("benchmark wall/monotonic gap=%fms exceeds100ms: host interruption or clock change", gapMS)
	}
	return wallMS, gapMS, nil
}

// Exactly one error per sample bounds the channel at 200 even when a
// suspended request also returns a deadline/transport error. No worker calls T.
func r5BenchSamples(sample func(int) (float64, float64, error)) ([]float64, float64, []error) {
	times, gaps := make([]float64, 200), make([]float64, 200)
	var wg sync.WaitGroup
	errs := make(chan error, 200)
	for worker := 0; worker < 20; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for n := worker; n < 200; n += 20 {
				var err error
				times[n], gaps[n], err = sample(n)
				if err != nil {
					errs <- err
				}
			}
		}(worker)
	}
	wg.Wait()
	close(errs)
	collected := make([]error, 0, len(errs))
	for err := range errs {
		collected = append(collected, err)
	}
	var maxGap float64
	for _, gap := range gaps {
		if gap > maxGap {
			maxGap = gap
		}
	}
	return times, maxGap, collected
}

func (s *r5BenchState) measure(t *testing.T, workload, cacheMode string, operation func(int) error) r5BenchMeasurement {
	t.Helper()
	var memBefore, memAfter runtime.MemStats
	runtime.ReadMemStats(&memBefore)
	beforeSQL := s.counter.count.Load()
	times, maxGap, sampleErrors := r5BenchSamples(func(n int) (float64, float64, error) {
		start := time.Now()
		operationErr := operation(n)
		end := time.Now()
		latency, gap, clockErr := r5BenchLatency(start, end, end.Sub(start))
		return latency, gap, errors.Join(operationErr, clockErr)
	})
	for _, err := range sampleErrors {
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
	return r5BenchMeasurement{workload, cacheMode, len(times), sql, times[99], times[189], times[197], memAfter.TotalAlloc - memBefore.TotalAlloc, rss, objectBytes + uint64(pgBytes), maxGap}
}

func TestR5BenchmarkGeneratorContract(t *testing.T) {
	if r5BenchmarkScales["S"] != [3]int{100, 500, 100000} || r5BenchmarkScales["M"] != [3]int{1000, 5000, 1000000} || r5BenchmarkScales["L"][2] != 10000000 {
		t.Fatal("original-scale generator contract changed")
	}
	a, b := rand.New(rand.NewSource(r5BenchmarkSeed)), rand.New(rand.NewSource(r5BenchmarkSeed))
	for block := 0; block < 3; block++ {
		left, right := r5BenchSizeBlock(a), r5BenchSizeBlock(b)
		counts := map[int]int{}
		for n, size := range left {
			if size != right[n] {
				t.Fatal("seed determinism changed")
			}
			counts[size]++
		}
		for size, expected := range map[int]int{4096: 800, 32768: 180, 262144: 19, 2097152: 1} {
			if counts[size] != expected {
				t.Fatalf("size bucket %d: got=%d want=%d", size, counts[size], expected)
			}
		}
	}
	life, tenant := map[string]int{}, [2]int{}
	for n := 0; n < 1000; n++ {
		life[r5BenchLife(n)]++
		ci := 0
		if (n/20)%5 == 4 {
			ci = 1
		}
		tenant[ci]++
	}
	if tenant != [2]int{800, 200} || life["inbox"] != 800 || life["archived"] != 100 || life["trash_expired"] != 50 || life["hard_expired"] != 50 {
		t.Fatal("exact tenant/lifecycle ordinal allocation changed")
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

func (s *r5BenchState) observedIndexReady(t *testing.T) int {
	t.Helper()
	n, err := s.indexReadyCount(s.ctx)
	r5BenchCheck(t, err)
	return n
}

func (s *r5BenchState) indexReadyCount(ctx context.Context) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM messages m JOIN mail_documents d ON d.tenant_id=m.tenant_id AND d.message_id=m.id JOIN mail_index_jobs j ON j.tenant_id=m.tenant_id AND j.message_id=m.id WHERE d.source_key=m.raw_object_key AND j.source_key=m.raw_object_key AND j.state='ready' AND d.parser_version=1`).Scan(&n)
	return n, err
}

// Read-only diagnostic of the actual ready heap immediately before the next
// real claim (or the terminal empty claim). EXPLAIN is NOT ANALYZE and makes no
// claim of execution timing. The following joined-claim log is actual timing.
// No planner settings, ANALYZE maintenance, index changes or copied claim SQL.
func (s *r5BenchState) preparationCheckpoint(stats r5BenchPreparation) (map[string]any, error) {
	ctx := s.ctx
	var total, pending, processing, readyJobs, failed, repeated int
	var schema int64
	err := s.pool.QueryRow(ctx, `SELECT count(*)::int,count(*) FILTER(WHERE state='pending')::int,count(*) FILTER(WHERE state='processing')::int,count(*) FILTER(WHERE state='ready')::int,count(*) FILTER(WHERE state='failed')::int,count(*) FILTER(WHERE attempts>1)::int FROM mail_index_jobs`).Scan(&total, &pending, &processing, &readyJobs, &failed, &repeated)
	if err != nil {
		return nil, err
	}
	ready, err := s.indexReadyCount(ctx)
	if err != nil {
		return nil, err
	}
	if total != stats.Written || pending != stats.Written-stats.Completed || pending > 20 || processing != 0 || failed != 0 || repeated != 0 || readyJobs != stats.Completed || ready != stats.Completed {
		return nil, fmt.Errorf("observed pipeline backlog/attempt/source-bound-ready mismatch: total=%d pending=%d processing=%d ready_jobs=%d durable_ready=%d failed=%d repeated=%d stats=%+v", total, pending, processing, readyJobs, ready, failed, repeated, stats)
	}
	if err = s.pool.QueryRow(ctx, `SELECT max(version_id) FROM goose_db_version WHERE is_applied`).Scan(&schema); err != nil {
		return nil, err
	}
	s.counter.mu.Lock()
	query, args := s.counter.claimSQL, append([]any(nil), s.counter.claimArgs...)
	s.counter.mu.Unlock()
	if query == "" || len(args) != 1 || args[0] != 20 {
		return nil, fmt.Errorf("missing actual production legal20 claim trace")
	}
	var raw []byte
	if err = s.pool.QueryRow(ctx, "EXPLAIN (FORMAT JSON) "+query, args...).Scan(&raw); err != nil {
		return nil, err
	}
	plan, err := r5BenchClaimPlan(raw)
	if err != nil {
		return nil, err
	}
	parameters, err := json.Marshal(args)
	if err != nil {
		return nil, err
	}
	queryHash, argsHash := sha256.Sum256([]byte(query)), sha256.Sum256(parameters)
	return map[string]any{"origin": "captured_preparation_claim_same_parameters_observed_heap", "phase": stats.Phase, "source_sha": os.Getenv("TABMAIL_R5_BENCHMARK_SOURCE_SHA"), "schema_version": schema, "maintenance": "no_explicit_analyze_or_planner_override", "claim_requested_limit": 20, "observed_jobs_total": total, "observed_pending": pending, "observed_processing": processing, "observed_ready_jobs": readyJobs, "observed_durable_source_bound_ready": ready, "observed_failed": failed, "observed_attempts_gt1": repeated, "query_sha256": hex.EncodeToString(queryHash[:]), "parameters_sha256": hex.EncodeToString(argsHash[:]), "explain_only_not_actual_claim_latency": true, "plan": plan, "preparation": stats}, nil
}

func (s *r5BenchState) preparationClaimDigests() (string, string, error) {
	s.counter.mu.Lock()
	query, args := s.counter.claimSQL, append([]any(nil), s.counter.claimArgs...)
	s.counter.mu.Unlock()
	if query == "" || len(args) != 1 || args[0] != 20 {
		return "", "", errors.New("missing actual legal20 claim identity")
	}
	parameters, err := json.Marshal(args)
	if err != nil {
		return "", "", err
	}
	qh, ph := sha256.Sum256([]byte(query)), sha256.Sum256(parameters)
	return hex.EncodeToString(qh[:]), hex.EncodeToString(ph[:]), nil
}

func r5BenchClaimPlan(raw []byte) ([]map[string]any, error) {
	var plans []map[string]any
	if err := json.Unmarshal(raw, &plans); err != nil {
		return nil, err
	}
	if len(plans) == 0 {
		return nil, errors.New("empty actual claim EXPLAIN plan")
	}
	var relation bool
	var walk func(map[string]any) error
	walk = func(node map[string]any) error {
		kind, ok := node["Node Type"].(string)
		if !ok || strings.TrimSpace(kind) == "" {
			return errors.New("claim EXPLAIN missing typed plan node")
		}
		if node["Relation Name"] == "mail_index_jobs" {
			relation = true
		}
		if value, exists := node["Plans"]; exists {
			children, ok := value.([]any)
			if !ok || len(children) == 0 {
				return errors.New("claim EXPLAIN malformed child plans")
			}
			for _, child := range children {
				v, ok := child.(map[string]any)
				if !ok {
					return errors.New("claim EXPLAIN malformed child node")
				}
				if err := walk(v); err != nil {
					return err
				}
			}
		}
		return nil
	}
	for _, entry := range plans {
		node, ok := entry["Plan"].(map[string]any)
		if !ok || len(node) == 0 {
			return nil, errors.New("claim EXPLAIN missing Plan object")
		}
		if err := walk(node); err != nil {
			return nil, err
		}
	}
	if !relation {
		return nil, errors.New("claim EXPLAIN lacks actual mail_index_jobs relation")
	}
	return plans, nil
}

func (s *r5BenchState) observations(t *testing.T, size [3]int) (map[string]any, string) {
	t.Helper()
	ctx := s.ctx
	count := func(query string) int { var n int; r5BenchCheck(t, s.pool.QueryRow(ctx, query).Scan(&n)); return n }
	distribution := map[string]any{
		"employees":          count(`SELECT count(*) FROM users WHERE role='user'`),
		"mailboxes":          count(`SELECT count(*) FROM mailboxes`),
		"messages":           count(`SELECT count(*) FROM messages`),
		"index_ready_count":  s.observedIndexReady(t),
		"personal_mailboxes": count(`SELECT count(*) FROM mailboxes WHERE mailbox_kind='personal'`),
		"shared_mailboxes":   count(`SELECT count(*) FROM mailboxes WHERE mailbox_kind='shared'`),
		"shared_grant_rows":  count(`SELECT count(*) FROM mailbox_grants g JOIN mailboxes b ON b.id=g.mailbox_id WHERE b.mailbox_kind='shared'`),
	}
	// Count labels alone are insufficient: assert the authoritative lifecycle,
	// tenant/grant boundary and index source rather than accepting stale rows.
	invalid := count(`SELECT count(*) FROM messages m JOIN mailboxes b ON b.id=m.mailbox_id WHERE
	 (m.deleted_at IS NOT NULL AND (m.purge_after IS NULL OR m.purge_after>=clock_timestamp())) OR
	 (m.expires_at IS NOT NULL AND (b.mailbox_kind<>'shared' OR m.expires_at>=clock_timestamp())) OR
	 (b.mailbox_kind='personal' AND m.expires_at IS NOT NULL)`)
	if invalid != 0 {
		t.Fatal("observed lifecycle is not exact purge/shared-expiry/personal-permanent contract")
	}
	if count(`SELECT count(*) FROM mailbox_grants g JOIN mailboxes b ON b.id=g.mailbox_id JOIN users u ON u.id=g.user_id WHERE g.tenant_id<>b.tenant_id OR u.tenant_id<>b.tenant_id OR NOT g.can_read`) != 0 {
		t.Fatal("observed shared grant is not legal tenant-local readable access")
	}
	if count(`SELECT count(*) FROM messages m LEFT JOIN mail_documents d ON d.message_id=m.id WHERE d.message_id IS NULL OR d.tenant_id<>m.tenant_id OR d.source_key<>m.raw_object_key OR d.parser_version<>1 OR d.text_body NOT LIKE 'needle-%' OR d.source_sha256=''`) != 0 {
		t.Fatal("observed index-ready rows do not match actual raw source/parser/body")
	}
	var lo, hi int
	r5BenchCheck(t, s.pool.QueryRow(ctx, `SELECT min(n),max(n) FROM (SELECT b.id,count(DISTINCT g.user_id)::int n FROM mailboxes b LEFT JOIN mailbox_grants g ON g.mailbox_id=b.id WHERE b.mailbox_kind='shared' GROUP BY b.id) x`).Scan(&lo, &hi))
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
	s.fingerprintStreams = nil
	streams := []string{
		`SELECT jsonb_build_array(t.name,p.name,p.can_send,p.daily_send_quota,p.daily_receive_quota,p.max_mailboxes,p.max_domains,p.allowed_zone_ids,p.can_create_domains,p.can_create_routes,p.can_create_api_keys)::text FROM permission_profiles p JOIN tenants t ON t.id=p.tenant_id ORDER BY t.name,p.name`,
		`SELECT jsonb_build_array(m.id,d.source_key,d.source_sha256,d.parser_version,d.text_body,d.html_body,d.body_access,d.parts,d.thread_key,d.search_text,j.state,j.source_key)::text FROM mail_documents d JOIN messages m ON m.id=d.message_id AND m.tenant_id=d.tenant_id JOIN mail_index_jobs j ON j.message_id=m.id AND j.tenant_id=m.tenant_id ORDER BY m.id`,
		`SELECT jsonb_build_array(m.id,t.name,b.full_address,m.subject,m.size,m.raw_object_key,m.archived_at IS NOT NULL,m.deleted_at IS NOT NULL,m.expires_at IS NOT NULL AND m.expires_at<clock_timestamp())::text FROM messages m JOIN mailboxes b ON b.id=m.mailbox_id JOIN tenants t ON t.id=m.tenant_id ORDER BY m.id`,
		`SELECT jsonb_build_array(b.full_address,u.email,g.can_read,g.can_organize,g.can_send)::text FROM mailbox_grants g JOIN mailboxes b ON b.id=g.mailbox_id JOIN users u ON u.id=g.user_id ORDER BY b.full_address,u.email`,
		`SELECT jsonb_build_array(t.name,u.email,u.role,u.is_active)::text FROM users u JOIN tenants t ON t.id=u.tenant_id ORDER BY t.name,u.email`,
		`SELECT jsonb_build_array(t.name,b.full_address,b.mailbox_kind,u.email)::text FROM mailboxes b JOIN tenants t ON t.id=b.tenant_id LEFT JOIN users u ON u.id=b.owner_user_id ORDER BY b.full_address`,
	}
	for i, q := range streams {
		fmt.Fprintf(h, "stream:%d\n", i)
		streamHash := sha256.New()
		rowCount := 0
		r, e := s.pool.Query(ctx, q)
		r5BenchCheck(t, e)
		for r.Next() {
			var value string
			r5BenchCheck(t, r.Scan(&value))
			_, e = h.Write([]byte(value + "\n"))
			r5BenchCheck(t, e)
			_, e = streamHash.Write([]byte(value + "\n"))
			r5BenchCheck(t, e)
			rowCount++
		}
		r5BenchCheck(t, r.Err())
		r.Close()
		s.fingerprintStreams = append(s.fingerprintStreams, map[string]any{"stream_id": i, "row_count": rowCount, "sha256": hex.EncodeToString(streamHash.Sum(nil))})
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
	r5BenchRunScale(t, scale, size, budget, output, source)
}

func r5BenchRunScale(t *testing.T, scale string, size [3]int, budget time.Duration, output, source string) {
	method, methodErr := r5SelectPreparation(os.Getenv("TABMAIL_R5_BENCHMARK_PREPARATION_METHOD"), scale, os.Getenv("TABMAIL_R5_BENCHMARK_METHOD_CONTRACT_SHA256"))
	r5BenchCheck(t, methodErr)
	started := time.Now()
	ctx, cancel := context.WithDeadline(context.Background(), time.Unix(0, started.UnixNano()+int64(budget)))
	clockStopped := make(chan struct{})
	go func() {
		defer close(clockStopped)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if time.Now().UnixNano()-started.UnixNano() >= int64(budget) {
					cancel()
					return
				}
			}
		}
	}()
	defer func() { cancel(); <-clockStopped }()
	state := r5BenchOwned(t, ctx)
	state.preparationMethod = method
	state.seed(t, size)
	distribution, actualFingerprint := state.observations(t, size)
	var indexed, employees, mailboxes, messages int
	var schema int64
	r5BenchCheck(t, state.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM users WHERE role='user'),(SELECT count(*) FROM mailboxes),(SELECT count(*) FROM messages),(SELECT count(*) FROM mail_documents),(SELECT max(version_id) FROM goose_db_version WHERE is_applied)`).Scan(&employees, &mailboxes, &messages, &indexed, &schema))
	indexed = state.observedIndexReady(t)
	if [3]int{employees, mailboxes, messages} != size || indexed != size[2] {
		t.Fatal("actual original scale/index-ready population mismatch")
	}
	var persistentObservations map[string]any
	if method == r5PreparationLookahead {
		var observedErr error
		persistentObservations, observedErr = state.preparationPersistentObservations(ctx, size, source, schema)
		r5BenchCheck(t, observedErr)
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
	personal, shared := c.personal[0], c.shared[len(c.users)]
	var personalMessage, sharedMessage, foreignMessage *models.Message
	for ordinal, message := range state.messages {
		if r5BenchLife(ordinal) != "inbox" {
			continue
		}
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
	foreignCompany := state.companies[1]
	var foreignReader *models.User
	for i, box := range foreignCompany.personal {
		if box.ID == foreignMessage.MailboxID {
			foreignReader = foreignCompany.users[i]
		}
	}
	for i, box := range foreignCompany.shared {
		if box.ID == foreignMessage.MailboxID {
			foreignReader = foreignCompany.users[i%len(foreignCompany.users)]
		}
	}
	if foreignReader == nil {
		t.Fatal("foreign positive control actor missing")
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
		// Use the actual foreign mailbox owner/grantee for positive control, not
		// an expired or inaccessible foreign message that always returns 404.
		foreignToken, e := authn.IssueAccessToken(secret, foreignReader)
		r5BenchCheck(t, e)
		r5BenchCheck(t, r5BenchHTTP(ctx, client, foreignPath, foreignToken, "GET", nil, "", 200))
		r5BenchCheck(t, r5BenchHTTP(ctx, client, foreignPath, token, "GET", nil, "", 404))
		sharedPath := server.URL + "/api/v1/company/mailboxes/" + shared.ID.String() + "/messages/" + sharedMessage.ID.String() + "/source"
		r5BenchCheck(t, r5BenchHTTP(ctx, client, sharedPath, token, "GET", nil, "", 200))
		current, e := state.store.GetWorkMailbox(ctx, c.actor, shared.ID)
		r5BenchCheck(t, e)
		r5BenchCheck(t, state.store.SetWorkGrant(ctx, c.actor, models.MailboxGrant{TenantID: c.tenant.ID, MailboxID: shared.ID, UserID: reader.ID}, current.Revision))
		revokedPath := server.URL + "/api/v1/company/mailboxes/" + shared.ID.String() + "/messages/" + sharedMessage.ID.String() + "/source"
		r5BenchCheck(t, r5BenchHTTP(ctx, client, revokedPath, token, "GET", nil, "", 403))
		current, e = state.store.GetWorkMailbox(ctx, c.actor, shared.ID)
		r5BenchCheck(t, e)
		r5BenchCheck(t, state.store.SetWorkGrant(ctx, c.actor, models.MailboxGrant{TenantID: c.tenant.ID, MailboxID: shared.ID, UserID: reader.ID, CanRead: true}, current.Revision))
		r5BenchCheck(t, r5BenchHTTP(ctx, client, sharedPath, token, "GET", nil, "", 200))
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
					status, raw, e := r5BenchRoundTrip(ctx, client, server.URL+"/api/v1/company/mailboxes/"+shared.ID.String()+"/messages?folder=inbox&q=needle", token, "GET", nil, "")
					if e != nil {
						return e
					}
					if status != 200 {
						return fmt.Errorf("indexed search status=%d", status)
					}
					var response struct {
						Data []models.Message
						Meta struct{ Total int }
					}
					if e = json.Unmarshal(raw, &response); e != nil {
						return e
					}
					if len(response.Data) == 0 || response.Meta.Total < 1 {
						return fmt.Errorf("shipping index body-only needle search returned no seeded results")
					}
					for _, m := range response.Data {
						if m.MailboxID != shared.ID {
							return fmt.Errorf("shipping search returned wrong mailbox")
						}
					}
					return nil
				case "indexed_message":
					status, raw, e := r5BenchRoundTrip(ctx, client, server.URL+"/api/v1/company/mailboxes/"+personal.ID.String()+"/messages/"+personalMessage.ID.String(), token, "GET", nil, "")
					if e != nil {
						return e
					}
					if status != 200 {
						return fmt.Errorf("indexed message status=%d", status)
					}
					var response struct{ Data models.MessageDetail }
					if e = json.Unmarshal(raw, &response); e != nil {
						return e
					}
					if response.Data.ID != personalMessage.ID || !strings.HasPrefix(response.Data.TextBody, "needle-") {
						return fmt.Errorf("shipping indexed detail returned no expected actual parsed body")
					}
					return nil
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
	result := map[string]any{"scale": scale, "employees": employees, "mailboxes": mailboxes, "messages": messages, "seed": r5BenchmarkSeed, "concurrency": 20, "pool_size": 24, "schema_version": schema, "source_sha": source, "dataset_sha256": hex.EncodeToString(hash[:]), "dataset_fingerprint": actualFingerprint, "fingerprint_method": "six_actual_canonical_sql_streams_v1", "fingerprint_streams": state.fingerprintStreams, "parameter_fingerprint": r5BenchParameterFingerprint(size), "actual_distribution": distribution, "preparation": state.preparation, "preparation_checkpoints": state.preparationCheckpoints, "sql_plans": sqlPlans, "resource_scope": r5BenchResourceScope, "system_memory_observation": "not_measured", "index_ready_count": indexed, "sql_tracer_calibration_count": calibration, "sql_tracer_calibration_expected": 20, "sql_count_scope": "actual application and worker pool queries, includes transaction/control queries; no SQL text or params recorded", "hardware": map[string]any{"os": runtime.GOOS, "arch": runtime.GOARCH, "cpu": runtime.NumCPU()}, "safety_assertions": security, "safety_assertions_passed": true, "measurements": measurements, "cache_boundary": "app lifecycle/Redis fresh versus reused; DB/OS cache is explicitly unspecified", "task_complete": false, "product_green": false}
	if method == r5PreparationLookahead {
		result["preparation_method"] = method
		result["method_contract_sha256"] = os.Getenv("TABMAIL_R5_BENCHMARK_METHOD_CONTRACT_SHA256")
		result["preparation_input_scheduler"] = state.inputScheduler.report(method)
		result["preparation_identity_boundary"] = r5PreparationBoundary()
		result["preparation_persistent_observations"] = persistentObservations
	}
	if scale == "tool_only" {
		result["mode"] = "TOOL_ONLY_DATASET_CALIBRATION"
		result["S_M_L_executed"] = false
		objectBytes, e := r5BenchDisk(state.objectRoot)
		r5BenchCheck(t, e)
		rss, e := r5BenchRSS()
		r5BenchCheck(t, e)
		var pgBytes int64
		r5BenchCheck(t, state.pool.QueryRow(ctx, `SELECT pg_database_size(current_database())`).Scan(&pgBytes))
		result["rss_bytes"], result["disk_bytes"] = rss, objectBytes+uint64(pgBytes)
	}
	completed := time.Now()
	elapsedWall := float64(completed.UnixNano()-started.UnixNano()) / float64(time.Second)
	if elapsedWall < 0 || elapsedWall > budget.Seconds() {
		t.Fatalf("actual execution wall seconds=%f exceed budget=%f or moved backwards", elapsedWall, budget.Seconds())
	}
	result["latency_clock"] = r5BenchLatencyClock
	result["execution_wall"] = map[string]any{"started_unix_seconds": float64(started.UnixNano()) / float64(time.Second), "completed_unix_seconds": float64(completed.UnixNano()) / float64(time.Second), "elapsed_wall_seconds": elapsedWall, "budget_seconds": int(budget / time.Second)}
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
	result := map[string]any{"mode": "TOOL_ONLY_DATASET_CALIBRATION", "scale": "tool_only", "S_M_L_executed": false, "employees": 20, "mailboxes": 100, "messages": 1000, "seed": r5BenchmarkSeed, "source_sha": source, "schema_version": schema, "dataset_sha256": hex.EncodeToString(hash[:]), "actual_distribution": distribution, "dataset_fingerprint": fingerprint, "fingerprint_method": "six_actual_canonical_sql_streams_v1", "fingerprint_streams": state.fingerprintStreams, "parameter_fingerprint": r5BenchParameterFingerprint(size), "preparation": state.preparation, "preparation_checkpoints": state.preparationCheckpoints, "sql_plans": []map[string]any{plan}, "resource_scope": r5BenchResourceScope, "system_memory_observation": "not_measured", "rss_bytes": rss, "disk_bytes": objectBytes + uint64(pgBytes), "task_complete": false, "product_green": false}
	b, e := json.MarshalIndent(result, "", "  ")
	r5BenchCheck(t, e)
	f, e := os.OpenFile(output, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	r5BenchCheck(t, e)
	_, e = f.Write(append(b, '\n'))
	r5BenchCheck(t, e)
	r5BenchCheck(t, f.Close())
	t.Log("TOOL_ONLY_DATASET_CALIBRATION actual20/100/1000 distributions/streamSHA/index/ACL/shippingplan observed; noS_M_Lbaseline")
}

func TestR5BenchmarkCapturedQueryIdentity(t *testing.T) {
	c := &r5SQLCounter{capture: true}
	c.TraceQueryStart(context.Background(), nil, pgx.TraceQueryStartData{SQL: `SELECT p.max_messages FROM tenants t JOIN plans p ON p.id=t.plan_id`})
	if c.sql != "" {
		t.Fatal("quota query confused with shipping messages query")
	}
	c.TraceQueryStart(context.Background(), nil, pgx.TraceQueryStartData{SQL: `SELECT m.id FROM messages m WHERE m.id=$1`, Args: []any{r5BenchID("message", 0)}})
	if c.sql == "" || len(c.args) != 1 {
		t.Fatal("actual message query/parameters not captured")
	}
}

func TestR5BenchmarkObservedIdentityBinding(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	state := r5BenchOwned(t, ctx)
	size := [3]int{20, 100, 1000}
	state.seed(t, size)
	_, baseline := state.observations(t, size)
	id := state.messages[0].ID
	_, err := state.pool.Exec(ctx, `UPDATE mail_documents SET source_sha256=repeat('a',64) WHERE message_id=$1`, id)
	r5BenchCheck(t, err)
	_, changed := state.observations(t, size)
	if changed == baseline {
		t.Fatal("persisted parsed digest omitted from fingerprint")
	}
	for _, mutation := range []string{
		`UPDATE mail_documents SET parser_version=2 WHERE message_id=$1`,
		`UPDATE mail_documents SET source_key='stale-original' WHERE message_id=$1`,
		`UPDATE mail_index_jobs SET source_key='stale-original' WHERE message_id=$1`,
		`UPDATE mail_index_jobs SET state='failed' WHERE message_id=$1`,
	} {
		_, err = state.pool.Exec(ctx, mutation, id)
		r5BenchCheck(t, err)
		if got := state.observedIndexReady(t); got != 999 {
			t.Fatalf("stale parse/job counted ready: %d", got)
		}
		_, err = state.pool.Exec(ctx, `UPDATE mail_documents d SET source_key=m.raw_object_key,parser_version=1 FROM messages m WHERE d.message_id=m.id AND m.id=$1`, id)
		r5BenchCheck(t, err)
		_, err = state.pool.Exec(ctx, `UPDATE mail_index_jobs j SET source_key=m.raw_object_key,state='ready' FROM messages m WHERE j.message_id=m.id AND m.id=$1`, id)
		r5BenchCheck(t, err)
		if got := state.observedIndexReady(t); got != 1000 {
			t.Fatalf("restored ready count: %d", got)
		}
	}
	for _, fault := range []string{"expired_lease", "stale_token", "changed_source"} {
		t.Run("real_complete_rejects_"+fault, func(t *testing.T) {
			message := state.messages[1]
			_, err := state.pool.Exec(ctx, `DELETE FROM mail_documents WHERE message_id=$1`, message.ID)
			r5BenchCheck(t, err)
			_, err = state.pool.Exec(ctx, `UPDATE mail_index_jobs SET state='pending',attempts=0,next_attempt_at=clock_timestamp(),lease_token=NULL,lease_until=NULL WHERE message_id=$1`, message.ID)
			r5BenchCheck(t, err)
			jobs, err := state.store.ClaimMailIndexJobs(ctx, 20)
			r5BenchCheck(t, err)
			if len(jobs) != 1 || jobs[0].MessageID != message.ID {
				t.Fatal("real claim did not bind the single fault fixture")
			}
			job := jobs[0]
			parser := mailcontent.New(state.objects)
			doc, err := parser.Document(ctx, job.MessageID, job.SourceKey)
			r5BenchCheck(t, err)
			switch fault {
			case "expired_lease":
				_, err = state.pool.Exec(ctx, `UPDATE mail_index_jobs SET lease_until=clock_timestamp()-interval '1 second' WHERE message_id=$1`, message.ID)
			case "stale_token":
				job.Token = uuid.New()
			case "changed_source":
				_, err = state.pool.Exec(ctx, `UPDATE messages SET raw_object_key='synthetic-changed-source' WHERE id=$1`, message.ID)
			}
			r5BenchCheck(t, err)
			err = state.store.CompleteMailIndexJob(ctx, job, *doc)
			wantMessage := "index lease lost"
			if fault == "changed_source" {
				wantMessage = "index provenance changed"
			}
			if rejection, ok := app.As(err); !ok || rejection.Kind != app.KindConflict || rejection.Message != wantMessage {
				t.Fatalf("real completion wrong lease/token/source rejection: want=%q err=%v", wantMessage, err)
			}
			var documents int
			r5BenchCheck(t, state.pool.QueryRow(ctx, `SELECT count(*) FROM mail_documents WHERE message_id=$1`, message.ID).Scan(&documents))
			if documents != 0 {
				t.Fatal("rejected completion left a durable document")
			}
			// Restore through the real source trigger and a new real lease, not a
			// forged ready row or document. Each fault must retain this positive control.
			_, err = state.pool.Exec(ctx, `UPDATE messages SET raw_object_key=$2 WHERE id=$1`, message.ID, message.RawObjectKey)
			r5BenchCheck(t, err)
			_, err = state.pool.Exec(ctx, `UPDATE mail_index_jobs SET state='pending',attempts=0,next_attempt_at=clock_timestamp(),lease_token=NULL,lease_until=NULL WHERE message_id=$1`, message.ID)
			r5BenchCheck(t, err)
			jobs, err = state.store.ClaimMailIndexJobs(ctx, 20)
			r5BenchCheck(t, err)
			if len(jobs) != 1 {
				t.Fatal("fault recovery lost its claim")
			}
			r5BenchCheck(t, state.store.CompleteMailIndexJob(ctx, jobs[0], *doc))
			if state.observedIndexReady(t) != 1000 {
				t.Fatal("fault recovery positive control was not durably ready")
			}
		})
	}
	t.Run("real_parser_error_propagates", func(t *testing.T) {
		key := state.messages[0].RawObjectKey
		r5BenchCheck(t, state.objects.Delete(ctx, key))
		parser := mailcontent.New(state.objects)
		err := r5BenchPrepare(ctx, func(ctx context.Context, enqueue func(func(context.Context) error) bool) error {
			enqueue(func(ctx context.Context) error { _, err := parser.Document(ctx, id, key); return err })
			return nil
		})
		if err == nil {
			t.Fatal("real missing raw parser error silently accepted")
		}
	})
	t.Run("real_object_error_propagates_and_rolls_back", func(t *testing.T) {
		r5BenchCheck(t, os.RemoveAll(state.objectRoot))
		r5BenchCheck(t, os.WriteFile(state.objectRoot, []byte("owned failure fixture"), 0600))
		c := state.companies[0]
		raw := []byte("From: synthetic@test\r\nSubject: worker object error\r\n\r\nbody")
		message := &models.Message{ID: r5BenchID("object-error", 0), TenantID: c.tenant.ID, MailboxID: c.personal[0].ID, ZoneID: c.zone.ID, Size: int64(len(raw)), RawObjectKey: rawobject.Key(raw), Sender: "synthetic@test", Recipients: []string{c.personal[0].FullAddress}, HeadersJSON: json.RawMessage(`{}`)}
		store := rawobject.NewStore(state.objects, state.store)
		err := r5BenchPrepare(ctx, func(ctx context.Context, enqueue func(func(context.Context) error) bool) error {
			enqueue(func(ctx context.Context) error { _, err := store.StoreMessage(ctx, message, raw, 20000000); return err })
			return nil
		})
		if err == nil {
			t.Fatal("real filesystem failure silently accepted")
		}
		var n int
		r5BenchCheck(t, state.pool.QueryRow(ctx, `SELECT count(*) FROM messages WHERE id=$1`, message.ID).Scan(&n))
		if n != 0 {
			t.Fatal("failed worker object write committed message")
		}
	})
}

// Executes exactly the same shipping workload/safety/sampling path at the
// explicitly non-S/M calibration population before expensive baseline runs.
func TestR5BenchmarkToolOnlyWorkloads(t *testing.T) {
	output, source := os.Getenv("TABMAIL_R5_BENCHMARK_OUTPUT"), os.Getenv("TABMAIL_R5_BENCHMARK_SOURCE_SHA")
	if output == "" || len(source) != 40 {
		t.Fatal("fresh private output/source required")
	}
	r5BenchRunScale(t, "tool_only", [3]int{20, 100, 1000}, 5*time.Minute, output, source)
}

func TestR5BenchmarkPrepareCancelJoin(t *testing.T) {
	injected := fmt.Errorf("injected fixture worker error")
	var active, started atomic.Int32
	ready := make(chan struct{})
	err := r5BenchPrepare(context.Background(), func(ctx context.Context, enqueue func(func(context.Context) error) bool) error {
		for n := 0; n < 20; n++ {
			if !enqueue(func(ctx context.Context) error {
				active.Add(1)
				defer active.Add(-1)
				if started.Add(1) == 20 {
					close(ready)
				}
				<-ready
				if n == 0 {
					return injected
				}
				<-ctx.Done()
				return ctx.Err()
			}) {
				return ctx.Err()
			}
		}
		return nil
	})
	if !errors.Is(err, injected) || active.Load() != 0 || started.Load() != 20 {
		t.Fatalf("fixture error/cancel/join lost: err=%v active=%d started=%d", err, active.Load(), started.Load())
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = r5BenchPrepare(ctx, func(ctx context.Context, enqueue func(func(context.Context) error) bool) error { return ctx.Err() })
	if err != context.Canceled {
		t.Fatalf("fixture cancellation disappeared: %v", err)
	}
}

func TestR5BenchmarkPipelineContract(t *testing.T) {
	var pending []company.MailIndexJob
	var mu sync.Mutex
	var written, completed, maxPending atomic.Int32
	stats, err := r5BenchPipeline(context.Background(), 65, func(ctx context.Context, enqueue func(func(context.Context) error) bool) error {
		for n := 0; n < 65; n++ {
			if !enqueue(func(context.Context) error {
				mu.Lock()
				defer mu.Unlock()
				pending = append(pending, company.MailIndexJob{MessageID: r5BenchID("pipeline", n)})
				written.Add(1)
				maxPending.Store(max(maxPending.Load(), int32(len(pending))))
				return nil
			}) {
				return ctx.Err()
			}
		}
		return nil
	}, func(_ context.Context, limit int) ([]company.MailIndexJob, error) {
		if limit != 20 {
			return nil, fmt.Errorf("illegal production claim limit %d", limit)
		}
		mu.Lock()
		defer mu.Unlock()
		// Partial batches must drain, rather than treating a short batch as EOF.
		n := min(7, len(pending))
		jobs := append([]company.MailIndexJob(nil), pending[:n]...)
		pending = pending[n:]
		return jobs, nil
	}, func(context.Context, company.MailIndexJob) error {
		completed.Add(1)
		return nil
	}, func(context.Context) (int, error) { return int(completed.Load()), nil }, nil)
	if err != nil || written.Load() != 65 || completed.Load() != 65 || maxPending.Load() > 20 || stats.MaxActive > 20 || stats.MaxWindowMessages != 20 || !stats.ProducerDone || !stats.FinalEmptyClaim || stats.SQLReady != 65 {
		t.Fatalf("bounded pipeline completion contract: stats=%+v written=%d completed=%d pendingMax=%d err=%v", stats, written.Load(), completed.Load(), maxPending.Load(), err)
	}
}

func TestR5BenchmarkPrepareIndependentCauses(t *testing.T) {
	workerA, workerB, producer := errors.New("independent worker A"), errors.New("independent worker B"), errors.New("independent producer")
	var started, active atomic.Int32
	barrier := make(chan struct{})
	err := r5BenchPrepare(context.Background(), func(ctx context.Context, enqueue func(func(context.Context) error) bool) error {
		for _, cause := range []error{workerA, workerB} {
			if !enqueue(func(context.Context) error {
				active.Add(1)
				defer active.Add(-1)
				if started.Add(1) == 2 {
					close(barrier)
				}
				<-barrier
				return cause
			}) {
				return ctx.Err()
			}
		}
		<-barrier
		return producer
	})
	if !errors.Is(err, workerA) || !errors.Is(err, workerB) || !errors.Is(err, producer) || started.Load() != 2 || active.Load() != 0 {
		t.Fatalf("distinct worker/producer causes or join lost: err=%v started=%d active=%d", err, started.Load(), active.Load())
	}
}

func TestR5BenchmarkClaimPlanProof(t *testing.T) {
	valid := []byte(`[{"Plan":{"Node Type":"ModifyTable","Relation Name":"mail_index_jobs","Plans":[{"Node Type":"Seq Scan","Relation Name":"mail_index_jobs"}]}}]`)
	if _, err := r5BenchClaimPlan(valid); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{`null`, `[]`, `{}`, `[{}]`, `[{"Plan":null}]`, `[{"Plan":{}}]`, `[{"Plan":{"Node Type":"Result"}}]`, `[{"Plan":{"Node Type":"Seq Scan","Relation Name":"messages"}}]`, `[{"Plan":{"Relation Name":"mail_index_jobs"}}]`, `[{"Plan":{"Node Type":"ModifyTable","Relation Name":"mail_index_jobs","Plans":null}}]`, `[{"Plan":{"Node Type":"ModifyTable","Relation Name":"mail_index_jobs","Plans":[null]}}]`} {
		t.Run(raw, func(t *testing.T) {
			if _, err := r5BenchClaimPlan([]byte(raw)); err == nil {
				t.Fatal("empty/malformed/wrong-relation EXPLAIN silently accepted")
			}
		})
	}
}

type r5BenchObjectReaderFunc func(context.Context, string) (io.ReadCloser, error)

func (f r5BenchObjectReaderFunc) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	return f(ctx, key)
}

type r5BenchBlockingReader struct {
	reader          io.Reader
	release, closed chan struct{}
	onClose         func()
	once            sync.Once
}

func (r *r5BenchBlockingReader) Read(buf []byte) (int, error) {
	select {
	case <-r.release:
		return r.reader.Read(buf)
	case <-r.closed:
		return 0, io.ErrClosedPipe
	}
}
func (r *r5BenchBlockingReader) Close() error {
	r.once.Do(func() { close(r.closed); r.onClose() })
	return nil
}

func TestR5BenchmarkParserJoinAdmission(t *testing.T) {
	for _, fault := range []string{"no_deadline", "exact_reserve", "canceled"} {
		t.Run(fault, func(t *testing.T) {
			var opens atomic.Int32
			parser := mailcontent.New(r5BenchObjectReaderFunc(func(context.Context, string) (io.ReadCloser, error) {
				opens.Add(1)
				return io.NopCloser(strings.NewReader("From: synthetic@test\r\n\r\nbody")), nil
			}))
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if fault == "exact_reserve" {
				var deadlineCancel context.CancelFunc
				ctx, deadlineCancel = context.WithDeadline(ctx, time.Now().Add(r5BenchParserJoinReserve))
				defer deadlineCancel()
			} else if fault == "canceled" {
				cancel()
			}
			joins := &r5BenchParserJoins{}
			_, err := r5BenchDocumentJoined(ctx, parser, joins, r5BenchID("admission", 0), "synthetic")
			want := context.DeadlineExceeded
			if fault == "canceled" {
				want = context.Canceled
			}
			if !errors.Is(err, want) || opens.Load() != 0 || joins.active.Load() != 0 || joins.joined.Load() != 0 {
				t.Fatalf("parser admission started background work: err=%v opens=%d joined=%d active=%d", err, opens.Load(), joins.joined.Load(), joins.active.Load())
			}
		})
	}
}

func TestR5BenchmarkParserFlightDrain(t *testing.T) {
	for _, mode := range []string{"cancel_then_release", "cancel_then_timeout"} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				release := make(chan struct{})
				var opens, closes, openActive, openPeak atomic.Int32
				parser := mailcontent.New(r5BenchObjectReaderFunc(func(ctx context.Context, key string) (io.ReadCloser, error) {
					if err := ctx.Err(); err != nil {
						return nil, err
					}
					opens.Add(1)
					if key == "positive_after_drain" {
						return io.NopCloser(strings.NewReader("From: synthetic@test\r\n\r\npositive")), nil
					}
					n := openActive.Add(1)
					for old := openPeak.Load(); n > old && !openPeak.CompareAndSwap(old, n); old = openPeak.Load() {
					}
					return &r5BenchBlockingReader{reader: strings.NewReader("From: synthetic@test\r\n\r\nbody"), release: release, closed: make(chan struct{}), onClose: func() { closes.Add(1); openActive.Add(-1) }}, nil
				}))
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
				defer cancel()
				joins := &r5BenchParserJoins{}
				done := make(chan error, 1)
				count := 1
				if mode == "cancel_then_timeout" {
					count = 20 // Four admitted real flights and sixteen gate waiters.
				}
				start := time.Now()
				go func() {
					done <- r5BenchPrepare(ctx, func(ctx context.Context, enqueue func(func(context.Context) error) bool) error {
						for n := 0; n < count; n++ {
							if !enqueue(func(ctx context.Context) error {
								_, err := r5BenchDocumentJoined(ctx, parser, joins, r5BenchID("drain", n), fmt.Sprintf("blocked_%d", n))
								return err
							}) {
								return ctx.Err()
							}
						}
						return nil
					})
				}()
				synctest.Wait()
				if joins.active.Load() != int64(count) || opens.Load() != int32(min(count, 4)) {
					t.Fatalf("real Parser gate/admission barrier missing: active=%d opens=%d", joins.active.Load(), opens.Load())
				}
				cancel()
				synctest.Wait()
				select {
				case err := <-done:
					t.Fatalf("fixture returned before detached Parser flight drained: %v", err)
				default:
				}
				if mode == "cancel_then_release" {
					close(release)
				}
				err := <-done // synctest advances the unchanged Parser's 30s timeout.
				synctest.Wait()
				if !errors.Is(err, context.Canceled) || joins.active.Load() != 0 || joins.joined.Load() != int64(count) || joins.canceled.Load() != int64(count) || openActive.Load() != 0 || closes.Load() != int32(min(count, 4)) || openPeak.Load() > 4 {
					t.Fatalf("real flight/readers not joined: err=%v joined=%d canceled=%d active=%d opens=%d closes=%d openActive=%d peak=%d", err, joins.joined.Load(), joins.canceled.Load(), joins.active.Load(), opens.Load(), closes.Load(), openActive.Load(), openPeak.Load())
				}
				if mode == "cancel_then_timeout" && (!errors.Is(err, context.DeadlineExceeded) || time.Since(start) != 30*time.Second || opens.Load() != 4) {
					t.Fatalf("queued flight timeout/drain bound lost: err=%v elapsed=%v opens=%d", err, time.Since(start), opens.Load())
				}
				deadline, _ := ctx.Deadline()
				if joins.minRemainingNanos.Load() <= int64(r5BenchParserJoinReserve) || joins.deadlineNanos.Load() != deadline.UnixNano() {
					t.Fatal("parser admission min wall remaining/original deadline was not observed")
				}
				// Same Parser must admit fresh work after drain: leaked gate slots fail.
				if _, err := parser.Document(context.Background(), r5BenchID("drain-positive", 0), "positive_after_drain"); err != nil {
					t.Fatal(err)
				}
			})
		})
	}
}

func TestR5BenchmarkPipelineCancelJoin(t *testing.T) {
	for _, phase := range []string{"store", "parse_complete"} {
		t.Run(phase, func(t *testing.T) {
			injected, secondary := errors.New("injected pipeline failure"), errors.New("second operation failure")
			var active, started atomic.Int32
			barrier := make(chan struct{})
			failing := func(ctx context.Context, n int) error {
				active.Add(1)
				defer active.Add(-1)
				if started.Add(1) == 20 {
					close(barrier)
				}
				<-barrier
				if n == 0 {
					return errors.Join(injected, secondary)
				}
				<-ctx.Done()
				return ctx.Err()
			}
			var jobs []company.MailIndexJob
			for n := 0; n < 20; n++ {
				jobs = append(jobs, company.MailIndexJob{MessageID: r5BenchID("pipeline-cancel", n)})
			}
			stats, err := r5BenchPipeline(context.Background(), 20, func(ctx context.Context, enqueue func(func(context.Context) error) bool) error {
				for n := 0; n < 20; n++ {
					if !enqueue(func(ctx context.Context) error {
						if phase == "store" {
							return failing(ctx, n)
						}
						return nil
					}) {
						return ctx.Err()
					}
				}
				return nil
			}, func(context.Context, int) ([]company.MailIndexJob, error) { return jobs, nil }, func(ctx context.Context, job company.MailIndexJob) error {
				n := 1
				if job.MessageID == jobs[0].MessageID {
					n = 0
				}
				return failing(ctx, n)
			}, func(context.Context) (int, error) { return 20, nil }, nil)
			if !errors.Is(err, injected) || !errors.Is(err, secondary) || active.Load() != 0 || started.Load() != 20 || stats.MaxActive != 20 || stats.FinalEmptyClaim || stats.ProducerDone {
				t.Fatalf("pipeline failed cancel/join/double-error: stats=%+v active=%d started=%d err=%v", stats, active.Load(), started.Load(), err)
			}
		})
	}
}

func TestR5BenchmarkPipelineRejectsIncomplete(t *testing.T) {
	for _, fault := range []string{"short_producer", "long_producer", "early_empty", "oversized_claim", "final_nonempty", "wrong_ready", "ready_error", "claim_error", "progress_error", "cancelled"} {
		t.Run(fault, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if fault == "cancelled" {
				cancel()
			}
			claims := 0
			_, err := r5BenchPipeline(ctx, 20, func(ctx context.Context, enqueue func(func(context.Context) error) bool) error {
				n := 20
				if fault == "short_producer" {
					n--
				} else if fault == "long_producer" {
					n++
				}
				for i := 0; i < n; i++ {
					if !enqueue(func(context.Context) error { return nil }) {
						return ctx.Err()
					}
				}
				return nil
			}, func(context.Context, int) ([]company.MailIndexJob, error) {
				claims++
				if fault == "claim_error" {
					return nil, errors.New("claim failed")
				}
				if fault == "early_empty" || claims > 1 && fault != "final_nonempty" {
					return nil, nil
				}
				n := 20
				if fault == "oversized_claim" {
					n++
				}
				return make([]company.MailIndexJob, n), nil
			}, func(context.Context, company.MailIndexJob) error { return nil }, func(context.Context) (int, error) {
				if fault == "wrong_ready" {
					return 19, nil
				}
				if fault == "ready_error" {
					return 0, errors.New("ready SQL failed")
				}
				return 20, nil
			}, func(r5BenchPreparation) error {
				if fault == "progress_error" {
					return errors.New("checkpoint failed")
				}
				return nil
			})
			if err == nil {
				t.Fatal("incomplete pipeline silently accepted")
			}
		})
	}
}

func TestR5BenchmarkClockProvenance(t *testing.T) {
	start := time.Unix(1790840000, 0)
	wall, gap, e := r5BenchLatency(start, start.Add(20*time.Millisecond), 20*time.Millisecond)
	if e != nil || wall != 20 || gap != 0 {
		t.Fatalf("normal clock failed: wall=%f gap=%f error=%v", wall, gap, e)
	}
	for _, tc := range []struct {
		name string
		end  time.Time
		mono time.Duration
	}{
		{"suspend_gap", start.Add(time.Minute + 20*time.Millisecond), 20 * time.Millisecond},
		{"backward_wall", start.Add(-time.Second), time.Millisecond},
		{"backward_monotonic", start.Add(time.Millisecond), -time.Millisecond},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, e := r5BenchLatency(start, tc.end, tc.mono); e == nil {
				t.Fatal("clock anomaly silently accepted")
			}
		})
	}
	_, gap, e = r5BenchLatency(start, start.Add(120*time.Millisecond), 20*time.Millisecond)
	if e != nil || gap != 100 {
		t.Fatal("exact100ms bound changed")
	}
	t.Logf("TOOL_ONLY_CLOCK_PROVENANCE go_now_wall_unix_seconds=%f latency_clock=%s; noS_M_Lcapacity", float64(time.Now().UnixNano())/float64(time.Second), r5BenchLatencyClock)
}

func TestR5BenchmarkMeasurementDualErrorJoin(t *testing.T) {
	operationErr := context.DeadlineExceeded
	clockErr := fmt.Errorf("injected suspended clock gap")
	var actual atomic.Int32
	times, gap, errs := r5BenchSamples(func(int) (float64, float64, error) {
		actual.Add(1)
		return 60000, 60000, errors.Join(operationErr, clockErr)
	})
	if actual.Load() != 200 || len(times) != 200 || gap != 60000 || len(errs) != 200 {
		t.Fatalf("sample dual-error join lost/hung: calls=%d times=%d errors=%d gap=%f", actual.Load(), len(times), len(errs), gap)
	}
	for _, err := range errs {
		if !errors.Is(err, operationErr) || !errors.Is(err, clockErr) {
			t.Fatal("sample error discarded a real cause")
		}
	}
}
