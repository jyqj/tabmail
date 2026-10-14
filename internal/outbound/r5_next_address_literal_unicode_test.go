package outbound

import (
	"strings"
	"testing"
)

func TestR5NextAddressLiteralUnicodeOffsets(t *testing.T) {
	for _, tc := range []struct{ name, input, want string }{
		{"shorter-lowercase-display", "K <Reader@[IPv6:::1]>", "reader@[ipv6:::1]"},
		{"longer-lowercase-display", strings.Repeat("Ⱥ", 16) + " <reader@[IPv6:::1]>", "reader@[ipv6:::1]"},
		{"shorter-lowercase-local", `"K local"@[IPv6:::1]`, `"k local"@[ipv6:::1]`},
		{"longer-lowercase-local", `"` + strings.Repeat("Ⱥ", 16) + `@Team"@[IPv6:::1]`, `"` + strings.Repeat("ⱥ", 16) + `@team"@[ipv6:::1]`},
		{"mixed-display-comment", "KȺ mail (联系@[IPv6:::2]) <Reader@[IPv6:::1]> (Ⱥ@[IPv6:::3])", "reader@[ipv6:::1]"},
		{"malformed-unicode-display", strings.Repeat("Ⱥ", 16) + " <reader@[IPv6:::1]", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if v := recover(); v != nil {
					t.Errorf("recipient parser panicked on Unicode input: %v", v)
				}
			}()
			got, err := ParseRecipientAddress(tc.input)
			if tc.want == "" {
				if err == nil {
					t.Fatal("malformed mailbox accepted")
				}
				return
			}
			if err != nil || got.Envelope != tc.want {
				t.Fatalf("Unicode literal mailbox rejected or changed: %+v %v", got, err)
			}
		})
	}
}
