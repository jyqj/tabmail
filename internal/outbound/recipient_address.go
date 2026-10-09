package outbound

import (
	"net/mail"
	"strings"
)

// RecipientAddress keeps the SMTP addr-spec separate from the decoded address
// used by the existing suppression list. A quoted local part can contain an @,
// comma, space or escaped character; mail.Address.Address deliberately removes
// its quotes and therefore cannot be placed directly on the SMTP/MIME wire.
type RecipientAddress struct {
	Envelope string
	Identity string
}

// ParseRecipientAddress preserves the established case-insensitive recipient
// identity while serializing the local part with exactly the quoting it needs.
// Display names remain excluded from the durable envelope, as before.
func ParseRecipientAddress(value string) (RecipientAddress, error) {
	parsed, err := parseRecipientMailbox(value)
	if err != nil {
		return RecipientAddress{}, err
	}
	identity := strings.ToLower(parsed.Address)
	// With no display name, Address.String always emits a single <addr-spec>.
	// Let net/mail escape the local part instead of hand-escaping user input.
	wire := (&mail.Address{Address: identity}).String()
	return RecipientAddress{Envelope: wire[1 : len(wire)-1], Identity: identity}, nil
}
