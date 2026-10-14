package handlers

import (
	"fmt"
	"net/http"
	"time"

	"tabmail/internal/models"
)

// The existing policy command replaces the complete persisted policy. Keep
// omitted/null values distinct from explicit false/[] so a partial request
// cannot silently switch receiving or storage off and erase domain rules.
type smtpPolicyInput struct {
	DefaultAccept       *bool     `json:"default_accept"`
	AcceptDomains       []*string `json:"accept_domains"`
	RejectDomains       []*string `json:"reject_domains"`
	DefaultStore        *bool     `json:"default_store"`
	StoreDomains        []*string `json:"store_domains"`
	DiscardDomains      []*string `json:"discard_domains"`
	RejectOriginDomains []*string `json:"reject_origin_domains"`
	// Historically accepted when clients echoed a GET response. The timestamp
	// is still ignored: persistence supplies the actual update time.
	UpdatedAt time.Time `json:"updated_at"`
}

func decodeSMTPPolicyInput(r *http.Request) (*models.SMTPPolicy, error) {
	var in smtpPolicyInput
	if err := decodeBody(r, &in); err != nil {
		return nil, err
	}
	if in.DefaultAccept == nil || in.DefaultStore == nil {
		return nil, fmt.Errorf("explicit default_accept and default_store booleans required")
	}
	out := &models.SMTPPolicy{DefaultAccept: *in.DefaultAccept, DefaultStore: *in.DefaultStore}
	for _, list := range []struct {
		in  []*string
		out *[]string
	}{
		{in.AcceptDomains, &out.AcceptDomains},
		{in.RejectDomains, &out.RejectDomains},
		{in.StoreDomains, &out.StoreDomains},
		{in.DiscardDomains, &out.DiscardDomains},
		{in.RejectOriginDomains, &out.RejectOriginDomains},
	} {
		if list.in == nil {
			return nil, fmt.Errorf("all five domain rule arrays must be explicit")
		}
		*list.out = make([]string, len(list.in))
		for i, domain := range list.in {
			if domain == nil {
				return nil, fmt.Errorf("domain rule entries must be strings")
			}
			(*list.out)[i] = *domain
		}
	}
	return out, nil
}
