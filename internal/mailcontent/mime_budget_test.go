package mailcontent

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jhillyerd/enmime/v2"
)

func budgetNested(depth int) []byte {
	raw := "Content-Type: text/plain\r\n\r\nbody"
	for level := depth - 1; level > 0; level-- {
		boundary := fmt.Sprintf("depth%d", level)
		raw = fmt.Sprintf("Content-Type: multipart/mixed; boundary=%s\r\n\r\n--%s\r\n%s\r\n--%s--\r\n", boundary, boundary, raw, boundary)
	}
	return []byte(raw)
}
func budgetWide(children int, childHeader string) []byte {
	var raw strings.Builder
	raw.WriteString("Content-Type: multipart/mixed; boundary=W\r\n\r\n")
	for i := 0; i < children; i++ {
		fmt.Fprintf(&raw, "--W\r\n%s\r\n\r\nbody\r\n", childHeader)
	}
	raw.WriteString("--W--\r\n")
	return []byte(raw.String())
}

// Probe the real allocator using the exact original retained-node count, then
// one fewer node. No reconstructed MIME walk or body-slice cursor is trusted.
func assertActualBudgetAgreement(t *testing.T, raw []byte, original *enmime.Envelope) {
	t.Helper()
	nodes := len(original.Root.DepthMatchAll(func(*enmime.Part) bool { return true }))
	parts := len(Parts(original))
	if parts == 0 {
		parts = 1
	}
	limits := enmime.ParseLimits{MaxDepth: MaxMIMEDepth, MaxNodes: nodes, MaxParts: parts, MaxBoundaryBytes: 4090}
	got, err := enmime.ReadEnvelopeBounded(context.Background(), bytes.NewReader(raw), limits)
	if err != nil || !reflect.DeepEqual(got, original) {
		t.Fatalf("exact real-event budget changed original envelope: %v; nodes=%d parts=%d", err, nodes, parts)
	}
	if nodes > 1 {
		limits.MaxNodes = nodes - 1
		rejected, err := enmime.ReadEnvelopeBounded(context.Background(), bytes.NewReader(raw), limits)
		if !errors.Is(err, enmime.ErrParseNodeLimit) || rejected != nil {
			t.Fatalf("actual-node budget minus one did not reject: %v, nodes=%d", err, nodes)
		}
	}
}
func TestMIMEPreparseBudgetDepthAndAllNodes(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  []byte
		want error
	}{
		{"depth_exact", budgetNested(MaxMIMEDepth), nil},
		{"depth_plus_one", budgetNested(MaxMIMEDepth + 1), ErrMIMEDepth},
		{"nodes_exact", budgetWide(MaxMIMENodes-1, "Content-Type: text/plain"), nil},
		{"body_nodes_plus_one", budgetWide(MaxMIMENodes, "Content-Type: text/plain"), ErrMIMENodes},
		{"missing_type_nodes_plus_one", budgetWide(MaxMIMENodes, "X-Test: no-content-type"), ErrMIMENodes},
		{"other_nodes_plus_one", budgetWide(MaxMIMENodes, "Content-Type: message/rfc822"), ErrMIMENodes},
		{"attachment_exact", budgetWide(maxParts, "Content-Type: text/plain\r\nContent-Disposition: attachment"), nil},
		{"attachment_plus_one", budgetWide(maxParts+1, "Content-Type: text/plain\r\nContent-Disposition: attachment"), ErrMIMEParts},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env, err := ParseBounded(tc.raw)
			if !errors.Is(err, tc.want) {
				t.Fatalf("parse %v want %v", err, tc.want)
			}
			if tc.want != nil {
				if env != nil {
					t.Fatal("refused tree exposed")
				}
				return
			}
			original, err := enmime.ReadEnvelope(bytes.NewReader(tc.raw))
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(env, original) {
				t.Fatal("admitted envelope changed")
			}
			assertActualBudgetAgreement(t, tc.raw, original)
		})
	}
}
func TestMIMEPreparseBudgetProjectionCompatibility(t *testing.T) {
	for _, ctype := range []string{"missing", "empty"} {
		for _, disposition := range []string{"attachment", "inline"} {
			t.Run(ctype+"_"+disposition, func(t *testing.T) {
				header := "Content-Disposition: " + disposition
				if ctype == "empty" {
					header = "Content-Type:\r\n" + header
				}
				raw := budgetWide(maxParts+1, header)
				original, err := enmime.ReadEnvelope(bytes.NewReader(raw))
				if err != nil {
					t.Fatal(err)
				}
				if len(Parts(original)) != 0 || len(original.Root.DepthMatchAll(func(*enmime.Part) bool { return true })) != maxParts+2 {
					t.Fatal("original missing/empty CT projection precondition changed")
				}
				got, err := ParseBounded(raw)
				if err != nil || !reflect.DeepEqual(got, original) {
					t.Fatalf("missing/empty CT changed: %v", err)
				}
				assertActualBudgetAgreement(t, raw, original)
			})
		}
	}
	for _, count := range []int{256, 257} {
		t.Run(fmt.Sprintf("octet_inline_%d", count), func(t *testing.T) {
			raw := budgetWide(count, "Content-Type: application/octet-stream\r\nContent-Disposition: inline")
			original, err := enmime.ReadEnvelope(bytes.NewReader(raw))
			if err != nil {
				t.Fatal(err)
			}
			if len(original.Attachments) != count || len(original.Inlines) != count || len(Parts(original)) != 2*count {
				t.Fatal("original double projection changed")
			}
			got, err := ParseBounded(raw)
			if count == 256 {
				if err != nil || !reflect.DeepEqual(got, original) {
					t.Fatalf("exact projection changed: %v", err)
				}
				assertActualBudgetAgreement(t, raw, original)
			} else if !errors.Is(err, ErrMIMEParts) || got != nil {
				t.Fatalf("projection above 512 admitted: %v", err)
			}
		})
	}
}

// Exact original V2/V3 fixture: marker offset includes the child's headers.
func budgetOffsetFixture(boundary string, markerOffset int, terminator bool) ([]byte, []byte) {
	header := "Content-Type: text/plain\r\n\r\n"
	marker := "--" + boundary
	if terminator {
		marker += "--"
	}
	child := header + strings.Repeat("x", markerOffset-len(header)) + marker + "\r\nContent-Type: text/plain\r\n\r\nsecond\r\n--" + boundary + "--\r\n"
	raw := "Content-Type: multipart/mixed; boundary=" + boundary + "\r\n\r\n--" + boundary + "\r\n" + child
	return []byte(raw), []byte(child)
}
func TestMIMEPreparseBudgetBoundaryOffsets(t *testing.T) {
	for _, boundaryLen := range []int{1, 70, 4090} {
		boundary := strings.Repeat("B", boundaryLen)
		for _, offset := range []int{4093, 4094, 4095, 4096, 4097, 4098} {
			for _, terminator := range []bool{false, true} {
				raw, _ := budgetOffsetFixture(boundary, offset, terminator)
				for _, nested := range []bool{false, true} {
					t.Run(fmt.Sprintf("len_%d/offset_%d/final_%v/nested_%v", boundaryLen, offset, terminator, nested), func(t *testing.T) {
						fixture := append([]byte(nil), raw...)
						if nested {
							fixture = append([]byte("Content-Type: multipart/mixed; boundary=OUTER\r\n\r\n--OUTER\r\n"), fixture...)
							fixture = append(fixture, []byte("\r\n--OUTER--\r\n")...)
						}
						original, originalErr := enmime.ReadEnvelope(bytes.NewReader(fixture))
						got, err := ParseBounded(fixture)
						if (err == nil) != (originalErr == nil) {
							t.Fatalf("error changed: original %v bounded %v", originalErr, err)
						}
						if err == nil {
							if !reflect.DeepEqual(got, original) {
								t.Fatal("exact fill-offset envelope changed")
							}
							assertActualBudgetAgreement(t, fixture, original)
						}
					})
				}
			}
		}
	}
	for _, nested := range []bool{false, true} {
		t.Run(fmt.Sprintf("recursive_boundary_4091_rejected/nested_%v", nested), func(t *testing.T) {
			boundary := strings.Repeat("L", 4091)
			raw := []byte("Content-Type: multipart/mixed; boundary=" + boundary + "\r\n\r\n--" + boundary + "\r\nContent-Type: text/plain\r\n\r\nbody\r\n--" + boundary + "--\r\n")
			if nested {
				raw = append([]byte("Content-Type: multipart/mixed; boundary=OUTER\r\n\r\n--OUTER\r\n"), raw...)
				raw = append(raw, []byte("\r\n--OUTER--\r\n")...)
			}
			if env, err := ParseBounded(raw); !errors.Is(err, ErrMIMEParse) || env != nil {
				t.Fatalf("long recursive boundary %v / %v", env, err)
			}
		})
	}
}
func TestMIMEPreparseBudgetBoundaryDrain(t *testing.T) {
	for _, boundaryLen := range []int{1, 70, 4090} {
		boundary := strings.Repeat("D", boundaryLen)
		for _, terminator := range []bool{false, true} {
			for _, nested := range []bool{false, true} {
				t.Run(fmt.Sprintf("len_%d/final_%v/nested_%v", boundaryLen, terminator, nested), func(t *testing.T) {
					header := "Content-Type: text/plain\r\n\r\n"
					marker := "--" + boundary
					if terminator {
						marker += "--"
					}
					child := header + strings.Repeat("x", 4093-len(header)) + marker + "\r\nContent-Type: application/octet-stream\r\nContent-Disposition: inline\r\n\r\ndiscarded phantom\r\n--" + boundary + "\r\nContent-Type: text/plain\r\nContent-Disposition: attachment; filename=real.txt\r\n\r\nreal attachment\r\n--" + boundary + "--\r\n"
					raw := []byte("Content-Type: multipart/mixed; boundary=" + boundary + "\r\n\r\n--" + boundary + "\r\n" + child)
					expectedNodes := 3
					if nested {
						raw = append([]byte("Content-Type: multipart/mixed; boundary=OUTER\r\n\r\n--OUTER\r\n"), raw...)
						raw = append(raw, []byte("\r\n--OUTER--\r\n")...)
						expectedNodes++
					}
					original, err := enmime.ReadEnvelope(bytes.NewReader(raw))
					if err != nil {
						t.Fatal(err)
					}
					if len(original.Root.DepthMatchAll(func(*enmime.Part) bool { return true })) != expectedNodes || len(Parts(original)) != 1 {
						t.Fatal("original drain fixture node/projection precondition changed")
					}
					got, err := ParseBounded(raw)
					if err != nil || !reflect.DeepEqual(got, original) {
						t.Fatalf("real drain sequence changed: %v", err)
					}
					assertActualBudgetAgreement(t, raw, original)
				})
			}
		}
	}
}

func TestMIMEPreparseBudgetCompatibility(t *testing.T) {
	fixtures := [][]byte{
		[]byte(multipartFixture),
		[]byte("Subject: plain\r\n\r\nbody"),
		[]byte("Content-Type: text/html; charset=utf-8\r\n\r\n<p>Body</p>"),
		[]byte("Content-Type: text/plain\r\nContent-Transfer-Encoding: base64\r\n\r\n%%%corrupt%%%"),
		[]byte("Content-Type: text/plain\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\nbody=ZZ"),
		[]byte(strings.ReplaceAll(multipartFixture, "\r\n", "\n")),
		[]byte(strings.ReplaceAll(multipartFixture, "boundary=boundary", "boundary=\r\n boundary")),
		[]byte(strings.ReplaceAll(multipartFixture, "Content-Type:", "Content-Type :")),
		[]byte(strings.TrimSuffix(multipartFixture, "--boundary--\r\n")),
		[]byte(strings.ReplaceAll(multipartFixture, "--boundary\r\n", "--boundary \t\r\n")),
		[]byte("Content-Type: message/rfc822\r\n\r\n" + string(budgetNested(MaxMIMEDepth+1))),
		[]byte("Content-Type: multipart/mixed; boundary=W\r\nContent-Transfer-Encoding: base64\r\n\r\nLS1XCg=="),
		[]byte("Content-Type: multipart/mixed; boundary=W\r\n\r\n--W\r\nContent-Type: text/plain; boundary=ignored\r\n\r\n--ignored\r\nbody\r\n--ignored--\r\n--W--\r\n"),
		[]byte("Content-Type: multipart/mixed; boundary=W\r\n\r\npreamble --W\r\nContent-Type: application/x-custom; boundary=C\r\n\r\n--C\r\nContent-Type: text/plain\r\n\r\nbody\r\n--C--\r\n--W--\r\nepilogue"),
	}
	// v2.3.0 atPartStart remains true throughout the first 4096-byte fill;
	// compare shifted/mid-line markers on both sides of that boundary.
	for _, padding := range []int{0, 4000, 4096, 4200} {
		fixtures = append(fixtures, []byte("Content-Type: multipart/mixed; boundary=W\r\n\r\n--W\r\nContent-Type: text/plain\r\n\r\n"+strings.Repeat("x", padding)+"literal --W\r\nContent-Type: text/plain\r\n\r\nsecond\r\n--W--\r\n"))
	}
	for i, raw := range fixtures {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			want, originalErr := enmime.ReadEnvelope(bytes.NewReader(raw))
			got, err := ParseBounded(raw)
			if (err == nil) != (originalErr == nil) {
				t.Fatalf("error changed: original %v, bounded %v", originalErr, err)
			}
			if err == nil {
				if !reflect.DeepEqual(got, want) {
					t.Fatal("envelope changed")
				}
				assertActualBudgetAgreement(t, raw, want)
			}
		})
	}
}

type budgetTestReader struct {
	raw     []byte
	step    int
	failure error
	cancel  context.CancelFunc
	closes  atomic.Int32
}

func (r *budgetTestReader) Read(p []byte) (int, error) {
	if len(r.raw) == 0 {
		if r.failure != nil {
			return 0, r.failure
		}
		return 0, io.EOF
	}
	n := len(p)
	if r.step > 0 && n > r.step {
		n = r.step
	}
	if n > len(r.raw) {
		n = len(r.raw)
	}
	copy(p, r.raw[:n])
	r.raw = r.raw[n:]
	if r.cancel != nil {
		r.cancel()
		r.cancel = nil
	}
	if len(r.raw) == 0 {
		return n, r.failure
	}
	return n, nil
}
func (r *budgetTestReader) Close() error { r.closes.Add(1); return nil }
func TestMIMEPreparseBudgetReaderBoundaries(t *testing.T) {
	fault := errors.New("short object read")
	for _, tc := range []struct {
		name   string
		raw    []byte
		step   int
		fault  error
		cancel bool
		want   error
	}{
		{"short_reads", []byte(multipartFixture), 3, nil, false, nil},
		{"short_read_error", []byte(multipartFixture), 3, fault, false, fault},
		{"malformed", []byte(badMIMEFixture), 2, nil, false, ErrMIMEParse},
		{"depth_limit", budgetNested(MaxMIMEDepth + 1), 5, nil, false, ErrMIMEDepth},
		{"canceled_during_read", []byte(multipartFixture), 3, nil, true, context.Canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			r := &budgetTestReader{raw: tc.raw, step: tc.step, failure: tc.fault}
			if tc.cancel {
				r.cancel = cancel
			}
			_, err := ParseBoundedReader(ctx, r)
			if !errors.Is(err, tc.want) {
				t.Fatalf("error %v want %v", err, tc.want)
			}
			if r.closes.Load() != 1 {
				t.Fatalf("closed %d times", r.closes.Load())
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := &budgetTestReader{raw: []byte(multipartFixture)}
	if _, err := ParseBoundedReader(ctx, r); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if len(r.raw) != len(multipartFixture) || r.closes.Load() != 1 {
		t.Fatal("canceled entry read or leaked reader")
	}
}

func budgetAncestorResume(siblings int, attachment bool, nestedPayload []byte) []byte {
	var raw strings.Builder
	raw.WriteString("Content-Type: multipart/mixed; boundary=A\r\n\r\n--A\r\nContent-Type: multipart/mixed; boundary=B\r\n\r\n--B\r\nContent-Type: text/plain\r\n\r\nx--A--\r\nignored\r\n")
	for i := 0; i < siblings; i++ {
		raw.WriteString("--B\r\nContent-Type: text/plain\r\n")
		if attachment {
			raw.WriteString("Content-Disposition: attachment; filename=real.txt\r\n")
		}
		raw.WriteString("\r\nreal attachment\r\n")
	}
	if nestedPayload != nil {
		raw.WriteString("--B\r\n")
		raw.Write(nestedPayload)
		raw.WriteString("\r\n")
	}
	raw.WriteString("--B--\r\n--A--\r\n")
	return []byte(raw.String())
}
func TestMIMEPreparseBudgetAncestorTemporaryEOF(t *testing.T) {
	for _, tc := range []struct {
		name      string
		raw       []byte
		wantNodes int
		wantParts int
		want      error
	}{
		{"small_review_counterexample", budgetAncestorResume(1, true, nil), 4, 1, nil},
		{"nodes_exact", budgetAncestorResume(1021, false, nil), 1024, 0, nil},
		{"nodes_plus_one", budgetAncestorResume(1022, false, nil), 1025, 0, ErrMIMENodes},
		{"parts_exact", budgetAncestorResume(512, true, nil), 515, 512, nil},
		{"parts_plus_one", budgetAncestorResume(513, true, nil), 516, 513, ErrMIMEParts},
		{"resumed_depth_exact", budgetAncestorResume(0, false, budgetNested(30)), 33, 0, nil},
		{"resumed_depth_plus_one", budgetAncestorResume(0, false, budgetNested(31)), 34, 0, ErrMIMEDepth},
	} {
		t.Run(tc.name, func(t *testing.T) {
			original, err := enmime.ReadEnvelope(bytes.NewReader(tc.raw))
			if err != nil {
				t.Fatal(err)
			}
			nodes := len(original.Root.DepthMatchAll(func(*enmime.Part) bool { return true }))
			if nodes != tc.wantNodes || len(Parts(original)) != tc.wantParts {
				t.Fatalf("original actual nodes/parts %d/%d want %d/%d", nodes, len(Parts(original)), tc.wantNodes, tc.wantParts)
			}
			got, err := ParseBounded(tc.raw)
			if !errors.Is(err, tc.want) {
				t.Fatalf("error %v want %v", err, tc.want)
			}
			if tc.want == nil {
				if !reflect.DeepEqual(got, original) {
					t.Fatal("ancestor resume changed envelope")
				}
				assertActualBudgetAgreement(t, tc.raw, original)
			} else if got != nil {
				t.Fatal("refused resumed subtree exposed")
			}
		})
	}
}
