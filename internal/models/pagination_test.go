package models

import (
	"math"
	"testing"
)

func TestPageOffsetPreservesBoundsWithoutWrapping(t *testing.T) {
	for _, tc := range []struct {
		name string
		page Page
		want int
	}{
		{"first", Page{Page: 1, PerPage: 30}, 0},
		{"ordinary", Page{Page: 3, PerPage: 30}, 60},
		{"largest_page_unit_size", Page{Page: math.MaxInt, PerPage: 1}, math.MaxInt - 1},
		{"last_representable_offset", Page{Page: math.MaxInt/100 + 1, PerPage: 100}, (math.MaxInt / 100) * 100},
		{"first_unrepresentable_offset", Page{Page: math.MaxInt/100 + 2, PerPage: 100}, math.MaxInt},
		{"largest_page", Page{Page: math.MaxInt, PerPage: 100}, math.MaxInt},
		{"wrap_to_zero", Page{Page: math.MaxInt/2 + 2, PerPage: 4}, math.MaxInt},
		{"wrap_to_small_positive", Page{Page: math.MaxInt/2 + 3, PerPage: 4}, math.MaxInt},
		{"zero_page", Page{PerPage: 30}, 0},
		{"negative_page", Page{Page: math.MinInt, PerPage: 30}, 0},
		{"zero_size", Page{Page: math.MaxInt}, 0},
		{"negative_size", Page{Page: 2, PerPage: math.MinInt}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.page.Offset(); got != tc.want {
				t.Fatalf("Offset(%+v) = %d, want %d", tc.page, got, tc.want)
			}
		})
	}
}

func TestPageNormalizationRetainsPaginationContract(t *testing.T) {
	for _, tc := range []struct {
		name     string
		in, want Page
	}{
		{"defaults", Page{}, Page{Page: 1, PerPage: 30}},
		{"negative_values", Page{Page: math.MinInt, PerPage: math.MinInt}, Page{Page: 1, PerPage: 30}},
		{"maximum_size", Page{Page: 2, PerPage: math.MaxInt}, Page{Page: 2, PerPage: 100}},
		{"largest_page_retained", Page{Page: math.MaxInt, PerPage: 100}, Page{Page: math.MaxInt, PerPage: 100}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.in.Normalize(); got != tc.want {
				t.Fatalf("Normalize(%+v) = %+v, want %+v", tc.in, got, tc.want)
			}
		})
	}
}
