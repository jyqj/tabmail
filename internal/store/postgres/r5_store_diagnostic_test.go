//go:build r5benchmark

package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"runtime/pprof"
	"runtime/trace"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"tabmail/internal/models"
	"tabmail/internal/rawobject"
	"tabmail/internal/store/fileobj"
)

const r5StoreDiagnosticMode = "DIAGNOSTIC_STORE_BREAKDOWN_NOT_S_M_TOOL_ADMISSION"
const r5StoreDiagnosticBudget = 180 * time.Second
const r5StoreDiagnosticTotalBudget = 300 * time.Second
const r5StoreDiagnosticDiskBudget uint64 = 1 << 30

type r5DiagnosticScopeKey struct{}
type r5DiagnosticQueryKey struct{}
type r5DiagnosticAcquireKey struct{}
type r5DiagnosticScope struct {
	ordinal int
	size    int64
	mailbox string
}
type r5DiagnosticQuery struct {
	start time.Time
	class string
	pid   uint32
}
type r5DiagnosticPID struct {
	scope r5DiagnosticScope
	class string
}

type r5DiagnosticSpan struct {
	Calls      int     `json:"calls"`
	Errors     int     `json:"errors"`
	ElapsedSum float64 `json:"elapsed_call_seconds_sum"`
	MinMS      float64 `json:"min_call_ms"`
	MaxMS      float64 `json:"max_call_ms"`
	MaxGapMS   float64 `json:"max_wall_monotonic_gap_ms"`
}
type r5DiagnosticWait struct {
	ElapsedSeconds float64 `json:"elapsed_wall_seconds"`
	PID            int32   `json:"owned_backend_pid"`
	Ordinal        int     `json:"ordinal"`
	Size           int64   `json:"raw_size_bucket"`
	Mailbox        string  `json:"mailbox_sha256"`
	Class          string  `json:"client_sql_phase"`
	State          string  `json:"state"`
	WaitType       string  `json:"wait_event_type"`
	Wait           string  `json:"wait_event"`
	Blockers       []int32 `json:"blocking_pids"`
}

// Every wrapper delegates the unchanged production port exactly once. No SQL
// arguments, raw bytes, addresses, DSNs or credentials enter diagnostic output.
type r5StoreDiagnostic struct {
	mu                                         sync.Mutex
	started                                    time.Time
	totalStarted                               time.Time
	dir, source, database                      string
	counter                                    *r5SQLCounter
	state                                      *r5BenchState
	admin                                      *pgxpool.Pool
	cancel                                     context.CancelFunc
	spans                                      map[string]*r5DiagnosticSpan
	queries                                    map[string]map[string]bool
	pids                                       map[uint32]r5DiagnosticPID
	waits                                      []r5DiagnosticWait
	failures                                   []error
	samplerQueries                             int
	rssMax, diskMax                            uint64
	stopOnce, cleanupOnce                      sync.Once
	watchCancel                                context.CancelFunc
	watchDone, profileDone                     chan struct{}
	profileCancel                              context.CancelFunc
	cleanupResult                              map[string]any
	cleanupErr                                 error
	profileStarted, profileStopped             time.Time
	sampleTimes                                []float64
	profilesClosed                             bool
	lookaheadEnabled                           bool
	lookahead                                  *r5Lookahead
	causeEnabled                               bool
	executionCtx                               context.Context
	storeObserverOnce                          sync.Once
	observerNanos, samplerNanos, resourceNanos atomic.Int64
}

func newR5StoreDiagnostic(start time.Time, dir, source string, cancel context.CancelFunc) *r5StoreDiagnostic {
	return &r5StoreDiagnostic{started: start, totalStarted: start, dir: dir, source: source, cancel: cancel, spans: map[string]*r5DiagnosticSpan{}, queries: map[string]map[string]bool{}, pids: map[uint32]r5DiagnosticPID{}}
}
func (d *r5StoreDiagnostic) fail(err error) {
	if err == nil {
		return
	}
	d.mu.Lock()
	d.failures = append(d.failures, err)
	d.mu.Unlock()
	if d.cancel != nil {
		d.cancel()
	}
}
func (d *r5StoreDiagnostic) scope(ctx context.Context, ordinal int, size int64, box uuid.UUID) context.Context {
	if d.causeEnabled {
		d.storeObserverOnce.Do(func() { d.fail(d.startObservers(d.executionCtx)) })
	}
	h := sha256.Sum256(box[:])
	return context.WithValue(ctx, r5DiagnosticScopeKey{}, r5DiagnosticScope{ordinal, size, hex.EncodeToString(h[:])})
}
func r5DiagnosticScoped(ctx context.Context) bool {
	_, ok := ctx.Value(r5DiagnosticScopeKey{}).(r5DiagnosticScope)
	return ok
}
func (d *r5StoreDiagnostic) record(ctx context.Context, name string, start time.Time, operationErr error) {
	d.recordInterval(ctx, name, start, time.Now(), operationErr)
}
func (d *r5StoreDiagnostic) recordInterval(ctx context.Context, name string, start, end time.Time, operationErr error) {
	if d.causeEnabled {
		began := time.Now()
		defer func() { d.observerNanos.Add(time.Now().UnixNano() - began.UnixNano()) }()
	}
	if !r5DiagnosticScoped(ctx) {
		return
	}
	ms, gap, clockErr := r5BenchLatency(start, end, end.Sub(start))
	if clockErr != nil || math.IsNaN(ms) || math.IsInf(ms, 0) || ms < 0 {
		d.fail(errors.Join(clockErr, errors.New("invalid diagnostic span clock")))
		return
	}
	d.mu.Lock()
	v := d.spans[name]
	if v == nil {
		v = &r5DiagnosticSpan{MinMS: ms}
		d.spans[name] = v
	}
	v.Calls++
	if operationErr != nil {
		v.Errors++
	}
	v.ElapsedSum += ms / 1000
	v.MinMS = min(v.MinMS, ms)
	v.MaxMS = max(v.MaxMS, ms)
	v.MaxGapMS = max(v.MaxGapMS, gap)
	d.mu.Unlock()
}

type r5DiagnosticBlob struct {
	rawobject.BlobStore
	diagnostic *r5StoreDiagnostic
}

func (b *r5DiagnosticBlob) Exists(ctx context.Context, key string) (bool, error) {
	start := time.Now()
	v, e := b.BlobStore.Exists(ctx, key)
	b.diagnostic.record(ctx, "blob_exists", start, e)
	return v, e
}
func (b *r5DiagnosticBlob) Put(ctx context.Context, key string, r io.Reader, n int64) error {
	if b.diagnostic.causeEnabled {
		ctx = fileobj.WithR5DiagnosticObserver(ctx, b.diagnostic)
	}
	start := time.Now()
	e := b.BlobStore.Put(ctx, key, r, n)
	b.diagnostic.record(ctx, "blob_put_including_original_durability", start, e)
	return e
}

type r5DiagnosticRefs struct {
	rawobject.ReferenceStore
	diagnostic *r5StoreDiagnostic
}

func (r *r5DiagnosticRefs) CreateMessageWithQuota(ctx context.Context, m *models.Message, maxMessages int, ensure func(context.Context) error) (bool, error) {
	start := time.Now()
	wrapped := ensure
	if ensure != nil {
		wrapped = func(ctx context.Context) error {
			s := time.Now()
			err := ensure(ctx)
			r.diagnostic.record(ctx, "ensure", s, err)
			return err
		}
	}
	v, e := r.ReferenceStore.CreateMessageWithQuota(ctx, m, maxMessages, wrapped)
	r.diagnostic.record(ctx, "reference_transaction_including_ensure", start, e)
	return v, e
}

func r5DiagnosticSQLClass(sql string) string {
	u := strings.Join(strings.Fields(strings.ToUpper(sql)), " ")
	compact := strings.ReplaceAll(u, " ", "")
	switch {
	case u == "BEGIN":
		return "begin"
	case u == "COMMIT":
		return "commit"
	case u == "ROLLBACK":
		return "rollback"
	case strings.HasPrefix(u, "SELECT PG_ADVISORY_XACT_LOCK("):
		return "advisory_raw_key"
	case strings.HasPrefix(compact, "UPDATEMAILBOXESSETMESSAGE_COUNT=MESSAGE_COUNT+1"):
		return "quota_mailbox_update"
	case strings.HasPrefix(u, "INSERT INTO MESSAGES ("):
		return "insert_message_including_trigger"
	case strings.HasPrefix(compact, "UPDATEMESSAGESSETARCHIVED_AT="):
		return "life_archived"
	case strings.HasPrefix(compact, "UPDATEMESSAGESSETDELETED_AT="):
		return "life_trash_expired"
	case strings.HasPrefix(compact, "UPDATEMESSAGESSETEXPIRES_AT="):
		return "life_hard_expired"
	default:
		return "unknown_store_sql"
	}
}
func (d *r5StoreDiagnostic) TraceQueryStart(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	ctx = d.counter.TraceQueryStart(ctx, conn, data)
	scope, ok := ctx.Value(r5DiagnosticScopeKey{}).(r5DiagnosticScope)
	if !ok {
		return ctx
	}
	class := r5DiagnosticSQLClass(data.SQL)
	if class == "unknown_store_sql" {
		d.fail(errors.New("unclassified SQL in diagnostic store scope"))
	}
	h := sha256.Sum256([]byte(data.SQL))
	digest := hex.EncodeToString(h[:])
	pid := conn.PgConn().PID()
	d.mu.Lock()
	if d.queries[class] == nil {
		d.queries[class] = map[string]bool{}
	}
	d.queries[class][digest] = true
	d.pids[pid] = r5DiagnosticPID{scope, class}
	d.mu.Unlock()
	return context.WithValue(ctx, r5DiagnosticQueryKey{}, r5DiagnosticQuery{time.Now(), class, pid})
}
func (d *r5StoreDiagnostic) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	v, ok := ctx.Value(r5DiagnosticQueryKey{}).(r5DiagnosticQuery)
	if !ok {
		return
	}
	d.record(ctx, "sql/"+v.class, v.start, data.Err)
	d.mu.Lock()
	if pid, ok := d.pids[v.pid]; ok {
		pid.class = "after/" + v.class
		d.pids[v.pid] = pid
	}
	d.mu.Unlock()
}
func (d *r5StoreDiagnostic) TraceAcquireStart(ctx context.Context, _ *pgxpool.Pool, _ pgxpool.TraceAcquireStartData) context.Context {
	if !r5DiagnosticScoped(ctx) {
		return ctx
	}
	return context.WithValue(ctx, r5DiagnosticAcquireKey{}, time.Now())
}
func (d *r5StoreDiagnostic) TraceAcquireEnd(ctx context.Context, _ *pgxpool.Pool, data pgxpool.TraceAcquireEndData) {
	start, ok := ctx.Value(r5DiagnosticAcquireKey{}).(time.Time)
	if !ok {
		return
	}
	d.record(ctx, "pool_acquire", start, data.Err)
	if data.Conn != nil {
		scope := ctx.Value(r5DiagnosticScopeKey{}).(r5DiagnosticScope)
		d.mu.Lock()
		d.pids[data.Conn.PgConn().PID()] = r5DiagnosticPID{scope, "before/begin_or_lifecycle"}
		d.mu.Unlock()
	}
}
func (d *r5StoreDiagnostic) TraceRelease(_ *pgxpool.Pool, data pgxpool.TraceReleaseData) {
	d.mu.Lock()
	delete(d.pids, data.Conn.PgConn().PID())
	d.mu.Unlock()
}

func (d *r5StoreDiagnostic) resources(ctx context.Context) error {
	if d.causeEnabled {
		start := time.Now()
		defer func() { d.resourceNanos.Add(time.Now().UnixNano() - start.UnixNano()) }()
	}
	raw, e := r5BenchDisk(d.state.objectRoot)
	if e != nil {
		return e
	}
	profiles, e := r5BenchDisk(d.dir)
	if e != nil {
		return e
	}
	rss, e := r5BenchRSS()
	if e != nil || rss == 0 {
		return errors.Join(e, errors.New("unknown diagnostic RSS"))
	}
	var db int64
	if e = d.state.pool.QueryRow(ctx, `SELECT pg_database_size(current_database())`).Scan(&db); e != nil {
		return e
	}
	if db <= 0 {
		return errors.New("unknown owned database disk bytes")
	}
	disk := raw + profiles + uint64(db)
	d.mu.Lock()
	d.rssMax = max(d.rssMax, rss)
	d.diskMax = max(d.diskMax, disk)
	d.mu.Unlock()
	if disk > r5StoreDiagnosticDiskBudget {
		return errors.New("diagnostic owned objects/database/profiles exceeded 1GiB")
	}
	return nil
}
func (d *r5StoreDiagnostic) sample(ctx context.Context) error {
	if d.causeEnabled {
		start := time.Now()
		defer func() { d.samplerNanos.Add(time.Now().UnixNano() - start.UnixNano()) }()
	}
	rows, e := d.state.pool.Query(ctx, `SELECT pid,state,COALESCE(wait_event_type,''),COALESCE(wait_event,''),pg_blocking_pids(pid) FROM pg_stat_activity WHERE datname=current_database() AND pid<>pg_backend_pid()`)
	if e != nil {
		return e
	}
	defer rows.Close()
	d.mu.Lock()
	d.samplerQueries++
	d.sampleTimes = append(d.sampleTimes, float64(time.Now().UnixNano()-d.started.UnixNano())/float64(time.Second))
	d.mu.Unlock()
	for rows.Next() {
		v := r5DiagnosticWait{ElapsedSeconds: float64(time.Now().UnixNano()-d.started.UnixNano()) / float64(time.Second)}
		if e = rows.Scan(&v.PID, &v.State, &v.WaitType, &v.Wait, &v.Blockers); e != nil {
			return e
		}
		d.mu.Lock()
		p, ok := d.pids[uint32(v.PID)]
		if ok {
			v.Ordinal, v.Size, v.Mailbox, v.Class = p.scope.ordinal, p.scope.size, p.scope.mailbox, p.class
			if v.Blockers == nil {
				v.Blockers = []int32{}
			}
			d.waits = append(d.waits, v)
		}
		d.mu.Unlock()
	}
	return rows.Err()
}
func (d *r5StoreDiagnostic) startObservers(ctx context.Context) error {
	d.profileStarted = time.Now()
	traceFile, e := os.OpenFile(filepath.Join(d.dir, "runtime.trace"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	cpuFile, e := os.OpenFile(filepath.Join(d.dir, "cpu.pprof"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		traceFile.Close()
		return e
	}
	if e = trace.Start(traceFile); e != nil {
		traceFile.Close()
		cpuFile.Close()
		return e
	}
	if e = pprof.StartCPUProfile(cpuFile); e != nil {
		trace.Stop()
		d.profileStopped = time.Now()
		traceFile.Close()
		cpuFile.Close()
		return e
	}
	profileCtx, cancel := context.WithCancel(ctx)
	d.profileCancel = cancel
	d.profileDone = make(chan struct{})
	go func() {
		defer close(d.profileDone)
		timer := time.NewTimer(20 * time.Second)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-profileCtx.Done():
		}
		pprof.StopCPUProfile()
		trace.Stop()
		closeErr := errors.Join(cpuFile.Close(), traceFile.Close())
		d.profileStopped = time.Now()
		d.profilesClosed = closeErr == nil
		d.fail(closeErr)
	}()
	watchCtx, cancel := context.WithCancel(ctx)
	d.watchCancel = cancel
	d.watchDone = make(chan struct{})
	go func() {
		defer close(d.watchDone)
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		nextResource := time.Now()
		for {
			select {
			case <-watchCtx.Done():
				return
			case <-ticker.C:
				if time.Now().UnixNano()-d.started.UnixNano() >= int64(r5StoreDiagnosticBudget) || time.Now().UnixNano()-d.totalStarted.UnixNano() >= int64(r5StoreDiagnosticTotalBudget) {
					d.fail(context.DeadlineExceeded)
					return
				}
				if e := d.sample(watchCtx); e != nil {
					if watchCtx.Err() == nil {
						d.fail(e)
					}
					return
				}
				if !time.Now().Before(nextResource) {
					if e := d.resources(watchCtx); e != nil {
						if watchCtx.Err() == nil {
							d.fail(e)
						}
						return
					}
					nextResource = time.Now().Add(time.Second)
				}
			}
		}
	}()
	return nil
}
func (d *r5StoreDiagnostic) stopObservers() {
	d.stopOnce.Do(func() {
		if d.watchCancel != nil {
			d.watchCancel()
			<-d.watchDone
		}
		if d.profileCancel != nil {
			d.profileCancel()
			<-d.profileDone
		}
	})
}
func r5DiagnosticJSON(path string, value any) error {
	b, e := json.MarshalIndent(value, "", "  ")
	if e != nil {
		return e
	}
	f, e := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	_, writeErr := f.Write(append(b, '\n'))
	return errors.Join(writeErr, f.Close())
}
func (d *r5StoreDiagnostic) cleanup() (map[string]any, error) {
	d.cleanupOnce.Do(func() {
		d.stopObservers()
		d.state.pool.Close()
		ctx, cancel := context.WithDeadline(context.Background(), time.Unix(0, d.totalStarted.UnixNano()+int64(r5StoreDiagnosticTotalBudget)))
		defer cancel()
		_, dropErr := d.admin.Exec(ctx, "DROP DATABASE "+pgx.Identifier{d.database}.Sanitize()+" WITH (FORCE)")
		var remaining int
		verifyErr := d.admin.QueryRow(ctx, `SELECT count(*) FROM pg_database WHERE datname=$1`, d.database).Scan(&remaining)
		removeErr := os.RemoveAll(d.state.objectRoot)
		_, statErr := os.Lstat(d.state.objectRoot)
		elapsed := float64(time.Now().UnixNano()-d.totalStarted.UnixNano()) / float64(time.Second)
		d.cleanupErr = errors.Join(dropErr, verifyErr, removeErr)
		if remaining != 0 || !os.IsNotExist(statErr) || elapsed < 0 || elapsed > 300 {
			d.cleanupErr = errors.Join(d.cleanupErr, errors.New("diagnostic cleanup absent-resource/absolute300s proof failed"))
		}
		if !d.profilesClosed {
			d.cleanupErr = errors.Join(d.cleanupErr, errors.New("diagnostic profile closure not observed"))
		}
		d.cleanupResult = map[string]any{"owned_database_absent": verifyErr == nil && remaining == 0, "owned_object_root_absent": os.IsNotExist(statErr), "watcher_joined": d.watchDone != nil, "profiles_stopped_and_closed": d.profilesClosed, "elapsed_wall_seconds_including_init_execution_cleanup": elapsed, "budget_seconds": 300, "complete": d.cleanupErr == nil}
		d.cleanupErr = errors.Join(d.cleanupErr, r5DiagnosticJSON(filepath.Join(d.dir, "cleanup.json"), d.cleanupResult))
	})
	return d.cleanupResult, d.cleanupErr
}
func r5DiagnosticValidate(spans map[string]*r5DiagnosticSpan) error {
	expected := map[string]int{"store": 4000, "reference_transaction_including_ensure": 4000, "ensure": 4000, "blob_exists": 4000, "blob_put_including_original_durability": 4000, "pool_acquire": 4800, "lifecycle/archived": 400, "lifecycle/trash_expired": 200, "lifecycle/hard_expired": 200, "sql/begin": 4000, "sql/advisory_raw_key": 4000, "sql/quota_mailbox_update": 4000, "sql/insert_message_including_trigger": 4000, "sql/commit": 4000, "sql/life_archived": 400, "sql/life_trash_expired": 200, "sql/life_hard_expired": 200}
	if len(spans) != len(expected) {
		return errors.New("missing/unexpected diagnostic span class")
	}
	for class, count := range expected {
		v := spans[class]
		if v == nil || v.Calls != count || v.Errors != 0 || v.ElapsedSum <= 0 || math.IsNaN(v.ElapsedSum) || math.IsInf(v.ElapsedSum, 0) || math.IsNaN(v.MinMS) || math.IsInf(v.MinMS, 0) || math.IsNaN(v.MaxMS) || math.IsInf(v.MaxMS, 0) || math.IsNaN(v.MaxGapMS) || math.IsInf(v.MaxGapMS, 0) || v.MinMS < 0 || v.MaxMS < v.MinMS || v.MaxGapMS < 0 || v.MaxGapMS > 100 {
			return fmt.Errorf("unknown/incomplete diagnostic span class %s", class)
		}
	}
	return nil
}

// This separate entry never runs the shipping workload matrix or a performance
// admission validator, and never writes the benchmark registry. Population is
// a fixed private diagnosis input, not S/M/L or the 1000-row tool admission.
func TestR5StoreDiagnostic(t *testing.T) {
	r5RunStoreDiagnostic(t, false)
}

func TestR5StoreDiagnosticLookaheadB(t *testing.T) {
	r5RunStoreDiagnostic(t, true)
}

func r5RunStoreDiagnostic(t *testing.T, lookahead bool) {
	r5RunStoreDiagnosticCause(t, lookahead, false)
}

func r5RunStoreDiagnosticCause(t *testing.T, lookahead, cause bool) {
	started := time.Now()
	if os.Getenv("TABMAIL_R5_STORE_DIAGNOSTIC_APPROVED") != "1" || os.Getenv("GOMAXPROCS") != "2" || runtime.GOMAXPROCS(0) != 2 {
		t.Fatal("explicit diagnostic-only approval/GOMAXPROCS2 required")
	}
	var baseline *r5DiagnosticBaseline
	if lookahead && !cause {
		if os.Getenv("TABMAIL_R5_STORE_DIAGNOSTIC_LOOKAHEAD_APPROVED") != "1" {
			t.Fatal("separate conditional B approval required")
		}
		var e error
		baseline, e = r5LoadDiagnosticA(os.Getenv("TABMAIL_R5_STORE_DIAGNOSTIC_A_RESULT"))
		r5BenchCheck(t, e)
	}
	totalStart, e := strconv.ParseInt(os.Getenv("TABMAIL_R5_STORE_DIAGNOSTIC_TOTAL_STARTED_UNIX_NANO"), 10, 64)
	if e != nil || totalStart <= 0 || totalStart > started.UnixNano() || started.UnixNano()-totalStart >= int64(r5StoreDiagnosticTotalBudget) {
		t.Fatal("original owner wall origin before native PG/bootstrap required; no new300s allowance")
	}
	dir, source := os.Getenv("TABMAIL_R5_STORE_DIAGNOSTIC_DIR"), os.Getenv("TABMAIL_R5_BENCHMARK_SOURCE_SHA")
	if !filepath.IsAbs(dir) || !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(source) {
		t.Fatal("fresh absolute private directory and frozen source identity required")
	}
	if e := os.Mkdir(dir, 0700); e != nil {
		t.Fatal(e)
	}
	executionDeadline := time.Unix(0, min(started.UnixNano()+int64(r5StoreDiagnosticBudget), totalStart+int64(r5StoreDiagnosticTotalBudget)))
	ctx, cancel := context.WithDeadline(context.Background(), executionDeadline)
	defer cancel()
	d := newR5StoreDiagnostic(started, dir, source, cancel)
	d.totalStarted = time.Unix(0, totalStart)
	d.lookaheadEnabled = lookahead
	d.causeEnabled, d.executionCtx = cause, ctx
	size := [3]int{20, 100, 4000}
	if cause {
		if os.Getenv("TABMAIL_R5_STORE_CAUSE_APPROVED") != "1" {
			t.Fatal("separate M-shaped cause diagnostic approval required")
		}
		size = [3]int{1000, 5000, 4000}
	}
	state := r5BenchOwnedDiagnostic(t, ctx, d)
	t.Cleanup(func() {
		if cause && t.Failed() {
			d.stopObservers()
			if e := d.preserveCauseFailure(); e != nil {
				t.Errorf("cause failure evidence capture: %v", e)
			}
		}
		_, e := d.cleanup()
		if e != nil {
			t.Errorf("diagnostic cleanup: %v", e)
		}
	})
	var schema int64
	r5BenchCheck(t, state.pool.QueryRow(ctx, `SELECT max(version_id) FROM goose_db_version WHERE is_applied`).Scan(&schema))
	if schema != 16 {
		t.Fatal("diagnostic requires actual schema16 before any population run")
	}
	uidColumns := 0
	if lookahead {
		r5BenchCheck(t, state.pool.QueryRow(ctx, `SELECT count(*) FROM pg_attribute WHERE attrelid='messages'::regclass AND NOT attisdropped AND attnum>0 AND lower(attname) IN ('uid','imap_uid','message_uid')`).Scan(&uidColumns))
	}
	if !cause {
		r5BenchCheck(t, d.startObservers(ctx))
	}
	state.seed(t, size)
	distribution, fingerprint := state.observations(t, size)
	if lookahead && !cause {
		r5BenchCheck(t, r5CompareDiagnosticStreams(baseline, state.fingerprintStreams, fingerprint, r5BenchParameterFingerprint([3]int{20, 100, 4000})))
	}
	d.stopObservers()
	r5BenchCheck(t, d.resources(ctx))
	var fsync, synchronous, fullPage string
	r5BenchCheck(t, state.pool.QueryRow(ctx, `SELECT current_setting('fsync'),current_setting('synchronous_commit'),current_setting('full_page_writes')`).Scan(&fsync, &synchronous, &fullPage))
	if fsync != "on" || synchronous != "on" || fullPage != "on" {
		t.Fatal("diagnostic refuses changed durability settings")
	}
	r5BenchCheck(t, state.pool.QueryRow(ctx, `SELECT max(version_id) FROM goose_db_version WHERE is_applied`).Scan(&schema))
	if schema != 16 {
		t.Fatal("diagnostic requires actual schema16")
	}
	d.mu.Lock()
	diagnosticErr := errors.Join(d.failures...)
	if len(d.pids) != 0 {
		diagnosticErr = errors.Join(diagnosticErr, errors.New("scoped pool acquire/release was not joined"))
	}
	d.mu.Unlock()
	r5BenchCheck(t, diagnosticErr)
	if cause {
		r5BenchCheck(t, r5CauseValidate(d.spans))
	} else {
		r5BenchCheck(t, r5DiagnosticValidate(d.spans))
	}
	if d.samplerQueries == 0 || len(d.waits) == 0 || d.rssMax == 0 || d.diskMax == 0 || state.pool.Stat().MaxConns() != 24 {
		t.Fatal("unknown diagnostic sampler/resource/pool observations")
	}
	if len(d.sampleTimes) != d.samplerQueries || !d.profilesClosed || d.profileStarted.IsZero() || d.profileStopped.IsZero() {
		t.Fatal("unknown sampler/profile observation")
	}
	for i, v := range d.sampleTimes {
		if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || i > 0 && v < d.sampleTimes[i-1] {
			t.Fatal("invalid observed sampler wall timeline")
		}
	}
	profileFiles := map[string]int64{}
	for _, name := range []string{"runtime.trace", "cpu.pprof"} {
		info, e := os.Lstat(filepath.Join(dir, name))
		r5BenchCheck(t, e)
		if !info.Mode().IsRegular() || info.Size() <= 0 {
			t.Fatal("unknown profile artifact")
		}
		profileFiles[name] = info.Size()
	}
	p := state.preparation
	if p.MaxActive > 20 || p.MaxWindowMessages != 20 || p.PendingUpperBound != 20 || p.JoinedInflight != 0 || !p.ProducerDone || !p.FinalEmptyClaim || p.SQLReady != 4000 || p.ParserJoinedInflight != 0 || !p.ParserQuiescent {
		t.Fatal("diagnostic terminal bound/join/source-ready proof failed")
	}
	ended := time.Now()
	elapsed := float64(ended.UnixNano()-started.UnixNano()) / float64(time.Second)
	_, executionGap, clockErr := r5BenchLatency(started, ended, ended.Sub(started))
	r5BenchCheck(t, clockErr)
	if elapsed < 0 || elapsed > 180 {
		t.Fatal("diagnostic original absolute180s execution budget exceeded")
	}
	cleanup, e := d.cleanup()
	r5BenchCheck(t, e)
	queryDigests := map[string][]string{}
	for class, digests := range d.queries {
		for digest := range digests {
			queryDigests[class] = append(queryDigests[class], digest)
		}
		sort.Strings(queryDigests[class])
	}
	result := map[string]any{"mode": r5StoreDiagnosticMode, "S_M_L_executed": false, "tool_admission": false, "registry_written": false, "task_complete": false, "product_green": false, "source_sha": source, "schema_version": schema, "population": []int{20, 100, 4000}, "seed": r5BenchmarkSeed, "concurrency": 20, "pool_size": 24, "gomaxprocs": 2, "method": "unchanged_bounded_store_claim_complete_window_v1_optional_observer", "maintenance": "no_explicit_analyze_or_planner_or_durability_override", "execution_wall": map[string]any{"started_unix_seconds": float64(started.UnixNano()) / float64(time.Second), "completed_unix_seconds": float64(ended.UnixNano()) / float64(time.Second), "elapsed_wall_seconds": elapsed, "budget_seconds": 180}, "resource_scope": "owned_raw_objects_plus_owned_pg_database_plus_private_diagnostic_files; process_high_water_rss_not_system_memory", "rss_max_bytes": d.rssMax, "disk_max_bytes": d.diskMax, "disk_budget_bytes": r5StoreDiagnosticDiskBudget, "span_scope": "overlapping_call_elapsed_sums_are_not_stage_wall_or_server_trigger_statement_timings", "call_spans": d.spans, "sql_query_sha256_by_class": queryDigests, "sql_args_recorded": false, "sampler_scope": "same_pool_readonly_owned_database_pid_state_wait_event_blocking_pids_no_query_text", "sampling_target_interval_ms": 100, "sampler_queries": d.samplerQueries, "store_state_samples": d.waits, "profile_scope": "first_at_most20s_of_seed_including_company_bootstrap; process_CPU_and_runtime_syscall_sync_sched_trace_not_entire_run_IO_metrics", "preparation": state.preparation, "preparation_checkpoints": state.preparationCheckpoints, "actual_distribution": distribution, "dataset_fingerprint": fingerprint, "fingerprint_method": "six_actual_canonical_sql_streams_v1", "fingerprint_streams": state.fingerprintStreams, "cleanup": cleanup}
	result["gomaxprocs"] = runtime.GOMAXPROCS(0)
	result["source_identity_kind"] = "canonical_source_closure_sha1_not_git_commit_or_tree"
	result["outer_total_started_unix_seconds"] = float64(totalStart) / float64(time.Second)
	result["outer_total_budget_seconds"] = 300
	result["execution_absolute_deadline_unix_seconds"] = float64(executionDeadline.UnixNano()) / float64(time.Second)
	result["native_pg_stop_scope"] = "owner_must_stop_owned_native_server_and_preserve_final_receipt_before_original_outer300s; Go only proves_owned_db_objects_observers_cleanup"
	result["execution_wall_monotonic_gap_ms"] = executionGap
	result["profile_files_bytes"] = profileFiles
	result["parameter_fingerprint"] = r5BenchParameterFingerprint(size)
	result["pool_scope"] = "application_and_sampler_share_max24; admin_setup_cleanup_pool_excluded"
	result["durability_observation"] = map[string]string{"fsync": fsync, "synchronous_commit": synchronous, "full_page_writes": fullPage}
	result["sampling_observed_elapsed_wall_seconds"] = d.sampleTimes
	result["profile_scope"] = "first_seed_window_target20s_including_company_bootstrap; synchronous_stop_flush_in_actual_wall; syscall_sync_sched_and_CPU_not_full_run_IO"
	result["profile_wall"] = map[string]any{"started_unix_seconds": float64(d.profileStarted.UnixNano()) / float64(time.Second), "closed_unix_seconds": float64(d.profileStopped.UnixNano()) / float64(time.Second), "elapsed_wall_seconds": float64(d.profileStopped.UnixNano()-d.profileStarted.UnixNano()) / float64(time.Second), "target_seconds": 20}
	if lookahead && !cause {
		stats := d.lookahead.snapshot()
		if stats.Generated != 4000 || stats.Emitted != 4000 || stats.FIFOCompleted != 4000 || stats.MaxBuffered > 100 || stats.JoinedInflight != 0 {
			t.Fatal("B scheduling FIFO/buffer/join observation incomplete")
		}
		result["mode"] = "DIAGNOSTIC_STORE_LOOKAHEAD100_B_NOT_S_M_TOOL_ADMISSION"
		result["method"] = "lookahead100_unpersisted_inputs_distinct_mailbox_windows20_ordinal_fifo_v1"
		result["lookahead"] = stats
		result["comparison_A"] = map[string]any{"source_sha": baseline.Source, "raw_result_sha256": r5DiagnosticARawSHA, "dataset_fingerprint_equal": true, "all_six_streams_equal": true, "A_reexecuted": false}
		result["identity_boundary"] = map[string]any{"generator_ordinal_mailbox_fifo_preserved": true, "A_random_commit_order_not_preserved_or_claimed": true, "all_persistent_fields_equal": false, "canonical_streams_unchanged_no_added_normalization": true, "uncovered_fields": "received_at/received-order, lifecycle clock timestamps, indexed_at, next_attempt_at, lease tokens; random actor/mailbox row UUIDs already excluded by existing canonical streams", "protocol_UID_equivalence": "not_certified_by_six_streams; no UID expression in these message Store INSERT fields", "uid_column_observation": uidColumns}
		result["identity_boundary"].(map[string]any)["received_at_and_received_order_may_change_with_B_scheduling"] = true
		result["identity_boundary"].(map[string]any)["all_physical_uuid_time_protocol_uid_fields_not_covered_by_six_streams"] = true
	}
	if cause {
		result["mode"] = "DIAGNOSTIC_M_SHAPED_STORE_CAUSE_01_NOT_M_BASELINE"
		result["population"] = []int{1000, 5000, 4000}
		result["method"] = "unchanged_lookahead100_real_store_exact_fs_observer_only_v1"
		result["lookahead"] = d.lookahead.snapshot()
		result["cause_boundaries"] = map[string]any{"fs_call_wall_includes_goroutine_rescheduling": true, "cross_goroutine_and_nested_elapsed_sums_are_not_stage_wall": true, "syscall_trace_vs_runnable_scheduling_delays_separate": true, "late_753k_depth_behavior_not_certified_by_fresh4k": true, "WAL_device_IO_not_certified_by_commit_client_span": true, "operation_count_order_durability_error_policy_unchanged": true}
		result["observer_overhead"] = map[string]any{"record_callback_wall_seconds_sum": float64(d.observerNanos.Load()) / float64(time.Second), "sampler_call_wall_seconds_sum": float64(d.samplerNanos.Load()) / float64(time.Second), "resource_walk_query_wall_seconds_sum": float64(d.resourceNanos.Load()) / float64(time.Second), "scope": "actual observer callback/query elapsed sums overlap workloads, not added wall, not total instrumentation CPU"}
		result["profile_scope"] = "first_actual_Store_window_target20s_after_company_bootstrap; stop_flush_counted_in_original_wall"
	}
	r5BenchCheck(t, r5DiagnosticJSON(filepath.Join(dir, "diagnostic.json"), result))
	t.Log("DIAGNOSTIC_STORE_BREAKDOWN complete actual4000ready/real ports/SQL-FS-call spans/owned waits/resources/join/cleanup; noS_M_L_tool_admission")
}

const r5DiagnosticASource = "b45b043d822ddabb536f0a2321a1ce0913ae7137"

// Approved original diagnostic/diagnostic.json bytes, not go-run/go.jsonl.
const r5DiagnosticARawSHA = "b6e2d421424aac265106d2cd111239686c987e25ac639eba9f06c457c25b229e"

type r5DiagnosticStream struct {
	ID   int    `json:"stream_id"`
	Rows int    `json:"row_count"`
	SHA  string `json:"sha256"`
}
type r5DiagnosticBaseline struct {
	Source      string               `json:"source_sha"`
	Mode        string               `json:"mode"`
	Fingerprint string               `json:"dataset_fingerprint"`
	Parameters  string               `json:"parameter_fingerprint"`
	Streams     []r5DiagnosticStream `json:"fingerprint_streams"`
}

func r5LoadDiagnosticA(path string) (*r5DiagnosticBaseline, error) {
	if !filepath.IsAbs(path) {
		return nil, errors.New("approved immutable A absolute result path required")
	}
	info, e := os.Lstat(path)
	if e != nil {
		return nil, e
	}
	if !info.Mode().IsRegular() || info.Size() > 32<<20 {
		return nil, errors.New("invalid approved A file")
	}
	b, e := os.ReadFile(path)
	if e != nil {
		return nil, e
	}
	h := sha256.Sum256(b)
	if hex.EncodeToString(h[:]) != r5DiagnosticARawSHA {
		return nil, errors.New("A original result bytes do not match approved raw evidence; no normalization allowed")
	}
	var a r5DiagnosticBaseline
	if e = json.Unmarshal(b, &a); e != nil {
		return nil, e
	}
	if a.Source != r5DiagnosticASource || a.Mode != r5StoreDiagnosticMode || len(a.Streams) != 6 {
		return nil, errors.New("wrong original A source/mode/streams")
	}
	return &a, nil
}
func r5CompareDiagnosticStreams(a *r5DiagnosticBaseline, streams []map[string]any, fingerprint, parameters string) error {
	if a == nil || fingerprint != a.Fingerprint || parameters != a.Parameters || len(streams) != 6 || len(a.Streams) != 6 {
		return errors.New("B dataset/parameter fingerprint differs from approved A")
	}
	for i, s := range streams {
		if a.Streams[i].ID != i || s["stream_id"] != i || s["row_count"] != a.Streams[i].Rows || s["sha256"] != a.Streams[i].SHA {
			return fmt.Errorf("B actual canonical stream%d differs from approved A; reject immediately", i)
		}
	}
	return nil
}

type r5LookaheadItem struct {
	ordinal    int
	box        uuid.UUID
	job        func(context.Context) error
	boxOrdinal int
}
type r5LookaheadStats struct {
	Generated          int     `json:"generated_inputs"`
	Emitted            int     `json:"emitted_inputs"`
	MaxBuffered        int     `json:"max_unpersisted_buffered_inputs"`
	FIFOCompleted      int64   `json:"ordinal_fifo_completed_jobs"`
	JoinedInflight     int64   `json:"joined_fifo_inflight"`
	GateWaitSecondsSum float64 `json:"fifo_gate_call_elapsed_seconds_sum"`
	WindowUnique       []int   `json:"actual_emitted_window_unique_mailboxes"`
	EmittedOrdinals    []int   `json:"actual_emitted_ordinal_order"`
	EmittedMailboxes   []int   `json:"actual_emitted_mailbox_ordinals"`
}
type r5Lookahead struct {
	ctx                            context.Context
	enqueue                        func(func(context.Context) error) bool
	buffer                         []r5LookaheadItem
	tails                          map[uuid.UUID]*r5LookaheadGate
	stats                          r5LookaheadStats
	err                            error
	inflight, completed, waitNanos atomic.Int64
	window                         map[uuid.UUID]bool
}
type r5LookaheadGate struct {
	done chan struct{}
	err  error
}

func newR5Lookahead(ctx context.Context, enqueue func(func(context.Context) error) bool) *r5Lookahead {
	return &r5Lookahead{ctx: ctx, enqueue: enqueue, buffer: make([]r5LookaheadItem, 0, 100), tails: map[uuid.UUID]*r5LookaheadGate{}, window: map[uuid.UUID]bool{}}
}
func (l *r5Lookahead) add(n int, box uuid.UUID, job func(context.Context) error) bool {
	return l.addMapped(n, box, 0, job)
}
func (l *r5Lookahead) addMapped(n int, box uuid.UUID, boxOrdinal int, job func(context.Context) error) bool {
	if l.err != nil {
		return false
	}
	if e := l.ctx.Err(); e != nil {
		l.err = e
		return false
	}
	if n != l.stats.Generated || job == nil {
		l.err = errors.New("lookahead producer ordinal gap/reorder/nil job")
		return false
	}
	l.stats.Generated++
	l.buffer = append(l.buffer, r5LookaheadItem{n, box, job, boxOrdinal})
	l.stats.MaxBuffered = max(l.stats.MaxBuffered, len(l.buffer))
	if len(l.buffer) == 100 {
		l.err = l.emit()
	}
	return l.err == nil
}

// Stable earliest-ordinal mailbox heads first; each head appears at most once
// until every available distinct mailbox is represented. Tail duplicates use
// dependency gates, never concurrent same-mailbox Store/lifecycle execution.
func (l *r5Lookahead) emit() error {
	if e := l.ctx.Err(); e != nil {
		return e
	}
	if len(l.buffer) == 0 {
		return nil
	}
	n := min(20, len(l.buffer))
	indices := make([]int, 0, n)
	used := map[int]bool{}
	boxes := map[uuid.UUID]bool{}
	for i, item := range l.buffer {
		if !boxes[item.box] {
			indices = append(indices, i)
			used[i] = true
			boxes[item.box] = true
			if len(indices) == n {
				break
			}
		}
	}
	for i := range l.buffer {
		if len(indices) == n {
			break
		}
		if !used[i] {
			indices = append(indices, i)
			used[i] = true
		}
	}
	sort.Ints(indices)
	batch := make([]r5LookaheadItem, 0, n)
	for _, i := range indices {
		batch = append(batch, l.buffer[i])
	}
	old := l.buffer
	kept := old[:0]
	for i, item := range old {
		if !used[i] {
			kept = append(kept, item)
		}
	}
	clear(old[len(kept):])
	l.buffer = kept
	for _, item := range batch {
		prev := l.tails[item.box]
		gate := &r5LookaheadGate{done: make(chan struct{})}
		l.tails[item.box] = gate
		l.stats.Emitted++
		l.stats.EmittedOrdinals = append(l.stats.EmittedOrdinals, item.ordinal)
		l.stats.EmittedMailboxes = append(l.stats.EmittedMailboxes, item.boxOrdinal)
		l.window[item.box] = true
		if l.stats.Emitted%20 == 0 {
			l.stats.WindowUnique = append(l.stats.WindowUnique, len(l.window))
			l.window = map[uuid.UUID]bool{}
		}
		if !l.enqueue(func(ctx context.Context) (err error) {
			l.inflight.Add(1)
			defer l.inflight.Add(-1)
			defer func() { gate.err = err; close(gate.done) }()
			start := time.Now()
			if prev != nil {
				select {
				case <-prev.done:
				case <-ctx.Done():
					l.waitNanos.Add(time.Now().UnixNano() - start.UnixNano())
					return ctx.Err()
				}
				if prev.err != nil {
					return prev.err
				}
			}
			l.waitNanos.Add(time.Now().UnixNano() - start.UnixNano())
			if e := ctx.Err(); e != nil {
				return e
			}
			e := item.job(ctx)
			if e == nil {
				l.completed.Add(1)
			}
			return e
		}) {
			if e := l.ctx.Err(); e != nil {
				return e
			}
			return errors.New("lookahead downstream enqueue/flush failed")
		}
	}
	return nil
}
func (l *r5Lookahead) finish() error {
	if l.err != nil {
		return l.err
	}
	for len(l.buffer) > 0 {
		if e := l.emit(); e != nil {
			l.err = e
			return e
		}
	}
	return l.ctx.Err()
}
func (l *r5Lookahead) snapshot() r5LookaheadStats {
	v := l.stats
	v.FIFOCompleted = l.completed.Load()
	v.JoinedInflight = l.inflight.Load()
	v.GateWaitSecondsSum = float64(l.waitNanos.Load()) / float64(time.Second)
	return v
}
func (l *r5Lookahead) report(method string) map[string]any {
	s := l.snapshot()
	return map[string]any{"method": method, "lookahead_limit": 100, "generated_inputs": s.Generated, "emitted_inputs": s.Emitted, "max_unpersisted_buffered_inputs": s.MaxBuffered, "ordinal_fifo_completed_jobs": s.FIFOCompleted, "joined_fifo_inflight": s.JoinedInflight, "fifo_gate_call_elapsed_seconds_sum": s.GateWaitSecondsSum, "actual_emitted_window_unique_mailboxes": s.WindowUnique, "actual_emitted_ordinal_order": s.EmittedOrdinals, "actual_emitted_mailbox_ordinals": s.EmittedMailboxes}
}

func TestR5LookaheadFairFIFOAndBound(t *testing.T) {
	var jobs []func(context.Context) error
	var active, maxActive atomic.Int32
	var mu sync.Mutex
	seen := map[int][]int{}
	enqueue := func(job func(context.Context) error) bool {
		jobs = append(jobs, job)
		if len(jobs) == 20 {
			batch := jobs
			jobs = nil
			err := r5BenchPrepare(context.Background(), func(ctx context.Context, put func(func(context.Context) error) bool) error {
				for _, fn := range batch {
					if !put(fn) {
						return ctx.Err()
					}
				}
				return nil
			})
			if err != nil {
				t.Error(err)
				return false
			}
		}
		return true
	}
	l := newR5Lookahead(context.Background(), enqueue)
	for n := 0; n < 400; n++ {
		box := n / 5
		if !l.add(n, r5BenchID("lookahead-box", box), func(context.Context) error {
			v := active.Add(1)
			defer active.Add(-1)
			for old := maxActive.Load(); v > old && !maxActive.CompareAndSwap(old, v); old = maxActive.Load() {
			}
			mu.Lock()
			defer mu.Unlock()
			want := box*5 + len(seen[box])
			if n != want {
				return fmt.Errorf("mailbox%d ordinalFIFO got%d want%d", box, n, want)
			}
			seen[box] = append(seen[box], n)
			return nil
		}) {
			t.Fatal(l.err)
		}
	}
	if err := l.finish(); err != nil {
		t.Fatal(err)
	}
	s := l.snapshot()
	if s.Generated != 400 || s.Emitted != 400 || s.FIFOCompleted != 400 || s.JoinedInflight != 0 || s.MaxBuffered != 100 || maxActive.Load() > 20 || len(jobs) != 0 || len(seen) != 80 || len(s.WindowUnique) != 20 || s.WindowUnique[0] != 20 || len(s.EmittedOrdinals) != 400 {
		t.Fatalf("fair/FIFO/buffer/active terminal: %+v maxActive%d", s, maxActive.Load())
	}
	for _, v := range s.WindowUnique {
		if v < 1 || v > 20 {
			t.Fatal("invalid observed window diversity")
		}
	}
}

func TestR5LookaheadCancelJoin(t *testing.T) {
	for _, mode := range []string{"cancel_waiting_duplicate", "predecessor_error", "downstream_reject"} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				injected := errors.New("independent predecessor failed")
				var calls atomic.Int32
				var queued []func(context.Context) error
				l := newR5Lookahead(ctx, func(fn func(context.Context) error) bool {
					if mode == "downstream_reject" {
						return false
					}
					queued = append(queued, fn)
					return true
				})
				for n := 0; n < 100; n++ {
					if !l.add(n, uuid.Nil, func(ctx context.Context) error {
						calls.Add(1)
						if mode == "predecessor_error" {
							return injected
						}
						<-ctx.Done()
						return ctx.Err()
					}) {
						break
					}
				}
				if mode == "downstream_reject" {
					if l.finish() == nil || calls.Load() != 0 || l.inflight.Load() != 0 {
						t.Fatal("rejected jobs started or failure disappeared")
					}
					return
				}
				done := make(chan error, 1)
				go func() {
					done <- r5BenchPrepare(ctx, func(ctx context.Context, put func(func(context.Context) error) bool) error {
						for _, fn := range queued {
							if !put(fn) {
								return ctx.Err()
							}
						}
						return nil
					})
				}()
				if mode == "cancel_waiting_duplicate" {
					synctest.Wait()
					if calls.Load() != 1 || l.inflight.Load() != 20 {
						t.Fatalf("duplicate FIFO gate barrier wrong: called=%d active=%d", calls.Load(), l.inflight.Load())
					}
					cancel()
				}
				err := <-done
				synctest.Wait()
				want := error(context.Canceled)
				if mode == "predecessor_error" {
					want = injected
				}
				if !errors.Is(err, want) || calls.Load() != 1 || l.inflight.Load() != 0 || l.completed.Load() != 0 {
					t.Fatalf("FIFO cancel/failure/unjoined dependent: err=%v called=%d active=%d completed=%d", err, calls.Load(), l.inflight.Load(), l.completed.Load())
				}
			})
		})
	}
}

func TestR5LookaheadRejectsProducerOrder(t *testing.T) {
	for _, mode := range []string{"gap", "repeat", "nil", "canceled"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			l := newR5Lookahead(ctx, func(func(context.Context) error) bool { t.Error("invalid producer emitted to writer"); return false })
			n := 0
			fn := func(context.Context) error { return nil }
			switch mode {
			case "gap":
				n = 1
			case "repeat":
				if !l.add(0, uuid.Nil, fn) {
					t.Fatal(l.err)
				}
			case "nil":
				fn = nil
			case "canceled":
				cancel()
			}
			if l.add(n, uuid.Nil, fn) || l.finish() == nil || l.inflight.Load() != 0 {
				t.Fatal("invalid producer accepted")
			}
		})
	}
}

func TestR5LookaheadFingerprintMismatch(t *testing.T) {
	a := &r5DiagnosticBaseline{Fingerprint: "aggregate", Parameters: "params"}
	rows := []map[string]any{}
	for i := 0; i < 6; i++ {
		a.Streams = append(a.Streams, r5DiagnosticStream{i, i + 1, fmt.Sprint(i)})
		rows = append(rows, map[string]any{"stream_id": i, "row_count": i + 1, "sha256": fmt.Sprint(i)})
	}
	if e := r5CompareDiagnosticStreams(a, rows, "aggregate", "params"); e != nil {
		t.Fatal(e)
	}
	for _, field := range []string{"stream_id", "row_count", "sha256"} {
		t.Run(field, func(t *testing.T) {
			old := rows[1][field]
			rows[1][field] = "wrong"
			defer func() { rows[1][field] = old }()
			if r5CompareDiagnosticStreams(a, rows, "aggregate", "params") == nil {
				t.Fatal("different actual stream accepted/normalized")
			}
		})
	}
	if r5CompareDiagnosticStreams(a, rows, "different", "params") == nil || r5CompareDiagnosticStreams(a, rows, "aggregate", "different") == nil {
		t.Fatal("different dataset/parameter fingerprint accepted")
	}
}

func TestR5StoreDiagnosticClassification(t *testing.T) {
	for sql, want := range map[string]string{"begin": "begin", "commit": "commit", "SELECT pg_advisory_xact_lock(hashtext($1::text))": "advisory_raw_key", "UPDATE mailboxes SET message_count = message_count + 1 WHERE id=$1": "quota_mailbox_update", "INSERT INTO messages (id) VALUES($1)": "insert_message_including_trigger", "UPDATE messages SET archived_at=clock_timestamp() WHERE id=$1": "life_archived", "UPDATE messages SET deleted_at=clock_timestamp() WHERE id=$1": "life_trash_expired", "UPDATE messages SET expires_at=clock_timestamp() WHERE id=$1": "life_hard_expired", "SELECT 1": "unknown_store_sql"} {
		if got := r5DiagnosticSQLClass(sql); got != want {
			t.Fatalf("class=%s want=%s", got, want)
		}
	}
}
func TestR5StoreDiagnosticUnknownMetrics(t *testing.T) {
	if r5DiagnosticValidate(nil) == nil {
		t.Fatal("unknown spans accepted")
	}
	d := newR5StoreDiagnostic(time.Now(), "", "", nil)
	ctx := d.scope(context.Background(), 0, 4096, r5BenchID("diagnostic-pure", 0))
	d.record(ctx, "store", time.Now().Add(-time.Millisecond), nil)
	if r5DiagnosticValidate(d.spans) == nil {
		t.Fatal("partial observation accepted")
	}
	if d.spans["store"].Calls != 1 || d.spans["store"].ElapsedSum <= 0 {
		t.Fatal("scoped delegate timing missing")
	}
	d.record(context.Background(), "not_store_scope", time.Now(), nil)
	if len(d.spans) != 1 {
		t.Fatal("observer SQL/control work contaminated store scope")
	}
}

type r5DiagnosticDelegateBlob struct {
	existsCalls, putCalls int
	key                   string
	raw                   []byte
	err                   error
}

func (b *r5DiagnosticDelegateBlob) Exists(context.Context, string) (bool, error) {
	b.existsCalls++
	return false, nil
}
func (b *r5DiagnosticDelegateBlob) Put(_ context.Context, key string, r io.Reader, n int64) error {
	b.putCalls++
	b.key = key
	b.raw, _ = io.ReadAll(r)
	if int64(len(b.raw)) != n {
		return errors.New("delegate length changed")
	}
	return b.err
}
func (b *r5DiagnosticDelegateBlob) Delete(context.Context, string) error { return nil }

type r5DiagnosticDelegateRefs struct {
	rawobject.ReferenceStore
	calls   int
	message *models.Message
	quota   int
}

func (r *r5DiagnosticDelegateRefs) CreateMessageWithQuota(ctx context.Context, m *models.Message, q int, ensure func(context.Context) error) (bool, error) {
	r.calls++
	r.message = m
	r.quota = q
	if e := ensure(ctx); e != nil {
		return false, e
	}
	return true, nil
}

func TestR5StoreDiagnosticExactDelegation(t *testing.T) {
	for _, failed := range []bool{false, true} {
		t.Run(fmt.Sprint(failed), func(t *testing.T) {
			d := newR5StoreDiagnostic(time.Now(), "", "", nil)
			blob, refs := &r5DiagnosticDelegateBlob{}, &r5DiagnosticDelegateRefs{}
			injected := errors.New("delegate object error")
			if failed {
				blob.err = injected
			}
			raw := []byte("From: synthetic@test\r\n\r\nunchanged body")
			m := &models.Message{ID: r5BenchID("diagdelegate", 0), RawObjectKey: rawobject.Key(raw)}
			ctx := d.scope(context.Background(), 0, int64(len(raw)), uuid.Nil)
			store := rawobject.NewStore(&r5DiagnosticBlob{blob, d}, &r5DiagnosticRefs{refs, d})
			created, e := store.StoreMessage(ctx, m, raw, 123)
			if created == failed || failed && !errors.Is(e, injected) || !failed && e != nil || blob.existsCalls != 1 || blob.putCalls != 1 || refs.calls != 1 || refs.message != m || refs.quota != 123 || blob.key != m.RawObjectKey || string(blob.raw) != string(raw) {
				t.Fatalf("observer replaced/mutated/retried production delegate: created=%t err=%v", created, e)
			}
			for _, name := range []string{"reference_transaction_including_ensure", "ensure", "blob_exists", "blob_put_including_original_durability"} {
				if d.spans[name] == nil || d.spans[name].Calls != 1 {
					t.Fatalf("delegate span %s missing", name)
				}
			}
		})
	}
}
