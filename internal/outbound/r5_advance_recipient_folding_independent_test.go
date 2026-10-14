package outbound

import (
	"bytes"
	"fmt"
	"mime"
	"net/mail"
	"reflect"
	"strings"
	"testing"
)

func advanceIndependentAddressLength(length int) string {
	return strings.Repeat("a", length-len("@example.test")) + "@example.test"
}

func TestAdvanceRecipientIndependentTokenBounds(t *testing.T) {
	for _, role := range []string{"To", "Cc"} {
		for _, length := range []int{73, 74, 996, 997} {
			for _, multiple := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%d/%t", role, length, multiple), func(t *testing.T) {
					values := []string{advanceIndependentAddressLength(length)}
					if multiple {
						values = append(values, "last@example.test")
					}
					message := Message{From: "sender@example.test", Subject: "Bounded list", TextBody: "body"}
					if role == "To" {
						message.To = values
					} else {
						message.CC = values
					}
					raw, err := Build(message)
					if length == 997 && multiple {
						if err == nil || raw != nil {
							t.Fatal("a full legacy token plus its comma exceeded the safe continuation budget")
						}
						return
					}
					if err != nil {
						t.Fatalf("representable mailbox token rejected: %v", err)
					}
					parsed, err := mail.ReadMessage(bytes.NewReader(raw))
					if err != nil {
						t.Fatal(err)
					}
					addresses, err := parsed.Header.AddressList(role)
					if err != nil || len(addresses) != len(values) {
						t.Fatalf("folded list lost recipients: %v, %v", addresses, err)
					}
					for i, value := range values {
						if addresses[i].Address != value {
							t.Fatalf("folding changed recipient order or identity: %q != %q", addresses[i].Address, value)
						}
					}
					header := strings.SplitN(string(raw), "\r\n\r\n", 2)[0]
					for _, line := range strings.Split(header, "\r\n") {
						if len(line) > 998 || length < 78 && len(line) > 78 {
							t.Fatalf("physical header line has %d octets", len(line))
						}
					}
				})
			}
		}
	}
}

func TestAdvanceRecipientIndependentQuotedTokensAndOtherHeaders(t *testing.T) {
	for _, role := range []string{"To", "Cc"} {
		t.Run(role, func(t *testing.T) {
			values := []string{
				`"quoted space,comma\"quote\\slash"@example.test`,
				`"another,separator@inside"@example.test`,
				`"多字节 空白,逗号"@example.test`,
			}
			message := Message{From: `"Named Sender" <sender@example.test>`,
				Subject: "  Chinese 主题 and literal =?UTF-8?Q?preserve?=\t", TextBody: "body", BCC: []string{"hidden@example.test"}}
			if role == "To" {
				message.To = values
			} else {
				message.CC = values
			}
			before := append([]string(nil), message.EnvelopeRecipients()...)
			raw, err := Build(message)
			if err != nil {
				t.Fatal(err)
			}
			parsed, err := mail.ReadMessage(bytes.NewReader(raw))
			if err != nil {
				t.Fatal(err)
			}
			addresses, err := parsed.Header.AddressList(role)
			if err != nil || len(addresses) != len(values) {
				t.Fatalf("quoted mailbox list invalid: %v %v", addresses, err)
			}
			for i, input := range values {
				want, err := mail.ParseAddress(input)
				if err != nil || addresses[i].Address != want.Address {
					t.Fatalf("quoted mailbox identity changed: %q %v", input, err)
				}
			}
			if got, err := new(mime.WordDecoder).DecodeHeader(parsed.Header.Get("Subject")); err != nil || got != message.Subject {
				t.Fatalf("Subject encoding changed: %q %v", got, err)
			}
			if parsed.Header.Get("From") != message.From || parsed.Header.Get("Bcc") != "" ||
				strings.Contains(strings.SplitN(string(raw), "\r\n\r\n", 2)[0], message.BCC[0]) || !reflect.DeepEqual(message.EnvelopeRecipients(), before) {
				t.Fatal("From, BCC or caller envelope contract changed")
			}
			field := ""
			for _, line := range strings.Split(strings.SplitN(string(raw), "\r\n\r\n", 2)[0], "\r\n") {
				if !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
					field, _, _ = strings.Cut(line, ":")
				}
				if field == role && len(line) > 78 {
					t.Fatalf("foldable quoted field exceeded 78 octets: %d", len(line))
				}
			}
		})
	}
}
