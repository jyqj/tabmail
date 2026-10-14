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
			// Test preflight directly as well: rejection is before envelope decode,
			// not a count of an already allocated enmime tree.
			b := mimeBudget{ctx: context.Background()}
			if err := b.walk(tc.raw, 1); !errors.Is(err, tc.want) {
				t.Fatalf("preflight = %v, want %v", err, tc.want)
			}
			env, err := ParseBounded(tc.raw)
			if !errors.Is(err, tc.want) {
				t.Fatalf("parse = %v, want %v", err, tc.want)
			}
			if tc.want != nil && env != nil {
				t.Fatal("rejected tree exposed")
			}
			if tc.want == nil {
				want, err := enmime.ReadEnvelope(bytes.NewReader(tc.raw))
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(env, want) {
					t.Fatal("admitted envelope changed")
				}
				actual := len(env.Root.DepthMatchAll(func(*enmime.Part) bool { return true }))
				if b.nodes != actual {
					t.Fatalf("preflight nodes %d != allocated nodes %d", b.nodes, actual)
				}
			}
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
				want, err := enmime.ReadEnvelope(bytes.NewReader(raw))
				if err != nil {
					t.Fatal(err)
				}
				if len(Parts(want)) != 0 {
					t.Fatalf("original parts %d, want 0", len(Parts(want)))
				}
				b := mimeBudget{ctx: context.Background()}
				if err := b.walk(raw, 1); err != nil {
					t.Fatal(err)
				}
				if b.parts != 0 || b.nodes != maxParts+2 {
					t.Fatalf("preflight nodes=%d parts=%d", b.nodes, b.parts)
				}
				got, err := ParseBounded(raw)
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(got, want) {
					t.Fatal("missing/empty CT disposition projection changed")
				}
			})
		}
	}
	for _, count := range []int{256, 257} {
		t.Run(fmt.Sprintf("octet_inline_%d", count), func(t *testing.T) {
			raw := budgetWide(count, "Content-Type: application/octet-stream\r\nContent-Disposition: inline")
			want, err := enmime.ReadEnvelope(bytes.NewReader(raw))
			if err != nil {
				t.Fatal(err)
			}
			if len(want.Attachments) != count || len(want.Inlines) != count || len(Parts(want)) != 2*count {
				t.Fatalf("original double projection changed")
			}
			b := mimeBudget{ctx: context.Background()}
			preErr := b.walk(raw, 1)
			got, err := ParseBounded(raw)
			if count == 256 {
				if preErr != nil || err != nil || b.parts != 512 || !reflect.DeepEqual(got, want) {
					t.Fatalf("exact double projection rejected/changed: %v / %v, count %d", preErr, err, b.parts)
				}
			} else {
				if !errors.Is(preErr, ErrMIMEParts) || !errors.Is(err, ErrMIMEParts) || got != nil {
					t.Fatalf("double projection above 512 admitted: %v / %v", preErr, err)
				}
			}
		})
	}
}

// markerOffset is relative to the exact start of the child, including its
// MIME headers. It is not a body-padding approximation to the bufio fill.
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
				raw, child := budgetOffsetFixture(boundary, offset, terminator)
				for _, nested := range []bool{false, true} {
					t.Run(fmt.Sprintf("len_%d/offset_%d/final_%v/nested_%v", boundaryLen, offset, terminator, nested), func(t *testing.T) {
						if nested {
							raw = append([]byte("Content-Type: multipart/mixed; boundary=OUTER\r\n\r\n--OUTER\r\n"), raw...)
							raw = append(raw, []byte("\r\n--OUTER--\r\n")...)
						}
						end, err := budgetPartEnd(context.Background(), child, []byte("--"+boundary))
						if err != nil {
							t.Fatal(err)
						}
						// A delimiter can match with one leading byte in the small
						// window. A final marker fills that window, so cannot shift.
						expected := bytes.LastIndex(child, []byte("\r\n--"+boundary+"--"))
						if terminator && offset <= 4095 {
							expected = offset
						}
						if !terminator && offset <= 4096 {
							expected = offset - 1
						}
						if end != expected {
							t.Fatalf("part end=%d want %d (child-relative marker=%d)", end, expected, offset)
						}
						want, originalErr := enmime.ReadEnvelope(bytes.NewReader(raw))
						got, parseErr := ParseBounded(raw)
						if (originalErr == nil) != (parseErr == nil) {
							t.Fatalf("error changed: original %v bounded %v", originalErr, parseErr)
						}
						if originalErr == nil {
							if !reflect.DeepEqual(got, want) {
								t.Fatal("exact fill-offset envelope changed")
							}
							b := mimeBudget{ctx: context.Background()}
							if err := b.walk(raw, 1); err != nil {
								t.Fatal(err)
							}
							if b.nodes != len(want.Root.DepthMatchAll(func(*enmime.Part) bool { return true })) || b.parts != len(Parts(want)) {
								t.Fatal("offset preflight/projection differs from enmime")
							}
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
			b := mimeBudget{ctx: context.Background()}
			if err := b.walk(raw, 1); !errors.Is(err, ErrMIMEParse) {
				t.Fatalf("long recursive boundary admission %v", err)
			}
			if env, err := ParseBounded(raw); !errors.Is(err, ErrMIMEParse) || env != nil {
				t.Fatalf("long recursive boundary %v / %v", env, err)
			}
		})
	}
}

// A first-fill logical EOF does not itself advance Next to the next part.
// Next drains with atPartStart=false before inspecting delimiter lines.
// The apparent node after a mid-line marker is discarded, while a later
// CRLF-delimited real child still allocates a node and projects an attachment.
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
					firstEnd, err := budgetPartEnd(context.Background(), []byte(child), []byte("--"+boundary))
					if err != nil {
						t.Fatal(err)
					}
					expectedEnd := 4092
					if terminator {
						expectedEnd = 4093
					}
					if firstEnd != expectedEnd {
						t.Fatalf("initial end %d want %d", firstEnd, expectedEnd)
					}
					drain, err := budgetBoundaryEnd(context.Background(), []byte(child)[firstEnd:], []byte("--"+boundary), false)
					if err != nil {
						t.Fatal(err)
					}
					canonical := strings.Index(child, "\r\n--"+boundary+"\r\nContent-Type: text/plain\r\nContent-Disposition: attachment")
					if firstEnd+drain != canonical {
						t.Fatalf("post-EOF discard cursor %d want canonical %d", firstEnd+drain, canonical)
					}
					raw := []byte("Content-Type: multipart/mixed; boundary=" + boundary + "\r\n\r\n--" + boundary + "\r\n" + child)
					expectedNodes := 3
					if nested {
						raw = append([]byte("Content-Type: multipart/mixed; boundary=OUTER\r\n\r\n--OUTER\r\n"), raw...)
						raw = append(raw, []byte("\r\n--OUTER--\r\n")...)
						expectedNodes++
					}
					want, err := enmime.ReadEnvelope(bytes.NewReader(raw))
					if err != nil {
						t.Fatal(err)
					}
					actualNodes := len(want.Root.DepthMatchAll(func(*enmime.Part) bool { return true }))
					if actualNodes != expectedNodes || len(Parts(want)) != 1 {
						t.Fatalf("original nodes/parts %d/%d want %d/1", actualNodes, len(Parts(want)), expectedNodes)
					}
					b := mimeBudget{ctx: context.Background()}
					if err := b.walk(raw, 1); err != nil {
						t.Fatal(err)
					}
					if b.nodes != actualNodes || b.parts != len(Parts(want)) {
						t.Fatalf("preflight nodes/parts %d/%d original %d/%d", b.nodes, b.parts, actualNodes, len(Parts(want)))
					}
					got, err := ParseBounded(raw)
					if err != nil {
						t.Fatal(err)
					}
					if !reflect.DeepEqual(got, want) {
						t.Fatal("Read-EOF-Next-discard sequence changed envelope")
					}
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
				b := mimeBudget{ctx: context.Background()}
				if err := b.walk(raw, 1); err != nil {
					t.Fatal(err)
				}
				if b.nodes != len(want.Root.DepthMatchAll(func(*enmime.Part) bool { return true })) {
					t.Fatal("node walk differs from enmime")
				}
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
