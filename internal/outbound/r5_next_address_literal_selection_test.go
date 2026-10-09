package outbound

import (
	"strings"
	"testing"
)

func TestR5NextAddressLiteralDecoySelection(t *testing.T) {
	for _, tc := range []struct{ name, input, want string }{
		{"display-tags", `"` + strings.Repeat("display [IPv6:::2] ", 200) + `" <Reader@[IPv6:::1]>`, "reader@[ipv6:::1]"},
		{"trailing-comment-tags", "Reader@[IPv6:::1] (" + strings.Repeat("contact@[IPv6:::2] ", 200) + ")", "reader@[ipv6:::1]"},
		{"quoted-local-tags", `"Team@[IPv6:::2]"@[IPv6:::1]`, `"team@[ipv6:::2]"@[ipv6:::1]`},
		{"nested-comments", "Person (nested (contact@[IPv6:::2])) <Reader@[IPv6:::1]>", "reader@[ipv6:::1]"},
		{"multiple-addresses", strings.Repeat("reader@[IPv6:::1], ", 200) + "other@[IPv6:::2]", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got RecipientAddress
			var err error
			allocations := testing.AllocsPerRun(1, func() { got, err = ParseRecipientAddress(tc.input) })
			if tc.want == "" {
				if err == nil {
					t.Fatal("multiple recipients accepted as one address")
				}
			} else if err != nil || got.Envelope != tc.want {
				t.Fatalf("wrong literal selected: %+v %v", got, err)
			}
			// A long display name/comment must not cause one whole-address
			// allocation and reparse per decoy tag. Leave ample room for the
			// standard parser while making the previous repeated scan visible.
			if tc.name == "display-tags" && allocations > 2000 {
				t.Fatalf("recipient parser reparsed decoy literals: %.0f allocations", allocations)
			}
		})
	}
}
