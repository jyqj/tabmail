package mailcontent

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
)

// A small, deterministic local microbenchmark, not the R5 S/M/L performance
// acceptance set. Overlay this exact file on the base to compare the same input.
func BenchmarkParserResourceCachedDocument(b *testing.B) {
	p := New(&fixtureObjects{raw: []byte(multipartFixture)})
	id, ctx := uuid.New(), context.Background()
	if _, err := p.Document(ctx, id, "hot"); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := p.Document(ctx, id, "hot"); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkParserResourceColdDocument(b *testing.B) {
	p := New(&fixtureObjects{raw: []byte(multipartFixture)})
	id, ctx := uuid.New(), context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := p.Document(ctx, id, fmt.Sprintf("cold-%d", i)); err != nil {
			b.Fatal(err)
		}
	}
}
