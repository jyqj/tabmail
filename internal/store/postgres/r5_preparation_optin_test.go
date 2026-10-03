//go:build r5benchmark

package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"tabmail/internal/models"
)

const r5PreparationOriginal = "bounded_store_claim_complete_window_v1"
const r5PreparationLookahead = "lookahead100_unpersisted_inputs_distinct_mailbox_windows20_ordinal_fifo_v1"
const r5PreparationUIDCatalogSQL = "SELECT count(*) FROM pg_attribute WHERE attrelid='messages'::regclass AND NOT attisdropped AND attnum>0 AND lower(attname) IN ('uid','imap_uid','message_uid')"
const r5PreparationReceivedOrderSQL = "SELECT id,mailbox_id,subject,received_at FROM messages ORDER BY mailbox_id,received_at,id"

func r5SelectPreparation(method, scale, contractSHA string) (string, error) {
	if method == "" || method == r5PreparationOriginal {
		return r5PreparationOriginal, nil
	}
	if method != r5PreparationLookahead {
		return "", errors.New("unsupported benchmark preparation method")
	}
	if scale != "S" && scale != "tool_only" && scale != "M" {
		return "", errors.New("lookahead benchmark admitted only for S/tool/original M; L remains unapproved")
	}
	if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(contractSHA) {
		return "", errors.New("explicit frozen lookahead method contract SHA256 required")
	}
	return method, nil
}
func (s *r5BenchState) selectedPreparation() string {
	if s.preparationMethod == "" {
		return r5PreparationOriginal
	}
	return s.preparationMethod
}

// Stable fixture mailbox numbering, not random DB UUID normalization added to
// a fingerprint. It is the generator's existing two-tenant array assignment.
func r5GeneratedMailboxOrdinal(n, employees int) int {
	main := employees * 80 / 100
	foreign := employees * 20 / 100
	count, offset := main, 0
	if (n/20)%5 == 4 {
		count, offset = foreign, main*5
	}
	slot := (n / 5) % (count * 5)
	if n%20 == 19 {
		slot = count + (n/20)%(count*4)
	}
	return offset + slot
}

func r5PreparationBoundary() map[string]any {
	return map[string]any{"generator_ordinal_mailbox_fifo_preserved": true, "A_random_commit_order_not_preserved_or_claimed": true, "received_at_and_received_order_may_change_with_B_scheduling": true, "all_persistent_equivalent": false, "protocol_UID_certified": false, "canonical_streams_unchanged_no_added_normalization": true, "all_physical_uuid_time_protocol_uid_fields_not_covered_by_six_streams": true, "uncovered_fields": "received_at/received-order, lifecycle clock timestamps, indexed_at, next_attempt_at, lease tokens, random actor/mailbox row UUIDs; protocol UID not certified"}
}

type r5ReceivedProjection struct {
	size                        [3]int
	boxes                       map[uuid.UUID]int
	lastBox                     uuid.UUID
	lastOrdinal                 int
	lastReceived                time.Time
	rows, mailboxes, violations int
}

func (p *r5ReceivedProjection) observe(id, box uuid.UUID, subject string, received time.Time) error {
	if !strings.HasPrefix(subject, "Benchmark ") {
		return errors.New("actual received-order subject binding missing")
	}
	n, e := strconv.Atoi(strings.TrimPrefix(subject, "Benchmark "))
	if e != nil || n < 0 || n >= p.size[2] || subject != "Benchmark "+strconv.Itoa(n) || id != r5BenchID("message", n) {
		return errors.New("actual received-order message identity/ordinal mismatch")
	}
	index, ok := p.boxes[box]
	if !ok || index != r5GeneratedMailboxOrdinal(n, p.size[0]) {
		return errors.New("actual received-order mailbox identity/ordinal mismatch")
	}
	if received.IsZero() {
		return errors.New("unknown actual received_at")
	}
	if p.rows == 0 || box != p.lastBox {
		p.mailboxes++
	} else if n <= p.lastOrdinal || !received.After(p.lastReceived) {
		p.violations++
	}
	p.rows++
	p.lastBox, p.lastOrdinal, p.lastReceived = box, n, received
	return nil
}
func (s *r5BenchState) preparationPersistentObservations(ctx context.Context, size [3]int, source string, schema int64) (map[string]any, error) {
	var uidColumns int
	if e := s.pool.QueryRow(ctx, r5PreparationUIDCatalogSQL).Scan(&uidColumns); e != nil {
		return nil, e
	}
	boxes := map[uuid.UUID]int{}
	index := 0
	for _, c := range s.companies {
		for _, set := range [][]*models.Mailbox{c.personal, c.shared} {
			for _, box := range set {
				boxes[box.ID] = index
				index++
			}
		}
	}
	p := r5ReceivedProjection{size: size, boxes: boxes, lastOrdinal: -1}
	rows, e := s.pool.Query(ctx, r5PreparationReceivedOrderSQL)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	for rows.Next() {
		var id, box uuid.UUID
		var subject string
		var received time.Time
		if e = rows.Scan(&id, &box, &subject, &received); e != nil {
			return nil, e
		}
		if e = p.observe(id, box, subject, received); e != nil {
			return nil, e
		}
	}
	if e = rows.Err(); e != nil {
		return nil, e
	}
	if p.rows != size[2] || p.violations != 0 {
		return nil, fmt.Errorf("actual received-order projection rows=%d violations=%d", p.rows, p.violations)
	}
	ready, e := s.indexReadyCount(ctx)
	if e != nil {
		return nil, e
	}
	if ready != size[2] {
		return nil, errors.New("persistent observation lacked source-bound index-ready rows")
	}
	uh, rh := sha256.Sum256([]byte(r5PreparationUIDCatalogSQL)), sha256.Sum256([]byte(r5PreparationReceivedOrderSQL))
	return map[string]any{"origin": "actual_owned_pg_catalog_and_received_at_projection", "source_sha": source, "schema_version": schema, "uid_catalog_query_sha256": hex.EncodeToString(uh[:]), "uid_column_count": uidColumns, "received_at_projection_query_sha256": hex.EncodeToString(rh[:]), "projected_messages": p.rows, "projected_mailboxes": p.mailboxes, "ordinal_fifo_violations": p.violations, "index_ready_count": ready, "source_identity_bindings_valid": true}, nil
}

func TestR5PreparationOptinSelection(t *testing.T) {
	sha := strings.Repeat("a", 64)
	for _, method := range []string{"", r5PreparationOriginal} {
		got, e := r5SelectPreparation(method, "S", "")
		if e != nil || got != r5PreparationOriginal {
			t.Fatal("default preparation changed")
		}
	}
	if got, e := r5SelectPreparation(r5PreparationLookahead, "S", sha); e != nil || got != r5PreparationLookahead {
		t.Fatal("explicit new method rejected")
	}
	for _, bad := range [][3]string{{"unknown", "S", sha}, {r5PreparationLookahead, "unknown_scale", sha}, {r5PreparationLookahead, "S", ""}, {r5PreparationLookahead, "L", sha}} {
		if _, e := r5SelectPreparation(bad[0], bad[1], bad[2]); e == nil {
			t.Fatal("unbound/unsupported preparation accepted")
		}
	}
	s := &r5BenchState{preparationMethod: r5PreparationLookahead}
	if s.selectedPreparation() != r5PreparationLookahead || s.diagnostic != nil {
		t.Fatal("lookahead incorrectly requires/enables diagnostic observer")
	}
}

func TestR5PreparationOriginalMAdmission(t *testing.T) {
	sha := strings.Repeat("a", 64)
	if r5BenchmarkScales["M"] != [3]int{1000, 5000, 1000000} {
		t.Fatal("original M population was reduced")
	}
	method, e := r5SelectPreparation(r5PreparationLookahead, "M", sha)
	if e != nil || method != r5PreparationLookahead {
		t.Fatal("approved original M method rejected")
	}
	for _, bad := range []struct{ scale, hash string }{{"L", sha}, {"M", ""}, {"M", "invalid"}, {"unknown", sha}} {
		t.Run(bad.scale+"_"+bad.hash, func(t *testing.T) {
			if _, e := r5SelectPreparation(r5PreparationLookahead, bad.scale, bad.hash); e == nil {
				t.Fatal("L/unbound/unknown M admission accepted")
			}
		})
	}
	if method, e := r5SelectPreparation("", "M", ""); e != nil || method != r5PreparationOriginal {
		t.Fatal("old default M method changed")
	}
}
func TestR5PreparationReceivedBoundary(t *testing.T) {
	box := r5BenchID("projectionbox", 0)
	makeProjection := func() *r5ReceivedProjection {
		return &r5ReceivedProjection{size: [3]int{20, 100, 1000}, boxes: map[uuid.UUID]int{box: 0}}
	}
	base := time.Unix(1700000000, 0)
	p := makeProjection()
	for _, n := range []int{0, 1} {
		if e := p.observe(r5BenchID("message", n), box, fmt.Sprintf("Benchmark %d", n), base.Add(time.Duration(n)*time.Millisecond)); e != nil {
			t.Fatal(e)
		}
	}
	if p.violations != 0 || p.rows != 2 || p.mailboxes != 1 {
		t.Fatal("actual-style received projection missing")
	}
	for _, kind := range []string{"reversed", "timestamp_tie", "wrong_id", "wrong_box", "unknown_received", "noncanonical_subject"} {
		t.Run(kind, func(t *testing.T) {
			p := makeProjection()
			_ = p.observe(r5BenchID("message", 1), box, "Benchmark 1", base)
			id, b, subject, date := r5BenchID("message", 2), box, "Benchmark 2", base.Add(time.Millisecond)
			switch kind {
			case "reversed":
				id, subject = r5BenchID("message", 0), "Benchmark 0"
			case "timestamp_tie":
				date = base
			case "wrong_id":
				id = uuid.Nil
			case "wrong_box":
				b = uuid.Nil
			case "unknown_received":
				date = time.Time{}
			case "noncanonical_subject":
				subject = "Benchmark 02"
			}
			e := p.observe(id, b, subject, date)
			if e == nil && p.violations == 0 {
				t.Fatal("missing/changed received identity/order accepted")
			}
		})
	}
	b := r5PreparationBoundary()
	if b["all_persistent_equivalent"] != false || b["protocol_UID_certified"] != false || b["received_at_and_received_order_may_change_with_B_scheduling"] != true {
		t.Fatal("six streams improperly certify uncovered persistent/UID fields")
	}
}

func TestR5PreparationMappedScheduler(t *testing.T) {
	var batch []func(context.Context) error
	l := newR5Lookahead(context.Background(), func(job func(context.Context) error) bool {
		batch = append(batch, job)
		if len(batch) == 20 {
			jobs := batch
			batch = nil
			e := r5BenchPrepare(context.Background(), func(ctx context.Context, put func(func(context.Context) error) bool) error {
				for _, job := range jobs {
					if !put(job) {
						return ctx.Err()
					}
				}
				return nil
			})
			if e != nil {
				t.Error(e)
				return false
			}
		}
		return true
	})
	for n := 0; n < 400; n++ {
		box := r5GeneratedMailboxOrdinal(n, 20)
		if box < 0 || box >= 100 {
			t.Fatal("invalid generator mailbox ordinal")
		}
		if !l.addMapped(n, r5BenchID("optin-actualbox", box), box, func(context.Context) error { return nil }) {
			t.Fatal(l.err)
		}
	}
	if e := l.finish(); e != nil {
		t.Fatal(e)
	}
	report := l.report(r5PreparationLookahead)
	s := l.snapshot()
	if report["method"] != r5PreparationLookahead || report["lookahead_limit"] != 100 || s.Generated != 400 || s.FIFOCompleted != 400 || s.JoinedInflight != 0 || len(s.EmittedOrdinals) != 400 || len(s.EmittedMailboxes) != 400 {
		t.Fatal("unbound/incomplete mapped scheduler report")
	}
	seen := map[int]bool{}
	last := map[int]int{}
	for i, n := range s.EmittedOrdinals {
		box := s.EmittedMailboxes[i]
		if seen[n] || box != r5GeneratedMailboxOrdinal(n, 20) {
			t.Fatal("emitted mailbox/source association changed")
		}
		if prev, ok := last[box]; ok && n <= prev {
			t.Fatal("emitted per-mailbox FIFO changed")
		}
		seen[n] = true
		last[box] = n
	}
	for w, unique := range s.WindowUnique {
		set := map[int]bool{}
		for _, box := range s.EmittedMailboxes[w*20 : (w+1)*20] {
			set[box] = true
		}
		if len(set) != unique {
			t.Fatal("window diversity metadata is not actual emitted association")
		}
	}
}
