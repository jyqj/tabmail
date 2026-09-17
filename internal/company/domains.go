package company

import (
	"time"

	"github.com/google/uuid"
)

// Company-facing projections of the domain setup wizard. They deliberately
// omit the open-platform zone fields (visibility, random subdomains, routes,
// owner) and never serialize signing material: the administrator only needs
// what to publish in DNS and whether verification passed.

// Domain is one company domain with its verification summary and the DNS
// records the administrator must publish.
type Domain struct {
	ID          uuid.UUID `json:"id"`
	Domain      string    `json:"domain"`
	IsVerified  bool      `json:"is_verified"`
	MXVerified  bool      `json:"mx_verified"`
	DKIMEnabled bool      `json:"dkim_enabled"`
	TXTRecord   string    `json:"txt_record"`
	ExpectedMX  string    `json:"expected_mx"`
	DKIMHost    string    `json:"dkim_host,omitempty"`
	DKIMRecord  string    `json:"dkim_record,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

// DNSCheck is the outcome of one live DNS lookup.
type DNSCheck struct {
	Status  string   `json:"status"`
	Details []string `json:"details,omitempty"`
}

// DomainVerificationChecks mirrors the per-record verification outcomes.
type DomainVerificationChecks struct {
	TXT   DNSCheck `json:"txt"`
	MX    DNSCheck `json:"mx"`
	SPF   DNSCheck `json:"spf"`
	DKIM  DNSCheck `json:"dkim"`
	DMARC DNSCheck `json:"dmarc"`
}

// DomainVerification is the verification view for one company domain: the
// same summary as Domain plus the live per-record check results.
type DomainVerification struct {
	ID          uuid.UUID                `json:"id"`
	Domain      string                   `json:"domain"`
	IsVerified  bool                     `json:"is_verified"`
	MXVerified  bool                     `json:"mx_verified"`
	DKIMEnabled bool                     `json:"dkim_enabled"`
	TXTRecord   string                   `json:"txt_record"`
	ExpectedMX  string                   `json:"expected_mx"`
	DKIMHost    string                   `json:"dkim_host,omitempty"`
	DKIMRecord  string                   `json:"dkim_record,omitempty"`
	Checks      DomainVerificationChecks `json:"checks"`
}
